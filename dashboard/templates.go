package dashboard

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
)

// pageTitle is the dashboard heading, asserted by tests and shown in the tab.
const pageTitle = "CodeSentinel — Live Pipeline"

// tmpl holds the parsed page + panel templates. The "panel" block is rendered
// both inline on the initial page load and as the SSE payload on every update,
// so the two views can never drift.
//
// The page uses a tiny vanilla-JS EventSource (no framework) that connects to
// /events and replaces #app's innerHTML with each server-rendered fragment.
// This is simpler and more robust than wiring HTMX's SSE extension for an
// innerHTML swap, while still meeting FR-09 (live updates, no page reload).
// HTMX is still loaded from CDN per the design, but the swap is driven by the
// EventSource handler below.
var tmpl = template.Must(template.New("dashboard").Parse(pageTemplate + panelTemplate + launcherTemplate))

// panelView is the flattened, render-ready snapshot handed to the template.
type panelView struct {
	Stage    string
	StagePct int

	FilesDone  int
	FilesTotal int

	IssuesFound   int
	CriticalCount int
	HighCount     int
	RecentIssues  []recentIssue

	PRsRaised int

	ValidatedDone  int
	ValidatedTotal int

	RPMCurrent int
	RPMMax     int

	Agents []agentLine
	Logs   []string

	Done bool // true when stage == "Complete"
}

// snapshot copies the current state into a render-ready view under the read
// lock. Slices are copied so the template renders without holding the lock.
func (d *Dashboard) snapshot() panelView {
	d.mu.RLock()
	defer d.mu.RUnlock()
	s := &d.state

	agents := make([]agentLine, 0, len(s.agentOrder))
	for _, name := range s.agentOrder {
		agents = append(agents, agentLine{Name: name, Status: s.agents[name]})
	}
	recent := make([]recentIssue, len(s.recentIssues))
	copy(recent, s.recentIssues)
	logs := make([]string, len(s.logs))
	copy(logs, s.logs)

	return panelView{
		Stage:          s.stage,
		StagePct:       s.stagePct,
		FilesDone:      s.filesDone,
		FilesTotal:     s.filesTotal,
		IssuesFound:    s.issuesFound,
		CriticalCount:  s.criticalCount,
		HighCount:      s.highCount,
		RecentIssues:   recent,
		PRsRaised:      s.prsRaised,
		ValidatedDone:  s.validatedDone,
		ValidatedTotal: s.validatedTotal,
		RPMCurrent:     s.rpmCurrent,
		RPMMax:         s.rpmMax,
		Agents:         agents,
		Logs:           logs,
		Done:           s.stage == "Complete",
	}
}

// renderPanel renders just the live panel fragment (the SSE payload).
func (d *Dashboard) renderPanel() []byte {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "panel", d.snapshot()); err != nil {
		return []byte(fmt.Sprintf("<pre>render error: %v</pre>", err))
	}
	return buf.Bytes()
}

// renderPage renders the full HTML page with the panel embedded for the
// initial, pre-SSE view.
func (d *Dashboard) renderPage(w io.Writer, panel []byte) error {
	data := struct {
		Title string
		Panel template.HTML
	}{
		Title: pageTitle,
		Panel: template.HTML(panel), // already escaped by the panel template
	}
	return tmpl.ExecuteTemplate(w, "page", data)
}

