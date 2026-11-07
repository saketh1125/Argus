# 🛡 CodeSentinel

**Autonomous Multi-Agent Bug Analysis & Fix Proposal System** — Go 1.24.

CodeSentinel analyzes repository source, produces evidence-backed candidate fixes,
and reports validation and publication status in a local dashboard. Validation is
available only through a configured isolated provider; the current build keeps
validation and live GitHub writes disabled and fails closed.

It is built from the locked PRD (`01_PRD.md`) and System Design (`02_System_Design.md`).

---

## Safe offline analysis mode

Adapters are selected from configuration. Missing model/GitHub credentials
select deterministic mocks, while the unverified sandbox is reported as
unavailable rather than replaced with a host executor:

| Integration | Real adapter | Offline adapter |
|---|---|---|
| LLM (analyst/rerank/fix) | Nvidia NIM (OpenAI-compatible HTTP) | heuristic mock that scans code for planted bug patterns |
| Embeddings | Ollama → Jina → Gemini fallback chain | deterministic hash embedding |
| Vector DB | Qdrant (REST) | in-memory cosine search |
| Code sandbox | Disabled pending provider verification | **unavailable**; no repository command runs |
| GitHub | Live repository pipeline is currently gated; production writes are refused before HTTP | in-memory recorder with synthetic PR/issue URLs |

`FORCE_MOCK=true go run . --repo ./testdata/sample-repo --no-dashboard` exercises
credential-free analysis. Since isolated validation is unavailable, candidate
fixes are demoted and are not eligible for publication. Synthetic URLs from the
mock GitHub adapter do not represent real GitHub writes. Adding credentials does
not enable sandbox execution or publication.

---

## Architecture

```
GitHub URL
  └─▶ Preprocessor   clone → parse (heuristic; 50 recent supported files by default, each ≤1 MiB) → call graph → embed → vector index
  └─▶ Localizer      vector search + LLM re-rank → suspicious LocationTargets (full-file fallback)
  └─▶ Analyst ×N     parallel goroutines (capped, 30s each) → evidence-enforced Issues
  └─▶ Aggregator     hash dedup + severity/confidence gate + call-graph enrichment
  └─▶ Fix Generator  3 candidate rewrites (temp-varied) → programmatic unified diff → rank
  └─▶ Validator      apply patch → isolated validation or explicit unavailable → demote if unavailable
  └─▶ PR Agent       mock records only; live GitHub writes are refused
       Dashboard     local SSE panel (localhost:8080)
```

Preprocessing excludes symlinks and non-regular files and skips supported source
files larger than 1 MiB before parsing. Git clone has a two-minute timeout, but
the transfer itself does not yet have a byte quota.

### Package layout
```
models/      shared types, RepoIndex (call graph), LLM I/O schemas, Reporter      (foundation, frozen)
config/      env/.env loading                                                      (foundation, frozen)
services/    interfaces + real/mock adapters: rate limiter, sanitizer, NIM,        (frozen interfaces)
             embeddings, Qdrant, sandbox + factory
tools/       interfaces + GitHub (real/mock), heuristic parser, navigation, diff   (frozen interfaces)
agents/      the 7 pipeline agents
dashboard/   html/template + HTMX + SSE live dashboard
main.go      pipeline orchestration
testdata/    sample-repo with intentionally planted bugs for the offline demo
```

The `models`, `config`, and the `*/interfaces.go` files define the shared
contracts between the pipeline and its adapters.

---

## Design decisions & deviations from the doc

The PRD/Design name specific libraries (go-github, go-diff, Qdrant Go SDK, Templ,
tree-sitter). The current Go implementation uses the standard library for the
core pipeline and keeps unavailable safety-critical integrations disabled. It
has no CGO or code-generation requirement:

- **NIM / Qdrant / GitHub / embeddings** → plain `net/http` against their REST/HTTP
  APIs (NIM is OpenAI-compatible; Qdrant and GitHub have full REST APIs).
- **Diffs** → a pure-Go LCS unified-diff (`tools/patch.go`), so a fix's diff is
  always generated *mechanically* from original→rewrite, never by the LLM.
- **Parsing** → a pragmatic per-language regex parser (`tools/parser.go`) instead
  of tree-sitter CGO bindings — robust to build, covers the supported languages.
- **Dashboard** → `html/template` + HTMX (CDN) + Server-Sent Events instead of
  Templ — single binary, no generate step.
- **Embeddings fallback tier 4** → the design originally specified
  `all-MiniLM-L6-v2` via a Python `sentence-transformers` subprocess, which would
  break the zero-dependency, single-binary principle. The design (PRD FR-08,
  System Design §8) has been amended to specify the **deterministic pure-Go hash
  embedding** (the same offline adapter) as the final tier. The chain is therefore
  Ollama → Jina → Gemini → hash-embedding; the first three are live embedding
  adapters when configured.

Why: zero dependency churn, an always-green build, and a true single binary. The
interfaces are unchanged, so reviewed SDKs can be added behind the same
contracts. The rate limiter, evidence enforcement, and severity/confidence
gating are implemented. Full isolated repository-context validation and
approved fork-based publication remain outstanding.

---

## Running

Prereqs: Go 1.24. Optional for *live* local embeddings/vector search: Docker
(Qdrant) and Ollama (`nomic-embed-text`).

```bash
# Offline analysis against the bundled sample; unavailable validation demotes fixes:
FORCE_MOCK=true go run . --repo ./testdata/sample-repo --no-dashboard

# Optional local embedding/vector services:
docker run -d -p 6333:6333 qdrant/qdrant
ollama serve & ollama pull nomic-embed-text
go run . --repo ./testdata/sample-repo

# Live remote analysis/publication is not enabled in this Phase 0 build.
```

Flags: `--repo <url|path>`, `--mock`, `--no-dashboard`.
The live dashboard is at `http://localhost:8080` during the run.

Build a single binary:
```bash
go build -o codesentinel .
```

---

## Tests

```bash
go test ./...
```

Unit tests cover the rate limiter (sliding window), the LLM sanitizer, the mock
analyst heuristics, the heuristic parser, the unified diff, the aggregator
dedup/gate, the analyst evidence enforcement, and the dashboard reporter/SSE.
