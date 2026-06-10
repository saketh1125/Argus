package agents

import (
	"testing"

	"github.com/saketh/codesentinel/config"
	"github.com/saketh/codesentinel/models"
)

func testConfig() *config.Config {
	return &config.Config{
		MaxFiles:         50,
		AnalystParallel:  4,
		FixCandidates:    3,
		ConfidenceGate:   0.80,
		LocalizeFallback: 0.50,
		SandboxTimeout:   60,
	}
}

func TestAggregatorDedup(t *testing.T) {
	cfg := testConfig()
	agg := NewAggregator(cfg, models.NoopReporter{})

	// Two issues with the same DedupKey (file:line:bugtype); higher confidence wins.
	a := &models.Issue{File: "a.go", LineStart: 10, BugType: models.BugLogic, Severity: models.SeverityCritical, Confidence: 0.85}
	b := &models.Issue{File: "a.go", LineStart: 10, BugType: models.BugLogic, Severity: models.SeverityCritical, Confidence: 0.95}
	c := &models.Issue{File: "b.go", LineStart: 5, BugType: models.BugNullDeref, Severity: models.SeverityHigh, Confidence: 0.90}

	forFix, summary := agg.Run(models.NewRepoIndex(), []*models.Issue{a, b, c})

	if len(forFix) != 2 {
		t.Fatalf("expected 2 deduped PR-eligible issues, got %d", len(forFix))
	}
	if len(summary) != 0 {
		t.Fatalf("expected 0 summary issues, got %d", len(summary))
	}
	// The a.go finding must be the higher-confidence representative.
	var found bool
	for _, iss := range forFix {
		if iss.File == "a.go" {
			found = true
			if iss.Confidence != 0.95 {
				t.Errorf("dedup kept wrong representative: confidence %.2f, want 0.95", iss.Confidence)
			}
		}
	}
	if !found {
		t.Fatal("a.go finding missing after dedup")
	}
}

func TestAggregatorGate(t *testing.T) {
	cfg := testConfig()
	agg := NewAggregator(cfg, models.NoopReporter{})

	// Critical@0.9 -> forFix (PR-eligible AND >= gate).
	// Medium@0.9   -> summary (high confidence but NOT PR-eligible).
	crit := &models.Issue{File: "x.go", LineStart: 1, BugType: models.BugSQLInjection, Severity: models.SeverityCritical, Confidence: 0.90}
	med := &models.Issue{File: "y.go", LineStart: 2, BugType: models.BugComplexity, Severity: models.SeverityMedium, Confidence: 0.90}
	// High but below gate -> summary.
	lowConf := &models.Issue{File: "z.go", LineStart: 3, BugType: models.BugOffByOne, Severity: models.SeverityHigh, Confidence: 0.60}

	forFix, summary := agg.Run(models.NewRepoIndex(), []*models.Issue{crit, med, lowConf})

	if len(forFix) != 1 || forFix[0].File != "x.go" {
		t.Fatalf("expected only Critical@0.9 in forFix, got %d: %+v", len(forFix), forFix)
	}
	if len(summary) != 2 {
		t.Fatalf("expected Medium@0.9 and High@0.6 in summary, got %d", len(summary))
	}
}

func TestAggregatorDependentFilesEnrichment(t *testing.T) {
	cfg := testConfig()
	agg := NewAggregator(cfg, models.NoopReporter{})

	// Build an index with a call edge so DependentFiles returns something.
	idx := models.NewRepoIndex()
	idx.AddFile(&models.ParsedFile{
		Path:     "caller.go",
		Language: "go",
		Signatures: []models.Signature{
			{Name: "Caller", Kind: "function", File: "caller.go", LineStart: 1, LineEnd: 5},
		},
		Calls: []models.CallEdge{{Caller: "Caller", Callee: "Helper", File: "caller.go", Line: 3}},
	})
	idx.AddFile(&models.ParsedFile{
		Path:     "helper.go",
		Language: "go",
		Signatures: []models.Signature{
			{Name: "Helper", Kind: "function", File: "helper.go", LineStart: 1, LineEnd: 4},
		},
	})
	idx.BuildCallGraph()

	iss := &models.Issue{
		File: "caller.go", FunctionName: "Caller", LineStart: 1,
		BugType: models.BugLogic, Severity: models.SeverityCritical, Confidence: 0.95,
	}
	forFix, _ := agg.Run(idx, []*models.Issue{iss})
	if len(forFix) != 1 {
		t.Fatalf("expected 1 PR-eligible issue, got %d", len(forFix))
	}
	deps := forFix[0].DependentFiles
	var hasHelper bool
	for _, d := range deps {
		if d == "helper.go" {
			hasHelper = true
		}
	}
	if !hasHelper {
		t.Errorf("expected DependentFiles to include helper.go, got %v", deps)
	}
}
