# ARGUS — Canonical Execution Plan

> **Single source of truth for ARGUS V1 scope, implementation order, engineering constraints, acceptance criteria, and progress.**

Repository: https://github.com/saketh1125/Argus  
Product: ARGUS — Autonomous Open-Source Contributor  
Version: 1.0  
Status: V1 scope frozen; implementation not yet certified complete  
Last updated: 2026-11-01

---

## 0. Instructions to Codex and contributors

Read this document completely before changing code. It is the authoritative contract for V1.

### Operating rules

1. Follow phases in order. Do not start a later phase while an earlier phase's exit gate is failing.
2. Inspect before editing. Read the relevant implementation, tests, configuration and call sites. Treat this document's code observations as an audit baseline to verify, not unquestionable truth.
3. Prove the baseline. Run checks and record actual results. Never claim a test, build, sandbox run or deployment succeeded unless it was executed and observed.
4. Prefer small, reviewable changes. Avoid unrelated refactors, speculative abstractions, new frameworks and unnecessary agents.
5. Preserve working behaviour. Identify callers and tests before changing interfaces. Add or update tests with implementation changes.
6. Fail closed at trust boundaries. If evidence, patch integrity, sandbox isolation, validation, permissions or approval cannot be established, stop that operation and report why.
7. No scope expansion. Do not implement deferred or excluded features unless Saketh explicitly changes this document.
8. Keep this document current. At the end of each work session, update the progress tracker and relevant decision notes here. Do not create another roadmap, PRD, status document or parallel checklist.
9. Do not overwrite user work. Inspect git status and diffs before editing. Never reset, clean, force-push or discard unrelated changes.
10. A phase is complete only when every mandatory acceptance criterion is met and evidence is recorded.

### Required Codex report after each task

Report:
- Phase and task attempted.
- Files changed and why.
- Tests/checks actually run, with pass/fail and relevant output.
- Security or compatibility implications.
- Acceptance criteria satisfied and still open.
- Blockers or decisions needed.
- Whether the phase exit gate is met.

Stop at the phase boundary. Do not silently continue into the next phase.

### Authority of other repository documents

This file supersedes earlier product plans, implementation roadmaps, incomplete-parts reports and milestone notes as the source of **product scope and execution order**.

Existing files such as 01_PRD.md, 02_System_Design.md, 03_API_Contract.md, FRONTEND_CONTEXT.md, INCOMPLETE_PARTS_REPORT.md and files under plans/ may contain useful technical details. They are reference material only. If they conflict with this file, follow this file and flag the conflict. Do not create a second source of truth or blindly trust stale status claims.

Do not delete these reference files during ordinary implementation. Their eventual consolidation/removal is a separate maintenance decision. This is the only plan Codex should follow.

---

## 1. Product contract — frozen V1

### Product identity

ARGUS is an **Autonomous Open-Source Contributor**.

It accepts a curated set of public GitHub repositories, analyses a pinned revision, identifies evidence-backed defects, generates minimal fixes, validates those fixes in an isolated environment, and prepares contribution-ready pull requests from the authenticated user's fork.

The product is measured by contribution quality, not the volume of findings or issues created.

**Core promise:** find a real problem, explain it with source evidence, propose the smallest appropriate correction, verify it safely, and prepare a high-quality upstream pull request for human approval.

### Frozen decisions

| Decision | V1 choice |
|---|---|
| Product identity | Autonomous Open-Source Contributor |
| Repository discovery | Curated only |
| Platform | GitHub |
| Visibility | Public repositories |
| Inputs | User-supplied URL, curated allowlist, or batch of URLs |
| Analysis | Existing multi-stage / multi-agent pipeline, improved for evidence quality |
| Fixes | Minimal, deterministic, complete-file patches |
| Validation | Isolated execution in repository context |
| Contribution | Fork → branch → commit → upstream PR |
| Publishing | Explicit human approval |
| Merge | Never automatic; merge functionality excluded |
| Dashboard | Existing dashboard extended for scan and contribution lifecycle |

### In scope

- Single-repository and curated batch ingestion.
- Repository eligibility checks and pinned commit identity.
- Repository structure and source-context extraction.
- Bug localization and evidence-based multi-agent analysis.
- Finding deduplication, severity and confidence assessment.
- Minimal fix generation and deterministic patch application.
- Regression-test generation where appropriate.
- Isolated repository-context validation.
- Patch export.
- Fork creation/reuse, branch creation, commit and upstream PR creation.
- Human approval tied to the exact patch being published.
- Scan history, findings, validation evidence and PR status.
- Bounded execution, observability, security and deployment.

### Explicitly out of scope

Do not implement in V1:
- Autonomous GitHub-wide repository discovery or ranking.
- Continuous monitoring or scheduled scanning.
- Private repository support.
- Automatic PR merging or maintainer replies.
- Automatic approval of ARGUS's own findings or patches.
- Mobile application or IDE extension.
- Universal language support or claims of complete AST understanding.
- Enterprise multi-tenancy.
- Self-modifying agents.
- Autonomous issue discussion.
- Security certification or guarantees that a repository is vulnerability-free.

### Language policy

Initial priority is Go, Python and JavaScript/TypeScript, conditional on actual parser, context extraction and validation coverage. The existing parser is heuristic/regex-based; do not describe it as a full AST parser. A language is not production-supported merely because its file extension is recognised. Label weakly supported languages honestly and do not make confident automated-fix claims for them.

---

## 2. Current implementation baseline

This section records the implementation state observed during the initial repository review. It does not claim that the current test suite passes.

### Existing capabilities

The repository is a Go 1.24 application with a seven-stage pipeline:

1. Preprocessor
2. Localizer
3. Analyst
4. Aggregator
5. Fix Generator
6. Validator
7. PR Agent

Existing adapters include real/mock options for LLM inference, embeddings, vector storage, sandbox execution and GitHub operations. The project also contains a Go HTML/SSE dashboard and a Python control panel.

The CLI currently accepts one repository target. The README describes repository ingestion, suspicious-code localization, parallel analysis, candidate fix generation, sandbox validation, GitHub issue/PR actions and a live dashboard.

Existing tests cover selected agent, parser, service, GitHub, rate-limiter, sanitizer and dashboard behaviour. Their presence does not prove end-to-end correctness.

### Known gaps and risks to verify/fix

Reconfirm each against the current checkout before editing because repository state may have changed.

