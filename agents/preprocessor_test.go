package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/tools"
)

type sourceCandidateTestParser struct{}

func (sourceCandidateTestParser) Supports(ext string) bool { return ext == ".go" }
func (sourceCandidateTestParser) Language(string) string   { return "go" }
func (sourceCandidateTestParser) Parse(string, string) (*models.ParsedFile, error) {
	return nil, nil
}
func (sourceCandidateTestParser) Name() string { return "test" }

func TestCollectSourceCandidatesSkipsSymlinkTargetsOutsideCheckout(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	outsidePath := filepath.Join(external, "outside.go")
	if err := os.WriteFile(outsidePath, []byte("package secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	insidePath := filepath.Join(root, "inside.go")
	if err := os.WriteFile(insidePath, []byte("package safe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, "linked.go")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Skipf("symlink creation unavailable on this host: %v", err)
	}

	candidates, _ := collectSourceCandidates(root, sourceCandidateTestParser{})
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want only the regular in-checkout file; candidates: %#v", len(candidates), candidates)
	}
	if candidates[0].rel != "inside.go" || candidates[0].abs != insidePath {
		t.Fatalf("candidate = %#v, want inside.go only", candidates[0])
	}
}

func TestCollectSourceCandidatesSkipsOversizedFiles(t *testing.T) {
	root := t.TempDir()
	insidePath := filepath.Join(root, "inside.go")
	if err := os.WriteFile(insidePath, []byte("package safe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oversizedPath := filepath.Join(root, "oversized.go")
	if err := os.WriteFile(oversizedPath, make([]byte, maxSourceFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}

	candidates, oversized := collectSourceCandidates(root, sourceCandidateTestParser{})
	if oversized != 1 {
		t.Fatalf("oversized files = %d, want 1", oversized)
	}
	if len(candidates) != 1 || candidates[0].rel != "inside.go" {
		t.Fatalf("candidates = %#v, want only inside.go", candidates)
	}
	if _, tooLarge, err := readIndexedSourceFile(oversizedPath); err != nil || !tooLarge {
		t.Fatalf("bounded read = tooLarge:%v err:%v, want tooLarge with no error", tooLarge, err)
	}
}

type partialCloneFailure struct {
	tools.GitHubClient
	dest string
}

func (g *partialCloneFailure) Clone(_ context.Context, _, dest string) (tools.CloneResult, error) {
	g.dest = dest
	if err := os.WriteFile(filepath.Join(dest, "partial-pack-data"), []byte("partial"), 0o600); err != nil {
		return tools.CloneResult{}, err
	}
	return tools.CloneResult{}, fmt.Errorf("synthetic interrupted clone")
}

func TestPreprocessorCleansWorkdirWhenCloneFails(t *testing.T) {
	base := tools.NewGitHub(&config.Config{ForceMock: true})
	github := &partialCloneFailure{GitHubClient: base}
	pre := NewPreprocessor(github, sourceCandidateTestParser{}, nil, nil, &config.Config{MaxFiles: 1}, models.NoopReporter{})

	_, workdir, _, _, err := pre.Run(context.Background(), "https://github.com/owner/repo")
	if err == nil || !strings.Contains(err.Error(), "synthetic interrupted clone") {
		t.Fatalf("Run error = %v, want clone failure", err)
	}
	if workdir != "" {
		t.Fatalf("failed Run returned workdir %q, want empty path", workdir)
	}
	if _, statErr := os.Stat(github.dest); !os.IsNotExist(statErr) {
		t.Fatalf("partial workdir still exists after clone failure: stat error = %v", statErr)
	}
}
