package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/tools"
)

// captureGitHub is a GitHubClient that records every CommitFile content and the
// last PR body, so tests can assert exactly what the PR agent commits and renders.
type captureGitHub struct {
	commits []tools.CommitSpec
	prSpecs []tools.PRSpec
	issues  int
}

func (c *captureGitHub) ParseURL(string) (string, string, error) { return "o", "r", nil }
func (c *captureGitHub) Clone(context.Context, string, string) (string, error) {
	return "", nil
}
func (c *captureGitHub) DefaultBranch(context.Context, string, string) (string, error) {
	return "main", nil
}
func (c *captureGitHub) CreateBranch(context.Context, string, string, string, string) error {
	return nil
}
func (c *captureGitHub) CommitFile(_ context.Context, _, _ string, spec tools.CommitSpec) error {
	c.commits = append(c.commits, spec)
	return nil
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

func TestPRAgentBlocksAllPublication(t *testing.T) {
	gh := &captureGitHub{}
	pr := NewPRAgent(gh, testConfig(), models.NoopReporter{}, nil, nil)
	issues := []*models.Issue{{Validated: true, Fix: &models.Fix{Selected: &models.FixCandidate{PatchedFile: "full file"}}}, {Validated: true, Fix: &models.Fix{Selected: &models.FixCandidate{RewrittenFunction: "partial"}}}}
	report, err := pr.Run(context.Background(), "o", "r", "main", issues, issues)
	if err != nil || len(gh.commits) != 0 || len(gh.prSpecs) != 0 || gh.issues != 0 || len(report.PRsRaised) != 0 {
		t.Fatalf("publication must be blocked: report=%+v err=%v github=%+v", report, err, gh)
	}
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
		Fix: &models.Fix{Selected: &models.FixCandidate{
			RewrittenFunction: "func Caller() {}", Diff: "d", PatchedFile: "func Caller() {}",
		}},
		Validated: true,
	}
	body := pr.prBody(iss, "d")
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
