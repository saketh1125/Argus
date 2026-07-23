package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/saketh1125/argus/models"
)

// ---------------------------------------------------------------------------
// Real adapter: Nvidia NIM (OpenAI-compatible chat completions)
// ---------------------------------------------------------------------------

// nimClient is the live LLMClient. It POSTs to an OpenAI-compatible
// /chat/completions endpoint and routes every call through the shared
// RateLimiter so all goroutines stay collectively under the RPM ceiling.
type nimClient struct {
	baseURL string
	apiKey  string
	model   string
	limiter RateLimiter
	http    *http.Client
}

// newNIMClient builds a live NIM adapter. baseURL is the API root (e.g.
// "https://integrate.api.nvidia.com/v1"); every call is gated by limiter.
func newNIMClient(baseURL, apiKey, model string, limiter RateLimiter) *nimClient {
	return &nimClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		limiter: limiter,
		http:    &http.Client{},
	}
}

// chatMessage is one OpenAI-style chat message.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the request body for /chat/completions.
type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	MaxTokens      int            `json:"max_tokens,omitempty"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

// chatResponse is the (subset of the) response body we consume.
type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Complete performs one chat completion against the live NIM endpoint. It waits
// on the shared rate limiter first, then issues the HTTP request bounded by ctx.
// Retry policy:
//   - 401 Unauthorized: fatal, return immediately.
//   - 429 Too Many Requests: sleep Retry-After (default 10s), retry once; error on second 429.
//   - 500 or 503: exponential backoff 1s/2s/4s, up to 4 total attempts.
//   - Other 4xx: return immediately, no retry.
func (c *nimClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("nim: rate limiter: %w", err)
	}

	model := c.model
	if req.Model != "" {
		model = req.Model
	}

	body := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.User},
		},
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSONMode {
		body.ResponseFormat = map[string]any{"type": "json_object"}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("nim: marshal request: %w", err)
	}

	// retried429 tracks whether we have already retried once after a 429.
	retried429 := false
	// backoffDurations are the sleeps before attempts 2, 3, 4 for 500/503.
	backoffDurations := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 1; attempt <= 4; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("nim: build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.http.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("nim: http: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("nim: read response: %w", readErr)
		}

		status := resp.StatusCode

		// Success.
		if status >= 200 && status < 300 {
			var parsed chatResponse
			if err := json.Unmarshal(respBody, &parsed); err != nil {
				return nil, fmt.Errorf("nim: decode response: %w", err)
			}
			if len(parsed.Choices) == 0 {
				return nil, fmt.Errorf("nim: empty choices")
			}
			return &CompletionResponse{
				Text:         parsed.Choices[0].Message.Content,
				Model:        model,
				PromptTokens: parsed.Usage.PromptTokens,
				OutputTokens: parsed.Usage.CompletionTokens,
			}, nil
		}

		// 401 Unauthorized — fatal, no retry.
		if status == http.StatusUnauthorized {
			return nil, fmt.Errorf("nim: unauthorized: check NIM_API_KEY")
		}

		// 429 Too Many Requests — retry once after Retry-After delay.
		if status == http.StatusTooManyRequests {
			if retried429 {
				return nil, fmt.Errorf("nim: status %d: %s", status, strings.TrimSpace(string(respBody)))
			}
			retried429 = true
			delay := 10 * time.Second
			if val := resp.Header.Get("Retry-After"); val != "" {
				if secs, parseErr := strconv.Atoi(val); parseErr == nil {
					delay = time.Duration(secs) * time.Second
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			time.Sleep(delay)
			continue
		}

		// 500 or 503 — exponential backoff, max 4 total attempts.
		if status == http.StatusInternalServerError || status == http.StatusServiceUnavailable {
			lastErr := fmt.Errorf("nim: status %d: %s", status, strings.TrimSpace(string(respBody)))
			if attempt >= 4 {
				return nil, lastErr
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			time.Sleep(backoffDurations[attempt-1])
			continue
		}

		// Other 4xx (or any other non-2xx) — return immediately, no retry.
		return nil, fmt.Errorf("nim: status %d: %s", status, strings.TrimSpace(string(respBody)))
	}

	// Unreachable: the loop always returns or continues.
	return nil, fmt.Errorf("nim: exhausted retries")
}

// Name identifies the adapter as "nim:<model>".
func (c *nimClient) Name() string { return "nim:" + c.model }

// Live reports that this is a real adapter.
func (c *nimClient) Live() bool { return true }

// ---------------------------------------------------------------------------
// Mock adapter: deterministic, offline, schema-correct
// ---------------------------------------------------------------------------

// mockLLM is the offline LLMClient. It switches on req.Task and returns a
// deterministic JSON-encoded models.*Result so the entire pipeline runs without
// any network access. The heuristics are intentionally simple regex/string
// scans — enough to surface a realistic bug on a planted snippet for the demo.
type mockLLM struct{}

// newMockLLM returns the offline LLM adapter.
func newMockLLM() *mockLLM { return &mockLLM{} }

// Complete dispatches on req.Task and returns the matching deterministic result
// as JSON in resp.Text.
func (m *mockLLM) Complete(_ context.Context, req CompletionRequest) (*CompletionResponse, error) {
	var (
		out []byte
		err error
	)
	switch req.Task {
	case models.TaskAnalyze:
		out, err = json.Marshal(m.analyze(req.User))
	case models.TaskRerank:
		out, err = json.Marshal(m.rerank(req.User))
	case models.TaskFixGen:
		out, err = json.Marshal(m.fixGen(req.User))
	case models.TaskTestGen:
		out, err = json.Marshal(m.testGen(req.User))
	default:
		// Unknown task: return an empty JSON object so callers parsing any
		// schema get a well-formed (if empty) document.
		out = []byte("{}")
	}
	if err != nil {
		return nil, fmt.Errorf("mock-llm: marshal %s result: %w", req.Task, err)
	}
	return &CompletionResponse{Text: string(out), Model: "mock-llm"}, nil
}

// Name identifies the mock adapter.
func (m *mockLLM) Name() string { return "mock-llm" }

// Live reports that this is NOT a real adapter.
func (m *mockLLM) Live() bool { return false }

// ----- numbered-chunk parsing ----------------------------------------------

// numberedLine is one parsed entry from a tools.NumberedChunk prompt.
type numberedLine struct {
	num  int    // absolute source line number
	code string // source text after "<num>: "
}

// lineNumRe matches the "<lineno>: <code>" lines produced by tools.NumberedChunk.
var lineNumRe = regexp.MustCompile(`^(\d+):\s?(.*)$`)

// parseNumbered extracts the LINE_BASE header and the numbered source lines from
// a TaskAnalyze prompt body. Lines that do not match the numbered format
// (including the header itself) are skipped.
func parseNumbered(user string) []numberedLine {
	var lines []numberedLine
	for _, raw := range strings.Split(user, "\n") {
		if strings.HasPrefix(raw, "LINE_BASE:") {
			continue
		}
		m := lineNumRe.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		lines = append(lines, numberedLine{num: n, code: m[2]})
	}
	return lines
}

// ----- TaskAnalyze heuristics ----------------------------------------------

// Bug-pattern regexes, evaluated in priority order (strongest/most severe
// first). The first matching line wins.
var (
	reSQLKeyword  = regexp.MustCompile(`(?i)(select|insert|update|delete|from)`)
	reSQLBuild    = regexp.MustCompile(`(?i)(%s|fmt\.Sprintf|'%s'|"\s*\+\s*\w)`)
	reHardcoded   = regexp.MustCompile(`(?i)(secret|api_key|apikey|password|token)\s*[:=]\s*["'][^"']{8,}`)
	reEval        = regexp.MustCompile(`\beval\s*\(`)
	reNullIndex   = regexp.MustCompile(`\[\s*["'][^"']+["']\s*\]`) // e.g. ["password_hash"]
	reNullField   = regexp.MustCompile(`\.options\.`)
	reNullUserIdx = regexp.MustCompile(`\buser\s*\[`)
	reOffRangeLen = regexp.MustCompile(`range\s*\(\s*len\s*\(`)
	reOffPlusOne  = regexp.MustCompile(`\+\s*1\b`)
	reOffArrLen   = regexp.MustCompile(`\[\s*\w+\.length\s*\]`)
	reOffLenIdx   = regexp.MustCompile(`\[\s*len\s*\(`)
	reScanCall    = regexp.MustCompile(`\.Scan\s*\(`)
	reErrAssign   = regexp.MustCompile(`(err\s*:?=|if\s)`)
	reDivByVar    = regexp.MustCompile(`/\s*b\b`)
	reLooseEq     = regexp.MustCompile(`[^=!<>]==[^=]|!=[^=]`)
)

