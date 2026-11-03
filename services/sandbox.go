package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Unavailable adapter
// ---------------------------------------------------------------------------

// unavailableSandbox refuses validation when no isolated provider is configured.
// It never materializes files or executes repository supplied code on the host.
type unavailableSandbox struct {
	reason string
}

func newUnavailableSandbox(reason string) *unavailableSandbox {
	return &unavailableSandbox{reason: reason}
}

func (u *unavailableSandbox) Run(context.Context, SandboxSpec) (*SandboxResult, error) {
	return nil, fmt.Errorf("validation unavailable: %s", u.reason)
}

func (u *unavailableSandbox) Name() string { return "unavailable" }

func (u *unavailableSandbox) Live() bool { return false }

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
			ExitCode int    `json:"exit_code"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			TimedOut bool   `json:"timed_out"`
		}
		if err := e.doE2B(ctx, http.MethodPost,
			e.baseURL+"/sandboxes/"+sandboxID+"/commands", processBody, &procResp); err != nil {
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
