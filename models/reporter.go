package models

// Reporter is the progress sink the pipeline writes to. The dashboard
// implements it (live SSE updates); tests and headless runs use NoopReporter.
// Defining it here (in the leaf package) lets every agent emit progress without
// importing the dashboard package, avoiding an import cycle.
//
// Implementations MUST be safe for concurrent use — analyst goroutines call
// these methods in parallel.
type Reporter interface {
	SetStage(stage string, pct int)
	SetFilesProgress(done, total int)
	AddIssue(issue *Issue)
	SetPRsRaised(n int)
	SetValidated(done, total int)
	SetRPM(current, max int)
	SetAgentStatus(agent, status string)
	Log(format string, args ...any)
}

// NoopReporter discards all progress events. Useful for tests and CLI runs
// where no dashboard is attached.
type NoopReporter struct{}

func (NoopReporter) SetStage(string, int)          {}
func (NoopReporter) SetFilesProgress(int, int)     {}
func (NoopReporter) AddIssue(*Issue)               {}
func (NoopReporter) SetPRsRaised(int)              {}
func (NoopReporter) SetValidated(int, int)         {}
func (NoopReporter) SetRPM(int, int)               {}
func (NoopReporter) SetAgentStatus(string, string) {}
func (NoopReporter) Log(string, ...any)            {}

// Ensure NoopReporter satisfies Reporter at compile time.
var _ Reporter = NoopReporter{}

// Pipeline stage names, shared between the orchestrator and dashboard.
const (
	StageIngest    = "Preprocessing"
	StageLocalize  = "Localizing"
	StageAnalyze   = "Analyzing"
	StageAggregate = "Aggregating"
	StageFix       = "Generating Fixes"
	StageValidate  = "Validating"
	StagePR        = "Raising PRs"
	StageDone      = "Complete"
)
