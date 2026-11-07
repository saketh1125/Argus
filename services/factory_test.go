package services

import (
	"context"
	"testing"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/tools"
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
	if c.Sandbox.Name() != "unavailable" {
		t.Fatalf("mock mode must not select a host executor, got sandbox %q", c.Sandbox.Name())
	}
	if c.Limiter == nil {
		t.Fatalf("limiter should be wired")
	}
}

func TestMissingE2BSelectsUnavailableSandbox(t *testing.T) {
	got := selectSandbox(&config.Config{})
	if got.Name() != "unavailable" || got.Live() {
		t.Fatalf("missing isolated provider must be reported unavailable, got %q live=%v", got.Name(), got.Live())
	}
}

func TestE2BCredentialDoesNotEnableUnverifiedSandbox(t *testing.T) {
	got := selectSandbox(&config.Config{E2BAPIKey: "test-key"})
	if got.Name() != "unavailable" || got.Live() {
		t.Fatalf("E2B credentials must not enable an unverified provider, got %q live=%v", got.Name(), got.Live())
	}
	if _, err := got.Run(context.Background(), SandboxSpec{Commands: []string{"unsafe"}}); err == nil {
		t.Fatal("unverified sandbox must fail closed")
	}
}

func TestMissingProviderCredentialsSelectVisibleMocksAndUnavailableValidation(t *testing.T) {
	cfg := &config.Config{MaxRPM: 35}
	llm := selectLLM(cfg, NewRateLimiter(cfg.MaxRPM))
	github := tools.NewGitHub(cfg)
	sandbox := selectSandbox(cfg)
	if llm.Live() || llm.Name() != "mock-llm" {
		t.Fatalf("missing model credentials must select an identifiable mock, got %q live=%v", llm.Name(), llm.Live())
	}
	if github.Live() || github.Name() != "mock-github" {
		t.Fatalf("missing GitHub credentials must select an identifiable mock, got %q live=%v", github.Name(), github.Live())
	}
	if sandbox.Live() || sandbox.Name() != "unavailable" {
		t.Fatalf("missing E2B credentials must leave validation unavailable, got %q live=%v", sandbox.Name(), sandbox.Live())
	}
	if _, err := sandbox.Run(context.Background(), SandboxSpec{Commands: []string{"unsafe"}}); err == nil {
		t.Fatal("missing sandbox credentials must never fall back to host execution")
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
