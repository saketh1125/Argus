# API Contract & Technical Specification
## CodeSentinel — Autonomous Multi-Agent Bug Detection & Auto PR System
**Version:** 1.0  
**Status:** Locked (reflects as-built stdlib implementation)  
**Date:** June 2026  

---

## 1. Overview

This document defines every interface boundary in CodeSentinel — between agents, between agents and tools, and between the system and external APIs. All contracts are binding. An agent may not pass data that does not conform to the schema defined here.

### Implementation Note
The production build uses Go stdlib `net/http` for all external API calls (NIM, GitHub, Qdrant, E2B, Ollama). No external SDK dependencies. All interfaces are identical to the original SDK-backed design — adapters are swappable behind the same Go interfaces.

---

## 2. Core Data Contracts

All core types live in `models/types.go`. The `models` package has no dependencies
on `services`, `tools`, or `agents` — every other package depends on it.

### 2.1 Severity (typed, not a bare string)

```go
// models/types.go

type Severity string

const (
    SeverityCritical Severity = "Critical"
    SeverityHigh     Severity = "High"
    SeverityMedium   Severity = "Medium"
    SeverityLow      Severity = "Low"
)

func (s Severity) Rank() int         // Critical=4, High=3, Medium=2, Low=1, unknown=0
func (s Severity) IsPREligible() bool // true only for Critical/High
func ParseSeverity(raw string) Severity // normalizes free-form LLM text; defaults to Low
```

**Invariant:** an issue is PR-eligible only if `Severity.IsPREligible()` **and**
`Confidence >= CONFIDENCE_GATE` (default 0.80). Both gates are enforced by the aggregator.

### 2.2 Issue (Primary Inter-Agent Struct)

```go
type Issue struct {
    // Identity / location.
    ID           string `json:"id"`
    File         string `json:"file"`
    FunctionName string `json:"function_name"`
    LineStart    int    `json:"line_start"`
    LineEnd      int    `json:"line_end"`

    // Classification.
    BugType  string   `json:"bug_type"`  // see BugType constants below
    Severity Severity `json:"severity"`

    // Evidence — MANDATORY. Empty/too-short = rejected by the analyst.
    Evidence    string  `json:"evidence"`    // exact quoted line(s) from source
    Explanation string  `json:"explanation"` // why this is wrong
    Confidence  float64 `json:"confidence"`  // 0.0 – 1.0

    // Enrichment added by the aggregator from the call graph.
    DependentFiles []string `json:"dependent_files,omitempty"`

    // Fix attached by the fix generator (nil until then).
    Fix *Fix `json:"fix,omitempty"`

    // Validation results, set by the validator.
    Validated     bool   `json:"validated"`
    ValidationLog string `json:"validation_log,omitempty"`

    // Set true when the issue could not be validated/fixed and should appear in
    // the summary issue instead of a PR.
    Demoted bool `json:"demoted,omitempty"`
}

// DedupKey is a method, not a stored field: file:line:bug_type.
func (i *Issue) DedupKey() string
```

**BugType constants** (`BugType` is an open string, not a closed enum — analysts may
surface unanticipated categories; these keep common cases consistent for dedup and
branch naming):
`logic`, `null_dereference`, `off_by_one`, `sql_injection`, `hardcoded_secret`,
`insecure_default`, `improper_auth`, `dead_code`, `excessive_complexity`,
`missing_error_handling`, `signature_drift`.

**Invariants:**
- `Evidence` is required; the analyst rejects an issue with empty/too-short evidence (anti-hallucination guard).
- `Confidence` is in [0.0, 1.0].
- `Severity` is one of the four typed `Severity` constants (`"Critical"`, `"High"`, `"Medium"`, `"Low"` — title case).

---

### 2.3 Fix & FixCandidate

```go
type FixCandidate struct {
    RewrittenFunction string  `json:"rewritten_function"` // full function, never a fragment or diff
    Diff              string  `json:"diff"`               // computed by tools.UnifiedDiff, never the LLM
    Confidence        float64 `json:"confidence"`
    Temperature       float64 `json:"temperature"`        // sampling temperature used
    Rationale         string  `json:"rationale,omitempty"`
}

type Fix struct {
    OriginalFunction string         `json:"original_function"`
    Candidates       []FixCandidate `json:"candidates"`
    Selected         *FixCandidate  `json:"selected,omitempty"` // pointer; nil until one is chosen+validated
}
```

