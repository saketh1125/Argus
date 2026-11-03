package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/services"
	"github.com/saketh1125/argus/tools"
)

// passSandbox is a Sandbox that always reports success, so the validator reaches
// the "validated" path regardless of the language/commands.
type passSandbox struct{}

func (passSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	return &services.SandboxResult{Success: true, Logs: "ok"}, nil
}
func (passSandbox) Name() string { return "pass-sandbox" }
func (passSandbox) Live() bool   { return false }

var _ services.Sandbox = passSandbox{}

// TestValidatorCapturesPatchedFile proves the data flow behind the
// destructive-overwrite fix: when validation passes, the validator stores the
// FULL patched file content on the selected candidate so the PR agent can commit
// a single-function edit rather than replacing the whole file.
func TestValidatorCapturesPatchedFile(t *testing.T) {
	// Original file on disk contains TWO functions; only f is being fixed.
	origFile := "package main\n\nfunc f() int {\n\treturn 1\n}\n\nfunc g() {}\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(origFile), 0o644); err != nil {
		t.Fatal(err)
	}

	origFunc := "func f() int {\n\treturn 1\n}"
	rewrite := "func f() int {\n\treturn 2\n}"

	iss := &models.Issue{
		File: "f.go", FunctionName: "f", LineStart: 3, LineEnd: 5,
		BugType: models.BugLogic, Severity: models.SeverityCritical, Confidence: 0.95,
		Fix: &models.Fix{
			OriginalFunction: origFunc,
			Candidates: []models.FixCandidate{
				{RewrittenFunction: rewrite, Diff: "d", Confidence: 0.9, Temperature: 0.2},
			},
		},
	}

	v := NewValidator(passSandbox{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	out, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out[0]
	if !got.Validated {
		t.Fatal("expected Validated=true")
	}
	if got.Fix == nil || got.Fix.Selected == nil {
		t.Fatal("expected a selected candidate")
	}

	// PatchedFile must be the FULL file with f rewritten — i.e. it must still
	// contain the untouched sibling function g(), proving the whole file was
	// preserved and only f was edited.
	if got.Fix.Selected.PatchedFile == "" {
		t.Fatal("expected Selected.PatchedFile to be populated with the full patched file")
	}
	if !strings.Contains(got.Fix.Selected.PatchedFile, "return 2") {
		t.Errorf("patched file should contain the rewrite; got %q", got.Fix.Selected.PatchedFile)
	}
	if !strings.Contains(got.Fix.Selected.PatchedFile, "func g()") {
		t.Errorf("patched file must preserve sibling code (func g); got %q", got.Fix.Selected.PatchedFile)
	}
	if got.Fix.Selected.OriginalFileSHA256 == "" || got.Fix.Selected.PatchedFileSHA256 == "" {
		t.Fatal("validated patch must carry source and result hashes")
	}
	if got.Fix.Selected.Diff == "" || !strings.Contains(got.Fix.Selected.Diff, "return 1") || !strings.Contains(got.Fix.Selected.Diff, "return 2") {
		t.Fatalf("expected mechanically generated function diff, got %q", got.Fix.Selected.Diff)
	}
}

func TestValidatorFailsClosedWhenSourceFileMissing(t *testing.T) {
	sandbox := &recordSandbox{}
	iss := &models.Issue{
		File: "missing.go", FunctionName: "f",
		Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() {}"}}},
	}
	v := NewValidator(sandbox, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), t.TempDir(), []*models.Issue{iss})
	if err != nil {
		t.Fatal(err)
	}
	if iss.Validated || !iss.Demoted || len(sandbox.specs) != 0 {
		t.Fatalf("missing source must fail before sandbox validation: validated=%v demoted=%v sandbox_calls=%d", iss.Validated, iss.Demoted, len(sandbox.specs))
	}
}

func TestValidatorRejectsStaleOriginalFunctionBeforeSandbox(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte("package main\nfunc f() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sandbox := &recordSandbox{}
	iss := &models.Issue{
		File: "f.go", FunctionName: "f",
		Fix: &models.Fix{OriginalFunction: "func f() int { return 9 }", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() int { return 2 }"}}},
	}
	v := NewValidator(sandbox, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil {
		t.Fatal(err)
	}
	if iss.Validated || !iss.Demoted || len(sandbox.specs) != 0 {
		t.Fatalf("stale source must fail before sandbox: valid=%v demoted=%v calls=%d", iss.Validated, iss.Demoted, len(sandbox.specs))
	}
}

func TestValidatorDoesNotGenerateTestCommands(t *testing.T) {
	original := "package main\nfunc f() {}\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{
		File: "f.go", FunctionName: "f",
		Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() {}"}}},
	}
	sandbox := &recordSandbox{}
	v := NewValidator(sandbox, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil {
		t.Fatal(err)
	}
	if len(sandbox.specs) != 1 || len(sandbox.specs[0].Commands) != 1 {
		t.Fatalf("validator must provide only its deterministic syntax command, specs=%+v", sandbox.specs)
	}
}

type recordSandbox struct{ specs []services.SandboxSpec }

func (s *recordSandbox) Run(_ context.Context, spec services.SandboxSpec) (*services.SandboxResult, error) {
	s.specs = append(s.specs, spec)
	return &services.SandboxResult{Success: true}, nil
}
func (*recordSandbox) Name() string { return "record" }
func (*recordSandbox) Live() bool   { return false }

func TestSafeRepoFileAcceptsNativeWindowsPathAndRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("src", "nested", "main.go")
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := safeRepoFile(root, rel)
	if err != nil {
		t.Fatalf("native relative path %q rejected: %v", rel, err)
	}
	if got != target {
		t.Fatalf("safe path = %q, want %q", got, target)
	}
	if _, err := safeRepoFile(root, filepath.Join("..", "outside.go")); err == nil {
		t.Fatal("parent traversal path must be rejected")
	}
}
