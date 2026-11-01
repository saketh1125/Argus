package agents

import (
	"context"
	"crypto/sha256"
	"fmt"
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
				{RewrittenFunction: rewrite, Diff: "d", Confidence: 0.9, Temperature: 0.2, BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(origFile)))},
			},
		},
	}

	v := NewValidator(passSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
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
	if got.Fix.Selected.PatchDigest != fmt.Sprintf("%x", sha256.Sum256([]byte(got.Fix.Selected.Diff))) || got.Fix.Selected.Diff == "d" {
		t.Fatal("expected digest of the full-file diff")
	}
	if got.Fix.Selected.PatchedFile == "" {
		t.Fatal("expected Selected.PatchedFile to be populated with the full patched file")
	}
	if !strings.Contains(got.Fix.Selected.PatchedFile, "return 2") {
		t.Errorf("patched file should contain the rewrite; got %q", got.Fix.Selected.PatchedFile)
	}
	if !strings.Contains(got.Fix.Selected.PatchedFile, "func g()") {
		t.Errorf("patched file must preserve sibling code (func g); got %q", got.Fix.Selected.PatchedFile)
	}
}

func TestPatchedContentFailsClosed(t *testing.T) {
	v := &Validator{}
	for _, tc := range []struct{ source, original, rewrite string }{
		{"", "old", "new"}, {"file old", "missing", "new"}, {"old old", "old", "new"}, {"old", "old", ""}, {"old", "old", "old"},
	} {
		if _, err := v.patchedContent(tc.source, tc.original, tc.rewrite); err == nil {
			t.Errorf("accepted invalid rewrite: %+v", tc)
		}
	}
}

func TestValidatorRejectsChangedSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte("package p\nfunc f() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: "stale"}}}}
	v := NewValidator(passSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, _ = v.Run(context.Background(), dir, []*models.Issue{iss})
	if iss.Validated || !iss.Demoted {
		t.Fatalf("stale patch accepted: %+v", iss)
	}
}

type failedSandbox struct{}

func (failedSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	return &services.SandboxResult{Success: true, ExitCode: 127, Stderr: "interpreter missing"}, nil
}
func (failedSandbox) Name() string { return "failed" }
func (failedSandbox) Live() bool   { return false }

func TestValidatorRejectsNonzeroExitEvenIfSuccessFlagSet(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	v := NewValidator(failedSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, _ = v.Run(context.Background(), dir, []*models.Issue{iss})
	if iss.Validated || !iss.Demoted {
		t.Fatalf("failed command accepted: %+v", iss)
	}
}

func TestSyntaxCommandsUseArgvAndRejectUnsupportedLanguage(t *testing.T) {
	path := "file; touch marker.py"
	cmds := agtSyntaxCmd("python", path)
	if len(cmds) != 1 || len(cmds[0]) != 4 || cmds[0][3] != path {
		t.Fatalf("unexpected argv: %#v", cmds)
	}
	if len(agtSyntaxCmd("unsupported", path)) != 0 {
		t.Fatal("unsupported language must not pass")
	}
}

type unavailableTestSandbox struct{}

func (unavailableTestSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	return nil, services.ErrSandboxUnavailable
}
func (unavailableTestSandbox) Name() string { return "unavailable" }
func (unavailableTestSandbox) Live() bool   { return false }

func TestValidatorDemotesWhenProviderUnavailable(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Validated: true, Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	v := NewValidator(unavailableTestSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted || !strings.Contains(iss.ValidationLog, "unavailable") {
		t.Fatalf("provider failure must invalidate validation: issue=%+v err=%v", iss, err)
	}
}

type mutationSandbox struct{ file string }

func (s mutationSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	if err := os.WriteFile(s.file, []byte("package p\nfunc f() { println(999) }\n"), 0600); err != nil {
		return nil, err
	}
	return &services.SandboxResult{Success: true}, nil
}
func (mutationSandbox) Name() string { return "mutation" }
func (mutationSandbox) Live() bool   { return false }

func TestValidatorRejectsSourceChangedDuringSandbox(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	file := filepath.Join(dir, "f.go")
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	v := NewValidator(mutationSandbox{file}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted {
		t.Fatalf("source mutation accepted: issue=%+v err=%v", iss, err)
	}
}
