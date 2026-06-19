package tools

// parser.go implements a pure-Go, regex-based CodeParser. It deliberately uses
// no tree-sitter/CGO: heuristic extraction keeps the build hermetic and the
// pipeline portable. Parsing is best-effort — Parse never returns a fatal error;
// on trouble it records ParsedFile.ParseError and still returns a usable struct.

import (
	"regexp"
	"sort"
	"strings"

	"github.com/saketh1125/argus/models"
)

// extLang maps file extensions to language names.
var extLang = map[string]string{
	".py":   "python",
	".js":   "javascript",
	".ts":   "typescript",
	".go":   "go",
	".java": "java",
	".cpp":  "cpp",
	".cc":   "cpp",
	".c":    "c",
	".rs":   "rust",
	".rb":   "ruby",
	".php":  "php",
}

// sigRule pairs a per-language regex with the metadata it yields. nameGroup is
// the submatch index holding the symbol name; kind is its Signature.Kind.
type sigRule struct {
	re        *regexp.Regexp
	nameGroup int
	kind      string
}

// Compiled once at package init (Go's regexp is RE2: no lookaround/backrefs).
var sigRules = map[string][]sigRule{
	"go": {
		// func (recv) Name( -> method ; func Name( -> function
		{regexp.MustCompile(`^\s*func\s+\([^)]*\)\s+([A-Za-z_]\w*)\s*\(`), 1, "method"},
		{regexp.MustCompile(`^\s*func\s+([A-Za-z_]\w*)\s*\(`), 1, "function"},
		{regexp.MustCompile(`^\s*type\s+([A-Za-z_]\w*)\s+(?:struct|interface)\b`), 1, "class"},
	},
	"python": {
		{regexp.MustCompile(`^\s*class\s+([A-Za-z_]\w*)`), 1, "class"},
		{regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)`), 1, "function"},
	},
	"javascript": jsTsRules(),
	"typescript": jsTsRules(),
	"java": {
		{regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|abstract\s+|final\s+)*class\s+([A-Za-z_]\w*)`), 1, "class"},
		{regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|static\s+|final\s+|abstract\s+|synchronized\s+)+[\w<>\[\].]+\s+([A-Za-z_]\w*)\s*\([^;]*\)\s*\{?\s*$`), 1, "method"},
	},
	"c":   cFamilyRules(),
	"cpp": cppRules(),
	"rust": {
		{regexp.MustCompile(`^\s*(?:pub\s+)?struct\s+([A-Za-z_]\w*)`), 1, "class"},
		{regexp.MustCompile(`^\s*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)`), 1, "function"},
	},
	"ruby": {
		{regexp.MustCompile(`^\s*class\s+([A-Za-z_]\w*)`), 1, "class"},
		{regexp.MustCompile(`^\s*def\s+(?:self\.)?([A-Za-z_]\w*[!?]?)`), 1, "method"},
	},
	"php": {
		{regexp.MustCompile(`^\s*(?:abstract\s+|final\s+)*class\s+([A-Za-z_]\w*)`), 1, "class"},
		{regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|static\s+)*function\s+([A-Za-z_]\w*)`), 1, "function"},
	},
}

