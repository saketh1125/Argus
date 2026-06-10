package agents

import (
	"sort"

	"github.com/saketh/codesentinel/config"
	"github.com/saketh/codesentinel/models"
)

// Aggregator deduplicates and gates analyst output before fix generation. It uses
// no LLM. Issues sharing a DedupKey collapse to the highest-confidence one.
// PR-eligible issues (severity Critical/High AND confidence >= cfg.ConfidenceGate)
// are enriched with DependentFiles from the call graph and routed to fix
// generation; everything else goes to the summary.
type Aggregator struct {
	cfg *config.Config
	rep models.Reporter
}

// NewAggregator wires the aggregator with config and reporter.
func NewAggregator(cfg *config.Config, rep models.Reporter) *Aggregator {
	return &Aggregator{cfg: cfg, rep: rep}
}

// Run deduplicates issues, then splits them into forFix (PR-eligible, enriched)
// and summary (everything else).
func (a *Aggregator) Run(idx *models.RepoIndex, issues []*models.Issue) (forFix []*models.Issue, summary []*models.Issue) {
	a.rep.SetStage(models.StageAggregate, 0)

	// Deduplicate by DedupKey, keeping the highest-confidence representative.
	bestByKey := make(map[string]*models.Issue)
	order := make([]string, 0, len(issues))
	for _, iss := range issues {
		if iss == nil {
			continue
		}
		key := iss.DedupKey()
		if cur, ok := bestByKey[key]; ok {
			if iss.Confidence > cur.Confidence {
				bestByKey[key] = iss
			}
			continue
		}
		bestByKey[key] = iss
		order = append(order, key)
	}

	deduped := make([]*models.Issue, 0, len(order))
	for _, key := range order {
		deduped = append(deduped, bestByKey[key])
	}

	// Gate and enrich.
	for _, iss := range deduped {
		if iss.Severity.IsPREligible() && iss.Confidence >= a.cfg.ConfidenceGate {
			if iss.FunctionName != "" && idx != nil {
				iss.DependentFiles = idx.DependentFiles(iss.FunctionName)
			}
			forFix = append(forFix, iss)
		} else {
			summary = append(summary, iss)
		}
	}

	// Most-severe-first ordering for stable, predictable downstream processing.
	sort.SliceStable(forFix, func(i, j int) bool {
		if forFix[i].Severity.Rank() != forFix[j].Severity.Rank() {
			return forFix[i].Severity.Rank() > forFix[j].Severity.Rank()
		}
		return forFix[i].Confidence > forFix[j].Confidence
	})

	a.rep.SetStage(models.StageAggregate, 100)
	a.rep.Log("aggregator: %d deduped; %d for fix, %d summary", len(deduped), len(forFix), len(summary))
	return forFix, summary
}
