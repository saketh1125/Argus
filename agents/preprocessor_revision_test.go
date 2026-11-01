package agents

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/services"
	"github.com/saketh1125/argus/tools"
)

type revisionFixtureGitHub struct {
	*captureGitHub
	source string
	target *string
}

func (g revisionFixtureGitHub) ParseURL(string) (string, string, error) { return "Owner", "Repo", nil }
func (g revisionFixtureGitHub) Live() bool                              { return true }
func (g revisionFixtureGitHub) Clone(ctx context.Context, _ string, dest string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "clone", "-q", "--no-local", "--", g.source, dest)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("fixture clone: %s: %w", out, err)
	}
	if g.target != nil {
		*g.target = dest
	}
	return dest, nil
}

type headChangingParser struct {
	tools.CodeParser
	workdir *string
	commit  string
}

func (p headChangingParser) Parse(file, content string) (*models.ParsedFile, error) {
	parsed, err := p.CodeParser.Parse(file, content)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(*p.workdir, ".git", "HEAD"), []byte(p.commit+"\n"), 0600); err != nil {
		return nil, err
	}
	return parsed, nil
}

func TestPreprocessorRecordsLiveRepositoryRevision(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	source := t.TempDir()
	home := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = source
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Argus Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Argus Test", "GIT_COMMITTER_EMAIL=test@example.invalid"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s: %v", out, err)
		}
	}
	run("init", "-q")
	rawSource := "package fixture\nfunc F() {}\n// " + strings.Repeat("A", 210) + "\n"
	if err := os.WriteFile(filepath.Join(source, "file.go"), []byte(rawSource), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package leaked\nfunc Secret() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "escape.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "oversized.go"), []byte(strings.Repeat("x", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "file.go", "escape.go", "oversized.go")
	run("-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "fixture")
	expected, err := tools.ResolveGitCommit(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ForceMock: true, MaxFiles: 5}
	clients, err := services.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPreprocessor(revisionFixtureGitHub{captureGitHub: &captureGitHub{}, source: source}, tools.NewParser(cfg), clients.Embed, clients.Vectors, cfg, models.NoopReporter{})
	idx, workdir, _, _, err := p.Run(context.Background(), "https://github.com/Owner/Repo")
	if workdir != "" {
		defer os.RemoveAll(workdir)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := idx.GetFile("oversized.go"); exists {
		t.Fatal("indexed oversized file")
	}
	if _, exists := idx.GetFile("escape.go"); exists {
		t.Fatal("indexed a symlink outside the checkout")
	}
	parsed, exists := idx.GetFile("file.go")
	if !exists || parsed.SourceHash != fmt.Sprintf("%x", sha256.Sum256([]byte(rawSource))) {
		t.Fatal("original file hash missing or incorrect")
	}
	if parsed.Content == rawSource {
		t.Fatal("expected sanitizer to alter the test fixture")
	}
	repo, commit := idx.SourceRevision()
	if repo != "github.com/owner/repo" || commit != expected {
		t.Fatalf("unexpected source identity: %q %q", repo, commit)
	}
	// The clone includes both commits. Change HEAD during parsing to reproduce
	// a revision moving while files are indexed.
	run("-c", "core.hooksPath=/dev/null", "commit", "-q", "--allow-empty", "-m", "second")
	var changedWorkdir string
	changingGH := revisionFixtureGitHub{captureGitHub: &captureGitHub{}, source: source, target: &changedWorkdir}
	changingParser := headChangingParser{CodeParser: tools.NewParser(cfg), workdir: &changedWorkdir, commit: expected}
	p = NewPreprocessor(changingGH, changingParser, clients.Embed, clients.Vectors, cfg, models.NoopReporter{})
	_, _, _, _, err = p.Run(context.Background(), "https://github.com/Owner/Repo")
	if err == nil {
		t.Fatal("accepted revision change during indexing")
	}
	if _, statErr := os.Stat(changedWorkdir); !os.IsNotExist(statErr) {
		t.Fatalf("failed workdir not cleaned: %v", statErr)
	}
}

type cloneFailureGitHub struct {
	*captureGitHub
	dest *string
}

func (g cloneFailureGitHub) Clone(_ context.Context, _ string, dest string) (string, error) {
	*g.dest = dest
	if err := os.WriteFile(filepath.Join(dest, "partial"), []byte("partial clone"), 0600); err != nil {
		return "", err
	}
	return "", fmt.Errorf("simulated clone failure")
}

func TestPreprocessorCleansFailedClone(t *testing.T) {
	cfg := &config.Config{ForceMock: true}
	clients, err := services.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var dest string
	p := NewPreprocessor(cloneFailureGitHub{&captureGitHub{}, &dest}, tools.NewParser(cfg), clients.Embed, clients.Vectors, cfg, models.NoopReporter{})
	_, workdir, _, _, err := p.Run(context.Background(), "https://github.com/owner/repo")
	if err == nil || workdir != "" || dest == "" {
		t.Fatalf("unexpected clone failure result: %q %q %v", dest, workdir, err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("partial clone survived: %v", err)
	}
}
