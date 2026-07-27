# CodeSentinel — Incomplete-Parts Report

> **Status (this PR): all items below are now RESOLVED.** This file is kept as the
> audit trail of what was found and fixed. See the "Resolution" notes appended to
> each item and the new/changed tests.

Scope: anything that would stop the project from being **"ready and capable the
moment API keys are dropped into `.env`,"** measured against the three locked
documents in this repo (`01_PRD.md`, `02_System_Design.md`, `03_API_Contract.md`).

The README's promise is: *"blank key → mock adapter; real key → real adapter,
same code path."* So a bug that the **mock** adapters hide but the **real**
adapters expose is a real blocker for "ready when keys are entered."

Findings are grouped by severity. Each item lists **where**, **what the doc
says**, **what the code does**, **impact**, and **fix**.

---

## 🔴 CRITICAL — blocks a live run once keys are entered

### 1. The PR Agent overwrites the *entire target file* with one rewritten function

- **Where:** `agents/pr_agent.go` (`raisePR`, ~lines 135–145)
- **Code:**
  ```go
  content := ""
  if iss.Fix != nil && iss.Fix.Selected != nil {
      content = iss.Fix.Selected.RewrittenFunction   // only the function body
  }
  ...
  p.gh.CommitFile(ctx, owner, repo, tools.CommitSpec{
      Path: iss.File, Content: content, ...           // GitHub PUT /contents replaces the WHOLE file
  })
  ```
- **Problem:** GitHub's Contents API `PUT /repos/.../contents/{path}` **replaces
  the whole file** with the bytes you send. The PR agent sends only the rewritten
  function, so a live PR would **delete every other function, import, and line**
  in the file and leave a single function behind. The code comment even admits it:
  *"PRAgent has no workdir, so it commits the best available content: the selected
  rewritten function."*
- **Why mock mode hides it:** `mockGitHub.CommitFile` just records the action and
  returns `nil`, so the offline demo looks perfect. The first real run with a
  `GITHUB_TOKEN` would produce destructive PRs.
- **The fix already exists, it's just thrown away:** the Validator computes the
  correct full patched file via `tools.ApplyFunctionRewrite` (`agents/validator.go`
  `patchedContent`, line ~82), runs the syntax check against it, then **discards**
  it. There is no field on `models.Issue` / `models.Fix` / `FixCandidate` to carry
  the validated full-file content forward.
- **Fix:** add a field (e.g. `models.Issue.PatchedContent` or
  `Fix.Selected.PatchedFile`), set it in `Validator.validate` when a candidate
  passes, and have `raisePR` commit **that** instead of `RewrittenFunction`.

### 2. The E2B sandbox command endpoint does not match the API Contract

- **Where:** `services/sandbox.go` line 244 — `POST /sandboxes/{sandboxID}/process`
- **Doc says:** `03_API_Contract.md` §5.4 mandates
  `POST /sandboxes/{id}/commands` with response fields `exit_code`, `stdout`,
  `stderr`, `timed_out`.
- **Problem:** With an `E2B_API_KEY` entered, every validation call posts to
  `/process` instead of `/commands`, so the live sandbox path will fail (the mock
  `localSandbox` never hits this code, so the offline demo is unaffected).
- **Fix:** align the path and JSON field names to §5.4 (the README states *"All
  contracts are binding,"* so the implementation should follow the doc; if the
  real E2B REST API genuinely differs, update §5.4 to match and keep them in
  sync).

### 3. `control_panel.py` "Start CodeSentinel" spawns a broken process

- **Where:** `control_panel.py`, `_start_service("codesentinel")` — the first
  `try` block:
  ```python
  proc = subprocess.Popen(
      [sys.executable, "-m", "go"],   # "python -m go" — does not exist
      ...
      start_new_session=True)
  except Exception:
      pass                            # silently swallowed
  ```
- **Problem:** `python -m go` is not a thing; this always fails, the exception is
  swallowed, and a phantom failing child process is left behind before the real
  `go run .` block runs below it. This is leftover/broken scaffolding.
- **Fix:** delete the dead `try/except` block entirely; keep only the `go run .
  --repo … --web` path that follows it.

---

## 🟠 CONTRACT DEVIATIONS — does not follow the documents

### 4. Embedding fallback chain stops one step short (FR-08 / System Design §8)

- **Doc says:** `Ollama → Jina → Gemini → all-MiniLM-L6-v2 on CPU`
  (sentence-transformers Python subprocess).
- **Code does:** `Ollama → Jina → Gemini → mock` (`services/factory.go`
  `selectEmbed`, `services/embedding.go`). MiniLM is never reached; with no
  Ollama/Jina/Gemini, embeddings silently run as the deterministic mock.
- `.env.example` half-acknowledges this: *"all blank => local Ollama, then
  MiniLM/mock."*