// analyze scans the numbered code chunk for the first/strongest bug pattern and
// returns a populated models.AnalystResult. Found=false signals "no bug".
func (m *mockLLM) analyze(user string) models.AnalystResult {
	lines := parseNumbered(user)

	for _, ln := range lines {
		code := ln.code
		trimmed := strings.TrimSpace(code)
		if trimmed == "" {
			continue
		}

		// 1. SQL injection — Critical, 0.94.
		if reSQLKeyword.MatchString(code) && reSQLBuild.MatchString(code) {
			return result(models.BugSQLInjection, models.SeverityCritical, ln, 0.94,
				"User-controlled input is concatenated into a SQL statement, allowing SQL injection.")
		}

		// 2. Hardcoded secret — High, 0.90.
		if reHardcoded.MatchString(code) {
			return result(models.BugHardcodedSecret, models.SeverityHigh, ln, 0.90,
				"A credential is hardcoded in source, exposing it to anyone with repo access.")
		}

		// 3. Code injection via eval — Critical, 0.90 (bug_type insecure_default).
		if reEval.MatchString(code) {
			return result(models.BugInsecureDefault, models.SeverityCritical, ln, 0.90,
				"Dynamic eval of input enables arbitrary code execution.")
		}

		// 4. Null dereference — High, 0.88.
		if reNullIndex.MatchString(code) || reNullField.MatchString(code) || reNullUserIdx.MatchString(code) {
			return result(models.BugNullDeref, models.SeverityHigh, ln, 0.88,
				"A value that may be nil/None is dereferenced without a guard, risking a crash.")
		}

		// 5. Off-by-one — High, 0.82.
		if (reOffRangeLen.MatchString(code) && reOffPlusOne.MatchString(code)) ||
			reOffArrLen.MatchString(code) || reOffLenIdx.MatchString(code) {
			return result(models.BugOffByOne, models.SeverityHigh, ln, 0.82,
				"An index runs one position past the end of the collection (off-by-one).")
		}

		// 6. Missing error handling — Medium, 0.70.
		if reScanCall.MatchString(code) && !reErrAssign.MatchString(code) {
			return result(models.BugMissingError, models.SeverityMedium, ln, 0.70,
				"The result/error of this call is ignored, so failures pass silently.")
		}

		// 7. Logic (division-by-zero / loose equality) — High/Medium, 0.75.
		if reDivByVar.MatchString(code) {
			return result(models.BugLogic, models.SeverityHigh, ln, 0.75,
				"Division by a variable without a zero guard can panic or produce NaN.")
		}
		if reLooseEq.MatchString(code) {
			return result(models.BugLogic, models.SeverityMedium, ln, 0.75,
				"Loose equality (== / !=) performs type coercion; use strict equality.")
		}
	}

	return models.AnalystResult{Found: false}
}