**Invariants:**
- `Candidates` holds up to `FIX_CANDIDATES` entries (default 3), generated across a temperature schedule.
- `RewrittenFunction` is the complete function. Never a fragment. Never a diff.
- `Diff` is always produced programmatically by `tools.UnifiedDiff` (pure-Go LCS), never by the LLM.

---

### 2.4 LocationTarget

```go
type LocationTarget struct {
    File         string  `json:"file"`
    FunctionName string  `json:"function_name"`
    LineStart    int     `json:"line_start"`
    LineEnd      int     `json:"line_end"`
    Confidence   float64 `json:"confidence"` // from vector search + LLM rerank
    Signature    string  `json:"signature"`
    Code         string  `json:"code"`
    FullFile     bool    `json:"full_file"`  // true => localization fallback: analyze the whole file
}
```

---

### 2.5 Repo graph types

```go
type Signature struct {
    Name, Kind, File, Language string // Kind: "function" | "method" | "class"
    LineStart, LineEnd         int
    Text                       string // the signature line(s)
    Body                       string // full source of the construct
}

type CallEdge struct {
    Caller, Callee, File string
    Line                 int
}

type CallGraphNode struct {
    Function string   `json:"function"`
    File     string   `json:"file"`
    Calls    []string `json:"calls"`     // functions this one calls
    CalledBy []string `json:"called_by"` // functions that call this one
}

type ParsedFile struct {
    Path, Language, Content string
    ModTime                 int64
    Signatures              []Signature
    Imports                 []string
    Calls                   []CallEdge
    ParseError              string
}

type VectorPoint struct {
    ID      string
    Vector  []float32
    Payload map[string]any
}

type SearchResult struct {
    ID      string
    Score   float32
    Payload map[string]any
}
```

---

### 2.6 RepoIndex (shared in-memory knowledge base)

`models.RepoIndex` is the concurrency-safe (RWMutex) index the preprocessor writes
once and the localizer/analysts/aggregator read in parallel.

```go
func NewRepoIndex() *RepoIndex
func (r *RepoIndex) AddFile(f *ParsedFile)
func (r *RepoIndex) BuildCallGraph()                       // call once after all AddFile
func (r *RepoIndex) GetCallGraph(fn string) *CallGraphNode // defensive copy
func (r *RepoIndex) DependentFiles(fn string) []string     // files in the call neighborhood
func (r *RepoIndex) GetFile(path string) (*ParsedFile, bool)
func (r *RepoIndex) Files() []*ParsedFile
func (r *RepoIndex) Signatures() []Signature
func (r *RepoIndex) FileCount() int
```

---

### 2.7 PRResult & RunReport (pipeline output)

```go
type PRResult struct {
    Number int
    URL    string
    Branch string
    Issue  *Issue `json:"-"`
}

type RunReport struct {
    RepoURL        string
    FilesAnalyzed  int
    IssuesFound    int
    PRsRaised      []PRResult
    SummaryIssue   string   `json:"summary_issue_url"`
    DemotedIssues  []*Issue
    DurationMillis int64
    Mode           string   // "live" | "mock" | "mixed"
}
```

---

### 2.8 LLM I/O schemas (`models/llmio.go`)

The mock LLM and the agents agree on these structs without coordinating. Each request
carries a `Task` hint (see §3.1) selecting which schema applies.

```go
const (
    TaskRerank  = "rerank"  // localizer stage 2: rank candidate locations
    TaskAnalyze = "analyze" // analyst: inspect one location for a bug
    TaskFixGen  = "fixgen"  // fix generator: rewrite a function
    TaskTestGen = "testgen" // validator: synthesize a minimal test
)

type RerankResult struct { Rankings []RerankItem `json:"rankings"` }
type RerankItem   struct { Index int; Confidence float64; Reason string }

type AnalystResult struct {
    Found        bool
    BugType      string
    Severity     string
    LineStart    int
    LineEnd      int
    Evidence     string
    Explanation  string
    Confidence   float64
    FunctionName string
}

type FixResult     struct { RewrittenFunction string; Confidence float64; Rationale string }
type TestGenResult struct { Filename string; TestCode string; RunCmd string }
```

---

## 3. Tool & Service Contracts

External systems sit behind interfaces (`tools/interfaces.go`, `services/interfaces.go`).
Each interface has a **real** adapter and a deterministic **mock** adapter; the factory
auto-selects per integration by credential/service availability. Tools are pure I/O —
no reasoning, no LLM calls.

