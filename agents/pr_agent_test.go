package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/tools"
)

// captureGitHub is a GitHubClient that records every CommitFile content and the
// last PR body, so tests can assert exactly what the PR agent commits and renders.
type captureGitHub struct {
	commits   []tools.CommitSpec
	prSpecs   []tools.PRSpec
	issues    int
	branches  int
	commitErr error
}

func (c *captureGitHub) ParseURL(string) (string, string, error) { return "o", "r", nil }
func (c *captureGitHub) Clone(context.Context, string, string) (string, error) {
	return "", nil
}
func (c *captureGitHub) DefaultBranch(context.Context, string, string) (string, error) {
	return "main", nil
}
func (c *captureGitHub) CreateBranch(context.Context, string, string, string, string, string) error {
	c.branches++
	return nil
}
func (c *captureGitHub) CommitFile(_ context.Context, _, _ string, spec tools.CommitSpec) error {
	c.commits = append(c.commits, spec)
	return c.commitErr
}
func (c *captureGitHub) CreatePR(_ context.Context, _, _ string, spec tools.PRSpec) (int, string, error) {
	c.prSpecs = append(c.prSpecs, spec)
	return len(c.prSpecs), "https://github.com/o/r/pr", nil
}
func (c *captureGitHub) CreateIssue(context.Context, string, string, string, string) (int, string, error) {
	c.issues++
	return c.issues, "https://github.com/o/r/issues", nil
}
func (c *captureGitHub) Name() string { return "capture" }
func (c *captureGitHub) Live() bool   { return false }

var _ tools.GitHubClient = (*captureGitHub)(nil)

// TestPRAgentCommitsFullPatchedFile is the regression test for the destructive
// overwrite bug: when the validator captured a full patched file, the PR agent
// MUST commit that (a single-function edit) and never just the rewritten
// function (which would replace the whole file via the GitHub Contents API).
func TestPRAgentCommitsValidatedMinimalPatch(t *testing.T) {
	gh := &captureGitHub{}
	pr := NewPRAgent(gh, testConfig(), models.NoopReporter{}, nil, nil)

	originalFile := "package main\n\nfunc f() int { return 1 }\nfunc g() {}\n"
	fullFile := "package main\n\nfunc f() int { return 2 }\nfunc g() {}\n"
	rewrite := "func f() int { return 2 }\n"
	iss := &models.Issue{
		File: "f.go", FunctionName: "f", LineStart: 3, LineEnd: 3,
		BugType: models.BugLogic, Severity: models.SeverityCritical, Confidence: 0.95,
		Evidence: "return 1", Explanation: "wrong return value",
		Fix: &models.Fix{
			OriginalFunction: "func f() int { return 1 }",
			Selected: &models.FixCandidate{
				RewrittenFunction: rewrite,
				PatchedFile:       fullFile,
				OriginalFile:      originalFile,
				Diff:              tools.UnifiedDiff("func f() int { return 1 }", rewrite, "f.go"),
			},
		},
		Validated: true,
	}
	setValidatedDigests(iss.Fix.Selected, originalFile)

	if _, err := pr.Run(context.Background(), "o", "r", "main", []*models.Issue{iss}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(gh.commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(gh.commits))
	}
	if gh.commits[0].Content != fullFile {
		t.Errorf("PR agent must commit the full patched file, got %q", gh.commits[0].Content)
	}
	if gh.commits[0].Content == rewrite {
		t.Error("PR agent committed only the rewritten function — this overwrites the whole file in live mode")
	}
	if gh.commits[0].Patch == "" || gh.commits[0].ExpectedContentSHA256 == "" || gh.commits[0].ExpectedPatchedContentSHA256 == "" {
		t.Fatal("commit must carry the validated patch and source/result digests")
	}
}

