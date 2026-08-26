# Unit 01 — Restyle the launcher form

Owner: executing model. Prerequisite: unit `00_design_system.md` is merged &
green. Orchestrator: do not run this yourself.

## Goal

Restyle the **launcher page** (the pre-run configuration form served at `/`
when `--web` mode is active and the pipeline has not started) to match the new
design system. Keep every form field `name` attribute and the POST `/start`
contract identical so `dashboard/launcher.go` `handleStart` keeps working
unchanged. The launcher uses tokens now defined in `pageTemplate` (unit 00), so
this unit may also embed its own minimal `<style>` for launcher-only elements —
but it MUST also work if rendered standalone (it is a separate `{{define
"launcher-page"}}` document, not inside `page`).

## Files you may edit

- `dashboard/templates.go` — **only** the `launcherTemplate` const. Do NOT touch
  `pageTemplate`, `panelTemplate`, `pageTitle`, or anything in `launcher.go`,
  `dashboard.go`, `server.go`.

## Hard constraints

- The launcher is its own full HTML document (`<!DOCTYPE html>…</html>`) under
  `{{define "launcher-page"}}…{{end}}`. Keep that.
- `<title>` stays "CodeSentinel — Launch".
- Keep the `<form method="POST" action="/start">` and all these input `name`s
  EXACTLY: `repo_url`, `github_token`, `nim_api_key`, `e2b_api_key`,
  `qdrant_host`, `qdrant_port`, `ollama_host`, `embed_model`, `max_files`,
  `confidence_gate`, `force_mock`. Keep `force_mock` as a checkbox whose
  "on" value means force-mock (launcher.go reads `r.FormValue("force_mock")=="on"`).
- Keep the badge element ids `qdrant-badge` and `ollama-badge` and the
  `/health` fetch logic verbatim (the `<script>` at the bottom that calls
  `fetch('/health')` and `setBadge(...)`). You may restyle the badges via CSS
  but keep the JS contract (`el.textContent`, `el.className='badge '+(ok?'ok':'err')`).
- Default values must match today: repo placeholder only (no value), max_files
  default 50, confidence_gate 0.80, qdrant_host "localhost", qdrant_port 6334,
  ollama_host "http://localhost:11434", embed_model "nomic-embed-text".
- Simple `<script>` only — no `<script src>`, no frameworks, no fetches besides
  the existing `/health` one.

## What to build

### A. Document frame

Mirror the appbar from unit 00 at the top of this document too, so the launcher
feels like the same product. You can either (preferred) inline the same CSS
tokens+appbar in the launcher's own `<style>`, OR if unit 00 exposed them in a
way the launcher can't reach (it can't — `launcher-page` is a separate document),
just re-declare the `:root` token block + appbar styles here. Duplication is
acceptable; the launcher document is self-contained.

- Appbar: SVG shield glyph + "CodeSentinel" + subtitle "Launch configuration".
- A short hero line under the appbar: "Configure services, then start an
  autonomous analysis run." in `--fg-dim`.
- Center the form in a `.wrap { max-width:680px; margin:0 auto; padding:… }`.

### B. Form sections (same data, better structure)

Keep the existing sections and fields but restyle:

1. **Target Repository** — a full-width single-line field for `repo_url` with a
   small inline-SVG folder icon to the left (use an input wrapper
   `.field.with-icon` containing the SVG absolutely positioned and left padding
   on the input). Placeholder: "https://github.com/owner/repo  ·  or  ./testdata/sample-repo".
   `required` kept.
2. **API Keys** (two-col grid `.grid-2`): `github_token` and `nim_api_key`
   (both `type=password`), then a full-width `e2b_api_key` (`type=password`) with
   a one-line hint "blank = mock/local mode". Each field gets a label above and
   a right-aligned show/hide toggle button (👁 / 🙈) implemented in vanilla JS:
   clicking toggles the input's `type` between `password` and `text`. Add a tiny
   `<script>` (or extend the existing one) to wire all `.toggle-pw` buttons via
   `data-target` attribute pointing at the input id. NO external deps.
3. **Local Services**: two `.svc-row` units (Qdrant, Ollama) each showing the
   live `badge` (keep ids), the host input, and (Qdrant only) the port input in
   a 3-col `.grid-3`-ish row (`2fr 1fr 100px` or similar). Keep the existing
   "checking…" initial text and the `/health`-driven update.
4. **Tunables** (`.grid-2`): `max_files` (number, min 1 max 500) and
   `confidence_gate` (text, e.g. 0.80). Add small unit suffixes: "files" and
   "0–1" as muted hints beside the labels.
5. **Force mock**: a `.switch`-styled checkbox (still `name="force_mock"`). Style
   the native checkbox into a pill toggle via CSS-only (input:checked + label
   sliding knob). Must still submit `force_mock=on` when checked — verify the
   checkbox is inside the `<form>` and has `name="force_mock"`. Keep it unchecked
   by default.
6. **Submit**: a prominent full-width primary button "▶ Start Analysis" using
   `--ok`/`--ok-bg` tokens, hover lighten, active darken. Add a secondary muted
   line under it: "Blank keys run everything in deterministic mock mode."

### C. Polish

- Consistent field styling: `--bg1` inputs, `--bd1` 1px borders, radius
  `--radius-sm`, focus ring `2px solid var(--accent)` + subtle glow
  (`box-shadow: 0 0 0 3px rgba(88,166,255,.18)`), transition `.15s`.
- Labels `--fg-dim`, `.8rem`. Section headers like the panel card titles
  (`.7rem` uppercase, accent bar `::before`).
- Spacing generous; cards in `--bg2`.

## What NOT to do

- Don't change `launcher.go` parsing defaults (maxFiles→50, gate→0.80,
  qdrantPort→6334, force_mock "on" check, strOr fallbacks).
- Don't add new `name` attributes or remove existing ones.
- Don't add any `<script src>` or external CSS.
- Don't add Go comments.
- Don't touch `panelTemplate` / `pageTemplate`.

## Verification

1. `cd /home/saketh/Code-Review && go build ./...` → exit 0.
2. `cd /home/saketh/Code-Review && go test ./dashboard/` → `ok`, exit 0
   (`TestIndexServesHTML` hits the panel path, not the launcher, but keep green).
3. Launcher name-attribute audit — run and confirm each prints ≥1:
   ```
   for n in repo_url github_token nim_api_key e2b_api_key qdrant_host qdrant_port ollama_host embed_model max_files confidence_gate force_mock; do
     printf '%s: ' "$n"; grep -c "name=\"$n\"" dashboard/templates.go
   done
   ```
4. `grep -c 'qdrant-badge\|ollama-badge' dashboard/templates.go` → ≥2.
5. `grep -c "/health" dashboard/templates.go` → ≥1 (launcher fetch).
6. Manual smoke (if you can run a server): `go run . --web` (or
   `FORCE_MOCK=true go run . --web`), open `http://localhost:8080`, confirm the
   launcher renders, the badges self-update after `/health`, and submitting
   "Start Analysis" with the bundled `./testdata/sample-repo` advances to the
   live panel. If you can't run a server, skip and say so.

## Report back

- Diff of `launcherTemplate`.
- The 11 name-attribute counts.
- The badge and `/health` counts.
- Note whether you re-declared the `:root` tokens in the launcher `<style>` (you
  should have) and whether you kept the show/hide toggle working for ALL password
  inputs.
- Smoke result or "skipped — no runnable env".