**P0 — Unsafe host execution**
- The reviewed factory selected a local executor when E2B was unavailable and in mock mode.
- The local executor ran commands through a host shell.
- The validator accepted an LLM-produced run command and appended it to commands.
- This is unacceptable for untrusted public repositories.

**P0 — Destructive incomplete-patch fallback**
- The reviewed validator could fall back to validating only a rewritten function and mark it validated.
- The PR agent could fall back to committing a rewritten function when a full patched file was absent.
- GitHub's file contents write replaces the entire file. A function body is not a safe substitute for a complete file.
- Existing tests reportedly enshrined this fallback and must be corrected.

**P1 — Contribution workflow assumes upstream write access**
- The reviewed PR agent created branches and commits in the scanned repository.
- This does not work for arbitrary third-party open-source projects.
- V1 must fork/reuse a fork, commit to the fork and open a PR against upstream.

**P1 — Validation lacks full repository context**
- The reviewed validator staged only the patched target file and optional generated test.
- This cannot reliably validate imports, dependencies, project configuration or integration behaviour.
- V1 requires bounded repository-context validation in a genuine isolated environment.

**P1 — No curated multi-repository workflow**
- The reviewed CLI handled one repository.
- There was no persistent curated repository manager or batch orchestration.

**Additional gaps**
- File selection based on checkout modification time may be nondeterministic; prefer Git history and deterministic prioritisation.
- Heuristic parser coverage and localisation quality need labelled evaluation.
- Add ignore-aware traversal, generated/vendor exclusions, binary detection, symlink/path traversal protections and file-size limits.
- HTTP clients need bounded timeouts, context-aware cancellation, retries and response-size limits.
- Mock/live adapter selection must be visible and impossible to confuse.
- Existing documentation may be stale or contradictory.
- CI and end-to-end fixtures must be confirmed and added if missing.
- PR text must report the actual sandbox provider and validation level. Never hardcode a claim such as “E2B passed” when another provider ran or validation was unavailable.

### Kryptos regression benchmark

ARGUS/CodeSentinel was previously run against saketh1125/Kryptos and created two summary issues containing six candidate findings. Later source inspection found several reports unsupported by surrounding implementation or tests. These are candidate false positives, not confirmed defects.

Use these as regression examples:
- Alleged incorrect argument order in ghkge/harvesters/base.py.
- Alleged undefined global in ghkge/harvesters/web.py.
- Alleged incorrect assertions in tests/test_basic.py.
- Recheck all remaining findings against the exact historical commit before labelling them true or false.

The objective is to reduce unsupported conclusions, not to hardcode Kryptos-specific suppressions.

---

## 3. Target user workflow

1. User adds one or more public GitHub repositories to the curated list.
2. ARGUS checks repository eligibility and access.
3. ARGUS pins a commit SHA and records the scan.
4. Preprocessor builds bounded repository context.
5. Localizer identifies candidate code regions.
6. Analysts investigate candidates and produce evidence-backed findings.
7. Aggregator deduplicates and classifies findings.
8. Fix Generator proposes a minimal complete-file patch.
9. Patch integrity checks verify exact application to the pinned source.
10. Validator applies the patch in a complete, isolated checkout and runs relevant checks.
11. ARGUS displays the finding, evidence, diff and validation report.
12. User explicitly approves the exact patch for publication.
13. ARGUS creates or reuses a fork, creates a branch and commits the patch.
14. ARGUS opens an upstream PR and records its URL/status.
15. Dashboard preserves the full provenance and lifecycle.

If a mandatory condition fails, ARGUS stops the contribution path and reports why. It may preserve a static-analysis report or export a patch where safe.

---

## 4. Architecture and engineering boundaries

Retain and evolve the existing Go application. Do not replace the stack without a measured reason.

### Target pipeline

Curated Repository Manager  
→ Eligibility and Commit Pinning  
→ Repository Ingestion / Preprocessor  
→ Repository Context and Retrieval  
→ Localizer  
→ Parallel Analysts  
→ Evidence Aggregator  
→ Fix Generator  
→ Patch Integrity Gate  
→ Isolated Repository Validator  
→ Human Approval Gate  
→ Fork / Branch / Commit Manager  
→ GitHub PR Publisher  
→ Dashboard and Persistent History

### Existing components to retain and improve

| Component | Direction |
|---|---|
| Go backend and CLI | Retain; add orchestration |
| Preprocessor | Improve deterministic and safe ingestion |
| Localizer | Improve repository-aware candidate selection |
| Analyst agents | Retain; strengthen evidence and counter-checking |
| Aggregator | Improve deduplication and evidence gates |
| Fix Generator | Retain; enforce minimal complete-file patches |
| Validator | Major safety and repository-context upgrade |
| PR Agent | Replace direct-upstream assumptions with fork workflow |
| GitHub adapter | Extend fork, branch, commit, PR and permission handling |
| Embedding adapter | Retain behind interfaces |
| Vector store | Retain for retrieval; not primary application persistence |
| Dashboard/SSE | Extend for scan and contribution lifecycle |
| Python control panel | Audit; avoid duplicate product control surfaces without a clear need |

### Persistence

Use a durable relational store for application state. Do not use the vector database as the source of truth for jobs, findings, approvals or contributions.

Minimum logical entities:
- **Repository:** owner, name, URL, default branch, language, license, eligibility, timestamps.
- **Scan:** repository, pinned commit SHA, status, configuration, timestamps, resource usage, errors.
- **Finding:** scan, category, severity, confidence, evidence, source path/lines, verification state.
- **Patch:** finding, original hash, resulting hash, diff, patch state.
- **Validation:** patch, sandbox provider, validation level, commands, exit codes, logs, duration and result.
- **Approval:** user, exact patch identifier/hash, decision and timestamp.
- **Contribution:** upstream, fork, branch, commit, PR URL and publishing state.

Choose the lightest reliable persistence approach compatible with the current project. Avoid adding Redis, a message broker or multiple databases unless measured queue/load requirements justify them. A database-backed job queue is acceptable initially.

### Trust boundaries

Treat all repository-controlled content as untrusted, including source files, comments, README files, build scripts, test names, issue text and embedded instructions. Repository content may inform analysis but must never override system policy or grant itself permissions.

Never expose arbitrary command execution through a public API or dashboard.

---

## 5. Mandatory global acceptance rules

These rules apply to every phase and cannot be bypassed by configuration.

**G1 — Reproducibility:** Every scan, finding, patch and validation result is traceable to a repository and exact commit SHA.

**G2 — Evidence:** A finding identifies exact source evidence. Model confidence is not proof.

