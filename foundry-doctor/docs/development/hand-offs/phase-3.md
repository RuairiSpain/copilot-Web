# Phase 3 hand-off: runtime diagnosis

Status: **gate round 1 on 2026-10-17: Foundry lead REJECT, Azure black belt REJECT. Fix loop 1 landed in commit 173db99 (Go) and this catalogue and docs sync (2026-10-18). Every blocker is fixed in loop 1, pending re-review. Phase 3 is not complete until both reviewers ACCEPT.** The security and Go principal reviews are deferred by ADR-019. The parent session updates this file after the re-review.

## Delivered
- `internal/runtime` (with foundry, search, network, monitor, rbac sub-packages): runtime probes, timeouts, 429 handling, redaction, metadata-only guard. The scheduler is wired: `rules/run.Schedule` (called from `internal/app/runtime.go`) runs the selected rules through `runtime.Scheduler` with `Options.Parallelism` (4) bounding the rules in flight and a per-probe `Options.Timeout` (`--timeout`, default 30s); a probe that exceeds it is skipped as input-unavailable naming `--timeout`, and the engine then replays the stored outcomes in rule order. Concurrent behaviour has been tested only with fakes (`schedule_test.go`), never against a live tenant.
- Rules FND-RUN-001..007 in `internal/rules/run`.
- `foundry-doctor runtime` command; `docs/runtime.md`, `docs/troubleshooting-report-template.md`, `docs/permissions-matrix.md` entries.
- Coverage on the new packages was 85-91% before the gate (not re-measured after loop 1).

## Changed contracts
- Phase 3 gate loop 1 changes the behaviour of every FND-RUN rule (see below); the catalogue `version` of each is raised by one.
- ADR-021 (g) (the `logQuery` allow-list bypass) is closed in Phase 3 loop 1, pending re-review: the call is the allow-listed operation `runtime.loganalytics.query` (after `runtime.workspace.get`) through the shared transport. See the (g) status paragraph in `docs/decisions/ADR-021-phase-2-deviations.md`.
- The runtime allow-list no longer contains any private DNS operation: the VNet-link probe (`ListPrivateDNSVNetLinks`, `LinkState`, `RuntimeDNS`, `runtime.privatedns.links`) was deleted from the Go code and from FND-RUN-002.
- The catalogue validator now accepts the literal api-version `v1` on the data plane (and nothing else that is not a date); FND-RUN-005 records it.

## Rule catalogue changes (loop 1, 2026-10-17)
Rules raised one `version` in loop 1 (2026-10-17) and one more when the text was synced to the Go code (2026-10-18): FND-RUN-001 (3), 002 (3), 003 (3), 004 (3), 005 (3), 006 (4), 007 (3). Catalogue text, tests lists, `lastVerified` and source notes were updated from the primary sources read on 2026-10-17 and 2026-10-18 (Azure/azure-rest-api-specs: cognitiveservices 2026-09-01, Foundry v1 OpenAPI, Search 2026-04-01, Insights metrics 2024-02-01, diagnostic settings 2021-05-01-preview, Log Analytics workspaces 2025-02-01, Cosmos DB rbac 2025-10-15; MicrosoftDocs: built-in-roles.md, Log Analytics API access-api.md, request-format.md, response-format.md). The catalogue now describes the Go code on disk, not intended behaviour; where the Go code relies on something the catalogue owner could not verify, the rule text says UNVERIFIED.

## Gate round 1 (2026-10-17) and remediation
Both reviewers rejected round 1. 'Fixed in loop 1, pending re-review' means the Go code and the catalogue were changed to address the finding; no reviewer has confirmed it. Summaries: `docs/development/reviews/phase-3-foundry-lead.md` and `docs/development/reviews/phase-3-azure-black-belt.md`.

