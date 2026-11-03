# Sprint Report — Phase 0 Safety Checkpoint

## Follow-up audit — 2026-11-03

Base: `6e7dc0033438d12c03eed016640f3e69278ecf3d` (`Record successful Argus CI checkpoint`). Follow-up commits: `8a882540a26b2613d687add46e789878e3770d82`, `6c88dbc94730b0c4f9077eb327c1f5236884d55b`, and `f22c54e7841e91e4b929bf0d54e24f67b8df96cb`. PR #2 remains open, draft and unmerged.

### High-risk findings and resolutions

- **E2B adapter contract and execution result were unverified.** The previous code selected it solely from `E2B_API_KEY`, used unverified REST routes, and could accept an empty process response as exit code zero. Removed the adapter implementation and made the factory return `unavailable` even when an E2B key is supplied. This prevents both false validation and repository execution through an unverified provider. E2B live isolation remains **BLOCKED**, not verified.
- **Live GitHub publication lacked exact-patch approval and fork publication.** `PRAgent.Run` now refuses a live GitHub client before any write. A regression test confirms no branch, commit, PR or issue side effect. Publication remains **BLOCKED** pending those capabilities.
- **GitHub REST retried writes after 5xx and had no client/body bounds.** Writes now make one attempt and label server/network failures as outcome-uncertain for reconciliation; only read-only calls retry. The client has a 30-second timeout and responses are capped at 8 MiB. Focused tests verify no write retry and oversized-response rejection.
- **Live NIM test printed the API key.** Removed key logging and require `ARGUS_RUN_LIVE_NIM_TESTS=true` before the live test can run. The test is not run by default.

### Credential inventory and verification classes

The Windows checkout has no `.env`; presence checks showed all integration credentials unset. No secret values were read or printed. Credential-free verification includes unit tests, mock GitHub/model behavior, unavailable-sandbox behavior, and the offline sample workflow. Live NIM requires `NIM_API_KEY` plus the explicit test opt-in. Live GitHub requires `GITHUB_TOKEN`, `GITHUB_ALLOWED_REPOS`, and explicit publication authorization, but writes are currently rejected. E2B requires `E2B_API_KEY`; its implementation is disabled pending API, isolation and lifecycle verification. Optional Jina/Gemini keys or local Ollama, and Qdrant endpoint settings affect embeddings/vectors, not safety tests.

### Verification

