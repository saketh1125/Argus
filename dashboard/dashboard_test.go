package dashboard

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/saketh/codesentinel/models"
)

// startTestDashboard spins up a dashboard on an ephemeral port and returns it,
// cleaning up on test completion.
func startTestDashboard(t *testing.T) *Dashboard {
	t.Helper()
	d := New(0)
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := d.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return d
}

func TestReporterUpdatesState(t *testing.T) {
	d := startTestDashboard(t)

	d.SetStage(models.StageAnalyze, 75)
	d.AddIssue(&models.Issue{
		BugType:    models.BugSQLInjection,
		File:       "db.go",
		LineStart:  142,
		Severity:   models.SeverityCritical,
		Confidence: 0.94,
	})
	d.SetPRsRaised(5)
	d.SetFilesProgress(42, 50)
	d.SetValidated(5, 7)
	d.SetRPM(28, 35)
	d.SetAgentStatus("Analyst-1", "analyzing auth.go")
	d.Log("started run on %s", "repo")

	d.mu.RLock()
	defer d.mu.RUnlock()
	s := &d.state

	if s.stage != models.StageAnalyze || s.stagePct != 75 {
		t.Errorf("stage = %q %d, want %q 75", s.stage, s.stagePct, models.StageAnalyze)
	}
	if s.issuesFound != 1 || s.criticalCount != 1 {
		t.Errorf("issues found=%d critical=%d, want 1/1", s.issuesFound, s.criticalCount)
	}
	if len(s.recentIssues) != 1 {
		t.Fatalf("recentIssues len = %d, want 1", len(s.recentIssues))
	}
	if got := s.recentIssues[0].Confidence; got != 94 {
		t.Errorf("confidence pct = %d, want 94", got)
	}
	if s.recentIssues[0].Icon != "🔴" {
		t.Errorf("icon = %q, want 🔴", s.recentIssues[0].Icon)
	}
	if s.prsRaised != 5 {
		t.Errorf("prsRaised = %d, want 5", s.prsRaised)
	}
	if s.filesDone != 42 || s.filesTotal != 50 {
		t.Errorf("files = %d/%d, want 42/50", s.filesDone, s.filesTotal)
	}
	if s.rpmCurrent != 28 || s.rpmMax != 35 {
		t.Errorf("rpm = %d/%d, want 28/35", s.rpmCurrent, s.rpmMax)
	}
	if got := s.agents["Analyst-1"]; got != "analyzing auth.go" {
		t.Errorf("agent status = %q", got)
	}
	if len(s.logs) != 1 || s.logs[0] != "started run on repo" {
		t.Errorf("logs = %v", s.logs)
	}
}

func TestIndexServesHTML(t *testing.T) {
	d := startTestDashboard(t)
	d.SetStage(models.StageAnalyze, 50)

	resp, err := http.Get(d.URL() + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, pageTitle) {
		t.Errorf("page missing title %q", pageTitle)
	}
	if !strings.Contains(html, "/events") {
		t.Errorf("page missing SSE wiring to /events")
	}
	if !strings.Contains(html, models.StageAnalyze) {
		t.Errorf("page missing current stage")
	}
}

func TestURLReflectsBoundPort(t *testing.T) {
	d := startTestDashboard(t)
	url := d.URL()
	if !strings.HasPrefix(url, "http://localhost:") {
		t.Errorf("URL = %q, want http://localhost:<port>", url)
	}
	if strings.HasSuffix(url, ":0") {
		t.Errorf("URL still on port 0: %q (Start should resolve ephemeral port)", url)
	}
}

func TestRecentIssuesCapped(t *testing.T) {
	d := New(0)
	for i := 0; i < maxRecentIssues+5; i++ {
		d.AddIssue(&models.Issue{BugType: "logic", File: "f.go", Severity: models.SeverityLow})
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.state.recentIssues) != maxRecentIssues {
		t.Errorf("recentIssues len = %d, want cap %d", len(d.state.recentIssues), maxRecentIssues)
	}
	if d.state.issuesFound != maxRecentIssues+5 {
		t.Errorf("issuesFound = %d, want %d", d.state.issuesFound, maxRecentIssues+5)
	}
}

func TestNilIssueIgnored(t *testing.T) {
	d := New(0)
	d.AddIssue(nil)
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.state.issuesFound != 0 {
		t.Errorf("nil issue counted: issuesFound = %d", d.state.issuesFound)
	}
}
