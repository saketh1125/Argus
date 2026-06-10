package services

import (
	"strings"
	"testing"
)

// TestSanitizeStripsControlChars verifies C0/C1 control chars are removed while
// newline and tab survive.
func TestSanitizeStripsControlChars(t *testing.T) {
	in := "line1\x00\x07line2\t\nline3\x1b[31m"
	out := SanitizeForLLM(in)
	if strings.ContainsAny(out, "\x00\x07\x1b") {
		t.Fatalf("control chars not stripped: %q", out)
	}
	if !strings.Contains(out, "\t") || !strings.Contains(out, "\n") {
		t.Fatalf("tab/newline should be preserved: %q", out)
	}
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line3") {
		t.Fatalf("legitimate content lost: %q", out)
	}
}

// TestSanitizeRedactsBlob verifies a base64 blob >200 chars is redacted.
func TestSanitizeRedactsBlob(t *testing.T) {
	blob := strings.Repeat("A", 250)
	in := "before " + blob + " after"
	out := SanitizeForLLM(in)
	if strings.Contains(out, blob) {
		t.Fatalf("long base64 blob not redacted: %q", out)
	}
	if !strings.Contains(out, "[REDACTED_BLOB]") {
		t.Fatalf("redaction marker missing: %q", out)
	}
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Fatalf("surrounding text lost: %q", out)
	}
}

// TestSanitizeShortBlobUntouched verifies a short base64-ish run is NOT redacted.
func TestSanitizeShortBlobUntouched(t *testing.T) {
	in := "token := \"" + strings.Repeat("A", 50) + "\""
	out := SanitizeForLLM(in)
	if strings.Contains(out, "[REDACTED_BLOB]") {
		t.Fatalf("short blob should not be redacted: %q", out)
	}
}

// TestSanitizeStripsZeroWidth verifies invisible unicode is removed.
func TestSanitizeStripsZeroWidth(t *testing.T) {
	in := "a\u200bb\u2060c\ufeffd"
	out := SanitizeForLLM(in)
	if out != "abcd" {
		t.Fatalf("zero-width chars not stripped: got %q want %q", out, "abcd")
	}
}
