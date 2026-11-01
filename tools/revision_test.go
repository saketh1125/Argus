package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

func TestResolveGitCommit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Argus Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Argus Test", "GIT_COMMITTER_EMAIL=test@example.invalid"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s: %v", out, err)
		}
	}
	run("init", "-q")
	run("remote", "add", "origin", "https://github.com/Owner/Repo.git")
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	run("add", "file", "link")
	run("-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "fixture")
	sha, err := ResolveGitCommit(context.Background(), dir)
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(sha) {
		t.Fatalf("invalid revision %q: %v", sha, err)
	}
	if err := VerifyPinnedFile(context.Background(), dir, "github.com/owner/repo", sha, "file", []byte("content")); err != nil {
		t.Fatalf("pinned file rejected: %v", err)
	}
	if err := VerifyPinnedFile(context.Background(), dir, "github.com/owner/repo", sha, "link", []byte("file")); err == nil {
		t.Fatal("Git symlink accepted as regular file")
	}
	if err := VerifyPinnedFile(context.Background(), dir, "github.com/owner/repo", sha, "../file", []byte("content")); err == nil {
		t.Fatal("traversal accepted")
	}
	if err := VerifyPinnedFile(context.Background(), dir, "github.com/owner/other", sha, "file", []byte("content")); err == nil {
		t.Fatal("wrong repository accepted")
	}
	if err := VerifyPinnedFile(context.Background(), dir, "github.com/owner/repo", sha, "file", []byte("modified")); err == nil {
		t.Fatal("modified file accepted")
	}
	if err := VerifyGitCommit(context.Background(), dir, sha); err != nil {
		t.Fatalf("current revision rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "file")
	run("-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "second")
	changed, err := ResolveGitCommit(context.Background(), dir)
	if err != nil || changed == sha {
		t.Fatalf("changed HEAD not detected: %q %v", changed, err)
	}
	if err := VerifyGitCommit(context.Background(), dir, sha); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err := ResolveGitCommit(context.Background(), t.TempDir()); err == nil {
		t.Fatal("accepted non-repository")
	}
}
