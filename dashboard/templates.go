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
// with the initial panel, the HTMX CDN script, and the EventSource wiring.
const pageTemplate = `{{define "page"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<script src="https://unpkg.com/htmx.org@1.9.12"></script>
<style>
  :root { color-scheme: dark; }
  body { font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
         background:#0d1117; color:#e6edf3; margin:0; padding:1.5rem; }
  h1 { font-size:1.25rem; margin:0 0 1rem; color:#58a6ff; }
  .grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(280px,1fr));
          gap:1rem; }
  .card { background:#161b22; border:1px solid #30363d; border-radius:8px;
          padding:1rem; }
  .card h2 { font-size:.8rem; text-transform:uppercase; letter-spacing:.05em;
             color:#8b949e; margin:0 0 .6rem; }
  table { width:100%; border-collapse:collapse; font-size:.9rem; }
  td { padding:.2rem 0; }
  td.k { color:#8b949e; width:45%; }
  .bar { background:#21262d; border-radius:4px; height:14px; overflow:hidden; }
  .bar > span { display:block; height:100%; background:#3fb950; }
  ul { list-style:none; margin:0; padding:0; font-size:.85rem; }
  li { padding:.15rem 0; border-bottom:1px solid #21262d; }
  .crit { color:#ff7b72; } .high { color:#d29922; }
  .log { max-height:200px; overflow-y:auto; font-size:.78rem; color:#8b949e; }
  .log div { white-space:pre-wrap; }
  .done-banner { background:#1a3a2a; border:1px solid #3fb950; border-radius:8px;
                 color:#3fb950; font-weight:600; padding:.75rem 1rem; margin-bottom:1rem; font-size:.95rem; }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<div id="app">{{.Panel}}</div>
<script>
  // Vanilla EventSource: connect to /events and replace #app with each
  // server-rendered fragment. No page reload (FR-09).
  const es = new EventSource("/events");
  es.onmessage = function(e) {
    document.getElementById("app").innerHTML = e.data;
  };
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
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
         background: #0d1117; color: #e6edf3; margin: 0;
         display: flex; justify-content: center; padding: 2rem 1rem; }
  .wrap { width: 100%; max-width: 560px; }
  h1 { font-size: 1.25rem; color: #58a6ff; margin: 0 0 .25rem; }
  .sub { color: #8b949e; font-size: .85rem; margin: 0 0 1.75rem; }
  .card { background: #161b22; border: 1px solid #30363d; border-radius: 8px;
          padding: 1.5rem; margin-bottom: 1rem; }
  .section { font-size: .7rem; text-transform: uppercase; letter-spacing: .08em;
             color: #8b949e; margin: 1.25rem 0 .6rem; border-bottom: 1px solid #21262d;
             padding-bottom: .3rem; }
  .section:first-child { margin-top: 0; }
  label { display: block; font-size: .8rem; color: #8b949e; margin-bottom: .25rem; }
  input[type=text], input[type=password], input[type=number] {
    width: 100%; background: #0d1117; border: 1px solid #30363d; border-radius: 6px;
    color: #e6edf3; font-family: inherit; font-size: .9rem;
    padding: .45rem .7rem; outline: none; }
  input:focus { border-color: #58a6ff; }
  .row { display: grid; grid-template-columns: 1fr 1fr; gap: .75rem; }
  .row3 { display: grid; grid-template-columns: 2fr 1fr 1fr; gap: .75rem; }
  .svc-row { display: flex; align-items: center; gap: .75rem; margin-bottom: .5rem; }
  .svc-label { width: 60px; font-size: .85rem; }
  .badge { font-size: .75rem; padding: .15rem .5rem; border-radius: 10px;
           background: #21262d; color: #8b949e; }
  .badge.ok  { background: #1a3a2a; color: #3fb950; }
  .badge.err { background: #3a1a1a; color: #ff7b72; }
  .checkbox-row { display: flex; align-items: center; gap: .5rem;
                  font-size: .85rem; margin-top: .5rem; }
  input[type=checkbox] { width: 15px; height: 15px; accent-color: #58a6ff; }
  .hint { color: #8b949e; font-size: .75rem; font-weight: normal; margin-left: .4rem; }
  .submit-btn {
    display: block; width: 100%; padding: .75rem;
    background: #238636; border: 1px solid #2ea043; border-radius: 6px;
    color: #fff; font-family: inherit; font-size: 1rem; font-weight: 600;
    cursor: pointer; margin-top: 1.5rem; transition: background .15s; }
  .submit-btn:hover { background: #2ea043; }
  .submit-btn:active { background: #1e7a2e; }
</style>
</head>
<body>
<div class="wrap">
  <h1>🛡 CodeSentinel</h1>
  <p class="sub">Configure services and credentials, then click Start Analysis.</p>

  <div class="card">
    <form method="POST" action="/start">

      <div class="section">Target Repository</div>
      <label>Repo URL or local path</label>
      <input type="text" name="repo_url" placeholder="https://github.com/owner/repo  or  ./testdata/sample-repo" required>

      <div class="section">API Keys <span class="hint">blank = deterministic mock mode</span></div>
      <div class="row">
        <div>
          <label>GitHub Token</label>
          <input type="password" name="github_token" placeholder="ghp_...">
        </div>
        <div>
          <label>NIM API Key (LLM)</label>
          <input type="password" name="nim_api_key" placeholder="nvapi-...">
        </div>
      </div>
      <div style="margin-top:.75rem">
        <label>E2B API Key (cloud sandbox) <span class="hint">blank = local syntax check</span></label>
        <input type="password" name="e2b_api_key" placeholder="e2b_...">
      </div>

      <div class="section">Local Services</div>
      <div class="svc-row">
        <span class="svc-label">Qdrant</span>
        <span id="qdrant-badge" class="badge">checking…</span>
        <input type="text" name="qdrant_host" value="localhost" style="flex:1">
        <input type="number" name="qdrant_port" value="6334" style="width:80px">
      </div>
      <div class="svc-row">
        <span class="svc-label">Ollama</span>
        <span id="ollama-badge" class="badge">checking…</span>
        <input type="text" name="ollama_host" value="http://localhost:11434" style="flex:1">
      </div>
      <div style="margin-top:.75rem">
        <label>Embed model</label>
        <input type="text" name="embed_model" value="nomic-embed-text">
      </div>

      <div class="section">Tunables</div>
      <div class="row">
        <div>
          <label>Max files to analyse</label>
          <input type="number" name="max_files" value="50" min="1" max="500">
        </div>
        <div>
          <label>Confidence gate (0–1)</label>
          <input type="text" name="confidence_gate" value="0.80">
        </div>
      </div>
      <div class="checkbox-row">
        <input type="checkbox" name="force_mock" id="force_mock">
        <label for="force_mock" style="margin:0;color:#e6edf3">
          Force mock mode <span class="hint">(ignore all keys, run fully offline)</span>
        </label>
      </div>

      <button type="submit" class="submit-btn">▶ Start Analysis</button>
    </form>
  </div>
</div>
<script>
  fetch('/health').then(r => r.json()).then(function(d) {
    setBadge('qdrant-badge', d.qdrant, 'localhost:6333');
    setBadge('ollama-badge', d.ollama, 'localhost:11434');
  }).catch(function() {
    setBadge('qdrant-badge', false, 'localhost:6333');
    setBadge('ollama-badge', false, 'localhost:11434');
  });
  function setBadge(id, ok, addr) {
    var el = document.getElementById(id);
    el.textContent = ok ? '● Running' : '○ Not detected';
    el.className = 'badge ' + (ok ? 'ok' : 'err');
  }
</script>
</body>
</html>{{end}}`
