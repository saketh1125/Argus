package services

import (
	"regexp"
	"strings"
)

// controlChars matches C0 and C1 control characters EXCEPT newline (\n, 0x0a)
// and tab (\t, 0x09). These are stripped because no legitimate source code uses
// them and they are a common prompt-injection / terminal-escape vector.
var controlChars = regexp.MustCompile("[\\x00-\\x08\\x0b\\x0c\\x0e-\\x1f\\x7f-\\x9f]")

// base64Blob matches a run of 200+ base64 characters (optionally padded). Blobs
// this large are essentially never legitimate hand-written code; they are far
// more likely to be smuggled instructions or embedded payloads, so they are
// redacted before reaching any LLM context.
var base64Blob = regexp.MustCompile("[A-Za-z0-9+/]{200,}={0,2}")

// SanitizeForLLM scrubs untrusted source content before it enters any LLM prompt
// (design §9, "Prompt Injection Prevention"). Applied to every file with no
// exceptions, it performs three passes:
//
//  1. Strip C0/C1 control characters except newline and tab.
//  2. Redact base64 blobs longer than 200 characters to "[REDACTED_BLOB]".
//  3. Strip zero-width / invisible Unicode (ZERO WIDTH SPACE U+200B,
//     WORD JOINER U+2060, ZERO WIDTH NO-BREAK SPACE / BOM U+FEFF).
//
// The transformation is deterministic and idempotent on already-clean input.
func SanitizeForLLM(content string) string {
	// 1. Control characters.
	content = controlChars.ReplaceAllString(content, "")

	// 2. Oversized base64 blobs.
	content = base64Blob.ReplaceAllString(content, "[REDACTED_BLOB]")

	// 3. Invisible Unicode. Written as \u escapes so the source stays clean.
	content = strings.ReplaceAll(content, "\u200b", "") // ZERO WIDTH SPACE
	content = strings.ReplaceAll(content, "\u2060", "") // WORD JOINER
	content = strings.ReplaceAll(content, "\ufeff", "") // ZERO WIDTH NO-BREAK SPACE / BOM

	return content
}
