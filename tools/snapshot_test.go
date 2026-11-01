package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotFixture(t *testing.T, files map[string]string, symlink bool) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	home := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Argus Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Argus Test", "GIT_COMMITTER_EMAIL=test@example.invalid"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s: %v", out, err)
		}
	}
	run("init", "-q")
	run("remote", "add", "origin", "https://github.com/owner/repo.git")
	for name, content := range files {
		file := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if symlink {
		if err := os.Symlink("file.go", filepath.Join(dir, "link.go")); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "--all")
	run("-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "fixture")
	sha, err := ResolveGitCommit(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, sha
}

func TestSnapshotPinnedRepositoryUsesCommittedBytes(t *testing.T) {
	files := map[string]string{"file.go": "package p\n", "nested/test.txt": "fixture\n"}
	dir, sha := snapshotFixture(t, files, false)
	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("modified worktree"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := SnapshotPinnedRepository(context.Background(), dir, "github.com/owner/repo", sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(files) {
		t.Fatalf("unexpected file count: %d", len(got))
	}
	for name, content := range files {
		if got[name] != content {
			t.Errorf("wrong content for %s", name)
		}
	}
	if _, err := SnapshotPinnedRepository(context.Background(), dir, "github.com/other/repo", sha); err == nil {
		t.Fatal("wrong origin accepted")
	}
	if _, err := SnapshotPinnedRepository(context.Background(), dir, "github.com/owner/repo", strings.Repeat("a", 40)); err == nil {
		t.Fatal("stale commit accepted")
	}
}

func TestSnapshotPinnedRepositoryRejectsSymlinksAndOversizedFiles(t *testing.T) {
	dir, sha := snapshotFixture(t, map[string]string{"file.go": "package p\n"}, true)
	if _, err := SnapshotPinnedRepository(context.Background(), dir, "github.com/owner/repo", sha); err == nil {
		t.Fatal("symlink tree accepted")
	}
	dir, sha = snapshotFixture(t, map[string]string{"file.go": strings.Repeat("x", (1<<20)+1)}, false)
	if _, err := SnapshotPinnedRepository(context.Background(), dir, "github.com/owner/repo", sha); err == nil {
		t.Fatal("oversized blob accepted")
	}
}