// result builds a populated AnalystResult for a matched line. Evidence is the
// exact trimmed source line — the anti-hallucination key the analyst enforces.
func result(bugType string, sev models.Severity, ln numberedLine, conf float64, explanation string) models.AnalystResult {
	return models.AnalystResult{
		Found:       true,
		BugType:     bugType,
		Severity:    string(sev),
		LineStart:   ln.num,
		LineEnd:     ln.num,
		Evidence:    strings.TrimSpace(ln.code),
		Explanation: explanation,
		Confidence:  conf,
	}
}

// ----- TaskRerank heuristics -----------------------------------------------

// reRankCandidate matches an index-prefixed candidate line "[0] name — sig".
var reRankCandidate = regexp.MustCompile(`^\[(\d+)\]`)

// reRankSignal flags candidate text that mentions security/correctness-sensitive
// surface area worth ranking highly.
var reRankSignal = regexp.MustCompile(`(?i)(query|exec|eval|sql|auth|password|secret|token|div|index|null|nil)`)

// rerank scores each indexed candidate line, boosting those whose text matches a
// risk signal, and returns rankings sorted by descending confidence.
func (m *mockLLM) rerank(user string) models.RerankResult {
	var items []models.RerankItem
	for _, raw := range strings.Split(user, "\n") {
		mch := reRankCandidate.FindStringSubmatch(raw)
		if mch == nil {
			continue
		}
		idx, err := strconv.Atoi(mch[1])
		if err != nil {
			continue
		}
		conf := 0.35
		reason := "no risk signal in signature"
		if reRankSignal.MatchString(raw) {
			conf = 0.85
			reason = "signature touches security/correctness-sensitive surface"
		}
		items = append(items, models.RerankItem{Index: idx, Confidence: conf, Reason: reason})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Confidence > items[j].Confidence
	})
	return models.RerankResult{Rankings: items}
}

