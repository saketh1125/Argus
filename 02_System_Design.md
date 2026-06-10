# System Design Document
## CodeSentinel — Autonomous Multi-Agent Bug Detection & Auto PR System
**Version:** 1.0  
**Status:** Locked  
**Date:** June 2026  

---

## 1. System Overview

CodeSentinel is a Go-based multi-agent pipeline that takes a GitHub repository URL and autonomously produces validated pull requests containing fixes for detected bugs, security vulnerabilities, and code quality issues.

The system is built on three core principles:
- **LLMs reason, tools only do I/O** — No logic lives in tools. All intelligence lives in agents.
- **Evidence before action** — No fix is generated without a cited line and explanation. No PR is raised without sandbox validation.
- **Fail loudly, recover gracefully** — Every agent stage has explicit fallback behavior. Nothing fails silently.

---

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        CodeSentinel                              │
│                                                                  │
│  INPUT: GitHub Repo URL                                          │
│         │                                                        │
│         ▼                                                        │
│  ┌─────────────────┐                                            │
│  │  PREPROCESSOR   │  Tree-sitter AST → Qdrant index           │
│  │  AGENT          │  Call graph construction                   │
│  │                 │  Content sanitization                      │
│  └────────┬────────┘                                            │
│           │                                                      │
│           ▼                                                      │
│  ┌─────────────────┐                                            │
│  │  LOCALIZER      │  Stage 1: Vector search (Qdrant)          │
│  │  AGENT          │  Stage 2: LLM re-rank (NIM)               │
│  └────────┬────────┘                                            │
│           │                                                      │
│    ┌──────┴──────┐                                              │
│    │  goroutines │  (up to 10 parallel)                        │
│    ▼             ▼                                              │
│  ┌──────┐    ┌──────┐                                          │
│  │ ANA  │    │ ANA  │  Structured evidence schema enforced     │
│  │ LYST │ .. │ LYST │  No evidence = output rejected           │
│  └──┬───┘    └──┬───┘                                          │
│     └─────┬─────┘                                              │
│           ▼                                                      │
│  ┌─────────────────┐                                            │
│  │  AGGREGATOR     │  Hash dedup, severity gate,               │
│  │                 │  call graph dependency pull               │
│  └────────┬────────┘                                            │
│           │                                                      │
│           ▼                                                      │
│  ┌─────────────────┐                                            │
│  │  FIX GENERATOR  │  3 candidates, LLM returns full func      │
│  │  AGENT          │  go-diff produces the diff                │
│  └────────┬────────┘                                            │
│           │                                                      │
│           ▼                                                      │
│  ┌─────────────────┐                                            │
│  │  VALIDATOR      │  E2B sandbox, syntax + tests              │
│  │  AGENT          │  Retry once on failure                    │
│  └────────┬────────┘                                            │
│           │                                                      │
│           ▼                                                      │
│  ┌─────────────────┐                                            │
│  │  PR AGENT       │  Branch → Commit → PR (High/Crit)        │
│  │                 │  Summary Issue (Med/Low)                  │
│  └────────┬────────┘                                            │
│           │                                                      │
│           ▼                                                      │
│  OUTPUT: Pull Requests + Summary Issue on GitHub                 │
│                                                                  │
│  ┌─────────────────┐                                            │
│  │  DASHBOARD      │  Go + Templ + HTMX                       │
│  │                 │  Real-time SSE updates                    │
│  └─────────────────┘                                            │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Technology Stack

