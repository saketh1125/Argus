// Package models holds the shared data types that flow through the
// CodeSentinel pipeline. It is the leaf of the dependency graph: it imports
// nothing from the rest of the project, so every other package can depend on
// it without creating an import cycle.
package models

// Severity classifies how serious a detected issue is. Only Critical and High
// issues (above the confidence gate) become pull requests; Medium and Low are
// rolled into the summary issue.
type Severity string

const (
	SeverityCritical Severity = "Critical"
	SeverityHigh     Severity = "High"
	SeverityMedium   Severity = "Medium"
	SeverityLow      Severity = "Low"
)

// Rank returns an ordering weight (higher = more severe) used for sorting.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// IsPREligible reports whether this severity is allowed to become a PR
// (subject to the separate confidence gate).
func (s Severity) IsPREligible() bool {
	return s == SeverityCritical || s == SeverityHigh
}

// ParseSeverity normalizes free-form LLM output into a Severity value.
func ParseSeverity(raw string) Severity {
	switch normalizeLower(raw) {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium", "moderate", "med":
		return SeverityMedium
	case "low", "minor", "info", "informational":
		return SeverityLow
	default:
		return SeverityLow
	}
}

// Common bug type identifiers. BugType is intentionally a string (not a closed
// enum) because analysts may surface categories we did not anticipate, but
// these constants keep the common cases consistent for dedup and branch naming.
const (
	BugLogic           = "logic"
	BugNullDeref       = "null_dereference"
	BugOffByOne        = "off_by_one"
	BugSQLInjection    = "sql_injection"
	BugHardcodedSecret = "hardcoded_secret"
	BugInsecureDefault = "insecure_default"
	BugImproperAuth    = "improper_auth"
	BugDeadCode        = "dead_code"
	BugComplexity      = "excessive_complexity"
	BugMissingError    = "missing_error_handling"
	BugSignatureDrift  = "signature_drift"
)

// Issue is the central artifact of the pipeline. An analyst produces it, the
// aggregator filters it, the fix generator attaches a Fix, the validator marks
// it validated, and the PR agent turns it into a GitHub PR or summary row.
type Issue struct {
	// Identity / location.
	ID           string `json:"id"`
	File         string `json:"file"`
	FunctionName string `json:"function_name"`
	LineStart    int    `json:"line_start"`
	LineEnd      int    `json:"line_end"`

	// Classification.
	BugType  string   `json:"bug_type"`
	Severity Severity `json:"severity"`

	// Evidence is the exact quoted source line(s) the analyst is reasoning
	// about. The analyst contract REJECTS any issue with empty/too-short
	// evidence — this is the system's anti-hallucination guard.
	Evidence    string  `json:"evidence"`
	Explanation string  `json:"explanation"`
	Confidence  float64 `json:"confidence"`

	// Enrichment added by the aggregator from the call graph.
	DependentFiles []string `json:"dependent_files,omitempty"`

	// Fix is attached by the fix generator (nil until then).
	Fix *Fix `json:"fix,omitempty"`

	// Validation results, set by the validator.
	Validated     bool   `json:"validated"`
	ValidationLog string `json:"validation_log,omitempty"`

	// Set true if this issue could not be validated / fixed and should be
	// reported in the summary issue instead of a PR.
	Demoted bool `json:"demoted,omitempty"`
}

// DedupKey is the hash basis for deduplication: same file + line + bug type is
// considered the same finding regardless of which analyst reported it.
func (i *Issue) DedupKey() string {
	return i.File + ":" + itoa(i.LineStart) + ":" + i.BugType
}

// FixCandidate is one of N rewrites produced by the fix generator at a given
// temperature. The diff is computed programmatically (go-diff) from the
// original function to RewrittenFunction — never produced by the LLM directly.
type FixCandidate struct {
	RewrittenFunction string  `json:"rewritten_function"`
	Diff              string  `json:"diff"`
	Confidence        float64 `json:"confidence"`
	Temperature       float64 `json:"temperature"`
	Rationale         string  `json:"rationale,omitempty"`
}

// Fix bundles the candidate rewrites for an issue and records which one was
// ultimately selected (highest ranked, then validated).
type Fix struct {
	OriginalFunction string         `json:"original_function"`
	Candidates       []FixCandidate `json:"candidates"`
	Selected         *FixCandidate  `json:"selected,omitempty"`
}

// LocationTarget is a suspicious code location handed from the localizer to an
// analyst goroutine. FullFile signals the localization fallback: send the whole
// file because per-function confidence was too low.
type LocationTarget struct {
	File         string  `json:"file"`
	FunctionName string  `json:"function_name"`
	LineStart    int     `json:"line_start"`
	LineEnd      int     `json:"line_end"`
	Confidence   float64 `json:"confidence"`
	Signature    string  `json:"signature"`
	Code         string  `json:"code"`
	FullFile     bool    `json:"full_file"`
}

// Signature is a function/method/class extracted by the parser.
type Signature struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "function" | "method" | "class"
	File      string `json:"file"`
	Language  string `json:"language"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Text      string `json:"text"` // the signature line(s)
	Body      string `json:"body"` // full source of the construct
}

// CallEdge is a single caller→callee relationship discovered during parsing.
type CallEdge struct {
	Caller string `json:"caller"`
	Callee string `json:"callee"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

// CallGraphNode is the per-function view of the call graph.
type CallGraphNode struct {
	Function string   `json:"function"`
	File     string   `json:"file"`
	Calls    []string `json:"calls"`     // functions this one calls
	CalledBy []string `json:"called_by"` // functions that call this one
}

// ParsedFile is the output of parsing one source file.
type ParsedFile struct {
	Path       string      `json:"path"`
	Language   string      `json:"language"`
	Content    string      `json:"content"`
	ModTime    int64       `json:"mod_time"`
	Signatures []Signature `json:"signatures"`
	Imports    []string    `json:"imports"`
	Calls      []CallEdge  `json:"calls"`
	ParseError string      `json:"parse_error,omitempty"`
}

// VectorPoint is one embedded item stored in the vector database.
type VectorPoint struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

// SearchResult is one hit returned from a vector similarity search.
type SearchResult struct {
	ID      string         `json:"id"`
	Score   float32        `json:"score"`
	Payload map[string]any `json:"payload"`
}

// PRResult records the outcome of creating a pull request.
type PRResult struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
	Issue  *Issue `json:"-"`
}

// RunReport is the final summary returned by the pipeline.
type RunReport struct {
	RepoURL        string     `json:"repo_url"`
	FilesAnalyzed  int        `json:"files_analyzed"`
	IssuesFound    int        `json:"issues_found"`
	PRsRaised      []PRResult `json:"prs_raised"`
	SummaryIssue   string     `json:"summary_issue_url"`
	DemotedIssues  []*Issue   `json:"demoted_issues"`
	DurationMillis int64      `json:"duration_millis"`
	Mode           string     `json:"mode"` // "live" | "mock" | "mixed"
}
