package tools

// github.go provides two GitHubClient adapters: a real REST client built on
// net/http (no SDK), and an in-memory mock that records calls and
// returns deterministic artifacts so the pipeline runs end-to-end offline.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// apiBase is the GitHub REST API root.
const apiBase = "https://api.github.com"

const maxGitHubResponseBytes = 8 << 20
const maxLocalCopyBytes int64 = 256 << 20
const maxLocalCopyEntries = 100_000
const maxArchiveCompressedBytes int64 = 128 << 20
const maxArchiveExpandedBytes int64 = 384 << 20
const maxArchiveFileBytes int64 = 256 << 20
const maxArchiveEntries = 100_000

// sampleRepoPath is the deterministic buggy source the mock clones when no real
// local directory is provided.
const sampleRepoPath = "testdata/sample-repo"

// githubURLRe extracts owner/repo from a GitHub URL. The repo group is
// non-greedy and stops before an optional ".git" suffix so both
// "…/repo" and "…/repo.git" yield "repo".
var githubPathPartRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// parseGitHubURL is the shared, pure URL parser used by both adapters.
func parseGitHubURL(rawURL string) (owner, repo string, err error) {
	value := strings.TrimSpace(rawURL)
	var path string
	if strings.HasPrefix(value, "git@github.com:") {
		path = strings.TrimPrefix(value, "git@github.com:")
	} else {
		u, parseErr := url.Parse(value)
		if parseErr != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") || u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", "", fmt.Errorf("parse url: %q must be a canonical HTTPS GitHub repository URL", rawURL)
		}
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("parse url: %q is not a recognized GitHub URL", rawURL)
	}
	owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
	if !githubPathPartRe.MatchString(owner) || !githubPathPartRe.MatchString(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", "", fmt.Errorf("parse url: %q contains an invalid repository path", rawURL)
	}
	return owner, repo, nil
}

// copyDir recursively copies a regular directory tree at src into dst. It
// rejects symlinks and special files so local repository copies cannot read
// content outside the selected tree.
func copyDir(src, dst string) error {
	return copyDirWithBudget(src, dst, maxLocalCopyBytes, maxLocalCopyEntries)
}

