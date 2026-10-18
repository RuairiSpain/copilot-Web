# ADR-022: Phase allocation for scope-policy owner rules

Status: Accepted
Date: 2026-10-17
Decided by: the project lead delegate

## Context

ADR-021 (e) records that the Phase 2 DoD item "cross-resource-group references follow an explicit scope policy" is met only for the resource IDs
of what-if predicted changes (FND-DEP-008) and the agent network-injection subnet (FND-DEP-011). It names four more reference kinds, each with
one owner rule. Two owners had no PRD section 20 phase able to carry a live check (FND-NET-002 is Phase 1 only, FND-SEC-005 is "Later") and one
had a Phase 2 slot the Phase 2 scope does not use (FND-IDN-002 is "1/2"). PRD section 20 allocation does not change without an ADR, so ADR-021
left the question for the lead. The Phase 2 gate round 3 (both reviewers ACCEPT with conditions) asked for the decision.

## Decision

The offline (Phase 1) part of each rule is unchanged. This ADR allocates only the live check that extends the scope policy.

| Reference kind | Owner rule | PRD s20 before | Allocation of the live check | Why it fits |
|---|---|---|---|---|
| Connection targets | FND-RUN-003 | 3 | **Phase 3** (unchanged) | PRD s11 scope: "Check capability host/project connections"; it reads `properties.target` through control-plane connection metadata. |
| Role-assignment scopes | FND-IDN-002 | 1/2 | **Phase 3** (live part moves from 2 to 3) | PRD s11 scope: "Resolve effective access for project identity on Search, Storage and Cosmos"; the Phase 3 tool row reads "role assignments". The live part needs runtime RBAC evidence (effective permissions, deny assignments, inheritance) that the Phase 2 preflight scope (DEP-001..012) does not contain. |
| Private endpoint subnets | FND-NET-002 | 1 | **Phase 3** (live part added) | PRD s11 names "private endpoint DNS" and network vantage-point probes; the live read of storage, Cosmos DB and Search network state and their private endpoints belongs with those probes. |
| Key Vault references | FND-SEC-005 | Later | **Phase 8** | Phase 8 (PRD s16) builds on the Phase 3 probe framework and is the last phase that adds rules (IQ and GW). It is the nearest phase that can carry an otherwise unallocated "Later" rule. It is a weaker fit than the other three: PRD s16 does not mention Key Vault, so the Phase 8 scope is extended by this ADR for this one rule. |

Consequences for the plan and the catalogue:

- The PRD s20 "Phase" column for FND-IDN-002 (live part), FND-NET-002 (live part) and FND-SEC-005 is read together with this ADR. The catalogue
  `phases` field of those rules is changed by the change that implements the live check, not by this ADR.
- The Phase 3 and Phase 8 hand-offs list these rules in their scope.
- Phase 3 gets two more live rules than PRD s11 lists (RUN-001..007). The Phase 3 reviewers must see them in the Phase 3 scope, and a Phase 3 hand-off
  that does not deliver them must say so and keep the Phase 9 gate below.
- The exit criterion in ADR-021 (e) stands: the scope policy (`internal/preflight` `scopeAllowed`, `--approved-scope` match on path boundaries) is
  extended to the kind in the change that implements the owner rule's check of the named property path. That change adds a **positive** test
  (reference outside both the target resource group and an approved scope is a finding) and an **uncertain** test (reference built from an
  unresolved expression). A property path that the catalogue has not verified is not implemented (CLAUDE.md).

## Phase 2 DoD item and v1 release gate

The Phase 2 DoD item "cross-resource-group references follow an explicit scope policy" stays **PARTLY MET**: what-if predicted change IDs and the
agent network-injection subnet only. It is a **release gate for v1**: the item blocks the Phase 9 / v1 hand-off until each of the four kinds is
covered by its owner phase with the positive and uncertain tests that ADR-021 (e) requires. A kind that is not covered by then needs a new ADR that
waives it with a dated reason; silence does not waive it.

## Consequences

- ADR-021 (e) refers to this ADR instead of "when the lead decides".
- The Phase 2 hand-off DoD table shows the item as partly met and as a release gate.
- Phase 9 / v1 hand-off must audit the four kinds against this ADR.

## Amendment (2026-10-18)

The Phase 3 live checks of FND-IDN-002 and FND-NET-002 were not delivered in Phase 3. ADR-023 moves their implementation to Phase 8, corrects the stated reason (FND-IDN-002 `properties.scope` is verified; only the FND-NET-002 subnet path is unverified), and keeps the v1 release gate. The table above shows the original allocation; ADR-023 supersedes it for those two rows.
