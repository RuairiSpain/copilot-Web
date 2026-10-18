# Phase 2 review: Foundry lead

Reviewer: `foundry-lead` (independent; did not write the code). Date: 2026-10-17. Round 1. Verdict: **REJECT**.
Scope: Phase 2 Azure read-only adapters and deployment preflight (FND-DEP-001..012), `docs/permissions-matrix.md`, samples, workflow, ADRs.

This file is a concise summary kept in the repository; the full report was delivered to the parent session. Remediation status is tracked in `docs/development/hand-offs/phase-2.md` (all items fixed in round 1, pending re-review, except the documented exceptions and waivers).

## Blocking findings

1. **B1 What-if parameters.** The what-if request did not use the real template parameters; unresolved required parameters must not be guessed.
2. **B2 What-if and permission scope.** What-if needs deployment-level permissions, which contradicted the read-only role and the documentation.
3. **B3 FND-DEP-002 Registering.** A provider in the Registering state was treated as a pass.
4. **B4 Cross-resource-group scope coverage.** The scope policy did not cover the references that the DoD item names.
5. **B5 Reusable workflow.** The PRD requires a reusable (`workflow_call`) preflight workflow; only `workflow_dispatch` existed.
6. **B6 ADR and process.** Deviations from the PRD and ADR-006 were not recorded in an ADR, and the hand-off overstated status.

## Non-blocking findings

N1-N9: recorded in the reviewer report held by the parent session; each is tracked as "fixed in round 1, pending re-review" in the hand-off.

## Round 2 (2026-10-17)

Reviewer: `foundry-lead`. Verdict: **ACCEPT with conditions**. This is a summary recorded by the catalogue owner from the verdict relayed by the parent session; the full report is held by the parent session.

New blocking finding:

1. **N-B1 FND-DEP-002 false pass.** The rule could report a pass although supporting-provider set (a) and Microsoft.Bing are not evaluated.

The conditions relayed to the catalogue owner were: a concrete ADR-021 (e) exit criterion (raised by both reviewers), documentation of the known limitations, catalogue wording for FND-DEP-002, 008, 009 and 010, matrix corrections, and the hand-off update.

Status: **fixed in round 2, pending re-review.** FND-DEP-002 is `version: 3` and never passes in Phase 2 (uncertain notes); `docs/preflight.md` Limitations and the hand-off list it; ADR-021 (e) now names an owner rule, PRD phase and exit criterion per uncovered reference kind. Nothing is closed until the reviewer confirms it. The hand-off is `docs/development/hand-offs/phase-2.md`.

## Round 3 (2026-10-17)

Reviewer: `foundry-lead`. Verdict: **ACCEPT with conditions**, no blocking findings. This is a summary recorded by the catalogue owner from the verdict relayed by the parent session; the full report is held by the parent session.

Conditions and status (all **fixed in loop 3, pending confirmation**):

1. **C1 Phase allocation for scope-policy owner rules.** New ADR-022 (Accepted, 2026-10-17, decided by the project lead delegate): FND-RUN-003 stays Phase 3; FND-IDN-002 live part Phase 3; FND-NET-002 live part Phase 3; FND-SEC-005 Phase 8 (weakest fit, scope extended by the ADR). ADR-021 (e) now refers to it; `docs/decisions/README.md` updated.
2. **Cross-resource-group DoD item.** Stays PARTLY MET (what-if change IDs and the agent subnet only) and is a release gate for v1: it blocks the Phase 9 / v1 hand-off until each kind is covered by its owner phase with the positive and uncertain tests ADR-021 (e) requires.
3. **Documentation of by-design uncertain rules and exit codes.** `docs/preflight.md` lists DEP-002, DEP-008, DEP-009; `--strict` outranks exit 1 (ADR-010) and masks error findings; exit 0 does not mean ready.
4. **Stale catalogue wording.** FND-DEP-008 v4, FND-DEP-010 and FND-DEP-002 (see the Azure black belt record).
5. **OIDC CI.** Stays unmet until the workflow runs on GitHub; run it once in a test repository with `what-if` false and true and record the evidence.

Nothing is closed until the reviewer confirms it.
