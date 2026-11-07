package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// patch.go produces unified diffs and applies function rewrites in pure Go (no
// external diff library), so a fix's diff is always generated mechanically from
// the original and rewritten text — never authored by the LLM.

const (
	maxFunctionBytes = 64 << 10
	maxChangedLines  = 100
	maxSourceBytes   = 4 << 20
	maxDiffCells     = 1 << 20
)

// ApplyFunctionRewrite applies one exact, bounded function edit. Newline style
// differences are normalized for matching; output preserves the target file's
// CRLF/LF style. Duplicate source matches are rejected instead of guessing.
func ApplyFunctionRewrite(fileContent, originalFunc, newFunc string) (string, error) {
	if len(fileContent) > maxSourceBytes {
		return "", fmt.Errorf("apply: source file exceeds %d byte limit", maxSourceBytes)
	}
	if !utf8.ValidString(fileContent) || strings.ContainsRune(fileContent, '\x00') {
		return "", fmt.Errorf("apply: source file is not valid text")
	}
	if !utf8.ValidString(originalFunc) || !utf8.ValidString(newFunc) || strings.ContainsRune(newFunc, '\x00') {
		return "", fmt.Errorf("apply: function edit is not valid text")
	}
	normalizedFile, offsets := normalizeNewlines(fileContent)
	normalizedOriginal, _ := normalizeNewlines(originalFunc)
	normalizedOriginal = strings.TrimRight(normalizedOriginal, "\n")
	normalizedRewrite, _ := normalizeNewlines(newFunc)
	normalizedRewrite = strings.TrimRight(normalizedRewrite, "\n")
	if normalizedOriginal == "" {
		return "", fmt.Errorf("apply: empty original function")
	}
	if normalizedRewrite == "" {
		return "", fmt.Errorf("apply: empty replacement function")
	}
	if len(normalizedOriginal) > maxFunctionBytes || len(normalizedRewrite) > maxFunctionBytes {
		return "", fmt.Errorf("apply: function exceeds %d byte limit", maxFunctionBytes)
	}
	if len(normalizedRewrite) > max(len(normalizedOriginal)*4, len(normalizedOriginal)+4096) {
		return "", fmt.Errorf("apply: replacement exceeds allowed growth")
	}
	originalLines := len(strings.Split(normalizedOriginal, "\n"))
	rewriteLines := len(strings.Split(normalizedRewrite, "\n"))
	if originalLines > 0 && rewriteLines > maxDiffCells/originalLines {
		return "", fmt.Errorf("apply: function diff exceeds complexity limit")
	}
	first := strings.Index(normalizedFile, normalizedOriginal)
	if first < 0 {
		return "", fmt.Errorf("apply: original function not found in file")
	}
	if strings.Index(normalizedFile[first+len(normalizedOriginal):], normalizedOriginal) >= 0 {
		return "", fmt.Errorf("apply: original function is ambiguous (multiple exact matches)")
	}
	start, end := offsets[first], offsets[first+len(normalizedOriginal)]
	if start < 0 || end < start || end > len(fileContent) {
		return "", fmt.Errorf("apply: invalid normalized source offsets")
	}

	if strings.Contains(fileContent, "\r\n") {
		normalizedRewrite = strings.ReplaceAll(normalizedRewrite, "\n", "\r\n")
	}
	patched := fileContent[:start] + normalizedRewrite + fileContent[end:]
	if changedLineCount(normalizedOriginal, normalizedRewrite) > maxChangedLines {
		return "", fmt.Errorf("apply: patch exceeds %d changed-line limit", maxChangedLines)
	}
	return patched, nil
}

