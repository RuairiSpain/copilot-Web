# Phase 0 closure status

Date: 2026-10-06

This document closes the documentation/metadata cleanup that could be completed
in-tree for Phase 0, and states the remaining blocked items plainly.

## Inputs reviewed

- `docs/requirements/Foundry_Doctor_Implementation_PRD.md` sections 8, 18, 19, 20 and 22
- `docs/development/hand-offs/phase-0.md`
- `docs/decisions/ADR-013-phase-0-evidence-and-gate-contract.md`
- prior checkpoint findings recorded in the session-state hand-offs

## Remaining Phase 0 gaps

1. **XF provenance remains blocked.**
   - The repository still has no authoritative XF asset bundle for the PRD's 59
     XF-origin checks.
   - Evidence: `docs/xf-origin-inventory.md`.

2. **Strict final acceptance evidence remains blocked.**
   - Windows ARM64 cannot satisfy the race gate locally.
   - A successful current Linux CI run is still required.

3. **Tagged azd/bicep spike evidence remains blocked.**
   - The workflows are configured to run the pinned spikes on Ubuntu, but a
     successful current run is still separate evidence.

4. **Synthetic-provider compile/enumeration evidence remains blocked.**
   - The schema spike is configured, but it still does not prove provider
     generated ARM compilation/enumeration unless the pinned run succeeds.

5. **Release-artefact licence/NOTICE evidence remains blocked.**
   - The inventory and checks exist, but a successful release-oriented evidence
     pack is still needed.

## Work completed in this pass

### 1. V1 source verification refresh

Current published documentation was re-read for the V1-facing metadata noted in
`docs/research/phase-0-v1-evidence.md`, and the affected catalogue entries had
their current-source verification refreshed while keeping
`lastVerified: 2026-10-05`, which is the latest non-future date accepted by the
in-tree catalogue invariant.

### 2. Honest metadata downgrades

Two V1 rules still carried explicit unverified caveats in their own notes, so
their evidence confidence was downgraded from `certain` to `likely`:

- `FND-CFG-001`
- `FND-CFG-004`

No rule logic changed.

### 3. Evidence-backed overlap cleanup

The four ENV comparison rules were safe to de-provisionalise because they are
about sibling azd environment correlation, which the configured lower-level
tools do not evaluate:

- `FND-ENV-001`
- `FND-ENV-002`
- `FND-ENV-003`
- `FND-ENV-004`

For those four rules only:

- `overlap.provisional` was set to `false`
- every overlap tool state was completed as `searched-none`

The generated overlap matrix now reflects those four non-provisional V1 rules.

### 4. Linux CI readiness note

The main CI workflow already ran the strict race gate on `ubuntu-latest` and the
pinned azd/bicep spikes on `ubuntu-latest`. This pass added explicit workflow
comments documenting **why** Ubuntu is required:

- the race detector is unavailable on local Windows ARM64;
- the azd isolation spike depends on Linux `unshare`/network-namespace hardening.

## DoD audit against PRD section 8 and section 22

| Item | Status | Evidence |
|---|---|---|
| Every proposed rule has a verified source or is explicitly labelled product opinion | **partially satisfied, Phase 0 still open** | Current catalogue split remains generated in `docs/rule-catalog.md`; V1 verification refresh recorded in `docs/research/phase-0-v1-evidence.md`. |
| Every rule has a reuse/wrap/adapt/native/drop decision | **metadata present** | Generated catalogue and overlap docs remain in sync. |
| At least one real or representative Foundry project validates the azure.yaml-only path | **still blocked for final closure** | Workflow exists; successful current run still required. |
| Bicep source mapping proven or documented as deferred | **satisfied as deferred limitation** | Existing ADR/hand-off evidence unchanged. |
| No unresolved namespace conflict | **satisfied** | Existing ADR/hand-off evidence unchanged. |
| Revised MVP contains 35-40 high-value rules | **satisfied** | Generated `docs/rule-catalog.md` still shows 38-rule Phase 1 MVP set. |
| Synthetic-infrastructure schema spike run or explicitly recorded as not run | **still blocked for final closure** | Workflow configured; successful current run still required. |
| XF-origin coverage traceable, or asset absence kept open | **open by design** | `docs/xf-origin-inventory.md` keeps the gate fail-closed. |
| Licence evidence includes graph, scanner output and notice obligations for distributable artefacts | **still blocked for final closure** | Local mechanisms exist; current release-style evidence still required. |
| Two independent expert reviews completed | **historical evidence exists; current closure still open** | Prior reviews remain historical, not substitute final gate evidence. |
| No unresolved critical/high security findings | **not newly introduced here** | This pass changed only metadata/docs/workflow comments; final supported-host acceptance evidence is still pending. |
| Final strict acceptance run uses the repository strict gate with no skipped checks | **blocked** | Needs Linux CI success; cannot be satisfied locally on Windows ARM64. |

## Rules downgraded in this pass

| Rule | Change |
|---|---|
| `FND-CFG-001` | `evidence.confidence`: `certain` → `likely` |
| `FND-CFG-004` | `evidence.confidence`: `certain` → `likely` |

## Remaining blocked items

- authoritative XF source assets for the 59 claimed XF-origin checks;
- successful current strict CI evidence on Linux;
- successful current pinned azd/bicep spike evidence on Linux;
- successful release-artefact licence / NOTICE evidence.

## Verdict

This pass closes the **metadata hygiene** leftovers for V1 rule claims. It does
**not** close Phase 0 itself. Phase 0 remains open until the blocked evidence
items above are satisfied.
