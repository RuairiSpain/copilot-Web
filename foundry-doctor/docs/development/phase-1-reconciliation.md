# Phase 1 reconciliation: two implementations

On 2026-10-15 this branch was merged with `main`. Two Phase 1 implementations existed:

| | Archive branch | `main` |
|---|---|---|
| Where | `claude/foundry-doctor-phase-1-core-archive` (commit `2b1c135`) | `main` (PR #18, commits `3193cb8` to `c91d158`) |
| Content | Phase 1 only: `pkg/sdk`, `internal/model`, `internal/engine`, 37 native rules, CLI (`doctor`, `explain`, `compare`), reporters, Bicep adapter, PSRule adapter (fakes only) | Phases 1 to 9: rules for all groups, runtime probes, WAF assessment, graph outputs, annotate, cost, LLM explain, release rehearsal |
| Relation | Both start from commit `d61c678`. `main` deleted or rewrote the engine files of the archive (`internal/model`, `internal/engine`, `internal/rules/executor.go`, `rule.go`) and reused the package names with different APIs. | |

A line-by-line merge does not compile, so this merge takes `main`'s code (`cmd/`, `internal/`, `pkg/`, `go.mod`, `go.sum`) and keeps the
archive's decision records and review reports. The archive code is not deleted; it is on the archive branch.

Kept from this branch: `ADR-019` (Go principal and security reviews deferred to Phase 9), the two independent review reports in
`docs/development/reviews/` (they examined the archived code and are a checklist for `main`'s code), and the `implement-prd-phase` skill and
`CLAUDE.md` changes that follow from ADR-019. Dropped: the archive's ADR-015 (docs, samples, end-to-end tests and PSRule deferred), because
`main` already contains docs, samples, end-to-end tests and an adapter layer; the archive's CLI doc and adapter doc.

Known state after the merge (checked in this sandbox on 2026-10-15):

- `go build ./...` and `go vet ./...` pass. `staticcheck` passes.
- `go test ./...` fails in 3 packages that also fail on a clean checkout of `main` here: `TestAnnotate`, `TestAnnotateRejectsUnsafeNonReviewOut` and
  `TestAnnotateReviewWritesCopy` (`cmd/foundry-doctor`, `internal/app`) and `TestLinkStateAndTimeoutResolver` (`internal/runtime/network`); the
  `test/spikes/bicep` process test also failed in the gate run. Cause not investigated.
- `git diff --check` fails on trailing whitespace in `THIRD_PARTY_NOTICES.md` (from `main`).
- No independent review of `main`'s code has been done. The reviews recorded here covered the archived code only.
