package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestVerifyUnifiedDiffReconstructsCompleteFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	original := "package p\n\nfunc first() int { return 1 }\nfunc sibling() {}\n"
	patched := "package p\n\nfunc first() int { return 2 }\nfunc sibling() {}\n"
	diff := UnifiedDiff(original, patched, "nested/file.go")
	if err := VerifyUnifiedDiff(context.Background(), "nested/file.go", original, patched, diff); err != nil {
		t.Fatalf("valid diff rejected: %v", err)
	}
	if err := VerifyUnifiedDiff(context.Background(), "nested/file.go", original, "func first() int { return 2 }\n", diff); err == nil {
		t.Fatal("partial result accepted")
	}
	if err := VerifyUnifiedDiff(context.Background(), "../escape", original, patched, diff); err == nil {
		t.Fatal("unsafe path accepted")
	}
	if err := VerifyUnifiedDiff(context.Background(), "nested/file.go", original, patched, "garbage"); err == nil {
		t.Fatal("malformed diff accepted")
	}
}

func TestVerifyUnifiedDiffRejectsLostFinalNewline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	original := "package p\nfunc f() { println(1) }"
	patched := "package p\nfunc f() { println(2) }"
	diff := UnifiedDiff(original, patched, "f.go")
	if err := VerifyUnifiedDiff(context.Background(), "f.go", original, patched, diff); err == nil {
		t.Fatal("accepted diff that loses missing-final-newline state")
	}
}

func TestUnifiedDiffRejectsExcessiveLines(t *testing.T) {
	original := strings.Repeat("line\n", 1025)
	if diff := UnifiedDiff(original, original+"changed\n", "f.go"); diff != "" {
		t.Fatal("generated unbounded diff")
	}
}
