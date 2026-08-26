# CodeSentinel — Frontend Context Anchor

This file captures everything needed to build/refactor the GUI without re-reading
the whole codebase. It is the single source of truth for the frontend work.

## 1. What the project is

**CodeSentinel** (package path `github.com/saketh1125/argus`) — a Go 1.24 single
binary that runs an *autonomous multi-agent bug-detection + auto-PR* pipeline on
a GitHub repo or local path. Every external integration (LLM, embeddings, vector
DB, sandbox, GitHub) degrades to a deterministic mock when its key/service is
absent, so it runs end-to-end with zero API keys.

Entry point: `main.go`. Pipeline stages (in order), each an agent in `agents/`:

1. `Preprocessor`  (StageIngest)    — clone → parse → embed → vector index
2. `Localizer`     (StageLocalize)  — vector search + LLM rerank → targets
3. `Analyst ×N`    (StageAnalyze)   — parallel goroutines → evidence-enforced Issues
4. `Aggregator`    (StageAggregate) — dedup + severity/confidence gate
5. `FixGenerator`  (StageFix)       — 3 candidate rewrites → unified diff → rank
6. `Validator`    (StageValidate)  — apply patch → sandbox syntax/test check
7. `PRAgent`      (StagePR)        — branch/commit/PR (Crit/High), summary issue (rest)
8. `Complete`     (StageDone)

## 2. The existing frontend (what we are refactoring)

There are TWO existing frontends; the **web dashboard is the one to refactor**.

### 2a. Web dashboard — `dashboard/` package (THE refactor target)

- `dashboard.go` — `Dashboard` type implements `models.Reporter`. Holds `state`
  guarded by `sync.RWMutex`. Each Reporter method mutates state then calls
  `broadcast()`. State fields:
  - `stage string`, `stagePct int` (0–100)
  - `filesDone`, `filesTotal int`
  - `issuesFound`, `criticalCount`, `highCount int`
  - `recentIssues []recentIssue` (newest last, capped `maxRecentIssues=12`).
    `recentIssue{Icon, BugType, File, Line, Confidence}` — Icon is 🔴🟠🟡⚪, Confidence is int percent.
  - `prsRaised int`
  - `validatedDone`, `validatedTotal int`
  - `rpmCurrent`, `rpmMax int`
  - `agentOrder []string` then `agents map[string]string` (insertion-ordered)
  - `logs []string` (rolling, oldest first, capped `maxLogLines=50`)
- `server.go` — `httpServer` with `net/http` + SSE. Routes: `/` (index/launcher),
  `/start` (POST launcher form), `/health` (JSON qdrant/ollama probes), `/events`
  (SSE stream). `broadcast()` renders panel once, fans out non-blocking to each
  buffered client channel (`clientBuffer=8`).
