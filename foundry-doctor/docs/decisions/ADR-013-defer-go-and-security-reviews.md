# ADR-013: Go principal and security reviews are deferred to the last phase

Status: Accepted (project owner decision, 2026-10-12)
Date: 2026-10-12

## Context

`CLAUDE.md` and the `implement-prd-phase` skill required independent reviews from `foundry-lead`, `azure-black-belt`,
`go-principal-engineer` (when Go production code changed) and `security-reviewer` (when Azure, data, reports or adapters are touched).
During Phase 1 the owner asked to stop the `go-principal-engineer` and `security-reviewer` reviews and to run them
only in the last project phase (Phase 9).

## Decision

1. Phases 0 to 8 run the `foundry-lead` and `azure-black-belt` reviews only.
2. `go-principal-engineer` and `security-reviewer` run once, in Phase 9, over the whole code base.
3. Each phase hand-off lists both reviews as **deferred by ADR-013**, never as met.
4. The global DoD item "no unresolved critical/high security findings" (PRD section 22) cannot be shown until the Phase 9 security review.
   Every phase hand-off says so. Until then the evidence is the implementers' own tests (canary secrets, path and symlink confinement,
   hostile-input fuzzing, the secret scan), which are not an independent review.
5. Phase 0 had both reviews (they found blocking issues, all fixed). Phase 1 has neither.

## Consequences

- Risk: defects in redaction, path handling, the Bicep subprocess, report output and CI workflows can ship in Phases 1 to 8 without an independent
  review, and later phases (Azure access in Phase 2, data-plane probes in Phase 3, annotation in Phase 6, LLM in Phase 9) build on that code.
  Review findings in Phase 9 may force changes across many packages.
- Mitigation: keep the secret scan, `-race`, fuzz targets and canary tests in every phase; do not publish a release (tag) before the Phase 9 reviews run.
- A phase that adds Azure credentials or an LLM data flow (Phases 2, 3 and 9) should be reconsidered for an earlier security review; this ADR is
  revisited then.