| Component | Technology | Version | Justification |
|---|---|---|---|
| Language | Go | 1.24 | True goroutine parallelism, single binary, compile-time type safety |
| GitHub API | google/go-github | v68 | First-class Go client, full PR/Issue/Branch API |
| AST Parsing | tree-sitter Go bindings | Latest | 66+ languages, single unified interface |
| Vector Database | Qdrant | Local via Docker | Purpose-built for vector search, Go SDK |
| Embeddings (primary) | nomic-embed-text v1.5 | via Ollama | 274MB, 300MB VRAM, 8192 token context, free |
| Embeddings (fallback 1) | jina-embeddings-v2-base-code | Jina AI API | Code-specialized, free tier |
| Embeddings (fallback 2) | gemini-embedding-2 | Google AI API | Free via AI Studio, MTEB code 84.0 |
| Embeddings (fallback 3) | all-MiniLM-L6-v2 | CPU local | 46MB, last resort |
| Code Sandbox | E2B | Go SDK | 78ms spinup, Firecracker microVM, free $100 credit |
| Diff Generation | go-diff | v1.3 | Programmatic unified diff, no LLM diff format risk |
| Dashboard | Templ + HTMX | Latest | Single binary, no separate frontend server |
| Rate Limiter | Custom Go struct | — | Sliding window, 35 RPM ceiling |
| LLM API | Nvidia NIM | — | 40 RPM limit, all major models available |
| Config | godotenv | v1.5 | Simple .env loading |

---

## 4. Agent Designs

### 4.1 Preprocessor Agent

**Responsibility:** Transform a raw GitHub repo into a structured, searchable knowledge base ready for analysis.

**Steps:**
1. Clone repo via GitHub API (shallow clone, depth=1)
2. Walk file tree, filter by supported extensions
3. Sort files by last-modified timestamp, take top 50
4. For each file:
   a. Sanitize content (strip control chars, base64 blobs, invisible unicode)
   b. Parse with tree-sitter → extract function/class signatures
   c. Extract import statements → build import graph
   d. Extract function calls → build call graph
5. Embed all signatures via EmbeddingService → store in Qdrant
6. Persist call graph in memory (Go map, goroutine-safe via sync.RWMutex)

**Output:** Populated Qdrant index + in-memory call graph

**Failure modes:**
- Tree-sitter parse failure → skip file, log warning, continue
- Embedding failure → trigger fallback chain
- Clone failure → fatal, return error to user

---

### 4.2 Localizer Agent

**Responsibility:** Narrow the entire codebase down to the 3 most suspicious locations per analysis pass.

**Stage 1 — Vector Search:**
- Embed the analysis query ("potential bug locations in [file subset]")
- Query Qdrant for top-10 nearest signature embeddings
- Returns: list of (file, function_name, similarity_score)

**Stage 2 — LLM Re-rank:**
- Send top-10 candidates to NIM LLM
- LLM scores each by actual bug likelihood given the signature
- Returns: top-3 ranked locations with confidence scores

**Fallback:**
- If Stage 2 confidence < 0.50 for all candidates → send full file content to analyst

**Output:** List of LocationTarget structs (file, line_start, line_end, confidence)

---

### 4.3 Analyst Agent (Parallel)

**Responsibility:** Analyze a localized code chunk and return a structured Issue or null.

**Goroutine model:**
- Orchestrator spawns one goroutine per LocationTarget
- Each goroutine calls NIM independently (rate limiter shared)
- Results collected via buffered channel
- Goroutines have 30s timeout via context.WithTimeout

**Prompt structure (priority-ordered blocks):**
1. System prompt (cached — never re-sent after first call)
2. Structured output schema (mandatory fields)
3. Code chunk with call graph context
4. Analysis instruction

**Evidence enforcement:**
- Output parsed into Issue struct
- If Evidence field empty or < 10 chars → output rejected, goroutine returns nil
- Confidence < 0.40 → output rejected

**Output:** Issue struct or nil

---

### 4.4 Aggregator

**Responsibility:** Deduplicate, filter, and gate all analyst outputs before fix generation.

**Steps:**
1. Collect all non-nil Issues from analyst channel
2. Deduplicate: hash(file + strconv.Itoa(line_start) + bug_type) → seen map
3. Severity gate: only Critical and High issues with confidence ≥ 0.80 proceed to fix generation
4. For each surviving issue: call GetCallGraph(function) → attach dependent files to issue context
5. Medium/Low issues → collect into SummaryIssueBuffer

