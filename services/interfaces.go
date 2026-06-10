// Package services abstracts every external system the pipeline talks to (LLM,
// embeddings, vector DB, code sandbox, rate limiting) behind interfaces. Each
// interface has a real adapter (live API/SDK) and a deterministic mock adapter;
// the factory in factory.go picks one per integration based on whether the
// corresponding credential/service is available.
//
// Dependency direction: services -> {models, config}. Nothing here imports
// tools or agents.
package services

import (
	"context"

	"github.com/saketh/codesentinel/models"
)

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

// RateLimiter throttles outbound API calls to stay under the provider's RPM
// ceiling. A single instance is shared across all goroutines.
type RateLimiter interface {
	// Wait blocks until a call slot is available or ctx is cancelled.
	Wait(ctx context.Context) error
	// Snapshot returns the current in-window call count and the configured max.
	Snapshot() (current, max int)
}

// ---------------------------------------------------------------------------
// LLM completion (Nvidia NIM, OpenAI-compatible)
// ---------------------------------------------------------------------------

// CompletionRequest is a single chat completion call.
type CompletionRequest struct {
	System      string
	User        string
	Temperature float64
	MaxTokens   int
	// JSONMode requests structured JSON output when the provider supports it.
	JSONMode bool
	// Model overrides the default model for this call (optional).
	Model string
	// Task is a hint identifying which pipeline step is calling (one of the
	// models.Task* constants). The real adapter ignores it; the mock adapter
	// uses it to choose which deterministic, schema-correct response to emit.
	// This lets the services author and agents author agree on behavior
	// without coordinating, since both follow the models.*Result schemas.
	Task string
}

// CompletionResponse is the model's reply.
type CompletionResponse struct {
	Text         string
	Model        string
	PromptTokens int
	OutputTokens int
}

// LLMClient performs chat completions. All calls route through the shared
// RateLimiter inside the implementation.
type LLMClient interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	// Name identifies the active adapter, e.g. "nim:llama-3.1-70b" or "mock".
	Name() string
	// Live reports whether this is a real (true) or mock (false) adapter.
	Live() bool
}

// ---------------------------------------------------------------------------
// Embeddings (Ollama -> Jina -> Gemini -> MiniLM fallback chain)
// ---------------------------------------------------------------------------

// EmbeddingClient turns text into vectors. Implementations encapsulate the
// fallback chain; callers do not choose a provider.
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim is the embedding dimensionality of the active provider.
	Dim() int
	// Name identifies the active provider, e.g. "ollama:nomic-embed-text".
	Name() string
	Live() bool
}

// ---------------------------------------------------------------------------
// Vector store (Qdrant)
// ---------------------------------------------------------------------------

// VectorStore persists and searches embeddings.
type VectorStore interface {
	EnsureCollection(ctx context.Context, name string, dim int) error
	Upsert(ctx context.Context, name string, points []models.VectorPoint) error
	Search(ctx context.Context, name string, vector []float32, topK int) ([]models.SearchResult, error)
	Name() string
	Live() bool
}

// ---------------------------------------------------------------------------
// Code sandbox (E2B)
// ---------------------------------------------------------------------------

// SandboxSpec describes a one-shot sandbox execution: write Files, then run
// Commands in order, for at most TimeoutSec seconds.
type SandboxSpec struct {
	Language   string
	Files      map[string]string // relative path -> content
	Commands   []string          // executed in order; first failure stops
	TimeoutSec int
}

// SandboxResult captures the outcome of a sandbox run.
type SandboxResult struct {
	Success  bool
	ExitCode int
	Stdout   string
	Stderr   string
	Logs     string
}

// Sandbox runs untrusted patched code in isolation to validate fixes.
type Sandbox interface {
	Run(ctx context.Context, spec SandboxSpec) (*SandboxResult, error)
	Name() string
	Live() bool
}
