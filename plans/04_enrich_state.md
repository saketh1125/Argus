# Unit 04 — (OPTIONAL stretch) Enrich dashboard state with PR URLs

Owner: executing model. Prerequisites: units `00`, `01`, `02`, `03` merged &
green AND the orchestrator has reviewed the result and explicitly approved
running this. **Default action: do nothing.** This unit risks the Reporter
interface contract and the existing test; only run if the orchestrator asks.

## Goal

Today the dashboard only knows `PRsRaised int` (a count). The pipeline actually
creates PRs with real GitHub URLs (`models.PRResult{Number, URL, Branch}`). It
would be a much better UX to show live PR links in the dashboard. This unit
adds the minimal plumbing to surface PR rows in the panel — WITHOUT breaking the
`models.Reporter` interface, `main.go`, or the existing tests.

## Why this is risky

1. `models.Reporter` is a frozen interface used by every agent. Adding a REQUIRED
   method would break `NoopReporter` and every test that constructs a Reporter.
   => The new method MUST have a default no-op implementation on `NoopReporter`
   and MUST NOT be added to the `Reporter` interface at all if avoidable.
2. `main.go` already calls `rep.SetPRsRaised(len(report.PRsRaised))` and has the
   full `report *models.RunReport`. The cleanest, lowest-risk path is to NOT
   touch the Reporter interface at all and instead reuse `SetPRsRaised` is
   insufficient (it only takes an int).
3. The existing dashboard test `dashboard_test.go` checks `s.prsRaised` and
   `recentIssues` and `agents`; any new state must not perturb those numbers.

## Chosen design (lowest risk)

Do **not** change `models.Reporter`. Instead, add a **new, separate, optional
method on `*Dashboard` only** (not on the interface) and have `main.go` call it
when it has the PRs. This keeps the interface frozen and `NoopReporter` untouched.

Concretely:

- Add to `Dashboard` a new state field `prs []prRow` (guarded by the existing
  `mu`) and a method:
  ```go
  // SetPRs records the list of pull requests raised so far, for live display.
  // Optional: main.go calls it with report.PRsRaised; NoopReporter ignores it.
  func (d *Dashboard) SetPRs(prs []models.PRResult)
  ```
- `prRow` is a small trimmed view: `{Number int, URL, Branch, Title string}` —
  Title can be derived (e.g. `fmt.Sprintf("Fix %s %s", iss.Severity, iss.BugType)`)
  but `PRResult.Issue` is an `*Issue` so we CAN read it. Keep the row minimal
  and never hold the whole Issue to avoid state bloat.
- Update `panelView` to include `PRs []prRow` and `snapshot()` to copy it under
  the read lock.
- Update `panelTemplate` (unit 03 territory, but owned here for this feature) to
  render a small PRs panel: a list of `<a href="{{.URL}}" target="_blank"
  rel="noreferrer">PR #{{.Number}}</a> · {{.Branch}}` rows, or an empty state.
  Place it as a new card or extend the KPI tile for "PRs raised" to expand into a
  popover/list — your call, keep it unobtrusive.
- In `main.go`, after `report, err := pr.Run(...)`, before `printReport`, add:
  ```go
  if dash != nil {
      dash.SetPRs(report.PRsRaised)
  }
  ```
  Guard for `dash != nil` (CLI mode has nil dash). Keep `SetPRsRaised` calls from
  the pr_agent as-is (they drive the live counter during the run, which is
  different from the final list). Actually — `SetPRsRaised` is called incrementally
  in `agents/pr_agent.go` and `SetPRs` here is called once at the end with the
  final list. Both can coexist; the panel shows both (a live count + a final list
  appears at the end). Make sure the final count and the list length match.

## Files you may edit

- `dashboard/dashboard.go` — add `prs []prRow` to `state`, add `SetPRs`, update
  `New` if needed.
- `dashboard/templates.go` — add `PRs []prRow` to `panelView`, copy in `snapshot`,
  render in `panelTemplate`.
- `main.go` — one guarded `dash.SetPRs(report.PRsRaised)` call.
- **Do NOT edit** `models/reporter.go`, `models/types.go`, `agents/pr_agent.go`,
  or `dashboard/launcher.go`.

## Hard constraints / fallback

- `models.Reporter` interface stays byte-for-byte unchanged.
- `NoopReporter` compiles with zero edits (it already satisfies Reporter; this
  unit adds a method on `*Dashboard` only, which NoopReporter is not).
- `dashboard_test.go`'s assertions (`s.prsRaised == 5`, etc.) must still pass —
  this unit adds `prs` slices, not changing `prsRaised`.
- If at ANY point `go build ./...` or `go test ./...` fails because of this
  change, **revert it entirely** and report "Unit 04 skipped — reverted, build
  green". Do not leave a broken tree.

## Verification

1. `cd /home/saketh/Code-Review && go build ./...` → exit 0.
2. `cd /home/saketh/Code-Review && go test ./...` → all `ok`, exit 0.
3. `cd /home/saketh/Code-Review && go vet ./...` → clean.
4. Manual smoke (mock): `FORCE_MOCK=true go run . --web` with
   `./testdata/sample-repo`, confirm the dashboard ends with a small list of PR
   rows (mock GitHub returns deterministic PR URLs — check `tools/github.go` mock
   to confirm the URL format) and the panel doesn't crash when the run completes.
   Report what you see or "skipped — no runnable env".

## Report back

- Whether you completed it or reverted it (say one of: "completed, build+test
  green" or "reverted — reason").
- The exact additions to `main.go`, `dashboard.go`, `templates.go`.
- The smoke result or skipped note.
- A note on whether the live `SetPRsRaised` counter and the final `SetPRs` list
  can momentarily disagree (they can — the counter is live during the run, the
  list lands at the end) and how the panel handles that (e.g. show the list only
  when `Done`, show the counter otherwise).
