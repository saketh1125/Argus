package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LaunchConfig is the configuration submitted via the launcher form.
// main.go merges it into config.Config before wiring services.
type LaunchConfig struct {
	RepoURL        string
	NIMAPIKey      string
	JinaAPIKey     string
	GeminiAPIKey   string
	OllamaHost     string
	QdrantHost     string
	QdrantPort     int
	EmbedModel     string
	ForceMock      bool
	MaxFiles       int
	ConfidenceGate float64
}

// ListenForLaunch arms the launcher form and returns a channel that receives
// exactly one LaunchConfig when the user clicks "Start Analysis". The form is
// protected by a random one-time token. Must be called before Start().
func (d *Dashboard) ListenForLaunch() (<-chan LaunchConfig, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("dashboard: create launcher request token: %w", err)
	}
	ch := make(chan LaunchConfig, 1)
	d.mu.Lock()
	if d.launchCh != nil {
		d.mu.Unlock()
		return nil, fmt.Errorf("dashboard: launcher is already armed")
	}
	d.launchCh = ch
	d.launchToken = base64.RawURLEncoding.EncodeToString(tokenBytes)
	d.mu.Unlock()
	return ch, nil
}

// handleStart processes POST /start from the launcher form.
func (d *Dashboard) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !validLocalBrowserOrigin(r, d.srv.port) {
		http.Error(w, "request origin is not allowed", http.StatusForbidden)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		http.Error(w, "unsupported form content type", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	d.mu.RLock()
	tokenOK := d.launchToken != "" && subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(d.launchToken)) == 1
	launchCh := d.launchCh
	alreadyLaunched := d.launched
	d.mu.RUnlock()
	if !tokenOK {
		http.Error(w, "invalid launcher request token", http.StatusForbidden)
		return
	}
	if alreadyLaunched || launchCh == nil {
		http.Error(w, "launcher request already handled", http.StatusConflict)
		return
	}

	maxFiles, _ := strconv.Atoi(r.PostForm.Get("max_files"))
	if maxFiles <= 0 {
		maxFiles = 50
	}
	gate, _ := strconv.ParseFloat(r.PostForm.Get("confidence_gate"), 64)
	if gate <= 0 || gate > 1 {
		gate = 0.80
	}
	qdrantPort, _ := strconv.Atoi(r.PostForm.Get("qdrant_port"))
	if qdrantPort <= 0 {
		qdrantPort = 6334
	}

	lc := LaunchConfig{
		RepoURL:        r.PostForm.Get("repo_url"),
		NIMAPIKey:      r.PostForm.Get("nim_api_key"),
		JinaAPIKey:     r.PostForm.Get("jina_api_key"),
		GeminiAPIKey:   r.PostForm.Get("gemini_api_key"),
		OllamaHost:     strOr(r.PostForm.Get("ollama_host"), "http://localhost:11434"),
		QdrantHost:     strOr(r.PostForm.Get("qdrant_host"), "localhost"),
		QdrantPort:     qdrantPort,
		EmbedModel:     strOr(r.PostForm.Get("embed_model"), "nomic-embed-text"),
		ForceMock:      r.PostForm.Get("force_mock") == "on",
		MaxFiles:       maxFiles,
		ConfidenceGate: gate,
	}

	d.mu.Lock()
	if d.launched || d.launchCh != launchCh || d.launchToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(d.launchToken)) != 1 {
		d.mu.Unlock()
		http.Error(w, "launcher request already handled", http.StatusConflict)
		return
	}
	select {
	case launchCh <- lc:
		d.launched = true
		d.launchToken = ""
	default:
		d.mu.Unlock()
		http.Error(w, "launcher is not accepting requests", http.StatusConflict)
		return
	}
	d.mu.Unlock()

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func validLocalBrowserOrigin(r *http.Request, port int) bool {
	requestHost, ok := localOriginHost(r.Host, port)
	if !ok {
		return false
	}
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || origin.Scheme != "http" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	originHost, ok := localOriginHost(origin.Host, port)
	return ok && strings.EqualFold(requestHost, originHost)
}

func localOriginHost(host string, port int) (string, bool) {
	name, portText, err := net.SplitHostPort(host)
	if err != nil || name == "" {
		return "", false
	}
	parsedPort, err := strconv.Atoi(portText)
	if err != nil || parsedPort != port {
		return "", false
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if name != "localhost" && name != "127.0.0.1" {
		return "", false
	}
	return net.JoinHostPort(name, portText), true
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