// pageTemplate is the outer document: head, styles, the #app container seeded
// with the initial panel, and the EventSource wiring.
const pageTemplate = `{{define "page"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
  /* ========================================================================
   * Design Tokens
   * ======================================================================== */
  :root {
    color-scheme: dark;

    /* Surface scale */
    --bg0: #0a0e14;   /* page background */
    --bg1: #0d1117;   /* panel surface */
    --bg2: #161b22;   /* card */
    --bg3: #21262d;   /* inset/control */
    --bd1: #30363d;   /* border */
    --bd2: #21262d;   /* subtle divider */

    /* Text scale */
    --fg: #e6edf3;
    --fg-dim: #8b949e;
    --fg-faint: #6e7681;

    /* Accent */
    --accent: #58a6ff;
    --accent-2: #1f6feb;

    /* Severity */
    --sev-crit: #ff7b72;
    --sev-high: #d29922;
    --sev-med: #e3b341;
    --sev-low: #8b949e;

    /* Status */
    --ok: #3fb950;
    --ok-bg: #1a3a2a;
    --warn: #d29922;
    --err: #ff7b72;
    --err-bg: #3a1a1a;

    /* Radius & spacing */
    --radius: 10px;
    --radius-sm: 6px;
    --sp-1: 4px;
    --sp-2: 8px;
    --sp-3: 12px;
    --sp-4: 16px;
    --sp-5: 20px;
    --sp-6: 24px;

    /* Font stacks */
    --mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    --sans: "Inter", system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
  }

  /* ========================================================================
   * Base Layout
   * ======================================================================== */
  * { box-sizing: border-box; }
  body {
    font-family: var(--sans);
    background:
      radial-gradient(1200px 400px at 50% -200px, rgba(88,166,255,.10), transparent 70%),
      var(--bg0);
    color: var(--fg);
    margin: 0;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }

  /* App bar */
  .appbar {
    position: sticky;
    top: 0;
    z-index: 20;
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 56px;
    padding: 0 var(--sp-5);
    background: rgba(10,14,20,.85);
    backdrop-filter: blur(8px);
    border-bottom: 1px solid var(--bd1);
  }
  .appbar__brand {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .appbar__icon {
    width: 20px;
    height: 20px;
    flex-shrink: 0;
    stroke: var(--accent);
  }
  .appbar__title {
    font-size: 1rem;
    font-weight: 700;
    letter-spacing: -0.02em;
    color: var(--fg);
  }
  .appbar__subtitle {
    font-size: .75rem;
    color: var(--fg-dim);
    font-weight: 400;
  }
  #conn-state {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    font-size: .7rem;
    font-family: var(--mono);
    font-variant-numeric: tabular-nums;
    padding: var(--sp-1) var(--sp-2);
    border-radius: 999px;
    background: var(--bg3);
    color: var(--fg-dim);
  }
  #conn-state.c-ok { color: var(--ok); background: var(--ok-bg); }
  #conn-state.c-warn { color: var(--warn); background: var(--warn); opacity: .15; }
  #conn-state.c-err { color: var(--err); background: var(--err-bg); }

  /* Main content wrapper */
  .wrap {
    flex: 1;
    width: 100%;
    max-width: 1280px;
    margin: 0 auto;
    padding: var(--sp-5) var(--sp-4);
  }

  /* ========================================================================
   * Utility Classes (shared by panel & launcher)
   * ======================================================================== */

  /* Card */
  .card {
    background: var(--bg2);
    border: 1px solid var(--bd1);
    border-radius: var(--radius);
    padding: var(--sp-4);
    box-shadow: 0 1px 0 rgba(255,255,255,.02) inset;
  }
  .card > h2 {
    position: relative;
    font-size: .7rem;
    text-transform: uppercase;
    letter-spacing: .08em;
    color: var(--fg-dim);
    margin: 0 0 var(--sp-3);
    padding-left: var(--sp-3);
  }
  .card > h2::before {
    content: "";
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
    width: 2px;
    height: 10px;
    background: var(--accent);
    border-radius: 1px;
  }

  /* Grid helpers */
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: var(--sp-4);
  }
  .grid-2 { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--sp-4); }
  .grid-3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--sp-4); }
  .grid-4 { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--sp-4); }
  @media (max-width: 900px) {
    .grid-2, .grid-3, .grid-4 { grid-template-columns: 1fr; }
  }

  /* Progress bar */
  .bar {
    background: var(--bg3);
    border-radius: var(--radius-sm);
    height: 8px;
    overflow: hidden;
  }
  .bar > span {
    display: block;
    height: 100%;
    background: linear-gradient(90deg, var(--ok), var(--accent));
    transition: width .3s ease;
    border-radius: var(--radius-sm);
  }
  .bar.warn > span { background: var(--warn); }
  .bar.crit > span { background: var(--err); }

  /* Badge */
  .badge {
    display: inline-flex;
    align-items: center;
    font-size: .7rem;
    font-family: var(--mono);
    padding: var(--sp-1) var(--sp-2);
    border-radius: 999px;
    background: var(--bg3);
    color: var(--fg-dim);
  }
  .badge.ok { background: var(--ok-bg); color: var(--ok); }
  .badge.err { background: var(--err-bg); color: var(--err); }
  .badge.warn { background: rgba(210,153,34,.15); color: var(--warn); }

  /* Text utilities */
  .muted { color: var(--fg-dim); }
  .mono { font-family: var(--mono); }
  .num { font-variant-numeric: tabular-nums; }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* Severity text colors */
  .sev-crit { color: var(--sev-crit); }
  .sev-high { color: var(--sev-high); }
  .sev-med { color: var(--sev-med); }
  .sev-low { color: var(--sev-low); }

  /* Scrollbar */
  .scroll-y {
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--bd2) var(--bd1);
  }
  .scroll-y::-webkit-scrollbar { width: 8px; height: 8px; }
  .scroll-y::-webkit-scrollbar-track { background: var(--bd1); border-radius: 4px; }
  .scroll-y::-webkit-scrollbar-thumb { background: var(--bd2); border-radius: 4px; }
  .scroll-y::-webkit-scrollbar-thumb:hover { background: var(--bd1); }

  /* Done banner */
  .done-banner {
    background: var(--ok-bg);
    border: 1px solid var(--ok);
    border-radius: var(--radius);
    color: var(--ok);
    font-weight: 600;
    padding: var(--sp-3) var(--sp-4);
    margin-bottom: var(--sp-4);
    font-size: .95rem;
  }
</style>
</head>
<body>
<header class="appbar">
  <div class="appbar__brand">
    <svg class="appbar__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
      <path d="M12 22C17.5228 22 22 17.5228 22 12C22 6.47715 17.5228 2 12 2C6.47715 2 2 6.47715 2 12C2 17.5228 6.47715 22 12 22Z"/>
      <path d="M12 6V12L15 15"/>
    </svg>
    <div>
      <div class="appbar__title">CodeSentinel</div>
      <div class="appbar__subtitle">Live Pipeline</div>
    </div>
  </div>
  <span id="conn-state" class="c-warn">○ connecting</span>
</header>
<div class="wrap">
  <div id="app">{{.Panel}}</div>
</div>
<script>
  (function() {
    const es = new EventSource("/events");
    const conn = document.getElementById("conn-state");

    function setState(cls, text) {
      conn.className = cls;
      conn.textContent = text;
    }

    es.onopen = function() {
      setState("c-ok", "● live");
    };
    es.onerror = function() {
      setState("c-warn", "○ reconnecting");
    };
    es.onmessage = function(e) {
      document.getElementById("app").innerHTML = e.data;
    };
  })();
</script>
</body>
</html>{{end}}`

