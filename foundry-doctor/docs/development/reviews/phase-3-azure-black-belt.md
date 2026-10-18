# Phase 3 review: Azure black belt

Reviewer: `azure-black-belt` (independent; did not write the code). Date: 2026-10-17. Round 1. Verdict: **REJECT**.
Scope: Phase 3 runtime diagnosis (FND-RUN-001..007), `internal/azure` runtime adapters and allow-list, `docs/runtime.md`, `docs/permissions-matrix.md`, ADR-021 (g).

This file is a concise summary recorded by the catalogue owner from the verdict relayed by the parent session; the full report is held by the parent session. Remediation status is tracked in `docs/development/hand-offs/phase-3.md`. Status of every item below: **in remediation, fix loop 1**. Nothing is closed until the reviewer confirms it.

## Blocking findings

1. **B1a Metrics query keys rejected by the allow-list.** The `runtime.metrics` operation sends query keys (`metricnames`, `aggregation`, `timespan`, `interval`, `$filter`, and others) that the transport allow-list rejects, so the metrics probes could never run against the real transport.
2. **B1b Resource-scope role assignments rejected by the allow-list.** Role assignment lists at a resource scope (Search, Storage, Cosmos DB account) are not on the allow-list, so FND-RUN-001 could not run live.
3. **B2 Foundry response shapes.** The Foundry v1 data plane response shapes in the adapter do not match the specification (same root cause as the Foundry lead B1).
4. **B3 Search enum, freshness, aggregation, timespan.** Indexer status and execution status values do not match the Search 2026-04-01 enums; statistics freshness was handled in the wrong unit; metric aggregation and timespan were wrong.
5. **B4 RBAC must be scope-aware and role-definition-aware.** Assignments were matched by name or without checking that the scope contains the resource and without resolving the role definition (custom roles, Cosmos `roleDefinitionId`).
6. **B5 DNS FAIL must be gated on private endpoint evidence.** A FAIL on DNS requires evidence that the dependency has a private endpoint.
7. **B6 `logQuery` bypass and workspace identifier.** The Log Analytics query still bypasses the transport allow-list, so the ADR-021 (g) exit criterion is unmet; the workspace identifier must be the workspace GUID.
8. **B7 Governance gaps.** The hand-off, the permissions matrix rows and the `-race` note were incomplete or untrue.

## Non-blocking findings

N1-N9: recorded in the reviewer report held by the parent session; each is tracked as "in remediation, fix loop 1" in the hand-off.

## Status

All of B1a, B1b, B2-B7 and N1-N9: **in remediation, fix loop 1** (2026-10-17). The Go agent owns the behaviour and allow-list changes. The catalogue owner updated the rule YAML, `docs/permissions-matrix.md` (ARM runtime rows verified against the stable spec; new rows for the Foundry v1 data plane, the Search data plane and the new allow-list operations), `docs/runtime.md` and the hand-off. ADR-021 (g) stays open until the Go patch removing the `logQuery` bypass is confirmed.


## Round 2 (2026-10-18)

Reviewer: `azure-black-belt`. Verdict: **ACCEPT with conditions**; all conditions are **non-blocking**. This section is a summary recorded by the catalogue owner from the verdict relayed by the parent session; the full report is held by the parent session. Status is 'fixed in loop 2, pending re-review' except where stated.

| Id | Condition | Status |
|---|---|---|
| C1 | No data-plane responses recorded from a live tenant | open by design: ADR-021 (d) Phase 3 clause records the exit criterion (first hand-off after a test tenant exists) |
| NB1 | DNS resolver wiring left the NXDOMAIN branch dead | fixed in loop 2, pending re-review |
| NB2 | Use `customSubDomainName` for FQDNs; only Approved private endpoint connections are proof | fixed in loop 2, pending re-review (FND-RUN-002 v4) |
| NB3 | Definition of a private address | fixed in loop 2, pending re-review (clearly public versus other; exact definition stays UNVERIFIED) |
| NB4 | Environment context-error cache | fixed in loop 2, pending re-review |
| NB5 | `DoJSON` hardening (host suffix, https, GET only) and `WaitDelay` | fixed in loop 2, pending re-review |
| NB6 | RBAC group noise and Cosmos container scope | kept uncertain, documented (FND-RUN-001 notes); not a fix |
| NB7 | FND-RUN-006 zero documents | fixed in loop 2, pending re-review (FND-RUN-006 v5) |
| NB8 | FND-RUN-007 KQL scoping | documented limit (FND-RUN-007 v4); not a fix |
| NB9 | Permissions matrix drift | fixed in loop 2, pending re-review |
| NB10 | Tests | fixed in loop 2, pending re-review |