- `go test ./services ./agents` — PASS after implementation.
- `go test ./tools -run 'TestGitHub(WriteFailureIsNotRetriedAndMarkedUncertain|ResponseSizeIsBounded)$' -count=1` — PASS.
- `go test ./tools -run 'TestGitHub(WriteFailureIsNotRetriedAndMarkedUncertain|ResponseSizeIsBounded|SuccessfulWriteWithMalformedResponseIsUncertain)$' -count=1` — PASS after the final partial-response regression was added.
- `go test ./...` — PASS.
- `go vet ./...` — PASS.
- `go build ./...` — exit 0; Go printed a restricted module stat-cache access warning.
- `go test -race ./...` — BLOCKED locally: `CGO_ENABLED=0`; Go requires CGO. GitHub Actions run [#31](https://github.com/saketh1125/Argus/actions/runs/41656786547) passed race tests on commit `a039095`.
- `FORCE_MOCK=true go run . --repo ./testdata/sample-repo --no-dashboard` — PASS. Banner identified all mocks and `sandbox=unavailable`; 3 candidates, 0 PRs, 2 demoted. The summary URL is an in-memory mock artifact.
- `git diff --check` — PASS (line-ending conversion warnings only).
- Live E2B, GitHub publication and NIM checks — NOT RUN; credentials are absent, and publication/sandbox live paths remain disabled.

### Status

P0 remains **IN PROGRESS**. Fail-closed behavior, patch provenance, no-side-effect live publication refusal, and CI are verified. Real E2B execution and exact-patch approval/fork publishing remain blockers.

## Continued audit — 2026-11-03 (Windows-to-Linux validation and credential-free verification)

### Risk findings

- **Medium — validation specs were host-platform dependent.** Windows generated `python` instead of the Linux sandbox's `python3` and retained backslash paths. Validator command construction and sandbox file paths now use POSIX conventions on every host. `TestBuildSpecUsesLinuxCommandsAndPathsOnEveryHost` covers the exact command and path. E2B remains disabled, so provider execution is not claimed.
- **Critical / High — no new open bypass found in this pass.** Missing and synthetic-present E2B keys both select the unavailable adapter; missing GitHub/model credentials select visible mocks; the live publication entry point and production REST client reject writes. Existing regressions cover no-side-effect GitHub refusal and sandbox refusal.

### Credential-free and mock verification

- `.env` is absent. Presence-only checks found `GITHUB_TOKEN`, `GITHUB_ALLOWED_REPOS`, `ENABLE_LIVE_AUTOPR`, `NIM_API_KEY`, `JINA_API_KEY`, `GEMINI_API_KEY`, `E2B_API_KEY`, `ARGUS_RUN_LIVE_NIM_TESTS`, and checked optional runtime settings unset. No secret values were read or emitted.
- Credential-free tests: `go test ./config ./services ./agents ./tools -count=1` — PASS; `go test ./... -count=1` — PASS.
- Mock-backed checks: missing/present-but-disabled adapter tests passed in the Go suite; offline sample workflow — PASS, all adapters labelled mock, `sandbox=unavailable`, zero PRs, two candidates demoted.
- Static checks: `go vet ./...` — PASS; changed-file `gofmt -l` — no output; `git diff --check` — PASS with Windows line-ending warnings; `go build ./...` — exit 0 with restricted module stat-cache warning.
- Local `go test -race ./...` — BLOCKED because `CGO_ENABLED=0`. Run [#55](https://github.com/saketh1125/Argus/actions/runs/41666072125) passed CI on pushed commit `69c2d83`, before the current local portability change.
- After commit `b79abba` was pushed, CI runs [#56](https://github.com/saketh1125/Argus/actions/runs/41668264467) and [#57](https://github.com/saketh1125/Argus/actions/runs/41668279346) both passed formatting, `go test ./...`, vet, race tests, native build and Windows AMD64 cross-build.
- Live E2B, NIM and GitHub publication were not run. E2B requires `E2B_API_KEY` only after an adapter is implemented and explicit authorization is given. NIM requires `NIM_API_KEY` and `ARGUS_RUN_LIVE_NIM_TESTS=true`. GitHub write credentials are not useful until the required approval/fork protections are implemented; writes remain rejected.

### Status

P0 remains **IN PROGRESS**. The Windows-to-Linux command-generation defect is fixed and passed CI. The genuine isolated provider, live sandbox lifecycle evidence, exact-patch approval and fork-based publishing remain unverified or blocked.

### Goal State completion estimate

**86%** by equal-weight Phase 0 acceptance-row scoring: 15 of 18 requirements are VERIFIED, 1 is PARTIALLY VERIFIED (counted as one half), and 2 are BLOCKED. This checklist percentage does not mean the phase exit gate is met; the live isolated-provider requirement and publication approval/fork workflow remain substantial blockers.

## Continued audit — 2026-11-03 (dashboard launcher trust boundary)

### High-priority finding and resolution

- **High — unauthenticated launcher POST allowed cross-site analysis starts.** The loopback dashboard accepted form submissions without an Origin/Host check or CSRF token. A malicious site could trigger analysis against a local path and influence service endpoints; with configured providers, this could expose local source or consume provider resources.
- /start now requires an exact loopback Host and same-origin match on the active port, a cryptographically random one-time token submitted in the POST body, URL-encoded form content, and a 16 KiB body cap. The form accepts only one launch, including under concurrent submissions. The token-bearing page uses Cache-Control: no-store.
- Removed the unused GitHub and disabled E2B API-key fields from the launcher. The dashboard no longer asks users to submit credentials for disabled integrations. README, launcher labels, KPI text, and Go comments now describe validation as unavailable and live writes as disabled.

### Verification

- Focused launcher regressions — PASS: cross-site Origin, rebound Host, wrong port, missing/query-only/replayed token, oversized form, concurrent submission, and no-store token page.
- Full go test ./... -count=1 — PASS; go vet ./... — PASS; go build ./... — exit 0 with the restricted module stat-cache warning.
- Offline sample workflow — PASS: all integrations labelled mock/unavailable, 3 candidates, zero PRs, 2 demoted.
- go test -race ./... — BLOCKED locally because CGO_ENABLED=0. GitHub Actions run [#61](https://github.com/saketh1125/Argus/actions/runs/41671901691) passed formatting, tests, vet, race test, native build, and Windows AMD64 cross-build on `77ccd1a`.
- git diff --check — PASS with Windows line-ending warnings. No credentials or live services were used.

### Current estimate and status

Current estimate: **87%** by equal-weight checklist scoring: 16 of 19 rows VERIFIED, 1 PARTIALLY VERIFIED (counted as one half), and 2 BLOCKED. The launcher change passed CI #61. E2B remains unavailable; exact-patch approval and fork-based publication remain incomplete. Phase 0 exit gate is not met.

## Continued audit — 2026-11-03

### Additional findings addressed

- **High — validator trusted `Success=true` alone.** It now requires aggregate and per-command zero exits, no timeout, a completed result for every requested command, and a non-cancelled context. Results are capped at 4 MiB and stored validation text at 64 KiB. Adversarial tests cover missing command counts, hidden earlier command failure, nonzero exit, timeout, explicit failure, cancellation and oversized output.
- Sandbox result output is capped at 4 MiB; persisted validation text and failure details are separately capped to prevent unbounded report growth.
- **High — direct `githubREST` writes could bypass `PRAgent`.** The production REST client now refuses branch, content, PR and issue writes before making a request. Its only write-enabled seam exists in package tests for testing stale-state and conflict safeguards.
- **High — host Git clone inherited execution configuration.** Clone URLs are parsed and normalized to canonical GitHub HTTPS URLs; ambient Git config, helper/path overrides, credential helpers and LFS smudge are disabled; cloning has a 2-minute deadline.
- Clone hook execution is disabled through a unique empty temporary hooks directory; this avoids relying on a platform-specific null-device path.
- **High — local mock copy followed symlinks.** `copyDir` now refuses symlinks and special files. The Windows test skips because creating a symlink requires an unavailable privilege; the test is mandatory on Linux and CI run #39 passed it.

### Verification for this continuation

- Targeted validator, GitHub write-path and clone-boundary tests — PASS.
- Full `go test ./...` — PASS.
- `go vet ./...` — PASS.
- `go build ./...` — exit 0 with the same restricted module stat-cache warning.
- Offline mock workflow — PASS; mock adapters visible, sandbox unavailable, zero PRs and two candidates demoted.
- Live sandbox/publication tests — NOT RUN. Windows symlink regression — SKIPPED locally; Linux CI run [#39](https://github.com/saketh1125/Argus/actions/runs/41660865916) passed the mandatory symlink test.
- Continuation commits `8a88254`, `6c88dbc`, and `f22c54e` are pushed. GitHub Actions run [#41](https://github.com/saketh1125/Argus/actions/runs/41662158674) passed all configured CI jobs on `f22c54e`.
- Documentation checkpoint `d9b82a2` is pushed; GitHub Actions run [#45](https://github.com/saketh1125/Argus/actions/runs/41662912698) passed all configured checks. PR #2 remains open, draft and unmerged.
- Documentation checkpoint `e5c67fa` is pushed; GitHub Actions run [#47](https://github.com/saketh1125/Argus/actions/runs/41663259463) passed all configured checks.
- Validation-provenance implementation `ee0c9c8` passed run [#49](https://github.com/saketh1125/Argus/actions/runs/41665041546); documentation checkpoint `53ccad0` passed run [#51](https://github.com/saketh1125/Argus/actions/runs/41665401475).

### Validation provenance correction

- **Medium — generated PR text overstated validation.** PR bodies hardcoded a generic sandbox-pass claim and issues did not identify the sandbox that produced a result. The validator now clears model-provided validation metadata, records the actual attempted provider and its live/mock mode, and successful-only patch eligibility requires live isolated-provider provenance. PR and failure-summary text show actual provider state. Run [#49](https://github.com/saketh1125/Argus/actions/runs/41665041546) passed all configured CI checks on `ee0c9c8`.
- E2B client research: the [official E2B project quickstart](https://github.com/e2b-dev/E2B#run-your-first-sandbox) documents JavaScript/Python SDKs; the Go SDK examined during this audit explicitly describes itself as [unofficial and community-maintained](https://github.com/eric642/e2b-go-sdk#e2b-go-sdk). No third-party runtime dependency was added without review. The Go client choice remains open, and live verification still requires explicit authorization and `E2B_API_KEY`.
- Focused provenance and truthful-reporting tests — PASS.
- Full `go test ./...` — PASS; `go vet ./...` — PASS; `go build ./...` — exit 0 with the restricted module stat-cache warning; `git diff --check` — PASS with Windows line-ending warnings.

Date: 2026-11-03
Branch: `codex/pr-patch-safety`
Baseline commit: `0089d0779c00f53c4f45acfd2437e093a2cbd484`
Checkpoint commit: `0c57dc7` (`Fail closed when isolated validation is unavailable`)
Status commit: `996d521` (`Record Argus Phase 0 checkpoint status`)
Pull request: [#2 — Harden Argus validated GitHub patch pipeline](https://github.com/saketh1125/Argus/pull/2)

## Changes

- Removed the local host command executor. When E2B is absent or mock mode is selected, the sandbox reports `unavailable` and returns an explicit validation-unavailable error.
- Removed model-generated test commands from the validator; it uses its deterministic syntax command list only.
- Added tests that ensure mock mode and missing E2B select the unavailable adapter and that the unavailable adapter refuses files and commands.
- Added GitHub Actions CI for changed-file formatting, tests, vet, race tests, native build and Windows AMD64 cross-build. Formatting is scoped to changed files because unrelated Go files in the existing repository are not gofmt-clean.
- Updated `EXECUTION_PLAN.md` with the baseline, current Phase 0 status and next actions.

## Files affected

- `.github/workflows/ci.yml`
- `agents/validator.go`
- `agents/validator_test.go`
- `main.go`
- `services/factory.go`
- `services/factory_test.go`
- `services/sandbox.go`
- `services/sandbox_test.go`
- `EXECUTION_PLAN.md`
- `SPRINT_REPORT.md`
- `EXECUTION_LOG.md`

## Verification

- `go test ./...` — PASS on Windows/AMD64, Go 1.27.3.
- `go vet ./...` — PASS.
- `go build ./...` — PASS.
- `gofmt -l` on changed Go files — PASS. A repository-wide scan found unrelated unformatted files; they were left untouched.
- `go test -race ./...` — NOT RUN: Go reports `-race requires cgo; enable cgo by setting CGO_ENABLED=1`; this environment has `CGO_ENABLED=0` and no discovered `gcc` executable.
- `git diff --check` — PASS; Git emitted line-ending conversion warnings for modified Go files.
- `FORCE_MOCK=true go run . --repo ./testdata/sample-repo --no-dashboard` — PASS. The run identified 3 candidate issues, raised 0 PRs, reported `sandbox=unavailable`, and demoted 2 fixes.
- GitHub Actions run [#27](https://github.com/saketh1125/Argus/actions/runs/41651669380) — PASS on commit `996d521`: changed-file formatting, `go test ./...`, `go vet ./...`, `go test -race ./...`, native build and Windows AMD64 cross-build all completed successfully.

## External state

- PR #2 remains open and draft on `codex/pr-patch-safety`, targeting `main`; its head is `996d521` and it has no reviews.
- Commits `0c57dc7` and `996d521` are pushed to `origin/codex/pr-patch-safety`.
- The working tree is clean.

## Limitations and open Phase 0 work

- E2B has not been exercised, and its current API compatibility, cleanup reporting and resource limits remain unverified.
- Local race testing is unavailable without CGO and a compiler; GitHub Actions supplied successful race-test evidence.
- Additional adversarial tests for interruption, persistence failures and sandbox/provider failure recovery remain open.
- Phase 0 is not complete. Do not start Phase 1 until the execution plan's P0 acceptance criteria and exit gate have evidence.

## Next task

Finish Phase 0: verify the isolated provider or keep validation unavailable, complete adversarial safety coverage, and reassess every P0 exit criterion.
