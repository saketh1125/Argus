package tools

import (
	"fmt"
	"strings"

	"github.com/saketh/codesentinel/models"
)

// NumberedChunk renders a code chunk for an LLM prompt with 1-based line
// numbers offset by baseLine, prefixed with a machine-readable LINE_BASE header.
//
// The LINE_BASE header is part of the LLM I/O contract: the mock analyst parses
// it to report absolute line numbers, and the real model is instructed to do
// the same. Output looks like:
//
//	LINE_BASE: 42
//	42: func foo() {
//	43:     ...
func NumberedChunk(code string, baseLine int) string {
	if baseLine < 1 {
		baseLine = 1
	}
	lines := strings.Split(code, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "LINE_BASE: %d\n", baseLine)
	for i, ln := range lines {
		fmt.Fprintf(&b, "%d: %s\n", baseLine+i, ln)
	}
	return b.String()
}

// GetCodeChunk extracts the inclusive 1-based line range [lineStart,lineEnd]
// from content. Out-of-range bounds are clamped. A zero/empty range returns the
// whole content.
func GetCodeChunk(content string, lineStart, lineEnd int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if lineStart <= 0 && lineEnd <= 0 {
		return content
	}
	if lineStart < 1 {
		lineStart = 1
	}
	if lineEnd < lineStart || lineEnd > len(lines) {
		lineEnd = len(lines)
	}
	if lineStart > len(lines) {
		return ""
	}
	return strings.Join(lines[lineStart-1:lineEnd], "\n")
}

// SearchSymbol returns all signatures across the repo whose name equals or
// contains the query (case-insensitive), exact matches first.
func SearchSymbol(idx *models.RepoIndex, query string) []models.Signature {
	if idx == nil || query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var exact, partial []models.Signature
	for _, s := range idx.Signatures() {
		name := strings.ToLower(s.Name)
		switch {
		case name == q:
			exact = append(exact, s)
		case strings.Contains(name, q):
			partial = append(partial, s)
		}
	}
	return append(exact, partial...)
}

// FunctionSource finds a function/method signature by file and name and returns
// it. When file is empty it matches on name alone.
func FunctionSource(idx *models.RepoIndex, file, name string) (models.Signature, bool) {
	if idx == nil {
		return models.Signature{}, false
	}
	for _, s := range idx.Signatures() {
		if s.Name == name && (file == "" || s.File == file) {
			return s, true
		}
	}
	return models.Signature{}, false
}
