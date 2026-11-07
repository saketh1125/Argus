package dashboard

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func launcherRequest(t *testing.T, d *Dashboard, origin, host, token string) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{
		"csrf_token": {token},
		"repo_url":   {"./testdata/sample-repo"},
	}
	req := httptest.NewRequest(http.MethodPost, d.URL()+"/start", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if host != "" {
		req.Host = host
	}
	w := httptest.NewRecorder()
	d.handleStart(w, req)
	return w
}

func armLauncher(t *testing.T) (*Dashboard, <-chan LaunchConfig, string) {
	t.Helper()
	d := New(0)
	ch, err := d.ListenForLaunch()
	if err != nil {
		t.Fatalf("ListenForLaunch: %v", err)
	}
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
	d.mu.RLock()
	token := d.launchToken
	d.mu.RUnlock()
	if token == "" {
		t.Fatal("launcher token must be generated")
	}
	return d, ch, token
}

func TestLauncherRejectsCrossSiteAndReboundHostRequests(t *testing.T) {
	d, ch, token := armLauncher(t)
	port := strconv.Itoa(d.srv.port)

	if got := launcherRequest(t, d, "http://attacker.example:"+port, "localhost:"+port, token).Code; got != http.StatusForbidden {
		t.Errorf("cross-site Origin status = %d, want %d", got, http.StatusForbidden)
	}
	if got := launcherRequest(t, d, "http://attacker.example:"+port, "attacker.example:"+port, token).Code; got != http.StatusForbidden {
		t.Errorf("rebound Host status = %d, want %d", got, http.StatusForbidden)
	}
	if len(ch) != 0 {
		t.Fatal("rejected cross-site requests must not launch analysis")
	}
}

func TestLauncherRejectsQueryTokenAndOversizedRequestBody(t *testing.T) {
	d, ch, token := armLauncher(t)
	values := url.Values{"repo_url": {"./testdata/sample-repo"}}
	req := httptest.NewRequest(http.MethodPost, d.URL()+"/start?csrf_token="+url.QueryEscape(token), strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", d.URL())
	w := httptest.NewRecorder()
	d.handleStart(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("query-string token status = %d, want %d", w.Code, http.StatusForbidden)
	}

	largeBody := strings.Repeat("x", (16<<10)+1)
	req = httptest.NewRequest(http.MethodPost, d.URL()+"/start", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", d.URL())
	w = httptest.NewRecorder()
	d.handleStart(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("oversized form status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if len(ch) != 0 {
		t.Fatal("invalid or oversized requests must not launch analysis")
	}
}

func TestLauncherRequiresOneTimeTokenAndAcceptsExactlyOneRequest(t *testing.T) {
	d, ch, token := armLauncher(t)
	port := strconv.Itoa(d.srv.port)
	origin := d.URL()
	host := "localhost:" + port

	if got := launcherRequest(t, d, origin, host, "").Code; got != http.StatusForbidden {
		t.Errorf("missing token status = %d, want %d", got, http.StatusForbidden)
	}
	if got := launcherRequest(t, d, origin, host, token).Code; got != http.StatusSeeOther {
		t.Fatalf("valid launch status = %d, want %d", got, http.StatusSeeOther)
	}
	select {
	case cfg := <-ch:
		if cfg.RepoURL != "./testdata/sample-repo" {
			t.Errorf("unexpected repository submitted")
		}
	default:
		t.Fatal("valid launch was not delivered")
	}
	if got := launcherRequest(t, d, origin, host, token).Code; got != http.StatusForbidden {
		t.Errorf("replayed token status = %d, want %d", got, http.StatusForbidden)
	}
	if len(ch) != 0 {
		t.Fatal("launcher accepted more than one analysis request")
	}
}

func TestLauncherConcurrentSubmissionsAcceptOnlyOneRequest(t *testing.T) {
	d, ch, token := armLauncher(t)
	origin := d.URL()
	host := "localhost:" + strconv.Itoa(d.srv.port)
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = launcherRequest(t, d, origin, host, token).Code
		}(i)
	}
	wg.Wait()

	accepted := 0
	for _, status := range statuses {
		if status == http.StatusSeeOther {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d concurrent launches; want exactly one (statuses %v)", accepted, statuses)
	}
	if len(ch) != 1 {
		t.Fatalf("queued %d launches; want exactly one", len(ch))
	}
	<-ch
}

func TestLauncherFormUsesUncachedCSRFAndDoesNotRequestDisabledE2BKey(t *testing.T) {
	d, _, _ := armLauncher(t)
	resp, err := http.Get(d.URL() + "/")
	if err != nil {
		t.Fatalf("GET launcher: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	page := string(body)
	if resp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(page, "name=\"csrf_token\" value=\"") {
		t.Fatal("launcher must serve an uncached form with a CSRF token")
	}
	if strings.Contains(page, "name=\"e2b_api_key\"") || strings.Contains(page, "blank = local syntax check") {
		t.Fatal("launcher must not request a key for the disabled sandbox or promise a host fallback")
	}
}

func TestLauncherCannotBeArmedTwice(t *testing.T) {
	d := New(0)
	if _, err := d.ListenForLaunch(); err != nil {
		t.Fatalf("first ListenForLaunch: %v", err)
	}
	if _, err := d.ListenForLaunch(); err == nil {
		t.Fatal("second ListenForLaunch must fail")
	}
}

func TestLauncherOriginMustUseItsActualLocalPort(t *testing.T) {
	d, ch, token := armLauncher(t)
	port := d.srv.port
	wrongOrigin := fmt.Sprintf("http://localhost:%d", port+1)
	if got := launcherRequest(t, d, wrongOrigin, d.URL()[len("http://"):], token).Code; got != http.StatusForbidden {
		t.Errorf("wrong-port Origin status = %d, want %d", got, http.StatusForbidden)
	}
	if len(ch) != 0 {
		t.Fatal("wrong-port Origin must not launch analysis")
	}
}