// A missing source snapshot must not fall back to committing a function as a
// whole file, and must fail before creating any branch or PR.
func TestPRAgentRejectsMissingPatchedFileBeforeBranchOrPR(t *testing.T) {
	gh := &captureGitHub{}
	pr := NewPRAgent(gh, testConfig(), models.NoopReporter{}, nil, nil)

	rewrite := "func f() int { return 2 }\n"
	iss := &models.Issue{
		File: "f.go", LineStart: 3,
		BugType: models.BugLogic, Severity: models.SeverityCritical, Confidence: 0.95,
		Evidence: "x", Explanation: "y",
		Fix:       &models.Fix{Selected: &models.FixCandidate{RewrittenFunction: rewrite, Diff: "d"}},
		Validated: true,
	}
	report, err := pr.Run(context.Background(), "o", "r", "main", []*models.Issue{iss}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gh.branches != 0 || len(gh.commits) != 0 || len(gh.prSpecs) != 0 || len(report.PRsRaised) != 0 {
		t.Fatalf("unsafe candidate caused side effects: branches=%d commits=%d PRs=%d", gh.branches, len(gh.commits), len(gh.prSpecs))
	}
}

func TestPRAgentFailedValidationCannotCreateBranchOrPR(t *testing.T) {
	gh := &captureGitHub{}
	pr := NewPRAgent(gh, testConfig(), models.NoopReporter{}, nil, nil)
	iss := validPRIssue()
	iss.Validated = false
	iss.Demoted = true
	report, err := pr.Run(context.Background(), "o", "r", "main", []*models.Issue{iss}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gh.branches != 0 || len(gh.commits) != 0 || len(gh.prSpecs) != 0 || len(report.PRsRaised) != 0 {
		t.Fatalf("failed validation caused PR side effects: branches=%d commits=%d PRs=%d", gh.branches, len(gh.commits), len(gh.prSpecs))
	}
}

func TestValidatedCommitSpecRejectsTamperedContent(t *testing.T) {
	iss := validPRIssue()
	iss.Fix.Selected.PatchedFile += "// later mutation\n"
	if _, err := validatedCommitSpec(iss); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("expected mutated result to fail provenance replay, got %v", err)
	}
}

func TestValidatedCommitSpecRejectsUnexpectedSiblingEditsEvenWithFreshDigest(t *testing.T) {
	iss := validPRIssue()
	iss.Fix.Selected.PatchedFile = strings.Replace(iss.Fix.Selected.PatchedFile, "func g() {}", "func g() { changed() }", 1)
	setValidatedDigests(iss.Fix.Selected, iss.Fix.Selected.OriginalFile)
	if _, err := validatedCommitSpec(iss); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("expected unrelated change to fail provenance replay, got %v", err)
	}
}

func TestPRAgentCommitConflictCannotPublishPR(t *testing.T) {
	gh := &captureGitHub{commitErr: errors.New("branch source changed since validation")}
	pr := NewPRAgent(gh, testConfig(), models.NoopReporter{}, nil, nil)
	if _, err := pr.Run(context.Background(), "o", "r", "main", []*models.Issue{validPRIssue()}, nil); err != nil {
		t.Fatal(err)
	}
	if gh.branches != 1 || len(gh.commits) != 1 || len(gh.prSpecs) != 0 {
		t.Fatalf("commit conflict must stop before PR publication: branches=%d commits=%d PRs=%d", gh.branches, len(gh.commits), len(gh.prSpecs))
	}
}

func validPRIssue() *models.Issue {
	iss := &models.Issue{
		File: "f.go", FunctionName: "f", LineStart: 3,
		BugType: models.BugLogic, Severity: models.SeverityHigh, Confidence: .95,
		Fix: &models.Fix{OriginalFunction: "func f() int { return 1 }", Selected: &models.FixCandidate{
			RewrittenFunction: "func f() int { return 2 }\n",
			PatchedFile:       "package main\n\nfunc f() int { return 2 }\nfunc g() {}\n",
			OriginalFile:      "package main\n\nfunc f() int { return 1 }\nfunc g() {}\n",
		}},
		Validated: true,
	}
	iss.Fix.Selected.Diff = tools.UnifiedDiff(iss.Fix.OriginalFunction, iss.Fix.Selected.RewrittenFunction, iss.File)
	setValidatedDigests(iss.Fix.Selected, iss.Fix.Selected.OriginalFile)
	return iss
}

func setValidatedDigests(candidate *models.FixCandidate, original string) {
	candidate.OriginalFile = original
	originalHash := sha256.Sum256([]byte(original))
	patchedHash := sha256.Sum256([]byte(candidate.PatchedFile))
	candidate.OriginalFileSHA256 = hex.EncodeToString(originalHash[:])
	candidate.PatchedFileSHA256 = hex.EncodeToString(patchedHash[:])
}