- **Fix:** implement the MiniLM-on-CPU adapter, or update the docs to say the
  4th tier is the mock.

### 5. Qdrant collection name doesn't match the contract

- **Doc says:** `03_API_Contract.md` §5.5 — collection name
  `codesentinel_signatures`.
- **Code does:** `agents/preprocessor.go:17` and `agents/localizer.go` use the
  literal `"signatures"`.
- **Impact:** functionally harmless (writer and reader agree), but it deviates
  from the locked contract.

### 6. Localizer returns *all* reranked targets, not "top-3"

- **Doc says:** System Design §4.2 — Stage 2 returns the **"top-3 ranked
  locations."**
- **Code does:** `agents/localizer.go` `stage2` builds a `LocationTarget` for
  **every** ranking (capped only at `maxLocalizeCandidates = 30` on the input
  side). This spawns more analyst goroutines than the design's 3.
- **Fix:** cap the emitted targets at 3 (configurable), or update the doc.

### 7. PR body & Summary templates deviate from §11 / §12

- **PR body (`pr_agent.prBody`):** omits the **"Call Graph Context"** section
  (Callers / Callees) required by API Contract §11.
- **Summary issue (`pr_agent.summaryBody`):** uses a single combined table with a
  Status column, whereas §12 specifies **two** tables — *Findings* and
  *Unvalidated High/Critical (sandbox failed)*.
- **Fix:** add the missing sections/tables to match §11/§12.

### 8. NIM HTTP client has no per-request timeout

- **Where:** `services/nim_client.go:42` — `http: &http.Client{}` (no `Timeout`).
- **Problem:** only context-bound calls get a deadline (the analyst's 90s
  context). The localizer rerank, fixgen, and testgen calls reuse the **root**
  context (cancellable on Ctrl-C, but otherwise unbounded). A hung connection on
  those paths stalls the stage.
- **Fix:** set a default `Timeout` on the client (e.g. 60s) or pass a deadline
  into every `Complete` call.

### 9. Web launcher (`--web`) wires only a subset of credentials

- **Where:** `dashboard/launcher.go` `LaunchConfig` and `main.applyLaunchConfig`.
- **Problem:** the launcher form / `LaunchConfig` carry `GITHUB_TOKEN`,
  `NIM_API_KEY`, `E2B_API_KEY`, Ollama, Qdrant — but **not** `JINA_API_KEY` /
  `GEMINI_API_KEY`. So the Jina/Gemini embedding fallbacks can only be enabled
  via `.env`, never from the GUI.
- **Fix:** add the two keys to `LaunchConfig` + the form + `applyLaunchConfig`.

---

## ⚪ NOTES (intentional / cosmetic — not blockers)

- **`QDRANT_PORT` is loaded but unused.** `services/vectorstore.go` hardcodes
  `:6333` (the REST port). `.env.example` documents this on purpose
  (*"default 6334; the REST adapter talks to 6333"*). Fine — just be aware.
- **PR body validation line is hard-coded** to *"✅ Passed E2B sandbox"* even
  when validation actually ran in the local sandbox (`local-sandbox`). Cosmetic;
  consider reporting the real `Sandbox.Name()`.

---

## ✅ What is complete and solid (so the picture is balanced)

- The **full offline/mock pipeline builds and runs end-to-end.** Every external
  integration has a real adapter **and** a deterministic mock, auto-selected by
  the factory (`services/factory.go`).
- **NIM client** retry policy matches §5.1: 401 fatal, 429 retry-once with
  `Retry-After`, 5xx exponential backoff.
- **Rate limiter** is a proper context-aware sliding window, shared across
  goroutines, pumped to the dashboard (`services/rate_limiter.go`).
- **Sanitizer, heuristic parser (10 languages), pure-Go LCS unified diff,
  GitHub REST adapter** (git clone + branch/commit/PR/issue with 5xx backoff),
  **cross-run dedup store, SSE dashboard + launcher** — all implemented and unit
  tested.
- Unit tests cover the rate limiter, sanitizer, mock analyst, parser, diff,
  aggregator gating/dedup, analyst evidence enforcement, factory mode selection,
  NIM live (skips without a key), and the dashboard reporter/SSE.

---

## Recommended order of operations to reach "keys-in → ready"

1. **Fix #1** (PR agent commits full patched file) — without this, **any live run
   with a `GITHUB_TOKEN` is destructive.** Highest priority.
2. **Fix #2** (E2B `/commands` endpoint) — required before using a real
   `E2B_API_KEY`.
3. **Fix #3** (control-panel dead `python -m go` block) — quick cleanup; the
   Start button is otherwise flaky.
4. Reconcile **#5–#9** to "follow the docs" (collection name, top-3 cap, PR/summary
   templates, NIM timeout, launcher keys).
5. **#4** (MiniLM tier) is optional if the mock 4th tier is acceptable.
