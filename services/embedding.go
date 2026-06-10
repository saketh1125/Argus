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
	"time"
)

// probeTimeout bounds availability probes so a missing local service degrades to
// the mock quickly instead of stalling startup.
const probeTimeout = 2 * time.Second

// mockEmbedDim is the dimensionality of the deterministic mock embedding.
const mockEmbedDim = 256

// ---------------------------------------------------------------------------
// Real adapter: Ollama (primary)
// ---------------------------------------------------------------------------

// ollamaEmbed is the live EmbeddingClient backed by a local Ollama server's
// /api/embeddings endpoint.
type ollamaEmbed struct {
	host  string
	model string
	dim   int
	http  *http.Client
}

// newOllamaEmbed probes the Ollama server at host (GET /api/tags) and, if
// reachable, returns a live adapter. It returns (nil, false) when Ollama is not
// available so the factory can fall back.
func newOllamaEmbed(ctx context.Context, host, model string) (*ollamaEmbed, bool) {
	host = strings.TrimRight(host, "/")
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pctx, http.MethodGet, host+"/api/tags", nil)
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
	return &ollamaEmbed{
		host:  host,
		model: model,
		http:  &http.Client{Timeout: 30 * time.Second},
	}, true
}

// Embed turns each input text into a vector by calling Ollama once per text.
func (o *ollamaEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		vec, err := o.embedOne(ctx, t)
		if err != nil {
			return nil, err
		}
		if o.dim == 0 {
			o.dim = len(vec)
		}
		out = append(out, vec)
	}
	return out, nil
}

func (o *ollamaEmbed) embedOne(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]string{"model": o.model, "prompt": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.host+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ollama: decode: %w", err)
	}
	if len(parsed.Embedding) == 0 {
		return nil, fmt.Errorf("ollama: empty embedding")
	}
	return parsed.Embedding, nil
}

// Dim returns the embedding dimensionality (learned from the first response).
func (o *ollamaEmbed) Dim() int {
	if o.dim == 0 {
		return 768 // nomic-embed-text default, until the first call confirms.
	}
	return o.dim
}

// Name identifies the adapter as "ollama:<model>".
func (o *ollamaEmbed) Name() string { return "ollama:" + o.model }

// Live reports that this is a real adapter.
func (o *ollamaEmbed) Live() bool { return true }

// ---------------------------------------------------------------------------
// Real adapter: Jina AI (fallback)
// ---------------------------------------------------------------------------

// jinaEmbed is a thin live EmbeddingClient for the Jina embeddings API.
type jinaEmbed struct {
	apiKey string
	model  string
	dim    int
	http   *http.Client
}

// newJinaEmbed builds a Jina adapter (no network probe; selected when a key is
// present and Ollama is unavailable).
func newJinaEmbed(apiKey string) *jinaEmbed {
	return &jinaEmbed{
		apiKey: apiKey,
		model:  "jina-embeddings-v2-base-en",
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Embed sends all texts in one batched request to the Jina API.
func (j *jinaEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": j.model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.jina.ai/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("jina: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+j.apiKey)
	resp, err := j.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jina: http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jina: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("jina: decode: %w", err)
	}
	out := make([][]float32, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		if j.dim == 0 {
			j.dim = len(d.Embedding)
		}
		out = append(out, d.Embedding)
	}
	return out, nil
}

// Dim returns the embedding dimensionality (768 for the base-en model).
func (j *jinaEmbed) Dim() int {
	if j.dim == 0 {
		return 768
	}
	return j.dim
}

// Name identifies the adapter.
func (j *jinaEmbed) Name() string { return "jina:" + j.model }

// Live reports that this is a real adapter.
func (j *jinaEmbed) Live() bool { return true }

// ---------------------------------------------------------------------------
// Real adapter: Gemini (fallback)
// ---------------------------------------------------------------------------

// geminiEmbed is a thin live EmbeddingClient for Google's Gemini embedding API.
type geminiEmbed struct {
	apiKey string
	model  string
	dim    int
	http   *http.Client
}

// newGeminiEmbed builds a Gemini adapter (no network probe; selected when a key
// is present and earlier providers are unavailable).
func newGeminiEmbed(apiKey string) *geminiEmbed {
	return &geminiEmbed{
		apiKey: apiKey,
		model:  "models/text-embedding-004",
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Embed calls the Gemini embedContent endpoint once per text.
func (g *geminiEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s:embedContent?key=%s", g.model, g.apiKey)
	for _, t := range texts {
		body, _ := json.Marshal(map[string]any{
			"model":   g.model,
			"content": map[string]any{"parts": []map[string]string{{"text": t}}},
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("gemini: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := g.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("gemini: http: %w", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("gemini: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}
		var parsed struct {
			Embedding struct {
				Values []float32 `json:"values"`
			} `json:"embedding"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("gemini: decode: %w", err)
		}
		if g.dim == 0 {
			g.dim = len(parsed.Embedding.Values)
		}
		out = append(out, parsed.Embedding.Values)
	}
	return out, nil
}

// Dim returns the embedding dimensionality (768 for text-embedding-004).
func (g *geminiEmbed) Dim() int {
	if g.dim == 0 {
		return 768
	}
	return g.dim
}

// Name identifies the adapter.
func (g *geminiEmbed) Name() string { return "gemini:" + g.model }

// Live reports that this is a real adapter.
func (g *geminiEmbed) Live() bool { return true }

// ---------------------------------------------------------------------------
// Mock adapter: deterministic pseudo-embedding
// ---------------------------------------------------------------------------

// mockEmbed produces deterministic, L2-normalized pseudo-embeddings derived from
// a token hash, so cosine similarity is meaningful and offline runs reproduce.
type mockEmbed struct{}

// newMockEmbed returns the offline embedding adapter.
func newMockEmbed() *mockEmbed { return &mockEmbed{} }

// Embed maps each text to a deterministic 256-dim unit vector.
func (m *mockEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = pseudoEmbed(t)
	}
	return out, nil
}

// pseudoEmbed hashes each whitespace-delimited token into the vector space,
// accumulating per-token FNV-derived contributions, then L2-normalizes.
func pseudoEmbed(text string) []float32 {
	vec := make([]float32, mockEmbedDim)
	tokens := strings.Fields(strings.ToLower(text))
	if len(tokens) == 0 {
		tokens = []string{text}
	}
	for _, tok := range tokens {
		h := fnv.New64a()
		h.Write([]byte(tok))
		seed := h.Sum64()
		// Spread each token across a handful of dimensions for stability.
		for k := 0; k < 8; k++ {
			seed = seed*6364136223846793005 + 1442695040888963407 // LCG step
			idx := int(seed % mockEmbedDim)
			// Map high bits to a signed unit contribution.
			val := float32(int64(seed>>33)%1000)/500.0 - 1.0
			vec[idx] += val
		}
	}
	// L2-normalize.
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	norm := math.Sqrt(sum)
	if norm == 0 {
		vec[0] = 1
		return vec
	}
	for i := range vec {
		vec[i] = float32(float64(vec[i]) / norm)
	}
	return vec
}

// Dim returns the fixed mock dimensionality (256).
func (m *mockEmbed) Dim() int { return mockEmbedDim }

// Name identifies the mock adapter.
func (m *mockEmbed) Name() string { return "mock-embed" }

// Live reports that this is NOT a real adapter.
func (m *mockEmbed) Live() bool { return false }
