package dashboard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// LaunchConfig is the configuration submitted via the launcher form.
// main.go merges it into config.Config before wiring services.
type LaunchConfig struct {
	RepoURL        string
	GitHubToken    string
	NIMAPIKey      string
	E2BAPIKey      string
	OllamaHost     string
	QdrantHost     string
	QdrantPort     int
	EmbedModel     string
	ForceMock      bool
	MaxFiles       int
	ConfidenceGate float64
}

// ListenForLaunch arms the launcher form and returns a channel that receives
// exactly one LaunchConfig when the user clicks "Start Analysis".
// Must be called before Start().
func (d *Dashboard) ListenForLaunch() <-chan LaunchConfig {
	ch := make(chan LaunchConfig, 1)
	d.mu.Lock()
	d.launchCh = ch
	d.mu.Unlock()
	return ch
}

// handleStart processes POST /start from the launcher form.
func (d *Dashboard) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	maxFiles, _ := strconv.Atoi(r.FormValue("max_files"))
	if maxFiles <= 0 {
		maxFiles = 50
	}
	gate, _ := strconv.ParseFloat(r.FormValue("confidence_gate"), 64)
	if gate <= 0 || gate > 1 {
		gate = 0.80
	}
	qdrantPort, _ := strconv.Atoi(r.FormValue("qdrant_port"))
	if qdrantPort <= 0 {
		qdrantPort = 6334
	}

	lc := LaunchConfig{
		RepoURL:        r.FormValue("repo_url"),
		GitHubToken:    r.FormValue("github_token"),
		NIMAPIKey:      r.FormValue("nim_api_key"),
		E2BAPIKey:      r.FormValue("e2b_api_key"),
		OllamaHost:     strOr(r.FormValue("ollama_host"), "http://localhost:11434"),
		QdrantHost:     strOr(r.FormValue("qdrant_host"), "localhost"),
		QdrantPort:     qdrantPort,
		EmbedModel:     strOr(r.FormValue("embed_model"), "nomic-embed-text"),
		ForceMock:      r.FormValue("force_mock") == "on",
		MaxFiles:       maxFiles,
		ConfidenceGate: gate,
	}

	d.mu.Lock()
	d.launched = true
	ch := d.launchCh
	d.mu.Unlock()

	if ch != nil {
		ch <- lc
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleHealth returns JSON reporting whether Qdrant and Ollama are reachable.
// The launcher form fetches this on load to show live service status badges.
func (d *Dashboard) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	qdrant := probeTCP("localhost:6333", 600*time.Millisecond)
	ollama := probeHTTP("http://localhost:11434/api/tags", 600*time.Millisecond)
	fmt.Fprintf(w, `{"qdrant":%v,"ollama":%v}`, qdrant, ollama)
}

// probeTCP dials addr and returns true if the connection succeeds within timeout.
func probeTCP(addr string, timeout time.Duration) bool {
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// probeHTTP GETs url and returns true if the status is 2xx within timeout.
func probeHTTP(url string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode < 300
}

func strOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
