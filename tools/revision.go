package tools

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
)

var gitCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

type limitedGitOutput struct{ bytes.Buffer }

func (b *limitedGitOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8192 {
		return 0, fmt.Errorf("git output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func readGit(ctx context.Context, workdir string, args ...string) (string, error) {
	argv := append([]string{"--no-optional-locks", "-C", workdir}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https"}
	var out limitedGitOutput
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git metadata: %w", err)
	}
	return out.String(), nil
}

// ResolveGitCommit resolves HEAD to an immutable SHA-1 commit. It does not
// establish that worktree files match that commit or verify repository identity.
func ResolveGitCommit(ctx context.Context, workdir string) (string, error) {
	out, err := readGit(ctx, workdir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve git commit: %w", err)
	}
	sha := strings.TrimSpace(out)
	if !gitCommitSHA.MatchString(sha) {
		return "", fmt.Errorf("resolve git commit: invalid SHA-1")
	}
	return sha, nil
}

// VerifyGitCommit rejects an invalid, unavailable, or changed HEAD.
func VerifyGitCommit(ctx context.Context, workdir, expected string) error {
	if !gitCommitSHA.MatchString(expected) {
		return fmt.Errorf("verify git commit: invalid expected SHA-1")
	}
	actual, err := ResolveGitCommit(ctx, workdir)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("verify git commit: source revision changed")
	}
	return nil
}

// VerifyPinnedFile binds repository origin, HEAD and regular-file bytes to a Git
// commit. Git SHA-1 verifies tree membership; callers must also verify SHA-256.
// It does not guarantee the worktree cannot change after this call.
func VerifyPinnedFile(ctx context.Context, workdir, repository, commit, file string, contents []byte) error {
	if repository != strings.ToLower(repository) || !strings.HasPrefix(repository, "github.com/") {
		return fmt.Errorf("invalid repository identity")
	}
	owner, repo, err := parseGitHubURL("https://" + repository)
	if err != nil || "github.com/"+owner+"/"+repo != repository {
		return fmt.Errorf("invalid repository identity")
	}
	if file == "" || len(file) > 1024 || path.IsAbs(file) || path.Clean(file) != file || file == ".." || strings.HasPrefix(file, "../") || strings.ContainsAny(file, "*?[]\\\x00") || len(contents) == 0 || len(contents) > 1<<20 {
		return fmt.Errorf("invalid pinned file")
	}
	if err := VerifyGitCommit(ctx, workdir, commit); err != nil {
		return err
	}
	origin, err := readGit(ctx, workdir, "config", "--local", "--get", "remote.origin.url")
	if err != nil {
		return fmt.Errorf("verify origin: %w", err)
	}
	actualOwner, actualRepo, err := parseGitHubURL(strings.TrimSpace(origin))
	if err != nil || "github.com/"+strings.ToLower(actualOwner)+"/"+strings.ToLower(actualRepo) != repository {
		return fmt.Errorf("verify origin: repository mismatch")
	}
	entry, err := readGit(ctx, workdir, "ls-tree", "-z", commit, "--", file)
	if err != nil {
		return fmt.Errorf("verify file in tree: %w", err)
	}
	if !strings.HasSuffix(entry, "\x00") || strings.Count(entry, "\x00") != 1 {
		return fmt.Errorf("verify file in tree: ambiguous entry")
	}
	parts := strings.SplitN(strings.TrimSuffix(entry, "\x00"), "\t", 2)
	if len(parts) != 2 || parts[1] != file {
		return fmt.Errorf("verify file in tree: path mismatch")
	}
	fields := strings.Fields(parts[0])
	if len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !gitCommitSHA.MatchString(fields[2]) {
		return fmt.Errorf("verify file in tree: not a regular blob")
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(contents))
	_, _ = h.Write(contents)
	if fmt.Sprintf("%x", h.Sum(nil)) != fields[2] {
		return fmt.Errorf("verify file in tree: content mismatch")
	}
	return nil
}