func normalizeNewlines(s string) (string, []int) {
	var b strings.Builder
	mapOffset := make([]int, 0, len(s)+1)
	for i := 0; i < len(s); {
		if s[i] == '\r' {
			mapOffset = append(mapOffset, i)
			b.WriteByte('\n')
			if i+1 < len(s) && s[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			continue
		}
		mapOffset = append(mapOffset, i)
		b.WriteByte(s[i])
		i++
	}
	mapOffset = append(mapOffset, len(s))
	return b.String(), mapOffset
}

func changedLineCount(before, after string) int {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	// A conservative linear estimate: an edit spanning a large middle section
	// is counted as all changed, even if some lines inside happen to be equal.
	return len(a) + len(b) - 2*prefix - 2*suffix
}

// UnifiedDiff returns a unified diff (with @@ hunk headers and 3 lines of
// context) transforming original into modified, labeled with filename. It uses
// a line-level longest-common-subsequence to find changed regions. Returns ""
// when the inputs are identical.
func UnifiedDiff(original, modified, filename string) string {
	if original == modified {
		return ""
	}
	a := splitLinesKeep(original)
	b := splitLinesKeep(modified)
	if len(a) > 0 && len(b) > maxDiffCells/len(a) {
		return ""
	}
	ops := lcsDiff(a, b)
	hunks := groupHunks(ops, 3)
	if len(hunks) == 0 {
		return ""
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n", filename)
	fmt.Fprintf(&out, "+++ b/%s\n", filename)
	for _, h := range hunks {
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", h.aStart, h.aCount, h.bStart, h.bCount)
		for _, ln := range h.lines {
			out.WriteString(ln)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func splitLinesKeep(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// op is a single diff operation on a line.
type op struct {
	kind byte // ' ' equal, '-' delete (from a), '+' insert (from b)
	text string
}

// lcsDiff computes a line diff via classic LCS dynamic programming.
func lcsDiff(a, b []string) []op {
	n, m := len(a), len(b)
	// dp[i][j] = LCS length of a[i:], b[j:]
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	return ops
}

type hunk struct {
	aStart, aCount int
	bStart, bCount int
	lines          []string
}

// groupHunks slices the op stream into hunks around changes with `context`
// equal lines of surrounding context.
func groupHunks(ops []op, context int) []hunk {
	// Index changed ops.
	changed := make([]bool, len(ops))
	any := false
	for i, o := range ops {
		if o.kind != ' ' {
			changed[i] = true
			any = true
		}
	}
	if !any {
		return nil
	}

	var hunks []hunk
	i := 0
	aLine, bLine := 1, 1 // 1-based positions as we walk
	// Precompute, for each op index, the a/b line number it starts at.
	aAt := make([]int, len(ops)+1)
	bAt := make([]int, len(ops)+1)
	ca, cb := 1, 1
	for k, o := range ops {
		aAt[k] = ca
		bAt[k] = cb
		switch o.kind {
		case ' ':
			ca++
			cb++
		case '-':
			ca++
		case '+':
			cb++
		}
	}
	aAt[len(ops)] = ca
	bAt[len(ops)] = cb
	_ = aLine
	_ = bLine

	for i < len(ops) {
		if !changed[i] {
			i++
			continue
		}
		// Expand region to include context on both sides; merge nearby changes.
		start := i - context
		if start < 0 {
			start = 0
		}
		// walk back over context that isn't itself a change boundary
		end := i
		for end < len(ops) {
			// find end of this run of changes + trailing context, merging runs
			// separated by <= 2*context equal lines
			if changed[end] {
				end++
				continue
			}
			// count equal run
			gap := end
			for gap < len(ops) && !changed[gap] {
				gap++
			}
			if gap < len(ops) && gap-end <= 2*context {
				end = gap // merge
				continue
			}
			break
		}
		stop := end + context
		if stop > len(ops) {
			stop = len(ops)
		}

		h := hunk{aStart: aAt[start], bStart: bAt[start]}
		for k := start; k < stop; k++ {
			o := ops[k]
			h.lines = append(h.lines, string(o.kind)+o.text)
			switch o.kind {
			case ' ':
				h.aCount++
				h.bCount++
			case '-':
				h.aCount++
			case '+':
				h.bCount++
			}
		}
		if h.aCount == 0 {
			h.aStart = 0
		}
		if h.bCount == 0 {
			h.bStart = 0
		}
		hunks = append(hunks, h)
		i = stop
	}
	return hunks
}
