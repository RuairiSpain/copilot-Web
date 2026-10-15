# Phase 2 hand-off: Azure read-only adapters and preflight

Status: implemented; open issues listed below. Not pushed (no write access to the remote).

## Delivered
- internal/azure: read-only adapters (inventory, permissions, models, policy, what-if, names, regions, deployments, subnet links), allow-listed transport, CLI-token auth.
- internal/preflight: DEP-001..012. Readiness is computed from unfiltered outcomes (skipped/uncertain never ready).
- `preflight` command, docs/preflight.md, docs/permissions-matrix.md, samples/preflight, preflight workflow with resource-group and location inputs.
- Go toolchain bumped to 1.25.13 (govulncheck GO-2026-6218, GO-2026-6090).

## Gates (windows/arm64, Go 1.25.13)
gofmt, go vet, go test ./..., staticcheck 0.7.0, govulncheck, check-no-secrets, check-rule-catalog: all pass. -race not runnable locally; must run in Linux CI.

## Open issues
- DEP-009 never passes; DEP-007 skips for Cosmos DB/ACR.
- DEP-012 matrix covers Agents/Responses/Private VNet only; 365-day staleness and DEP-010 margin of 80 are product decisions.
- DEP-011 link ownership untested against real tenants; expression-named accounts may over-flag.
- Several API versions/RBAC actions marked UNVERIFIED in permissions-matrix.md.
- No azidentity SDK: tokens via azd/az CLI (needs ADR acceptance).
- Re-review of Phase 2 blockers and Azure black-belt review not yet run.
