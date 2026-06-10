package tools

import (
	"fmt"
	"strings"
)

// patch.go produces unified diffs and applies function rewrites in pure Go (no
// external diff library), so a fix's diff is always generated mechanically from
// the original and rewritten text — never authored by the LLM.

// ApplyFunctionRewrite replaces the first occurrence of originalFunc in
// fileContent with newFunc and returns the new file content. It returns an
// error if originalFunc is not found verbatim.
func ApplyFunctionRewrite(fileContent, originalFunc, newFunc string) (string, error) {
	originalFunc = strings.TrimRight(originalFunc, "\n")
	if originalFunc == "" {
		return "", fmt.Errorf("apply: empty original function")
	}
	idx := strings.Index(fileContent, originalFunc)
	if idx < 0 {
		return "", fmt.Errorf("apply: original function not found in file")
	}
	return fileContent[:idx] + strings.TrimRight(newFunc, "\n") + fileContent[idx+len(originalFunc):], nil
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
