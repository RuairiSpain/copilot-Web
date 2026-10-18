# Phase 3 review: Foundry lead

Reviewer: `foundry-lead` (independent; did not write the code). Date: 2026-10-17. Round 1. Verdict: **REJECT**.
Scope: Phase 3 runtime diagnosis (FND-RUN-001..007), `docs/runtime.md`, `docs/permissions-matrix.md`, the Phase 3 hand-off, ADR-022 obligations.

This file is a concise summary recorded by the catalogue owner from the verdict relayed by the parent session; the full report is held by the parent session. Remediation status is tracked in `docs/development/hand-offs/phase-3.md`. Status of every item below: **in remediation, fix loop 1**. Nothing is closed until the reviewer confirms it.

## Blocking findings

1. **B1 FND-RUN-005 wrong response shapes.** The Foundry v1 data plane shapes did not match the specification: the connections list returns `value` (not `data`); an agent carries its versions under `versions.latest`; `limit` is at most 100; list responses page (`has_more`, `nextLink`); an empty status must not be read as healthy.
2. **B2 FND-RUN-007 cannot pass or fail.** The Log Analytics query was addressed with the workspace ARM ID instead of the workspace GUID (`customerId`); the result table was not read as `PrimaryResult`; the query endpoint was the `api.loganalytics.io` host where the reviewer expects the `.azure.com` endpoint.
3. **B3 FND-RUN-004 malformed metric queries and misattributed evidence.** `Total` was requested where the reviewer expects `Maximum`/`Average`; no `*` split on the dimension used for attribution; the `StatusCode` value domain is unverified; the `timespan` was not an explicit start/end pair.
4. **B4 FND-RUN-001 false pass, false fail, wrong principal.** Cosmos DB false pass (role definition not resolved, scope not checked); Storage false fail; user-assigned identity principal not used. The reviewer's exact wording is held by the parent session; the catalogue owner's reading is that account-level roles apply to the system-assigned identity only.
5. **B5 FND-RUN-002 fails on public endpoints; probe 2 not wired.** A public IP answer was a FAIL even for a dependency that is not private-endpoint-backed; the control-plane DNS link probe (probe 2) is described but not wired.
6. **B6 FND-RUN-006 impossible fail path, false fail, invented metadata.** An impossible fail path, a false fail, and invented metadata (the catalogue owner's reading: `errorCode`, which the indexer result does not carry, and connection metadata index keys that no source documents).
7. **B7 ADR-022 obligations undisclosed.** The hand-off did not say that FND-IDN-002 and FND-NET-002 live checks and the FND-RUN-003 scope policy were not delivered, and did not keep the v1 release gate.

## Non-blocking findings

N1-N9: recorded in the reviewer report held by the parent session; each is tracked as "in remediation, fix loop 1" in the hand-off.

## Status

All of B1-B7 and N1-N9: **in remediation, fix loop 1** (2026-10-17). Catalogue changes made by the catalogue owner in loop 1: FND-RUN-001..007 evidence, limitations, tests, `apiVersions` and `lastVerified` (each `version` raised by one). The Go behaviour changes are owned by the Go agent. The ADR-022 live checks FND-IDN-002 and FND-NET-002 are not delivered in Phase 3 (see the hand-off).


## Round 2 (2026-10-18)

Reviewer: `foundry-lead`. Verdict: **ACCEPT with conditions**; four conditions are **blocking**. This section is a summary recorded by the catalogue owner from the verdict relayed by the parent session; the full report is held by the parent session. Status below is 'fixed in loop 2, pending re-review': the Go agent, the catalogue or the documents were changed; the reviewer has not confirmed any item.

### Blocking conditions

1. **C1 FND-RUN-001 false pass with no resource ID.** A resource-backed connection with no resolvable ARM resource ID was not checked and could pass. Fixed in loop 2, pending re-review: the rule reports uncertain, 'effective access was not checked' (catalogue FND-RUN-001 v4).
2. **C2 FND-RUN-002 unreadable account masked as by-design.** An unreadable Foundry account network read was reported as the by-design 'nothing in scope'. Fixed in loop 2, pending re-review: uncertain or skipped (FND-RUN-002 v4).
3. **C3 FND-RUN-005 agent name assumption.** The agent name was assumed to be the azure.yaml service name. Fixed in loop 2, pending re-review: the name is resolved from the agent definition (azd deploys with `agentDef.Name`); otherwise 'not listed' is uncertain and names the assumption (FND-RUN-005 v4).
4. **C4 Hand-off and ADR gaps.** (a) recorded data-plane fixtures, (b) integration tests and timeout, throttling and partial-permission tests, (c) FND-IDN-002 and (d) FND-NET-002 live checks left allocated to Phase 3 but not delivered. Fixed in loop 2, pending re-review: hand-off DoD rows, the ADR-021 (d) Phase 3 clause, and ADR-023 (implementing phase 8, corrected reasons, v1 release gate kept).

### Non-blocking findings

N1 to N11: recorded in the reviewer report held by the parent session; each is tracked as 'fixed in loop 2, pending re-review' in `docs/development/hand-offs/phase-3.md`.
