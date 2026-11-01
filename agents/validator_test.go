package agents

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/services"
	"github.com/saketh1125/argus/tools"
)

func validationGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Argus Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Argus Test", "GIT_COMMITTER_EMAIL=test@example.invalid"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fixture: %s: %v", out, err)
	}
}

func pinValidationIssue(t *testing.T, dir string, iss *models.Issue) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	validationGit(t, dir, "init", "-q")
	validationGit(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("tracked context\n"), 0600); err != nil {
		t.Fatal(err)
	}
	validationGit(t, dir, "add", "f.go", "extra.txt")
	validationGit(t, dir, "-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "fixture")
	commit, err := tools.ResolveGitCommit(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	iss.Repository = "github.com/owner/repo"
	iss.BaseCommit = commit
	for i := range iss.Fix.Candidates {
		iss.Fix.Candidates[i].Repository = "github.com/owner/repo"
		iss.Fix.Candidates[i].BaseCommit = commit
	}
}

// passSandbox is a Sandbox that always reports success, so the validator reaches
// the "validated" path regardless of the language/commands.
type passSandbox struct{}

func (passSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	return &services.SandboxResult{Success: true, Level: services.ValidationTargetedTests, RepositoryContext: true, Logs: "ok"}, nil
}
func (passSandbox) Name() string            { return "pass-sandbox" }
func (passSandbox) Live() bool              { return true }
func (passSandbox) IsolationVerified() bool { return true }

var _ services.Sandbox = passSandbox{}

type captureValidationSandbox struct{ spec services.SandboxSpec }

func (s *captureValidationSandbox) Run(_ context.Context, spec services.SandboxSpec) (*services.SandboxResult, error) {
	s.spec = spec
	return &services.SandboxResult{Success: true, Level: services.ValidationTargetedTests, RepositoryContext: true}, nil
}
func (*captureValidationSandbox) Name() string            { return "capture-validation" }
func (*captureValidationSandbox) Live() bool              { return true }
func (*captureValidationSandbox) IsolationVerified() bool { return true }

// TestValidatorCapturesPatchedFile checks whole-file preservation and rejects
// falsely claimed repository-context validation for single-file staging.
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

	pinValidationIssue(t, dir, iss)
	sandbox := &captureValidationSandbox{}
	v := NewValidator(sandbox, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	out, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out[0]
	if got.Validated || !got.Demoted || got.Fix.Selected != nil {
		t.Fatal("single-file staging must not be marked repository-context validated")
	}
	if sandbox.spec.TargetPath != "f.go" || sandbox.spec.OriginalHash != fmt.Sprintf("%x", sha256.Sum256([]byte(origFile))) || sandbox.spec.ResultHash != fmt.Sprintf("%x", sha256.Sum256([]byte(sandbox.spec.Files["f.go"]))) || sandbox.spec.PatchDigest == "" {
		t.Fatal("sandbox spec lost byte-bound patch identity")
	}
	if sandbox.spec.Repository != iss.Repository || sandbox.spec.BaseCommit != iss.BaseCommit {
		t.Fatal("sandbox spec lost pinned repository identity")
	}
	if sandbox.spec.Files["extra.txt"] != "tracked context\n" {
		t.Fatal("tracked repository context missing from staged files")
	}
	patched := sandbox.spec.Files["f.go"]
	if !strings.Contains(patched, "return 2") || !strings.Contains(patched, "func g()") {
		t.Fatalf("staged patch must preserve sibling code: %q", patched)
	}
	if sandbox.spec.RepositoryContext {
		t.Fatal("single-file spec claimed repository context")
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
	pinValidationIssue(t, dir, iss)
	v := NewValidator(passSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, _ = v.Run(context.Background(), dir, []*models.Issue{iss})
	if iss.Validated || !iss.Demoted {
		t.Fatalf("stale patch accepted: %+v", iss)
	}
}

type failedSandbox struct{}

func (failedSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	return &services.SandboxResult{Success: true, Level: services.ValidationTargetedTests, RepositoryContext: true, ExitCode: 127, Stderr: "interpreter missing"}, nil
}
func (failedSandbox) Name() string            { return "failed" }
func (failedSandbox) Live() bool              { return true }
func (failedSandbox) IsolationVerified() bool { return true }

func TestValidatorRejectsNonzeroExitEvenIfSuccessFlagSet(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
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
func (unavailableTestSandbox) Name() string            { return "unavailable" }
func (unavailableTestSandbox) Live() bool              { return true }
func (unavailableTestSandbox) IsolationVerified() bool { return true }

func TestValidatorDemotesWhenProviderUnavailable(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Validated: true, Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
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
	return &services.SandboxResult{Success: true, Level: services.ValidationTargetedTests, RepositoryContext: true}, nil
}
func (mutationSandbox) Name() string            { return "mutation" }
func (mutationSandbox) Live() bool              { return true }
func (mutationSandbox) IsolationVerified() bool { return true }

func TestValidatorRejectsSourceChangedDuringSandbox(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	file := filepath.Join(dir, "f.go")
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
	v := NewValidator(mutationSandbox{file}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted || !strings.Contains(iss.ValidationLog, "base file changed during validation") {
		t.Fatalf("source mutation accepted: issue=%+v err=%v", iss, err)
	}
}

func TestValidatorClearsStaleSelectedPatchAfterFailure(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	old := &models.FixCandidate{PatchedFile: "previously validated file", BaseHash: "old", ResultHash: "old", PatchDigest: "old"}
	iss := &models.Issue{File: "f.go", Validated: true, Fix: &models.Fix{OriginalFunction: "func f() {}", Selected: old, Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
	v := NewValidator(unavailableTestSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || iss.Fix.Selected != nil {
		t.Fatalf("stale selection survived failed validation: %+v, %v", iss, err)
	}
}

func TestValidatorRejectsStaleCommit(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
	validationGit(t, dir, "-c", "core.hooksPath=/dev/null", "commit", "-q", "--allow-empty", "-m", "next")
	v := NewValidator(passSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted {
		t.Fatalf("stale base accepted: %+v %v", iss, err)
	}
}

func TestValidatorRejectsWorktreeMatchingForgedHash(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.go")
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }"}}}}
	pinValidationIssue(t, dir, iss)
	changed := original + "// uncommitted alteration\n"
	if err := os.WriteFile(file, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	iss.Fix.Candidates[0].BaseHash = fmt.Sprintf("%x", sha256.Sum256([]byte(changed)))
	v := NewValidator(passSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted {
		t.Fatalf("uncommitted file accepted: %+v %v", iss, err)
	}
}

type commitMutationSandbox struct {
	t   *testing.T
	dir string
}

func (s commitMutationSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	validationGit(s.t, s.dir, "-c", "core.hooksPath=/dev/null", "commit", "-q", "--allow-empty", "-m", "mutated during validation")
	return &services.SandboxResult{Success: true, Level: services.ValidationTargetedTests, RepositoryContext: true}, nil
}
func (commitMutationSandbox) Name() string            { return "commit-mutation" }
func (commitMutationSandbox) Live() bool              { return true }
func (commitMutationSandbox) IsolationVerified() bool { return true }

func TestValidatorRejectsCommitChangedDuringSandbox(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
	v := NewValidator(commitMutationSandbox{t, dir}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted || !strings.Contains(iss.ValidationLog, "pinned source changed during validation") {
		t.Fatalf("changed commit accepted: %+v %v", iss, err)
	}
}

type originMutationSandbox struct {
	t   *testing.T
	dir string
}

func (s originMutationSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	validationGit(s.t, s.dir, "remote", "set-url", "origin", "https://github.com/attacker/other.git")
	return &services.SandboxResult{Success: true, Level: services.ValidationTargetedTests, RepositoryContext: true}, nil
}
func (originMutationSandbox) Name() string            { return "origin-mutation" }
func (originMutationSandbox) Live() bool              { return true }
func (originMutationSandbox) IsolationVerified() bool { return true }

func TestValidatorRejectsOriginChangedDuringSandbox(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
	v := NewValidator(originMutationSandbox{t, dir}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted || !strings.Contains(iss.ValidationLog, "pinned source changed during validation") {
		t.Fatalf("changed origin accepted: %+v %v", iss, err)
	}
}

func TestValidatorRejectsMissingRevision(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	v := NewValidator(passSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted {
		t.Fatalf("unproven source accepted: %+v %v", iss, err)
	}
}

func TestValidatorRejectsFindingFromDifferentRepository(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
	iss.Repository = "github.com/other/repo"
	v := NewValidator(passSandbox{}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted {
		t.Fatalf("cross-repository finding accepted: %+v %v", iss, err)
	}
}

type countingSandbox struct{ calls int }

func (s *countingSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	s.calls++
	return &services.SandboxResult{Success: true, Level: services.ValidationTargetedTests, RepositoryContext: true}, nil
}
func (*countingSandbox) Name() string            { return "counting" }
func (*countingSandbox) Live() bool              { return true }
func (*countingSandbox) IsolationVerified() bool { return true }

func TestValidatorRejectsUnreconstructablePatchBeforeSandbox(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
	sandbox := &countingSandbox{}
	v := NewValidator(sandbox, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted || sandbox.calls != 0 {
		t.Fatalf("unreconstructable patch accepted: %+v calls=%d err=%v", iss, sandbox.calls, err)
	}
}

type unverifiedSuccessSandbox struct{ calls int }

func (s *unverifiedSuccessSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	s.calls++
	return &services.SandboxResult{Success: true, Level: services.ValidationTargetedTests, RepositoryContext: true}, nil
}
func (*unverifiedSuccessSandbox) Name() string            { return "unverified" }
func (*unverifiedSuccessSandbox) Live() bool              { return true }
func (*unverifiedSuccessSandbox) IsolationVerified() bool { return false }

func TestValidatorRejectsUnverifiedSandbox(t *testing.T) {
	dir := t.TempDir()
	original := "package p\nfunc f() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
	pinValidationIssue(t, dir, iss)
	sandbox := &unverifiedSuccessSandbox{}
	v := NewValidator(sandbox, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
	_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
	if err != nil || iss.Validated || !iss.Demoted || sandbox.calls != 0 {
		t.Fatalf("unverified sandbox accepted: %+v calls=%d err=%v", iss, sandbox.calls, err)
	}
}

type levelSandbox struct {
	level             services.ValidationLevel
	repositoryContext bool
}

func (s levelSandbox) Run(context.Context, services.SandboxSpec) (*services.SandboxResult, error) {
	return &services.SandboxResult{Success: true, Level: s.level, RepositoryContext: s.repositoryContext}, nil
}
func (levelSandbox) Name() string            { return "level-fixture" }
func (levelSandbox) Live() bool              { return true }
func (levelSandbox) IsolationVerified() bool { return true }

func TestValidatorRejectsInsufficientValidationEvidence(t *testing.T) {
	for _, tc := range []struct {
		level services.ValidationLevel
		repo  bool
	}{
		{services.ValidationSyntax, true}, {services.ValidationTargetedTests, false}, {"", true}, {services.ValidationTargetedTests, true},
	} {
		dir := t.TempDir()
		original := "package p\nfunc f() {}\n"
		if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		iss := &models.Issue{File: "f.go", Fix: &models.Fix{OriginalFunction: "func f() {}", Candidates: []models.FixCandidate{{RewrittenFunction: "func f() { println(1) }", BaseHash: fmt.Sprintf("%x", sha256.Sum256([]byte(original)))}}}}
		pinValidationIssue(t, dir, iss)
		v := NewValidator(levelSandbox{tc.level, tc.repo}, &fakeLLM{}, tools.NewParser(testConfig()), testConfig(), models.NoopReporter{})
		_, err := v.Run(context.Background(), dir, []*models.Issue{iss})
		if err != nil || iss.Validated || !iss.Demoted {
			t.Fatalf("insufficient evidence accepted: %+v %v", iss, err)
		}
	}
}