- `templates.go` — `html/template` with three templates:
  - `page` — outer HTML doc, dark theme (#0d1117), loads HTMX from CDN, vanilla
    `EventSource("/events")` that swaps `#app` innerHTML on each SSE message.
  - `panel` — the live fragment (4 cards in a grid: Pipeline, Active Agents,
    Issues Detected, Log). Both rendered inline on first load AND as each SSE payload.
  - `launcher-page` — pre-run config form (POSTed to `/start`).
  - `panelView` is the render-ready snapshot struct handed to the template.
- `launcher.go` — `LaunchConfig` struct, `ListenForLaunch()`, `handleStart`
  (parses form → sends one LaunchConfig on the channel → redirect to `/`),
  `handleHealth` (probes Qdrant TCP :6333 and Ollama HTTP :11434).
  `LaunchConfig` fields: `RepoURL, GitHubToken, NIMAPIKey, E2BAPIKey, OllamaHost,
  QdrantHost, QdrantPort int, EmbedModel, ForceMock bool, MaxFiles int,
  ConfidenceGate float64`.
- `dashboard_test.go` — tests assert: state fields update correctly, `/`
  returns HTML containing `pageTitle` ("CodeSentinel — Live Pipeline") and
  "/events", URL is `http://localhost:<port>` with port != 0, recent issues cap,
  nil issue ignored. **These must keep passing.** Do NOT rename `pageTitle`.

### 2b. Tkinter control panel — `control_panel.py` (NOT refactor target)

A standalone Python Tkinter desktop app for start/stop of Qdrant/Ollama/CodeSentinel
and editing `.env`. It launches `go run . --web`. Keep around; not part of the
Go web frontend. Out of scope.

## 3. How the pipeline drives the dashboard

`main.go` constructs `dashboard.New(cfg.DashboardPort)`, starts it, and uses it
as the `models.Reporter` (`rep = dash`). In `--web` mode it calls
`dash.ListenForLaunch()` and waits on the channel before running the pipeline.
`applyLaunchConfig` merges the browser form into `cfg`. A goroutine `pumpRPM`
calls `rep.SetRPM(cur,max)` every 500ms from the rate limiter snapshot.

Reporter interface (`models/reporter.go`), all thread-safe:
- `SetStage(stage string, pct int)`
- `SetFilesProgress(done, total int)`
- `AddIssue(issue *Issue)`
- `SetPRsRaised(n int)`
- `SetValidated(done, total int)`
- `SetRPM(current, max int)`
- `SetAgentStatus(agent, status string)`
- `Log(format string, args ...any)`

Stage name constants (`models/reporter.go`):
`StageIngest="Preprocessing"`, `StageLocalize="Localizing"`, `StageAnalyze="Analyzing"`,
`StageAggregate="Aggregating"`, `StageFix="Generating Fixes"`,
`StageValidate="Validating"`, `StagePR="Raising PRs"`, `StageDone="Complete"`.

## 4. Key data types (the GUI renders these)

From `models/types.go`:

- `Issue{ID, File, FunctionName, LineStart, LineEnd int, BugType, Severity,
  Evidence, Explanation, Confidence float64, DependentFiles, Fix *Fix, Validated bool,
  ValidationLog, Demoted bool}`. Severity is `Critical|High|Medium|Low`.
- `Fix{OriginalFunction, Candidates []FixCandidate, Selected *FixCandidate}`.
- `FixCandidate{RewrittenFunction, Diff, Confidence float64, Temperature float64, Rationale}`.
- `PRResult{Number int, URL, Branch, Issue *Issue}`.
- `RunReport{RepoURL, FilesAnalyzed int, IssuesFound int, PRsRaised []PRResult,
  SummaryIssue string, DemotedIssues []*Issue, DurationMillis int64, Mode}` —
  `Mode` is "live"|"mock"|"mixed".

Note: the dashboard currently does NOT receive the detailed Issue list, PR URLs,
or the final `RunReport`. It only gets the trimmed `recentIssue` view and counts.
This is a refactor opportunity (see §7).

## 5. Config (`config/config.go`)

`Config` struct fields the GUI cares about: `RepoURL, GitHubToken, NIMAPIKey,
NIMBaseURL, NIMModel, JinaAPIKey, GeminiAPIKey, E2BAPIKey, OllamaHost, QdrantHost,
QdrantPort int, EmbedModel, MaxRPM, MaxFiles, AnalystParallel, FixCandidates,
ConfidenceGate float64, LocalizeFallback, SandboxTimeout, DashboardPort int,
ForceMock bool, NoDashboard bool`. Defaults: MaxRPM=35, MaxFiles=50,
AnalystParallel=10, FixCandidates=3, ConfidenceGate=0.80, DashboardPort=8080.

## 6. Constraints for the refactor

- **Stdlib only.** No external Go modules (`go.mod` has zero requires). No CGO,
  no codegen, no JS build step. The dashboard must remain a true single binary.
  HTMX is loaded from CDN; otherwise vanilla JS. Chrome-only is fine but keep it
  reasonable. CSS is inline `<style>` in the template string.
- **Keep `pageTitle` constant** (`"CodeSentinel — Live Pipeline"`) — tested.
- **Keep `/`, `/start`, `/events`, `/health` routes.** Keep `LaunchConfig` shape
  (main.go depends on it). Keep `ListenForLaunch` / `applyLaunchConfig` contract.
- **SSE contract:** `broadcast()` renders the panel fragment and SSE-framing in
  `writeSSE` is line-based (`data: …\n` per line, `\n\n` terminator). The browser
  swaps `#app` innerHTML with `e.data`. Any new fragment rendering must stay
  SSE-safe (multiline is handled by writeSSE, but keep one root element).
- **Thread-safety:** Reporter methods are called from many analyst goroutines;
  keep `mu sync.RWMutex` discipline. `broadcast()` must not hold the client lock
  while rendering.
- **Build green:** `go build ./...` and `go test ./...` must pass. The dashboard
  test file is the gate.

## 7. What to build (the great frontend refactor)

Goal: a modern, polished, information-dense real-time dashboard that surfaces the
full pipeline state — not just the 4 coarse cards of today. Keep stdlib-only and
the SSE architecture. Specifically:

1. **Refactor templates.go** into a cohesive design system: a CSS design-token
   block, a responsive shell (header + stage rail + main grid + log drawer),
   and split panels into reusable sub-templates. Add an icon set (inline SVG, no
   external deps) and severity color tokens.
2. **Enrich the panel** within the existing Reporter contract: add a pipeline
   stage timeline (all 8 stages with the current one active and pct), KPI tiles
   (Issues, Critical, High, PRs, Files, RPM with sparkline-ish bar), a richer
   issues table (sort by severity, show evidence/fix badge), per-agent cards with
   pulse animation, and a collapsible autoscrolling log with level coloring.
   Mark "Done" with a prominent complete state.
3. **Keep the launcher form** but restyle it to match the design system; keep all
   existing form field `name` attributes so `handleStart` keeps working. Keep the
   `/health` badges but restyle.
4. Use a single `page` template, one `panel` template, one `launcher-page`
   template (same three `{{define}}` names). The vanilla EventSource swap stays.
5. Keep build green and tests passing. No new deps.

Stretch (do only after the above is solid): optionally extend `Dashboard` state +
Reporter with a method to expose PR URLs (e.g. add a `PRs []PRRow` to state and a
`AddPR` or reuse `SetPRsRaised` to also send URLs) WITHOUT breaking the Reporter
interface contract or the existing test. If it risks the test/build, skip it.

## 8. Verification

```bash
go build ./...                 # must compile
go test ./...                  # all tests pass, incl. dashboard_test.go
FORCE_MOCK=true go run . --web # manual smoke: launcher → live panel → complete
```
curl smoke: `curl -s localhost:8080/ | grep "CodeSentinel — Live Pipeline"`
and `curl -N localhost:8080/events` should stream `data:` frames.