**G3 — Patch integrity:** A patch applies to the exact original source. The complete resulting file is available and verified. No function-only or partial-file fallback.

**G4 — Safe execution:** Untrusted repository code and tests run only inside a genuine isolated environment with bounded resources. A temporary directory on the host is not a security sandbox.

**G5 — No LLM-controlled shell:** Do not execute arbitrary commands returned by an LLM. Construct commands from trusted project metadata using explicit allowlists and argument arrays, without shell interpolation.

**G6 — Validation honesty:** Distinguish syntax checks, targeted tests, project tests, failures and unavailable validation. Record what actually ran. Never report a stronger validation level than the evidence supports.

**G7 — No unvalidated PR:** No PR may be published unless the exact patch passed required validation policy and the user approved that exact patch.

**G8 — No upstream writes:** For third-party repositories, branch and commit in the authenticated user's fork. Upstream is the PR target, not the write target.

**G9 — Human control:** Publishing requires explicit approval. Approval is invalidated if patch or target commit changes. Never auto-merge.

**G10 — Honest adapter modes:** Mock/live model mode, sandbox provider and GitHub write capability must be visible in logs and UI. Mock output must never appear to be a real contribution.

---

## 6. Phase-by-phase execution plan

Phases are sequential. Tasks within a phase may be parallelised only when dependencies are understood and the phase gate remains protected.

# PHASE 0 — Reproducible baseline and critical safety fixes

**Priority:** Blocker  
**Goal:** Know exactly what works today and remove dangerous failure modes before expanding functionality.

### Tasks

**0.1 Establish local baseline**
- Inspect branch, working tree, remotes and latest commit.
- Record exact commit SHA.
- Inspect Go version and module dependencies.
- Run formatting checks, unit tests, vet, race tests where practical and build.
- Record failures without hiding or rewriting them.
- Reproduce mock mode separately from live provider mode.
- Confirm which adapters are actually selected at runtime.

Suggested checks (adapt to repository scripts; inspect git status before formatting):
    git status --short
    git rev-parse HEAD
    go version
    go test ./...
    go vet ./...
    go test -race ./...
    go build ./...

**0.2 Remove unsafe host execution**
- Trace factory selection, sandbox interface, local executor and validator command generation.
- Remove execution of untrusted repository commands on the host.
- Remove arbitrary LLM-produced run-command execution.
- Represent sandbox capability and validation availability explicitly.
- If no genuine isolation provider is configured, validation is unavailable; static analysis may continue.
- Ensure mock mode does not imply safe execution of untrusted code.
- Add adversarial tests for shell metacharacters, command injection, path traversal and malicious LLM command output.

**0.3 Enforce complete-file patch integrity**
- Trace original source retrieval, function rewrite, patch construction, validator and PR agent.
- Require deterministic patch application against the exact original file.
- Verify complete resulting file content and hashes.
- Remove validator fallback that marks a rewritten function as a validated full file.
- Remove PR-agent fallback from full patched file to rewritten function.
- Reject missing source, source mismatch, ambiguous replacement and empty full-file patch.
- Ensure sibling functions/imports remain intact.
- Replace tests that enshrine unsafe fallback with fail-closed tests.

**0.4 Make runtime mode explicit**
- Expose selected LLM, embedding, vector-store, sandbox and GitHub adapters.
- Fail clearly on invalid live configuration.
- Ensure mock results are visibly labelled and cannot be published as real contributions.

**0.5 Establish CI**
- Add or repair CI for formatting, tests, vet and build.
- Add race tests where stable and useful.
- Do not claim cross-platform support until the build matrix actually passes.

### Acceptance criteria

- [ ] Exact baseline commit and environment recorded.
- [ ] Existing test/build outcomes documented accurately.
- [ ] No arbitrary LLM-supplied command can execute on the host.
- [ ] No untrusted repository code is executed outside genuine isolation.
- [ ] Missing sandbox is reported as validation unavailable.
- [ ] Validator cannot mark a partial/function-only rewrite as a valid full-file patch.
- [ ] PR agent refuses missing, empty, stale or incomplete full-file patches.
- [ ] Regression tests cover source mismatch and preservation of unrelated code.
- [ ] Mock/live adapter state is explicit.
- [ ] CI runs required checks successfully.

### Exit gate

**Do not start Phase 1 until all safety criteria pass.** If the external sandbox provider currently fails (the earlier Kryptos run reported an E2B file-upload 404), diagnose and fix the integration or choose another genuine isolated provider. Do not bypass the gate by falling back to host execution.

---

# PHASE 1 — Reliable single-repository analysis

**Goal:** Turn the prototype into a repeatable, evidence-based scanner before adding batch workflows.

### Tasks

**1.1 Harden repository ingestion**
- Pin a commit before analysis.
- Make file ordering deterministic; do not rely on clone filesystem modification times.
- Respect ignore rules.
- Exclude generated, vendor, build, binary and oversized files.
- Protect against symlink escape, path traversal and malicious archive paths.
- Add bounded clone, file-count, file-size and total-context budgets.
- Record skipped files and reasons.

**1.2 Improve repository context**
- Identify language, manifests, project layout, test directories and CI configuration.
- Extract available function/module relationships.
- Preserve file path, line range, commit and source hash through all stages.
- Keep retrieval bounded and relevant.
- Narrow language claims to tested coverage.

**1.3 Improve localisation**
- Replace generic-only seed queries with repository-aware signals and explicit defect categories.
- Make localisation outputs explain why a file/function was selected.
- Track evidence provenance.
- Evaluate candidate selection on labelled fixtures.

**1.4 Improve analysis quality**
- Require analysts to state the alleged failure mechanism.
- Require exact source evidence and relevant context.
- Ask for counter-evidence or plausible intended behaviour.
- Separate confirmed, likely, speculative and rejected candidates.
- Deduplicate semantically identical reports.
- Do not treat multiple agents agreeing on the same unsupported assumption as independent proof.

**1.5 Calibrate severity and confidence**
- Define severity based on impact and reach, not dramatic language.
- Define confidence based on evidence strength and reproducibility.
- Store them separately.
- Establish a conservative eligibility threshold for fix generation and PR consideration.
- Do not tune thresholds merely to increase finding counts.

**1.6 Add structured scan output**
- Persist a machine-readable scan report.
- Include commit SHA, adapter modes, findings, evidence, rejected candidates, errors, timings and resource use.
- Keep unverified findings separate from actionable findings.

### Acceptance criteria

