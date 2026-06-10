// Package agents implements the seven pipeline stages of CodeSentinel:
// preprocessor, localizer, analyst, aggregator, fix generator, validator, and
// PR agent. Each agent is a small struct wired with the service/tool interfaces
// it needs (frozen in the services and tools packages) plus the loaded config
// and a progress Reporter. Agents follow the design principle "LLMs reason,
// tools only do I/O": every LLM call sets CompletionRequest.Task to a
// models.Task* constant, embeds numbered code via tools.NumberedChunk, instructs
// the model to return JSON matching a models.*Result schema, and parses the
// reply tolerantly through agtParseJSON.
//
// Dependency direction: agents -> {models, config, services, tools}. Nothing in
// the rest of the project imports agents except main/orchestration.
package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
)

// agtParseJSON tolerantly extracts a JSON object from an LLM reply and unmarshals
// it into v. LLMs frequently wrap JSON in ```json fences or prose, so we strip
// fences, slice from the first '{' to the last '}', and only then unmarshal.
// The deterministic mock LLM returns exactly the schema keyed by Task, so this
// path also works fully offline.
func agtParseJSON(text string, v any) error {
	s := strings.TrimSpace(text)
	// Strip code fences (```json ... ``` or ``` ... ```).
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "json")
		s = strings.TrimPrefix(s, "JSON")
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	// Slice to the outermost object braces.
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		s = s[start : end+1]
	}
	return json.Unmarshal([]byte(s), v)
}

// agtIssueID derives a stable Issue.ID from its DedupKey (file:line:bugtype) as
// the first 12 hex chars of its SHA-256, so the same finding always hashes to
// the same id across runs.
func agtIssueID(dedupKey string) string {
	sum := sha256.Sum256([]byte(dedupKey))
	return hex.EncodeToString(sum[:])[:12]
}

// agtTempSchedule returns n temperatures for fix-candidate sampling, drawn from
// a fixed ascending schedule and clamped to the last value when n exceeds it.
func agtTempSchedule(n int) []float64 {
	base := []float64{0.2, 0.5, 0.7, 0.9, 1.0}
	if n <= 0 {
		n = 1
	}
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		if i < len(base) {
			out = append(out, base[i])
		} else {
			out = append(out, base[len(base)-1])
		}
	}
	return out
}

// agtSyntaxCmd returns a best-effort, language-appropriate syntax-check command
// for a single file, suitable for a sandbox SandboxSpec.Commands entry. The file
// path is relative to the sandbox working directory. Go uses `gofmt -e` rather
// than `go build <file>` because a single-file go build fails without module
// context. Unknown languages fall back to a no-op success.
func agtSyntaxCmd(lang, file string) []string {
	q := agtShellQuote(file)
	switch lang {
	case "python":
		return []string{"python3 -m py_compile " + q}
	case "javascript", "typescript":
		return []string{"node --check " + q}
	case "go":
		return []string{"gofmt -e " + q}
	case "ruby":
		return []string{"ruby -c " + q}
	case "php":
		return []string{"php -l " + q}
	default:
		// Best-effort: nothing to check, treat as a trivially-passing command.
		return []string{"true"}
	}
}

// agtShellQuote single-quotes a path for safe embedding in a shell command.
func agtShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// agtClamp constrains f to the inclusive range [lo, hi].
func agtClamp(f, lo, hi float64) float64 {
	if f < lo {
		return lo
	}
	if f > hi {
		return hi
	}
	return f
}

// agtSanitizeForBranch converts an arbitrary string into a git-branch-safe slug:
// lowercase, with runs of non-alphanumerics collapsed to single dashes.
func agtSanitizeForBranch(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// agtBaseName returns the file's base name without directory, used in branch
// names and commit messages.
func agtBaseName(file string) string {
	return filepath.Base(file)
}
