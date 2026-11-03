package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/services"
	"github.com/saketh1125/argus/tools"
)

// Validator proves a fix compiles before it can become a PR. For each issue it
// applies the selected candidate to the file on disk under workdir, builds a
// SandboxSpec with a deterministic language-appropriate syntax check and runs it
// in the sandbox. On success
// it sets Issue.Validated=true; on syntax failure it retries once with the next
// ranked candidate; on final failure it marks Validated=false and Demoted=true.
type Validator struct {
	sandbox services.Sandbox
	parser  tools.CodeParser
	cfg     *config.Config
	rep     models.Reporter
}

// NewValidator wires the validator with its sandbox and parser dependencies.
func NewValidator(sandbox services.Sandbox, parser tools.CodeParser, cfg *config.Config, rep models.Reporter) *Validator {
	return &Validator{sandbox: sandbox, parser: parser, cfg: cfg, rep: rep}
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
	// These fields are trusted provenance and must not survive from model input,
	// an earlier attempt, or a mutated issue.
	iss.Validated = false
	iss.ValidationProvider = ""
	iss.ValidationProviderLive = false
	iss.ValidationLog = ""
	if iss.Fix == nil || len(iss.Fix.Candidates) == 0 {
		iss.Demoted = true
		iss.ValidationLog = "no fix candidate to validate"
		return
	}

	absPath, pathErr := safeRepoFile(workdir, iss.File)
	if pathErr != nil {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = "refused unsafe source path: " + pathErr.Error()
		return
	}
	lang := v.parser.Language(filepath.Ext(iss.File))
	if len(agtSyntaxCmd(lang, iss.File)) == 0 {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = fmt.Sprintf("no configured syntax validator for language %q", lang)
		return
	}
	original, rerr := os.ReadFile(absPath)
	if rerr != nil {
		v.rep.Log("validator: read %s: %v", iss.File, rerr)
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = fmt.Sprintf("source file unavailable; refusing isolated rewrite validation: %v", rerr)
		return
	}
	if len(original) > 4<<20 || !utf8.Valid(original) || strings.IndexByte(string(original), 0) >= 0 {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = "source file is binary or exceeds the patch safety limit"
		return
	}
	if iss.FunctionName == "" {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = "source function name missing; refusing ambiguous patch"
		return
	}
	sourceParsed, parseErr := v.parser.Parse(iss.File, string(original))
	if parseErr != nil || sourceParsed == nil {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = "source could not be parsed; refusing patch"
		return
	}
	var sourceMatches []models.Signature
	for _, sig := range sourceParsed.Signatures {
		if sig.Name == iss.FunctionName && sig.Kind != "class" {
			sourceMatches = append(sourceMatches, sig)
		}
	}
	if len(sourceMatches) != 1 || normalizeFunctionSource(sourceMatches[0].Body) != normalizeFunctionSource(iss.Fix.OriginalFunction) {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = fmt.Sprintf("source target %q is missing, ambiguous, or stale", iss.FunctionName)
		return
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
		if err := v.verifyRewrite(iss.File, iss.FunctionName, cand.RewrittenFunction); err != nil {
			lastLog = fmt.Sprintf("attempt %d: rewrite rejected: %v", attempt+1, err)
			continue
		}
		patched, perr := v.patchedContent(string(original), iss.Fix.OriginalFunction, cand.RewrittenFunction)
		if perr != nil {
			lastLog = fmt.Sprintf("attempt %d: apply failed: %v", attempt+1, perr)
			continue
		}

		spec := v.buildSpec(lang, iss.File, patched)
		if v.sandbox == nil {
			lastLog = fmt.Sprintf("attempt %d: sandbox is not configured", attempt+1)
			continue
		}
		iss.ValidationProvider = v.sandbox.Name()
		iss.ValidationProviderLive = v.sandbox.Live()
		res, serr := v.sandbox.Run(ctx, spec)
		if serr != nil {
			lastLog = fmt.Sprintf("attempt %d: sandbox error: %v", attempt+1, serr)
			continue
		}
		if ctx.Err() == nil && res.SuccessfulFor(spec) {
			originalSum := sha256.Sum256(original)
			patchedSum := sha256.Sum256([]byte(patched))
			cand.PatchedFile = patched
			cand.OriginalFile = string(original)
			cand.OriginalFileSHA256 = hex.EncodeToString(originalSum[:])
			cand.PatchedFileSHA256 = hex.EncodeToString(patchedSum[:])
			cand.Diff = tools.UnifiedDiff(iss.Fix.OriginalFunction, cand.RewrittenFunction, iss.File)
			selected := cand
			iss.Fix.Selected = &selected
			iss.Validated = true
			iss.Demoted = false
			iss.ValidationLog = strings.TrimSpace(boundedValidationText(res.Logs + "\n" + res.Stdout))
			return
		}
		lastLog = fmt.Sprintf("attempt %d: %s", attempt+1, sandboxResultFailure(ctx.Err(), spec, res))
	}

	iss.Fix.Selected = nil
	iss.Validated = false
	iss.Demoted = true
	iss.ValidationLog = lastLog
}

