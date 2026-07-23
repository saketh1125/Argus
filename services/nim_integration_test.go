package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/saketh1125/argus/config"
)

// TestNIMLiveConnect performs a real NIM API call and prints the full response
// for debugging. Run with: go test -v -run TestNIMLiveConnect ./services/
func TestNIMLiveConnect(t *testing.T) {
	cwd, _ := os.Getwd()
	t.Logf("CWD: %s", cwd)

	cfg := config.Load()

	t.Logf("NIM_API_KEY=%q (len=%d)", cfg.NIMAPIKey, len(cfg.NIMAPIKey))
	t.Logf("NIMBaseURL=%q", cfg.NIMBaseURL)
	t.Logf("NIMModel=%q", cfg.NIMModel)
	t.Logf("ForceMock=%v", cfg.ForceMock)

	if cfg.NIMAPIKey == "" {
		t.Skip("NIM_API_KEY not set, skipping live test")
	}

	t.Logf("Base URL: %s", cfg.NIMBaseURL)
	t.Logf("Model:    %s", cfg.NIMModel)

	limiter := NewRateLimiter(cfg.MaxRPM)
	client := newNIMClient(cfg.NIMBaseURL, cfg.NIMAPIKey, cfg.NIMModel, limiter)

	// Test 1: simple completion
	t.Run("simple", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		start := time.Now()
		resp, err := client.Complete(ctx, CompletionRequest{
			System: "You are a helpful assistant.",
			User:   "Say hello in one word.",
		})
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("FAILED after %v: %v", elapsed, err)
		}
		t.Logf("Response in %v: model=%s tokens=%d/%d text=%q",
			elapsed, resp.Model, resp.PromptTokens, resp.OutputTokens, resp.Text)
	})

	// Test 2: JSON mode with analyst-style prompt
	t.Run("json_analyst", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		start := time.Now()
		resp, err := client.Complete(ctx, CompletionRequest{
			System: "You are a security analyst. Return ONLY JSON matching {\"found\":bool,\"bug_type\":string,\"severity\":\"Critical|High|Medium|Low\",\"line_start\":int,\"line_end\":int,\"evidence\":string,\"explanation\":string,\"confidence\":0..1,\"function_name\":string}. If no bug, set found=false.",
			User: `Analyze this code for a single, concrete bug.
File: auth.py
Function: login
Code:
LINE_BASE: 10
10: def login(username):
11:     query = "SELECT * FROM users WHERE name = '" + username + "'"
12:     db.execute(query)`,
			JSONMode:  true,
			MaxTokens: 1024,
			Task:      "analyze",
		})
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("FAILED after %v: %v", elapsed, err)
		}
		t.Logf("Response in %v: model=%s tokens=%d/%d", elapsed, resp.Model, resp.PromptTokens, resp.OutputTokens)

		// Try parsing the JSON
		var parsed map[string]any
		if err := json.Unmarshal([]byte(resp.Text), &parsed); err != nil {
			// Try stripping fences
			s := resp.Text
			if i := indexOf(s, "```"); i >= 0 {
				s = s[i+3:]
				s = stripPrefix(s, "json")
				s = stripPrefix(s, "JSON")
				if j := lastIndexOf(s, "```"); j >= 0 {
					s = s[:j]
				}
			}
			start2 := indexOfByte(s, '{')
			end2 := lastIndexOfByte(s, '}')
			if start2 >= 0 && end2 > start2 {
				s = s[start2 : end2+1]
			}
			if err2 := json.Unmarshal([]byte(s), &parsed); err2 != nil {
				t.Fatalf("JSON parse failed even after cleanup: %v\nRaw text:\n%s", err2, resp.Text)
			}
		}
		pretty, _ := json.MarshalIndent(parsed, "", "  ")
		t.Logf("Parsed JSON:\n%s", string(pretty))
	})

	// Test 3: fixgen-style prompt
	t.Run("json_fixgen", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		start := time.Now()
		resp, err := client.Complete(ctx, CompletionRequest{
			System: "You are an expert software engineer. Rewrite the given function to fix the described bug. Return ONLY JSON matching {\"rewritten_function\":string,\"confidence\":0..1,\"rationale\":string}.",
			User: `Fix this SQL_INJECTION bug (severity Critical) in auth.py.
Evidence:
query = "SELECT * FROM users WHERE name = '" + username + "'"
Explanation:
User-controlled input is concatenated into a SQL statement, allowing SQL injection.

ORIGINAL FUNCTION:
def login(username):
    query = "SELECT * FROM users WHERE name = '" + username + "'"
    db.execute(query)`,
			Temperature: 0.2,
			JSONMode:    true,
			MaxTokens:   2048,
			Task:        "fixgen",
		})
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("FAILED after %v: %v", elapsed, err)
		}
		t.Logf("Response in %v: model=%s tokens=%d/%d", elapsed, resp.Model, resp.PromptTokens, resp.OutputTokens)

		var parsed map[string]any
		if err := json.Unmarshal([]byte(resp.Text), &parsed); err != nil {
			s := resp.Text
			if i := indexOf(s, "```"); i >= 0 {
				s = s[i+3:]
				s = stripPrefix(s, "json")
				if j := lastIndexOf(s, "```"); j >= 0 {
					s = s[:j]
				}
			}
			start2 := indexOfByte(s, '{')
			end2 := lastIndexOfByte(s, '}')
			if start2 >= 0 && end2 > start2 {
				s = s[start2 : end2+1]
			}
			if err2 := json.Unmarshal([]byte(s), &parsed); err2 != nil {
				t.Fatalf("JSON parse failed: %v\nRaw text:\n%s", err2, resp.Text)
			}
		}
		pretty, _ := json.MarshalIndent(parsed, "", "  ")
		t.Logf("Parsed JSON:\n%s", string(pretty))
	})

	// Test 4: rapid parallel calls (simulating analyst fan-out)
	t.Run("parallel_10", func(t *testing.T) {
		limiter2 := NewRateLimiter(cfg.MaxRPM)
		client2 := newNIMClient(cfg.NIMBaseURL, cfg.NIMAPIKey, cfg.NIMModel, limiter2)

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		type result struct {
			idx    int
			err    error
			text   string
			tokens int
		}
		results := make(chan result, 10)

		for i := 0; i < 10; i++ {
			go func(n int) {
				start := time.Now()
				resp, err := client2.Complete(ctx, CompletionRequest{
					System: "You are a security analyst. Return ONLY JSON matching {\"found\":bool,\"bug_type\":string,\"confidence\":0..1}.",
					User:   fmt.Sprintf("Analyze location %d for bugs. This is test %d.", n, n),
					Task:   "analyze",
				})
				elapsed := time.Since(start)
				text := ""
				tokens := 0
				if resp != nil {
					text = resp.Text
					tokens = resp.PromptTokens + resp.OutputTokens
				}
				results <- result{idx: n, err: err, text: text[:min(len(text), 120)], tokens: tokens}
				t.Logf("  goroutine %d: done in %v (tokens=%d)", n, elapsed, tokens)
			}(i)
		}

		succeeded, failed := 0, 0
		for i := 0; i < 10; i++ {
			r := <-results
			if r.err != nil {
				failed++
				t.Errorf("  goroutine %d FAILED: %v", r.idx, r.err)
			} else {
				succeeded++
			}
		}
		t.Logf("Parallel results: %d succeeded, %d failed", succeeded, failed)
	})
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func indexOfByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func lastIndexOfByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func stripPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
