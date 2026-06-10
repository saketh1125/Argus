package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/saketh/codesentinel/models"
)

func TestFixGeneratorProducesDiff(t *testing.T) {
	idx := models.NewRepoIndex()
	idx.AddFile(&models.ParsedFile{
		Path:     "calc.go",
		Language: "go",
		Content:  "func Div(a, b int) int {\n\treturn a / b\n}\n",
		Signatures: []models.Signature{
			{Name: "Div", Kind: "function", File: "calc.go", LineStart: 1, LineEnd: 3,
				Body: "func Div(a, b int) int {\n\treturn a / b\n}"},
		},
	})
	idx.BuildCallGraph()

	llm := &fakeLLM{byTask: map[string]string{
		models.TaskFixGen: `{"rewritten_function":"func Div(a, b int) (int, error) {\n\tif b == 0 {\n\t\treturn 0, errors.New(\"div by zero\")\n\t}\n\treturn a / b, nil\n}","confidence":0.9,"rationale":"guard zero"}`,
	}}

	cfg := testConfig()
	cfg.FixCandidates = 2
	fg := NewFixGenerator(llm, cfg, models.NoopReporter{})

	iss := &models.Issue{
		File: "calc.go", FunctionName: "Div", LineStart: 1, LineEnd: 3,
		BugType: models.BugLogic, Severity: models.SeverityCritical, Confidence: 0.95,
		Evidence: "return a / b", Explanation: "division by zero",
	}
	out, err := fg.Run(context.Background(), idx, []*models.Issue{iss})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Fix == nil {
		t.Fatal("expected Fix to be attached")
	}
	if out[0].Fix.Selected == nil {
		t.Fatal("expected a Selected candidate")
	}
	if !strings.Contains(out[0].Fix.Selected.Diff, "+") {
		t.Errorf("expected non-empty diff, got %q", out[0].Fix.Selected.Diff)
	}
	if out[0].Fix.OriginalFunction == "" {
		t.Error("expected OriginalFunction to be captured")
	}
}
