package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectRootEnvFromPackageDirectory(t *testing.T) {
	got := findProjectRootEnv()
	if filepath.Base(got) != ".env" {
		t.Fatalf("findProjectRootEnv() = %q, want path ending in .env", got)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("findProjectRootEnv() = %q, want an absolute path", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(got), "go.mod")); err != nil {
		t.Fatalf("findProjectRootEnv() did not resolve the module root: %v", err)
	}
}

func TestLiveTargetMustBeExplicitlyAllowlisted(t *testing.T) {
	cfg := &Config{AllowedGitHubRepos: []string{"saketh1125/Argus"}}
	if !cfg.AllowsGitHubRepo("saketh1125", "Argus") {
		t.Fatal("explicit target should be allowed")
	}
	if cfg.AllowsGitHubRepo("another-owner", "Argus") || cfg.AllowsGitHubRepo("saketh1125", "Sentra") {
		t.Fatal("unlisted repository must be rejected")
	}
}