// ----- TaskFixGen heuristics -----------------------------------------------

// fixGen returns the original function with a single leading comment prepended,
// keeping it otherwise byte-identical so it stays syntactically valid. The
// comment token is chosen from the file extension named in the prompt so the
// patched file parses in its language (e.g. "#" for Python/Ruby, "//" for
// C-family/Go/JS).
func (m *mockLLM) fixGen(user string) models.FixResult {
	original := extractOriginal(user)
	rewritten := commentToken(user) + " Fixed by CodeSentinel\n" + original
	return models.FixResult{
		RewrittenFunction: rewritten,
		Confidence:        0.85,
		Rationale:         "Annotated the function as reviewed; mock generator preserves behavior.",
	}
}

// fixGenFileRe finds the target filename in the fixgen prompt ("... in auth.py.").
var fixGenFileRe = regexp.MustCompile(`\bin ([\w./-]+\.\w+)\b`)

// commentToken returns the single-line comment token for the language implied by
// the filename in the prompt, defaulting to "#".
func commentToken(user string) string {
	ext := ""
	if m := fixGenFileRe.FindStringSubmatch(user); m != nil {
		if dot := strings.LastIndexByte(m[1], '.'); dot >= 0 {
			ext = strings.ToLower(m[1][dot:])
		}
	}
	switch ext {
	case ".py", ".rb", ".sh", ".pl", ".yaml", ".yml":
		return "#"
	default:
		return "//"
	}
}

// extractOriginal pulls the source after an "ORIGINAL FUNCTION:" marker if
// present; otherwise it returns the trimmed prompt body.
func extractOriginal(user string) string {
	const marker = "ORIGINAL FUNCTION:"
	if i := strings.Index(user, marker); i >= 0 {
		return strings.TrimLeft(user[i+len(marker):], "\r\n")
	}
	return strings.TrimSpace(user)
}

// ----- TaskTestGen heuristics ----------------------------------------------

// langHintRe extracts a language hint like "language: go" from the prompt.
var langHintRe = regexp.MustCompile(`(?i)language\s*[:=]\s*(\w+)`)

// testGen returns a minimal passing test and an appropriate run command for the
// hinted language, defaulting to a no-op shell command.
func (m *mockLLM) testGen(user string) models.TestGenResult {
	lang := ""
	if mch := langHintRe.FindStringSubmatch(user); mch != nil {
		lang = strings.ToLower(mch[1])
	}
	switch lang {
	case "go", "golang":
		return models.TestGenResult{
			Filename: "sentinel_mock_test.go",
			TestCode: "package main\n\nimport \"testing\"\n\nfunc TestSentinelMock(t *testing.T) {}\n",
			RunCmd:   "go test ./...",
		}
	case "python", "py":
		return models.TestGenResult{
			Filename: "test_sentinel_mock.py",
			TestCode: "def test_sentinel_mock():\n    assert True\n",
			RunCmd:   "python3 -m pytest -q",
		}
	case "javascript", "js", "node":
		return models.TestGenResult{
			Filename: "sentinel_mock.test.js",
			TestCode: "test('sentinel mock', () => { expect(true).toBe(true); });\n",
			RunCmd:   "node --check sentinel_mock.test.js",
		}
	default:
		return models.TestGenResult{
			Filename: "sentinel_mock.txt",
			TestCode: "ok\n",
			RunCmd:   "true",
		}
	}
}
