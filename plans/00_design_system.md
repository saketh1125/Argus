# Unit 00 — Design system + page shell (foundation)

Owner: executing model. Orchestrator: do not run this yourself.

## Goal

Establish the CSS design-token layer and the outer `page` template that every
later unit renders inside. Replace the minimal inline styles with a coherent
dark "security tool" design system, but keep the exact same app shell contract
(`<div id="app">{{.Panel}}</div>` + vanilla `EventSource` SSE swap). This unit
**only** changes the look of the document frame; the panel fragment itself is
restructured in units 02/03 (unit 00 leaves `panelTemplate` untouched).

## Files you may edit

- `dashboard/templates.go` — **only** the `pageTemplate` const. Do NOT touch
  `panelTemplate`, `launcherTemplate`, `pageTitle`, `panelView`, `snapshot`,
  `renderPanel`, or `renderPage` (those signatures stay).

## Hard constraints (must hold after this unit)

- `pageTitle` constant stays exactly `"CodeSentinel — Live Pipeline"`.
- The rendered `/` page MUST still contain the literal `"CodeSentinel — Live Pipeline"`
  (it's in `<title>{{.Title}}</title>` — keep `.Title` usage) AND the literal
  `/events`. The dashboard test `TestIndexServesHTML` greps for both.
- `<div id="app">{{.Panel}}</div>` MUST remain verbatim (units 02/03 render into
  `#app`). The vanilla JS below MUST remain verbatim:
  ```html
  <script>
    const es = new EventSource("/events");
    es.onmessage = function(e) {
      document.getElementById("app").innerHTML = e.data;
    };
  </script>
  ```
- Stdlib only: no `<link>` stylesheets, no new `<script src>` (you MAY remove the
  HTMX `<script src="https://unpkg.com/htmx.org@1.9.12"></script>` line — it is
  not used; the SSE swap is vanilla `EventSource`. Removing it is preferred so we
  have zero CDN dependency). No fetches.
- One root document; keep it a single `{{define "page"}}…{{end}}` string.

## What to build

### A. Design tokens on `:root`

Inside the single `<style>` block, define CSS custom properties on `:root`:

- Surface scale: `--bg0:#0a0e14` (page), `--bg1:#0d1117` (panel surface),
  `--bg2:#161b22` (card), `--bg3:#21262d` (inset/control), `--bd1:#30363d`
  (border), `--bd2:#21262d` (subtle divider).
- Text scale: `--fg:#e6edf3`, `--fg-dim:#8b949e`, `--fg-faint:#6e7681`.
- Accent: `--accent:#58a6ff` (info), `--accent-2:#1f6feb`.
- Severity tokens (used by later units; define now): `--sev-crit:#ff7b72`,
  `--sev-high:#d29922`, `--sev-med:#e3b341`, `--sev-low:#8b949e`.
- Status tokens: `--ok:#3fb950`, `--ok-bg:#1a3a2a`, `--warn:#d29922`,
  `--err:#ff7b72`, `--err-bg:#3a1a1a`.
- `--radius:10px`, `--radius-sm:6px`. Spacing `--sp-1:4px … --sp-6:24px` (your
  choice of exact steps, just be consistent).
- `--mono` font stack (ui-monospace, SFMono-Regular, Menlo, Consolas, monospace)
  and a `--sans` stack ("Inter", system-ui, -apple-system, Segoe UI, Roboto,
  sans-serif). Body uses `--sans`; code/log/data uses `--mono`.
- `:root { color-scheme: dark; }` kept.

### B. Base layout + page shell

Restyle the body/page shell:

- `body` uses `--sans`, `var(--bg0)`, `var(--fg)`, margin 0, min-height 100vh,
  display flex column. Add a subtle radial-gradient header backdrop (pure CSS, no
  images): `background-image: radial-gradient(1200px 400px at 50% -200px,
  rgba(88,166,255,.10), transparent 70%), var(--bg0);`.
- Add a **sticky top app bar** inside the `page` template (NOT inside `#app`):
  a `<header class="appbar">` containing a small inline-SVG shield/dome glyph
  (define it inline, 18–20px, stroke `var(--accent)`) + the heading text
  "CodeSentinel" (bold, tracking tight) + a muted subtitle span "Live Pipeline".
  Right-aligned: a `<span id="conn-state">` that the JS updates (see below). The
  appbar must be `position:sticky; top:0; z-index:20;` with a translucent
  `backdrop-filter: blur(8px)` and bottom border `var(--bd1)`.
- Below the appbar, `<div id="app">{{.Panel}}</div>` (verbatim) in a max-width
  container `.wrap { max-width:1280px; margin:0 auto; padding:var(--sp-5)
  var(--sp-4); }` so the panel is centered and roomy.
- Replace the old `<h1>{{.Title}}</h1>` — the title now lives in the appbar. The
  `<title>{{.Title}}</title>` MUST stay (test greps the page source; `<title>`
  still contains the required literal).

### C. SSE script (replace the existing one)

Replace the existing `<script>` with one that:
- Keeps the exact `EventSource` swap (`document.getElementById("app").innerHTML = e.data;`).
- Adds connection state feedback to `#conn-state`:
  `es.onopen` → set text "● live" + class `c-ok`; `es.onerror` → set text
  "○ reconnecting" + class `c-warn`. Start with class `c-warn` text "○ connecting".
  This element is in the appbar so the header updates as SSE connects.
- Defines a tiny `window.__fmt` no-op helper is NOT required — keep the script
  minimal and dependency-free.

### D. Utility classes (for later units to use)

In the same `<style>`, add these utility classes that units 02/03 will rely on
(defining them now means later units touch `panelTemplate` only, no style churn):
- `.card` (bg `--bg2`, border `--bd1`, radius `--radius`, padding `--sp-4`,
  box-shadow `0 1px 0 rgba(255,255,255,.02) inset`);
- `.card > h2` (the panel card title style: `.7rem`, uppercase, letter-spacing
  `.08em`, color `--fg-dim`, margin 0 0 `.6rem`, with a small accent bar
  `::before` of 2px wide `--accent` height 10px inline-block, margin-right 6px).
- `.grid` and `.grid-2` / `.grid-3` / `.grid-4` (CSS grid with `auto-fit,
  minmax(…)` columns; `.grid` = 280px min like today, `.grid-2/3/4` use 2/3/4
  explicit columns collapsing to 1 under 900px).
- `.bar` / `.bar > span` (track `--bg3`, fill `--ok` linear-gradient; height 8px;
  radius `--radius-sm`; transition width `.3s`).
- `.bar.warn > span { background: var(--warn); }` and `.bar.crit > span` (red).
- `.badge`, `.badge.ok`, `.badge.err`, `.badge.warn` (small pill, `--sp-1
  var(--sp-2)` padding, mono `.7rem`, border radius 999px).
- `.muted { color: var(--fg-dim); }` `.mono { font-family: var(--mono); }`
  `.num { font-variant-numeric: tabular-nums; }` `.ellipsis { overflow:hidden;
  text-overflow:ellipsis; white-space:nowrap; }`.
- `.sev-crit`, `.sev-high`, `.sev-med`, `.sev-low` setting `color` to the token.
- Scrollbar styling for `.log`-like containers (`.scroll-y`): thin, `--bd1` track,
  `--bd2` thumb, radius.

Keep all of this in the one `<style>` inside `pageTemplate`. It can be ~120–180
lines of CSS — that's fine.

## What NOT to do

- Do not add any `<script src>`. Remove the HTMX CDN line.
- Do not change `panelTemplate` or `launcherTemplate`.
- Do not rename `pageTitle`, `panelView`, `snapshot`, `renderPage`.
- Do not add Go functions or change any `.go` logic — this is a template string
  edit only.
- Do not add comments to the Go source. (CSS comments inside the `<style>` are
  fine and encouraged for token sections.)

## Verification (run exactly these; stop and report if any fails)

1. `cd /home/saketh/Code-Review && go build ./...` → prints nothing, exit 0.
2. `cd /home/saketh/Code-Review && go test ./dashboard/` → `ok` line, exit 0.
3. `cd /home/saketh/Code-Review && go vet ./dashboard/` → clean.
4. Manual grep check (run all three):
   ```
   grep -c 'CodeSentinel — Live Pipeline' dashboard/templates.go   # expect >=1
   grep -c 'id="app"' dashboard/templates.go                       # expect 1 in pageTemplate
   grep -c '"\/events"' dashboard/templates.go                      # expect >=1
   ```
5. Fetch smoke (only if a `go run` is feasible in your env; skip if you cannot
   bind a port — units 02/03 will exercise it):
   ```
   FORCE_MOCK=true go run . --no-dashboard & sleep 1; kill %1   # just confirms it builds/runs
   ```

## Report back to the orchestrator

- The exact diff lines changed in `pageTemplate`.
- Confirmation that `panelTemplate` and `launcherTemplate` bytes are unchanged
  (paste `git diff --stat`).
- The four grep counts from verification step 4.
- Any deviation from this plan and why.
