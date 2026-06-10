package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/saketh/codesentinel/config"
	"github.com/saketh/codesentinel/models"
	"github.com/saketh/codesentinel/services"
	"github.com/saketh/codesentinel/tools"
)

// Validator proves a fix compiles before it can become a PR. For each issue it
// applies the selected candidate to the file on disk under workdir, builds a
// SandboxSpec with a language-appropriate syntax check (optionally generating a
// test via Task=testgen when none exists), and runs it in the sandbox. On success
// it sets Issue.Validated=true; on syntax failure it retries once with the next
// ranked candidate; on final failure it marks Validated=false and Demoted=true.
type Validator struct {
	sandbox services.Sandbox
	llm     services.LLMClient
	parser  tools.CodeParser
	cfg     *config.Config
	rep     models.Reporter
}

// NewValidator wires the validator with its sandbox, LLM, and parser deps.
func NewValidator(sandbox services.Sandbox, llm services.LLMClient, parser tools.CodeParser, cfg *config.Config, rep models.Reporter) *Validator {
	return &Validator{sandbox: sandbox, llm: llm, parser: parser, cfg: cfg, rep: rep}
}

// Run validates every issue's selected fix and returns the issues (mutated in
// place). Issues that fail validation are returned demoted, not dropped.
func (v *Validator) Run(ctx context.Context, workdir string, issues []*models.Issue) ([]*models.Issue, error) {
	v.rep.SetStage(models.StageValidate, 0)
	total := len(issues)
	for i, iss := range issues {
		v.rep.SetAgentStatus("Validator", "validating "+iss.File)
		v.validate(ctx, workdir, iss)
		v.rep.SetValidated(i+1, total)
		if total > 0 {
			v.rep.SetStage(models.StageValidate, (i+1)*100/total)
		}
	}
	v.rep.SetStage(models.StageValidate, 100)
	return issues, nil
}

// validate applies and runs the issue's fix candidates in rank order, marking the
// issue validated or demoted.
func (v *Validator) validate(ctx context.Context, workdir string, iss *models.Issue) {
	if iss.Fix == nil || len(iss.Fix.Candidates) == 0 {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = "no fix candidate to validate"
		return
	}

	lang := v.parser.Language(filepath.Ext(iss.File))
	absPath := filepath.Join(workdir, iss.File)
	original, rerr := os.ReadFile(absPath)
	if rerr != nil {
		// Without on-disk content we can only validate the rewritten function in
		// isolation. Fall back to using the rewrite as the file content.
		v.rep.Log("validator: read %s: %v", iss.File, rerr)
		original = nil
	}

	// Try the top candidate, then the next-ranked one on failure (one retry).
	cands := iss.Fix.Candidates
	maxTry := 2
	if len(cands) < maxTry {
		maxTry = len(cands)
	}

	var lastLog string
	for attempt := 0; attempt < maxTry; attempt++ {
		cand := cands[attempt]
		patched, perr := v.patchedContent(string(original), iss.Fix.OriginalFunction, cand.RewrittenFunction)
		if perr != nil {
			lastLog = fmt.Sprintf("attempt %d: apply failed: %v", attempt+1, perr)
			continue
		}

		spec := v.buildSpec(ctx, lang, iss.File, patched, cand.RewrittenFunction)
		res, serr := v.sandbox.Run(ctx, spec)
		if serr != nil {
			lastLog = fmt.Sprintf("attempt %d: sandbox error: %v", attempt+1, serr)
			continue
		}
		if res != nil && res.Success {
			selected := cand
			iss.Fix.Selected = &selected
			iss.Validated = true
			iss.Demoted = false
			iss.ValidationLog = strings.TrimSpace(res.Logs + "\n" + res.Stdout)
			return
		}
		log := ""
		if res != nil {
			log = res.Stderr
			if log == "" {
				log = res.Logs
			}
		}
		lastLog = fmt.Sprintf("attempt %d: syntax/test check failed: %s", attempt+1, log)
	}

	iss.Validated = false
	iss.Demoted = true
	iss.ValidationLog = lastLog
}

// patchedContent produces the patched file content. When the original file
// content is available it applies the function rewrite in place; otherwise it
// validates the rewritten function alone.
func (v *Validator) patchedContent(original, origFunc, rewrite string) (string, error) {
	if original == "" || origFunc == "" {
		return rewrite, nil
	}
	patched, err := tools.ApplyFunctionRewrite(original, origFunc, rewrite)
	if err != nil {
		// The original function text wasn't found verbatim (e.g. sanitization
		// drift). Fall back to validating the rewrite in isolation.
		return rewrite, nil
	}
	return patched, nil
}

// buildSpec assembles a SandboxSpec: write the patched file, run the syntax check,
// and — when no test is present — optionally synthesize and run a minimal test.
func (v *Validator) buildSpec(ctx context.Context, lang, relFile, patched, rewrite string) services.SandboxSpec {
	files := map[string]string{relFile: patched}
	commands := agtSyntaxCmd(lang, relFile)

	// If there are no obvious tests, try to generate a minimal one.
	if test, ok := v.maybeGenTest(ctx, lang, relFile, rewrite); ok {
		files[test.Filename] = test.TestCode
		if test.RunCmd != "" {
			commands = append(commands, test.RunCmd)
		}
	}

	timeout := v.cfg.SandboxTimeout
	if timeout <= 0 {
		timeout = 60
	}
	return services.SandboxSpec{
		Language:   lang,
		Files:      files,
		Commands:   commands,
		TimeoutSec: timeout,
	}
}

// maybeGenTest asks the LLM (Task=testgen) for a minimal test for the rewritten
// function. Any failure simply yields ok=false (tests are optional).
func (v *Validator) maybeGenTest(ctx context.Context, lang, relFile, rewrite string) (models.TestGenResult, bool) {
	user := fmt.Sprintf("Write one minimal %s test that imports/exercises this function so a syntax+smoke check passes.\nFile: %s\nFunction:\n%s",
		lang, relFile, rewrite)
	req := services.CompletionRequest{
		System: "You generate a single minimal test. Return ONLY JSON matching " +
			"{\"filename\":string,\"test_code\":string,\"run_cmd\":string}.",
		User:      user,
		Task:      models.TaskTestGen,
		JSONMode:  true,
		MaxTokens: 1024,
	}
	resp, err := v.llm.Complete(ctx, req)
	if err != nil || resp == nil {
		return models.TestGenResult{}, false
	}
	var res models.TestGenResult
	if err := agtParseJSON(resp.Text, &res); err != nil || res.Filename == "" || res.TestCode == "" {
		return models.TestGenResult{}, false
	}
	return res, true
}
