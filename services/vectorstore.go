package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/saketh1125/argus/models"
)

// ---------------------------------------------------------------------------
// Real adapter: Qdrant over REST
// ---------------------------------------------------------------------------

// qdrantStore is the live VectorStore backed by Qdrant's REST API. Note that the
// REST port is always 6333 (cfg.QdrantPort is the separate gRPC port and is not
// used here).
type qdrantStore struct {
	baseURL string
	http    *http.Client
}

// newQdrantStore probes Qdrant (GET /collections) at host:6333 and returns a
// live adapter if reachable, else (nil, false) so the factory can fall back.
func newQdrantStore(ctx context.Context, host string) (*qdrantStore, bool) {
	base := fmt.Sprintf("http://%s:6333", host)
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pctx, http.MethodGet, base+"/collections", nil)
	if err != nil {
		return nil, false
	}
	resp, err := (&http.Client{Timeout: probeTimeout}).Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false
	}
	return &qdrantStore{
		baseURL: base,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, true
}

// doJSON issues a JSON request and returns the response body, failing on non-2xx.
func (q *qdrantStore) doJSON(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.baseURL+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return out, resp.StatusCode, nil
}

// EnsureCollection creates the collection with the given dimensionality and
// cosine distance. An already-existing collection is treated as success.
func (q *qdrantStore) EnsureCollection(ctx context.Context, name string, dim int) error {
	body := map[string]any{
		"vectors": map[string]any{"size": dim, "distance": "Cosine"},
	}
	raw, status, err := q.doJSON(ctx, http.MethodPut, "/collections/"+name, body)
	if err != nil {
		return fmt.Errorf("qdrant: ensure collection: %w", err)
	}
	if status >= 200 && status < 300 {
		return nil
	}
	// Already-exists manifests as a 4xx mentioning "exist"; treat as success.
	if strings.Contains(strings.ToLower(string(raw)), "exist") {
		return nil
	}
	return fmt.Errorf("qdrant: ensure collection status %d: %s", status, strings.TrimSpace(string(raw)))
}

// stableID maps an arbitrary string ID to a stable uint64 (Qdrant point IDs must
// be unsigned ints or UUIDs); the original string is preserved in the payload.
func stableID(id string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(id))
	return h.Sum64()
}

// Upsert writes the points into the collection (waiting for indexing). Each
// point's original string ID is mapped to a stable uint64 and also stored in the
// payload under "_id" so Search can recover it.
func (q *qdrantStore) Upsert(ctx context.Context, name string, points []models.VectorPoint) error {
	qp := make([]map[string]any, 0, len(points))
	for _, p := range points {
		payload := map[string]any{}
		for k, v := range p.Payload {
			payload[k] = v
		}
		payload["_id"] = p.ID
		qp = append(qp, map[string]any{
			"id":      stableID(p.ID),
			"vector":  p.Vector,
			"payload": payload,
		})
	}
	body := map[string]any{"points": qp}
	raw, status, err := q.doJSON(ctx, http.MethodPut, "/collections/"+name+"/points?wait=true", body)
	if err != nil {
		return fmt.Errorf("qdrant: upsert: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("qdrant: upsert status %d: %s", status, strings.TrimSpace(string(raw)))
	}
	return nil
}

// Search runs a cosine similarity search and maps results back to
// models.SearchResult, recovering the original string ID from the payload.
func (q *qdrantStore) Search(ctx context.Context, name string, vector []float32, topK int) ([]models.SearchResult, error) {
	body := map[string]any{
		"vector":       vector,
		"limit":        topK,
		"with_payload": true,
	}
	raw, status, err := q.doJSON(ctx, http.MethodPost, "/collections/"+name+"/points/search", body)
	if err != nil {
		return nil, fmt.Errorf("qdrant: search: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("qdrant: search status %d: %s", status, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Result []struct {
			ID      any            `json:"id"`
			Score   float32        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("qdrant: decode search: %w", err)
	}
	out := make([]models.SearchResult, 0, len(parsed.Result))
	for _, r := range parsed.Result {
		id := ""
		if v, ok := r.Payload["_id"].(string); ok {
			id = v
		}
		out = append(out, models.SearchResult{ID: id, Score: r.Score, Payload: r.Payload})
	}
	return out, nil
}

// Name identifies the adapter.
func (q *qdrantStore) Name() string { return "qdrant" }

// Live reports that this is a real adapter.
func (q *qdrantStore) Live() bool { return true }

// ---------------------------------------------------------------------------
// Mock adapter: in-memory cosine store
// ---------------------------------------------------------------------------

// mockVectors is a thread-safe in-memory VectorStore. It keeps points per
// collection and answers searches by brute-force cosine similarity — enough for
// offline runs and tests.
type mockVectors struct {
	mu          sync.RWMutex
	collections map[string][]models.VectorPoint
}

// newMockVectors returns the offline vector store adapter.
func newMockVectors() *mockVectors {
	return &mockVectors{collections: make(map[string][]models.VectorPoint)}
}

// EnsureCollection registers an empty collection if absent.
func (s *mockVectors) EnsureCollection(_ context.Context, name string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.collections[name]; !ok {
		s.collections[name] = nil
	}
	return nil
}

// Upsert appends or replaces points by ID within a collection.
func (s *mockVectors) Upsert(_ context.Context, name string, points []models.VectorPoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.collections[name]
	byID := make(map[string]int, len(existing))
	for i, p := range existing {
		byID[p.ID] = i
	}
	for _, p := range points {
		if i, ok := byID[p.ID]; ok {
			existing[i] = p
		} else {
			byID[p.ID] = len(existing)
			existing = append(existing, p)
		}
	}
	s.collections[name] = existing
	return nil
}

// Search returns the topK points by cosine similarity to vector.
func (s *mockVectors) Search(_ context.Context, name string, vector []float32, topK int) ([]models.SearchResult, error) {
	s.mu.RLock()
	points := s.collections[name]
	s.mu.RUnlock()

	type scored struct {
		p     models.VectorPoint
		score float32
	}
	scoredPts := make([]scored, 0, len(points))
	for _, p := range points {
		scoredPts = append(scoredPts, scored{p: p, score: cosine(vector, p.Vector)})
	}
	// Selection sort for the top-K (collections are small in offline runs).
	for i := 0; i < len(scoredPts) && i < topK; i++ {
		best := i
		for j := i + 1; j < len(scoredPts); j++ {
			if scoredPts[j].score > scoredPts[best].score {
				best = j
			}
		}
		scoredPts[i], scoredPts[best] = scoredPts[best], scoredPts[i]
	}
	n := topK
	if n > len(scoredPts) {
		n = len(scoredPts)
	}
	out := make([]models.SearchResult, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, models.SearchResult{
			ID:      scoredPts[i].p.ID,
			Score:   scoredPts[i].score,
			Payload: scoredPts[i].p.Payload,
		})
	}
	return out, nil
}

// Name identifies the mock adapter.
func (s *mockVectors) Name() string { return "mock-vectors" }

// Live reports that this is NOT a real adapter.
func (s *mockVectors) Live() bool { return false }

// cosine computes the cosine similarity between two equal-length vectors; it
// returns 0 if either is empty, zero-magnitude, or mismatched in length.
func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
