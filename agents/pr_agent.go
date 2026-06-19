package agents

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/tools"
)

// PRAgent translates validated fixes into GitHub artifacts. For each validated,
// non-demoted issue it creates a branch, commits the rewritten function, and opens
// a PR with the design §4.7 body template. All summary issues plus demoted issues
// are compiled into a single GitHub issue with a markdown findings table. It
// returns a *models.RunReport with the fields it can populate (PRsRaised,
// SummaryIssue, DemotedIssues, IssuesFound); main fills RepoURL/Duration/Mode.
type PRAgent struct {
	gh    tools.GitHubClient
	cfg   *config.Config
	rep   models.Reporter
	dedup *models.DedupStore // nil = no cross-run deduplication
}

// NewPRAgent wires the PR agent with its GitHub dependency.
// Pass a non-nil DedupStore to skip findings whose PR was already raised in a
// previous run; pass nil to disable cross-run deduplication.
func NewPRAgent(gh tools.GitHubClient, cfg *config.Config, rep models.Reporter, dedup *models.DedupStore) *PRAgent {
	return &PRAgent{gh: gh, cfg: cfg, rep: rep, dedup: dedup}
}

// Run raises PRs for validated fixes and a single summary issue for the rest,
// returning a populated RunReport. The validated slice may contain demoted issues
// (validator demotes in place); those are routed to the summary, not to PRs.
func (p *PRAgent) Run(ctx context.Context, owner, repo, base string, validated []*models.Issue, summary []*models.Issue) (*models.RunReport, error) {
	p.rep.SetStage(models.StagePR, 0)
	if base == "" {
		base = "main"
	}

	report := &models.RunReport{}

	// Split validated input into true PR candidates and demoted ones.
	var prCandidates []*models.Issue
	var demoted []*models.Issue
	for _, iss := range validated {
		if iss == nil {
			continue
		}
		if iss.Validated && !iss.Demoted {
			prCandidates = append(prCandidates, iss)
		} else {
			demoted = append(demoted, iss)
		}
	}
	report.DemotedIssues = demoted
	report.IssuesFound = len(validated) + len(summary)

	// Raise one PR per validated issue.
	for i, iss := range prCandidates {
		// Cross-run deduplication: skip if a PR was already raised for this
		// finding in a previous run.
		if p.dedup != nil && p.dedup.Has(iss.DedupKey()) {
			p.rep.Log("pragent: skipping %s (already raised in prior run)", iss.DedupKey())
			continue
		}
		p.rep.SetAgentStatus("PRAgent", "raising PR for "+iss.File)
		pr, err := p.raisePR(ctx, owner, repo, base, iss)
		if err != nil {
			p.rep.Log("pragent: PR for %s failed: %v", iss.File, err)
			// Demote on failure so it still surfaces in the summary.
			iss.Demoted = true
			report.DemotedIssues = append(report.DemotedIssues, iss)
			continue
		}
		// Record the dedup key so future runs skip this finding.
		if p.dedup != nil {
			p.dedup.Record(iss.DedupKey())
		}
		report.PRsRaised = append(report.PRsRaised, pr)
		p.rep.SetPRsRaised(len(report.PRsRaised))
		if len(prCandidates) > 0 {
			p.rep.SetStage(models.StagePR, (i+1)*80/len(prCandidates))
		}
	}

	// Compile the single summary issue from summary + demoted findings.
	summaryRows := append([]*models.Issue{}, summary...)
	summaryRows = append(summaryRows, report.DemotedIssues...)
	if len(summaryRows) > 0 {
		title := fmt.Sprintf("[CodeSentinel] Analysis Summary — %d findings", len(summaryRows))
		body := p.summaryBody(summaryRows)
		if _, url, err := p.gh.CreateIssue(ctx, owner, repo, title, body); err != nil {
			p.rep.Log("pragent: summary issue failed: %v", err)
		} else {
			report.SummaryIssue = url
		}
	}

	p.rep.SetStage(models.StagePR, 100)
	p.rep.Log("pragent: raised %d PRs, %d demoted", len(report.PRsRaised), len(report.DemotedIssues))
	return report, nil
}

