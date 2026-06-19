package tools

// github.go provides two GitHubClient adapters: a real REST client built on
// net/http + os/exec git (no SDK), and an in-memory mock that records calls and
// returns deterministic artifacts so the pipeline runs end-to-end offline.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// apiBase is the GitHub REST API root.
const apiBase = "https://api.github.com"

// sampleRepoPath is the deterministic buggy source the mock clones when no real
// local directory is provided.
const sampleRepoPath = "testdata/sample-repo"

// githubURLRe extracts owner/repo from a GitHub URL. The repo group is
// non-greedy and stops before an optional ".git" suffix so both
// "…/repo" and "…/repo.git" yield "repo".
var githubURLRe = regexp.MustCompile(`github\.com[/:]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// parseGitHubURL is the shared, pure URL parser used by both adapters.
func parseGitHubURL(url string) (owner, repo string, err error) {
	m := githubURLRe.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return "", "", fmt.Errorf("parse url: %q is not a recognized GitHub URL", url)
	}
	return m[1], m[2], nil
}

// copyDir recursively copies the directory tree at src into dst, creating dst
// and any parents. Symlinks are followed as regular files.
func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("copy dir: stat %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("copy dir: %s is not a directory", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("copy dir: mkdir %s: %w", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("copy dir: read %s: %w", src, err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(s, d); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies a single regular file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("copy file: open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("copy file: create %s: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy file: %s -> %s: %w", src, dst, err)
	}
	return nil
}

// isLocalDir reports whether path names an existing local directory.
func isLocalDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ---------------------------------------------------------------------------
// Real REST client
// ---------------------------------------------------------------------------

// githubREST talks to the GitHub REST API over net/http and shells out to git
// for cloning. It is selected only when a token is configured and mock mode is
// off.
type githubREST struct {
	token string
	http  *http.Client
}

// newGitHubREST builds a real client bound to the given token.
func newGitHubREST(token string) *githubREST {
	return &githubREST{token: token, http: &http.Client{}}
}

func (g *githubREST) Name() string { return "github" }
func (g *githubREST) Live() bool   { return true }

func (g *githubREST) ParseURL(url string) (string, string, error) {
	return parseGitHubURL(url)
}

// Clone shallow-clones url into dest. When url is an existing local directory it
// is copied instead, which keeps tests and reruns hermetic.
func (g *githubREST) Clone(ctx context.Context, url, dest string) (string, error) {
	if isLocalDir(url) {
		if err := copyDir(url, dest); err != nil {
			return "", fmt.Errorf("clone: copy local repo: %w", err)
		}
		return dest, nil
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", url, dest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("clone: git clone %s: %w: %s", url, err, stderr.String())
	}
	return dest, nil
}

// do performs an authenticated request and decodes a 2xx JSON body into out
// (which may be nil to discard the body). It retries on 500/502/503/504 with
// exponential backoff (1s, 2s, 4s), for up to 4 total attempts.
func (g *githubREST) do(ctx context.Context, method, url string, body any, out any) error {
	// Marshal the body once so it can be rewound on each retry attempt.
	var buf []byte
	if body != nil {
		var err error
		buf, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("request: marshal body: %w", err)
		}
	}

	delays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	var lastErr error

	for attempt := 0; attempt < 4; attempt++ {
		// Sleep with context-cancellation check before each retry.
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			time.Sleep(delays[attempt-1])
		}

		// Build the request body reader, rewound for this attempt.
		var rdr io.Reader
		if buf != nil {
			rdr = bytes.NewReader(buf)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, rdr)
		if err != nil {
			return fmt.Errorf("request: build %s %s: %w", method, url, err)
		}
		req.Header.Set("Authorization", "Bearer "+g.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := g.http.Do(req)
		if err != nil {
			return fmt.Errorf("request: %s %s: %w", method, url, err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil && len(data) > 0 {
				if err := json.Unmarshal(data, out); err != nil {
					return fmt.Errorf("request: decode %s %s: %w", method, url, err)
				}
			}
			return nil
		}

		// Non-2xx: decide whether to retry or return immediately.
		switch resp.StatusCode {
		case http.StatusUnauthorized: // 401
			return fmt.Errorf("github: unauthorized: check GITHUB_TOKEN")
		case http.StatusUnprocessableEntity: // 422
			return fmt.Errorf("request: %s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(data)))
		case http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout: // 500/502/503/504
			lastErr = fmt.Errorf("request: %s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(data)))
			// Retry if we have attempts remaining.
			if attempt < 3 {
				continue
			}
			return lastErr
		default:
			return fmt.Errorf("request: %s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(data)))
		}
	}

	return lastErr
}

func (g *githubREST) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	var out struct {
		DefaultBranch string `json:"default_branch"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s", apiBase, owner, repo)
	if err := g.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return "", fmt.Errorf("default branch: %w", err)
	}
	return out.DefaultBranch, nil
}

func (g *githubREST) CreateBranch(ctx context.Context, owner, repo, base, name string) error {
	// Resolve the base branch's tip SHA. The ref response nests it under
	// .object.sha.
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	refURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", apiBase, owner, repo, base)
	if err := g.do(ctx, http.MethodGet, refURL, nil, &ref); err != nil {
		return fmt.Errorf("create branch: resolve base %q: %w", base, err)
	}
	body := map[string]string{
		"ref": "refs/heads/" + name,
		"sha": ref.Object.SHA,
	}
	postURL := fmt.Sprintf("%s/repos/%s/%s/git/refs", apiBase, owner, repo)
	if err := g.do(ctx, http.MethodPost, postURL, body, nil); err != nil {
		return fmt.Errorf("create branch %q: %w", name, err)
	}
	return nil
}

