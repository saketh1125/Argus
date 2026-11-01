package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

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

func TestDockerPrototypeCleanupFailsClosedWhenDaemonUnavailable(t *testing.T) {
	s := &dockerSandbox{docker: "/missing/docker", image: "sha256:" + strings.Repeat("a", 64)}
	if err := s.cleanup("argus-experimental-test"); err == nil {
		t.Fatal("unverifiable cleanup accepted")
	}
}

func TestDockerPrototypeNeverSucceedsWhenCleanupCannotBeVerified(t *testing.T) {
	s := &dockerSandbox{image: "sha256:" + strings.Repeat("a", 64)}
	s.callOverride = func(_ context.Context, _ []byte, args ...string) (string, string, error) {
		switch args[0] {
		case "create", "start", "exec":
			return "", "", nil
		case "rm", "inspect":
			return "", "daemon unavailable", fmt.Errorf("daemon unavailable")
		default:
			return "", "", fmt.Errorf("unexpected command")
		}
	}
	res, err := s.Run(context.Background(), SandboxSpec{Language: "python", Files: map[string]string{"f.py": "x = 1\n"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1})
	if res != nil || err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("unverified cleanup accepted: %+v %v", res, err)
	}
}

func TestDockerPrototypeCancelsAndCleansUp(t *testing.T) {
	s := &dockerSandbox{image: "sha256:" + strings.Repeat("a", 64)}
	removed, checked := false, false
	s.callOverride = func(ctx context.Context, input []byte, args ...string) (string, string, error) {
		switch args[0] {
		case "create", "start":
			return "", "", nil
		case "exec":
			if input != nil {
				return "", "", nil
			}
			<-ctx.Done()
			return "", "", ctx.Err()
		case "rm":
			removed = true
			return "", "", nil
		case "inspect":
			checked = true
			return "", "no such object", fmt.Errorf("not found")
		default:
			return "", "", fmt.Errorf("unexpected command")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res, err := s.Run(ctx, SandboxSpec{Language: "python", Files: map[string]string{"f.py": "x = 1\n"}, Commands: [][]string{{"python3", "-m", "py_compile", "f.py"}}, TimeoutSec: 1})
	if res != nil || !errors.Is(err, context.DeadlineExceeded) || !removed || !checked {
		t.Fatalf("deadline did not clean up: %+v %v removed=%t checked=%t", res, err, removed, checked)
	}
}