func sandboxResultFailure(ctxErr error, spec services.SandboxSpec, res *services.SandboxResult) string {
	if ctxErr != nil {
		return fmt.Sprintf("validation context ended: %v", ctxErr)
	}
	if res == nil {
		return "sandbox returned no result"
	}
	if res.TimedOut {
		return "sandbox command timed out"
	}
	if res.CommandsRun != len(spec.Commands) {
		return fmt.Sprintf("sandbox returned %d completed commands; expected %d", res.CommandsRun, len(spec.Commands))
	}
	if res.ExitCode != 0 {
		details := res.Stderr
		if details == "" {
			details = res.Logs
		}
		return fmt.Sprintf("sandbox command exited with code %d: %s", res.ExitCode, strings.TrimSpace(boundedValidationText(details)))
	}
	if !res.Success {
		return "sandbox reported failure"
	}
	return "sandbox result was inconsistent"
}

const maxValidationLogBytes = 64 << 10

func boundedValidationText(value string) string {
	if len(value) <= maxValidationLogBytes {
		return value
	}
	return value[:maxValidationLogBytes] + "\n[validation output truncated]"
}

func normalizeFunctionSource(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	return strings.TrimRight(source, "\n")
}

// patchedContent produces a complete file by a unique exact function edit.
// It fails closed: no isolated rewrite fallback is allowed into PR validation.
func (v *Validator) patchedContent(original, origFunc, rewrite string) (string, error) {
	if original == "" || origFunc == "" {
		return "", fmt.Errorf("original file/function is empty")
	}
	patched, err := tools.ApplyFunctionRewrite(original, origFunc, rewrite)
	if err != nil {
		return "", err
	}
	return patched, nil
}

func (v *Validator) verifyRewrite(file, functionName, rewrite string) error {
	parsed, err := v.parser.Parse(file, rewrite)
	if err != nil {
		return fmt.Errorf("cannot parse replacement: %w", err)
	}
	matches := 0
	for _, sig := range parsed.Signatures {
		if sig.Name == functionName && sig.Kind != "class" {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("replacement must contain exactly one %q declaration; found %d", functionName, matches)
	}
	return nil
}

func safeRepoFile(root, rel string) (string, error) {
	if !filepath.IsLocal(rel) || rel == "." {
		return "", fmt.Errorf("path must be repository-relative")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	candidate := filepath.Join(rootReal, filepath.Clean(rel))
	targetReal, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve source file: %w", err)
	}
	inside, err := filepath.Rel(rootReal, targetReal)
	if err != nil || inside == ".." || filepath.IsAbs(inside) || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source path resolves outside repository")
	}
	return targetReal, nil
}

// buildSpec assembles a SandboxSpec with a deterministic syntax check. Model
// generated test commands are excluded until commands use an explicit allowlist
// and argument arrays.
func (v *Validator) buildSpec(lang, relFile, patched string) services.SandboxSpec {
	sandboxPath := filepath.ToSlash(relFile)
	files := map[string]string{sandboxPath: patched}
	commands := agtSyntaxCmd(lang, sandboxPath)

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
