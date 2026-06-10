package agents

import (
	"context"
	"testing"

	"github.com/saketh/codesentinel/models"
	"github.com/saketh/codesentinel/services"
)

// fakeLLM is a self-contained LLMClient that returns canned JSON keyed by Task.
// The per-task responses can be overridden per test.
type fakeLLM struct {
	byTask map[string]string
}

func (f *fakeLLM) Complete(_ context.Context, req services.CompletionRequest) (*services.CompletionResponse, error) {
	text := f.byTask[req.Task]
	return &services.CompletionResponse{Text: text, Model: "fake"}, nil
}
func (f *fakeLLM) Name() string { return "fake" }
func (f *fakeLLM) Live() bool   { return false }

func newIndexWithFunc() *models.RepoIndex {
	idx := models.NewRepoIndex()
	idx.AddFile(&models.ParsedFile{
		Path:     "auth.py",
		Language: "python",
		Content:  "def login(user):\n    if user == None:\n        return user.name\n",
		Signatures: []models.Signature{
			{Name: "login", Kind: "function", File: "auth.py", LineStart: 1, LineEnd: 3,
				Body: "def login(user):\n    if user == None:\n        return user.name"},
		},
	})
	idx.BuildCallGraph()
	return idx
}

func target() models.LocationTarget {
	return models.LocationTarget{
		File: "auth.py", FunctionName: "login", LineStart: 1, LineEnd: 3,
		Code: "def login(user):\n    if user == None:\n        return user.name",
	}
}

func TestAnalystConfirmsGoodIssue(t *testing.T) {
	llm := &fakeLLM{byTask: map[string]string{
		models.TaskAnalyze: `{"found":true,"bug_type":"null_dereference","severity":"High",
			"line_start":3,"line_end":3,"evidence":"return user.name when user is None",
			"explanation":"Null dereference on user.","confidence":0.9,"function_name":"login"}`,
	}}
	a := NewAnalyst(llm, testConfig(), models.NoopReporter{})

	issues, err := a.Run(context.Background(), newIndexWithFunc(), []models.LocationTarget{target()})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 confirmed issue, got %d", len(issues))
	}
	got := issues[0]
	if got.BugType != "null_dereference" || got.Severity != models.SeverityHigh {
		t.Errorf("unexpected classification: %s/%s", got.BugType, got.Severity)
	}
	if got.ID == "" {
		t.Error("expected Issue.ID to be set")
	}
}

func TestAnalystRejectsShortEvidence(t *testing.T) {
	llm := &fakeLLM{byTask: map[string]string{
		models.TaskAnalyze: `{"found":true,"bug_type":"logic","severity":"High",
			"line_start":1,"line_end":1,"evidence":"bad","explanation":"x","confidence":0.9}`,
	}}
	a := NewAnalyst(llm, testConfig(), models.NoopReporter{})
	issues, _ := a.Run(context.Background(), newIndexWithFunc(), []models.LocationTarget{target()})
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues for short evidence, got %d", len(issues))
	}
}

func TestAnalystRejectsLowConfidence(t *testing.T) {
	llm := &fakeLLM{byTask: map[string]string{
		models.TaskAnalyze: `{"found":true,"bug_type":"logic","severity":"High",
			"line_start":1,"line_end":1,"evidence":"this is long enough evidence","explanation":"x","confidence":0.20}`,
	}}
	a := NewAnalyst(llm, testConfig(), models.NoopReporter{})
	issues, _ := a.Run(context.Background(), newIndexWithFunc(), []models.LocationTarget{target()})
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues for low confidence, got %d", len(issues))
	}
}

func TestAnalystNotFound(t *testing.T) {
	llm := &fakeLLM{byTask: map[string]string{
		models.TaskAnalyze: `{"found":false}`,
	}}
	a := NewAnalyst(llm, testConfig(), models.NoopReporter{})
	issues, _ := a.Run(context.Background(), newIndexWithFunc(), []models.LocationTarget{target()})
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues for found=false, got %d", len(issues))
	}
}
