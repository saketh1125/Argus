# Sprint Report — Phase 0 Safety Checkpoint

## Follow-up audit — 2026-11-03

Base: `6e7dc0033438d12c03eed016640f3e69278ecf3d` (`Record successful Argus CI checkpoint`); PR #2 open, draft and unmerged. This follow-up is being prepared for the same branch; its commit and CI result will be recorded after push.

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
- `go test -race ./...` — BLOCKED locally: `CGO_ENABLED=0`; Go requires CGO. Prior GitHub Actions run #29 passed race tests on the previous head; new code awaits CI.
- `FORCE_MOCK=true go run . --repo ./testdata/sample-repo --no-dashboard` — PASS. Banner identified all mocks and `sandbox=unavailable`; 3 candidates, 0 PRs, 2 demoted. The summary URL is an in-memory mock artifact.
- `git diff --check` — PASS (line-ending conversion warnings only).
- Live E2B, GitHub publication and NIM checks — NOT RUN; credentials are absent, and publication/sandbox live paths remain disabled.

### Status

P0 remains **IN PROGRESS**. Fail-closed behavior, patch provenance, and no-side-effect live publication refusal are verified. Real E2B execution and exact-patch approval/fork publishing remain blockers. CI for the follow-up commit is pending.

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
