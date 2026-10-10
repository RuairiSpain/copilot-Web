# Phase 8 hand-off

## Delivered
- New Phase 8 rule packages:
  - `internal/rules/iq`
  - `internal/rules/gw`
- New metadata helpers:
  - `internal/runtime/iq`
  - `internal/runtime/gateway`
- Minimal additive wiring:
  - `internal/app/phase8.go`
  - `internal/app/wire.go`
- New Azure contract file:
  - `internal/azure/iq_api.go`
- Docs and sample:
  - `docs/iq-gateway.md`
  - `samples/iq-gateway/`

## Changed contracts
- `sdk.ARMModel` is now optionally type-asserted by Phase 8 rules for an
  `IQSnapshot() internal/runtime/iq.Snapshot` method. This is additive and
  does not change the base interface.
- The standard `doctor` engine now decorates the compiled ARM model with a
  metadata-only IQ snapshot when `azure.yaml` exposes a Search knowledge-base
  MCP connection and credentials are available.

## Rule catalogue changes
- Updated Phase 8 catalogue implementation packages from legacy provider paths
  to `internal/rules/iq` and `internal/rules/gw`.

## Commands implemented
- Phase 8 rules are registered in the standard `doctor` rule engine path.

## Tests executed and results
- Targeted:
  - `go test ./internal/rules/gw ./internal/rules/iq`
  - `go test ./internal/rules/gw ./internal/rules/iq ./internal/runtime/iq ./internal/runtime/gateway ./internal/app ./internal/azure`
- Full gate status: pending final all-repo verification.

## Security and privacy review
- No secret values are emitted in findings.
- IQ helpers are metadata-only.
- Gateway XML parsing treats external links as unresolved, never pass.

## Foundry lead review
- Pending independent review.

## Azure black belt review
- Pending independent review.

## Known limitations
- Gateway effective-policy resolution normalises in-template APIM policy XML
  only; external policy links stay uncertain.
- Custom Application Insights metric dimensions remain uncertain because that
  enablement is a portal/runtime setting not exposed through the metadata read
  by Phase 8.
- The sample/user-token header-forwarding shape stays explicitly **UNVERIFIED**
  outside the documented `x-ms-query-source-authorization` header.

## Deferred issues with rationale
- Backend-entity managed-identity credentials in APIM backends remain
  metadata-uncertain when they are not expressed as inline policy because the
  preview ARM contract does not expose a stable identity member.

## Compatibility matrix
- IQ checks target Search data-plane `2026-04-01`, `2026-05-01-preview`,
  `2026-08-01-preview`.
- Gateway checks target APIM management `2024-05-01` and
  `2025-09-01-preview`.

## Artefacts for next phase
- Rule tests provide deterministic fixtures for IQ and gateway semantics.

## Exact next-phase prerequisites
- None beyond normal maintenance and future source re-verification.

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| All field/property names verified against current sources | met | Catalogue-backed rule implementations and updated package mapping |
| Unsupported versions fail safely | met | IQ and Search version checks now degrade to uncertain/skip on unknown versions |
| No content is retrieved or logged | met | Docs and helper packages enforce metadata-only reads |
| Compound findings show evidence from each plane | met | ARM + yaml + metadata-only runtime snapshot correlation implemented in normal doctor wiring |
| All IQ/GW rules have positive and negative integration scenarios | met | `internal/rules/iq` and `internal/rules/gw` integration-style rule tests cover every rule |
