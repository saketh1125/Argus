package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/saketh/codesentinel/models"
)

// numbered reproduces tools.NumberedChunk's stable, documented output format so
// these tests don't depend on the tools package compiling (it is implemented in
// parallel). Format: a "LINE_BASE: N" header followed by "<lineno>: <code>".
func numbered(code string, base int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "LINE_BASE: %d\n", base)
	for i, ln := range strings.Split(code, "\n") {
		fmt.Fprintf(&b, "%d: %s\n", base+i, ln)
	}
	return b.String()
}

// TestMockAnalyzeSQLInjection feeds a numbered SQL-injection snippet through the
// mock LLM's TaskAnalyze path and asserts a well-formed AnalystResult.
func TestMockAnalyzeSQLInjection(t *testing.T) {
	code := "func lookup(name string) {\n" +
		"\tq := fmt.Sprintf(\"SELECT * FROM users WHERE name = '%s'\", name)\n" +
		"\tdb.Query(q)\n}"
	user := numbered(code, 40)

	m := newMockLLM()
	resp, err := m.Complete(context.Background(), CompletionRequest{
		Task: models.TaskAnalyze,
		User: user,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	var res models.AnalystResult
	if err := json.Unmarshal([]byte(resp.Text), &res); err != nil {
		t.Fatalf("response is not valid AnalystResult JSON: %v\n%s", err, resp.Text)
	}

	if !res.Found {
		t.Fatalf("expected Found=true, got false (resp=%s)", resp.Text)
	}
	if res.BugType != models.BugSQLInjection {
		t.Fatalf("expected bug_type %q, got %q", models.BugSQLInjection, res.BugType)
	}
	if res.Severity != string(models.SeverityCritical) {
		t.Fatalf("expected severity Critical, got %q", res.Severity)
	}
	if strings.TrimSpace(res.Evidence) == "" {
		t.Fatalf("evidence must be non-empty (anti-hallucination guard)")
	}
	if !strings.Contains(res.Evidence, "SELECT") {
		t.Fatalf("evidence should quote the offending line, got %q", res.Evidence)
	}
	// The SQL line is line 41 (LINE_BASE 40, second source line).
	if res.LineStart != 41 || res.LineEnd != 41 {
		t.Fatalf("expected line 41..41, got %d..%d", res.LineStart, res.LineEnd)
	}
}

// TestMockAnalyzeClean verifies a benign snippet yields Found=false.
func TestMockAnalyzeClean(t *testing.T) {
	code := "func add(a, c int) int {\n\treturn a + c\n}"
	user := numbered(code, 1)

	m := newMockLLM()
	resp, err := m.Complete(context.Background(), CompletionRequest{Task: models.TaskAnalyze, User: user})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	var res models.AnalystResult
	if err := json.Unmarshal([]byte(resp.Text), &res); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if res.Found {
		t.Fatalf("benign snippet should not report a bug, got %+v", res)
	}
}

// TestMockFixGenPrependsComment verifies the fix generator prepends exactly one
// comment line and otherwise preserves the original function.
func TestMockFixGenPrependsComment(t *testing.T) {
	orig := "func f() int { return 1 }"
	user := "ORIGINAL FUNCTION:\n" + orig

	m := newMockLLM()
	resp, err := m.Complete(context.Background(), CompletionRequest{Task: models.TaskFixGen, User: user})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	var res models.FixResult
	if err := json.Unmarshal([]byte(resp.Text), &res); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if !strings.HasPrefix(res.RewrittenFunction, "// Fixed by CodeSentinel\n") {
		t.Fatalf("rewrite missing leading comment: %q", res.RewrittenFunction)
	}
	if !strings.Contains(res.RewrittenFunction, orig) {
		t.Fatalf("rewrite should preserve original body: %q", res.RewrittenFunction)
	}
}

// TestMockNameAndLive checks adapter identity.
func TestMockNameAndLive(t *testing.T) {
	m := newMockLLM()
	if m.Name() != "mock-llm" {
		t.Fatalf("unexpected name %q", m.Name())
	}
	if m.Live() {
		t.Fatalf("mock should report Live()=false")
	}
}
