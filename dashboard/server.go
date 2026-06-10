package dashboard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// clientBuffer is the per-client SSE channel depth. A few frames of slack let a
// briefly-slow browser keep up; once full, broadcasts to that client are
// dropped (the next frame carries the full current state anyway).
const clientBuffer = 8

// httpServer holds the net/http machinery and the SSE client registry. It is a
// separate type from Dashboard purely to keep dashboard.go focused on state.
type httpServer struct {
	port int
	srv  *http.Server

	// quit is closed by Stop to release any SSE handlers still blocked on their
	// channel, so graceful Shutdown does not hang on a live /events connection.
	quit     chan struct{}
	quitOnce sync.Once

	// mu guards clients. A client is a buffered channel receiving rendered SSE
	// payloads. Channels are never closed on disconnect — the handler simply
	// deregisters and returns — so a concurrent broadcast can never send on a
	// closed channel.
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// newHTTPServer builds the server bound (logically) to port; the listener is
// created in Start.
func newHTTPServer(d *Dashboard, port int) *httpServer {
	h := &httpServer{
		port:    port,
		quit:    make(chan struct{}),
		clients: make(map[chan []byte]struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", d.handleIndex)
	mux.HandleFunc("/start", d.handleStart)
	mux.HandleFunc("/health", d.handleHealth)
	mux.HandleFunc("/events", d.handleEvents)
	h.srv = &http.Server{Handler: mux}
	return h
}

// Start binds the TCP listener (so the caller knows the port is ready and can
// read URL) and serves HTTP in a background goroutine. It returns an error only
// if the port cannot be bound.
func (d *Dashboard) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", d.srv.port))
	if err != nil {
		return fmt.Errorf("dashboard: bind port %d: %w", d.srv.port, err)
	}
	// Resolve the actual port (matters when port 0 was requested).
	d.srv.port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		// Serve returns ErrServerClosed on graceful Shutdown; that is expected.
		_ = d.srv.srv.Serve(ln)
	}()
	return nil
}

// URL returns the dashboard's base URL, e.g. "http://localhost:8080".
func (d *Dashboard) URL() string {
	return fmt.Sprintf("http://localhost:%d", d.srv.port)
}

// Stop gracefully shuts the server down. It first releases any SSE handlers
// (which otherwise hold their connections open and would block Shutdown until
// ctx expires), then waits for in-flight requests to drain or ctx to cancel.
func (d *Dashboard) Stop(ctx context.Context) error {
	d.srv.quitOnce.Do(func() { close(d.srv.quit) })
	return d.srv.srv.Shutdown(ctx)
}

// handleIndex serves the launcher form before the pipeline starts, then
// switches to the live pipeline panel once launched.
func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	d.mu.RLock()
	showLauncher := d.launchCh != nil && !d.launched
	d.mu.RUnlock()

	if showLauncher {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "launcher-page", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	panel := d.renderPanel()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := d.renderPage(w, panel); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleEvents is the SSE endpoint. It registers a per-client channel, sends the
// current panel immediately, then streams a fresh panel on every state change
// until the client disconnects or the server stops.
func (d *Dashboard) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, clientBuffer)
	d.registerClient(ch)
	defer d.unregisterClient(ch)

	// Send the current state right away so a fresh tab is not blank until the
	// next update.
	writeSSE(w, d.renderPanel())
	flusher.Flush()

	for {
		select {
		case data := <-ch:
			writeSSE(w, data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-d.srv.quit:
			return
		}
	}
}

// registerClient adds an SSE channel to the broadcast registry.
func (d *Dashboard) registerClient(ch chan []byte) {
	d.srv.mu.Lock()
	d.srv.clients[ch] = struct{}{}
	d.srv.mu.Unlock()
}

// unregisterClient removes an SSE channel from the registry. The channel is not
// closed, so a concurrent broadcast in flight cannot panic.
func (d *Dashboard) unregisterClient(ch chan []byte) {
	d.srv.mu.Lock()
	delete(d.srv.clients, ch)
	d.srv.mu.Unlock()
}

// broadcast renders the current panel once and fans it out to every connected
// client with a non-blocking send. A client whose buffer is full is skipped:
// it will receive the next frame, which already reflects the latest state.
//
// The render happens without holding the client-registry lock, and sends are
// non-blocking, so a slow or stalled browser can never stall a pipeline
// goroutine.
func (d *Dashboard) broadcast() {
	if d.srv == nil {
		return
	}
	data := d.renderPanel()
	d.srv.mu.Lock()
	for ch := range d.srv.clients {
		select {
		case ch <- data:
		default:
		}
	}
	d.srv.mu.Unlock()
}

// writeSSE frames a (possibly multi-line) payload as a single SSE event. Each
// line of the payload gets its own "data: " prefix — required because the SSE
// wire format is line-oriented and a bare multi-line write would drop all but
// the first line — followed by a blank line terminating the event.
func writeSSE(w http.ResponseWriter, data []byte) {
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(data[start:i])
			_, _ = w.Write([]byte("\n"))
			start = i + 1
		}
	}
	// Trailing segment with no newline.
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data[start:])
	_, _ = w.Write([]byte("\n\n"))
}
