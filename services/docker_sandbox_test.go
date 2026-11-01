package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerPrototypePreservesUncertainRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unresolved.json")
	contents := []byte(`{"state":"create_uncertain"}`)
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	s := &dockerSandbox{image: "sha256:" + strings.Repeat("a", 64), ownershipDir: dir}
	called := false
	s.callOverride = func(context.Context, []byte, ...string) (string, string, error) { called = true; return "", "", nil }
	res, err := s.Run(context.Background(), SandboxSpec{Language: "python", Files: map[string]string{"f.py": "x = 1\n"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1})
	got, readErr := os.ReadFile(path)
	if res != nil || err == nil || called || readErr != nil || string(got) != string(contents) {
		t.Fatalf("unresolved record changed or daemon contacted: %v %v %t %v", res, err, called, readErr)
	}
}

func TestDockerPrototypeRejectsUntrustedCommandsBeforeExecution(t *testing.T) {
	s := &dockerSandbox{docker: "/missing/docker", image: "sha256:" + strings.Repeat("a", 64)}
	for _, spec := range []SandboxSpec{
		{Language: "python", Files: map[string]string{"f.py": "print(1)"}, Commands: [][]string{{"sh", "-c", "touch /tmp/marker"}}, TimeoutSec: 1},
		{Language: "python", Files: map[string]string{"../escape": "x"}, Commands: [][]string{{"python3", "-m", "py_compile", "../escape"}}, TimeoutSec: 1},
		{Language: "python", Files: map[string]string{"f.py": "x"}, Commands: [][]string{{"python3", "-m", "py_compile", "missing.py"}}, TimeoutSec: 1},
		{Language: "python", Files: map[string]string{"f.py": "x"}, Commands: nil, TimeoutSec: 1},
		{RepositoryContext: true, Language: "python", Files: map[string]string{"f.py": "x"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1},
		{Language: "python", Files: map[string]string{"f.py": "x"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 0},
		{Language: "python", Files: map[string]string{"f.py": "x"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 61},
		{Language: "javascript", Files: map[string]string{"f.py": "x"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1},
	} {
		res, err := s.Run(context.Background(), spec)
		if err == nil || res != nil || strings.Contains(err.Error(), "missing/docker") {
			t.Fatalf("spec was not rejected before execution: %+v %v", res, err)
		}
	}
}

func TestDockerPrototypeBoundsOutputAndRequiresPinnedImage(t *testing.T) {
	var b boundedDockerOutput
	if _, err := b.Write([]byte(strings.Repeat("x", 64<<10))); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("unbounded output accepted")
	}
	if _, err := newDockerSandbox("python:latest"); err == nil {
		t.Fatal("mutable image accepted")
	}
}

func TestDockerPrototypeHonorsAlreadyCancelledContext(t *testing.T) {
	s := &dockerSandbox{docker: "/missing/docker", image: "sha256:" + strings.Repeat("a", 64)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := s.Run(ctx, SandboxSpec{Language: "python", Files: map[string]string{"f.py": "x = 1\n"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1})
	if err != context.Canceled || res != nil {
		t.Fatalf("cancelled run: %+v %v", res, err)
	}
}

func TestDockerPrototypeDoesNotContactDaemonWithoutRecovery(t *testing.T) {
	s := &dockerSandbox{image: "sha256:" + strings.Repeat("a", 64), ownershipDir: t.TempDir()}
	called := false
	s.callOverride = func(context.Context, []byte, ...string) (string, string, error) {
		called = true
		return "", "", nil
	}
	res, err := s.Run(context.Background(), SandboxSpec{Language: "python", Files: map[string]string{"f.py": "x = 1\n"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1})
	if res != nil || err == nil || called {
		t.Fatalf("unrecoverable lifecycle contacted daemon: result=%+v err=%v called=%t", res, err, called)
	}
}

func TestDockerPrototypePersistenceFailurePreventsDocker(t *testing.T) {
	s := &dockerSandbox{image: "sha256:" + strings.Repeat("a", 64), ownershipDir: t.TempDir() + "/missing"}
	called := false
	s.callOverride = func(context.Context, []byte, ...string) (string, string, error) { called = true; return "", "", nil }
	res, err := s.Run(context.Background(), SandboxSpec{Language: "python", Files: map[string]string{"f.py": "x = 1\n"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1})
	if res != nil || err == nil || called {
		t.Fatalf("persistence failure contacted daemon: %+v %v called=%t", res, err, called)
	}
}

func TestDockerPrototypeMissingOwnershipPreventsDocker(t *testing.T) {
	s := &dockerSandbox{image: "sha256:" + strings.Repeat("a", 64)}
	called := false
	s.callOverride = func(context.Context, []byte, ...string) (string, string, error) { called = true; return "", "", nil }
	res, err := s.Run(context.Background(), SandboxSpec{Language: "python", Files: map[string]string{"f.py": "x = 1\n"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1})
	if res != nil || err == nil || called {
		t.Fatalf("missing ownership contacted daemon: %+v %v called=%t", res, err, called)
	}
}
