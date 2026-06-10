package tools

// factory.go selects concrete tool implementations based on configuration,
// mirroring the services factory pattern: a real adapter when credentials are
// present and mock mode is off, otherwise a deterministic mock.

import (
	"github.com/saketh/codesentinel/config"
)

// compile-time interface conformance checks.
var (
	_ GitHubClient = (*githubREST)(nil)
	_ GitHubClient = (*mockGitHub)(nil)
	_ CodeParser   = (*heuristicParser)(nil)
)

// NewGitHub returns the real REST client when a token is configured and mock
// mode is off; otherwise the in-memory mock that makes the offline demo work.
func NewGitHub(cfg *config.Config) GitHubClient {
	if cfg != nil && cfg.GitHubToken != "" && !cfg.ForceMock {
		return newGitHubREST(cfg.GitHubToken)
	}
	return newMockGitHub()
}

// NewParser returns the parser. Only the heuristic (pure-Go) parser exists for
// now; the signature is config-driven so a tree-sitter build can slot in later.
func NewParser(cfg *config.Config) CodeParser {
	return newHeuristicParser()
}
