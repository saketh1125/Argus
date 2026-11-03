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
	"runtime"
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

func TestCopyDirRejectsSymlinkEscape(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("outside repository"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	link := filepath.Join(source, "linked-secret.txt")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable on this platform: %v", err)
		}
		t.Fatalf("symlink creation must be available for this path-safety test: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "copy")
	if err := copyDir(source, dest); err == nil || !strings.Contains(err.Error(), "refusing symlink") {
		t.Fatalf("copyDir must reject repository symlinks, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "linked-secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("copyDir exposed linked outside content: stat err=%v", err)
	}
}

func TestGitHubCloneRejectsRemoteHelperURLs(t *testing.T) {
	client := newGitHubREST("test-token")
	if _, err := client.Clone(context.Background(), "ext::sh -c 'touch marker'", filepath.Join(t.TempDir(), "checkout")); err == nil || !strings.Contains(err.Error(), "refusing non-GitHub remote") {
		t.Fatalf("clone must reject Git remote helper URLs, got %v", err)
	}
}

func TestGitCloneEnvironmentDisablesAmbientGitExecutionConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "filter.hostile.smudge")
	t.Setenv("GIT_CONFIG_VALUE_0", "touch marker")
	t.Setenv("GIT_DIR", "C:\\outside\\.git")
	t.Setenv("GIT_EXEC_PATH", "C:\\outside\\git-core")
	t.Setenv("GIT_SSH_COMMAND", "touch marker")

	got := make(map[string]string)
	for _, item := range gitCloneEnvironment() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			got[strings.ToUpper(key)] = value
		}
	}
	for _, blocked := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_DIR", "GIT_EXEC_PATH", "GIT_SSH_COMMAND"} {
		if _, ok := got[blocked]; ok {
			t.Errorf("ambient %s must not reach git clone", blocked)
		}
	}
	if got["GIT_CONFIG_NOSYSTEM"] != "1" || got["GIT_CONFIG_GLOBAL"] != os.DevNull || got["GIT_LFS_SKIP_SMUDGE"] != "1" || got["GIT_TERMINAL_PROMPT"] != "0" {
		t.Fatalf("git clone safety environment incomplete: %+v", got)
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

	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL, writesAllowed: true}
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

	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL, writesAllowed: true}
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
	client := &githubREST{token: "test", http: http.DefaultClient, baseURL: "http://127.0.0.1", writesAllowed: true}
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
	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL, writesAllowed: true}
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
	client := &githubREST{token: "test", http: server.Client(), baseURL: server.URL, writesAllowed: true}
	_, _, err := client.CreatePR(context.Background(), "o", "r", PRSpec{Head: "feature", Base: "main", ExpectedBaseSHA: "analyzed-base"})
	if err == nil || !strings.Contains(err.Error(), "changed during analysis") {
		t.Fatalf("expected moved base rejection, got %v", err)
	}
	if postCalled {
		t.Fatal("must not create a PR after the base moves")
	}
}

func TestProductionGitHubRESTClientBlocksAllWritesBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	client := newGitHubREST("test-token")
	client.baseURL = server.URL

	checks := []struct {
		name string
		err  error
	}{
		{"branch", client.CreateBranch(context.Background(), "o", "r", "main", "feature", "sha")},
		{"commit", client.CommitFile(context.Background(), "o", "r", CommitSpec{})},
	}
	_, _, prErr := client.CreatePR(context.Background(), "o", "r", PRSpec{})
	checks = append(checks, struct {
		name string
		err  error
	}{"pull request", prErr})
	_, _, issueErr := client.CreateIssue(context.Background(), "o", "r", "title", "body")
	checks = append(checks, struct {
		name string
		err  error
	}{"issue", issueErr})

	for _, check := range checks {
		if check.err == nil || !strings.Contains(check.err.Error(), "exact-patch human approval") {
			t.Errorf("%s write was not refused: %v", check.name, check.err)
		}
	}
	if requests != 0 {
		t.Fatalf("production client sent %d network requests while writes are disabled", requests)
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
