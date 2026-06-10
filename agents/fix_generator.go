package agents

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/saketh/codesentinel/config"
	"github.com/saketh/codesentinel/models"
	"github.com/saketh/codesentinel/services"
	"github.com/saketh/codesentinel/tools"
)

// FixGenerator produces ranked fix candidates for each issue. It works SEQUENTIALLY
// per issue (to respect the RPM budget): it fetches the original function text from
// the index, calls the LLM (Task=fixgen) cfg.FixCandidates times across an ascending
// temperature schedule, computes a unified diff for each rewrite, ranks candidates
// by confidence, and attaches a models.Fix with the top candidate Selected.
type FixGenerator struct {
	llm services.LLMClient
	cfg *config.Config
	rep models.Reporter
}

// NewFixGenerator wires the fix generator with its LLM dependency.
func NewFixGenerator(llm services.LLMClient, cfg *config.Config, rep models.Reporter) *FixGenerator {
	return &FixGenerator{llm: llm, cfg: cfg, rep: rep}
}

// Run attaches a Fix to every issue (where an original function can be found) and
// returns the issues. Issues whose original function cannot be located are passed
// through unchanged (no Fix), so the validator can demote them.
func (f *FixGenerator) Run(ctx context.Context, idx *models.RepoIndex, issues []*models.Issue) ([]*models.Issue, error) {
	f.rep.SetStage(models.StageFix, 0)
	total := len(issues)
	for i, iss := range issues {
		f.rep.SetAgentStatus("FixGenerator", "fixing "+iss.File)
		f.generate(ctx, idx, iss)
		if total > 0 {
			f.rep.SetStage(models.StageFix, (i+1)*100/total)
		}
	}
	f.rep.SetStage(models.StageFix, 100)
	f.rep.Log("fixgen: processed %d issues", total)
	return issues, nil
}

// generate attaches a Fix to a single issue, or leaves it without one if the
// original function text cannot be recovered.
func (f *FixGenerator) generate(ctx context.Context, idx *models.RepoIndex, iss *models.Issue) {
	orig := f.originalFunction(idx, iss)
	if orig == "" {
		f.rep.Log("fixgen: no original function for %s:%s", iss.File, iss.FunctionName)
		return
	}

	fix := &models.Fix{OriginalFunction: orig}
	temps := agtTempSchedule(f.cfg.FixCandidates)

	for _, temp := range temps {
		cand, ok := f.candidate(ctx, idx, iss, orig, temp)
		if !ok {
			continue
		}
		fix.Candidates = append(fix.Candidates, cand)
	}

	if len(fix.Candidates) == 0 {
		return
	}

	// Rank by confidence descending.
	sort.SliceStable(fix.Candidates, func(i, j int) bool {
		return fix.Candidates[i].Confidence > fix.Candidates[j].Confidence
	})
	top := fix.Candidates[0]
	fix.Selected = &top
	iss.Fix = fix
}

// candidate performs one fixgen LLM call at the given temperature and returns the
// resulting FixCandidate (with a programmatically computed diff).
func (f *FixGenerator) candidate(ctx context.Context, idx *models.RepoIndex, iss *models.Issue, orig string, temp float64) (models.FixCandidate, bool) {
	var deps string
	if len(iss.DependentFiles) > 0 {
		deps = "Dependent files: " + strings.Join(iss.DependentFiles, ", ") + "\n"
	}

	// The raw function source goes last, after the ORIGINAL FUNCTION: marker, so
	// the model rewrites verbatim source (not line-numbered analysis text). The
	// rewritten function is later diffed programmatically against this same orig.
	user := fmt.Sprintf(
		"Fix this %s bug (severity %s) in %s.\nEvidence:\n%s\nExplanation:\n%s\n%s"+
			"Return the COMPLETE corrected function, preserving its name and signature.\n\n"+
			"ORIGINAL FUNCTION:\n%s",
		iss.BugType, iss.Severity, iss.File, iss.Evidence, iss.Explanation, deps, orig)

	req := services.CompletionRequest{
		System: "You are an expert software engineer. Rewrite the given function to fix the described bug. " +
			"Return ONLY JSON matching {\"rewritten_function\":string,\"confidence\":0..1,\"rationale\":string}. " +
			"rewritten_function must be the full corrected function source, not a diff.",
		User:        user,
		Task:        models.TaskFixGen,
		Temperature: temp,
		JSONMode:    true,
		MaxTokens:   2048,
	}

	resp, err := f.llm.Complete(ctx, req)
	if err != nil || resp == nil {
		f.rep.Log("fixgen: complete %s@%.1f: %v", iss.File, temp, err)
		return models.FixCandidate{}, false
	}

	var res models.FixResult
	if err := agtParseJSON(resp.Text, &res); err != nil || strings.TrimSpace(res.RewrittenFunction) == "" {
		f.rep.Log("fixgen: parse %s@%.1f: %v", iss.File, temp, err)
		return models.FixCandidate{}, false
	}

	diff := tools.UnifiedDiff(orig, res.RewrittenFunction, iss.File)
	return models.FixCandidate{
		RewrittenFunction: res.RewrittenFunction,
		Diff:              diff,
		Confidence:        agtClamp(res.Confidence, 0, 1),
		Temperature:       temp,
		Rationale:         res.Rationale,
	}, true
}

// originalFunction recovers the exact source text of the issue's function from the
// index, preferring a parser-extracted Body (matched by file+name) and falling
// back to a line-range chunk of the file. The returned text must match the file
// verbatim so tools.ApplyFunctionRewrite can locate it.
func (f *FixGenerator) originalFunction(idx *models.RepoIndex, iss *models.Issue) string {
	if idx == nil {
		return ""
	}
	if iss.FunctionName != "" {
		if sig, ok := tools.FunctionSource(idx, iss.File, iss.FunctionName); ok && sig.Body != "" {
			return sig.Body
		}
	}
	if file, ok := idx.GetFile(iss.File); ok {
		return tools.GetCodeChunk(file.Content, iss.LineStart, iss.LineEnd)
	}
	return ""
}
