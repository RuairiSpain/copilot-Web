# Phase 3 hand-off: runtime diagnosis

Status: **gate round 1 on 2026-10-17: Foundry lead REJECT, Azure black belt REJECT. Phase 3 is not complete. Fix loop 1 is in remediation.** No item below is claimed fixed until a reviewer confirms it. The security and Go principal reviews are deferred by ADR-019. The parent session updates this file after the re-review.

## Delivered
- `internal/runtime` (with foundry, search, network, monitor, rbac sub-packages): runtime probes, timeouts, 429 handling, redaction, metadata-only guard. The earlier claim of a parallel probe scheduler is not repeated here: whether independent probes run concurrently is not established by this hand-off.
- Rules FND-RUN-001..007 in `internal/rules/run`.
- `foundry-doctor runtime` command; `docs/runtime.md`, `docs/troubleshooting-report-template.md`, `docs/permissions-matrix.md` entries.
- Coverage on the new packages was 85-91% before the gate (not re-measured after loop 1).

## Changed contracts
- Phase 3 gate loop 1 changes the behaviour of every FND-RUN rule (see below); the catalogue `version` of each is raised by one.
- ADR-021 (g) (the `logQuery` allow-list bypass) stays open until the Go patch that routes the call through the allow-list is confirmed.

## Rule catalogue changes (loop 1, 2026-10-17)
Rules raised one `version`: FND-RUN-001 (2), 002 (2), 003 (2), 004 (2), 005 (2), 006 (3), 007 (2). Catalogue text, tests lists, `lastVerified` and source notes were updated from the primary sources read on 2026-10-17 (Azure/azure-rest-api-specs: cognitiveservices 2026-09-01, Foundry v1 OpenAPI, Search 2026-04-01, Insights metrics 2024-02-01, diagnostic settings 2021-05-01-preview, Log Analytics workspaces 2023-09-01, Cosmos DB rbac 2025-10-15). The Go behaviour is implemented by the Go agent and arrives as a patch; the catalogue describes the intended behaviour.

## Gate round 1 (2026-10-17) and remediation
Both reviewers rejected round 1. Summaries: `docs/development/reviews/phase-3-foundry-lead.md` and `docs/development/reviews/phase-3-azure-black-belt.md`.

| Reviewer finding | Blocking finding (summary) | Status |
|---|---|---|
| Foundry lead B1 | FND-RUN-005 wrong response shapes (connections `value`, `versions.latest`, limit at most 100, paging, empty status) | in remediation, fix loop 1 (RUN-005 v2) |
| Foundry lead B2 | FND-RUN-007 cannot pass or fail (workspace GUID, `PrimaryResult`, `.azure.com` endpoint) | in remediation, fix loop 1 (RUN-007 v2) |
| Foundry lead B3 | FND-RUN-004 malformed metric queries, misattributed evidence | in remediation, fix loop 1 (RUN-004 v2) |
| Foundry lead B4 | FND-RUN-001 Cosmos false pass, Storage false fail, user-assigned principal | in remediation, fix loop 1 (RUN-001 v2) |
| Foundry lead B5 | FND-RUN-002 fails on public endpoints; probe 2 not wired | in remediation, fix loop 1 (RUN-002 v2) |
| Foundry lead B6 | FND-RUN-006 impossible fail path, false fail, invented metadata | in remediation, fix loop 1 (RUN-006 v3) |
| Foundry lead B7 | ADR-022 obligations undisclosed | in remediation, fix loop 1 (this file, section below) |
| Foundry lead N1-N9 | Non-blocking findings | in remediation, fix loop 1 |
| Azure black belt B1a | Metrics query keys rejected by the allow-list | in remediation, fix loop 1 (Go agent) |
| Azure black belt B1b | Resource-scope role assignments rejected by the allow-list | in remediation, fix loop 1 (Go agent) |
| Azure black belt B2 | Foundry response shapes | in remediation, fix loop 1 |
| Azure black belt B3 | Search enum, freshness, aggregation, timespan | in remediation, fix loop 1 |
| Azure black belt B4 | Scope-aware, role-definition-aware RBAC | in remediation, fix loop 1 |
| Azure black belt B5 | DNS FAIL gated on private endpoint evidence | in remediation, fix loop 1 |
| Azure black belt B6 | `logQuery` bypass (ADR-021 g exit criterion unmet) and workspace identifier | in remediation, fix loop 1 |
| Azure black belt B7 | Governance gaps (this hand-off, matrix rows, `-race` note) | in remediation, fix loop 1 (matrix rows, this file); `-race` note updated below |
| Azure black belt N1-N9 | Non-blocking findings | in remediation, fix loop 1 |

## ADR-022 obligations (Foundry lead B7)
- **FND-RUN-003 connection-target scope policy**: delivered in loop 1 (catalogue and Go), pending re-review. A literal ARM ID outside the project's resource group and every `--approved-scope` is a finding; a target whose resource group cannot be determined is uncertain. Most live targets are URLs, so the uncertain outcome is expected to be common until `metadata.ResourceId` is read under a verified source.
- **FND-IDN-002 live check and FND-NET-002 live check are NOT delivered in Phase 3.** Their property paths are unverified in the catalogue, and CLAUDE.md and ADR-022 forbid implementing an unverified property path first. They remain part of the **v1 release gate** (ADR-022): the cross-resource-group DoD item stays PARTLY MET and the gate is carried to the Phase 9 audit. FND-SEC-005 stays allocated to Phase 8. Silence does not waive any of these; only a new dated ADR does.

