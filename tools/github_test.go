package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func responseFor(req *http.Request, status int, header http.Header, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)), Request: req}
}

func makeTestTarGzip(t *testing.T, entries []struct {
	name string
	kind byte
	body string
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, entry := range entries {
		h := &tar.Header{Name: entry.name, Mode: 0o644, Typeflag: entry.kind, Size: int64(len(entry.body))}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if entry.body != "" {
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

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

func TestCopyDirRejectsDirectorySwappedToSymlinkAfterInspection(t *testing.T) {
	source := t.TempDir()
	original := filepath.Join(source, "sub")
	other := filepath.Join(source, "other")
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "secret.go"), []byte("package secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, filepath.Join(source, "moved")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("rename unavailable on this platform: %v", err)
		}
		t.Fatal(err)
	}
	if err := os.Symlink("other", original); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable on this platform: %v", err)
		}
		t.Fatal(err)
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	child, err := openCopyDirectory(root, "sub", expected)
	if err == nil {
		_ = child.Close()
		t.Fatal("copy directory open accepted a symlink substituted after inspection")
	}
}

func TestCopyDirRejectsFileSwappedToSymlinkAfterInspection(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "candidate.go"), []byte("package original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "secret.go"), []byte("package secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(filepath.Join(source, "candidate.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(source, "candidate.go"), filepath.Join(source, "moved.go")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("rename unavailable on this platform: %v", err)
		}
		t.Fatal(err)
	}
	if err := os.Symlink("secret.go", filepath.Join(source, "candidate.go")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable on this platform: %v", err)
		}
		t.Fatal(err)
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dest := filepath.Join(t.TempDir(), "candidate.go")
	if _, err := copyFileBounded(root, "candidate.go", dest, expected, 1024); err == nil {
		t.Fatal("copy file accepted a symlink substituted after inspection")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("copy created destination after source identity mismatch: stat error = %v", err)
	}
}

func TestCopyDirEnforcesAggregateByteAndEntryBudgets(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		source := t.TempDir()
		for name, data := range map[string]string{"a.txt": "123", "b.txt": "456"} {
			if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		dest := filepath.Join(t.TempDir(), "copy")
		if err := copyDirWithBudget(source, dest, 5, 10); err == nil || !strings.Contains(err.Error(), "byte") {
			t.Fatalf("copyDirWithBudget error = %v, want byte-budget error", err)
		}
		if data, err := os.ReadFile(filepath.Join(dest, "a.txt")); err != nil || string(data) != "123" {
			t.Fatalf("first file copy = %q, %v; want successful 3-byte copy", data, err)
		}
		if _, err := os.Stat(filepath.Join(dest, "b.txt")); !os.IsNotExist(err) {
			t.Fatalf("file beyond aggregate budget was copied: stat error = %v", err)
		}
	})

	t.Run("exact byte boundary", func(t *testing.T) {
		source := t.TempDir()
		if err := os.WriteFile(filepath.Join(source, "exact.txt"), []byte("123"), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), "copy")
		if err := copyDirWithBudget(source, dest, 3, 1); err != nil {
			t.Fatalf("exact-boundary copy failed: %v", err)
		}
	})

	t.Run("entries", func(t *testing.T) {
		source := t.TempDir()
		for _, name := range []string{"a.txt", "b.txt"} {
			if err := os.WriteFile(filepath.Join(source, name), []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := copyDirWithBudget(source, filepath.Join(t.TempDir(), "copy"), 100, 1); err == nil || !strings.Contains(err.Error(), "entry count") {
			t.Fatalf("copyDirWithBudget error = %v, want entry-count error", err)
		}
	})
}

func TestGitHubCloneRejectsRemoteHelperURLs(t *testing.T) {
	client := newGitHubREST("test-token")
	if _, err := client.Clone(context.Background(), "ext::sh -c 'touch marker'", filepath.Join(t.TempDir(), "checkout")); err == nil || !strings.Contains(err.Error(), "refusing non-GitHub remote") {
		t.Fatalf("clone must reject Git remote helper URLs, got %v", err)
	}
}

func TestGitHubClonePinsArchiveAndDoesNotForwardToken(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	archive := makeTestTarGzip(t, []struct {
		name string
		kind byte
		body string
	}{
		{name: "repo-root/", kind: tar.TypeDir},
		{name: "repo-root/src/main.go", kind: tar.TypeReg, body: "package main\n"},
	})
	var sawPinnedArchive, sawUnauthenticatedCDN bool
	client := &githubREST{token: "test-token", baseURL: "https://api.github.test"}
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "api.github.test" && req.URL.Path == "/repos/o/r":
			return responseFor(req, http.StatusOK, make(http.Header), []byte(`{"default_branch":"main"}`)), nil
		case req.URL.Host == "api.github.test" && req.URL.Path == "/repos/o/r/commits/main":
			return responseFor(req, http.StatusOK, make(http.Header), []byte(`{"sha":"`+sha+`"}`)), nil
		case req.URL.Host == "api.github.test" && req.URL.Path == "/repos/o/r/tarball/"+sha:
			sawPinnedArchive = true
			return responseFor(req, http.StatusFound, http.Header{"Location": []string{"https://codeload.github.com/o/r/legacy.tar.gz/" + sha}}, nil), nil
		case req.URL.Host == "codeload.github.com":
			sawUnauthenticatedCDN = req.Header.Get("Authorization") == ""
			return responseFor(req, http.StatusOK, make(http.Header), archive), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	})}
	dest := filepath.Join(t.TempDir(), "source")
	got, err := client.Clone(context.Background(), "https://github.com/o/r", dest)
	if err != nil {
		t.Fatalf("Clone error: %v", err)
	}
	if !sawPinnedArchive || !sawUnauthenticatedCDN {
		t.Fatalf("pinned archive=%v unauthenticated CDN=%v", sawPinnedArchive, sawUnauthenticatedCDN)
	}
	if got.Path != dest || got.SnapshotCommitSHA != sha {
		t.Fatalf("Clone result = %#v", got)
	}
	content, err := os.ReadFile(filepath.Join(dest, "src", "main.go"))
	if err != nil || string(content) != "package main\n" {
		t.Fatalf("extracted content = %q, %v", content, err)
	}
}

