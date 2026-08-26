# Orchestration Plan — CodeSentinel Frontend Refactor

This directory is the **execution plan** for refactoring the CodeSentinel web
dashboard (the `dashboard/` Go package) into a polished, modern GUI. Each
`.md` file is a self-contained, verifiable unit of work that a smaller
executing model can run **independently** once given that single file plus
`FRONTEND_CONTEXT.md`.

## How the orchestrator (you, the strong model) sliced this

The refactor is constrained (see `FRONTEND_CONTEXT.md` §6): **stdlib-only**, no
external Go deps, no JS build, no CGO, keep SSE architecture, keep routes/
`LaunchConfig`/`pageTitle`, keep tests green. The work decomposes along file
boundaries so each unit has a single owner and no merge conflicts:

1. **Design tokens + shell** (`00_design_system.md`) — establishes the CSS
   token block and the outer `page` template + the JS bootstrapping. Foundation
   that every later panel renders inside. Touches `templates.go` only (the
   `pageTemplate` const and the `<script>` block).
2. **Launcher form** (`01_launcher.md`) — restyles `launcherTemplate`. Fully
   independent of the live panel. Ships a great first-run experience.
3. **Live panel — layout & KPIs** (`02_panel_layout.md`) — restructures the
   `panel` fragment's outer grid + KPI tiles + stage timeline skeleton.
   Touches `templates.go` `panelTemplate` and `templates.go` `panelView`-related
   rendering only (no state changes).
4. **Rich issues + agents + log** (`03_panel_details.md`) — fills in the
   issues table, agent cards, and log drawer inside the panel grid. Continues
   in the same file as unit 3, sequenced after it.
5. **Enrich reporter state** (`04_enrich_state.md`) — OPTIONAL stretch. Extends
   `Dashboard` state + adds a new method to expose PR URLs without breaking the
   `Reporter` interface or the existing test. Has explicit guardrails and a
   fallback ("do nothing"). Only run this if units 1–4 are green.

## Execution rules for the small model

- **One doc at a time.** Read `FRONTEND_CONTEXT.md` and the current plan `.md`,
  do exactly what it says, nothing more.
- **Never add comments** unless the plan explicitly asks for a docstring/comment.
- **Stdlib only.** No `go get`, no new `require` lines, no external JS/CSS files,
  no fetch of CDNs beyond the already-present HTMX script tag (and you may remove
  HTMX if a plan says so — the SSE swap uses vanilla `EventSource`, not HTMX).
- **Verify at the end of each unit** with the exact command(s) stated in that
  doc's "Verification" section. Do not proceed if verification fails — stop and
  report the exact failing output.
- **Do NOT touch files outside the unit's "Files you may edit" list.** This is
  how the orchestrator guarantees no cross-unit regressions.
- **Preserve every public symbol and route** named in `FRONTEND_CONTEXT.md` §6.
- **Do NOT commit.** Leave changes in the working tree for the orchestrator to
  review/commit.

## Sequencing (run strictly in order)

1. `00_design_system.md`  → build green, tests green.
2. `01_launcher.md`       → build green, tests green, launcher smokes.
3. `02_panel_layout.md`   → build green, tests green.
4. `03_panel_details.md`  → build green, tests green, full manual smoke.
5. `04_enrich_state.md`   → OPTIONAL; only after 1–4 are green and review shows
   it's safe. Falls back to no-op.

After all units: orchestrator runs `go test ./...`, a manual `--web` smoke, and
reviews the diff for coherence before any commit.

## What "done" looks like

- A single-binary Go dashboard that looks like a purpose-built security tool:
  stage timeline, KPI tiles, severity-coded issues table, live agent cards,
  autoscroll log drawer, a matching launcher form, and a clear "Complete" state.
- `go build ./...` green; `go test ./...` green (dashboard test file untouched
  in its assertions).
- Zero new dependencies. No frontend build step.
