# Phase 2 hand-off: Azure read-only adapters and preflight

Status: **gate round 1 on 2026-10-17: Foundry lead REJECT, Azure black belt REJECT. Phase 2 is not complete.** Remediation is in progress (below). The parent session updates this file after the re-review; no item here is claimed fixed until a reviewer confirms it. The security and Go principal reviews are deferred by ADR-019.

## Delivered
- `internal/azure`: read-only adapters (inventory, permissions, models, policy, what-if, names, regions, deployments, subnet links), allow-listed transport, token acquisition from `AZURE_ACCESS_TOKEN`, `azd auth token` or `az account get-access-token` (ADR-021 a). One flat package (ADR-021 f).
- `internal/preflight`: FND-DEP-001..012. Readiness is computed from unfiltered outcomes (skipped and uncertain are never ready).
- `preflight` command, `docs/preflight.md`, `docs/permissions-matrix.md`, `samples/preflight` (read-only role, optional what-if role, OIDC samples).
- `.github/workflows/foundry-doctor-preflight.yml`: reusable (`workflow_call`) and `workflow_dispatch`, OIDC, `os: linux|windows`, pinned Bicep 0.48.1, refuses pull request events. Not yet run on GitHub.
- Go toolchain 1.25.13 (govulncheck GO-2026-6218, GO-2026-6090).

## Changed contracts
- ADR-006 item 8 amended in practice: what-if requests `ResourceIdOnly` (ADR-021 c).
- New ADR-021 records the Phase 2 deviations.

## Gate round 1 (2026-10-17) and remediation
Both reviewers rejected round 1. The reports are summarised in `docs/development/reviews/phase-2-foundry-lead.md` and `docs/development/reviews/phase-2-azure-black-belt.md`. Every item below is **fixed in round 1, pending re-review** unless the status says otherwise; nothing is claimed closed until a reviewer confirms it.

| Reviewer finding | Blocking finding (summary) | Status |
|---|---|---|
| Foundry lead B1 | What-if parameters (template parameters sent to what-if; unresolved required parameters) | fixed in round 1, pending re-review (unresolved required parameter skips; FND-DEP-008 v2) |
| Foundry lead B2 | What-if and permission scope (what-if action versus the read-only role and docs) | fixed in round 1, pending re-review (separate optional what-if role; FND-DEP-003 v2) |
| Foundry lead B3 | FND-DEP-002 treated Registering as passed | fixed in round 1, pending re-review (Registering is uncertain; FND-DEP-002 v2) |
| Foundry lead B4 | Cross-resource-group scope coverage | fixed in round 1, pending re-review (scope policy for what-if IDs and the agent subnet; narrowed by ADR-021 e) |
| Foundry lead B5 | Reusable workflow missing | fixed in round 1, pending re-review (`workflow_call`, `os` input, Bicep install, PR guard; unrun on GitHub) |
| Foundry lead B6 | ADR and process gaps | fixed in round 1, pending re-review (ADR-021, ADR-006 item 8, this hand-off) |
| Foundry lead N1-N9 | Non-blocking findings | fixed in round 1, pending re-review |
| Azure black belt B1 | Locks (lists return locks at and below the scope; relevance and resource-level reads) | fixed in round 1, pending re-review (FND-DEP-010 v2, `locks.list.resource`) |
| Azure black belt B2 | FND-DEP-002 namespace set (template-defined namespaces are not a finding) | fixed in round 1, pending re-review (FND-DEP-002 v2) |
| Azure black belt B3 | FND-DEP-009 content sent to checkPolicyRestrictions | fixed in round 1, pending re-review (unresolved expressions and secrets omitted; uncertain when anything is dropped; FND-DEP-009 v2) |
| Azure black belt B4 | API versions (inventory 2022-12-01 refuted) | fixed in round 1, pending re-review (2025-04-01; `docs/permissions-matrix.md`) |
| Azure black belt B5 | RBAC strings and sample role | fixed in round 1, pending re-review (corrected capability strings, `samples/preflight`) |
| Azure black belt B6 | Missing rule tests | fixed in round 1, pending re-review (`gate_test.go`, `gate_contract_test.go`) |
| Azure black belt B7 | What-if status, Incremental mode, Replace | fixed in round 1, pending re-review (`ResourceIdOnly`; Incremental cannot report deletions so a clean result is uncertain; no Replace claim; FND-DEP-008 v2) |
| Azure black belt N1-N12 | Non-blocking findings | fixed in round 1, pending re-review |
| `logQuery` bypasses the transport allow-list (Phase 3 code) | Reviewer observation | documented exception, not fixed: ADR-021 (g); exit criterion in Phase 3 remediation |
| Live disposable-subscription test and Reader-only replay | No Azure environment | waived-by-ADR, ADR-021 (d) |
| FND-DEP-009 never passes | Reviewer observation | by design (the catalogue: a clean policy check is uncertain, never a pass) |
| Cross-resource-group references other than what-if IDs and the agent subnet | Connection targets, private endpoint subnets, role-assignment scopes, Key Vault references | waived-by-ADR, ADR-021 (e); exit criterion: add when the catalogue verifies the property paths |