- [ ] Same repository commit and configuration produce deterministic ingestion metadata.
- [ ] Files excluded by policy are not analysed.
- [ ] Every finding resolves to exact source lines in the pinned commit.
- [ ] Findings contain a clear failure mechanism and supporting evidence.
- [ ] Unsupported/speculative candidates are not promoted to confirmed defects.
- [ ] Duplicate findings are consolidated.
- [ ] Kryptos historical examples are regression cases reviewed against exact source revisions.
- [ ] Scan report distinguishes live and mock modes.
- [ ] Resource budgets and cancellation are enforced.
- [ ] A labelled fixture corpus demonstrates repeatable detection behaviour.

### Exit gate

A single repository can be scanned repeatedly with reproducible, inspectable results. False-positive regression cases are not repeated as confident defects. A dashboard displaying findings is not sufficient evidence.

---

# PHASE 2 — Patch generation and repository-context validation

**Goal:** Ensure a generated fix is minimal, applicable and verified in actual project context.

### Tasks

**2.1 Define patch representation**
- Prefer unified diffs or another deterministic patch representation tied to the original blob/hash.
- Preserve verified full-file reconstruction where required by the GitHub write path.
- Store original and resulting hashes.
- Reject stale patches if the base commit or source file changed.

**2.2 Improve fix generation**
- Give the model relevant implementation, callers, tests and project conventions.
- Ask for the smallest change that addresses the demonstrated defect.
- Avoid unrelated formatting, refactoring and dependency changes.
- Generate a regression test only when it expresses the defect meaningfully.
- The model cannot claim a test passed; only the validator records execution results.

**2.3 Implement deterministic patch application**
- Apply patches to a clean checkout at the recorded commit.
- Fail on ambiguous hunks or mismatched source.
- Inspect the final diff.
- Verify unrelated file content remains unchanged.
- Reject unexpected file additions/deletions or broad changes unless justified and separately approved.

**2.4 Implement repository-context sandboxing**
- Transfer a bounded, sanitised checkout or immutable archive into the isolated provider.
- Verify the checkout commit inside the sandbox.
- Apply the patch there.
- Do not execute repository scripts on the host.
- Detect test/build commands from trusted project metadata and explicit allowlists.
- Construct argv directly; never concatenate untrusted strings into shell commands.
- Enforce CPU, memory, disk, process, network and time limits supported by the provider.
- Restrict or disable network access by default.
- Capture bounded logs and exit codes.
- Clean up sandbox resources.

**2.5 Validation stages**
Run checks in increasing cost:
1. Patch application and integrity.
2. Formatting/static checks where safe.
3. Syntax or compile check.
4. Targeted regression tests.
5. Relevant package tests.
6. Full project tests only when budget and project configuration justify it.

Report each stage independently. A syntax check is not a passing test suite.

**2.6 Failure and retry policy**
- Permit only bounded, explainable retries.
- If a test fails because the patch is incorrect, reject or revise the candidate.
- If the environment is broken or unavailable, mark validation unavailable/blocked.
- Never relabel infrastructure failure as successful patch validation.
- Record all attempts.

### Acceptance criteria

- [ ] Patch applies only to the exact pinned source.
- [ ] Final diff is minimal and inspectable.
- [ ] Unrelated source remains unchanged.
- [ ] Generated regression test fails before the fix and passes after it, where applicable.
- [ ] Validation uses a complete bounded repository checkout.
- [ ] All untrusted execution occurs inside a genuine isolated provider.
- [ ] Commands are allowlisted and passed as argument arrays, not shell strings.
- [ ] Resource limits and timeouts are enforced.
- [ ] Logs, exit codes, provider, commit and duration are recorded.
- [ ] Validation state reflects the checks actually run.
- [ ] Failed, unavailable or incomplete validation blocks PR eligibility.

### Exit gate

At least one seeded-bug fixture per supported language can be fixed and validated end to end in repository context. No patch with uncertain integrity or unavailable required validation can become PR-eligible.

---

# PHASE 3 — Curated repository management and orchestration

**Goal:** Move from one-off CLI scans to a manageable curated portfolio without autonomous discovery.

### Tasks

**3.1 Repository registry**
- Add repositories by GitHub URL.
- Normalise owner/name and canonical URL.
- Prevent duplicates.
- Allow enable/disable and removal from future scans.
- Do not delete historical scan records when a repository is removed from the active list.

**3.2 Eligibility checks**
- Confirm public repository access.
- Detect archived status.
- Detect default branch, license metadata, primary languages and repository size.
- Apply configurable language, size and licence policy.
- Treat unknown licence as ineligible for automated contribution until reviewed.
- Record the reason for every accepted/rejected repository.

**3.3 Batch scan orchestration**
- Create persistent scan jobs.
- Track queued, running, completed, failed and cancelled states.
- Add per-repository and global budgets.
- Start with low concurrency (default 2), configurable.
- Support cancellation and bounded retry/backoff.
- Avoid rescanning the same repo+commit+configuration unnecessarily.
- Ensure one repository failure does not terminate the batch.

**3.4 CLI and API**
- Preserve the existing single-repository CLI.
- Add curated registry and batch commands/endpoints only after the service layer is tested.
- Keep HTTP handlers thin; business logic belongs in services.
- Add authentication before exposing write-capable endpoints publicly.

### Acceptance criteria

- [ ] User can add, list, disable and remove curated repositories.
- [ ] Duplicate URLs normalise to one repository record.
- [ ] Eligibility and rejection reasons are persisted.
- [ ] User can start a batch scan from selected repositories.
- [ ] Jobs survive application restart.
- [ ] Concurrency, budgets, cancellation and retry limits work.
- [ ] Duplicate repo+commit scans are handled idempotently.
- [ ] A failed job does not cancel unrelated jobs.
- [ ] Existing single-repository workflow remains usable.

### Exit gate

A small curated set can be scanned reliably with persistent job state, transparent eligibility decisions and bounded resource usage.

---

# PHASE 4 — Fork-based contribution workflow and approval

**Goal:** Prepare real contributions to third-party repositories safely.

### Tasks

**4.1 GitHub permission model**
- Use least-privilege GitHub permissions.
- Distinguish read-only scanning from contribution/write permission.
- Check authenticated identity and required permissions before publishing.
- Never log tokens or sensitive headers.
- Handle rate limits and permission errors explicitly.

**4.2 Fork manager**
- Resolve the authenticated user.
- Check whether a fork already exists.
- Create a fork when necessary.
- Wait/poll for fork availability with bounded retries.
- Verify fork parent/upstream identity.
- Never assume a fork is ready immediately after creation.

