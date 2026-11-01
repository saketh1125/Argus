package agents

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/services"
	"github.com/saketh1125/argus/tools"
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
	validatedCount := 0
	for i, iss := range issues {
		v.rep.SetAgentStatus("Validator", "validating "+iss.File)
		v.validate(ctx, workdir, iss)
		if iss.Validated {
			validatedCount++
		}
		v.rep.SetValidated(validatedCount, total)
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
	iss.Validated = false
	if iss.Fix != nil {
		iss.Fix.Selected = nil
	}
	if v.sandbox == nil || !v.sandbox.IsolationVerified() {
		iss.Demoted = true
		iss.ValidationLog = "validation unavailable: no verified isolated sandbox"
		return
	}
	if iss.Fix == nil || len(iss.Fix.Candidates) == 0 {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = "no fix candidate to validate"
		return
	}

	lang := v.parser.Language(filepath.Ext(iss.File))
	root, rootErr := filepath.EvalSymlinks(workdir)
	absPath := filepath.Join(root, iss.File)
	resolved, pathErr := filepath.EvalSymlinks(absPath)
	rel, relErr := filepath.Rel(root, resolved)
	if rootErr != nil || pathErr != nil || relErr != nil || filepath.IsAbs(iss.File) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = "validation unavailable: invalid source path"
		return
	}
	original, rerr := os.ReadFile(resolved)
	if rerr != nil || len(original) == 0 {
		iss.Validated = false
		iss.Demoted = true
		iss.ValidationLog = "validation unavailable: source file missing or empty"
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
		if cand.Repository == "" || cand.BaseCommit == "" || cand.Repository != iss.Repository || cand.BaseCommit != iss.BaseCommit {
			lastLog = "finding and candidate provenance mismatch"
			continue
		}
		if cand.BaseHash == "" || cand.BaseHash != fmt.Sprintf("%x", sha256.Sum256(original)) {
			lastLog = "base file identity mismatch"
			continue
		}
		if err := tools.VerifyPinnedFile(ctx, workdir, cand.Repository, cand.BaseCommit, filepath.ToSlash(iss.File), original); err != nil {
			lastLog = fmt.Sprintf("pinned source unavailable: %v", err)
			continue
		}
		patched, perr := v.patchedContent(string(original), iss.Fix.OriginalFunction, cand.RewrittenFunction)
		if perr != nil {
			lastLog = fmt.Sprintf("attempt %d: apply failed: %v", attempt+1, perr)
			continue
		}

		fullDiff := tools.UnifiedDiff(string(original), patched, iss.File)
		if err := tools.VerifyUnifiedDiff(ctx, filepath.ToSlash(iss.File), string(original), patched, fullDiff); err != nil {
			lastLog = fmt.Sprintf("complete-file patch integrity failed: %v", err)
			continue
		}
		files, snapshotErr := tools.SnapshotPinnedRepository(ctx, workdir, cand.Repository, cand.BaseCommit)
		if snapshotErr != nil {
			lastLog = fmt.Sprintf("repository snapshot unavailable: %v", snapshotErr)
			continue
		}
		relFile := filepath.ToSlash(iss.File)
		if files[relFile] != string(original) {
			lastLog = "snapshot target differs from pinned original"
			continue
		}
		files[relFile] = patched
		provenance, provenanceErr := models.NewPatchProvenance(cand.Repository, cand.BaseCommit, relFile, original, []byte(patched), []byte(fullDiff))
		if provenanceErr != nil || provenance.OriginalHash != cand.BaseHash {
			lastLog = "patch provenance unavailable"
			continue
		}
		spec := v.buildSpec(lang, relFile, files, cand.Repository, cand.BaseCommit)
		spec.TargetPath = relFile
		spec.OriginalHash = provenance.OriginalHash
		spec.ResultHash = provenance.ResultHash
		spec.PatchDigest = provenance.PatchDigest
		if len(spec.Commands) == 0 {
			lastLog = "validation unavailable: unsupported language"
			continue
		}
		res, serr := v.sandbox.Run(ctx, spec)
		if serr != nil {
			lastLog = fmt.Sprintf("attempt %d: sandbox error: %v", attempt+1, serr)
			continue
		}
		if res != nil && res.Success && res.ExitCode == 0 && ctx.Err() == nil {
			if err := provenance.VerifyBytes(original, []byte(spec.Files[relFile]), []byte(fullDiff)); err != nil {
				lastLog = "staged patch changed during validation"
				continue
			}
			resolvedNow, pathErr := filepath.EvalSymlinks(absPath)
			if pathErr != nil || resolvedNow != resolved {
				lastLog = "base file changed during validation"
				continue
			}
			current, readErr := os.ReadFile(resolvedNow)
			if readErr != nil || fmt.Sprintf("%x", sha256.Sum256(current)) != cand.BaseHash {
				lastLog = "base file changed during validation"
				continue
			}
			// Carry the validated full-file content forward so the PR agent can
			// commit a single-function edit instead of replacing the whole file.
			if err := tools.VerifyPinnedFile(ctx, workdir, cand.Repository, cand.BaseCommit, filepath.ToSlash(iss.File), current); err != nil {
				lastLog = fmt.Sprintf("pinned source changed during validation: %v", err)
				continue
			}
			if !spec.RepositoryContext || !res.RepositoryContext || (res.Level != services.ValidationTargetedTests && res.Level != services.ValidationProjectTests) {
				lastLog = "validation unavailable: insufficient repository-context test evidence"
				continue
			}
			cand.PatchedFile = patched
			cand.Diff = fullDiff
			if cand.Diff == "" {
				lastLog = "empty complete-file diff"
				continue
			}
			cand.PatchDigest = provenance.PatchDigest
			cand.ResultHash = provenance.ResultHash
			selected := cand
			iss.Fix.Selected = &selected
			iss.Validated = true
			iss.Demoted = false
			iss.ValidationLog = strings.TrimSpace(res.Logs + "\n" + res.Stdout)
			return
		}
		log := ""
		if res != nil && res.Success && (!res.RepositoryContext || (res.Level != services.ValidationTargetedTests && res.Level != services.ValidationProjectTests)) {
			log = "validation unavailable: insufficient repository-context test evidence"
		}
		if res != nil && log == "" {
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
	if original == "" || origFunc == "" || strings.TrimSpace(rewrite) == "" {
		return "", fmt.Errorf("missing full source or rewrite")
	}
	patched, err := tools.ApplyFunctionRewrite(original, origFunc, rewrite)
	if err != nil {
		return "", err
	}
	if patched == original {
		return "", fmt.Errorf("empty patch")
	}
	return patched, nil
}

// buildSpec stages the bounded committed tree and target patch. It does not set
// RepositoryContext: the provider has not verified the revision in isolation,
// and the only configured command is a syntax check, not a repository test.
func (v *Validator) buildSpec(lang, relFile string, files map[string]string, repository, commit string) services.SandboxSpec {
	commands := agtSyntaxCmd(lang, relFile)
	timeout := v.cfg.SandboxTimeout
	if timeout <= 0 {
		timeout = 60
	}
	return services.SandboxSpec{Repository: repository, BaseCommit: commit, Language: lang, Files: files, Commands: commands, TimeoutSec: timeout}
}
