package models

// This file defines the structured input/output contract for every LLM call in
// the pipeline. Both sides depend on it:
//
//   - The agents (localizer, analyst, fix generator, validator) set
//     CompletionRequest.Task to one of the Task* constants and parse the
//     model's reply into the matching *Result struct below.
//   - The mock LLM adapter switches on Task and emits a deterministic,
//     schema-correct *Result as JSON, so the whole pipeline runs offline.
//   - The real NIM adapter ignores Task; the agents' prompts instruct the real
//     model to return JSON matching these same structs.
//
// Keeping these schemas in the leaf models package means the services author
// and the agents author never have to coordinate directly.

// Task identifiers carried in CompletionRequest.Task.
const (
	TaskRerank  = "rerank"  // localizer stage 2: rank candidate locations
	TaskAnalyze = "analyze" // analyst: inspect one location for a bug
	TaskFixGen  = "fixgen"  // fix generator: rewrite a function
	TaskTestGen = "testgen" // validator: synthesize a minimal test
)

// RerankResult is returned for TaskRerank. Rankings refers to candidates by
// their zero-based index in the prompt's candidate list.
type RerankResult struct {
	Rankings []RerankItem `json:"rankings"`
}

// RerankItem scores one candidate location.
type RerankItem struct {
	Index      int     `json:"index"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`
}

// AnalystResult is returned for TaskAnalyze. Found=false means "no bug here"
// (the analyst returns nil). When Found=true the embedded fields populate an
// Issue. Evidence MUST be a non-empty exact quote or the analyst rejects it.
type AnalystResult struct {
	Found        bool    `json:"found"`
	BugType      string  `json:"bug_type"`
	Severity     string  `json:"severity"`
	LineStart    int     `json:"line_start"`
	LineEnd      int     `json:"line_end"`
	Evidence     string  `json:"evidence"`
	Explanation  string  `json:"explanation"`
	Confidence   float64 `json:"confidence"`
	FunctionName string  `json:"function_name,omitempty"`
}

// FixResult is returned for TaskFixGen: the complete rewritten function (never
// a diff — the diff is computed programmatically from original -> rewrite).
type FixResult struct {
	RewrittenFunction string  `json:"rewritten_function"`
	Confidence        float64 `json:"confidence"`
	Rationale         string  `json:"rationale,omitempty"`
}

// TestGenResult is returned for TaskTestGen when a function has no existing
// tests: a minimal test plus the filename/command to run it.
type TestGenResult struct {
	Filename string `json:"filename"`
	TestCode string `json:"test_code"`
	RunCmd   string `json:"run_cmd"`
}
