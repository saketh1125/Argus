# 🛡 CodeSentinel

**Autonomous Multi-Agent Bug Detection & Auto-PR System** — Go 1.24, single binary.

CodeSentinel ingests a GitHub repository, localizes and analyzes suspicious code
with parallel LLM analyst agents, generates and **sandbox-validates** fixes, and
raises structured pull requests — with a live web dashboard the whole time.

It is built from the locked PRD (`01_PRD.md`) and System Design (`02_System_Design.md`).

---

## Key property: runs end-to-end with **zero API keys**

Every external integration sits behind a Go interface with **two adapters** — a
real one (live API/SDK) and a deterministic **mock** — selected automatically at
startup by whether the credential/service is present:

| Integration | Real adapter | Offline adapter |
|---|---|---|
| LLM (analyst/rerank/fix) | Nvidia NIM (OpenAI-compatible HTTP) | heuristic mock that scans code for planted bug patterns |
| Embeddings | Ollama → Jina → Gemini fallback chain | deterministic hash embedding |
| Vector DB | Qdrant (REST) | in-memory cosine search |
| Code sandbox | E2B | **local executor** (real `py_compile`/`node --check`/`go build`) |
| GitHub | GitHub REST API | in-memory recorder returning deterministic PR/issue URLs |

So `FORCE_MOCK=true go run .` exercises the entire pipeline offline, and dropping
in real keys flips each stage to live with no code change.

---

## Architecture

```
GitHub URL
  └─▶ Preprocessor   clone → parse (heuristic, top-50 recent files) → call graph → embed → vector index
  └─▶ Localizer      vector search + LLM re-rank → suspicious LocationTargets (full-file fallback)
  └─▶ Analyst ×N     parallel goroutines (capped, 30s each) → evidence-enforced Issues
  └─▶ Aggregator     hash dedup + severity/confidence gate + call-graph enrichment
  └─▶ Fix Generator  3 candidate rewrites (temp-varied) → programmatic unified diff → rank
  └─▶ Validator      apply patch → sandbox syntax/test check → retry once → validate/demote
  └─▶ PR Agent       branch + commit + PR (Crit/High) ; one Summary Issue (Med/Low + demoted)
       Dashboard     live SSE panel throughout (localhost:8080)
```

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

The `models`, `config`, and the `*/interfaces.go` files are the **frozen contract
layer**: every package builds against them, which is what let the implementation
packages be written in parallel without conflicts.

---

## Design decisions & deviations from the doc

The PRD/Design name specific libraries (go-github, go-diff, Qdrant Go SDK, Templ,
tree-sitter). This implementation delivers the **same behavior on the Go standard
library only** — no external modules, no CGO, no codegen toolchain:

- **NIM / Qdrant / GitHub / embeddings** → plain `net/http` against their REST/HTTP
  APIs (NIM is OpenAI-compatible; Qdrant and GitHub have full REST APIs).
- **Diffs** → a pure-Go LCS unified-diff (`tools/patch.go`), so a fix's diff is
  always generated *mechanically* from original→rewrite, never by the LLM.
- **Parsing** → a pragmatic per-language regex parser (`tools/parser.go`) instead
  of tree-sitter CGO bindings — robust to build, covers the supported languages.
- **Dashboard** → `html/template` + HTMX (CDN) + Server-Sent Events instead of
  Templ — single binary, no generate step.

Why: zero dependency churn, an always-green build, and a true single binary. The
interfaces are unchanged, so the named SDKs can be swapped back in later behind
the same contracts. The rate limiter, evidence enforcement, severity/confidence
gating, and validation semantics follow the design exactly.

---

## Running

Prereqs: Go 1.24. Optional for *live* local embeddings/vector search: Docker
(Qdrant) and Ollama (`nomic-embed-text`).

```bash
# Fully offline deterministic demo against the bundled buggy sample repo:
FORCE_MOCK=true go run . --repo ./testdata/sample-repo

# Use live local embeddings + Qdrant (start them first), mock LLM/GitHub:
docker run -d -p 6333:6333 qdrant/qdrant
ollama serve & ollama pull nomic-embed-text
go run . --repo ./testdata/sample-repo

# Fully live (provide keys in .env — see .env.example):
go run . --repo https://github.com/owner/repo
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