### 3.1 LLM service (`services.LLMClient`)

```go
// services/interfaces.go

type CompletionRequest struct {
    System      string
    User        string
    Temperature float64
    MaxTokens   int
    JSONMode    bool   // request structured JSON when the provider supports it
    Model       string // optional per-call model override
    Task        string // one of models.Task* — mock uses it to pick the schema-correct reply
}

type CompletionResponse struct {
    Text         string
    Model        string
    PromptTokens int
    OutputTokens int
}

type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
    Name() string // e.g. "nim:meta/llama-3.1-70b-instruct" or "mock-llm"
    Live() bool   // real adapter (true) vs mock (false)
}
```

### 3.2 Embeddings & Vector store

```go
type EmbeddingClient interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dim() int     // active provider dimensionality
    Name() string // e.g. "ollama:nomic-embed-text"
    Live() bool
}

type VectorStore interface {
    EnsureCollection(ctx context.Context, name string, dim int) error
    Upsert(ctx context.Context, name string, points []models.VectorPoint) error
    Search(ctx context.Context, name string, vector []float32, topK int) ([]models.SearchResult, error)
    Name() string
    Live() bool
}
```

### 3.3 Sandbox service (`services.Sandbox`)

Replaces the doc's old free-function `CheckSyntax` — syntax validation runs inside the sandbox.

```go
type SandboxSpec struct {
    Language   string
    Files      map[string]string // relative path -> content
    Commands   []string          // executed in order; first failure stops
    TimeoutSec int
}

type SandboxResult struct {
    Success  bool
    ExitCode int
    Stdout   string
    Stderr   string
    Logs     string
}

type Sandbox interface {
    Run(ctx context.Context, spec SandboxSpec) (*SandboxResult, error)
    Name() string // "e2b" or "local-sandbox"
    Live() bool
}
```

The local sandbox executes commands via `sh -c` (after a `LookPath` probe of the
interpreter) so shell-quoted filenames are handled correctly.

### 3.4 GitHub tool (`tools.GitHubClient`)

```go
// tools/interfaces.go

type PRSpec     struct { Title, Body, Head, Base string }
type CommitSpec struct { Branch, Path, Content, Message string }

type GitHubClient interface {
    ParseURL(url string) (owner, repo string, err error)
    Clone(ctx context.Context, url, dest string) (string, error)
    DefaultBranch(ctx context.Context, owner, repo string) (string, error)
    CreateBranch(ctx context.Context, owner, repo, base, name string) error
    CommitFile(ctx context.Context, owner, repo string, spec CommitSpec) error
    CreatePR(ctx context.Context, owner, repo string, spec PRSpec) (number int, url string, err error)
    CreateIssue(ctx context.Context, owner, repo, title, body string) (number int, url string, err error)
    Name() string // "github" or "mock-github"
    Live() bool
}
```

**Branch naming convention:** `codesentinel/fix-{bug_type}-{sanitized_file}-L{line}`.

**Real-adapter GitHub REST endpoints (stdlib `net/http`):**
```
GET  /repos/{owner}/{repo}/git/ref/heads/{default_branch}
POST /repos/{owner}/{repo}/git/refs
GET  /repos/{owner}/{repo}/contents/{path}
PUT  /repos/{owner}/{repo}/contents/{path}
POST /repos/{owner}/{repo}/pulls
POST /repos/{owner}/{repo}/issues
```

### 3.5 Code parser (`tools.CodeParser`)

```go
type CodeParser interface {
    Supports(ext string) bool          // handles this extension?
    Language(ext string) string        // ".go" -> "go"
    Parse(path, content string) (*models.ParsedFile, error)
    Name() string
}
```

The shipped implementation is a pure-Go regex/heuristic parser (no tree-sitter/CGO).

### 3.6 Navigation & patch helpers (pure functions, `tools/`)

```go
// tools/navigation.go
func NumberedChunk(code string, baseLine int) string          // "LINE_BASE: N" + numbered lines
func GetCodeChunk(content string, lineStart, lineEnd int) string
func SearchSymbol(idx *models.RepoIndex, query string) []models.Signature
func FunctionSource(idx *models.RepoIndex, file, name string) (models.Signature, bool)

// tools/patch.go
func ApplyFunctionRewrite(fileContent, originalFunc, newFunc string) (string, error)
func UnifiedDiff(original, modified, filename string) string   // pure-Go LCS unified diff
```

