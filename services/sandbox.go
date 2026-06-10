package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Working adapter: local executor
// ---------------------------------------------------------------------------

// localSandbox runs a one-shot spec on the host inside a throwaway temp dir. It
// is the working Sandbox in the absence of an E2B key: it writes the spec files,
// runs each command in order under a timeout, and reports combined output. It is
// marked Live()=false because it is not the isolated production sandbox.
//
// Real syntax checks (py_compile, node --check, go build) execute here. If an
// interpreter is missing on the host, the offending command is SKIPPED and noted
// in Logs rather than failing the run.
type localSandbox struct{}

// newLocalSandbox returns the local executor adapter.
func newLocalSandbox() *localSandbox { return &localSandbox{} }

// Run materializes spec.Files in a temp directory, runs spec.Commands in order
// under a spec.TimeoutSec-second deadline, and returns the combined result.
// Success is true when every command exited 0 (skipped commands count as
// success). The temp directory is always removed.
func (l *localSandbox) Run(ctx context.Context, spec SandboxSpec) (*SandboxResult, error) {
	tmp, err := os.MkdirTemp("", "codesentinel-sbx-*")
	if err != nil {
		return nil, fmt.Errorf("local-sandbox: temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	// Write files, creating parent directories as needed.
	for rel, content := range spec.Files {
		dst := filepath.Join(tmp, filepath.Clean(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("local-sandbox: mkdir for %s: %w", rel, err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("local-sandbox: write %s: %w", rel, err)
		}
	}

	timeout := time.Duration(spec.TimeoutSec) * time.Second
	if spec.TimeoutSec <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := &SandboxResult{Success: true, ExitCode: 0}
	var stdout, stderr, logs bytes.Buffer

	for _, cmdline := range spec.Commands {
		fields := strings.Fields(cmdline)
		if len(fields) == 0 {
			continue
		}
		// Probe the interpreter/binary; skip (don't fail) if missing.
		if _, lookErr := exec.LookPath(fields[0]); lookErr != nil {
			fmt.Fprintf(&logs, "SKIP (%s not found): %s\n", fields[0], cmdline)
			continue
		}

		// Execute through a shell so quoting/redirection in the command string
		// behaves as written (the probe above already confirmed fields[0] exists).
		cmd := exec.CommandContext(runCtx, "sh", "-c", cmdline)
		cmd.Dir = tmp
		var cout, cerr bytes.Buffer
		cmd.Stdout = &cout
		cmd.Stderr = &cerr
		runErr := cmd.Run()

		stdout.WriteString(cout.String())
		stderr.WriteString(cerr.String())
		fmt.Fprintf(&logs, "RUN: %s\n", cmdline)

		if runErr != nil {
			res.Success = false
			if ee := (&exec.ExitError{}); errors.As(runErr, &ee) {
				res.ExitCode = ee.ExitCode()
			} else {
				res.ExitCode = 1
			}
			fmt.Fprintf(&logs, "FAILED (exit %d): %v\n", res.ExitCode, runErr)
			break // first failure stops the chain
		}
		fmt.Fprintf(&logs, "OK: %s\n", cmdline)
	}

	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	res.Logs = logs.String()
	return res, nil
}

// Name identifies the adapter.
func (l *localSandbox) Name() string { return "local-sandbox" }

// Live reports that this is NOT the isolated production sandbox.
func (l *localSandbox) Live() bool { return false }

// ---------------------------------------------------------------------------
// Real adapter: E2B (selected only when a key is present)
// ---------------------------------------------------------------------------

// e2bSandbox is the live, isolated Sandbox. It is only selected by the factory
// when cfg.E2BAPIKey is set. It drives the E2B REST API to create a microVM
// sandbox, upload files, run commands, and tear down the sandbox.
type e2bSandbox struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// newE2BSandbox builds the E2B adapter from an API key.
func newE2BSandbox(apiKey string) *e2bSandbox {
	return &e2bSandbox{
		apiKey:  apiKey,
		baseURL: "https://api.e2b.dev",
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

// doE2B is a helper that marshals body to JSON, performs an authenticated HTTP
// request against the E2B REST API, and (optionally) unmarshals the response
// into out. Non-2xx responses are returned as a formatted error containing the
// HTTP status code and up to 512 bytes of the response body.
func (e *e2bSandbox) doE2B(ctx context.Context, method, url string, body, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("e2b: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("e2b: build request: %w", err)
	}
	req.Header.Set("X-API-Key", e.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("e2b: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(respBytes)
		if len(snippet) > 512 {
			snippet = snippet[:512]
		}
		return fmt.Errorf("e2b: %s %s: status %d: %s", method, url, resp.StatusCode, snippet)
	}

	if out != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("e2b: unmarshal response: %w", err)
		}
	}
	return nil
}

// Run creates an E2B sandbox, uploads spec.Files, runs spec.Commands in order,
// tears down the sandbox (always), and returns the combined result.
func (e *e2bSandbox) Run(ctx context.Context, spec SandboxSpec) (*SandboxResult, error) {
	timeout := spec.TimeoutSec
	if timeout <= 0 {
		timeout = 30
	}

	// 1. Create sandbox.
	createBody := map[string]interface{}{
		"templateID": "base",
		"timeout":    timeout,
	}
	var createResp struct {
		SandboxID string `json:"sandboxID"`
	}
	if err := e.doE2B(ctx, http.MethodPost, e.baseURL+"/sandboxes", createBody, &createResp); err != nil {
		return nil, fmt.Errorf("e2b: create sandbox: %w", err)
	}
	sandboxID := createResp.SandboxID

	// Always destroy the sandbox when done; errors are silently discarded.
	defer func() {
		_ = e.doE2B(context.Background(), http.MethodDelete,
			e.baseURL+"/sandboxes/"+sandboxID, nil, nil)
	}()

	// 2. Upload files.
	for relPath, content := range spec.Files {
		fileBody := map[string]string{
			"path":    "/workspace/" + relPath,
			"content": content,
		}
		if err := e.doE2B(ctx, http.MethodPost,
			e.baseURL+"/sandboxes/"+sandboxID+"/files", fileBody, nil); err != nil {
			return nil, fmt.Errorf("e2b: upload file %s: %w", relPath, err)
		}
	}

	// 3. Run commands.
	res := &SandboxResult{Success: true}
	var stdout, stderr, logs strings.Builder

	for _, cmdline := range spec.Commands {
		processBody := map[string]interface{}{
			"cmd":     "sh",
			"args":    []string{"-c", cmdline},
			"timeout": timeout,
		}
		var procResp struct {
			ExitCode int    `json:"exitCode"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			TimedOut bool   `json:"timedOut"`
		}
		if err := e.doE2B(ctx, http.MethodPost,
			e.baseURL+"/sandboxes/"+sandboxID+"/process", processBody, &procResp); err != nil {
			return nil, fmt.Errorf("e2b: run command %q: %w", cmdline, err)
		}

		stdout.WriteString(procResp.Stdout)
		stderr.WriteString(procResp.Stderr)
		fmt.Fprintf(&logs, "RUN: %s\n", cmdline)

		if procResp.ExitCode != 0 || procResp.TimedOut {
			res.Success = false
			res.ExitCode = procResp.ExitCode
			fmt.Fprintf(&logs, "FAILED: %s\n", cmdline)
			break
		}
		fmt.Fprintf(&logs, "OK: %s\n", cmdline)
	}

	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	res.Logs = logs.String()
	return res, nil
}

// Name identifies the adapter.
func (e *e2bSandbox) Name() string { return "e2b" }

// Live reports that this is a real adapter.
func (e *e2bSandbox) Live() bool { return true }