| Reviewer finding | Blocking finding (summary) | Status |
|---|---|---|
| Foundry lead B1 | FND-RUN-005 wrong response shapes (connections `value`, `versions.latest`, limit at most 100, paging, empty status) | fixed in loop 1, pending re-review (RUN-005 v3) |
| Foundry lead B2 | FND-RUN-007 cannot pass or fail (workspace GUID, `PrimaryResult`, `.azure.com` endpoint) | fixed in loop 1, pending re-review (RUN-007 v3) |
| Foundry lead B3 | FND-RUN-004 malformed metric queries, misattributed evidence | fixed in loop 1, pending re-review (RUN-004 v3) |
| Foundry lead B4 | FND-RUN-001 Cosmos false pass, Storage false fail, user-assigned principal | fixed in loop 1, pending re-review (RUN-001 v3) |
| Foundry lead B5 | FND-RUN-002 fails on public endpoints; probe 2 not wired (probe 2 deleted from the Go code and the catalogue) | fixed in loop 1, pending re-review (RUN-002 v3) |
| Foundry lead B6 | FND-RUN-006 impossible fail path, false fail, invented metadata | fixed in loop 1, pending re-review (RUN-006 v4) |
| Foundry lead B7 | ADR-022 obligations undisclosed | fixed in loop 1, pending re-review (this file, section below) |
| Foundry lead N1-N9 | Non-blocking findings | fixed in loop 1, pending re-review |
| Azure black belt B1a | Metrics query keys rejected by the allow-list | fixed in loop 1, pending re-review (Go agent) |
| Azure black belt B1b | Resource-scope role assignments rejected by the allow-list | fixed in loop 1, pending re-review (Go agent) |
| Azure black belt B2 | Foundry response shapes | fixed in loop 1, pending re-review |
| Azure black belt B3 | Search enum, freshness, aggregation, timespan | fixed in loop 1, pending re-review |
| Azure black belt B4 | Scope-aware, role-definition-aware RBAC | fixed in loop 1, pending re-review |
| Azure black belt B5 | DNS FAIL gated on private endpoint evidence | fixed in loop 1, pending re-review |
| Azure black belt B6 | `logQuery` bypass (ADR-021 g exit criterion) and workspace identifier: bypass removed, ADR-021 (g) closed | fixed in loop 1, pending re-review |
| Azure black belt B7 | Governance gaps (this hand-off, matrix rows, `-race` note) | fixed in loop 1, pending re-review (matrix rows, this file); `-race` note updated below |
| Azure black belt N1-N9 | Non-blocking findings | fixed in loop 1, pending re-review |

## ADR-022 obligations (Foundry lead B7)
- **FND-RUN-003 connection-target scope policy**: delivered in loop 1 (catalogue and Go), pending re-review. The Go code uses `metadata.ResourceId` when present, otherwise an ARM-ID `properties.target`; a literal ARM ID outside the project's resource group and every `--approved-scope` (a `runtime` flag) is a finding; a resource-backed connection with no resource ID, or an unresolved expression, is uncertain. The key name `ResourceId` is UNVERIFIED as a schema fact (the ARM spec types `metadata` as a free map).
- **FND-IDN-002 live check and FND-NET-002 live check are NOT delivered in Phase 3.** Their property paths are unverified in the catalogue, and CLAUDE.md and ADR-022 forbid implementing an unverified property path first. They remain part of the **v1 release gate** (ADR-022): the cross-resource-group DoD item stays PARTLY MET and the gate is carried to the Phase 9 audit. FND-SEC-005 stays allocated to Phase 8. Silence does not waive any of these; only a new dated ADR does.

## Commands implemented
`foundry-doctor runtime` (flags in `docs/runtime.md`). `--vantage vnet` is an operator assertion, not a verified fact.

## Tests executed and results
`-race` was previously noted as not run. It now passes on linux/amd64 with go1.26.9 (reported by the parent session; this agent did not run it). The Phase 2 toolchain note (Go 1.25.13) is superseded by the toolchain the parent session uses. Other gates were not re-run by the catalogue owner. Catalogue gates run after the loop 1 catalogue edits (from `foundry-doctor`): `go run ./cmd/rulecatalog validate`, `generate-docs --check`, `generate-overlap --check`, `bash scripts/claude/check-rule-catalog.sh`: see the parent session's final run. No live Azure test has ever run.

## Security and privacy review
Deferred by ADR-019 to Phase 9. Not met.

## Foundry lead review
Round 1: REJECT (2026-10-17). See `docs/development/reviews/phase-3-foundry-lead.md`. Re-review after loop 1: pending.

## Azure black belt review
Round 1: REJECT (2026-10-17). See `docs/development/reviews/phase-3-azure-black-belt.md`. Re-review after loop 1: pending.

## Known limitations
- Nothing in loop 1 has been re-reviewed; no live Azure test, real-tenant or VNet probe has ever run.
- FND-RUN-001 does not evaluate group-based or deny assignments (confidence capped at medium). Role definitions are not fetched; roles are matched by built-in GUID (VERIFIED in `built-in-roles.md` 2026-10-18).
- FND-RUN-002: the vantage point is asserted, not verified; what counts as a private address is unverified; non-Foundry dependencies (Search, Storage, Cosmos DB) are proven private-endpoint-backed only through the connection `peStatus` Active, never through their own private endpoint connections.
- FND-RUN-004: the `StatusCode` value domain is unverified; values that are not three-digit codes give uncertain.
- FND-RUN-006: Search role per operation and the data-plane token scope are unverified; connection metadata keys for index and indexer names are UNVERIFIED hints; the 60-minute freshness window is product opinion.
- FND-RUN-007: the query RBAC action and the Monitoring Reader and Log Analytics Reader sufficiency are unverified; resource-specific destinations are uncertain.