**4.3 Branch and commit manager**
- Use deterministic, collision-resistant branch naming.
- Create branches in the fork from the correct upstream base commit.
- Verify the fork branch base before committing.
- Commit only approved patch files.
- Handle existing branch collisions without overwriting unrelated work.
- Never push directly to upstream.

**4.4 PR publisher**
- Open PR against upstream.
- Set head to fork owner and branch.
- Set base to the correct upstream default/selected branch.
- Detect an existing equivalent open PR before creating another.
- Include a concise PR description containing defect summary, evidence, change, tests, exact validation level/results, sandbox provider, commit provenance and limitations.
- Never claim checks passed if they did not run.

**4.5 Human approval**
- Present exact diff, finding, target repo, base commit and validation report.
- Require explicit user approval.
- Bind approval to a hash of the exact patch plus repository and base commit.
- Invalidate approval if any of those values change.
- Publishing must be separate from scanning/analysis.
- No auto-merge endpoint or code path.

**4.6 Patch export fallback**
- If fork creation or publishing is unavailable, provide the complete patch and instructions.
- Clearly label it as not published.
- Do not attempt a direct upstream write as fallback.

### Acceptance criteria

- [ ] Fork is created or safely reused.
- [ ] Fork parent is verified against intended upstream.
- [ ] Branch is created in fork from correct base.
- [ ] Commit contains only approved patch changes.
- [ ] PR targets upstream with correct fork head and base.
- [ ] Duplicate PRs are detected.
- [ ] Permission/rate-limit errors are recoverable and visible.
- [ ] Exact patch approval is required.
- [ ] Changed patch or base commit invalidates approval.
- [ ] Unvalidated patch cannot be published.
- [ ] No code path writes to unrelated upstream.
- [ ] Patch export works when publishing cannot proceed.
- [ ] No automatic merge capability exists.

### Exit gate

Complete an end-to-end contribution against a safe test upstream repository using a fork. Verify the actual branch, commit, PR head/base, diff, approval record and validation evidence.

---

# PHASE 5 — Dashboard, persistence and product experience

**Goal:** Make the entire process usable and understandable without hiding uncertainty.

### Tasks

**5.1 Dashboard views**
- Overview: repositories, scans, findings, candidate patches, validated patches and PRs.
- Repository list: eligibility, last scan, latest commit and status.
- Repository detail: scan history and findings.
- Finding detail: evidence, source location, severity, confidence and explanation.
- Patch review: actual diff, file list, integrity state and validation report.
- Approval: exact target, patch hash, base commit and explicit publish action.
- Contribution history: fork, branch, commit, PR URL and status.

**5.2 Live progress**
- Retain SSE if suitable.
- Ensure reconnects recover current persisted state.
- Browser connection must not be the source of truth for job execution.
- Show stage progress and actionable errors.

**5.3 Persistent history**
- Persist scans, findings, patch versions, validation attempts, approvals and contributions.
- Make operations idempotent.
- Preserve provenance when repositories are removed from the active curated list.

**5.4 User-facing clarity**
- Distinguish candidate from confirmed findings.
- Distinguish generated from validated patches.
- Distinguish validation unavailable from validation failed.
- Distinguish approved from published.
- Distinguish mock from live execution.
- Avoid vanity metrics that reward high false-positive counts.

### Acceptance criteria

- [ ] User can follow a scan from queued to final state.
- [ ] User can inspect source evidence and the complete diff.
- [ ] Validation detail shows provider, commands, exit codes and result.
- [ ] Approval is explicit and patch-specific.
- [ ] PR status and URL are visible.
- [ ] Dashboard recovers state after refresh/reconnect.
- [ ] Errors are understandable and actionable.
- [ ] Mock/live and validation states cannot be confused.

### Exit gate

A user can operate the complete curated scan-to-approved-PR flow through the UI without manual database edits or hidden CLI state.

---

# PHASE 6 — Deployment, security review and V1 release

**Goal:** Deploy a bounded, maintainable service only after the complete workflow is safe locally.

### Tasks

**6.1 Deployment architecture**
- Separate public API/dashboard from background scan workers where needed.
- Containerise with a non-root runtime.
- Add health/readiness checks.
- Add graceful shutdown and job recovery.
- Set explicit CPU, memory, disk and concurrency limits.
- Use durable persistent storage and backups.
- Validate target architecture compatibility before choosing hosting hardware.
- Do not assume a cloud instance is suitable without load/resource tests.

**6.2 Secrets and network**
- Store credentials in deployment secret management, never images or source.
- Rotate credentials if exposed.
- Enforce HTTPS.
- Restrict inbound network access.
- Authenticate dashboard and write-capable API actions.
- Apply CSRF/origin protections where applicable.
- Rate-limit expensive scan and publishing operations.
- Keep sandbox execution isolated from application credentials and internal services.

**6.3 Observability**
- Structured logs with scan/job correlation IDs.
- Metrics for queue depth, scan duration, failure rate, sandbox failures, API rate limits, model cost and resource consumption.
- Redact secrets and sensitive environment data.
- Alert on worker crashes, queue stalls and repeated provider failures.

**6.4 Backups and retention**
- Define database backup/restore.
- Define retention for cloned source, logs, generated patches and sandbox artifacts.
- Delete temporary checkouts and sandbox resources after use.
- Keep minimum evidence necessary to reproduce contribution decisions.

**6.5 Release documentation**
- Installation and configuration.
- GitHub permission setup.
- Sandbox setup and security boundary.
- Supported language limitations.
- Curated repository management.
- Scan and contribution workflow.
- Troubleshooting.
- Honest known limitations.

### Acceptance criteria

- [ ] Production deployment runs non-root.
- [ ] API/dashboard and workers have health checks.
- [ ] Jobs recover safely after restart.
- [ ] Secrets are absent from images, logs and repository.
- [ ] HTTPS and authentication are enforced.
- [ ] Scan execution is bounded and isolated.
- [ ] Persistent data backup and restore are tested.
- [ ] Logs/metrics identify failures without leaking secrets.
- [ ] End-to-end scan and contribution workflow passes in deployed environment.
- [ ] Documentation matches actual deployed behaviour.

### Exit gate

A deployed instance can ingest a curated public repository, analyse a pinned commit, generate and safely validate a patch, obtain explicit approval, publish through a fork, and preserve history. No critical security or data-integrity issue remains open.

---

# PHASE 7 — Benchmark, evaluate and public V1