// TestPRBodyIncludesCallGraphContext verifies the §11 "Call Graph Context"
// section is rendered when an index is wired and the function has neighbors.
func TestPRBodyIncludesCallGraphContext(t *testing.T) {
	idx := models.NewRepoIndex()
	idx.AddFile(&models.ParsedFile{
		Path: "caller.go", Language: "go",
		Signatures: []models.Signature{{Name: "Caller", Kind: "function", File: "caller.go", LineStart: 1, LineEnd: 5}},
		Calls:      []models.CallEdge{{Caller: "Caller", Callee: "Helper", File: "caller.go", Line: 3}},
	})
	idx.AddFile(&models.ParsedFile{
		Path: "helper.go", Language: "go",
		Signatures: []models.Signature{{Name: "Helper", Kind: "function", File: "helper.go", LineStart: 1, LineEnd: 4}},
		Calls:      []models.CallEdge{{Caller: "Boss", Callee: "Caller", File: "helper.go", Line: 2}},
	})
	idx.BuildCallGraph()

	gh := &captureGitHub{}
	pr := NewPRAgent(gh, testConfig(), models.NoopReporter{}, nil, idx)
	iss := &models.Issue{
		File: "caller.go", FunctionName: "Caller", LineStart: 1,
		BugType: models.BugLogic, Severity: models.SeverityCritical, Confidence: 0.95,
		Evidence: "x", Explanation: "y",
		Fix: &models.Fix{OriginalFunction: "func Caller() { return }", Selected: &models.FixCandidate{
			RewrittenFunction: "func Caller() {}\n", PatchedFile: "package main\nfunc Caller() {}\n",
			OriginalFile: "package main\nfunc Caller() { return }\n",
		}},
		Validated: true,
	}
	iss.Fix.Selected.Diff = tools.UnifiedDiff(iss.Fix.OriginalFunction, iss.Fix.Selected.RewrittenFunction, iss.File)
	setValidatedDigests(iss.Fix.Selected, iss.Fix.Selected.OriginalFile)
	if _, err := pr.Run(context.Background(), "o", "r", "main", []*models.Issue{iss}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(gh.prSpecs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(gh.prSpecs))
	}
	body := gh.prSpecs[0].Body
	for _, want := range []string{"Call Graph Context", "Helper", "Boss"} {
		if !strings.Contains(body, want) {
			t.Errorf("PR body missing %q\n--- body ---\n%s", want, body)
		}
	}
}

// TestSummaryBodyHasTwoTables verifies §12: separate Findings and Unvalidated
// High/Critical (sandbox failed) tables.
func TestSummaryBodyHasTwoTables(t *testing.T) {
	pr := NewPRAgent(&captureGitHub{}, &config.Config{}, models.NoopReporter{}, nil, nil)
	med := &models.Issue{
		File: "a.go", LineStart: 1, BugType: models.BugComplexity,
		Severity: models.SeverityMedium, Confidence: 0.6, Evidence: "var x = 0",
	}
	demoted := &models.Issue{
		File: "b.go", LineStart: 9, BugType: models.BugSQLInjection,
		Severity: models.SeverityHigh, Confidence: 0.9, Evidence: "e",
		Demoted: true, ValidationLog: "syntax check failed: unexpected token",
	}
	body := pr.summaryBody([]*models.Issue{med, demoted})

	for _, want := range []string{"## Findings", "## Unvalidated High/Critical (sandbox failed)"} {
		if !strings.Contains(body, want) {
			t.Errorf("summary body missing section %q\n--- body ---\n%s", want, body)
		}
	}
	// Findings table should list the Medium finding; unvalidated table the demoted one.
	findingsIdx := strings.Index(body, "## Findings")
	unvalidIdx := strings.Index(body, "## Unvalidated")
	if !strings.Contains(body[findingsIdx:unvalidIdx], "a.go") {
		t.Error("Findings table should include the Medium finding (a.go)")
	}
	if !strings.Contains(body[unvalidIdx:], "b.go") {
		t.Error("Unvalidated table should include the demoted High finding (b.go)")
	}
}
