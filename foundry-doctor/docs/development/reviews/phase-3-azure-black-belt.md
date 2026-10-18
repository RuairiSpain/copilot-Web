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