// panelTemplate is the live fragment: every panel from design §10. Rendered
// both inline (initial load) and as each SSE update.
const panelTemplate = `{{define "panel"}}{{if .Done}}<div class="done-banner">✅ Analysis Complete &mdash; {{.PRsRaised}} PR{{if ne .PRsRaised 1}}s{{end}} raised &middot; {{.IssuesFound}} issues found &middot; {{.ValidatedDone}} validated</div>{{end}}<div class="grid">
  <div class="card">
    <h2>Pipeline</h2>
    <table>
      <tr><td class="k">Stage</td><td{{if .Done}} style="color:#3fb950;font-weight:600"{{end}}>{{.Stage}}</td></tr>
      <tr><td class="k">Progress</td><td><div class="bar"><span style="width:{{.StagePct}}%"></span></div> {{.StagePct}}%</td></tr>
      <tr><td class="k">Files Done</td><td>{{.FilesDone}} / {{.FilesTotal}}</td></tr>
      <tr><td class="k">Issues Found</td><td>{{.IssuesFound}} (<span class="crit">{{.CriticalCount}} Critical</span>, <span class="high">{{.HighCount}} High</span>)</td></tr>
      <tr><td class="k">PRs Raised</td><td>{{.PRsRaised}}</td></tr>
      <tr><td class="k">Validated</td><td>{{.ValidatedDone}} / {{.ValidatedTotal}}</td></tr>
      <tr><td class="k">RPM Usage</td><td>{{.RPMCurrent}} / {{.RPMMax}}</td></tr>
    </table>
  </div>
  <div class="card">
    <h2>Active Agents</h2>
    <ul>
      {{range .Agents}}<li>{{.Name}}: {{.Status}}</li>{{else}}<li>No active agents</li>{{end}}
    </ul>
  </div>
  <div class="card">
    <h2>Issues Detected</h2>
    <ul>
      {{range .RecentIssues}}<li>{{.Icon}} {{.BugType}} — {{.File}}:{{.Line}} (conf: {{.Confidence}}%)</li>{{else}}<li>None yet</li>{{end}}
    </ul>
  </div>
  <div class="card">
    <h2>Log</h2>
    <div class="log">
      {{range .Logs}}<div>{{.}}</div>{{else}}<div>—</div>{{end}}
    </div>
  </div>
</div>{{end}}`

