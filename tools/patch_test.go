package tools

import (
	"strings"
	"testing"
)

func TestApplyFunctionRewritePreservesSurroundingSourceAndCRLF(t *testing.T) {
	originalFile := "// package comment\r\npackage demo\r\n\r\nimport \"fmt\"\r\n\r\n// sibling remains unchanged\r\nfunc sibling() { fmt.Println(\"safe\") }\r\n\r\n// target comment: café 雪\r\nfunc target() string {\r\n\t// preserve this comment\r\n\treturn \"old 雪\"\r\n}\r\n\r\nfunc last() {}\r\n"
	originalFunction := "// target comment: café 雪\nfunc target() string {\n\t// preserve this comment\n\treturn \"old 雪\"\n}"
	replacement := "// target comment: café 雪\nfunc target() string {\n\t// preserve this comment\n\treturn \"new 雪\"\n}"

	got, err := ApplyFunctionRewrite(originalFile, originalFunction, replacement)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(originalFile, "return \"old 雪\"", "return \"new 雪\"", 1)
	if got != want {
		t.Fatalf("patch changed unrelated source or formatting\nwant: %q\n got: %q", want, got)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatal("patch introduced LF-only line endings into a CRLF file")
	}
	if !strings.Contains(got, "import \"fmt\"\r\n") || !strings.Contains(got, "func sibling()") || !strings.Contains(got, "func last()") {
		t.Fatal("imports or sibling functions were not preserved")
	}
}

func TestApplyFunctionRewriteRejectsAmbiguousDuplicate(t *testing.T) {
	function := "func same() { return }"
	_, err := ApplyFunctionRewrite(function+"\n\n"+function, function, "func same() { changed() }")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous-source rejection, got %v", err)
	}
}

func TestApplyFunctionRewriteRejectsMalformedOrOversizedReplacement(t *testing.T) {
	if _, err := ApplyFunctionRewrite("func f() {}", "func missing() {}", "func f() {}"); err == nil {
		t.Fatal("expected missing original to fail")
	}
	if _, err := ApplyFunctionRewrite("func f() {}", "func f() {}", strings.Repeat("x", maxFunctionBytes+1)); err == nil {
		t.Fatal("expected oversized replacement to fail")
	}
	large := "func f() {\n" + strings.Repeat("\told()\n", maxChangedLines+1) + "}"
	replacement := "func f() {\n" + strings.Repeat("\tnew()\n", maxChangedLines+1) + "}"
	if _, err := ApplyFunctionRewrite(large, large, replacement); err == nil {
		t.Fatal("expected excessive changed lines to fail")
	}
}

func TestApplyFunctionRewriteRejectsBinaryOversizedAndExpensiveInputs(t *testing.T) {
	for name, source := range map[string]string{
		"nul":       "func f() {\x00 }",
		"bad-utf8":  string([]byte{0xff, 'f'}),
		"too-large": strings.Repeat("x", maxSourceBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ApplyFunctionRewrite(source, "func f() {}", "func f() { changed() }"); err == nil {
				t.Fatal("unsafe source should be rejected")
			}
		})
	}
	largeFn := "func f() {\n" + strings.Repeat("x\n", 1100) + "}"
	if _, err := ApplyFunctionRewrite(largeFn, largeFn, largeFn); err == nil || !strings.Contains(err.Error(), "complexity") {
		t.Fatalf("expected diff complexity rejection, got %v", err)
	}
}
