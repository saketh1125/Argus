// Package dashboard serves CodeSentinel's real-time pipeline dashboard. It
// implements models.Reporter: every progress event the pipeline emits mutates
// in-memory state (guarded by a sync.RWMutex) and is pushed to connected
// browsers over Server-Sent Events, so the page updates live without a reload
// (PRD FR-09, design §10).
//
// The dashboard is a single self-contained package depending only on the leaf
// models package. The HTTP server, SSE plumbing, and HTML template live in
// server.go and templates.go respectively.
package dashboard

import (
	"fmt"
	"sync"

	"github.com/saketh/codesentinel/models"
)

// maxRecentIssues caps the "Issues Detected" panel so a long run does not grow
// the rendered fragment without bound. Newest issues are kept.
const maxRecentIssues = 12

// maxLogLines caps the rolling log buffer. Oldest lines are dropped.
const maxLogLines = 50

// recentIssue is the trimmed view of an Issue shown in the issues panel.
type recentIssue struct {
	Icon       string // severity glyph: 🔴 🟠 🟡 ⚪
	BugType    string
	File       string
	Line       int
	Confidence int // percent, 0–100
}

// agentLine pairs an agent name with its latest status string. Stored as a
// slice (not just the map) so the rendered order is stable and deterministic.
type agentLine struct {
	Name   string
	Status string
}

// state is the full set of values rendered into the live panel. It is the
// single source of truth, mutated only while holding Dashboard.mu's write lock.
type state struct {
	stage    string
	stagePct int

	filesDone  int
	filesTotal int

	issuesFound   int
	criticalCount int
	highCount     int
	recentIssues  []recentIssue // newest last

	prsRaised int

	validatedDone  int
	validatedTotal int

	rpmCurrent int
	rpmMax     int

	agentOrder []string          // insertion order of agent names
	agents     map[string]string // name -> status

	logs []string // rolling, oldest first
}

// Dashboard is the live progress sink and HTTP server. It is safe for
// concurrent use: Reporter methods may be called from many analyst goroutines
// in parallel, and SSE clients connect/disconnect independently.
type Dashboard struct {
	// mu guards state. Reporter methods take the write lock to mutate; the
	// renderer takes the read lock.
	mu    sync.RWMutex
	state state

	// launched is set to true once the user submits the launcher form.
	// handleIndex serves the launcher until this is true.
	launched bool
	// launchCh is armed by ListenForLaunch; nil means web-launcher not in use.
	launchCh chan LaunchConfig

	// srv holds the HTTP server, listener, and SSE client registry (server.go).
	srv *httpServer
}

// Compile-time assertion that Dashboard satisfies the pipeline's Reporter sink.
var _ models.Reporter = (*Dashboard)(nil)

// New creates a dashboard bound to the given TCP port. Port 0 selects an
// ephemeral port, resolved when Start binds the listener. The HTTP server is
// not started until Start is called.
func New(port int) *Dashboard {
	d := &Dashboard{
		state: state{
			stage:  "Starting",
			rpmMax: 0,
			agents: make(map[string]string),
		},
	}
	d.srv = newHTTPServer(d, port)
	return d
}

// SetStage records the current pipeline stage and its completion percentage
// (clamped to 0–100), then broadcasts the change.
func (d *Dashboard) SetStage(stage string, pct int) {
	d.mu.Lock()
	d.state.stage = stage
	d.state.stagePct = clampPct(pct)
	d.mu.Unlock()
	d.broadcast()
}

// SetFilesProgress records how many files have been processed out of the total.
func (d *Dashboard) SetFilesProgress(done, total int) {
	d.mu.Lock()
	d.state.filesDone = done
	d.state.filesTotal = total
	d.mu.Unlock()
	d.broadcast()
}

// AddIssue records a newly detected issue: it bumps the found/severity counters
// and appends a trimmed view to the capped recent-issues list.
func (d *Dashboard) AddIssue(issue *models.Issue) {
	if issue == nil {
		return
	}
	d.mu.Lock()
	d.state.issuesFound++
	switch issue.Severity {
	case models.SeverityCritical:
		d.state.criticalCount++
	case models.SeverityHigh:
		d.state.highCount++
	}
	ri := recentIssue{
		Icon:       severityIcon(issue.Severity),
		BugType:    issue.BugType,
		File:       issue.File,
		Line:       issue.LineStart,
		Confidence: confidencePct(issue.Confidence),
	}
	d.state.recentIssues = append(d.state.recentIssues, ri)
	if len(d.state.recentIssues) > maxRecentIssues {
		// Drop the oldest, keep the newest maxRecentIssues.
		d.state.recentIssues = d.state.recentIssues[len(d.state.recentIssues)-maxRecentIssues:]
	}
	d.mu.Unlock()
	d.broadcast()
}

// SetPRsRaised records the number of pull requests opened so far.
func (d *Dashboard) SetPRsRaised(n int) {
	d.mu.Lock()
	d.state.prsRaised = n
	d.mu.Unlock()
	d.broadcast()
}

// SetValidated records validation progress (validated done out of total).
func (d *Dashboard) SetValidated(done, total int) {
	d.mu.Lock()
	d.state.validatedDone = done
	d.state.validatedTotal = total
	d.mu.Unlock()
	d.broadcast()
}

// SetRPM records current vs. maximum requests-per-minute usage against the API
// rate limit.
func (d *Dashboard) SetRPM(current, max int) {
	d.mu.Lock()
	d.state.rpmCurrent = current
	d.state.rpmMax = max
	d.mu.Unlock()
	d.broadcast()
}

// SetAgentStatus records a per-agent status line (e.g. "Analyst-1" ->
// "analyzing auth.go"). First sighting of an agent fixes its display order.
func (d *Dashboard) SetAgentStatus(agent, status string) {
	d.mu.Lock()
	if _, ok := d.state.agents[agent]; !ok {
		d.state.agentOrder = append(d.state.agentOrder, agent)
	}
	d.state.agents[agent] = status
	d.mu.Unlock()
	d.broadcast()
}

// Log appends a formatted line to the rolling log buffer (last maxLogLines
// lines are retained).
func (d *Dashboard) Log(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	d.mu.Lock()
	d.state.logs = append(d.state.logs, line)
	if len(d.state.logs) > maxLogLines {
		d.state.logs = d.state.logs[len(d.state.logs)-maxLogLines:]
	}
	d.mu.Unlock()
	d.broadcast()
}

// clampPct constrains a percentage to the 0–100 range.
func clampPct(pct int) int {
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// severityIcon maps a severity to its panel glyph.
func severityIcon(s models.Severity) string {
	switch s {
	case models.SeverityCritical:
		return "🔴"
	case models.SeverityHigh:
		return "🟠"
	case models.SeverityMedium:
		return "🟡"
	default:
		return "⚪"
	}
}

// confidencePct converts a 0–1 confidence (the convention used throughout the
// pipeline, e.g. 0.94) to an integer percentage. Values already on a 0–100
// scale, or noisy >1 values, are clamped so the panel never shows nonsense.
// Flip this single function if the upstream convention ever changes.
func confidencePct(conf float64) int {
	pct := int(conf*100 + 0.5)
	if conf > 1 { // already a percentage, or out of range
		pct = int(conf + 0.5)
	}
	return clampPct(pct)
}