## Unverified list (catalogue owner, 2026-10-18)
- `StatusCode` dimension value domain of `AzureOpenAIRequests` (FND-RUN-004).
- Cosmos DB Built-in Data Contributor SQL role GUID `00000000-0000-0000-0000-000000000002` (FND-RUN-001); the `enterprise_memory` database name is taken from the catalogue evidence.
- Connection metadata keys: `ResourceId` (FND-RUN-003) and `index`/`indexName`/`indexes`/`indexer`/`indexerName`/`indexers` (FND-RUN-006).
- Foundry data plane api-version `v1`: the spec documents `info.version` v1 and its `api-version` parameter has no enum, so other values are not excluded.
- `metricnamespace` is not sent on the metrics query; whether these metrics need one is unverified.
- Workspaces_Get 2025-02-01 was read; a newer stable 2026-03-01 exists and was not read.
- Diagnostic setting category group `audit` is not assumed to carry Audit or RequestResponse rows; only the `allLogs` group is counted.
- Non-Foundry dependencies are proven PE-backed only through the connection `peStatus` Active.
- RBAC action names for most runtime operations and the built-in roles carrying them (Monitoring Reader, Log Analytics Reader, Foundry User) remain UNVERIFIED; see `docs/permissions-matrix.md`.

## Deferred issues with rationale
- Live disposable-subscription tests: no Azure environment (ADR-021 d).
- FND-IDN-002 and FND-NET-002 live checks: see ADR-022 obligations above (not deferred silently; carried to the Phase 9 audit as a v1 release gate).

## Compatibility matrix
Linux and Windows, amd64 and arm64 (ADR-011 Amendment 2). `-race` evidence only on linux/amd64.

## Artefacts for next phase
`internal/runtime`, `internal/rules/run`, `docs/runtime.md`, `docs/permissions-matrix.md` (Phases 2 and 3), catalogue rules FND-RUN-001..007.

## Exact next-phase prerequisites
- Re-review of Phase 3 loop 1 by the Foundry lead and Azure black belt, both ACCEPT.
- ADR-021 (g) is closed in loop 1; the reviewers confirm the closure.
- Phase 9 audit lists FND-IDN-002, FND-NET-002 and FND-SEC-005 against ADR-022.

## Definition of Done audit
Statuses are conservative while the re-review is pending.

| DoD item | Status (met / unmet / waived-by-ADR) | Current evidence |
|---|---|---|
| FND-RUN-001..007 report pass/fail/skipped/uncertain correctly | fixed in loop 1, pending re-review | Round 1 REJECT from both reviewers; Go patch (173db99) and catalogue sync landed; not re-reviewed |
| Runtime probes are read-only and go through the allow-list | fixed in loop 1, pending re-review | Metrics, resource-scope role assignments, account, workspace and Log Analytics query are allow-listed operations; `logQuery` bypass removed (ADR-021 g closed, pending re-review) |
| Skipped and uncertain are never reported as pass | fixed in loop 1, pending re-review | Catalogue states this per rule; Go report matches it |
| No document, prompt, completion or secret content is read or emitted | unmet pending re-review | Metadata-only guard; not independently reviewed (ADR-019) |
| Permissions matrix covers every runtime probe | partly met | Rows for every runtime allow-list operation exist; several action names, roles and the Search token scope are UNVERIFIED |
| Cross-resource-group scope policy (ADR-022): FND-RUN-003 | fixed in loop 1, pending re-review | Delivered and synced to the Go code; not yet reviewed |
| Cross-resource-group scope policy (ADR-022): FND-IDN-002, FND-NET-002 live checks | unmet; v1 release gate (ADR-022), carried to the Phase 9 audit | Not delivered in Phase 3: property paths unverified in the catalogue |
| Independent Foundry lead and Azure black belt reviews | unmet | Round 1 REJECT both (2026-10-17) |
| Security and Go principal reviews | waived-by-ADR-019 | Deferred to Phase 9 |
| `-race` run | met | Passes on linux/amd64 go1.26.9 (parent session report) |
| Hand-off written, DoD audited | met | This file |
