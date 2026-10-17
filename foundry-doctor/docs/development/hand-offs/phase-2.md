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
The reviewers' finding identifiers are in the gate reports held by the parent session; they were not available to the author of this section, so findings are listed by remediation item. The parent session adds the reviewer finding ids and updates each status after re-review.

| Remediation item | Blocking finding (summary) | Fix status |
|---|---|---|
| D1 | Preflight workflow was `workflow_dispatch` only; PRD requires a reusable workflow | in remediation (workflow_call added, os input, Bicep install, PR guard; YAML parses; unrun on GitHub) |
| D2 | Sample role missing verified actions; what-if permission contradiction between docs and role | in remediation (read-only role and separate what-if role) |
| D3 | Permissions matrix statuses wrong or unverified (inventory api-version refuted, subnet-link action names, role/deny verification, assignedTo filter, unused policyExemptions row) | in remediation |
| D4 | PRD and ADR-006 deviations undocumented | in remediation (ADR-021; placeholders for (e) and (g) await the Go agent report) |
| D5 | Hand-off status overstated | in remediation (this rewrite) |
| D6 | Limitations undocumented | in remediation (`docs/preflight.md` Limitations section) |
| Go code findings (inventory api-version, capability strings, scope policy coverage) | owned by the Go agent | in remediation |

## Commands implemented
`foundry-doctor preflight` (flags in `docs/preflight.md`).

## Tests executed and results
Last full run before the gate (windows/arm64, Go 1.25.13): gofmt, go vet, go test ./..., staticcheck 0.7.0, govulncheck, check-no-secrets, check-rule-catalog all passed. `-race` was not runnable locally; it runs on the CI runners. Not re-run after the remediation edits; the parent session runs `scripts/claude/verify-phase.sh` after integration. No live Azure test has ever run (ADR-021 d).

## Security and privacy review
Deferred by ADR-019 to Phase 9. Not met. Evidence meanwhile: allow-list tests, token held in memory only, no secrets in reports (implementers' tests, not an independent review).

## Foundry lead review
Round 1: REJECT (2026-10-17). Re-review pending.

## Azure black belt review
Round 1: REJECT (2026-10-17). Re-review pending.

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
- Fill the placeholders in ADR-021 (e) and (g).
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
