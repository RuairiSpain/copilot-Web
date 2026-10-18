# Phase 7 review: Azure black belt (round 3, final)

Reviewer: `azure-black-belt` (independent). Date: 2026-10-18. Round 3.
Inputs: round 2 review, the "Loop 2" section of `phase-7-fixlog-loop1.md`, and the code in `/mnt/workspace/working/canon/foundry-doctor`.
Environment: there is no Go toolchain, so nothing was built, vetted or tested. This review is based on reading only. The maintainer must
run `go build ./...`, `go vet ./...`, `go test ./...` and `scripts/claude/verify-phase.sh --strict`, as the hand-off and ADR-025 already state.

## Round 2 blocker

| Id | Status | Evidence |
|---|---|---|
| B10 `Fprintln` redundant newline (vet failure under `go test`) | **CLOSED** | `internal/cost/report/report.go:213` is now `fmt.Fprint(w, "\n## Comparisons\n\n")`. The old call wrote the string followed by a newline, and the new call writes the same characters. `testdata/markdown.golden` still ends `- ptu (T): PTU`, blank line, `## Comparisons`, blank line, `- dev -> prod: x`, which is what the new code writes. The golden file was not touched. A scan of `internal/cost`, `internal/rules/cost`, `internal/app/cost.go` and `cmd/foundry-doctor/cost.go` found no other `Println`/`Fprintln` with a trailing `\n`. |

## Regression check

Since the round 2 review, the only in-scope file that changed is `internal/cost/report/report.go`. The scope covers
`internal/cost/**`, `internal/rules/cost/**`, `internal/app/cost*.go`, `cmd/foundry-doctor/cost.go`, `internal/armmodel`, `pkg/sdk`,
`rules/catalog/cost`, `docs/cost.md` and ADR-025. File modification times show no other change. All findings that round 2 recorded
as closed (B1–B9, N1–N3, N5, N6, N8–N12) therefore still hold, and the round 2 compile-correctness reading still applies.

## Open non-blocking items (carried, not gating)

- N4 (no report-header price date) and N7 (contactRoles scope, opinion label) are only partly fixed.
- N13–N17 from round 2:
  - N13: looped pay-per-token deployments mark the estimate incomplete.
  - N14: a database could be priced twice when it also has a throughputSettings child.
  - N15: Cosmos estimates need a parameter file; this should be documented.
  - N16: APIM `Developer Unit` and `Standard Unit` are not in the recorded fixtures.
  - N17: the recorded responses should be re-recorded with a direct API call.
- ADR-025 deferrals stand: the Retail Prices origin in the `internal/azure` allow-list, control-plane budgets, and Advisor.

## Verdict

All blocking findings from rounds 1 and 2 are closed. The Phase 7 DoD items are met in code and disclosed honestly as not yet
executed. This verdict depends on the maintainer's gate run, which ADR-025 already requires; the orchestrator has accepted that
treatment for this loop.

VERDICT: APPROVE