## Commands implemented
`foundry-doctor runtime` (flags in `docs/runtime.md`). `--vantage vnet` is an operator assertion, not a verified fact.

## Tests executed and results
`-race` was previously noted as not run. It now passes on linux/amd64 with go1.26.9 (reported by the parent session; this agent did not run it). The Phase 2 toolchain note (Go 1.25.13) is superseded by the toolchain the parent session uses. Other gates were not re-run by the catalogue owner. Catalogue gates run after the loop 1 catalogue edits (from `foundry-doctor`): `go run ./cmd/rulecatalog validate`, `generate-docs --check`, `generate-overlap --check`, `bash scripts/claude/check-rule-catalog.sh`: see the parent session's final run. No live Azure test has ever run.

## Security and privacy review
Deferred by ADR-019 to Phase 9. Not met.

## Foundry lead review
Round 1: REJECT (2026-10-17). See `docs/development/reviews/phase-3-foundry-lead.md`.

## Azure black belt review
Round 1: REJECT (2026-10-17). See `docs/development/reviews/phase-3-azure-black-belt.md`.

## Known limitations
- The Go behaviour of loop 1 is not on disk at the time of writing; the catalogue describes intended behaviour and must be reconciled with the patch.
- `v1` (Foundry data plane) is not expressible in `compatibility.apiVersions`, because the catalogue validator accepts only `YYYY-MM-DD[-preview]`; it is recorded in the FND-RUN-005 notes. Relaxing the validator needs a Go change in `internal/catalog`.
- FND-RUN-001 does not evaluate group-based or deny assignments (confidence 'likely'); the Cosmos built-in role GUID is unverified, so matching uses the definition type and name returned by the service.
- FND-RUN-002: the vantage point is asserted, not verified; what counts as a private address is unverified; wiring of the DNS link probe (probe 2) is tracked here, not assumed.
- FND-RUN-004: the `StatusCode` value domain and the supported aggregation types of `AzureOpenAIRequests` are unverified; 429 not identifiable gives uncertain.
- FND-RUN-006: Search role per operation and the data-plane token scope are unverified; the 60-minute freshness window is product opinion.
- FND-RUN-007: Log Analytics query host, token scope and response shape are taken from the reviews, not from a spec the catalogue owner could read; resource-specific destinations are uncertain; Monitoring Reader and Log Analytics Reader sufficiency is unverified.
- Real-tenant and VNet probes have never run.

## Deferred issues with rationale
- Live disposable-subscription tests: no Azure environment (ADR-021 d).
- FND-IDN-002 and FND-NET-002 live checks: see ADR-022 obligations above (not deferred silently; carried to the Phase 9 audit as a v1 release gate).

## Compatibility matrix
Linux and Windows, amd64 and arm64 (ADR-011 Amendment 2). `-race` evidence only on linux/amd64.

## Artefacts for next phase
`internal/runtime`, `internal/rules/run`, `docs/runtime.md`, `docs/permissions-matrix.md` (Phases 2 and 3), catalogue rules FND-RUN-001..007.

## Exact next-phase prerequisites
- Go patch for loop 1 applied, then catalogue and Go reconciled.
- Re-review of Phase 3 by the Foundry lead and Azure black belt, both ACCEPT.
- ADR-021 (g) closed only after the allow-list routing of `logQuery` is confirmed.
- Phase 9 audit lists FND-IDN-002, FND-NET-002 and FND-SEC-005 against ADR-022.

## Definition of Done audit
Statuses are conservative while the re-review is pending.

| DoD item | Status (met / unmet / waived-by-ADR) | Current evidence |
|---|---|---|
| FND-RUN-001..007 report pass/fail/skipped/uncertain correctly | unmet, in remediation, fix loop 1 | Round 1 REJECT from both reviewers; catalogue updated, Go patch pending |
| Runtime probes are read-only and go through the allow-list | unmet, in remediation, fix loop 1 | `logQuery` bypasses the allow-list (ADR-021 g); metrics query keys and resource-scope role assignments rejected by the allow-list (B1a, B1b) |
| Skipped and uncertain are never reported as pass | unmet pending re-review | Catalogue states this per rule; Go patch pending |
| No document, prompt, completion or secret content is read or emitted | unmet pending re-review | Metadata-only guard; not independently reviewed (ADR-019) |
| Permissions matrix covers every runtime probe | partly met | ARM runtime rows VERIFIED against the 2026-09-01 spec; Foundry v1, Search and Log Analytics rows added; several action names and roles UNVERIFIED |
| Cross-resource-group scope policy (ADR-022): FND-RUN-003 | unmet pending re-review | Delivered in loop 1, not yet reviewed |
| Cross-resource-group scope policy (ADR-022): FND-IDN-002, FND-NET-002 live checks | unmet; v1 release gate (ADR-022), carried to the Phase 9 audit | Not delivered in Phase 3: property paths unverified in the catalogue |
| Independent Foundry lead and Azure black belt reviews | unmet | Round 1 REJECT both (2026-10-17) |
| Security and Go principal reviews | waived-by-ADR-019 | Deferred to Phase 9 |
| `-race` run | met | Passes on linux/amd64 go1.26.9 (parent session report) |
| Hand-off written, DoD audited | met | This file |
