# Unit 03 — Rich issues table, agent cards, and log drawer

Owner: executing model. Prerequisite: units `00`, `01`, `02` merged & green.
Orchestrator: do not run this yourself.

## Goal

Fill in the three detail panels left as placeholders in unit 02's lower grid:
(1) a severity-coded, sortable-looking issues table that renders the existing
`RecentIssues` data well; (2) per-agent live cards with a pulse; (3) a polished
autoscrolling log drawer with level coloring. All using the existing `panelView`
fields — no Go state changes. This unit is purely `panelTemplate` continuation.

## Files you may edit

- `dashboard/templates.go` — **only** the `panelTemplate` const. You may extend
  the inline `<style>` that unit 02 placed inside the fragment, and the inline
  `<script>` if present. Do NOT touch `pageTemplate`, `launcherTemplate`,
  `pageTitle`, or any `.go` file.

## Hard constraints

- Same root-element and SSE-safety rules as unit 02 (one `.panel` root, inline
  `<style>`/`<script>` idempotent on re-render).
- Use existing fields only. `RecentIssues []recentIssue` with `.Icon, .BugType,
  .File, .Line, .Confidence`. Note: `.Confidence` is already an int percent
  (0–100). `.Icon` is one of 🔴🟠🟡⚪ set by Go — keep using it OR derive a CSS
  class from the severity; the icon is your choice but keep it readable. There is
  NO `Severity` field on `recentIssue`, so don't try to template-grep severity
  from the icon; instead color the row by the icon via CSS attribute matching is
  not possible — so derive the row's severity class from the BugType? No — keep
  it simple: color the row's left border by mapping the emoji is hacky. **Use the
  icon glyph for the leading marker and a neutral row** — or, optionally, pass
  severity through by having unit 04 add it (do NOT rely on that here). Acceptable
  approach: show the icon + bugType badge; color the file:line in `--fg-dim`;
  show `conf {{.Confidence}}%` right-aligned with a `.bar` sized to
  `width:{{.Confidence}}%`. That's enough and looks great.
- `Agents []agentLine` with `.Name, .Status`. `.Logs []string`.
- No new deps. No Go comments. No `<script src>`.

## What to build

### A. Issues table

Replace the unit-02 placeholder "Issues detected" card body with a real table:

- A `.card .issues-card wide` so it can take more width; consider promoting it to
  `grid-column:1/-1` (full width) and making the agents + log a `.grid-2` below
  it. Layout call: recommend
  ```
  Lower grid (overrides unit 02's grid-3 if needed via inline style):
  Row 1: Issues (full width, grid-column:1/-1)
  Row 2: .grid-2 → Agents (left) | Log (right, but Logs is the wide one...)
  ```
  Better: Issues full-width on top, Agents and Logs in a `.grid-2` below where
  Agents is 1fr and Log is 1.6fr. Pick what reads best and keep it responsive.
