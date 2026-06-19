package agents

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/services"
	"github.com/saketh1125/argus/tools"
)

// localizeQuery is the seed text embedded for Stage-1 vector search.
const localizeQuery = "potential bug: security, logic, null, injection, off-by-one"

// maxLocalizeCandidates caps how many signatures we ever feed forward, both for
// the rerank prompt and for any fallback target list.
const maxLocalizeCandidates = 30

// Localizer narrows the indexed repo down to the most suspicious code locations.
// Stage 1 runs a vector search over the "signatures" collection; Stage 2 asks the
// LLM (Task=rerank) to score each candidate by bug likelihood. When the best
// rerank confidence is below cfg.LocalizeFallback, it emits whole-file targets
// (FullFile=true). When vector search yields nothing (offline/empty store) it
// falls back to scanning signatures directly from the index, so the demo always
// has somewhere to look.
type Localizer struct {
	llm    services.LLMClient
	embed  services.EmbeddingClient
	vstore services.VectorStore
	cfg    *config.Config
	rep    models.Reporter
}

// NewLocalizer wires the localizer with its LLM, embedding, and vector-store deps.
func NewLocalizer(llm services.LLMClient, embed services.EmbeddingClient, vstore services.VectorStore, cfg *config.Config, rep models.Reporter) *Localizer {
	return &Localizer{llm: llm, embed: embed, vstore: vstore, cfg: cfg, rep: rep}
}

// Run returns the suspicious LocationTargets to hand to the analysts. It always
// returns a non-empty list when the index contains any functions.
func (l *Localizer) Run(ctx context.Context, idx *models.RepoIndex) ([]models.LocationTarget, error) {
	l.rep.SetStage(models.StageLocalize, 0)
	l.rep.SetAgentStatus("Localizer", "vector search")

	sigs := idx.Signatures()
	if len(sigs) == 0 {
		return nil, nil
	}

	// Stage 1: vector search, with an offline fallback to direct index scan.
	candidates := l.stage1(ctx, idx, sigs)
	if len(candidates) == 0 {
		candidates = l.directCandidates(sigs)
	}
	if len(candidates) > maxLocalizeCandidates {
		candidates = candidates[:maxLocalizeCandidates]
	}
	l.rep.SetStage(models.StageLocalize, 40)

	// Stage 2: LLM rerank. On any failure we fall back to one target per
	// candidate so the analysts still get work.
	targets, ok := l.stage2(ctx, idx, candidates)
	if !ok || len(targets) == 0 {
		l.rep.Log("localizer: rerank unavailable, using all candidates as targets")
		targets = make([]models.LocationTarget, 0, len(candidates))
		for _, c := range candidates {
			targets = append(targets, l.targetFromSig(idx, c, 0.5))
		}
	}

	// Fallback to whole-file targets when every confidence is below the gate.
	best := 0.0
	for _, t := range targets {
		if t.Confidence > best {
			best = t.Confidence
		}
	}
	if best < l.cfg.LocalizeFallback {
		l.rep.Log("localizer: best confidence %.2f < fallback %.2f, sending full files", best, l.cfg.LocalizeFallback)
		targets = l.fullFileTargets(idx, candidates)
	}

	l.rep.SetStage(models.StageLocalize, 100)
	l.rep.Log("localizer: produced %d targets", len(targets))
	return targets, nil
}

// stage1 embeds the analysis query and returns matching signatures via the vector
// store. Returns nil when embedding/search is unavailable or empty.
func (l *Localizer) stage1(ctx context.Context, idx *models.RepoIndex, sigs []models.Signature) []models.Signature {
	vecs, err := l.embed.Embed(ctx, []string{localizeQuery})
	if err != nil || len(vecs) == 0 {
		l.rep.Log("localizer: embed query failed: %v", err)
		return nil
	}
	results, err := l.vstore.Search(ctx, sigCollection, vecs[0], 10)
	if err != nil || len(results) == 0 {
		l.rep.Log("localizer: vector search empty: %v", err)
		return nil
	}

	// Map search hits back to signatures by (file, name).
	byKey := make(map[string]models.Signature, len(sigs))
	for _, s := range sigs {
		byKey[s.File+"\x00"+s.Name] = s
	}
	var out []models.Signature
	for _, r := range results {
		file, _ := r.Payload["file"].(string)
		name, _ := r.Payload["name"].(string)
		if s, ok := byKey[file+"\x00"+name]; ok {
			out = append(out, s)
		}
	}
	return out
}

