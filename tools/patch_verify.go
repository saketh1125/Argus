package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var safePatchPath = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`)

// VerifyUnifiedDiff applies the mechanically generated patch to a private
// temporary copy and requires the exact complete resulting bytes. Git only
// parses the patch; no target repository scripts or code are executed.
func VerifyUnifiedDiff(ctx context.Context, filename, original, expected, diff string) error {
	if len(filename) > 1024 || !safePatchPath.MatchString(filename) || path.Clean(filename) != filename || strings.HasPrefix(filename, "../") || len(original) == 0 || len(expected) == 0 || len(original) > 1<<20 || len(expected) > 1<<20 || len(diff) == 0 || len(diff) > 4<<20 || strings.Count(original, "\n") > 1024 || strings.Count(expected, "\n") > 1024 {
		return fmt.Errorf("invalid patch input")
	}
	if diff != UnifiedDiff(original, expected, filename) {
		return fmt.Errorf("patch does not match complete source and result")
	}
	dir, err := os.MkdirTemp("", "argus-patch-check-*")
	if err != nil {
		return fmt.Errorf("patch check: create temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, filepath.FromSlash(filename))
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return fmt.Errorf("patch check: create file directory: %w", err)
	}
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		return fmt.Errorf("patch check: write original: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "apply", "--", "-")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CEILING_DIRECTORIES=" + filepath.Dir(dir)}
	cmd.Stdin = strings.NewReader(diff)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("patch check: apply failed: %w", err)
	}
	actual, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("patch check: read result: %w", err)
	}
	if string(actual) != expected {
		return fmt.Errorf("patch check: complete result mismatch")
	}
	return nil
}