func copyDirWithBudget(src, dst string, maxBytes int64, maxEntries int) error {
	if maxBytes < 0 || maxEntries < 0 {
		return fmt.Errorf("copy dir: invalid copy budget")
	}
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("copy dir: stat %s: %w", src, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("copy dir: %s is not a directory", src)
	}
	root, err := os.OpenRoot(src)
	if err != nil {
		return fmt.Errorf("copy dir: open source root %s: %w", src, err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("copy dir: source root changed while opening")
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("copy dir: mkdir %s: %w", dst, err)
	}
	budget := copyBudget{remainingBytes: maxBytes, remainingEntries: maxEntries}
	return copyDirContents(root, dst, &budget)
}

type copyBudget struct {
	remainingBytes   int64
	remainingEntries int
}

func copyDirContents(root *os.Root, dst string, budget *copyBudget) error {
	dir, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("copy dir: open source directory: %w", err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("copy dir: read source directory: %w", err)
	}
	for _, e := range entries {
		if budget.remainingEntries <= 0 {
			return fmt.Errorf("copy dir: entry count exceeds configured limit")
		}
		budget.remainingEntries--
		name := e.Name()
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return fmt.Errorf("copy dir: refusing unsafe entry name %q", name)
		}
		d := filepath.Join(dst, name)
		info, err := root.Lstat(name)
		if err != nil {
			return fmt.Errorf("copy dir: inspect %s: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("copy dir: refusing symlink %s", name)
		}
		if info.IsDir() {
			child, err := openCopyDirectory(root, name, info)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(d, 0o755); err != nil {
				_ = child.Close()
				return fmt.Errorf("copy dir: mkdir %s: %w", d, err)
			}
			if err := copyDirContents(child, d, budget); err != nil {
				_ = child.Close()
				return err
			}
			if err := child.Close(); err != nil {
				return fmt.Errorf("copy dir: close source directory %s: %w", name, err)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("copy dir: refusing non-regular file %s", name)
		}
		if info.Size() > budget.remainingBytes {
			return fmt.Errorf("copy dir: total file bytes exceed %d-byte limit", maxLocalCopyBytes)
		}
		copied, err := copyFileBounded(root, name, d, info, budget.remainingBytes)
		if err != nil {
			return err
		}
		budget.remainingBytes -= copied
	}
	return nil
}

// openCopyDirectory pins the inspected directory and verifies it did not
// change into a symlink or a different directory before it was opened.
func openCopyDirectory(root *os.Root, name string, expected os.FileInfo) (*os.Root, error) {
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("copy dir: open source directory %s: %w", name, err)
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(expected, opened) {
		_ = child.Close()
		return nil, fmt.Errorf("copy dir: source directory %s changed while opening", name)
	}
	return child, nil
}

// copyFileBounded copies a regular file only if it is still the same file
// inspected by its caller, and never reads more than limit+1 bytes.
func copyFileBounded(root *os.Root, src, dst string, expected os.FileInfo, limit int64) (int64, error) {
	in, err := root.Open(src)
	if err != nil {
		return 0, fmt.Errorf("copy file: open %s: %w", src, err)
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil {
		return 0, fmt.Errorf("copy file: inspect opened %s: %w", src, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		return 0, fmt.Errorf("copy file: %s changed or is not a regular file", src)
	}
	if opened.Size() > limit {
		return 0, fmt.Errorf("copy file: %s exceeds remaining %d-byte copy budget", src, limit)
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("copy file: create %s: %w", dst, err)
	}
	defer out.Close()
	copied, err := io.Copy(out, io.LimitReader(in, limit))
	if err != nil {
		return copied, fmt.Errorf("copy file: %s -> %s: %w", src, dst, err)
	}
	if copied == limit {
		var extra [1]byte
		n, readErr := in.Read(extra[:])
		if n > 0 {
			return copied, fmt.Errorf("copy file: %s grew beyond remaining %d-byte copy budget", src, limit)
		}
		if readErr != nil && readErr != io.EOF {
			return copied, fmt.Errorf("copy file: check copy limit for %s: %w", src, readErr)
		}
	}
	return copied, nil
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
	token         string
	http          *http.Client
	baseURL       string
	writesAllowed bool // set only by package tests until a human approval flow exists
}

// newGitHubREST builds a real client bound to the given token.
func newGitHubREST(token string) *githubREST {
	return &githubREST{token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

func (g *githubREST) apiRoot() string {
	if g.baseURL != "" {
		return strings.TrimRight(g.baseURL, "/")
	}
	return apiBase
}

func (g *githubREST) Name() string { return "github" }
func (g *githubREST) Live() bool   { return true }

func (g *githubREST) requireWriteAuthorization() error {
	if !g.writesAllowed {
		return fmt.Errorf("refusing live GitHub write: exact-patch human approval and fork publication are not implemented")
	}
	return nil
}

func (g *githubREST) ParseURL(url string) (string, string, error) {
	return parseGitHubURL(url)
}

// Clone copies a local test source or extracts a bounded archive pinned to an
// immutable GitHub commit. It does not execute Git on the host.
func (g *githubREST) Clone(ctx context.Context, repoURL, dest string) (CloneResult, error) {
	if isLocalDir(repoURL) {
		if err := copyDir(repoURL, dest); err != nil {
			return CloneResult{}, fmt.Errorf("clone: copy local repo: %w", err)
		}
		return CloneResult{Path: dest}, nil
	}
	owner, repo, err := parseGitHubURL(repoURL)
	if err != nil {
		return CloneResult{}, fmt.Errorf("clone: refusing non-GitHub remote: %w", err)
	}
	cloneCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var repository struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.do(cloneCtx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s", g.apiRoot(), owner, repo), nil, &repository); err != nil {
		return CloneResult{}, fmt.Errorf("clone: read repository metadata: %w", err)
	}
	if repository.DefaultBranch == "" || strings.ContainsAny(repository.DefaultBranch, "\\\x00") {
		return CloneResult{}, fmt.Errorf("clone: GitHub returned an invalid default branch")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	commitURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s", g.apiRoot(), owner, repo, url.PathEscape(repository.DefaultBranch))
	if err := g.do(cloneCtx, http.MethodGet, commitURL, nil, &commit); err != nil {
		return CloneResult{}, fmt.Errorf("clone: resolve default branch commit: %w", err)
	}
	if !regexp.MustCompile(`^[0-9a-fA-F]{40}$`).MatchString(commit.SHA) {
		return CloneResult{}, fmt.Errorf("clone: GitHub returned an invalid commit SHA")
	}
	if err := g.downloadArchive(cloneCtx, owner, repo, strings.ToLower(commit.SHA), dest); err != nil {
		return CloneResult{}, err
	}
	return CloneResult{Path: dest, SnapshotCommitSHA: strings.ToLower(commit.SHA)}, nil
}

func (g *githubREST) downloadArchive(ctx context.Context, owner, repo, sha, dest string) error {
	archiveURL := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", g.apiRoot(), owner, repo, sha)
	client := *g.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return fmt.Errorf("clone: build archive request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("clone: request archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently && resp.StatusCode != http.StatusTemporaryRedirect {
		return fmt.Errorf("clone: archive endpoint returned HTTP %d", resp.StatusCode)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || location.Scheme != "https" || !strings.EqualFold(location.Hostname(), "codeload.github.com") || location.Port() != "" || location.User != nil {
		return fmt.Errorf("clone: archive endpoint returned an untrusted redirect")
	}
	// Never forward the GitHub token to codeload. Enforce both wire bytes and
	// decompressed stream bytes while extracting the untrusted tarball.
	archiveReq, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return fmt.Errorf("clone: build download request: %w", err)
	}
	archiveReq.Header.Set("Accept-Encoding", "identity")
	archiveResp, err := client.Do(archiveReq)
	if err != nil {
		return fmt.Errorf("clone: download archive: %w", err)
	}
	defer archiveResp.Body.Close()
	if archiveResp.StatusCode != http.StatusOK || archiveResp.Uncompressed || archiveResp.Header.Get("Content-Encoding") != "" {
		return fmt.Errorf("clone: archive download returned an invalid response")
	}
	if archiveResp.ContentLength > maxArchiveCompressedBytes {
		return fmt.Errorf("clone: compressed archive exceeds limit")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("clone: create destination: %w", err)
	}
	compressed := &limitCounterReader{r: archiveResp.Body, remaining: maxArchiveCompressedBytes}
	if err := extractBoundedTarGzip(compressed, dest); err != nil {
		return fmt.Errorf("clone: extract bounded archive: %w", err)
	}
	if compressed.exceeded {
		return fmt.Errorf("clone: compressed archive exceeds limit")
	}
	return nil
}

type limitCounterReader struct {
	r         io.Reader
	remaining int64
	exceeded  bool
}

func (r *limitCounterReader) Read(p []byte) (int, error) {
	if r.remaining < 0 {
		r.exceeded = true
		return 0, fmt.Errorf("input exceeds byte limit")
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.r.Read(p)
	if int64(n) > r.remaining {
		r.exceeded = true
		return n, fmt.Errorf("input exceeds byte limit")
	}
	r.remaining -= int64(n)
	return n, err
}

func extractBoundedTarGzip(input io.Reader, dest string) error {
	return extractTarGzipWithLimits(input, dest, maxArchiveExpandedBytes, maxArchiveFileBytes, maxArchiveEntries)
}

func extractTarGzipWithLimits(input io.Reader, dest string, maxExpanded, maxFileBytes int64, maxEntries int) error {
	gz, err := gzip.NewReader(input)
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	bounded := &limitCounterReader{r: gz, remaining: maxExpanded}
	tr := tar.NewReader(bounded)
	root := ""
	entries := 0
	var fileBytes int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}
		entries++
		if entries > maxEntries {
			return fmt.Errorf("archive entry count exceeds limit")
		}
		name := strings.TrimSuffix(h.Name, "/")
		clean := path.Clean(name)
		if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(name) || strings.ContainsAny(name, ":\\\x00") || clean != name || !filepath.IsLocal(filepath.FromSlash(clean)) {
			return fmt.Errorf("archive contains unsafe path")
		}
		parts := strings.SplitN(clean, "/", 2)
		if root == "" {
			root = parts[0]
		} else if parts[0] != root {
			return fmt.Errorf("archive contains multiple roots")
		}
		if len(parts) < 2 {
			continue
		}
		rel := filepath.FromSlash(parts[1])
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("archive contains unsafe path")
		}
		target := filepath.Join(dest, rel)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if h.Size < 0 || h.Size > maxFileBytes-fileBytes {
				return fmt.Errorf("archive file bytes exceed limit")
			}
			fileBytes += h.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(f, tr, h.Size)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if h.Mode&0o111 != 0 {
				if err := os.Chmod(target, 0o755); err != nil {
					return err
				}
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Skip links entirely; preprocessing indexes only regular files.
		default:
			return fmt.Errorf("archive contains unsupported entry type")
		}
	}
	if root == "" {
		return fmt.Errorf("archive contains no repository root")
	}
	// Consume through gzip EOF so checksum/trailing expansion errors are detected.
	if _, err := io.Copy(io.Discard, bounded); err != nil {
		return err
	}
	if bounded.exceeded {
		return fmt.Errorf("expanded archive exceeds limit")
	}
	return nil
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
	readOnly := method == http.MethodGet || method == http.MethodHead
	var lastErr error

	for attempt := 0; attempt < 4; attempt++ {
		// Retry only safe reads. A write may have reached GitHub even when its
		// response was lost, so automatic retries can duplicate side effects.
		if attempt > 0 {
			timer := time.NewTimer(delays[attempt-1])
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
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
			if !readOnly {
				return fmt.Errorf("request: %s %s: write outcome may be uncertain: %w", method, url, err)
			}
			return fmt.Errorf("request: %s %s: %w", method, url, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxGitHubResponseBytes+1))
		resp.Body.Close()
		if readErr != nil {
			if !readOnly {
				return fmt.Errorf("request: read %s %s: write outcome may be uncertain: %w", method, url, readErr)
			}
			return fmt.Errorf("request: read %s %s: %w", method, url, readErr)
		}
		if len(data) > maxGitHubResponseBytes {
			if !readOnly {
				return fmt.Errorf("request: %s %s: response exceeds %d-byte limit; write outcome may be uncertain", method, url, maxGitHubResponseBytes)
			}
			return fmt.Errorf("request: %s %s: response exceeds %d-byte limit", method, url, maxGitHubResponseBytes)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil && len(data) > 0 {
				if err := json.Unmarshal(data, out); err != nil {
					if !readOnly {
						return fmt.Errorf("request: decode %s %s: write outcome may be uncertain: %w", method, url, err)
					}
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
			if !readOnly {
				return fmt.Errorf("%w; write outcome may be uncertain and must be reconciled before retry", lastErr)
			}
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

func (g *githubREST) CreateBranch(ctx context.Context, owner, repo, base, name, expectedBaseSHA string) error {
	if err := g.requireWriteAuthorization(); err != nil {
		return err
	}
	if expectedBaseSHA == "" {
		return fmt.Errorf("create branch: analyzed base commit SHA is required")
	}
	// Resolve the base branch's tip SHA. The ref response nests it under
	// .object.sha.
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	refURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", g.apiRoot(), owner, repo, escapeGitHubPath(base))
	if err := g.do(ctx, http.MethodGet, refURL, nil, &ref); err != nil {
		return fmt.Errorf("create branch: resolve base %q: %w", base, err)
	}
	if ref.Object.SHA == "" || ref.Object.SHA != expectedBaseSHA {
		return fmt.Errorf("create branch: base %q changed since repository scan", base)
	}
	body := map[string]string{
		"ref": "refs/heads/" + name,
		"sha": expectedBaseSHA,
	}
	postURL := fmt.Sprintf("%s/repos/%s/%s/git/refs", apiBase, owner, repo)
	if err := g.do(ctx, http.MethodPost, postURL, body, nil); err != nil {
		return fmt.Errorf("create branch %q: %w", name, err)
	}
	return nil
}

func (g *githubREST) CommitFile(ctx context.Context, owner, repo string, spec CommitSpec) error {
	if err := g.requireWriteAuthorization(); err != nil {
		return err
	}
	if !filepath.IsLocal(spec.Path) || spec.Path == "." {
		return fmt.Errorf("commit file %q: path must be repository-relative", spec.Path)
	}
	if len(spec.ExpectedContentSHA256) != sha256.Size*2 || len(spec.ExpectedPatchedContentSHA256) != sha256.Size*2 || spec.Patch == "" {
		return fmt.Errorf("commit file %q: validated patch and source/result digests are required", spec.Path)
	}
	patchedSum := sha256.Sum256([]byte(spec.Content))
	if hex.EncodeToString(patchedSum[:]) != spec.ExpectedPatchedContentSHA256 {
		return fmt.Errorf("commit file %q: patched content digest does not match validated result", spec.Path)
	}
	contentsURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", g.apiRoot(), owner, repo, escapeGitHubPath(filepath.ToSlash(spec.Path)))

	// Look up the existing blob SHA (required by the API to update a file).
	var existing struct {
		SHA      string `json:"sha"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	getURL := contentsURL + "?ref=" + spec.Branch
	if err := g.do(ctx, http.MethodGet, getURL, nil, &existing); err != nil {
		return fmt.Errorf("commit file %q: read branch source: %w", spec.Path, err)
	}
	if existing.SHA == "" || existing.Encoding != "base64" {
		return fmt.Errorf("commit file %q: branch source is missing or not base64 encoded", spec.Path)
	}
	encodedContent := strings.NewReplacer("\n", "", "\r", "").Replace(strings.TrimSpace(existing.Content))
	remoteContent, err := base64.StdEncoding.DecodeString(encodedContent)
	if err != nil {
		return fmt.Errorf("commit file %q: decode branch source: %w", spec.Path, err)
	}
	remoteSum := sha256.Sum256(remoteContent)
	if hex.EncodeToString(remoteSum[:]) != spec.ExpectedContentSHA256 {
		return fmt.Errorf("commit file %q: branch source changed since validation; refusing stale patch", spec.Path)
	}

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

func escapeGitHubPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func (g *githubREST) CreatePR(ctx context.Context, owner, repo string, spec PRSpec) (int, string, error) {
	if err := g.requireWriteAuthorization(); err != nil {
		return 0, "", err
	}
	if spec.ExpectedBaseSHA == "" {
		return 0, "", fmt.Errorf("create PR: analyzed base commit SHA is required")
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	refURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", g.apiRoot(), owner, repo, escapeGitHubPath(spec.Base))
	if err := g.do(ctx, http.MethodGet, refURL, nil, &ref); err != nil {
		return 0, "", fmt.Errorf("create PR: verify base %q: %w", spec.Base, err)
	}
	if ref.Object.SHA != spec.ExpectedBaseSHA {
		return 0, "", fmt.Errorf("create PR: base %q changed during analysis; refusing stale PR", spec.Base)
	}
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
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", g.apiRoot(), owner, repo)
	if err := g.do(ctx, http.MethodPost, url, body, &out); err != nil {
		return 0, "", fmt.Errorf("create pr: %w", err)
	}
	return out.Number, out.HTMLURL, nil
}

func (g *githubREST) CreateIssue(ctx context.Context, owner, repo, title, body string) (int, string, error) {
	if err := g.requireWriteAuthorization(); err != nil {
		return 0, "", err
	}
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
func (m *mockGitHub) Clone(ctx context.Context, url, dest string) (CloneResult, error) {
	src := sampleRepoPath
	if isLocalDir(url) {
		src = url
	}
	if err := copyDir(src, dest); err != nil {
		return CloneResult{}, fmt.Errorf("mock clone: %w", err)
	}
	m.record(ghAction{Kind: "clone", Detail: src + " -> " + dest})
	return CloneResult{Path: dest}, nil
}

func (m *mockGitHub) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	return "main", nil
}

func (m *mockGitHub) CreateBranch(ctx context.Context, owner, repo, base, name, expectedBaseSHA string) error {
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