// directCandidates returns signatures straight from the index, preferring
// callable constructs (functions/methods) over classes, capped sensibly.
func (l *Localizer) directCandidates(sigs []models.Signature) []models.Signature {
	var funcs, others []models.Signature
	for _, s := range sigs {
		if s.Kind == "function" || s.Kind == "method" {
			funcs = append(funcs, s)
		} else {
			others = append(others, s)
		}
	}
	out := append(funcs, others...)
	if len(out) > maxLocalizeCandidates {
		out = out[:maxLocalizeCandidates]
	}
	return out
}

// stage2 builds a rerank prompt over candidates, calls the LLM (Task=rerank), and
// constructs LocationTargets from the ranked results. The second return value is
// false when the rerank call or parse fails.
func (l *Localizer) stage2(ctx context.Context, idx *models.RepoIndex, candidates []models.Signature) ([]models.LocationTarget, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	l.rep.SetAgentStatus("Localizer", "LLM rerank")

	var prompt strings.Builder
	prompt.WriteString("Candidate code locations (index, name, signature, file):\n")
	for i, c := range candidates {
		sig := c.Text
		if sig == "" {
			sig = c.Name
		}
		fmt.Fprintf(&prompt, "[%d] %s — %s (%s)\n", i, c.Name, strings.TrimSpace(sig), c.File)
	}
	prompt.WriteString("\nScore each candidate by how likely it contains a bug.")

	req := services.CompletionRequest{
		System:    "You are a code-localization reranker. Given candidate functions, return ONLY JSON matching {\"rankings\":[{\"index\":int,\"confidence\":0..1,\"reason\":string}]}. Higher confidence means more likely to contain a bug.",
		User:      prompt.String(),
		Task:      models.TaskRerank,
		JSONMode:  true,
		MaxTokens: 1024,
	}
	resp, err := l.llm.Complete(ctx, req)
	if err != nil || resp == nil {
		l.rep.Log("localizer: rerank call failed: %v", err)
		return nil, false
	}

	var rr models.RerankResult
	if err := agtParseJSON(resp.Text, &rr); err != nil || len(rr.Rankings) == 0 {
		l.rep.Log("localizer: rerank parse failed: %v", err)
		return nil, false
	}

	// Sort rankings by confidence descending and build targets.
	sort.SliceStable(rr.Rankings, func(i, j int) bool {
		return rr.Rankings[i].Confidence > rr.Rankings[j].Confidence
	})
	var targets []models.LocationTarget
	for _, item := range rr.Rankings {
		if item.Index < 0 || item.Index >= len(candidates) {
			continue
		}
		targets = append(targets, l.targetFromSig(idx, candidates[item.Index], agtClamp(item.Confidence, 0, 1)))
	}
	return targets, true
}

// targetFromSig builds a LocationTarget from a signature, populating Code from the
// file content via tools.GetCodeChunk.
func (l *Localizer) targetFromSig(idx *models.RepoIndex, s models.Signature, conf float64) models.LocationTarget {
	code := s.Body
	if code == "" {
		if f, ok := idx.GetFile(s.File); ok {
			code = tools.GetCodeChunk(f.Content, s.LineStart, s.LineEnd)
		}
	}
	return models.LocationTarget{
		File:         s.File,
		FunctionName: s.Name,
		LineStart:    s.LineStart,
		LineEnd:      s.LineEnd,
		Confidence:   conf,
		Signature:    s.Text,
		Code:         code,
	}
}

// fullFileTargets collapses the candidate set to one whole-file target per
// distinct file, used when per-function confidence is uniformly low.
func (l *Localizer) fullFileTargets(idx *models.RepoIndex, candidates []models.Signature) []models.LocationTarget {
	seen := map[string]bool{}
	var out []models.LocationTarget
	for _, c := range candidates {
		if seen[c.File] {
			continue
		}
		seen[c.File] = true
		content := ""
		if f, ok := idx.GetFile(c.File); ok {
			content = f.Content
		}
		out = append(out, models.LocationTarget{
			File:       c.File,
			LineStart:  1,
			Confidence: l.cfg.LocalizeFallback,
			Code:       content,
			FullFile:   true,
		})
	}
	return out
}
