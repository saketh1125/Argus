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
		{"https://github.com/saketh/codesentinel", "saketh", "codesentinel"},
		{"https://github.com/saketh/codesentinel.git", "saketh", "codesentinel"},
		{"https://github.com/octocat/Hello-World/", "octocat", "Hello-World"},
		{"git@github.com:saketh/codesentinel.git", "saketh", "codesentinel"},
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