---

## 4. Agent Contracts

Each agent is a concrete struct constructed with its dependencies (services, tools,
config, reporter) and exposes a single **`Run`** method. Agents are batch-oriented and
thread the shared `*models.RepoIndex` rather than operating one item at a time. The
pipeline in `main.go` invokes them in order: Preprocessor → Localizer → Analyst →
Aggregator → FixGenerator → Validator → PRAgent.

```go
// agents/

// Preprocessor — clone, parse, embed, index, build call graph.
func NewPreprocessor(gh tools.GitHubClient, parser tools.CodeParser,
    embed services.EmbeddingClient, vstore services.VectorStore,
    cfg *config.Config, rep models.Reporter) *Preprocessor
func (p *Preprocessor) Run(ctx context.Context, repoURL string) (
    idx *models.RepoIndex, workdir, owner, repo string, err error)

// Localizer — two-stage retrieval (vector search + LLM rerank) -> suspicious targets.
func NewLocalizer(llm services.LLMClient, embed services.EmbeddingClient,
    vstore services.VectorStore, cfg *config.Config, rep models.Reporter) *Localizer
func (l *Localizer) Run(ctx context.Context, idx *models.RepoIndex) ([]models.LocationTarget, error)

// Analyst — parallel goroutines (capped at ANALYST_PARALLEL) inspect each target.
// Issues failing the evidence/confidence guard are dropped (not returned).
func NewAnalyst(llm services.LLMClient, cfg *config.Config, rep models.Reporter) *Analyst
func (a *Analyst) Run(ctx context.Context, idx *models.RepoIndex,
    targets []models.LocationTarget) ([]*models.Issue, error)

// Aggregator — dedup, call-graph enrichment, and severity/confidence gating.
// Returns (forFix: PR-eligible) and (summary: demoted) partitions. No ctx/error.
func NewAggregator(cfg *config.Config, rep models.Reporter) *Aggregator
func (a *Aggregator) Run(idx *models.RepoIndex, issues []*models.Issue) (
    forFix []*models.Issue, summary []*models.Issue)

// FixGenerator — N candidates across a temperature schedule; attaches Issue.Fix.
func NewFixGenerator(llm services.LLMClient, cfg *config.Config, rep models.Reporter) *FixGenerator
func (f *FixGenerator) Run(ctx context.Context, idx *models.RepoIndex,
    issues []*models.Issue) ([]*models.Issue, error)

// Validator — applies the rewrite and runs a sandbox syntax check; sets
// Validated / demotes on failure.
func NewValidator(sandbox services.Sandbox, llm services.LLMClient,
    parser tools.CodeParser, cfg *config.Config, rep models.Reporter) *Validator
func (v *Validator) Run(ctx context.Context, workdir string,
    issues []*models.Issue) ([]*models.Issue, error)

// PRAgent — raises one PR per validated issue and a single summary issue for the rest.
func NewPRAgent(gh tools.GitHubClient, cfg *config.Config, rep models.Reporter) *PRAgent
func (p *PRAgent) Run(ctx context.Context, owner, repo, base string,
    validated []*models.Issue, summary []*models.Issue) (*models.RunReport, error)
```

### 4.1 Wiring (factories)

```go
// services.New builds all service adapters and computes Mode.
func services.New(ctx context.Context, cfg *config.Config) (*services.Clients, error)

type Clients struct {
    LLM     LLMClient
    Embed   EmbeddingClient
    Vectors VectorStore
    Sandbox Sandbox
    Limiter RateLimiter
    Mode    string // "live" | "mock" | "mixed"
}

// tools.NewGitHub / tools.NewParser pick the real or mock adapter from cfg.
func tools.NewGitHub(cfg *config.Config) GitHubClient
func tools.NewParser(cfg *config.Config) CodeParser
```

### 4.2 Reporter (progress sink, `models/reporter.go`)

Agents report progress through this interface. The live `*dashboard.Dashboard`
implements it; `models.NoopReporter` is used with `--no-dashboard`.

```go
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

// Stage constants (title-case display strings):
StageIngest="Preprocessing"  StageLocalize="Localizing"  StageAnalyze="Analyzing"
StageAggregate="Aggregating"  StageFix="Generating Fixes"  StageValidate="Validating"
StagePR="Raising PRs"  StageDone="Complete"
```

---

## 5. External API Contracts

### 5.1 Nvidia NIM API