func (g *githubREST) CommitFile(ctx context.Context, owner, repo string, spec CommitSpec) error {
	contentsURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", apiBase, owner, repo, spec.Path)

	// Look up the existing blob SHA (required by the API to update a file).
	var existing struct {
		SHA string `json:"sha"`
	}
	getURL := contentsURL + "?ref=" + spec.Branch
	// A 404 here simply means the file is new; ignore the error in that case.
	_ = g.do(ctx, http.MethodGet, getURL, nil, &existing)

	body := map[string]any{
		"message": spec.Message,
		"content": base64.StdEncoding.EncodeToString([]byte(spec.Content)),
		"branch":  spec.Branch,
	}
	if existing.SHA != "" {
		body["sha"] = existing.SHA
	}
	if err := g.do(ctx, http.MethodPut, contentsURL, body, nil); err != nil {
		return fmt.Errorf("commit file %q: %w", spec.Path, err)
	}
	return nil
}

func (g *githubREST) CreatePR(ctx context.Context, owner, repo string, spec PRSpec) (int, string, error) {
	body := map[string]string{
		"title": spec.Title,
		"head":  spec.Head,
		"base":  spec.Base,
		"body":  spec.Body,
	}
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", apiBase, owner, repo)
	if err := g.do(ctx, http.MethodPost, url, body, &out); err != nil {
		return 0, "", fmt.Errorf("create pr: %w", err)
	}
	return out.Number, out.HTMLURL, nil
}

func (g *githubREST) CreateIssue(ctx context.Context, owner, repo, title, body string) (int, string, error) {
	payload := map[string]string{"title": title, "body": body}
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/issues", apiBase, owner, repo)
	if err := g.do(ctx, http.MethodPost, url, payload, &out); err != nil {
		return 0, "", fmt.Errorf("create issue: %w", err)
	}
	return out.Number, out.HTMLURL, nil
}

// ---------------------------------------------------------------------------
// Mock client
// ---------------------------------------------------------------------------

// ghAction is one recorded mock operation, exposed for debugging/tests.
type ghAction struct {
	Kind   string // "clone" | "branch" | "commit" | "pr" | "issue"
	Detail string
	Number int
	URL    string
}

// mockGitHub records calls in memory and returns deterministic artifacts so the
// offline demo runs without a token. It is safe for concurrent use because PR
// creation may happen from parallel goroutines.
type mockGitHub struct {
	mu      sync.Mutex
	actions []ghAction
	prSeq   int
	issSeq  int
}

// newMockGitHub builds an empty mock client.
func newMockGitHub() *mockGitHub { return &mockGitHub{} }

func (m *mockGitHub) Name() string { return "mock-github" }
func (m *mockGitHub) Live() bool   { return false }

// ParseURL uses the same regex as the real client but never fails: an
// unparseable URL falls back to demo-owner/demo-repo so the demo always runs.
func (m *mockGitHub) ParseURL(url string) (string, string, error) {
	owner, repo, err := parseGitHubURL(url)
	if err != nil {
		return "demo-owner", "demo-repo", nil
	}
	return owner, repo, nil
}

// Clone copies an existing local directory when url points at one; otherwise it
// copies the bundled sample repo so the pipeline always has real, deterministic
// buggy source to analyze.
func (m *mockGitHub) Clone(ctx context.Context, url, dest string) (string, error) {
	src := sampleRepoPath
	if isLocalDir(url) {
		src = url
	}
	if err := copyDir(src, dest); err != nil {
		return "", fmt.Errorf("mock clone: %w", err)
	}
	m.record(ghAction{Kind: "clone", Detail: src + " -> " + dest})
	return dest, nil
}

func (m *mockGitHub) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	return "main", nil
}

func (m *mockGitHub) CreateBranch(ctx context.Context, owner, repo, base, name string) error {
	m.record(ghAction{Kind: "branch", Detail: fmt.Sprintf("%s/%s: %s from %s", owner, repo, name, base)})
	return nil
}

func (m *mockGitHub) CommitFile(ctx context.Context, owner, repo string, spec CommitSpec) error {
	m.record(ghAction{Kind: "commit", Detail: fmt.Sprintf("%s/%s@%s: %s", owner, repo, spec.Branch, spec.Path)})
	return nil
}

func (m *mockGitHub) CreatePR(ctx context.Context, owner, repo string, spec PRSpec) (int, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prSeq++
	n := m.prSeq
	url := fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, n)
	m.actions = append(m.actions, ghAction{Kind: "pr", Detail: spec.Title, Number: n, URL: url})
	return n, url, nil
}

func (m *mockGitHub) CreateIssue(ctx context.Context, owner, repo, title, body string) (int, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.issSeq++
	n := m.issSeq
	url := fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, n)
	m.actions = append(m.actions, ghAction{Kind: "issue", Detail: title, Number: n, URL: url})
	return n, url, nil
}

// record appends an action under the mutex.
func (m *mockGitHub) record(a ghAction) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, a)
}

// Actions returns a snapshot of every recorded operation, for debugging/tests.
func (m *mockGitHub) Actions() []ghAction {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ghAction(nil), m.actions...)
}