// raisePR creates the branch, commits the rewritten function, and opens the PR.
// If the branch already exists (422 from GitHub), a timestamp suffix is appended
// and the creation is retried once — preventing collision on re-runs.
func (p *PRAgent) raisePR(ctx context.Context, owner, repo, base string, iss *models.Issue) (models.PRResult, error) {
	branch := fmt.Sprintf("codesentinel/fix-%s-%s-%d",
		agtSanitizeForBranch(iss.BugType),
		agtSanitizeForBranch(agtBaseName(iss.File)),
		iss.LineStart)

	if err := p.gh.CreateBranch(ctx, owner, repo, base, branch); err != nil {
		if strings.Contains(err.Error(), "422") || strings.Contains(err.Error(), "already exists") {
			// Append a Unix-second suffix and retry once.
			branch = fmt.Sprintf("%s-%d", branch, time.Now().Unix())
			if retryErr := p.gh.CreateBranch(ctx, owner, repo, base, branch); retryErr != nil {
				return models.PRResult{}, fmt.Errorf("create branch (retry): %w", retryErr)
			}
		} else {
			return models.PRResult{}, fmt.Errorf("create branch: %w", err)
		}
	}

	// PRAgent has no workdir, so it commits the best available content: the
	// selected rewritten function. The mechanically-computed diff carries the
	// real change in the PR body.
	content := ""
	diff := ""
	if iss.Fix != nil && iss.Fix.Selected != nil {
		content = iss.Fix.Selected.RewrittenFunction
		diff = iss.Fix.Selected.Diff
	}

	commitMsg := fmt.Sprintf("[CodeSentinel] Fix %s %s in %s:%d", iss.Severity, iss.BugType, iss.File, iss.LineStart)
	if err := p.gh.CommitFile(ctx, owner, repo, tools.CommitSpec{
		Branch:  branch,
		Path:    iss.File,
		Content: content,
		Message: commitMsg,
	}); err != nil {
		return models.PRResult{}, fmt.Errorf("commit: %w", err)
	}

	title := fmt.Sprintf("[CodeSentinel] Fix %s %s in %s", iss.Severity, iss.BugType, iss.File)
	number, url, err := p.gh.CreatePR(ctx, owner, repo, tools.PRSpec{
		Title: title,
		Body:  p.prBody(iss, diff),
		Head:  branch,
		Base:  base,
	})
	if err != nil {
		return models.PRResult{}, fmt.Errorf("create PR: %w", err)
	}
	return models.PRResult{Number: number, URL: url, Branch: branch, Issue: iss}, nil
}

// prBody renders the design §4.7 PR body template for an issue and its diff.
func (p *PRAgent) prBody(iss *models.Issue, diff string) string {
	var b strings.Builder
	b.WriteString("## CodeSentinel Analysis Report\n\n")
	fmt.Fprintf(&b, "**Type:** %s\n", iss.BugType)
	fmt.Fprintf(&b, "**Severity:** %s\n", iss.Severity)
	fmt.Fprintf(&b, "**Confidence:** %.0f%%\n", iss.Confidence*100)
	b.WriteString("**Validation:** ✅ Passed E2B sandbox\n\n")
	b.WriteString("### Evidence\n")
	fmt.Fprintf(&b, "File: `%s` | Lines: %d–%d\n\n", iss.File, iss.LineStart, iss.LineEnd)
	for _, line := range strings.Split(strings.TrimRight(iss.Evidence, "\n"), "\n") {
		fmt.Fprintf(&b, "> %s\n", line)
	}
	b.WriteString("\n### Why This Is a Bug\n")
	b.WriteString(iss.Explanation + "\n\n")
	b.WriteString("### Fix Applied\n")
	b.WriteString("```diff\n")
	b.WriteString(strings.TrimRight(diff, "\n") + "\n")
	b.WriteString("```\n\n")
	b.WriteString("---\n*Generated by CodeSentinel — Autonomous Multi-Agent Bug Detection System*\n")
	return b.String()
}

// summaryBody renders a markdown table of all summary/demoted findings.
func (p *PRAgent) summaryBody(issues []*models.Issue) string {
	// Most-severe-first for readability.
	rows := append([]*models.Issue{}, issues...)
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Severity.Rank() > rows[j].Severity.Rank()
	})

	var b strings.Builder
	fmt.Fprintf(&b, "## CodeSentinel Analysis Summary — %d findings\n\n", len(rows))
	b.WriteString("These findings were not auto-fixed (low severity/confidence, or failed validation).\n\n")
	b.WriteString("| Severity | Type | File | Lines | Confidence | Status | Explanation |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, iss := range rows {
		status := "summary"
		if iss.Demoted {
			status = "validation failed"
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` | %d–%d | %.0f%% | %s | %s |\n",
			iss.Severity, iss.BugType, iss.File, iss.LineStart, iss.LineEnd,
			iss.Confidence*100, status, summaryEscape(iss.Explanation))
	}
	b.WriteString("\n---\n*Generated by CodeSentinel — Autonomous Multi-Agent Bug Detection System*\n")
	return b.String()
}

// summaryEscape flattens newlines and pipes so an explanation fits a table cell.
func summaryEscape(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}
