# ADR-021: Phase 2 deviations from the PRD and ADR-006

Status: Accepted
Date: 2026-10-17

## Context

The Phase 2 gate round 1 (2026-10-17) was rejected by the Foundry lead and the Azure black belt. Several findings are not defects to fix but
differences between the PRD (section 10, Phase 2) or ADR-006 and what was built. This ADR records each difference, why it exists, and the exit
criterion where the difference is temporary. Anything not listed here is not waived.

## Decision

**(a) Token acquisition, no Azure SDK for Go.** The PRD names the Azure SDK for Go and the azd/Azure credential chain. Phase 2 acquires a bearer token
from, in order, the `AZURE_ACCESS_TOKEN` environment variable, `azd auth token`, then `az account get-access-token` (`internal/azure/auth.go`). The token is held
in memory only and never logged. `azidentity` and the Azure SDK for Go are not used. Reason: ADR-005 and ADR-011 keep the dependency set minimal, the
transport is a small allow-listed HTTP client (ADR-006 item 6), and an SDK client would expose generic request methods that the allow-list forbids. Adopting
the SDK later needs a new ADR and a `scripts/dependency_audit.py` run.

**(b) Azure Resource Graph is not used.** The PRD lists Resource Graph for inventory. Resource Graph queries are `POST` to
`/providers/Microsoft.ResourceGraph/resources`, which is outside the ADR-006 allow-list (POST only for documented evaluation operations, listed by name).
Inventory uses ARM list calls (`resources.list`, `resourcegroup.get`, api-version 2025-04-01). Consequence: inventory is per scope and slower on large
subscriptions. Revisit only with a new ADR that adds the operation to the allow-list.

**(c) What-if requests `ResourceIdOnly` (ADR-006 item 8 amended).** ADR-006 item 8 said what-if is requested as `FullResourcePayloads` "only to read
`changeType`". The implementation requests `ResultFormat: ResourceIdOnly`. Rationale: `ResourceIdOnly` returns the change type and resource ID per change and
no `before`/`after` bodies, so no payload secrets reach the process at all, which is stronger than discarding them at the client boundary. Consequence: no
property-level diffs; FND-DEP-008 reports resource IDs and change types only. Further, what-if in Incremental deployment mode cannot report deletions of resources absent from the
template, so FND-DEP-008 never reports a clean pass on deletions: it reports **uncertain** for that aspect, not pass.

**(d) Reader-only replay and live integration test are deferred.** ADR-006 item 9 requires running every "Reader suffices" row with a Reader-only identity
before the matrix says Reader suffices. The PRD also asks for integration tests in a disposable subscription. No Azure environment is available to the
project today. Both are deferred (waived by this ADR for the Phase 2 gate). The permissions matrix and `docs/preflight.md` keep the word "hypothesis" for
Reader sufficiency. Exit criterion: the first phase hand-off written after an Azure test subscription and a Reader-only test identity exist must run the
replay suite and the live scenarios (provider, quota, lock, policy, soft-delete), then change the matrix wording. Until then no document may say that Reader suffices without
"hypothesis".

**(e) Cross-resource-group scope policy coverage.** The PRD DoD item "cross-resource-group references follow explicit scope policy" is scoped to
what-if change IDs, the agent subnet, and the other literal resource-ID references the Go agent reports covering.
Covered references: the scope policy covers exactly (1) the resource IDs that a what-if predicts a change for
(FND-DEP-008; a change other than Ignore or NoChange outside the target resource group and outside `--approved-scope` is a finding) and (2) the agent
network-injection subnet (FND-DEP-011). NOT covered: connection targets, private endpoint subnets, role-assignment scopes and Key Vault references, because
their property names and paths have no verified catalogue entry (CLAUDE.md forbids implementing an unverified property name). The DoD item is therefore narrowed
to those two references and waived-by-ADR for the rest. Exit criterion: add each remaining reference kind to the scope policy in the phase in which the
catalogue verifies its property path.
References assembled from template expressions the doctor cannot evaluate are reported uncertain, not covered.

**(f) Package layout.** The PRD lists sub-packages (`internal/azure/auth`, `inventory`, `permissions`, `models`, `policy`, `whatif`, `names`). The
implementation is one flat `internal/azure` package plus `internal/preflight`. Reason: the allow-list, retry, concurrency and redaction live in one
transport, the adapters share unexported helpers, and sub-packages would force exporting the transport internals. The single `Client` interface is defined where
consumed (`internal/preflight`). Splitting later is a mechanical move.

**(g) Phase 3 `runtime.go` `logQuery` exception.** The Phase 3 runtime adapters live in the same flat package (`internal/azure/runtime.go`) and include a Log
Analytics count-only query (`logQuery`) that ADR-006 item 7 constrains to fixed aggregate templates. Exception: `logQuery` is Phase 3 code that the Phase 2 preflight does not call. It does not go through the transport allow-list (`internal/azure/transport.go`): it builds its
own request to `https://api.loganalytics.io/v1/workspaces/{id}/query` (an origin that is not the ARM endpoint) and requests its own token scope
(`https://api.loganalytics.io/.default`). Mitigations: the account name must match `^[A-Za-z0-9._-]{1,64}$` before it is placed in the fixed count-only KQL
(`summarize count() by Category`), the workspace ID is path-escaped, the call is a read-only query, and no result rows other than aggregate counts are kept. The
mitigations are weaker than ADR-006 item 7 asks for (the item also requires a shipped template and a client-side rejection list). Exit criterion: in the Phase 3
remediation, route the call through the allow-list (operation, origin and token scope declared there, with a hostile-name replay test), then delete this exception.
This is recorded here because it shares the package and allow-list; Phase 3 owns its review.

## Consequences

- The Phase 2 hand-off marks the affected DoD items as waived-by-ADR-021 with the exit criteria above, never as met.
- ADR-006 item 8 and the PRD tool table are read together with this ADR.
- The security review of credential handling is still deferred to Phase 9 (ADR-019); (a) makes that review simpler because token handling is one file.
