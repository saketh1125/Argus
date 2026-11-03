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