- Table (`<table class="issues">`):
  - `<thead>` row: Severity | Type | File | Line | Confidence.
  - `<tbody>` from `{{range .RecentIssues}}`:
    - Severity cell: the `.Icon` glyph + an `<span class="sev-…">` — since we
      can't get severity text, map by icon in the TEMPLATE using `{{if eq .Icon "🔴"}}crit{{else if eq .Icon "🟠"}}high{{else if eq .Icon "🟡"}}med{{else}}low{{end}}`
      to derive a class. Apply that class to the `<tr class="row {{ sevClass }}">`
      so the left border (`border-left:3px solid var(--sev-…)` via a CSS rule per
      class) lights the row.
    - Type: `<span class="badge">{{.BugType}}</span>` (snake_case stays as-is;
      it's a defined bug-type token). Mono font.
    - File: `<span class="mono ellipsis" title="{{.File}}">{{.File}}</span>` so
      long paths truncate with a tooltip.
    - Line: `{{.Line}}` (`.num`).
    - Confidence: a `<div class="conf">` with a `.bar` `width:{{.Confidence}}%`
      and the numeric `{{.Confidence}}%` in `.num .muted`. Color: `.bar.warn` if
      Confidence < 60, `.bar.crit` if < 40, else default ok/green. Derive class
      in template with `{{if lt .Confidence 40}}crit{{else if lt .Confidence 60}}warn{{else}}ok{{end}}`.
  - Empty state: `{{else}}<tr><td colspan="5" class="muted">No issues detected yet</td></tr>{{end}}`.
- A small footer row in the card showing `{{.IssuesFound}} total · {{.CriticalCount}} critical · {{.HighCount}} high`
  with the crit/high numbers colored.

Make the table dense (small font, tight padding, `border-bottom` on `<td>`), and
zebra-stripe rows via `:nth-child(even)` for readability.

### B. Agent cards

Replace the unit-02 placeholder "Active agents" body with a list of per-agent
cards (not a flat `<li>` list):

- A `.agents` container. For each `{{range .Agents}}` render a `.agent-card`:
  - A small status dot. Because `.Status` is free text (e.g. "analyzing
    auth.go"), derive "active vs idle" by whether Status is non-empty and not
    starting with "idle"/"done". Use a template `{{if and . (not (or (hasPrefix .Status "idle") (hasPrefix .Status "done")))}}active{{else}}idle{{end}}`
    — Go templates have `hasPrefix`? No built-in. Use `{{if eq (slice .Status 0 4) "idle"}}…{{end}}`
    is fragile. **Simplest**: treat any non-empty status as "active" and color
    the dot `--accent` with the pulse animation; empty status = grey idle. Add a
    CSS `.agent-card.active .dot { animation: pulse … }`.
  - `.Name` in `.mono` bold (e.g. "Analyst-3", "FixGenerator", "PRAgent",
    "Localizer", "Validator").
  - `.Status` in `--fg-dim`, wrapped with `word-break:break-word`.
- Empty: `{{else}}<div class="muted">No active agents</div>{{end}}`.
- If there are MANY agents (parallel analysts can be 10), constrain the container
  to a max-height with `scroll-y` so the card doesn't grow unbounded. Cap at
  ~260px height, 2-col flow where possible.

### C. Log drawer

Replace the unit-02 Log card body with a polished drawer:

- `<div class="log scroll-y">` (max-height ~220px, mono, `.78rem`).
- For each `{{range .Logs}}` classify the line by prefix using Go template
  conditionals into a level for coloring:
  - `.err` if the line contains "error" or "fail" or "panic" or "demote" — but
    template `contains` isn't built-in. Use startswith heuristics: lines that
    start with `[` and contain "ERROR" are unlikely. **Pragmatic**: color lines
    that start with "pragent:" with the `--warn`, lines containing "skipping"
    with `--fg-faint`. Given Go template limits, do a coarse classification:
    ```
    {{ $lvl := "info" }}
    {{if hasPrefix . "ERROR"}}{{ $lvl := "err" }}{{end}}   # won't work — $lvl scoping
    ```
    The scoping problem makes this hard in one pass. **Easiest robust approach**:
    render the raw line inside a `<div class="line">{{.}}</div>` and let a tiny
    inline `<script>` at the bottom of the fragment add a `warn`/`err` class by
    inspecting `textContent` (regex `/error|fail|panic|demote|skip/i`). The
    script is idempotent (operates on the freshly-rendered `.log .line` nodes
    each swap). Keep it short.
  - Each line: `<div class="line">` with optional leading timestamp removed
    (they already include context). Keep `<br>`-free `white-space:pre-wrap`.
- Add a header row to the log card with a small "clear" affordance is NOT needed
  (logs are server-owned; don't add fake controls).
- Autoscroll: add to the inline `<script>` a line that sets
  `.log.scrollTop = .log.scrollHeight` after each render so the newest line is
  visible. Idempotent and safe.

### D. Keep the inline `<script>` tidy

If unit 02 already added an inline `<script>` (e.g. for the RPM bar), extend it
with the log-classification + autoscroll rather than adding a second `<script>`.
Keep the whole script idempotent (it re-runs every SSE frame). Wrap in an IIFE
or a null-guard so re-runs don't stack handlers.

## What NOT to do

- Don't add `Severity` to `recentIssue` (unit 04's job; do not depend on it).
- Don't touch `pageTemplate`, `launcherTemplate`, or any `.go` file.
- No new Go comments. No `<script src>`.
- Don't break unit 02's root `.panel` or its stage rail / KPI row.

## Verification

1. `cd /home/saketh/Code-Review && go build ./...` → exit 0.
2. `cd /home/saketh/Code-Review && go test ./dashboard/` → `ok`, exit 0. The
   panel must still render "Analyzing" for StageAnalyze (the test greps the full
   page HTML which includes the inline panel) — keep that label in the rail.
3. Static checks:
   ```
   grep -c 'class="panel"' dashboard/templates.go          # 1
   grep -ce 'RecentIssues\|Agents\|Logs' dashboard/templates.go   # all three used
   grep -c '"Analyzing"' dashboard/templates.go            # >=1 (rail, from unit 02, must remain)
   grep -c '<table' dashboard/templates.go                 # >=1 (issues table)
   ```
4. `go vet ./dashboard/` → clean.
5. Manual smoke: `FORCE_MOCK=true go run . --web`, confirm the issues table fills
   with rows as the mock analysts surface bugs (the mock analyst heuristic plants
   findings in `testdata/sample-repo`), agent cards appear/pulse, and the log
   autoscrolls with colored warn/err lines. Report what you see or
   "skipped — no runnable env".

## Report back

- Diff of `panelTemplate`.
- The 4 grep counts.
- The exact inline `<script>` you ended up with (paste the block).
- Smoke result or skipped.
- Anything in unit 02's fragment that you had to adjust (and why) — note it so
  the orchestrator can reconcile.