**Goal:** Demonstrate useful contributions, not merely plausible-looking output.

### Tasks

**7.1 Build evaluation corpus**
- Seeded bugs with known expected behaviour.
- Real historical bug-fix commits where ground truth is available.
- Negative examples containing correct but suspicious-looking code.
- Kryptos false-positive regression cases.
- Multiple fixtures per supported language.
- Repository-context tests involving imports, dependencies and existing tests.

**7.2 Measure quality**

Track:
- Finding precision.
- Actionable finding rate.
- Patch applicability.
- Targeted test pass rate.
- Project test pass rate.
- Regression rate.
- Human/maintainer PR quality assessment.
- PR acceptance/merge rate when enough observations exist.
- Scan duration.
- Model/sandbox cost per validated contribution.
- False-positive rate.

Do not claim statistical confidence from a tiny sample. Publish sample size and evaluation method.

**7.3 Release readiness**
- Reproduce benchmark runs from recorded commits/configuration.
- Document limitations and unsupported languages.
- Provide a small public demo repository or safe fixture.
- Ensure demo distinguishes mock from live operation.
- Publish only measured claims.

### Acceptance criteria

- [ ] Evaluation corpus is versioned and reproducible.
- [ ] Negative fixtures test false-positive behaviour.
- [ ] Metrics are calculated from actual outcomes.
- [ ] Sample sizes and limitations are disclosed.
- [ ] Public demo does not misrepresent mock output as real contribution.
- [ ] Release notes and documentation match product behaviour.

### Exit gate

V1 demonstrates repeatable, evidence-backed, safely validated contributions and publishes transparent quality metrics.

---

## 7. Cross-phase testing strategy

Testing is part of each phase, not a final activity.

### Unit tests
- Patch construction and application.
- Evidence schema validation.
- Finding deduplication.
- Severity/confidence rules.
- Repository eligibility.
- GitHub branch/head/base construction.
- Approval hash binding.
- Sandbox command allowlist.
- Path and archive safety.

### Integration tests
- Repository clone/pinning.
- Full pipeline with deterministic model fixtures.
- Sandbox upload/apply/test/log collection.
- GitHub fork/branch/commit/PR lifecycle against a safe test repository.
- Persistent job recovery.
- Dashboard state recovery.

### Adversarial tests
- Malicious instructions in README/source comments.
- LLM returning shell metacharacters or destructive commands.
- Symlink escape and path traversal.
- Malformed archives and oversized files.
- Source changed between analysis and patch application.
- Patch attempts to replace an entire file with a function body.
- Fork permission denied or fork delayed.
- Branch collision.
- Approval followed by patch mutation.
- Sandbox unavailable or timed out.
- GitHub rate limit and transient API failure.

### Golden end-to-end scenarios
1. Seeded bug → evidence-backed finding → minimal patch → targeted test passes → human approval → fork PR.
2. False-positive candidate → rejected/demoted → no patch/PR.
3. Sandbox unavailable → report remains unvalidated → no PR.
4. Stale source/patch mismatch → fail closed.
5. GitHub write permission unavailable → patch export, no upstream write.
6. One failed repository in a batch → other scans continue.

---

## 8. Resource and cost policy

ARGUS must remain resource-aware and predictable.

Initial conservative defaults (tune from measurements):
- Batch scan concurrency: 2.
- Analyst parallelism: 5.
- Per-repository file count: bounded and configurable.
- Per-file size: bounded and configurable.
- Per-scan model/token budget: configurable.
- Sandbox execution timeout: bounded per stage.
- Retry count: low and explicit.
- Logs and sandbox artifacts: size-limited.

Requirements:
- Cancelled jobs stop downstream work and release resources.
- Failed provider calls do not create infinite retry loops.
- Duplicate scans reuse completed work only when commit and relevant configuration match.
- Cost and time are recorded per scan and contribution.
- Never increase concurrency simply to make a demo appear faster.

---

## 9. Definition of V1 complete

ARGUS V1 is complete only when this scenario is demonstrated:

1. User adds a public GitHub repository to the curated registry.
2. ARGUS validates eligibility and pins a commit SHA.
3. It analyses the repository with traceable source context.
4. It identifies a genuine defect with evidence and a clear failure mechanism.
5. It generates a minimal patch that preserves unrelated code.
6. The patch applies cleanly to the exact source revision.
7. It validates the patch in an isolated complete repository checkout.
8. It records actual commands, exit codes, logs and validation level.
9. The user reviews the finding and exact diff.
10. The user explicitly approves that exact patch.
11. ARGUS creates or reuses the user's fork.
12. It creates a branch and commits only the approved changes.
13. It opens a PR against the correct upstream repository.
14. The dashboard records full provenance and contribution status.
15. The workflow is reproducible from the recorded repository commit and configuration.

A running dashboard, generated issue, mock PR URL or high finding count does not satisfy this definition.

---

## 10. Progress tracker — update this section only as work is completed

Never mark a phase complete without recording test evidence, commit SHA and remaining limitations. Use dates and actual outcomes.

| Phase | Status | Evidence / commit | Blockers |
|---|---|---|---|
| P0 — Baseline and safety | IN PROGRESS | Baseline started at c189f53de2e1e6eb6a137cb4d5c5a5c441bd4b47; see P0.1 results below | Isolation, safety fixes, adapter verification and CI outstanding |
| P1 — Reliable analysis | NOT STARTED | — | Evidence quality, deterministic ingestion, Kryptos regression corpus |
| P2 — Patch and validation | NOT STARTED | — | Full repository sandbox validation |
| P3 — Curated orchestration | NOT STARTED | — | Registry, batch jobs, persistence |
| P4 — Fork contribution | NOT STARTED | — | Fork/branch/PR and approval |
| P5 — Dashboard/product UX | NOT STARTED | — | End-to-end visibility and persistent history |
| P6 — Deployment | NOT STARTED | — | Security, operations and deployed workflow |
| P7 — Evaluation/release | NOT STARTED | — | Benchmark and measured quality |

### Initialization handoff — 2026-11-01

Preparation only; **all phase statuses above remain NOT STARTED**. Next implementation model: GPT-6.1 Terra Medium.