**Output:** []Issue (filtered) + SummaryIssueBuffer

---

### 4.5 Fix Generator Agent

**Responsibility:** Generate 3 validated fix candidates per issue.

**Steps:**
1. For each Issue, build context: original function + call graph dependencies + issue explanation
2. Call NIM 3 times with temperature variation (0.2, 0.5, 0.7) → 3 candidate rewrites
3. Each candidate: LLM returns complete rewritten function only (not a diff)
4. System calls go-diff to produce unified diff from original → rewritten
5. Candidates ranked by LLM-assigned confidence score
6. Highest-ranked candidate passed to Validator

**Concurrency:** Fix generation runs sequentially per issue (not parallel) to respect RPM budget

**Output:** Issue with Fix struct attached (3 candidates, ranked)

---

### 4.6 Validator Agent

**Responsibility:** Prove the fix works before it goes anywhere near GitHub.

**Steps:**
1. Spin E2B sandbox (Go SDK, 78ms median startup)
2. Upload repo + patched file to sandbox
3. Run syntax check:
   - Python: `python -m py_compile`
   - JS/TS: `node --check`
   - Go: `go build ./...`
   - Java: `javac`
   - Others: best-effort via tree-sitter re-parse
4. If syntax passes and tests exist → run `go test ./...` (or equivalent)
5. If no tests exist → generate minimal test for fixed function via LLM → run it
6. Pass → mark Issue.Validated = true
7. Fail → retry with second-ranked fix candidate
8. Fail again → mark Issue.Validated = false, exclude from PR creation, add to Summary Issue

**Timeout:** 60s per sandbox session

**Output:** Issue with Validated field set

---

### 4.7 PR Agent

**Responsibility:** Translate validated fixes into GitHub artifacts.

**For each Validated Issue:**
1. CreateBranch: `codesentinel/fix-{bug_type}-{file}-{line_start}`
2. CommitFile: apply diff, commit with message `[CodeSentinel] Fix {severity} {bug_type} in {file}:{line_start}`
3. CreatePullRequest with structured body (see PR body template below)

**For SummaryIssueBuffer:**
1. CreateIssue: single issue titled `[CodeSentinel] Analysis Summary — {N} findings`
2. Body: markdown table of all Medium/Low findings + unvalidated High/Critical

**PR Body Template:**
```
## CodeSentinel Analysis Report

**Type:** {bug_type}
**Severity:** {severity}
**Confidence:** {confidence * 100}%
**Validation:** ✅ Passed E2B sandbox

### Evidence
File: `{file}` | Lines: {line_start}–{line_end}

> {evidence}

### Why This Is a Bug
{explanation}

### Fix Applied
{diff}

---
*Generated by CodeSentinel — Autonomous Multi-Agent Bug Detection System*
```

---

## 5. Data Flow

```
GitHub URL
    │
    ▼ (HTTP clone)
Raw Files
    │
    ▼ (tree-sitter)
AST Signatures + Call Graph
    │
    ▼ (nomic-embed-text via Ollama)
Vector Embeddings
    │
    ▼ (Qdrant upsert)
Searchable Index
    │
    ▼ (Qdrant query → NIM rerank)
LocationTargets []{file, line, confidence}
    │
    ▼ (goroutines → NIM)
Issues []{file, line, evidence, severity, confidence}
    │
    ▼ (hash dedup + gate)
FilteredIssues
    │
    ▼ (NIM × 3 candidates)
Issues with Fix{rewritten_func, diff, candidates}
    │
    ▼ (E2B sandbox)
Issues with Validated=true/false
    │
    ├─► Validated Critical/High → GitHub Pull Requests
    └─► All others → GitHub Summary Issue
```

---

## 6. Concurrency Model