**Base URL:** `https://integrate.api.nvidia.com/v1`  
**Auth:** `Authorization: Bearer {NIM_API_KEY}`  
**Rate limit:** 40 RPM (system operates at 35 RPM via RateLimiter)

**Request:**
```json
POST /chat/completions
{
  "model": "meta/llama-3.1-70b-instruct",
  "messages": [
    {"role": "system", "content": "{system_prompt}"},
    {"role": "user",   "content": "{user_content}"}
  ],
  "temperature": 0.2,
  "max_tokens": 2048,
  "stream": false
}
```

**Response (relevant fields):**
```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "{llm_output}"
    },
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 512,
    "completion_tokens": 384
  }
}
```

**Error handling:**
- `429` → RateLimiter should have prevented this. Log + wait 10s + retry once.
- `500/503` → Retry with exponential backoff (1s, 2s, 4s). Max 3 attempts.
- `401` → Fatal. Invalid API key. Surface to user immediately.

---

### 5.2 Analyst Agent — LLM Prompt Contract

**System prompt (sent once, cached):**
```
You are a senior software engineer performing a security and quality audit.

You MUST respond ONLY with a valid JSON object matching this exact schema.
Do not include markdown, explanation, or any text outside the JSON.

Schema:
{
  "file": "string — exact file path",
  "line_start": "integer",
  "line_end": "integer",
  "bug_type": "one of: logic | security | quality",
  "severity": "one of: critical | high | medium | low",
  "evidence": "string — EXACT quoted lines from the code, minimum 10 chars",
  "explanation": "string — why this is a bug, specific and technical",
  "confidence": "float between 0.0 and 1.0"
}

If you find no genuine bug, return: {"confidence": 0.0, "evidence": ""}
Do not invent bugs. Do not return a bug without citing exact evidence.
```

**User prompt structure:**
```
Analyze the following code chunk for bugs, security vulnerabilities, and quality issues.

File: {path}
Language: {language}
Lines {line_start}–{line_end}:

{sanitized_code_chunk}

Call graph context:
- Callers: {callers}
- Callees: {callees}
- Imports: {imports}
```

---

### 5.3 Fix Generator — LLM Prompt Contract

**System prompt:**
```
You are a senior software engineer writing a bug fix.

You MUST respond ONLY with the complete rewritten function — no explanation,
no diff syntax, no markdown code fences, no line numbers.

Return the ENTIRE function from its signature to its closing brace.
Do not return a fragment. Do not return a diff.
```

**User prompt:**
```
Fix the following bug in {language}.

Bug: {explanation}
Evidence: {evidence}
Severity: {severity}

Original function (lines {line_start}–{line_end} of {file}):

{original_function}

Rewrite the complete function with the bug fixed.
```

**Called 3 times with temperatures:** `[0.2, 0.5, 0.7]`

---

### 5.4 E2B Sandbox API

**Base URL:** `https://api.e2b.dev`  
**Auth:** `E2B_API_KEY` header  
**Median startup:** 78ms (Firecracker microVM)

**Sandbox lifecycle per validation:**
```
POST /sandboxes              → create sandbox, get sandbox_id
POST /sandboxes/{id}/files   → upload repo + patched file
POST /sandboxes/{id}/commands → run syntax check
POST /sandboxes/{id}/commands → run tests (if exist)
DELETE /sandboxes/{id}       → destroy sandbox
```

**Command contract:**
```json
POST /sandboxes/{id}/commands
{
  "cmd": "sh",
  "args": ["-c", "{validation_command}"],
  "timeout": 30
}
```

**Validation commands by language:**
```
Python:     python -m py_compile {file}
JavaScript: node --check {file}
TypeScript: npx tsc --noEmit {file}
Go:         go build ./...
Java:       javac {file}
Rust:       rustc --edition 2021 {file}
C/C++:      gcc -fsyntax-only {file}
Ruby:       ruby -c {file}
```

**Response:**
```json
{
  "exit_code": 0,
  "stdout": "",
  "stderr": "",
  "timed_out": false
}
```

`exit_code == 0` → syntax valid → proceed to test run.

---

### 5.5 Qdrant Vector DB

**Mode:** Local Docker instance  
**Port:** `6333`  
**Collection name:** `codesentinel_signatures`  
**Vector dimensions:** 768 (nomic-embed-text) | 768 (jina-v2-code) | 384 (MiniLM)

