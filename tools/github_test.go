package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestParseURLRejectsLookalikeHostsAndNonRepositoryPaths(t *testing.T) {
	for _, value := range []string{
		"https://evil.example/github.com/o/r",
		"https://github.com.evil.example/o/r",
		"http://github.com/o/r",
		"https://github.com/o/r/tree/main",
		"https://user@github.com/o/r",
		"https://github.com/o/r?tab=readme",
	} {
		if _, _, err := parseGitHubURL(value); err == nil {
			t.Errorf("expected target URL %q to be rejected", value)
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

func TestGitHubCommitFileUsesValidatedDiffAndHashesBranchContent(t *testing.T) {
	original := "package demo\nfunc f() { old() }\n"
	patched := "package demo\nfunc f() { fixed() }\n"
	originalSum, patchedSum := sha256.Sum256([]byte(original)), sha256.Sum256([]byte(patched))
	putCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"sha": "blob-sha", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(original)),
			})
			return
		}
		if r.Method == http.MethodPut {
			putCalled = true
			var body struct {
				Content string `json:"content"`
				SHA     string `json:"sha"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PUT: %v", err)
			}
			content, err := base64.StdEncoding.DecodeString(body.Content)
			if err != nil || !bytes.Equal(content, []byte(patched)) || body.SHA != "blob-sha" {
				t.Errorf("unexpected guarded content update: sha=%q content=%q err=%v", body.SHA, content, err)
			}
			w.WriteHeader(http.StatusCreated)
			return
		}
		t.Errorf("unexpected method %s", r.Method)
	}))
	defer server.Close()

	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL}
	err := client.CommitFile(context.Background(), "o", "r", CommitSpec{
		Branch: "feature", Path: "src/f.go", Content: patched, Patch: "@@ minimal @@\n",
		ExpectedContentSHA256:        hex.EncodeToString(originalSum[:]),
		ExpectedPatchedContentSHA256: hex.EncodeToString(patchedSum[:]), Message: "fix",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !putCalled {
		t.Fatal("expected guarded content update")
	}
}

func TestGitHubCommitFileRejectsConflictingBranchModification(t *testing.T) {
	validatedOriginal := "package demo\nfunc f() { old() }\n"
	remoteModified := "package demo\nfunc f() { concurrent() }\n"
	patched := "package demo\nfunc f() { fixed() }\n"
	validatedSum, patchedSum := sha256.Sum256([]byte(validatedOriginal)), sha256.Sum256([]byte(patched))
	putCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"sha": "new-blob", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(remoteModified)),
			})
			return
		}
		if r.Method == http.MethodPut {
			putCalled = true
			w.WriteHeader(http.StatusCreated)
			return
		}
		t.Errorf("unexpected method %s", r.Method)
	}))
	defer server.Close()

	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL}
	err := client.CommitFile(context.Background(), "o", "r", CommitSpec{
		Branch: "feature", Path: "src/f.go", Content: patched, Patch: "@@ minimal @@\n",
		ExpectedContentSHA256:        hex.EncodeToString(validatedSum[:]),
		ExpectedPatchedContentSHA256: hex.EncodeToString(patchedSum[:]), Message: "fix",
	})
	if err == nil || !strings.Contains(err.Error(), "changed since validation") {
		t.Fatalf("expected stale source conflict, got %v", err)
	}
	if putCalled {
		t.Fatal("must not PUT replacement content over concurrent changes")
	}
}

func TestGitHubCommitFileRejectsMissingGuards(t *testing.T) {
	client := &githubREST{token: "test", http: http.DefaultClient, baseURL: "http://127.0.0.1"}
	err := client.CommitFile(context.Background(), "o", "r", CommitSpec{Path: "f.go", Content: "replacement"})
	if err == nil || !strings.Contains(err.Error(), "digests are required") {
		t.Fatalf("expected fail-closed missing guard, got %v", err)
	}
}

func TestGitHubCreateBranchRejectsMovedBaseBeforeCreatingRef(t *testing.T) {
	postCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": "new-base"}})
			return
		}
		postCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL}
	err := client.CreateBranch(context.Background(), "o", "r", "main", "codesentinel/fix", "analyzed-base")
	if err == nil || !strings.Contains(err.Error(), "changed since repository scan") {
		t.Fatalf("expected stale base rejection, got %v", err)
	}
	if postCalled {
		t.Fatal("must not create a branch from a base different from the analyzed commit")
	}
}

func TestGitHubCreatePRRejectsMovedBase(t *testing.T) {
	postCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": "new-base"}})
			return
		}
		postCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL}
	_, _, err := client.CreatePR(context.Background(), "o", "r", PRSpec{Head: "feature", Base: "main", ExpectedBaseSHA: "analyzed-base"})
	if err == nil || !strings.Contains(err.Error(), "changed during analysis") {
		t.Fatalf("expected moved base rejection, got %v", err)
	}
	if postCalled {
		t.Fatal("must not create a PR after the base moves")
	}
}

func TestGitHubWriteFailureIsNotRetriedAndMarkedUncertain(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("temporary failure"))
	}))
	defer server.Close()
	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL}
	err := client.do(context.Background(), http.MethodPost, server.URL+"/writes", map[string]string{"x": "y"}, nil)
	if err == nil || !strings.Contains(err.Error(), "write outcome may be uncertain") {
		t.Fatalf("expected an uncertain write failure, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("write request retried %d times; want one attempt", requests)
	}
}

func TestGitHubResponseSizeIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxGitHubResponseBytes+1)))
	}))
	defer server.Close()
	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL}
	var out map[string]any
	err := client.do(context.Background(), http.MethodGet, server.URL+"/large", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("expected oversized response rejection, got %v", err)
	}
}

func TestGitHubSuccessfulWriteWithMalformedResponseIsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()
	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL}
	var out map[string]any
	err := client.do(context.Background(), http.MethodPost, server.URL+"/writes", map[string]string{"x": "y"}, &out)
	if err == nil || !strings.Contains(err.Error(), "write outcome may be uncertain") {
		t.Fatalf("expected successful-but-unparseable write to be marked uncertain, got %v", err)
	}
}