```go
// Orchestrator spawns analyst goroutines
resultCh := make(chan *Issue, len(locations))
var wg sync.WaitGroup

for _, loc := range locations {
    wg.Add(1)
    go func(loc LocationTarget) {
        defer wg.Done()
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        issue := analystAgent.Analyze(ctx, loc)
        resultCh <- issue  // nil if rejected
    }(loc)
}

// Close channel when all goroutines finish
go func() {
    wg.Wait()
    close(resultCh)
}()

// Collect results
var issues []*Issue
for issue := range resultCh {
    if issue != nil {
        issues = append(issues, issue)
    }
}
```

**Key rules:**
- All shared state accessed via `sync.RWMutex` or channels only
- No global variables mutated after initialization
- Rate limiter is the only shared resource — uses `sync.Mutex` internally
- E2B sandbox instances are never shared between goroutines

---

## 7. Rate Limiter Design

```go
type RateLimiter struct {
    maxRPM int
    calls  []time.Time
    mu     sync.Mutex
}

func NewRateLimiter(maxRPM int) *RateLimiter {
    return &RateLimiter{maxRPM: maxRPM}
}

func (r *RateLimiter) Wait() {
    r.mu.Lock()
    defer r.mu.Unlock()

    now := time.Now()
    window := now.Add(-60 * time.Second)

    // Evict calls outside the window
    valid := r.calls[:0]
    for _, t := range r.calls {
        if t.After(window) {
            valid = append(valid, t)
        }
    }
    r.calls = valid

    // If at ceiling, sleep until oldest call expires
    if len(r.calls) >= r.maxRPM {
        sleepDuration := r.calls[0].Add(60*time.Second).Sub(now) + 100*time.Millisecond
        time.Sleep(sleepDuration)
    }

    r.calls = append(r.calls, time.Now())
}

// Global singleton — instantiated once in main()
var Limiter = NewRateLimiter(35)
```

---

## 8. Embedding Service Design

```go
type EmbeddingMode string

const (
    ModeLocal     EmbeddingMode = "local_ollama"
    ModeJinaAPI   EmbeddingMode = "jina_api"
    ModeGeminiAPI EmbeddingMode = "gemini_api"
    ModeMiniLM    EmbeddingMode = "minilm_cpu"
)

type EmbeddingService struct {
    mode   EmbeddingMode
    client interface{} // concrete client per mode
}

func NewEmbeddingService() *EmbeddingService {
    // Auto-detect at startup
    // Priority: local → jina api → gemini api → minilm cpu
}

func (e *EmbeddingService) Embed(text string) ([]float32, error) {
    // Delegates to mode-specific implementation
}
```

**Startup detection order:**
1. Attempt `ollama.Embed("nomic-embed-text", "test")` → success: use local
2. Check `JINA_API_KEY` env var + ping → success: use Jina API
3. Check `GEMINI_API_KEY` env var + ping → success: use Gemini API
4. Fallback: load `all-MiniLM-L6-v2` via sentence-transformers Python subprocess

---

## 9. Security Design

### Prompt Injection Prevention