func TestExtractBoundedTarGzipRejectsTraversalAndSkipsLinks(t *testing.T) {
	for _, name := range []string{"root/../../outside.go", "root/C:/outside.go", "root\\outside.go"} {
		t.Run(name, func(t *testing.T) {
			archive := makeTestTarGzip(t, []struct {
				name string
				kind byte
				body string
			}{{name: name, kind: tar.TypeReg, body: "package p"}})
			if err := extractBoundedTarGzip(bytes.NewReader(archive), t.TempDir()); err == nil {
				t.Fatalf("unsafe path %q was accepted", name)
			}
		})
	}
	dest := t.TempDir()
	archive := makeTestTarGzip(t, []struct {
		name string
		kind byte
		body string
	}{
		{name: "root/link.go", kind: tar.TypeSymlink},
		{name: "root/safe.go", kind: tar.TypeReg, body: "package safe"},
	})
	if err := extractBoundedTarGzip(bytes.NewReader(archive), dest); err != nil {
		t.Fatalf("safe archive rejected: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link.go")); !os.IsNotExist(err) {
		t.Fatalf("archive link was extracted: %v", err)
	}
}

func TestExtractTarGzipEnforcesExpandedFileAndEntryBudgets(t *testing.T) {
	archive := makeTestTarGzip(t, []struct {
		name string
		kind byte
		body string
	}{
		{name: "root/a.go", kind: tar.TypeReg, body: "123456"},
		{name: "root/b.go", kind: tar.TypeReg, body: "abcdef"},
	})
	t.Run("expanded bytes", func(t *testing.T) {
		if err := extractTarGzipWithLimits(bytes.NewReader(archive), t.TempDir(), 16, 100, 10); err == nil {
			t.Fatalf("extract error = %v, want expanded-byte limit", err)
		}
	})
	t.Run("aggregate regular bytes", func(t *testing.T) {
		if err := extractTarGzipWithLimits(bytes.NewReader(archive), t.TempDir(), 10000, 10, 10); err == nil || !strings.Contains(err.Error(), "archive file bytes exceed limit") {
			t.Fatalf("extract error = %v, want regular-file byte limit", err)
		}
	})
	t.Run("entry count", func(t *testing.T) {
		if err := extractTarGzipWithLimits(bytes.NewReader(archive), t.TempDir(), 10000, 100, 1); err == nil || !strings.Contains(err.Error(), "entry count exceeds") {
			t.Fatalf("extract error = %v, want entry-count limit", err)
		}
	})
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
	if got.Path != dest {
		t.Errorf("Clone returned path %q, want %q", got.Path, dest)
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
