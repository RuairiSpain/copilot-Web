# Phase 3 hand-off: runtime diagnosis

Status: implemented; independent reviews (foundry-lead, black-belt, privacy) not yet run.

- internal/runtime (+foundry, search, network, monitor, rbac): probe scheduler, timeouts, 429 handling, redaction, metadata-only guard.
- Rules FND-RUN-001..007 in internal/rules/run.
- `foundry-doctor runtime` command; docs/runtime.md, docs/troubleshooting-report-template.md, permissions-matrix entries.
- Gates (windows/arm64, Go 1.25.13): gofmt, vet, test, staticcheck, govulncheck, secret scan, catalogue check pass. -race not run locally.
- Coverage on new packages 85-91%.

Open: independent reviews pending; API versions/RBAC actions marked UNVERIFIED in permissions-matrix.md; real-tenant and VNet probes untested; monitor resource-specific tables yield uncertain.
