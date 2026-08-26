# Unit 02 — Panel layout, stage timeline & KPI tiles

Owner: executing model. Prerequisites: units `00_design_system.md` and
`01_launcher.md` are merged & green. Orchestrator: do not run this yourself.

## Goal

Restructure the **live panel fragment** (the `panelTemplate` `{{define "panel"}}`
string rendered both inline on first load and as every SSE payload). Replace the
flat 4-card grid with a real dashboard layout: a stage timeline rail at top, a
row of KPI tiles, and the lower grid that units 03 will fill with the issues
table, agent cards, and log drawer. This unit ships the **shell and the KPI/timeline
rendering**; unit 03 fills the three detail panels. Keep the existing `panelView`
struct fields (do NOT change Go state in this unit — render what's already there).

## Files you may edit

- `dashboard/templates.go` — **only** the `panelTemplate` const. Do NOT touch
  `pageTemplate`, `launcherTemplate`, `pageTitle`, `panelView`, `snapshot`,
  `renderPanel`, `renderPage`, or any other `.go` file.

## Hard constraints

- Keep exactly one root element wrapping the whole fragment (the SSE `writeSSE`
  framing is line-safe, and `#app` innerHTML swap expects one fragment; a single
  `<div class="panel">…</div>` root is correct).
- Keep using ONLY the existing `panelView` fields (see `FRONTEND_CONTEXT.md` §2a
  and `templates.go`): `Stage, StagePct, FilesDone, FilesTotal, IssuesFound,
  CriticalCount, HighCount, RecentIssues, PRsRaised, ValidatedDone, ValidatedTotal,
  RPMCurrent, RPMMax, Agents []agentLine, Logs []string, Done bool`. The
  `recentIssue` fields: `Icon, BugType, File, Line, Confidence`. `agentLine`:
  `Name, Status`. **Do not add new fields.** (Unit 04 may; this one must not.)
- Templates are Go `html/template` inside a backtick string — escape carefully.
  Don't break existing template syntax. Keep `{{if .Done}}…{{end}}`,
  `{{range .Agents}}…{{else}}…{{end}}`, etc. style.
- Utility classes from unit 00 (`.card`, `.grid`, `.grid-2/3/4`, `.bar`,
  `.badge`, `.sev-*`, `.muted`, `.mono`, `.num`, `.scroll-y`) are available in
  the page `<style>`; reuse them. You MAY add panel-only CSS by appending to the
  `pageTemplate` `<style>` — NO, you may not edit `pageTemplate` here. Instead,
  put any panel-only CSS into a `<style>` block INSIDE the `panelTemplate`
  fragment (inline `<style>` inside an SSE fragment is fine; it's idempotent on
  repeated swaps and small). Keep it minimal.
- Stdlib only; no `<script src>`; inline JS only if needed (a tiny
  `<script>` in the panel is allowed but keep it idempotent because the panel is
  re-rendered on every SSE frame — any script re-runs each swap, so make it
  safe/no-op on rerun or avoid it entirely).

## What to build

### A. Panel root + Done banner

- Root: `<div class="panel"> … </div>`.
- If `{{.Done}}`, render a prominent `.done-banner` (keep the existing semantic,
  restyle): full-width, `--ok-bg`/`--ok` border, an inline-SVG check glyph, text
  "Analysis complete — {{.PRsRaised}} PR(s) raised · {{.IssuesFound}} issues
  found · {{.ValidatedDone}} validated". Place at top of `.panel`.

### B. Stage timeline rail

Render a horizontal rail of all 8 stages in fixed order with the current one
active. Because `panelView` only carries the current `Stage` string + `StagePct`,
compute "active" by string equality in the template (use Go template `eq`). The
8 stages in order (names must match `models/reporter.go` constants exactly):

1. Preprocessing
2. Localizing
3. Analyzing
4. Aggregating
5. Generating Fixes
6. Validating
7. Raising PRs
8. Complete

Implementation: a `<ol class="rail">` of `<li>` items. Each `<li>` has:
- a node dot (`.node`),
- the stage label (`.lbl`, `.mono`, tiny),
- an optional connector line between items (use `::before` on the `<li>` except
  the first).

Add CSS classes via Go template `{{if eq .Stage "Preprocessing"}}active{{end}}`
and a special `{{if eq .Stage "Complete"}}done{{end}}` for the last node. The
active node uses `--accent` with a soft pulse animation (CSS `@keyframes pulse`
box-shadow, 1.4s infinite). Completed stages before the active one should be
filled with `--ok` — but since we only know the current stage, approximate
"completed = stages before the active index in the fixed order". You can do
this with a chain of `{{if or (eq .Stage "Validating") (eq .Stage "Raising PRs")
(eq .Stage "Complete")}}completed{{end}}` per `<li>`. Keep it readable.

Under the rail, a single overall progress bar: `.bar` with `> span` width
`{{.StagePct}}%` and a numeric `{{.StagePct}}%` to the right (`.num`). Use
`.bar.warn` when `StagePct` is between 1–99 and not Done (it's in-progress); use
default (green) only when Done. (Optional polish; not required.)

### C. KPI tile row

A `.grid-4` (collapses to 2 then 1 on narrow screens — handled by the utility
class from unit 00) with 6 tiles:

1. **Issues** — big `.num` `{{.IssuesFound}}`, subline `{{.CriticalCount}} crit
   · {{.HighCount}} high` (color crit/high via `.sev-crit`/`.sev-high`).
2. **PRs raised** — `{{.PRsRaised}}`, subline "pull requests".
3. **Validated** — `{{.ValidatedDone}}/{{.ValidatedTotal}}`, subline "fixes
   sandbox-checked".
4. **Files** — `{{.FilesDone}}/{{.FilesTotal}}`, subline "indexed".
5. **RPM** — `{{.RPMCurrent}}/{{.RPMMax}}`, plus a tiny inline `.bar` whose width
   is `{{if gt .RPMMax 0}}{{(printf "%d" (mul .RPMCurrent (div 100 .RPMMax)))}}{{else}}0{{end}}%`
   — NOTE Go templates have no `mul`/`div`; instead do this in CSS by emitting
   `style="width:{{.RPMCurrent}}%"` is WRONG (max could be 35). **Simplest safe
   approach**: emit two data attributes `data-cur="{{.RPMCurrent}}"` and
   `data-max="{{.RPMMax}}"` on the bar's parent, and add a tiny inline
   `<script>` at the bottom of the panel that sets the width. Because the panel
   re-renders each frame, the script re-runs and recomputes — make it idempotent
   (it just reads the latest `data-*` and sets `style.width`). Apply a
   `.bar.warn` class if `cur/max > 0.8` and `.bar.crit` if `> 0.95`. Color the
   numeric `{{.RPMCurrent}}` red-ish via the same check at render time using
   `{{if and (gt .RPMMax 0) (gt (printf "%d" .RPMCurrent) (printf "%d" .RPMMax))}}…{{end}}`
   is overkill — keep it simple: just render the numbers and let the JS bar do
   the color.
6. **Stage** — `{{.Stage}}` as a label + the `{{.StagePct}}%` again (or reuse the
   rail's bar; if redundant, drop this tile and make it a 5-tile `.grid` — your
   call, but keep Issues/PRs/Validated/Files/RPM minimum).

Each tile is a `.card .kpi` with a small inline-SVG glyph (top-left, muted), the
big number centered/left, and the subline below. Keep SVGs inline and tiny
(16px, stroke currentColor, `var(--fg-dim)`).

### D. Lower detail grid (shell only)

Below the KPI row, a `.grid-3` (or `grid` with a custom template that puts the
log full-width) that units 03 will populate. For THIS unit, render the three
section **cards with their titles and a "Loading…" placeholder body** so the
layout is visible before unit 03:

- `.card` titled "Issues detected" → body `<p class="muted">awaiting data…</p>`
  (unit 03 will replace).
- `.card` titled "Active agents" → body `<ul>…placeholder…</ul>` rendering the
  existing `{{range .Agents}}<li>{{.Name}}: {{.Status}}</li>{{else}}<li class="muted">No active agents</li>{{end}}`
  (keep this real so the agents still show live; unit 03 just restyles).
- `.card` titled "Log" (full-width, `grid-column: 1 / -1` via an inline
  `style="grid-column:1/-1"`) → body `<div class="log scroll-y">…</div>` rendering
  `{{range .Logs}}<div class="mono">{{.}}</div>{{else}}<div class="muted">—</div>{{end}}`
  (keep real; unit 03 restyles).

Rationale: keep agents + logs live (so a smoke test still shows data flowing)
while leaving the Issues card for unit 03 to fill richly.

## What NOT to do

- Don't change Go state, `panelView`, `snapshot`, or `Dashboard` methods.
- Don't edit `pageTemplate` or `launcherTemplate`.
- Don't add new `panelView` fields (unit 04 owns that).
- No external scripts/css. Inline `<style>` inside the fragment is allowed but
  keep it small; an idempotent inline `<script>` for the RPM bar is allowed.
- No Go code comments.

## Verification

1. `cd /home/saketh/Code-Review && go build ./...` → exit 0.
2. `cd /home/saketh/Code-Review && go test ./dashboard/` → `ok`, exit 0.
   `TestIndexServesHTML` greps for `pageTitle` (in `pageTemplate`, untouched) and
   `/events` (in `pageTemplate`, untouched) and `models.StageAnalyze` — your
   panel must still render the literal "Analyzing" when stage is Analyzing, so
   that test keeps passing. Do not rename or drop that label.
3. Static checks:
   ```
   grep -c 'class="panel"' dashboard/templates.go        # expect 1 (the root)
   grep -c 'CodeSentinel — Live Pipeline' dashboard/templates.go   # still >=1 (pageTitle, untouched)
   grep -c '"Analyzing"' dashboard/templates.go         # expect >=1 (rail label)
   grep -ce 'Preprocessing\|Localizing\|Analyzing\|Aggregating\|Generating Fixes\|Validating\|Raising PRs\|Complete' dashboard/templates.go  # expect all 8 present
   ```
4. `go vet ./dashboard/` → clean.
5. Manual smoke if possible: `FORCE_MOCK=true go run . --web`, open the URL,
   confirm: the rail shows 8 stages, the current stage pulses, KPI tiles show
   numbers that update, and the lower grid shows the three cards. Report what
   you see. If you can't run a server, say "skipped — no runnable env".

## Report back

- Diff of `panelTemplate`.
- The 4 grep counts.
- Whether you added an inline `<style>` and/or `<script>` in the fragment and
  why (RPM bar JS, panel-only polish CSS).
- Smoke result or skipped.
- Any template-escape gotchas you hit (e.g. `{{}}` inside JS) and how you solved
  them.
