package agents

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/saketh/codesentinel/config"
	"github.com/saketh/codesentinel/models"
	"github.com/saketh/codesentinel/services"
	"github.com/saketh/codesentinel/tools"
)

const (
	// minEvidenceLen and minAnalystConfidence are the anti-hallucination guards:
	// an analyst result is rejected unless it quotes at least this much evidence
	// and is at least this confident (design §4.3).
	minEvidenceLen       = 10
	minAnalystConfidence = 0.40

	// analystTimeout bounds each analyst goroutine (design §6).
	analystTimeout = 30 * time.Second
)

// Analyst inspects localized code chunks for bugs. It fans out one goroutine per
// LocationTarget, capped at cfg.AnalystParallel via a semaphore, each performing
// an LLM Task=analyze call with a 30s context. Confirmed issues are collected via
// a buffered channel and reported through rep.AddIssue.
type Analyst struct {
	llm services.LLMClient
	cfg *config.Config
	rep models.Reporter
}

// NewAnalyst wires the analyst with its LLM dependency.
func NewAnalyst(llm services.LLMClient, cfg *config.Config, rep models.Reporter) *Analyst {
	return &Analyst{llm: llm, cfg: cfg, rep: rep}
}

// Run analyzes every target in parallel and returns the confirmed, non-nil issues.
func (a *Analyst) Run(ctx context.Context, idx *models.RepoIndex, targets []models.LocationTarget) ([]*models.Issue, error) {
	a.rep.SetStage(models.StageAnalyze, 0)
	if len(targets) == 0 {
		return nil, nil
	}

	parallel := a.cfg.AnalystParallel
	if parallel < 1 {
		parallel = 1
	}
	sem := make(chan struct{}, parallel)
	resultCh := make(chan *models.Issue, len(targets))

	var wg sync.WaitGroup
	var done int
	var doneMu sync.Mutex
	total := len(targets)

	for i, t := range targets {
		wg.Add(1)
		go func(n int, target models.LocationTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			label := fmt.Sprintf("Analyst-%d", n+1)
			a.rep.SetAgentStatus(label, "analyzing "+target.File)

			gctx, cancel := context.WithTimeout(ctx, analystTimeout)
			defer cancel()
			issue := a.analyze(gctx, idx, target)
			resultCh <- issue // nil if rejected

			doneMu.Lock()
			done++
			a.rep.SetFilesProgress(done, total)
			doneMu.Unlock()
		}(i, t)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var issues []*models.Issue
	for issue := range resultCh {
		if issue != nil {
			issues = append(issues, issue)
			a.rep.AddIssue(issue)
		}
	}
	a.rep.SetStage(models.StageAnalyze, 100)
	a.rep.Log("analyst: confirmed %d issues from %d targets", len(issues), total)
	return issues, nil
}

// analyze runs a single LLM analysis on one target and returns an Issue or nil.
// It enforces the evidence-length and confidence guards.
func (a *Analyst) analyze(ctx context.Context, idx *models.RepoIndex, t models.LocationTarget) *models.Issue {
	base := t.LineStart
	if base < 1 {
		base = 1
	}
	numbered := tools.NumberedChunk(t.Code, base)

	var ctxNote string
	if t.FunctionName != "" {
		if node := idx.GetCallGraph(t.FunctionName); node != nil {
			if len(node.Calls) > 0 || len(node.CalledBy) > 0 {
				ctxNote = fmt.Sprintf("\nCall graph: %s calls %v; called by %v.\n", t.FunctionName, node.Calls, node.CalledBy)
			}
		}
	}

	user := fmt.Sprintf("Analyze this code for a single, concrete bug.\nFile: %s\nFunction: %s\n%s\nCode:\n%s",
		t.File, t.FunctionName, ctxNote, numbered)

	req := services.CompletionRequest{
		System: "You are a security and correctness analyst. Inspect the code for ONE concrete bug. " +
			"Return ONLY JSON matching {\"found\":bool,\"bug_type\":string,\"severity\":\"Critical|High|Medium|Low\"," +
			"\"line_start\":int,\"line_end\":int,\"evidence\":string,\"explanation\":string,\"confidence\":0..1," +
			"\"function_name\":string}. Evidence MUST be an exact quote of the offending source line(s). " +
			"Use the absolute line numbers shown after LINE_BASE. If there is no real bug, set found=false.",
		User:      user,
		Task:      models.TaskAnalyze,
		JSONMode:  true,
		MaxTokens: 1024,
	}

	resp, err := a.llm.Complete(ctx, req)
	if err != nil || resp == nil {
		a.rep.Log("analyst: complete %s: %v", t.File, err)
		return nil
	}

	var res models.AnalystResult
	if err := agtParseJSON(resp.Text, &res); err != nil {
		a.rep.Log("analyst: parse %s: %v", t.File, err)
		return nil
	}

	// Anti-hallucination enforcement.
	if !res.Found {
		return nil
	}
	if len(res.Evidence) < minEvidenceLen || res.Confidence < minAnalystConfidence {
		return nil
	}

	lineStart := res.LineStart
	if lineStart <= 0 {
		lineStart = t.LineStart
	}
	lineEnd := res.LineEnd
	if lineEnd < lineStart {
		lineEnd = t.LineEnd
	}
	fnName := res.FunctionName
	if fnName == "" {
		fnName = t.FunctionName
	}

	issue := &models.Issue{
		File:         t.File,
		FunctionName: fnName,
		LineStart:    lineStart,
		LineEnd:      lineEnd,
		BugType:      res.BugType,
		Severity:     models.ParseSeverity(res.Severity),
		Evidence:     res.Evidence,
		Explanation:  res.Explanation,
		Confidence:   agtClamp(res.Confidence, 0, 1),
	}
	issue.ID = agtIssueID(issue.DedupKey())
	return issue
}
