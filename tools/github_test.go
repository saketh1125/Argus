package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseURL(t *testing.T) {
	cases := []struct {
		url, owner, repo string
	}{
		{"https://github.com/saketh1125/argus", "saketh1125", "argus"},
		{"https://github.com/saketh1125/argus.git", "saketh1125", "argus"},
		{"https://github.com/octocat/Hello-World/", "octocat", "Hello-World"},
		{"git@github.com:saketh1125/argus.git", "saketh1125", "argus"},
	}
	for _, c := range cases {
		owner, repo, err := parseGitHubURL(c.url)
		if err != nil {
			t.Fatalf("parseGitHubURL(%q) error: %v", c.url, err)
		}
		if owner != c.owner || repo != c.repo {
			t.Errorf("parseGitHubURL(%q) = %q/%q, want %q/%q", c.url, owner, repo, c.owner, c.repo)
		}
	}
}

func TestMockParseURLFallback(t *testing.T) {
	m := newMockGitHub()
	owner, repo, err := m.ParseURL("not a url at all")
	if err != nil {
		t.Fatalf("mock ParseURL unexpected error: %v", err)
	}
	if owner != "demo-owner" || repo != "demo-repo" {
		t.Errorf("fallback = %q/%q, want demo-owner/demo-repo", owner, repo)
	}
}

func TestMockCloneCopiesSampleRepo(t *testing.T) {
	if _, err := os.Stat(sampleRepoPath); err != nil {
		t.Skipf("sample repo not present at %s: %v", sampleRepoPath, err)
	}
	m := newMockGitHub()
	dest := filepath.Join(t.TempDir(), "clone")
	got, err := m.Clone(context.Background(), "https://github.com/x/y", dest)
	if err != nil {
		t.Fatalf("Clone error: %v", err)
	}
	if got != dest {
		t.Errorf("Clone returned %q, want %q", got, dest)
	}
	if _, err := os.Stat(filepath.Join(dest, "auth.py")); err != nil {
		t.Errorf("expected auth.py copied into %s: %v", dest, err)
	}
}

func TestMockCreatePRAndIssue(t *testing.T) {
	m := newMockGitHub()
	n1, url1, err := m.CreatePR(context.Background(), "o", "r", PRSpec{Title: "fix"})
	if err != nil || n1 != 1 {
		t.Fatalf("CreatePR = %d, %q, %v; want number 1", n1, url1, err)
	}
	if url1 != "https://github.com/o/r/pull/1" {
		t.Errorf("PR url = %q", url1)
	}
	n2, _, _ := m.CreatePR(context.Background(), "o", "r", PRSpec{Title: "fix2"})
	if n2 != 2 {
		t.Errorf("second PR number = %d, want 2", n2)
	}
	in, iurl, _ := m.CreateIssue(context.Background(), "o", "r", "summary", "body")
	if in != 1 || iurl != "https://github.com/o/r/issues/1" {
		t.Errorf("CreateIssue = %d, %q", in, iurl)
	}
	if len(m.Actions()) == 0 {
		t.Error("expected recorded actions")
	}
}

func TestLiveGitHubWritesFailClosed(t *testing.T) {
	g := newGitHubREST("not-a-real-token")
	ctx := context.Background()
	if g.CreateBranch(ctx, "upstream", "repo", "main", "branch") == nil {
		t.Error("branch write allowed")
	}
	if g.CommitFile(ctx, "upstream", "repo", CommitSpec{Content: "partial"}) == nil {
		t.Error("file write allowed")
	}
	if _, _, err := g.CreatePR(ctx, "upstream", "repo", PRSpec{}); err == nil {
		t.Error("PR write allowed")
	}
	if _, _, err := g.CreateIssue(ctx, "upstream", "repo", "title", "body"); err == nil {
		t.Error("issue write allowed")
	}
}

func TestParseURLRejectsRemoteHelperAndSpoofedHosts(t *testing.T) {
	for _, raw := range []string{"ext::sh -c touch marker", "--upload-pack=sh", "https://evil.example/github.com/owner/repo", "https://github.com.evil.example/owner/repo", "https://github.com/../repo", "https://github.com/owner/.."} {
		if _, _, err := parseGitHubURL(raw); err == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
}

func TestCopyDirRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("sensitive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "copy")
	if err := copyDir(root, dest); err == nil {
		t.Fatal("copied a symlink to outside the repository")
	}
	if _, err := os.Stat(filepath.Join(dest, "link")); !os.IsNotExist(err) {
		t.Fatalf("escaped file copied: %v", err)
	}
}