// jsTsRules covers the common JavaScript/TypeScript declaration shapes.
func jsTsRules() []sigRule {
	return []sigRule{
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`), 1, "class"},
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)\s*\(`), 1, "function"},
		// const foo = (args) => / const foo = async (args) =>
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\(`), 1, "function"},
		// const foo = async function / = function
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?function\b`), 1, "function"},
		// class methods: name(args) {
		{regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|static\s+|async\s+)*([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*\{`), 1, "method"},
	}
}

// cFamilyRules is a pragmatic "type name(args) {" function heuristic for C.
func cFamilyRules() []sigRule {
	return []sigRule{
		{regexp.MustCompile(`^\s*(?:static\s+|inline\s+|extern\s+)*[A-Za-z_][\w\s\*]*?\s+\*?([A-Za-z_]\w*)\s*\([^;]*\)\s*\{?\s*$`), 1, "function"},
	}
}

// cppRules adds class/struct on top of the C function heuristic.
func cppRules() []sigRule {
	rules := []sigRule{
		{regexp.MustCompile(`^\s*(?:class|struct)\s+([A-Za-z_]\w*)`), 1, "class"},
	}
	return append(rules, cFamilyRules()...)
}

// importRules maps language -> regexes whose capture groups hold module names.
var importRules = map[string][]*regexp.Regexp{
	"python": {
		regexp.MustCompile(`^\s*from\s+(\S+)\s+import\b`),
		regexp.MustCompile(`^\s*import\s+(.+)$`),
	},
	"javascript": jsImportRules(),
	"typescript": jsImportRules(),
	"go": {
		regexp.MustCompile(`^\s*import\s+"([^"]+)"`),
		regexp.MustCompile(`^\s*(?:[A-Za-z_.]\w*\s+)?"([^"]+)"\s*$`), // line inside import ( ... )
	},
	"java": {
		regexp.MustCompile(`^\s*import\s+(?:static\s+)?([\w.*]+)\s*;`),
	},
	"c":   {regexp.MustCompile(`^\s*#\s*include\s+[<"]([^>"]+)[>"]`)},
	"cpp": {regexp.MustCompile(`^\s*#\s*include\s+[<"]([^>"]+)[>"]`)},
	"rust": {
		regexp.MustCompile(`^\s*(?:pub\s+)?use\s+([\w:{}*, ]+)\s*;`),
	},
	"ruby": {
		regexp.MustCompile(`^\s*require(?:_relative)?\s+['"]([^'"]+)['"]`),
	},
	"php": {
		regexp.MustCompile(`^\s*(?:require|require_once|include|include_once|use)\s+['"]?([\w\\/.]+)['"]?`),
	},
}

func jsImportRules() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`\brequire\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`\bimport\b.*?\bfrom\s+['"]([^'"]+)['"]`),
		regexp.MustCompile(`^\s*import\s+['"]([^'"]+)['"]`),
	}
}

// callRe finds identifier( occurrences anywhere on a line.
var callRe = regexp.MustCompile(`([A-Za-z_]\w*)\s*\(`)

// callKeywords are control-flow/declaration tokens that look like calls but are
// not, and must be skipped when building call edges.
var callKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true,
	"return": true, "func": true, "def": true, "function": true, "class": true,
	"sizeof": true, "case": true, "with": true, "elif": true, "else": true,
	"do": true, "in": true, "and": true, "or": true, "not": true, "new": true,
	"await": true, "yield": true, "typeof": true, "delete": true, "throw": true,
	"match": true, "fn": true, "let": true, "const": true, "var": true,
	"struct": true, "interface": true, "type": true, "use": true, "import": true,
}

// heuristicParser implements tools.CodeParser with regex extraction.
type heuristicParser struct{}

// newHeuristicParser returns a ready parser.
func newHeuristicParser() *heuristicParser { return &heuristicParser{} }

func (p *heuristicParser) Name() string { return "heuristic" }

// Supports reports whether the parser handles a file with this extension.
func (p *heuristicParser) Supports(ext string) bool {
	_, ok := extLang[strings.ToLower(ext)]
	return ok
}

// Language returns the language name for an extension, or "" if unsupported.
func (p *heuristicParser) Language(ext string) string {
	return extLang[strings.ToLower(ext)]
}

// Parse analyzes one file's content into a ParsedFile. It never returns a fatal
// error; unsupported extensions yield a struct with ParseError set.
func (p *heuristicParser) Parse(path, content string) (*models.ParsedFile, error) {
	ext := extOf(path)
	lang := extLang[ext]
	pf := &models.ParsedFile{
		Path:     path,
		Language: lang,
		Content:  content,
	}
	if lang == "" {
		pf.ParseError = "unsupported extension: " + ext
		return pf, nil
	}

	lines := strings.Split(content, "\n")
	pf.Signatures = extractSignatures(lines, path, lang)
	pf.Imports = extractImports(lines, lang)
	pf.Calls = extractCalls(lines, path, pf.Signatures)
	return pf, nil
}

// extractSignatures scans line-by-line, matches the language's signature rules,
// then sorts by LineStart and fills LineEnd/Body from neighboring ranges.
func extractSignatures(lines []string, path, lang string) []models.Signature {
	rules := sigRules[lang]
	if len(rules) == 0 {
		return nil
	}
	var sigs []models.Signature
	for i, line := range lines {
		for _, r := range rules {
			m := r.re.FindStringSubmatch(line)
			if m == nil || r.nameGroup >= len(m) {
				continue
			}
			// Method/function heuristics for C-family/JS/Java also match
			// control-flow lines like `if (...) {`; drop keyword "names".
			if r.kind != "class" && callKeywords[m[r.nameGroup]] {
				continue
			}
			sigs = append(sigs, models.Signature{
				Name:      m[r.nameGroup],
				Kind:      r.kind,
				File:      path,
				Language:  lang,
				LineStart: i + 1, // 1-based
				Text:      strings.TrimRight(line, "\r"),
			})
			break // first matching rule wins for this line
		}
	}
	sort.SliceStable(sigs, func(a, b int) bool {
		return sigs[a].LineStart < sigs[b].LineStart
	})
	// LineEnd = line before the next signature, or EOF; Body = those lines.
	for i := range sigs {
		end := len(lines)
		if i+1 < len(sigs) {
			end = sigs[i+1].LineStart - 1
		}
		if end < sigs[i].LineStart {
			end = sigs[i].LineStart
		}
		sigs[i].LineEnd = end
		sigs[i].Body = strings.Join(lines[sigs[i].LineStart-1:end], "\n")
	}
	return sigs
}

// extractImports collects raw module strings using the language's import rules.
func extractImports(lines []string, lang string) []string {
	rules := importRules[lang]
	if len(rules) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	inGoBlock := false
	for _, line := range lines {
		// Track Go's grouped `import ( ... )` block so the bare-string rule only
		// fires inside it (avoiding stray quoted-string false positives).
		if lang == "go" {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "import (") {
				inGoBlock = true
				continue
			}
			if inGoBlock {
				if trimmed == ")" {
					inGoBlock = false
					continue
				}
				if mod := goImportLine(trimmed); mod != "" && !seen[mod] {
					seen[mod] = true
					out = append(out, mod)
				}
				continue
			}
		}
		for _, re := range rules {
			if lang == "go" && re.String() == `^\s*(?:[A-Za-z_.]\w*\s+)?"([^"]+)"\s*$` {
				continue // grouped-block rule handled above
			}
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				mod := strings.TrimSpace(m[1])
				if mod == "" || seen[mod] {
					continue
				}
				seen[mod] = true
				out = append(out, mod)
			}
		}
	}
	return out
}

