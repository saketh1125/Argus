# Argus Execution Log

Append-only checkpoint log. Record verified work and blockers; do not rewrite prior entries.

## 2026-11-03 — Fail-closed provider and publication audit

- Starting checkout was clean on `codex/pr-patch-safety` at `6e7dc0033438d12c03eed016640f3e69278ecf3d`; PR #2 was open, draft, unmerged, with the same head.
- GitHub Actions run [#29](https://github.com/saketh1125/Argus/actions/runs/41652050754) passed on that starting head: formatting, `go test ./...`, vet, race tests, native build and Windows AMD64 build.
- `.env` is absent; a presence-only inventory found `GITHUB_TOKEN`, `GITHUB_ALLOWED_REPOS`, `NIM_API_KEY`, `JINA_API_KEY`, `GEMINI_API_KEY`, `E2B_API_KEY` unset. No secret values were read or emitted. Optional endpoint/tuning variables were also unset.
- Removed the unverified E2B REST adapter. E2B credentials, if later provided, cannot enable validation until a reviewed implementation exists. Missing or configured-but-disabled E2B returns an unavailable sandbox and refuses commands.
- Added regression coverage for E2B key not enabling an unverified adapter and missing provider credentials selecting identifiable mock GitHub/model services with unavailable validation.
- Live GitHub publication now refuses before side effects because exact-patch approval and fork-based publication are not implemented. A test confirms no branch, commit, PR or issue write is reached.
- GitHub REST writes no longer retry on 5xx; ambiguous write failures are marked outcome-uncertain. Added a 30-second client timeout, an 8 MiB response cap, and tests for uncertain non-retried writes and oversized responses.
- Removed NIM API-key logging from the live integration test; it now requires `ARGUS_RUN_LIVE_NIM_TESTS=true` explicitly and is not part of default live verification.
- Targeted `go test ./services ./agents .` — PASS; full `go test ./...` — PASS; `go vet ./...` — PASS.
- Focused GitHub retry/response-bound tests — PASS.
- `go build ./...` — exit 0, with a Go module stat-cache access warning under the restricted environment.
- Local `go test -race ./...` — BLOCKED because `CGO_ENABLED=0`; prior CI race run #29 passed only on the pre-audit head.
- Offline mock sample workflow — PASS: mode and selected adapters explicitly labelled mock; sandbox unavailable; 3 candidates, 0 PRs, 2 demoted. No live provider or publication credentials were used.
- `git diff --check` — PASS; Git emitted expected Windows line-ending conversion warnings.
- P0 remains in progress. E2B live API/isolation/lifecycle and exact-patch approval/fork publication remain blocked; follow-up CI must run after push.

## 2026-11-03 — Push and CI confirmation

- Committed the follow-up as `a039095fbb517e90fcda430d3806f056ea79f6a9` and pushed it fast-forward to `origin/codex/pr-patch-safety` after checking the remote still pointed to `6e7dc00`.
- PR #2 remains open, draft and unmerged; its head is `a039095`.
- GitHub Actions run [#31](https://github.com/saketh1125/Argus/actions/runs/41656786547) completed successfully on the audit commit: formatting, `go test ./...`, vet, race tests, native build and Windows AMD64 build passed.

## 2026-11-03 — Documentation checkpoint CI

- Recorded the successful audit CI result in `EXECUTION_PLAN.md` and `SPRINT_REPORT.md`, and replaced the completed push task with the current E2B verification blocker.
- Documentation checkpoint commit `3d94f81e6046bbea45ade8289c343231cd58a4b6` was pushed fast-forward to `origin/codex/pr-patch-safety`.
- PR #2 remains open, draft and unmerged at `3d94f81`.
- GitHub Actions run [#33](https://github.com/saketh1125/Argus/actions/runs/41657123955) completed successfully on that documentation checkpoint.

## 2026-11-03 — Validator result and host-clone boundary audit

- Reconfirmed clean baseline `b73e02476b22ef4f9a8178affa3ed3e0d64394b0` on the synchronized `codex/pr-patch-safety` branch before edits.
- Validator success now requires aggregate and per-command zero exits, no timeout, exact completed-command count and non-cancelled context. Adversarial tests reject missing completion metadata, hidden earlier command failure, nonzero exit, timeout, explicit failure, cancellation and oversized output.
- Sandbox result output is capped at 4 MiB; stored validation text and failure details are capped at 64 KiB.
- Production `githubREST` clients now reject all branch/content/PR/issue writes before network access. Added a regression that asserts zero requests; package tests explicitly opt into the internal test seam when exercising REST conflict guards.
- Git cloning now rejects remote-helper URLs, normalizes accepted remotes to GitHub HTTPS, clears ambient Git configuration/path/helper variables, disables credential helpers and LFS smudge, and applies a two-minute timeout.
- Local directory copying now rejects symlinks and non-regular files. The Windows symlink regression skipped because symlink creation privilege is unavailable; Linux CI must verify it.
- Focused tests, full `go test ./...`, `go vet ./...`, and `go build ./...` pass; build emits a restricted stat-cache warning. The offline mock workflow passes with sandbox unavailable and no PRs. No live credentials were used.
- This continuation remains uncommitted; its CI, including the Linux symlink test, is pending.

## 2026-11-03 — Host-boundary CI confirmation

- Pushed implementation commit `8a882540a26b2613d687add46e789878e3770d82` and test-hardening commit `6c88dbc94730b0c4f9077eb327c1f5236884d55b` as fast-forwards to `origin/codex/pr-patch-safety`.
- The symlink test skips only on Windows, where symlink creation is privilege-gated; it fails on other platforms if symlink creation is unavailable, so Linux CI cannot silently skip the check.
- PR #2 remains open, draft and unmerged at `6c88dbc`.
- GitHub Actions [#39](https://github.com/saketh1125/Argus/actions/runs/41660865916) passed formatting, tests, vet, race tests, native build and Windows AMD64 build on the current head.

## 2026-11-03 — Git hook execution hardening

- Replaced the null-device Git hook path with a unique empty temporary hooks directory for platform-independent hook suppression during clone.
- Added a regression for the clone arguments. Targeted test and full `go test ./...`, `go vet ./...`, `go build ./...` pass locally; the build still emits the restricted module stat-cache warning.
- This refinement is local after `6c88dbc`; CI is pending its push. E2B remains unavailable and no live credentials were used.

## 2026-11-03 — Git hook hardening CI confirmation

- Pushed `f22c54e7841e91e4b929bf0d54e24f67b8df96cb` fast-forward to `origin/codex/pr-patch-safety`.
- PR #2 remains open, draft and unmerged at `f22c54e`.
- GitHub Actions [#41](https://github.com/saketh1125/Argus/actions/runs/41662158674) passed formatting, tests, vet, race tests, native build and Windows AMD64 build on the current head.

## 2026-11-03 — Checkpoint documentation CI

- Documentation checkpoint `9f05f925fb1ddde732c98cc6b610b741d6b09de8` was pushed fast-forward after recording the Git hook hardening result.
- PR #2 remained open, draft and unmerged at `9f05f92`.
- GitHub Actions [#43](https://github.com/saketh1125/Argus/actions/runs/41662533632) passed all configured checks on the documentation checkpoint.

## 2026-11-03 — Latest checkpoint CI and credential-free audit

- Confirmed `.env` is absent and checked integration credential presence without reading values; `GITHUB_TOKEN`, `GITHUB_ALLOWED_REPOS`, `NIM_API_KEY`, `JINA_API_KEY`, `GEMINI_API_KEY`, and `E2B_API_KEY` are unset.
- Reviewed `.env.example`, config loading, and integration selection. Credential-free and mock tests cover model/GitHub adapters and unavailable sandbox behavior. Live NIM needs `NIM_API_KEY` plus `ARGUS_RUN_LIVE_NIM_TESTS=true`; live E2B would need `E2B_API_KEY` after a supported adapter is implemented; GitHub writes also require repository authorization and remain disabled.
- PR #2 is open, draft, unmerged, and at `d9b82a2d2187ffde22828cc421d02bf99795fea7`.
- GitHub Actions [#45](https://github.com/saketh1125/Argus/actions/runs/41662912698) completed successfully on `d9b82a2`, passing formatting, tests, vet, race tests, native build, and Windows AMD64 build.
- Phase 0 remains in progress: real sandbox isolation and lifecycle, exact-patch approval, fork publication, and additional interruption/persistence failure coverage are not complete.

## 2026-11-03 — Validation provider provenance

- Fixed a reporting gap where PR text asserted sandbox validation without naming the provider. `Issue` now carries validator-stamped provider and live/mock metadata; each validation run clears model-provided claims, records the actual attempted adapter, and retains the result only from the sandbox outcome.
- Publication patch preparation now rejects missing or non-live sandbox provenance. PR bodies and failed-validation summaries identify the actual provider/mode and do not claim mock or missing validation passed.
- Added regressions for forged provider metadata, mock/missing publication provenance, accurate PR wording, and failure-summary provider reporting.
- Focused regressions — PASS; full `go test ./...` — PASS; `go vet ./...` — PASS; `go build ./...` — exit 0 with restricted module stat-cache warning; `git diff --check` — PASS.
- CI and push for this implementation are pending. No live credentials or publication were used; E2B remains disabled.
- Reviewed current E2B SDK sources: the [official project quickstart](https://github.com/e2b-dev/E2B#run-your-first-sandbox) lists JavaScript/Python; the Go SDK located identifies itself as [unofficial and community-maintained](https://github.com/eric642/e2b-go-sdk#e2b-go-sdk). No package was added because this provider choice needs review.

## 2026-11-03 — Validation provenance CI confirmation

- Pushed `ee0c9c8d0d9816775ef7a8565aa75d4565b13534` fast-forward to the PR branch.
- GitHub Actions [#49](https://github.com/saketh1125/Argus/actions/runs/41665041546) passed all configured checks on the validation-provenance implementation.
- PR #2 remains open, draft and unmerged; E2B and live publication remain disabled.

## 2026-11-03 — Checkpoint documentation CI

- Documentation checkpoint `53ccad0a6a6309e8e103cc41ff8cfb8b2ac4f25d` was pushed fast-forward to the PR branch.
- GitHub Actions [#51](https://github.com/saketh1125/Argus/actions/runs/41665401475) passed all configured checks on the checkpoint.
- PR #2 remains open, draft and unmerged.

## 2026-11-03 — Phase 0 baseline and host-execution safety

- Checkout began clean on `codex/pr-patch-safety` at `0089d0779c00f53c4f45acfd2437e093a2cbd484`; remote is `origin` at `https://github.com/saketh1125/Argus.git`.
- Runtime: Go 1.27.3, Windows/AMD64, `CGO_ENABLED=0`; module directive is Go 1.24.
- PR #2 was open and draft, with one commit, no reviews and no GitHub checks. It targets `main` from `codex/pr-patch-safety`.
- Baseline `go test ./...` passed before edits.
- Removed host-local execution fallback. Mock mode and missing E2B now choose an unavailable sandbox that refuses validation explicitly.
- Validator no longer invokes model-generated test commands.
- Added unavailable-provider tests and a Go CI workflow covering formatting, tests, vet, race, native build and Windows AMD64 cross-build.
- Post-change `go test ./...`, `go vet ./...`, `go build ./...`, and mock sample workflow passed. The mock workflow raised no PRs and demoted two fixes due unavailable isolated validation.
- Formatting passed for changed Go files. A repository-wide scan found pre-existing unformatted files outside this change; they were not modified.
- `go test -race ./...` could not start because CGO is disabled; no compiler was found.
- Committed the checkpoint locally as `0c57dc7`. Push to `origin/codex/pr-patch-safety` was rejected by automatic approval review because the exact destination and source/workflow payload were not considered sufficiently authorized. No alternate publication route was attempted; PR #2 remains unchanged.
- Phase 0 remains open. E2B behavior, more adversarial failure recovery, and remote CI results remain unverified.

## 2026-11-03 — Push and CI confirmation

- With explicit user approval, pushed commits `0c57dc7` and `996d521` to `origin/codex/pr-patch-safety`.
- PR #2 remains open and draft; its head is `996d521`.
- GitHub Actions run [#27](https://github.com/saketh1125/Argus/actions/runs/41651669380) completed successfully: formatting, test, vet, race test, native build and Windows AMD64 cross-build all passed.
- The branch is synchronized with origin and the working tree is clean.
- Phase 0 remains open pending E2B/provider verification and remaining adversarial safety coverage.

## 2026-11-03 — Windows-to-Linux validator portability and credential audit

- Starting state: branch `codex/pr-patch-safety`, local/remote commit `69c2d83b82cf7e3997ea291c5a275d15036c0672`, PR #2 open/draft/unmerged, no remote diff. The tree already contained three local validator portability edits from the prior continuation; these were preserved.
- PR #2 metadata confirms head `69c2d83`; GitHub Actions run [#55](https://github.com/saketh1125/Argus/actions/runs/41666072125) completed successfully on that pushed head. No CI result yet covers the current local edits.
- Confirmed `.env` absent and all checked credential/config variable names unset using presence-only checks. No secret values were read or emitted.
- Medium issue fixed locally: buildSpec and syntax command generation now produce Linux sandbox paths and `python3`/POSIX quoting regardless of host OS. Added `TestBuildSpecUsesLinuxCommandsAndPathsOnEveryHost`.
- Credential-free `go test ./config ./services ./agents ./tools -count=1` — PASS; full `go test ./... -count=1` — PASS. Regression suite confirms missing and synthetic-present-but-disabled E2B fail closed, missing GitHub/model credentials choose visible mocks, direct live GitHub writes are rejected before network calls, and mock publication remains isolated from real GitHub.
- `go vet ./...` — PASS; changed-file `gofmt -l` — no output; `git diff --check` — PASS with Windows line-ending conversion warnings; `go build ./...` — exit 0 with restricted Go module stat-cache warning.
- Local `go test -race ./...` — BLOCKED: Go requires CGO and `CGO_ENABLED=0`. Offline sample run — PASS: mock mode identified, sandbox unavailable, zero PRs, two candidates demoted.
- Live E2B/NIM/GitHub verification — NOT RUN. Live E2B will require a reviewed adapter, `E2B_API_KEY`, and explicit user authorization before credential transfer; no credential is requested by this checkpoint. Live NIM requires `NIM_API_KEY` plus `ARGUS_RUN_LIVE_NIM_TESTS=true`. GitHub live publication remains fail-closed pending exact-patch approval and fork workflow.
- Phase 0 remains IN PROGRESS. No new Critical/High bypass was found in this pass. Local edits and CI status are pending checkpoint commit/push.

## 2026-11-03 — Validator portability CI confirmation

- Committed the Windows-to-Linux validator portability fix and checkpoint updates as `b79abbae397ae97c2fca84b1d9f7a7ea2a8f1a48`, then pushed a fast-forward from `69c2d83`; no history rewrite or merge was performed.
- PR #2 remains open, draft and unmerged at `b79abba`.
- GitHub Actions runs [#56](https://github.com/saketh1125/Argus/actions/runs/41668264467) and [#57](https://github.com/saketh1125/Argus/actions/runs/41668279346) both passed formatting, tests, vet, race tests, native build and Windows AMD64 cross-build.
- Phase 0 remains IN PROGRESS. CI verifies the command-construction change, while live E2B execution still requires a reviewed provider implementation, `E2B_API_KEY`, and explicit authorization. Exact-patch human approval and fork-based publication remain unimplemented and live writes remain disabled.

## 2026-11-03 — Dashboard launcher CSRF and truthful status audit

- Starting state: clean branch `codex/pr-patch-safety` at `d9cb6d2650d12a475525ae79e4658969605dc8df`; PR #2 remained open/draft/unmerged. Public Actions metadata showed documentation checkpoint runs #58 and #59 passed on that head.
- High issue found: dashboard POST /start had no Host/Origin/CSRF validation and could trigger analysis of a user-selected local path from a cross-site form. This could direct configured model/embedding services to process local source.
- Added strict loopback Host/Origin checks, a cryptographic one-time CSRF token in the POST body, no-store on the token-bearing page, a 16 KiB form cap, POST-body-only parsing, and atomic single-launch acceptance.
- Removed the unused GitHub token and disabled E2B key fields from the web launcher. Corrected README, .env.example, UI and source comments that incorrectly claimed host-local syntax validation, enabled PR creation, or fully live zero-key operation.
- Focused launcher tests — PASS, including cross-origin, Host rebinding, wrong port, query-only/missing/replayed token, oversized request, one-shot and concurrent submissions. Full go test ./... -count=1 — PASS; go vet ./... — PASS; go build ./... — exit 0 with restricted module stat-cache warning.
- Offline mock sample — PASS: mode/adapters labelled, sandbox=unavailable, 3 candidates, zero PRs, 2 demoted. Local race test — BLOCKED because CGO_ENABLED=0. CI for current local changes is pending.
- .env/credential values were not read. No live model, GitHub or E2B operation occurred. Phase 0 remains IN PROGRESS; sandbox execution and publication workflow remain unresolved.

## 2026-11-03 — Dashboard launcher fix CI confirmation

- Committed launcher hardening and checkpoint evidence as `77ccd1a2d98ced452d9f75c715ff4099cd74211b`; pushed a fast-forward from `d9cb6d2` to `origin/codex/pr-patch-safety`. PR #2 remains open, draft and unmerged.
- GitHub Actions run [#61](https://github.com/saketh1125/Argus/actions/runs/41671901691) passed formatting, tests, vet, race test, native build and Windows AMD64 cross-build.
- Focused launcher tests and `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and offline mock sample also passed locally. The local race command remains unavailable because `CGO_ENABLED=0`.
- Presence-only environment inventory confirmed `.env` is absent and integration variables are unset; secret values were not read. Credential-free, mock-based and live-only requirements are classified in `EXECUTION_PLAN.md`.
- No live provider, E2B, credential, GitHub publication or PR creation was exercised. Phase 0 remains IN PROGRESS; E2B isolation and exact-patch approval/fork publication remain blockers.

## 2026-11-03 — Repository symlink preprocessing boundary

- Found a High host-data exposure risk: live Git checkouts can contain symlink entries, and preprocessing later read selected paths with `os.ReadFile` without rejecting symlinks. A repository link could point outside the checkout and expose the target contents to parsing/embedding providers.
- Candidate discovery now ignores symlink entries and non-regular files before the later reads. Added `TestCollectSourceCandidatesSkipsSymlinkTargetsOutsideCheckout`.
- Focused regression and package/full Go tests passed, but Windows skipped symlink creation because the host lacks the required privilege. `go vet ./...` passed; `go build ./...` exited 0 with the restricted module stat-cache warning. Linux CI is required for execution evidence and is pending.
- Starting pushed head was `37022ff`; CI run #63 passed on that prior head. At this checkpoint entry the fix was local and pending CI; the following entry records its commit and CI result. No live credentials/provider calls were used.

## 2026-11-03 — Repository symlink boundary CI confirmation

- Committed the symlink filtering fix and regression as `b135e96f2d6b2b372fb29acd60e0cce2a960ccc7` and pushed a fast-forward from `37022ff`.
- GitHub Actions run [#65](https://github.com/saketh1125/Argus/actions/runs/41681861358) passed formatting, tests, vet, race test, native build and Windows AMD64 cross-build on `ubuntu-latest`. The symlink regression executed there; it skips only on this Windows host due symlink privilege restrictions.
- Host clone/preprocessing boundary and current-code CI checkpoint are now VERIFIED. The equal-weight 19-row estimate is 87%: 16 VERIFIED, 1 PARTIALLY VERIFIED, 2 BLOCKED.
- PR #2 remains draft and unmerged. E2B provider execution and exact-patch approval/fork publication remain unresolved. No credentials or live services were used.

## 2026-11-03 — Per-file indexing byte limit

- Found a Medium resource-exhaustion path: selected supported source files were read in full without a byte ceiling. Added a 1 MiB source-file limit during discovery plus a bounded read to protect against growth between discovery and open; oversized files are counted and reported.
- Added a regression proving oversized files are excluded and bounded reads stop at the cap. Focused and full Go tests, vet and build passed locally. Build exited 0 with the known restricted stat-cache warning.
- README documents the per-file limit and states that the two-minute live clone timeout does not provide a download byte quota. The clone transfer storage bound remains unimplemented.
- Local change is pending CI; no secrets or live providers were used.

## 2026-11-03 — Per-file byte limit CI confirmation

- Committed the per-file size bound and checkpoint updates as `78f775a55656494072f5ae7f63abb1e0682443d4`; pushed a fast-forward from `3ec51e1`.
- GitHub Actions run [#69](https://github.com/saketh1125/Argus/actions/runs/41683305374) passed formatting, tests, vet, race test, native build and Windows AMD64 cross-build. The oversized-file and bounded-read regression ran in CI.
- Per-file preprocessing input bounds and the current CI gate are now VERIFIED. The remaining clone transfer byte-quota gap is documented. E2B live execution and exact-patch approval/fork publication remain blocked; no secrets or live services were used.

## 2026-11-03 — Current PR and checkpoint CI state

- Read-only GitHub metadata showed PR #2 open, draft and unmerged, targeting `main`, with head `fd0ee9ce989ef003c9e4937c4c419ca95852b014` at the time of this check.
- GitHub Actions run [#71](https://github.com/saketh1125/Argus/actions/runs/41683558156) passed formatting, tests, vet, race test, native build and Windows AMD64 cross-build on checkpoint commit `fd0ee9c`.
- Working tree was clean and branch `codex/pr-patch-safety` matched origin at the checked head. Phase 0 remains IN PROGRESS due E2B verification and disabled exact-patch/fork publication.

## 2026-11-03 — Bounded provider responses and NIM retries

- **Medium — live provider responses were read without a byte ceiling.** Added a shared 4 MiB reader for NIM, Ollama, Jina, Gemini and Qdrant that rejects oversized declared lengths and bounds unknown-length bodies to limit+1 bytes.
- **Medium — NIM retry could honor an extreme delay and ignore cancellation while sleeping.** Numeric `Retry-After` values now cap at 10 seconds; NIM retry and exponential-backoff waits return on context cancellation.
- Added focused fake-transport regressions for all five adapters, unknown-length byte limits, retry parsing/capping and prompt cancellation after a mock 24-hour `Retry-After` response.
- Focused service tests — PASS. `go test ./... -count=1` — PASS. `go vet ./...` — PASS. `go build ./...` — exit 0 with the restricted module stat-cache access warning. `git diff --check` — PASS; Git reports line-ending conversion warnings.
- No credentials or live provider calls were used. Latest confirmed pushed checkpoint remains `8640790`, with CI run [#73](https://github.com/saketh1125/Argus/actions/runs/41683949872) passing there; current local source changes are pending CI.
- The current acceptance estimate is 83%: 16 of 21 rows VERIFIED, 3 PARTIALLY VERIFIED at half credit, 2 BLOCKED. E2B live verification and exact-patch approval/fork publication remain blocked; clone transfer still has no byte quota.

## 2026-11-03 — Provider-boundary CI confirmation

- Pushed implementation commit `114ebd46ab3e494c02170976b9b7df91080ac948` as a fast-forward from `8640790`.
- GitHub Actions [#74](https://github.com/saketh1125/Argus/actions/runs/41685108596) and [#75](https://github.com/saketh1125/Argus/actions/runs/41685120189) both passed formatting, tests, vet, race test, native build and Windows AMD64 cross-build. The single `verify` job and every step passed on the pushed head.
- Read-only GitHub API metadata confirms PR #2 is open, draft and unmerged at `114ebd4`. Working tree was clean and synchronized before this documentation checkpoint.
- Provider-response and retry bounds are now VERIFIED with local mocked regressions and current-head CI. The current equal-weight estimate is 88%: 18/21 VERIFIED, 1/21 PARTIALLY VERIFIED at half credit, and 2/21 BLOCKED. E2B behavior and exact-patch approval/fork publication remain unresolved; clone transfer still lacks a byte quota.

## 2026-11-03 — Current checkpoint CI confirmation

- Documentation checkpoint `0944a79cc1148c7ea850700bc471158c8c4d98ad` passed GitHub Actions [#76](https://github.com/saketh1125/Argus/actions/runs/41685441353) and [#77](https://github.com/saketh1125/Argus/actions/runs/41685450175). Formatting, tests, vet, race, native build, Windows AMD64 cross-build, and every step succeeded.
- Read-only GitHub API metadata confirms PR #2 is still open, draft and unmerged at `0944a79`. The working tree is synchronized and clean at the verified checkpoint before this log entry.
- Phase 0 remains IN PROGRESS at 88% checklist estimate. E2B live behavior and exact-patch approval/fork publication remain BLOCKED; clone transfer has no byte quota.

## 2026-11-03 — Clone diagnostics and failed-workdir cleanup

- **Medium — Git clone stderr accumulation was unbounded.** Added a writer that retains no more than 64 KiB and marks truncated diagnostics without causing subprocess short writes.
- **Medium — failed preprocessing could leave a partial checkout in the system temp directory.** `Preprocessor.Run` now removes its workdir on every unsuccessful return, joins a cleanup error if necessary, and clears the returned path. A regression makes the mock clone write partial data and then fail, and confirms the directory is removed.
- Focused clone and preprocessing tests — PASS. `go test ./... -count=1` — PASS. `go vet ./...`, changed-file gofmt, and `git diff --check` — PASS. `go build ./...` — exit 0 with restricted Go module-cache warning. Symlink regression skipped locally on Windows because the required privilege is unavailable; prior Ubuntu CI #65 executed it.
- Current local changes are uncommitted and pending CI; last pushed checkpoint `eb533fd` passed CI #78/#79. No live credentials or providers were used.
- Interim estimate is 83% across 23 acceptance rows: 17 VERIFIED, 4 PARTIALLY VERIFIED at half weight, 2 BLOCKED. E2B and approval/fork publication blockers remain; a hard Git clone transfer byte quota is still unresolved.

## 2026-11-03 — Clone failure-path CI confirmation

- Pushed `bebaa311302175163a781060307a81e9bccaee7f` as a fast-forward from `eb533fd`.
- GitHub Actions [#80](https://github.com/saketh1125/Argus/actions/runs/41686622449) passed formatting, tests, vet, race, native build and Windows AMD64 cross-build; all steps succeeded.
- GitHub metadata confirms PR #2 remains open/draft/unmerged at `bebaa31`; working tree is clean and synchronized.
- The 64 KiB clone diagnostic cap and failed-preprocessing temp cleanup are VERIFIED. Current equal-weight estimate is 89% (20 VERIFIED, 1 PARTIALLY VERIFIED at half weight, 2 BLOCKED out of 23). E2B live execution and exact-patch approval/fork publication remain unresolved; a hard clone transfer byte quota remains an operational gap.

## 2026-11-03 — Latest pushed checkpoint verification

- Checkpoint `1cec25b981bcb97c343c64d866e05ec33a50a7e9` passed GitHub Actions [#82](https://github.com/saketh1125/Argus/actions/runs/41687009058): formatting, tests, vet, race, native build and Windows AMD64 cross-build all succeeded.
- Read-only GitHub API metadata confirms PR #2 remains open, draft and unmerged at `1cec25b`. The worktree is clean and synchronized at the checked head.
- Phase 0 remains IN PROGRESS. The hard clone transfer byte quota, live E2B execution and exact-patch approval/fork publication are outstanding.

## 2026-11-03 — Completion estimate denominator correction

- Split the hard clone transfer/disk quota into a separate BLOCKED acceptance row. The earlier 23-row estimate mentioned this gap but did not count it, so its 89% figure overstated coverage.
- Corrected estimate is **85%** across 24 equal-weight rows: 20 VERIFIED, 1 PARTIALLY VERIFIED at half weight, and 3 BLOCKED; `(20 + 0.5) / 24 = 85.4%`.
- This is a documentation correction only. The implementation checkpoint remains `bebaa31` (CI #80 passed), and the latest documentation checkpoint `e284e24` passed CI #84. No implementation or CI evidence was removed.

## 2026-11-03 — Bound local directory-copy fallback

- Added a 256 MiB aggregate file-byte limit and a 100,000-entry limit to local directory copies. Each file is revalidated against its inspected identity and streamed within the remaining aggregate byte budget, including a one-byte overflow probe that is not written to disk.
- Focused byte/entry regressions and full `go test ./... -count=1` pass. `go vet ./...`, gofmt and `git diff --check` pass; `go build ./...` exits 0 with the module-cache access warning.
- The remote Git HTTPS transfer itself remains unbounded; local copy caps do not resolve that blocker. This source change is local and awaits CI; last pushed checkpoint `12f3e27` passed CI #86.
- Interim checklist estimate is 82% across 25 rows: 19 VERIFIED, 3 PARTIALLY VERIFIED at half credit, and 3 BLOCKED. No live credentials or services were used.

## 2026-11-03 — Local-copy bounds CI confirmation

- Pushed implementation `430aa38cadd8d056fe985c7559d80047031f15ff` as a fast-forward from `12f3e27`.
- Connected GitHub checks [41688619335](https://github.com/saketh1125/Argus/actions/runs/41688619335) and [41688631081](https://github.com/saketh1125/Argus/actions/runs/41688631081) both passed formatting, tests, vet, race, native build and Windows AMD64 cross-build on `430aa38`; every job step succeeded.
- The 256 MiB/100,000-entry local-copy limits and regressions are now VERIFIED. Remote Git HTTPS transfer remains unbounded. Current score is 86% across 25 acceptance rows: 21 VERIFIED, 1 PARTIALLY VERIFIED at half credit, 3 BLOCKED.

## 2026-11-03 — Aggregate budget regression refinement

- Strengthened the byte-budget regression to prove aggregate accounting across multiple files and success exactly at the byte limit. Focused and full Go tests pass locally.
- This test-only refinement is pending CI; checkpoint run 41688981532 passed on `2cd61ae`. No live services or credentials were used.
- Interim checklist estimate is 82% (19 VERIFIED, 3 PARTIALLY VERIFIED at half weight, 3 BLOCKED across 25 rows) until the strengthened regression passes CI.

## 2026-11-03 — Aggregate-copy bounds CI confirmation

- Pushed `6c7dbf4022aaa597612f580d01b6dd2726f5b593` as a fast-forward from `2cd61ae`.
- CI run [41689430471](https://github.com/saketh1125/Argus/actions/runs/41689430471) passed formatting, tests, vet, race, native build and Windows AMD64 cross-build; every step succeeded.
- The aggregate byte, exact-limit and entry-budget tests now pass CI. PR #2 is open/draft/unmerged at `6c7dbf4`; working tree is clean and synchronized.
- Corrected checklist status is 86%: 21 VERIFIED, 1 PARTIALLY VERIFIED at half credit and 3 BLOCKED out of 25 rows. Remote Git transfer quota, E2B live execution and exact-patch approval/fork publication remain outstanding.

## 2026-11-03 — Bounded remote archive ingestion

- Replaced live host Git clone with GitHub REST metadata lookup and an archive request pinned to the resolved commit SHA. The preprocessor takes provenance from `CloneResult` and no longer runs `git rev-parse` on the host.
- The remote archive path caps compressed bytes at 128 MiB, expanded bytes at 384 MiB, regular-file bytes at 256 MiB, entries at 100,000, and total duration at two minutes. Redirects are restricted to HTTPS `codeload.github.com`; GitHub authorization is never forwarded to that host. Path traversal and special files are rejected; symlinks and hard links are skipped.
- Added mock HTTP and extraction regressions covering pinned SHA, redirect token isolation, traversal, symlinks, and expanded/file/entry limits. `go test ./...`, `go vet ./...`, `go build ./...` and `git diff --check` pass locally. Build emitted a restricted module stat-cache access warning but exited 0. CI run [41692502687](https://github.com/saketh1125/Argus/actions/runs/41692502687) failed the C:-component path regression on Linux; fixed by rejecting colon-bearing names on every OS. Focused/full local tests pass; new-head CI is pending. No live credentials or sandbox were used.
- Interim estimate: 88% across 25 rows: 21 VERIFIED, 2 PARTIALLY VERIFIED, 2 BLOCKED; `(21 + 2×0.5) / 25 = 88%`. CI is pending; E2B live behavior and exact-patch approval/fork publication remain blocked.

## 2026-11-03 — Cross-platform archive path CI confirmation

- CI run [41692502687](https://github.com/saketh1125/Argus/actions/runs/41692502687) caught that Linux accepted an archive member containing a nested `C:` path component. Updated extraction to reject colon-bearing names on every OS; focused/full tests pass locally, as do vet and build.
- Follow-up commit `bd67a3073f4d0ff84665c7fc86718bdd9fcacb36` passed GitHub Actions [41693458484](https://github.com/saketh1125/Argus/actions/runs/41693458484): formatting, tests, vet, race, native build and Windows AMD64 cross-build all succeeded.
- PR #2 is open, draft and unmerged at `bd67a30`. Branch push was a normal fast-forward; worktree was clean before this documentation update. No live services or credentials were used.
- Updated estimate: 90% across 25 equal-weight rows: 22 VERIFIED, 1 PARTIALLY VERIFIED, 2 BLOCKED; `(22 + 0.5) / 25 = 90%`. E2B live behavior and exact-patch approval/fork publication remain blocked.

## 2026-11-04 — Fail closed on publication branch collisions

- The PR agent previously generated a timestamp-suffixed branch and retried when deterministic branch creation returned “already exists.” That behavior could bypass evidence of a prior partial/response-lost attempt and publish a duplicate. Branch collisions now stop the attempt; the candidate is surfaced as demoted. Regression verifies no commit or PR and only one branch attempt.
- E2B provider research rechecked current official materials: the vendor-maintained SDKs documented are JavaScript and Python; the Go SDK found is community-maintained. The sandbox factory remains unavailable with or without `E2B_API_KEY`; no host-execution fallback or unsupported dependency was introduced.
- Focused collision tests, full Go tests, vet, build and diff check pass locally; build exits 0 with a restricted module-cache stat warning. GitHub Actions run [41695801609](https://github.com/saketh1125/Argus/actions/runs/41695801609) passed formatting, full tests, vet, race tests, native build and Windows AMD64 cross-build on `71033f8`. No credentials or live publication were used.
- Updated estimate: 90% across 25 rows (22 VERIFIED, 1 PARTIALLY VERIFIED, 2 BLOCKED); `(22 + 0.5) / 25 = 90%`. Explicit mock/live distinction remains partial; E2B verification and exact-approval/fork publication remain blocked.

## 2026-11-04 — Reject forged analyst provenance

- Added `TestAnalystIgnoresForgedValidationAndProviderFields`. An adversarial model response attempts to inject validation success, live E2B provenance, validation logs and fake fix metadata; the analyst-created issue retains none of these trusted fields.
- Focused test, full `go test ./... -count=1`, `go vet ./...`, `go build ./...` and `git diff --check` passed locally. Build exited 0 with a restricted Go module stat-cache warning.
- Commit `cea1c59` passed CI [41697879198](https://github.com/saketh1125/Argus/actions/runs/41697879198): formatting, tests, vet, race, native build and Windows AMD64 cross-build. No credentials or live services were used.
- Checklist estimate remains 90%: 22 VERIFIED, 1 PARTIALLY VERIFIED, 2 BLOCKED across 25 rows. E2B live behavior and exact-approval/fork publication remain blocked.

## 2026-11-04 — Pin local copy reads to inspected entries

- Closed a local-copy TOCTOU gap: path-based recursive reads could follow a directory or file swapped to a symlink after inspection. Copy now uses `os.Root` handles, verifies source identity after opening, and preserves lexical entry order.
- Added deterministic file and directory replacement tests. Focused copy tests, full `go test ./... -count=1`, vet, build and diff check pass locally. Windows cannot perform the rename used by these tests in this environment; Linux CI executes them.
- CI [#112](https://github.com/saketh1125/Argus/actions/runs/41699606817) caught the initially unsorted directory order; fixed in `d764978`. CI [#114](https://github.com/saketh1125/Argus/actions/runs/41700207329) then passed formatting, full tests, vet, race, native build and Windows AMD64 cross-build.
- No live services or credentials were used. Checklist estimate remains 90%; live E2B and exact-approval/fork publication remain blocked.

## 2026-11-04 — Credential-free continuation audit

- Rechecked local and remote branch identity: both are `1199f701ddfa431a431c64192c25dff8a237d622`. PR #2 remains open, draft, unmerged and mergeable at that head. GitHub Actions run [#117](https://github.com/saketh1125/Argus/actions/runs/41700509066) passed its verify job, including formatting, tests, vet, race, native build and Windows AMD64 cross-build.
- Inspected `.env.example`, config loading, environment references, adapter factories and integration tests. `.env` is absent; presence-only checks found GitHub, NIM, E2B, Jina and Gemini credentials unset and the live NIM test opt-in false. No secret values were read or printed.
- Existing regressions verify blank GitHub/NIM keys select labelled mocks and missing or synthetic-present E2B credentials leave validation unavailable; unavailable sandbox rejects command execution. No live E2B, NIM, GitHub, or publication tests ran.
- `go test ./services ./tools ./config . -count=1` — PASS. `go test ./... -count=1` — PASS. `go vet ./...` — PASS. `go build ./...` — exit 0 with a restricted Go module stat-cache access warning. GitHub PR metadata is read-only; `git fetch` could not write `.git/FETCH_HEAD` and the GitHub CLI is unauthenticated.
- The inventory and verification classes are current in `EXECUTION_PLAN.md`. The estimate remains 90% over 25 equal-weight criteria: 22 VERIFIED, 1 PARTIALLY VERIFIED, 2 BLOCKED; `(22 + 0.5) / 25 = 90%`. E2B adapter/live isolation and exact-patch approval/fork publication remain blocked.