**Upsert (during preprocessing):**
```json
PUT /collections/codesentinel_signatures/points
{
  "points": [{
    "id": "{uuid}",
    "vector": [0.12, -0.34, ...],
    "payload": {
      "file": "auth/handler.go",
      "func_name": "validateToken",
      "line_start": 42,
      "line_end": 67,
      "language": "go",
      "signature_text": "func validateToken(token string) (*Claims, error)"
    }
  }]
}
```

**Query (Stage 1 localization):**
```json
POST /collections/codesentinel_signatures/points/search
{
  "vector": [0.12, -0.34, ...],
  "limit": 10,
  "with_payload": true
}
```

---

### 5.6 Ollama Embedding API

**Base URL:** `http://localhost:11434`  
**Model:** `nomic-embed-text`

**Request:**
```json
POST /api/embeddings
{
  "model": "nomic-embed-text",
  "prompt": "{text_to_embed}"
}
```

**Response:**
```json
{
  "embedding": [0.12, -0.34, 0.56, ...]
}
```

**Health check (used for startup detection):**
```
GET /api/tags
→ 200 with model list = Ollama running
→ connection refused = fall back to next embedding provider
```

---

## 6. Dashboard SSE Contract

The dashboard is **server-rendered HTML over SSE** (HTMX-style), not a JSON feed.
`*dashboard.Dashboard` implements `models.Reporter`; agents push state through that
interface and the server re-renders a panel fragment.

```go
func (d *Dashboard) Start() error            // binds DASHBOARD_PORT, serves "/" and "/events"
func (d *Dashboard) URL() string             // e.g. http://localhost:8080
func (d *Dashboard) Stop(ctx context.Context) error
```

**Endpoints:**
- `GET /` → `text/html` full page; an `EventSource("/events")` swaps `#app`'s innerHTML on each message (no page reload, FR-09).
- `GET /events` → `text/event-stream`; each message is a server-rendered HTML fragment (the live panel). Multi-line fragments are split so every output line carries its own `data: ` prefix, per the SSE spec.

**Payload:** the rendered panel fragment (HTML), **not** JSON.

**Stage labels** are the title-case `models.Stage*` constants (see §4.2):
`Preprocessing → Localizing → Analyzing → Aggregating → Generating Fixes → Validating → Raising PRs → Complete`.

---

## 7. Rate Limiter Contract

```go
// services/rate_limiter.go — sliding-window limiter, goroutine-safe.
// Every LLM call routes through Wait() inside the adapter before hitting the API.

// NewRateLimiter returns the interface implementation for the given RPM ceiling
// (built once in services.New from cfg.MaxRPM; default 35 under a 40 RPM hard limit).
func NewRateLimiter(maxRPM int) RateLimiter

type RateLimiter interface {
    // Wait blocks until a call slot frees up, or returns ctx.Err() on cancellation.
    Wait(ctx context.Context) error
    // Snapshot returns the current in-window call count and the configured max.
    Snapshot() (current, max int)
}
```

**Usage:** the limiter is created once in `services.New`, stored on `Clients.Limiter`,
and shared by the live LLM adapter. `main.go` pumps `Snapshot()` into the dashboard via
`Reporter.SetRPM`.

---

## 8. Sanitizer Contract

```go
// services/sanitizer.go

// SanitizeForLLM removes content that could cause prompt injection.
// MUST be called on every file before it enters any LLM prompt.
// Input: raw file content string
// Output: sanitized content string (may be shorter, never nil)
func SanitizeForLLM(content string) string
```

**What it removes:**
- C0/C1 control characters (except `\n`, `\t`, `\r`)
- Base64 blobs > 200 characters (replaced with `[REDACTED_BLOB]`)
- Invisible unicode: U+200B (zero-width space), U+2060 (word joiner), U+FEFF (BOM)
- Null bytes

**What it preserves:**
- All printable ASCII
- All valid unicode text characters
- Newlines and tabs (needed for code structure)

---

## 9. Error Handling Contract

Errors are handled inline per stage using Go's standard `error` (no custom error type).
Three behaviors are applied by convention:

- **Fatal** — the agent's `Run` returns a non-nil `error`; the pipeline aborts and `main.go` prints the failure.
- **Skippable** — the offending item is dropped or demoted (`Issue.Demoted = true`, routed to the summary issue) and the run continues.
- **Retryable** — handled inside the relevant adapter (e.g. HTTP backoff in the NIM/GitHub clients), invisible to agents.