- Canonical scope/order/acceptance: this document, read completely during initialization. Permanent operating instructions: root `AGENTS.md`. No parallel roadmap or handoff file was created.
- Verified starting Git state: `main`, HEAD `e5f636d34b734ac6ff42414335bdae79ae6a5d85`, origin `https://github.com/saketh1125/Argus.git`. The initial checkout was clean; the user then supplied this document as the only untracked file. Initialization stages only this document and `AGENTS.md`. Resolve the initialization commit via `git log -1 -- AGENTS.md`; inspect current HEAD/status again on takeover rather than treating this snapshot as current forever.
- Inspected package layout, orchestration, configuration, interfaces/factories, parsing/patching, key agent paths, service adapters, tests, dashboard, control-panel process calls and existing documentation. No root/nested `AGENTS.md` existed; `.agents/` and `.codex/` contained no instruction/configuration files. Existing ignored `.claude/` settings and all earlier reference documents were preserved. No tracked CI workflow was found.
- Static verification confirms host-shell sandbox fallback (including mock mode), LLM-generated test commands, incomplete-file validator/publication fallbacks, direct scanned-repository GitHub writes, and hardcoded E2B success text. These remain unfixed. See `services/sandbox.go`, `services/factory.go`, `agents/validator.go` and `agents/pr_agent.go`.
- Other verified caveats: preprocessing parses sanitized source and orders files by filesystem modification time; analysts check evidence length/confidence without verifying the quote against source; analyst timeout is 90 seconds despite stale 30-second comments. Service mode excludes GitHub from its aggregate, although the CLI banner prints GitHub separately. Mock fix generation adds a comment while preserving behavior; generated mock tests are placeholders, not regression proof.
- Existing test inspection found selected unit coverage, an always-successful fake validator sandbox, and a PR-agent test explicitly expecting the unsafe function-only fallback. No coverage percentage or end-to-end correctness is established. `services/nim_integration_test.go:TestNIMLiveConnect` loads configuration, logs the API key and calls NIM directly when a key exists; `FORCE_MOCK` does not gate that test. Establish a credential-free test environment without loading/printing local secrets before executing baseline checks.
- Checks performed in preparation: read-only Git status/branch/HEAD/remotes/history/tree inspection; source, test, configuration and documentation inspection. Documentation scope verification passed: the supplied plan is unchanged outside this handoff and all eight phase statuses remain NOT STARTED. Staged whitespace checking flags only pre-existing Markdown hard-break spaces in the supplied plan; preserve those intentional spaces. New instruction/handoff text has no trailing whitespace. No Go tests, vet, race tests, builds, runtime demos, provider calls or deployments were run. No application code, tests, dependencies, runtime configuration or CI was modified. No Phase 0 task or exit gate is certified.
- Exact next task: **P0.1 — Establish local baseline**. Inspect the current Git state and instructions first; record actual toolchain/check outcomes and mock/live behavior safely. The prohibition on untrusted host execution and LLM-controlled commands applies during baseline reproduction too. Use genuine isolation for unsafe paths; if unavailable, record that portion as blocked rather than running the unsafe fallback. Do not mistake documentation preparation for baseline execution.
- Continue Phase 0 tasks sequentially only within the authorized implementation session. Stop and report at the Phase 0 boundary; do not begin Phase 1 until its prerequisite gate passes and continuation is authorized. Report unavailable isolation, provider failures and other unmet criteria explicitly.

### P0.1 partial baseline — 2026-11-01

- Starting HEAD `c189f53de2e1e6eb6a137cb4d5c5a5c441bd4b47`, branch `main`, clean tracked working tree; Go `go1.24.0 linux/amd64`, module declares Go 1.24 and no third-party requirements.
- Checks used a `git archive` copy under `/tmp`, an empty environment with explicit tool paths and no credentials, `GOPROXY=off`, and an isolated Go cache. No pipeline or untrusted target was run. `go test ./...` and `go test -race ./...` failed: three dashboard tests cannot bind `127.0.0.1` in this sandbox (`socket: operation not permitted`); agents, services and tools passed. Live NIM test was skipped because credentials were absent. `go vet ./...` passed.
- `go build ./...` failed in the archive because Git metadata was unavailable for VCS stamping; `go build -buildvcs=false ./...` passed. `gofmt -l .` identified `agents/pr_agent_test.go`.
- Adapter selection is statically inspected but has not been reproduced at runtime. Genuine sandbox availability and live mode remain unverified. The existing host executor and publication fallbacks remain unsafe; do not run the pipeline against untrusted inputs. Phase 0 exit gate remains unmet.

### P0 continuation — 2026-11-01