// launcherTemplate is the pre-run configuration form served at / when
// --web mode is active. Submitting it POSTs to /start.
const launcherTemplate = `{{define "launcher-page"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>CodeSentinel — Launch</title>
<style>
  /* ========================================================================
   * Design Tokens (mirrored from pageTemplate for standalone document)
   * ======================================================================== */
  :root {
    color-scheme: dark;

    /* Surface scale */
    --bg0: #0a0e14;   /* page background */
    --bg1: #0d1117;   /* panel surface */
    --bg2: #161b22;   /* card */
    --bg3: #21262d;   /* inset/control */
    --bd1: #30363d;   /* border */
    --bd2: #21262d;   /* subtle divider */

    /* Text scale */
    --fg: #e6edf3;
    --fg-dim: #8b949e;
    --fg-faint: #6e7681;

    /* Accent */
    --accent: #58a6ff;
    --accent-2: #1f6feb;

    /* Severity */
    --sev-crit: #ff7b72;
    --sev-high: #d29922;
    --sev-med: #e3b341;
    --sev-low: #8b949e;

    /* Status */
    --ok: #3fb950;
    --ok-bg: #1a3a2a;
    --warn: #d29922;
    --err: #ff7b72;
    --err-bg: #3a1a1a;

    /* Radius & spacing */
    --radius: 10px;
    --radius-sm: 6px;
    --sp-1: 4px;
    --sp-2: 8px;
    --sp-3: 12px;
    --sp-4: 16px;
    --sp-5: 20px;
    --sp-6: 24px;

    /* Font stacks */
    --mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    --sans: "Inter", system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
  }

  /* ========================================================================
   * Base Layout
   * ======================================================================== */
  * { box-sizing: border-box; }
  body {
    font-family: var(--sans);
    background:
      radial-gradient(1200px 400px at 50% -200px, rgba(88,166,255,.10), transparent 70%),
      var(--bg0);
    color: var(--fg);
    margin: 0;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }

  /* App bar */
  .appbar {
    position: sticky;
    top: 0;
    z-index: 20;
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 56px;
    padding: 0 var(--sp-5);
    background: rgba(10,14,20,.85);
    backdrop-filter: blur(8px);
    border-bottom: 1px solid var(--bd1);
  }
  .appbar__brand {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .appbar__icon {
    width: 20px;
    height: 20px;
    flex-shrink: 0;
    stroke: var(--accent);
  }
  .appbar__title {
    font-size: 1rem;
    font-weight: 700;
    letter-spacing: -0.02em;
    color: var(--fg);
  }
  .appbar__subtitle {
    font-size: .75rem;
    color: var(--fg-dim);
    font-weight: 400;
  }

  /* Main content wrapper for launcher */
  .wrap {
    flex: 1;
    width: 100%;
    max-width: 680px;
    margin: 0 auto;
    padding: var(--sp-5) var(--sp-4);
  }

  /* Hero line */
  .hero {
    color: var(--fg-dim);
    font-size: .95rem;
    margin: var(--sp-4) 0 var(--sp-5);
    text-align: center;
  }

  /* ========================================================================
   * Form Components
   * ======================================================================== */

  /* Card */
  .card {
    background: var(--bg2);
    border: 1px solid var(--bd1);
    border-radius: var(--radius);
    padding: var(--sp-5);
  }
  .card > h2 {
    position: relative;
    font-size: .7rem;
    text-transform: uppercase;
    letter-spacing: .08em;
    color: var(--fg-dim);
    margin: 0 0 var(--sp-3);
    padding-left: var(--sp-3);
  }
  .card > h2::before {
    content: "";
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
    width: 2px;
    height: 10px;
    background: var(--accent);
    border-radius: 1px;
  }
  .section {
    margin-top: var(--sp-5);
  }
  .section:first-child { margin-top: 0; }

  /* Grid helpers */
  .grid-2 { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--sp-4); }
  .grid-3 { display: grid; grid-template-columns: 2fr 1fr 100px; gap: var(--sp-3); align-items: end; }
  @media (max-width: 700px) {
    .grid-2, .grid-3 { grid-template-columns: 1fr; }
  }

  /* Field */
  .field { margin-bottom: var(--sp-4); }
  .field:last-child { margin-bottom: 0; }
  label {
    display: block;
    font-size: .8rem;
    color: var(--fg-dim);
    margin-bottom: var(--sp-1);
  }
  .hint {
    font-size: .7rem;
    color: var(--fg-faint);
    font-weight: 400;
    margin-left: var(--sp-1);
  }
  .input-wrap { position: relative; }
  input[type=text], input[type=password], input[type=number] {
    width: 100%;
    background: var(--bg1);
    border: 1px solid var(--bd1);
    border-radius: var(--radius-sm);
    color: var(--fg);
    font-family: var(--mono);
    font-size: .9rem;
    padding: .55rem .85rem;
    outline: none;
    transition: border-color .15s, box-shadow .15s;
  }
  input[type=text]:focus, input[type=password]:focus, input[type=number]:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px rgba(88,166,255,.18);
  }
  .input-wrap.with-icon input { padding-left: 2.5rem; }
  .input-icon {
    position: absolute;
    left: .75rem;
    top: 50%;
    transform: translateY(-50%);
    width: 16px;
    height: 16px;
    stroke: var(--fg-dim);
    pointer-events: none;
  }
  .toggle-pw {
    position: absolute;
    right: .6rem;
    top: 50%;
    transform: translateY(-50%);
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: .85rem;
    cursor: pointer;
    padding: .15rem;
    line-height: 1;
    opacity: .7;
    transition: opacity .15s;
  }
  .toggle-pw:hover { opacity: 1; color: var(--fg); }

  /* Service row */
  .svc-row { display: flex; align-items: end; gap: var(--sp-3); margin-bottom: var(--sp-3); }
  .svc-label {
    width: 70px;
    font-size: .85rem;
    color: var(--fg-dim);
    flex-shrink: 0;
  }
  .svc-row .input-wrap { flex: 1; }
  .svc-row input[type=number] { width: 100px; }

  /* Badge */
  .badge {
    display: inline-flex;
    align-items: center;
    font-size: .7rem;
    font-family: var(--mono);
    padding: var(--sp-1) var(--sp-2);
    border-radius: 999px;
    background: var(--bg3);
    color: var(--fg-dim);
  }
  .badge.ok { background: var(--ok-bg); color: var(--ok); }
  .badge.err { background: var(--err-bg); color: var(--err); }

  /* Switch (checkbox styled as toggle) */
  .switch-row {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    margin-top: var(--sp-3);
  }
  .switch {
    position: relative;
    width: 48px;
    height: 26px;
    flex-shrink: 0;
  }
  .switch input { position: absolute; opacity: 0; width: 0; height: 0; }
  .switch-slider {
    position: absolute;
    inset: 0;
    background: var(--bg3);
    border: 1px solid var(--bd1);
    border-radius: 999px;
    transition: background .2s, border-color .2s;
    cursor: pointer;
  }
  .switch-slider::before {
    content: "";
    position: absolute;
    top: 2px;
    left: 2px;
    width: 20px;
    height: 20px;
    background: var(--fg-dim);
    border-radius: 50%;
    transition: transform .2s, background .2s;
    box-shadow: 0 1px 3px rgba(0,0,0,.3);
  }
  .switch input:checked + .switch-slider {
    background: var(--ok);
    border-color: var(--ok);
  }
  .switch input:checked + .switch-slider::before {
    transform: translateX(22px);
    background: var(--bg0);
  }
  .switch input:focus-visible + .switch-slider {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .switch-label {
    font-size: .85rem;
    color: var(--fg);
    cursor: pointer;
  }
  .switch-hint { font-size: .7rem; color: var(--fg-faint); margin-left: var(--sp-1); }

  /* Submit button */
  .submit-btn {
    display: block;
    width: 100%;
    padding: .85rem;
    background: var(--ok);
    border: 1px solid var(--ok);
    border-radius: var(--radius-sm);
    color: var(--bg0);
    font-family: var(--sans);
    font-size: 1rem;
    font-weight: 600;
    cursor: pointer;
    margin-top: var(--sp-5);
    transition: background .15s, border-color .15s, transform .05s;
  }
  .submit-btn:hover { background: #4ec460; border-color: #4ec460; }
  .submit-btn:active { transform: scale(.99); }
  .submit-hint {
    text-align: center;
    margin-top: var(--sp-3);
    font-size: .75rem;
    color: var(--fg-faint);
  }

  /* Focus visible for accessibility */
  :focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
</style>
</head>
<body>
<header class="appbar">
  <div class="appbar__brand">
    <svg class="appbar__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
      <path d="M12 22C17.5228 22 22 17.5228 22 12C22 6.47715 17.5228 2 12 2C6.47715 2 2 6.47715 2 12C2 17.5228 6.47715 22 12 22Z"/>
      <path d="M12 6V12L15 15"/>
    </svg>
    <div>
      <div class="appbar__title">CodeSentinel</div>
      <div class="appbar__subtitle">Launch configuration</div>
    </div>
  </div>
</header>
<div class="wrap">
  <p class="hero">Configure services, then start an autonomous analysis run.</p>

  <div class="card">
    <form method="POST" action="/start">

      <h2>Target Repository</h2>
      <div class="field">
        <label>Repo URL or local path</label>
        <div class="input-wrap with-icon">
          <svg class="input-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
          </svg>
          <input type="text" name="repo_url" placeholder="https://github.com/owner/repo  ·  or  ./testdata/sample-repo" required autocomplete="url">
        </div>
      </div>

      <h2>API Keys</h2>
      <div class="grid-2">
        <div class="field">
          <label>GitHub Token <span class="hint">(PR creation)</span></label>
          <div class="input-wrap">
            <input type="password" name="github_token" id="github_token" placeholder="ghp_..." autocomplete="current-password">
            <button type="button" class="toggle-pw" data-target="github_token" aria-label="Show GitHub token">👁</button>
          </div>
        </div>
        <div class="field">
          <label>NIM API Key (LLM) <span class="hint">(Nvidia NIM)</span></label>
          <div class="input-wrap">
            <input type="password" name="nim_api_key" id="nim_api_key" placeholder="nvapi_..." autocomplete="current-password">
            <button type="button" class="toggle-pw" data-target="nim_api_key" aria-label="Show NIM API key">👁</button>
          </div>
        </div>
      </div>
      <div class="field">
        <label>E2B API Key (cloud sandbox) <span class="hint">blank = local syntax check</span></label>
        <div class="input-wrap">
          <input type="password" name="e2b_api_key" id="e2b_api_key" placeholder="e2b_..." autocomplete="current-password">
          <button type="button" class="toggle-pw" data-target="e2b_api_key" aria-label="Show E2B API key">👁</button>
        </div>
      </div>
      <div class="row" style="margin-top:.75rem">
        <div>
          <label>Jina API Key (embeddings fallback)</label>
          <input type="password" name="jina_api_key" placeholder="jina_...">
        </div>
        <div>
          <label>Gemini API Key (embeddings fallback)</label>
          <input type="password" name="gemini_api_key" placeholder="AIza...">
        </div>
      </div>

      <h2>Local Services</h2>
      <div class="svc-row">
        <span class="svc-label">Qdrant</span>
        <span id="qdrant-badge" class="badge">checking…</span>
        <div class="input-wrap" style="flex:1">
          <input type="text" name="qdrant_host" value="localhost" autocomplete="off">
        </div>
        <div class="input-wrap" style="width:100px">
          <input type="number" name="qdrant_port" value="6334" min="1" max="65535" autocomplete="off">
        </div>
      </div>
      <div class="svc-row">
        <span class="svc-label">Ollama</span>
        <span id="ollama-badge" class="badge">checking…</span>
        <div class="input-wrap" style="flex:1">
          <input type="text" name="ollama_host" value="http://localhost:11434" autocomplete="off">
        </div>
      </div>
      <div class="field">
        <label>Embed model</label>
        <div class="input-wrap">
          <input type="text" name="embed_model" value="nomic-embed-text" autocomplete="off">
        </div>
      </div>

      <h2>Tunables</h2>
      <div class="grid-2">
        <div class="field">
          <label>Max files to analyse <span class="hint">files</span></label>
          <div class="input-wrap">
            <input type="number" name="max_files" value="50" min="1" max="500" autocomplete="off">
          </div>
        </div>
        <div class="field">
          <label>Confidence gate <span class="hint">0–1</span></label>
          <div class="input-wrap">
            <input type="text" name="confidence_gate" value="0.80" autocomplete="off">
          </div>
        </div>
      </div>

      <div class="switch-row">
        <label class="switch">
          <input type="checkbox" name="force_mock" id="force_mock">
          <span class="switch-slider"></span>
        </label>
        <span class="switch-label">Force mock mode</span>
        <span class="switch-hint">(ignore all keys, run fully offline)</span>
      </div>

      <button type="submit" class="submit-btn">▶ Start Analysis</button>
      <p class="submit-hint">Blank keys run everything in deterministic mock mode.</p>

    </form>
  </div>
</div>
<script>
  (function() {
    // Password visibility toggles
    document.querySelectorAll('.toggle-pw').forEach(function(btn) {
      btn.addEventListener('click', function() {
        var targetId = this.getAttribute('data-target');
        var input = document.getElementById(targetId);
        if (!input) return;
        if (input.type === 'password') {
          input.type = 'text';
          this.textContent = '🙈';
        } else {
          input.type = 'password';
          this.textContent = '👁';
        }
      });
    });

    // Health check badges
    fetch('/health').then(function(r) { return r.json(); }).then(function(d) {
      setBadge('qdrant-badge', d.qdrant, 'localhost:6333');
      setBadge('ollama-badge', d.ollama, 'localhost:11434');
    }).catch(function() {
      setBadge('qdrant-badge', false, 'localhost:6333');
      setBadge('ollama-badge', false, 'localhost:11434');
    });
    function setBadge(id, ok, addr) {
      var el = document.getElementById(id);
      if (!el) return;
      el.textContent = ok ? '● Running' : '○ Not detected';
      el.className = 'badge ' + (ok ? 'ok' : 'err');
    }
  })();
</script>
</body>
</html>{{end}}`
