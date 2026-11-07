package services

import (
	"context"

	"github.com/saketh1125/argus/config"
)

// Clients is the bundle of selected adapters that the orchestrator (main) wires
// into the pipeline. Each field holds either a live or mock adapter, chosen by
// New based on credential/service availability.
type Clients struct {
	LLM     LLMClient
	Embed   EmbeddingClient
	Vectors VectorStore
	Sandbox Sandbox
	Limiter RateLimiter
	// Mode is "live" (every adapter real), "mock" (every adapter mock), or
	// "mixed" (a combination).
	Mode string
}

// New assembles the service Clients for a run. It builds the shared RateLimiter
// once, then selects each adapter:
//
//   - cfg.ForceMock forces supported integrations to their mock adapters; the
//     sandbox remains unavailable because mock mode never executes repository code.
//   - Otherwise a supported adapter is chosen live when its credential/service
//     is present and (for local services) a short probe succeeds; supported
//     mock adapters are used when not. Sandbox validation stays unavailable
//     until its provider contract, bounds, isolation and cleanup are verified.
//
// Probes use short timeouts and never abort startup. Mode is computed uniformly
// from how many selected adapters report Live().
func New(ctx context.Context, cfg *config.Config) (*Clients, error) {
	limiter := NewRateLimiter(cfg.MaxRPM)

	c := &Clients{Limiter: limiter}

	c.LLM = selectLLM(cfg, limiter)
	c.Embed = selectEmbed(ctx, cfg)
	c.Vectors = selectVectors(ctx, cfg)
	c.Sandbox = selectSandbox(cfg)

	c.Mode = computeMode(c.LLM.Live(), c.Embed.Live(), c.Vectors.Live(), c.Sandbox.Live())
	return c, nil
}

// selectLLM picks the NIM adapter when a key is configured, else the mock.
func selectLLM(cfg *config.Config, limiter RateLimiter) LLMClient {
	if cfg.ForceMock || cfg.NIMAPIKey == "" {
		return newMockLLM()
	}
	return newNIMClient(cfg.NIMBaseURL, cfg.NIMAPIKey, cfg.NIMModel, limiter)
}

// selectEmbed walks the fallback chain (ollama -> jina -> gemini) and returns
// the first available real provider, else the deterministic mock.
func selectEmbed(ctx context.Context, cfg *config.Config) EmbeddingClient {
	if cfg.ForceMock {
		return newMockEmbed()
	}
	if oe, ok := newOllamaEmbed(ctx, cfg.OllamaHost, cfg.EmbedModel); ok {
		return oe
	}
	if cfg.JinaAPIKey != "" {
		return newJinaEmbed(cfg.JinaAPIKey)
	}
	if cfg.GeminiAPIKey != "" {
		return newGeminiEmbed(cfg.GeminiAPIKey)
	}
	return newMockEmbed()
}

// selectVectors picks Qdrant when reachable, else the in-memory mock.
func selectVectors(ctx context.Context, cfg *config.Config) VectorStore {
	if cfg.ForceMock {
		return newMockVectors()
	}
	if qs, ok := newQdrantStore(ctx, cfg.QdrantHost); ok {
		return qs
	}
	return newMockVectors()
}

// selectSandbox refuses validation until the provider API, resource limits,
// isolation settings, and cleanup behaviour have been verified. An API key by
// itself is not evidence that the adapter is safe to use.
func selectSandbox(cfg *config.Config) Sandbox {
	if cfg.ForceMock {
		return newUnavailableSandbox("mock mode does not execute repository code")
	}
	if cfg.E2BAPIKey != "" {
		return newUnavailableSandbox("E2B adapter is disabled pending API and live isolation verification")
	}
	return newUnavailableSandbox("E2B_API_KEY is not configured; isolated validation is unavailable")
}

// computeMode reduces the per-adapter Live() flags to a single Mode string:
// all real -> "live", all mock -> "mock", otherwise "mixed".
func computeMode(live ...bool) string {
	real, mock := 0, 0
	for _, l := range live {
		if l {
			real++
		} else {
			mock++
		}
	}
	switch {
	case mock == 0:
		return "live"
	case real == 0:
		return "mock"
	default:
		return "mixed"
	}
}
