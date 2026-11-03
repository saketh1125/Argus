package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/saketh1125/argus/models"
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

	candidates := collectSourceCandidates(root, sourceCandidateTestParser{})
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want only the regular in-checkout file; candidates: %#v", len(candidates), candidates)
	}
	if candidates[0].rel != "inside.go" || candidates[0].abs != insidePath {
		t.Fatalf("candidate = %#v, want inside.go only", candidates[0])
	}
}