- Baseline rerun in credential-free local Git clone of `c189f53de2e1e6eb6a137cb4d5c5a5c441bd4b47`: build and vet passed. Dashboard tests failed under restricted execution with `socket: operation not permitted`; the unchanged tests passed with loopback access outside that restriction. Fixed pre-existing formatting in `agents/pr_agent_test.go`.
- Replaced local shell and unverified E2B execution with an unavailable sandbox for every mode. Added adversarial regression checking no execution or file writes. Validator uses deterministic language-specific argv, discards generated test commands, and refuses unsupported languages. No provider is asserted isolated; E2B implementation and live validation remain unavailable.
- Validator rejects missing/empty source, source identity mismatch, ambiguous/missing function, empty/no-op rewrites, unsafe paths, failed exit codes and cancelled contexts. It records SHA-256 hashes of the indexed base and patched file. Source is sanitized before indexing, so hash mismatch can conservatively reject a valid patch; this is fail closed. Commit-SHA binding is not yet implemented. Added tests for ambiguous/missing/no-op patches, stale source, nonzero interpreter exit, argv and unavailable sandbox.
- Disabled all live GitHub branch, commit, PR and issue writes and the PR agent publication path until fork provenance, isolated validation and patch/base-bound human approval exist. Added no-write regression tests. No fork workflow or approval mechanism is implemented; publication remains unavailable.
- Added `.github/workflows/ci.yml` for formatting, vet, build, unit and race checks. Hosted CI has not run. The dashboard reports the count actually validated rather than attempts, and the CLI displays `sandbox=unavailable`.
- Final credential-free Git checkout checks with loopback access: `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, `go build ./...` all passed; `gofmt -l .` returned no files. Live NIM was skipped without credentials. `go run . --mock --no-dashboard --repo ./testdata/sample-repo` completed with mock adapter labels, unavailable sandbox, zero PRs, and two demoted findings; it did not execute target code.
- Phase 0 remains **IN PROGRESS**: no genuine isolated provider verified, commit-pinned provenance and full fork/approval workflow absent, live mode and hosted CI not verified. Do not begin Phase 1. Working changes are uncommitted; inspect Git state on resumption.

### P0 continuation — hosted CI and provider availability

- With explicit user authorization, committed the existing Phase 0 changes on `codex/phase0-safety-ci` and pushed commit `c23a756627b4b0a6ef765c522446c7dbffce8dd1`. GitHub Actions run [41311024906](https://github.com/saketh1125/Argus/actions/runs/41311024906) completed successfully for that exact commit, including formatting, vet, build, unit and race checks. The later uncommitted provider-failure regression is not included in that hosted run.
- Container tooling exists. Restricted execution could not access the Docker socket; an elevated read-only `docker info` returned version 29.8.2 with seccomp and SELinux available. Podman and bubblewrap probes failed under the restricted environment. These probes do not verify untrusted-code isolation, file-transfer security, limits, timeouts or cleanup; no provider was enabled. The sandbox remains unavailable, with no host execution fallback.
- Added a regression test that a sandbox provider error invalidates a previously validated finding and demotes it. `go test -count=1 ./agents` passed in the credential-free checkout. Commit-bound provenance, modified-patch approval invalidation, verified isolated-provider integration and fork-based publication remain unimplemented. Publication stays disabled; the Phase 0 exit gate is not met.

### P0 continuation — isolation probe and approval binding

- A disposable `redis:7-alpine` container was run with `--network=none`, read-only root, UID/GID 65534, all capabilities dropped, `no-new-privileges`, 64 MiB memory, 32 processes, 0.5 CPU and a constrained temporary filesystem. Observed UID 65534, zero effective capabilities, no-new-privileges enabled, cgroup values of 67108864 bytes / 32 processes / 50000/100000 CPU, a non-writable `/etc`, no Docker socket and only the loopback interface. The container exited and Docker `--rm` was requested. This probe did not establish escape resistance, transfer safety, network attack resistance, timeout enforcement, provider failure behavior or cleanup under failures. The daemon is not rootless and shares the host kernel; no execution provider was enabled. Adversarial integration and independent boundary assessment remain outstanding.
- Added a standalone `models.PatchProvenance` / `PatchApproval` digest binding and tests for changed commit, patch digest and result hash, and incomplete provenance. The tests were first observed failing to compile before implementation; `go test -count=1 ./models` passed after implementation. This binding does not authenticate an approver, validate actual bytes or provide consent; it is not wired into ingestion, validation or publication. Commit pinning and durable approval records remain outstanding.
- The PR agent and live GitHub writes remain blocked. No fork creation/reuse, commit or PR workflow has been implemented or verified. Hosted CI success reported above applies to `c23a756`, not these subsequent uncommitted changes. Phase 0 remains in progress; do not begin Phase 1.

### P0 continuation — actual-byte provenance primitive

- Added byte-derived SHA-256 provenance construction and verification to the existing standalone approval-binding model. Tests reject mutations of original, resulting and patch bytes; existing tests reject changed base, patch and result digests. This is a primitive only: it does not prove patch application or repository/commit identity, authenticate an approver, persist approval, or connect the chain to the pipeline. Publication stays disabled.
- In a credential-free Git checkout with loopback available, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, `go build ./...` passed and `gofmt -l .` reported no files. These checks include the previously uncommitted provider-failure test and provenance changes; no live provider test ran. Hosted CI must be verified against the resulting commit.
- The host has Docker with shared kernel, but no `/dev/kvm`, Firecracker or gVisor executable found. A secure provider lifecycle, adversarial isolation, commit-pinned ingestion, authenticated durable approval and fork publication remain outstanding. Phase 0 is not complete and Phase 1 has not started.

### P0 continuation — recheck source during validation

- Hosted CI [41311612987](https://github.com/saketh1125/Argus/actions/runs/41311612987) passed for commit `59cc2ec34a7a2e8a1e2d07ddb6e798151362cdad`, including formatting, vet, build, tests and race checks.
- Validator now re-resolves and re-hashes its source after the sandbox returns and rejects an observed change; an adversarial mutation test failed before the fix and passes after it. The selected candidate now carries the full-file diff and its SHA-256 digest. This does not close filesystem TOCTOU races, establish commit pinning or verify patch application at publication; publication remains blocked.
- For the new code, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, `go build ./...` and formatting checks passed in the credential-free checkout with loopback access. Hosted CI must be checked for the commit containing this change. The isolated provider, authenticated approval, fork workflow and Phase 0 exit gate remain open.

### P0 continuation — constrain Git clone inputs

- Hosted CI [41311729107](https://github.com/saketh1125/Argus/actions/runs/41311729107) succeeded for commit `034a9b7f77664a1f7d31813e30f508e90ab79284`.
- The live clone path previously accepted arbitrary remote strings as Git arguments, allowing remote-helper or option interpretation. It now accepts anchored GitHub URLs, reconstructs an HTTPS URL, uses an option terminator and disables inherited Git configuration and non-HTTPS remote protocols. Spoofed hosts and traversal-like identities are rejected. The adversarial URL test failed before the fix and passes after it. Existing local-copy behavior is unchanged and still requires separate traversal/symlink hardening.
- Credential-free checkout checks after this change: full tests, race tests, vet, build and formatting passed. Hosted CI has not yet been checked for the commit containing this change. Secure isolated validation, commit-bound provenance and fork/approval publication remain unfinished; Phase 0 is not complete.

### Current next action

Start **P0.1 — Establish local baseline**. Do not add features yet.

1. Inspect git status and current commit.
2. Run and record current test/build checks.
3. Reproduce current adapter selection.
4. Trace unsafe sandbox execution and incomplete patch fallback.
5. Implement and test the two P0 fixes.
6. Stop and report against the P0 exit gate.

---

## 11. Decision log

Record only decisions that materially change this plan. Do not create parallel notes.

| Date | Decision | Impact |
|---|---|---|
| 2026-11-01 | Product identity frozen as Autonomous Open-Source Contributor | Optimise for validated contributions, not issue volume |
| 2026-11-01 | Repository discovery frozen as curated | No autonomous GitHub-wide discovery in V1 |
| 2026-11-01 | Fork-based publishing and explicit approval required | No direct upstream writes and no automatic merging |
| 2026-11-01 | This file is the canonical execution source | Earlier plans/specifications are reference-only |

---

## 12. Final instruction

Build ARGUS in the order defined here. Prioritise correctness, safety, reproducibility and contribution quality over feature count.

When a requirement is ambiguous, inspect implementation and tests, then choose the smallest safe interpretation consistent with this contract. If a decision changes product scope, stop and ask Saketh rather than silently changing the plan.

**The goal is not to make ARGUS look autonomous. The goal is to make its contributions trustworthy.**