Graceful degradation is part of the contract: if a live service is unreachable, its
factory falls back to the mock adapter and the run continues in `mixed`/`mock` mode
rather than failing.

**Behavior by stage:**

| Stage | Error | Kind |
|---|---|---|
| Preprocessor | Clone failure | Fatal |
| Preprocessor | Single file parse failure | Skippable |
| Preprocessor | Embedding failure (all providers) | Fatal |
| Localizer | Vector search failure | Retryable (×2) |
| Analyst | LLM timeout | Skippable (goroutine returns nil) |
| Analyst | Invalid output schema | Skippable |
| FixGenerator | LLM failure on all 3 candidates | Skippable (issue → Summary) |
| Validator | E2B spinup failure | Retryable (×1) |
| Validator | Syntax failure after retry | Skippable (issue → Summary) |
| PRAgent | GitHub API 401 | Fatal |
| PRAgent | GitHub API 422 (branch exists) | Retryable (rename branch) |
| PRAgent | GitHub API 5xx | Retryable (×3, exponential backoff) |

---

## 10. Configuration Contract

Configuration via environment variables, loaded by `config.Load()` which also parses a
`.env` file in the working directory. Any blank credential routes that integration to
its mock adapter. See `.env.example` for a ready-to-copy template.

```bash
# --- Credentials (blank => that integration runs in mock mode) ---
GITHUB_TOKEN=                 # repo read + PR/issue write
NIM_API_KEY=                  # Nvidia NIM (LLM)
E2B_API_KEY=                  # E2B sandbox
JINA_API_KEY=                 # embedding fallback 1
GEMINI_API_KEY=               # embedding fallback 2

# --- LLM endpoint ---
NIM_BASE_URL=https://integrate.api.nvidia.com/v1
NIM_MODEL=meta/llama-3.1-70b-instruct

# --- Local services ---
OLLAMA_HOST=http://localhost:11434
QDRANT_HOST=localhost
QDRANT_PORT=6334              # default 6334; the REST adapter talks to 6333
EMBED_MODEL=nomic-embed-text

# --- Tunables (defaults shown) ---
MAX_RPM=35
MAX_FILES=50
ANALYST_PARALLEL=10           # max parallel analyst goroutines
FIX_CANDIDATES=3
CONFIDENCE_GATE=0.80
LOCALIZE_FALLBACK=0.50        # below this, analyze the whole file
SANDBOX_TIMEOUT=60            # seconds
DASHBOARD_PORT=8080

# --- Behavior flags ---
FORCE_MOCK=false              # true => mock every integration regardless of keys
NO_DASHBOARD=false            # true => disable the dashboard server
```

---

## 11. PR Body Template Contract

Every PR body MUST follow this exact structure:

```markdown
## CodeSentinel Analysis Report

**Type:** {bug_type}
**Severity:** {severity}
**Confidence:** {confidence_percent}%
**Validation:** ✅ Passed E2B sandbox syntax check

### Evidence
**File:** `{file}` | **Lines:** {line_start}–{line_end}

```
{evidence}
```

### Why This Is a Bug
{explanation}

### Fix Applied
```diff
{diff}
```

### Call Graph Context
- **Callers:** {callers_list}
- **Callees:** {callees_list}

---
*Generated by [CodeSentinel](https://github.com/{owner}/{repo}) —
Autonomous Multi-Agent Bug Detection System*
*Pipeline run: {timestamp} | Files analyzed: {files_analyzed}*
```

---

## 12. Summary Issue Template Contract

```markdown
# CodeSentinel Analysis Summary — {N} findings

This issue was automatically generated by CodeSentinel.
It contains findings that did not meet the auto-PR threshold
(confidence < 0.80 or severity medium/low).

## Findings

| Severity | File | Lines | Type | Confidence | Evidence |
|---|---|---|---|---|---|
| 🟡 Medium | auth.go | 42–67 | security | 74% | `token := req.Header.Get(...)` |
| 🔵 Low | utils.go | 12 | quality | 61% | `var x = 0` |

## Unvalidated High/Critical (sandbox failed)

| Severity | File | Lines | Type | Reason |
|---|---|---|---|---|
| 🔴 High | db.go | 89–102 | logic | Fix candidate failed syntax check after 2 retries |

---
*Generated by CodeSentinel | {timestamp}*
*To promote any finding to an auto-PR, resolve the sandbox failure
or increase fix confidence by providing more context in the repo.*
```