// goImportLine extracts the quoted path (and ignores any alias) from a line
// inside a Go import block.
func goImportLine(line string) string {
	start := strings.IndexByte(line, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(line[start+1:], '"')
	if end < 0 {
		return ""
	}
	return line[start+1 : start+1+end]
}

// extractCalls builds caller→callee edges. Each call is attributed to the
// enclosing signature whose [LineStart, LineEnd] covers its line. Declaration
// lines and language keywords are skipped to avoid self-edges and noise.
func extractCalls(lines []string, path string, sigs []models.Signature) []models.CallEdge {
	var edges []models.CallEdge
	for i, line := range lines {
		lineNo := i + 1
		caller, callerStart := enclosingFunc(sigs, lineNo)
		// Skip the declaration line itself: it would record a self-edge.
		if lineNo == callerStart {
			continue
		}
		for _, m := range callRe.FindAllStringSubmatch(line, -1) {
			callee := m[1]
			if callKeywords[callee] {
				continue
			}
			edges = append(edges, models.CallEdge{
				Caller: caller,
				Callee: callee,
				File:   path,
				Line:   lineNo,
			})
		}
	}
	return edges
}

// enclosingFunc returns the name and LineStart of the innermost function/method
// signature covering lineNo, or ("", 0) if none. Class signatures are skipped so
// methods attribute correctly.
func enclosingFunc(sigs []models.Signature, lineNo int) (string, int) {
	name, start := "", 0
	for _, s := range sigs {
		if s.Kind == "class" {
			continue
		}
		if lineNo >= s.LineStart && lineNo <= s.LineEnd {
			// Prefer the signature with the latest start (innermost/closest).
			if s.LineStart >= start {
				name, start = s.Name, s.LineStart
			}
		}
	}
	return name, start
}

// extOf returns the lowercased extension (with dot) of a path.
func extOf(path string) string {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return ""
	}
	return strings.ToLower(path[i:])
}