Rules whose behaviour changed in round 1 and whose catalogue `version` is now 2: FND-DEP-002, 003, 005, 006, 007, 008, 009, 010, 011.

## Commands implemented
`foundry-doctor preflight` (flags in `docs/preflight.md`).

## Tests executed and results
Last full run before the gate (windows/arm64, Go 1.25.13): gofmt, go vet, go test ./..., staticcheck 0.7.0, govulncheck, check-no-secrets, check-rule-catalog all passed. `-race` was not runnable locally; it runs on the CI runners. Not re-run after the remediation edits; the parent session runs `scripts/claude/verify-phase.sh` after integration. No live Azure test has ever run (ADR-021 d).

## Security and privacy review
Deferred by ADR-019 to Phase 9. Not met. Evidence meanwhile: allow-list tests, token held in memory only, no secrets in reports (implementers' tests, not an independent review).

## Foundry lead review
Round 1: REJECT (2026-10-17), see `docs/development/reviews/phase-2-foundry-lead.md`. Re-review pending.

## Azure black belt review
Round 1: REJECT (2026-10-17), see `docs/development/reviews/phase-2-azure-black-belt.md`. Re-review pending.

## Known limitations
See `docs/preflight.md` (Limitations): DEP-009 always uncertain; DEP-007 skips Cosmos DB and ACR gaps; DEP-008 cannot see deletions; required parameters must be resolvable; subscription-scope deployments; scope policy coverage; Reader sufficiency is a hypothesis. DEP-012 matrix covers Agents, Responses and Private VNet only; the 365-day staleness and the DEP-010 margin of 80 are product decisions. DEP-011 link ownership is untested against real tenants.

## Deferred issues with rationale
- Reader-only replay and live disposable-subscription tests: no Azure environment (ADR-021 d).
- Azure Resource Graph, Azure SDK for Go: not used (ADR-021 a, b).
- Several matrix rows remain UNVERIFIED (runtime data-plane, Cosmos SQL roles, Private DNS links, Insights metrics and diagnostic settings, Log Analytics): `learn.microsoft.com` and the REST specs repository were unreachable from the authoring sandbox.

## Compatibility matrix
Linux and Windows, amd64 and arm64 (ADR-011 Amendment 2). The preflight workflow supports `os: linux|windows`. Bicep CLI 0.48.1.

## Artefacts for next phase
`internal/azure` allow-listed client and adapters; `docs/permissions-matrix.md`; `samples/preflight`; ADR-021.

## Exact next-phase prerequisites
- Re-review of Phase 2 by the Foundry lead and Azure black belt, both ACCEPT.
- Run the preflight workflow once on a protected environment when an Azure test subscription exists.

## Definition of Done audit
Statuses are conservative while the re-review is pending.

| DoD item | Status (met / unmet / waived-by-ADR) | Current evidence |
|---|---|---|
| All DEP rules report pass/fail/skipped/uncertain correctly | unmet pending re-review | Unit and fixture tests passed before the gate; reviewers rejected round 1; no live Azure run |
| No preflight check writes or changes Azure resources | unmet pending re-review | Allow-list test in `internal/azure` (GET plus named POST reads); ADR-006; not independently reviewed (ADR-019) |
| Lack of permission identifies the exact skipped capability | unmet pending re-review | 403 degrades to `capability unavailable: <capability> at <path> (missing permission: <action>)`; matrix action names being corrected (D3) |
| What-if destructive changes surfaced with resource IDs | partly met, DEP-008 deletions uncertain | `ResourceIdOnly` what-if returns IDs and change types; Incremental mode cannot report deletions (ADR-021 c) |
| Cross-resource-group references follow explicit scope policy | waived-by-ADR-021 (e) for references beyond those listed there | `--approved-scope`, FND-DEP-008 and FND-DEP-011 tests |
| CI supports OIDC and no client secrets are required | unmet until the workflow runs | `foundry-doctor-preflight.yml` uses `azure/login` with `vars.*`, `id-token: write`, no secrets; never run on GitHub |
| Recorded Azure API fixtures for deterministic tests | partly met | Fixtures in `internal/azure/testdata`; none recorded from a live subscription |
| Integration tests in disposable subscriptions; Reader-only replay (ADR-006 item 9) | waived-by-ADR-021 (d) | Exit criterion in ADR-021 (d) |
| Azure SDK for Go and Resource Graph used | waived-by-ADR-021 (a, b) | Token acquisition via CLI and ARM list calls |
| Permission matrix documenting Reader and elevated read requirements | partly met | `docs/permissions-matrix.md`; some rows UNVERIFIED; Reader sufficiency labelled hypothesis |
| Sample OIDC configuration and least-privilege role guidance | met pending re-review | `samples/preflight/` (read-only role, what-if role, caller samples) and `docs/preflight.md` |
| Independent Foundry lead and Azure black belt reviews | unmet | Round 1 REJECT both (2026-10-17); re-review pending |
| Security and Go principal reviews | waived-by-ADR-019 | Deferred to Phase 9 |
| Hand-off written, DoD audited | met | This file |
