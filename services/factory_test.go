package services

import (
	"context"
	"testing"

	"github.com/saketh1125/argus/config"
)

// TestForceMockSelectsAllMocks verifies ForceMock yields the mock adapter for
// every integration and Mode "mock".
func TestForceMockSelectsAllMocks(t *testing.T) {
	cfg := &config.Config{
		ForceMock:  true,
		MaxRPM:     35,
		NIMAPIKey:  "ignored",
		E2BAPIKey:  "ignored",
		OllamaHost: "http://localhost:11434",
		QdrantHost: "localhost",
		EmbedModel: "nomic-embed-text",
		NIMBaseURL: "https://example/v1",
		NIMModel:   "test",
	}
	c, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if c.LLM.Live() || c.Embed.Live() || c.Vectors.Live() || c.Sandbox.Live() {
		t.Fatalf("ForceMock should make every adapter mock; got LLM=%v Embed=%v Vectors=%v Sandbox=%v",
			c.LLM.Live(), c.Embed.Live(), c.Vectors.Live(), c.Sandbox.Live())
	}
	if c.Mode != "mock" {
		t.Fatalf("expected Mode mock, got %q", c.Mode)
	}
	if c.Limiter == nil {
		t.Fatalf("limiter should be wired")
	}
}

// TestComputeMode covers the three mode reductions.
func TestComputeMode(t *testing.T) {
	cases := []struct {
		flags []bool
		want  string
	}{
		{[]bool{true, true, true, true}, "live"},
		{[]bool{false, false, false, false}, "mock"},
		{[]bool{true, false, true, false}, "mixed"},
	}
	for _, tc := range cases {
		if got := computeMode(tc.flags...); got != tc.want {
			t.Fatalf("computeMode(%v) = %q, want %q", tc.flags, got, tc.want)
		}
	}
}
