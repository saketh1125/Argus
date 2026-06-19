package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/saketh1125/argus/config"
	"github.com/saketh1125/argus/models"
	"github.com/saketh1125/argus/services"
	"github.com/saketh1125/argus/tools"
)

// sigCollection is the Qdrant collection name holding embedded signatures.
const sigCollection = "signatures"

// Preprocessor transforms a raw GitHub repository into a structured, searchable
// knowledge base: it shallow-clones the repo, parses the top-N recently-modified
// supported files into a RepoIndex, builds the call graph, and embeds every
// signature into the "signatures" vector collection. Embedding/vector errors are
// non-fatal (the localizer has an offline fallback); only a clone failure is
// fatal.
type Preprocessor struct {
	gh     tools.GitHubClient
	parser tools.CodeParser
	embed  services.EmbeddingClient
	vstore services.VectorStore
	cfg    *config.Config
	rep    models.Reporter
}

// NewPreprocessor wires the preprocessor with its GitHub, parser, embedding, and
// vector-store dependencies.
func NewPreprocessor(gh tools.GitHubClient, parser tools.CodeParser, embed services.EmbeddingClient, vstore services.VectorStore, cfg *config.Config, rep models.Reporter) *Preprocessor {
	return &Preprocessor{gh: gh, parser: parser, embed: embed, vstore: vstore, cfg: cfg, rep: rep}
}

// Run clones repoURL into a fresh temp directory, indexes its source files, and
// returns the populated index along with the workdir and parsed owner/repo. The
// caller is responsible for cleaning up workdir.
func (p *Preprocessor) Run(ctx context.Context, repoURL string) (idx *models.RepoIndex, workdir, owner, repo string, err error) {
	p.rep.SetStage(models.StageIngest, 0)
	p.rep.SetAgentStatus("Preprocessor", "cloning "+repoURL)

	owner, repo, perr := p.gh.ParseURL(repoURL)
	if perr != nil || owner == "" || repo == "" {
		owner, repo = "demo-owner", "demo-repo"
	}

	workdir, err = os.MkdirTemp("", "codesentinel-*")
	if err != nil {
		return nil, "", owner, repo, fmt.Errorf("preprocessor: create workdir: %w", err)
	}

	if _, err = p.gh.Clone(ctx, repoURL, workdir); err != nil {
		return nil, workdir, owner, repo, fmt.Errorf("preprocessor: clone %q: %w", repoURL, err)
	}
	p.rep.SetStage(models.StageIngest, 20)

	// Walk the tree and collect supported files with their mod times.
	type cand struct {
		abs, rel string
		mod      int64
	}
	var cands []cand
	_ = filepath.WalkDir(workdir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil // skip unreadable entries, keep walking
		}
		if d.IsDir() {
			name := d.Name()
			if path != workdir && (name == ".git" || name == "node_modules" || name == "vendor" || name == ".venv") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if !p.parser.Supports(ext) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(workdir, path)
		if rerr != nil {
			rel = path
		}
		cands = append(cands, cand{abs: path, rel: rel, mod: info.ModTime().UnixNano()})
		return nil
	})

	// Sort by modification time descending, take the top MaxFiles.
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod > cands[j].mod })
	maxFiles := p.cfg.MaxFiles
	if maxFiles > 0 && len(cands) > maxFiles {
		cands = cands[:maxFiles]
	}

	idx = models.NewRepoIndex()
	total := len(cands)
	for i, c := range cands {
		p.rep.SetFilesProgress(i, total)
		data, rerr := os.ReadFile(c.abs)
		if rerr != nil {
			p.rep.Log("preprocessor: read %s: %v", c.rel, rerr)
			continue
		}
		sanitized := services.SanitizeForLLM(string(data))
		pf, perr := p.parser.Parse(c.rel, sanitized)
		if perr != nil || pf == nil {
			p.rep.Log("preprocessor: parse %s: %v", c.rel, perr)
			continue
		}
		pf.ModTime = c.mod
		idx.AddFile(pf)
	}
	p.rep.SetFilesProgress(total, total)
	idx.BuildCallGraph()
	p.rep.SetStage(models.StageIngest, 70)

	// Embed signatures into the vector collection. All errors here are tolerated.
	p.embedSignatures(ctx, idx)

	p.rep.SetStage(models.StageIngest, 100)
	p.rep.Log("preprocessor: indexed %d files, %d signatures", idx.FileCount(), len(idx.Signatures()))
	return idx, workdir, owner, repo, nil
}

// embedSignatures embeds every signature's "{file} {name} {signature}" text into
// the "signatures" collection. Any embedding/vector-store error is logged and
// ignored — localization falls back to direct index scanning when search is
// empty.
func (p *Preprocessor) embedSignatures(ctx context.Context, idx *models.RepoIndex) {
	sigs := idx.Signatures()
	if len(sigs) == 0 {
		return
	}
	if err := p.vstore.EnsureCollection(ctx, sigCollection, p.embed.Dim()); err != nil {
		p.rep.Log("preprocessor: ensure collection: %v", err)
		return
	}

	texts := make([]string, len(sigs))
	for i, s := range sigs {
		sigText := s.Text
		if sigText == "" {
			sigText = s.Name
		}
		texts[i] = s.File + " " + s.Name + " " + sigText
	}

	vecs, err := p.embed.Embed(ctx, texts)
	if err != nil {
		p.rep.Log("preprocessor: embed signatures: %v", err)
		return
	}
	if len(vecs) != len(sigs) {
		p.rep.Log("preprocessor: embed returned %d vectors for %d signatures", len(vecs), len(sigs))
		return
	}

	points := make([]models.VectorPoint, 0, len(sigs))
	for i, s := range sigs {
		points = append(points, models.VectorPoint{
			ID:     agtIssueID(s.File + ":" + s.Name + ":" + fmt.Sprint(s.LineStart)),
			Vector: vecs[i],
			Payload: map[string]any{
				"file":       s.File,
				"name":       s.Name,
				"line_start": s.LineStart,
				"line_end":   s.LineEnd,
				"kind":       s.Kind,
			},
		})
	}
	if err := p.vstore.Upsert(ctx, sigCollection, points); err != nil {
		p.rep.Log("preprocessor: upsert signatures: %v", err)
	}
}
