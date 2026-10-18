# ADR-023: FND-IDN-002 and FND-NET-002 live checks move from Phase 3 to Phase 8

Status: Accepted
Date: 2026-10-18
Decided by: the project lead delegate (recorded by the catalogue owner from the Phase 3 gate round 2 conditions)

## Context

ADR-022 (2026-10-17) allocated the live parts of FND-IDN-002 (role-assignment scopes) and FND-NET-002 (private endpoint subnets) to
Phase 3. The Phase 3 hand-off then said they were not delivered, and kept the v1 release gate. Phase 3 gate round 2 (2026-10-18; Foundry lead
ACCEPT with blocking condition C4c and C4d) asked that an undelivered allocation is not left in its original phase: an audit is not a delivery.

## Decision

1. The live parts of FND-IDN-002 and FND-NET-002 are **not delivered in Phase 3**. ADR-022 is amended: their implementing phase is **Phase 8**
   (PRD section 16, "Foundry IQ, Search and APIM gateway deep validation"). PRD section 20 rule IDs and the offline (Phase 1) parts are unchanged.
2. **Why Phase 8, and how honest the fit is.** PRD section 16 (lines 678 to 741) builds on the Phase 3 probe framework ("Requires generic runtime probe
   framework from Phase 3"), validates Azure AI Search with metadata-only operations, and is the last phase that adds rules (IQ-001..012 and
   GW-001..006). Phases 4 to 7 (WAF reporting, graphs, annotated copies, cost) and Phase 9 (optional LLM text) add no resource probes. The fit is
   partial, not complete: section 16 names Search only. It does not name identity or network validation of Storage or Cosmos DB, and the Phase 8
   hand-off written so far does not list these rules. As in ADR-022 for FND-SEC-005, the Phase 8 scope is extended by this ADR for these two rules,
   and the Phase 8 hand-off must list them. If Phase 8 cannot carry them, a new dated ADR must pick another phase or waive them; silence does not.
3. **Corrected reasons for the deferral.**
   - FND-IDN-002: the property `properties.scope` of `Microsoft.Authorization/roleAssignments` **is verified** (2022-04-01 authorization spec,
     read for FND-RUN-001). The check is deferred for scope and priority (Phase 3 delivered the effective-access rule FND-RUN-001 and the other six
     runtime rules first), **not** because the path is unverified.
   - FND-NET-002: the private endpoint subnet property path (`properties.subnet.id` of `Microsoft.Network/privateEndpoints`) is **still unverified**
     in the catalogue evidence (only shown in the fix example). CLAUDE.md forbids implementing an unverified property path, so the live check needs
     that path read from a primary source first.
4. **The v1 release gate stays.** The Phase 2 DoD item "cross-resource-group references follow an explicit scope policy" stays PARTLY MET and blocks
   the Phase 9 / v1 hand-off until each of the four reference kinds is covered or waived by a new dated ADR (ADR-022).
5. **Test obligations (ADR-021 e).** The change that implements either check extends `scopeAllowed` to the kind and adds a **positive** test
   (a reference outside the target resource group and every approved scope is a finding) and an **uncertain** test (a reference built from an
   unresolved expression, or an unreadable resource).

## Consequences

- The Phase 3 hand-off lists both checks as not delivered, carried to Phase 8, with the v1 gate.
- The Phase 8 hand-off and the Phase 9 audit list FND-IDN-002 (live), FND-NET-002 (live) and FND-SEC-005 against ADR-022 and this ADR.
- The catalogue `phases` field of both rules is changed by the change that implements the live check, not by this ADR.