```go
func SanitizeForLLM(content string) string {
    // 1. Strip C0 and C1 control characters (except \n \t)
    re1 := regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]`)
    content = re1.ReplaceAllString(content, "")

    // 2. Remove base64 blobs > 200 chars (not legitimate code)
    re2 := regexp.MustCompile(`[A-Za-z0-9+/]{200,}={0,2}`)
    content = re2.ReplaceAllString(content, "[REDACTED_BLOB]")

    // 3. Strip invisible unicode
    content = strings.ReplaceAll(content, "\u200b", "")
    content = strings.ReplaceAll(content, "\u2060", "")
    content = strings.ReplaceAll(content, "\ufeff", "")

    return content
}
```

**Applied:** To every file before it enters any LLM context. No exceptions.

### E2B Sandbox Isolation
- All patch execution happens inside Firecracker microVM
- No host filesystem access from sandbox
- Sandbox destroyed after validation (never reused)
- Network egress disabled in sandbox (no exfiltration risk)

### GitHub Token Scoping
- Token requires only: `repo` (read) + `pull_requests` (write) + `issues` (write)
- No admin, no delete, no org-level access

---

## 10. Dashboard Design

**Technology:** Go (Templ templates + HTMX server-sent events)

**Served at:** `localhost:8080` during pipeline execution

**Panels:**
```
┌─────────────────────────────────────────────────┐
│  CodeSentinel — Live Pipeline                    │
├──────────────┬──────────────────────────────────┤
│ Stage        │ ████████████░░░░ 75%             │
│ Files Done   │ 42 / 50                          │
│ Issues Found │ 7 (3 Critical, 4 High)           │
│ PRs Raised   │ 5                                │
│ Validated    │ 5 / 7                            │
│ RPM Usage    │ 28 / 35                          │
├──────────────┴──────────────────────────────────┤
│ Active Agents                                    │
│  Analyst-1   [████████] analyzing auth.go       │
│  Analyst-2   [████░░░░] analyzing db.go         │
│  Analyst-3   [IDLE]                             │
├─────────────────────────────────────────────────┤
│ Issues Detected                                  │
│  🔴 SQL injection risk — db.go:142 (conf: 94%) │
│  🔴 Null deref — handler.go:67 (conf: 91%)     │
│  🟠 Hardcoded secret — config.go:23 (conf: 88%)│
└─────────────────────────────────────────────────┘
```

**Update mechanism:** Go `http.Flusher` + HTMX `hx-ext="sse"` — no WebSocket, no JS framework, no separate process.

---

## 11. Project Structure

```
codesentinel/
├── main.go                    # Entry point, pipeline orchestration
├── go.mod
├── go.sum
├── .env.example
│
├── agents/
│   ├── preprocessor.go        # Repo clone, tree-sitter, sanitize, embed
│   ├── localizer.go           # Two-stage vector + LLM retrieval
│   ├── analyst.go             # Parallel bug detection goroutines
│   ├── aggregator.go          # Dedup, gate, call graph enrichment
│   ├── fix_generator.go       # 3-candidate fix generation
│   ├── validator.go           # E2B sandbox validation
│   └── pr_agent.go            # GitHub branch, commit, PR, issue
│
├── tools/
│   ├── repo.go                # build_repo_index, list_files, get_signatures
│   ├── navigation.go          # search_symbol, get_code_chunk, get_call_graph
│   ├── patch.go               # rewrite_function, check_syntax
│   └── github.go              # create_branch, commit_file, create_pr, create_issue
│
├── services/
│   ├── embedding.go           # EmbeddingService with fallback chain
│   ├── rate_limiter.go        # RateLimiter singleton
│   ├── nim_client.go          # NIM API wrapper
│   └── sanitizer.go           # SanitizeForLLM
│
├── models/
│   └── types.go               # Issue, Fix, LocationTarget, CallGraph structs
│
├── dashboard/
│   ├── server.go              # HTTP server, SSE handler
│   └── templates/
│       └── index.templ        # Templ dashboard template
│
└── config/
    └── config.go              # Config loading via godotenv
```

---

## 12. Deployment

**Build:**
```bash
CGO_ENABLED=1 go build -o codesentinel ./main.go
```

CGO required for tree-sitter Go bindings.

**Run:**
```bash
GITHUB_TOKEN=xxx \
NIM_API_KEY=xxx \
JINA_API_KEY=xxx \     # optional
GEMINI_API_KEY=xxx \   # optional
E2B_API_KEY=xxx \
./codesentinel --repo https://github.com/owner/repo
```

**Output:** Single binary ~15–30MB. No Docker required. No venv. No runtime dependencies except Ollama (optional, for local embeddings).
