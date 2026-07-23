// Package config loads runtime configuration from environment variables and an
// optional .env file. It is a leaf package (depends on nothing internal) so any
// other package may import it.
package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config holds every tunable and credential the system uses. Credentials left
// empty cause the corresponding service factory to select its mock adapter.
type Config struct {
	// Target.
	RepoURL string

	// Credentials (empty => mock mode for that integration).
	GitHubToken  string
	NIMAPIKey    string
	NIMBaseURL   string
	NIMModel     string
	JinaAPIKey   string
	GeminiAPIKey string
	E2BAPIKey    string

	// Local services.
	OllamaHost string
	QdrantHost string
	QdrantPort int
	EmbedModel string

	// Tunables.
	MaxRPM           int
	MaxFiles         int
	AnalystParallel  int
	FixCandidates    int
	ConfidenceGate   float64
	LocalizeFallback float64
	SandboxTimeout   int
	DashboardPort    int

	// Behavior flags.
	ForceMock   bool // force all integrations to mock regardless of keys
	NoDashboard bool
}

// envOr returns the env var value or a default.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

// Load reads .env (if present) into the process environment, then builds a
// Config from environment variables with sensible defaults matching the design
// document (35 RPM ceiling, top-50 files, etc.).
func Load() *Config {
	loadDotEnv(findProjectRootEnv())

	return &Config{
		GitHubToken:  os.Getenv("GITHUB_TOKEN"),
		NIMAPIKey:    os.Getenv("NIM_API_KEY"),
		NIMBaseURL:   envOr("NIM_BASE_URL", "https://integrate.api.nvidia.com/v1"),
		NIMModel:     envOr("NIM_MODEL", "meta/llama-3.1-70b-instruct"),
		JinaAPIKey:   os.Getenv("JINA_API_KEY"),
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
		E2BAPIKey:    os.Getenv("E2B_API_KEY"),

		OllamaHost: envOr("OLLAMA_HOST", "http://localhost:11434"),
		QdrantHost: envOr("QDRANT_HOST", "localhost"),
		QdrantPort: envInt("QDRANT_PORT", 6334),
		EmbedModel: envOr("EMBED_MODEL", "nomic-embed-text"),

		MaxRPM:           envInt("MAX_RPM", 35),
		MaxFiles:         envInt("MAX_FILES", 50),
		AnalystParallel:  envInt("ANALYST_PARALLEL", 10),
		FixCandidates:    envInt("FIX_CANDIDATES", 3),
		ConfidenceGate:   envFloat("CONFIDENCE_GATE", 0.80),
		LocalizeFallback: envFloat("LOCALIZE_FALLBACK", 0.50),
		SandboxTimeout:   envInt("SANDBOX_TIMEOUT", 60),
		DashboardPort:    envInt("DASHBOARD_PORT", 8080),

		ForceMock:   envBool("FORCE_MOCK", false),
		NoDashboard: envBool("NO_DASHBOARD", false),
	}
}

// findProjectRootEnv searches for .env starting from the current working
// directory and walking up to the filesystem root, looking for a directory that
// contains go.mod (the project root marker). Falls back to ".env" if not found.
func findProjectRootEnv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ".env"
	}
	for {
		// Check for .env in this directory.
		candidate := dir + "/.env"
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		// Check for go.mod (project root).
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			// We're at the project root but .env wasn't found here either;
			// return the path so loadDotEnv can try it (it's optional).
			return candidate
		}
		// Walk up.
		parent := dir[:strings.LastIndexByte(dir, '/')]
		if parent == dir {
			break
		}
		dir = parent
	}
	return ".env"
}

// loadDotEnv parses a simple KEY=VALUE .env file and sets any vars not already
// present in the environment. Lines starting with # and blank lines are ignored.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // .env is optional
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
