# ADR-028: Phase 8 Foundry IQ and gateway decisions, and the ADR-022/023 allocations Phase 8 does not deliver

Status: Accepted (records the Phase 8 fix-loop-1 decisions; pending the round-2 reviews)
Date: 2026-10-18
Decided by: the Phase 8 implementer under the fix-loop brief, for confirmation by the project lead delegate

## Context

The Phase 8 round-1 reviews (`docs/development/reviews/phase-8-foundry-lead-round1.md` B1, B10;
`phase-8-azure-black-belt-round1.md` B1, B10, B13, N9) found that:

1. ADR-022 allocated the live check of FND-SEC-005 to Phase 8, and ADR-023 moved the live checks of FND-IDN-002 and
   FND-NET-002 to Phase 8. ADR-023 decision 2 says Phase 8 must list them and, if it cannot carry them, a new dated ADR must
   pick another phase or waive them. The Phase 8 hand-off did neither.
2. The Foundry IQ data-plane probe ran on every non-`--local` `doctor` run, outside the permissions matrix, without a bound on
   the total probe time.
3. Several PRD section 16 deliverables and test items were missing or were claimed without evidence.

## Decision

### 1. FND-IDN-002 (live), FND-NET-002 (live) and FND-SEC-005 are not delivered in Phase 8

Phase 8 does not carry these three checks. Reasons:

- Phase 8 fix loop 1 had no Go toolchain, so the scope-policy change (`internal/preflight` `scopeAllowed`) and the
  ADR-021 (e) / ADR-023 decision 5 positive and uncertain tests could not be compiled or run. Shipping untested changes to a
  shared preflight package owned by another phase would be worse than an explicit deferral.
- FND-NET-002 still needs `properties.subnet.id` of `Microsoft.Network/privateEndpoints` read from a primary source
  (ADR-023 decision 3). That was not done in this loop.
- FND-SEC-005 (Key Vault references) is outside PRD section 16 (ADR-022 already called it a weak fit).

The three checks are re-allocated to a dedicated **pre-v1 scope-policy work packet** owned by the Phase 9 / v1 gate, not to a
later product phase. Its exit criteria are unchanged from ADR-021 (e), ADR-022 and ADR-023: extend `scopeAllowed` to each
reference kind, verify the property path from a primary source (FND-NET-002), add the positive and uncertain tests, and change
the catalogue `phases` field in the same change.

**The v1 release gate stays.** The Phase 2 DoD item "cross-resource-group references follow an explicit scope policy" stays
PARTLY MET and blocks the Phase 9 / v1 hand-off until each of the four reference kinds of ADR-022 is covered or waived by a new
dated ADR signed by the project lead. This ADR does not waive anything. The catalogue `phases` fields of FND-IDN-002
(`["1","2"]`), FND-NET-002 (`["1"]`) and FND-SEC-005 (`[later]`) are deliberately unchanged.

### 2. The Foundry IQ data-plane probe is opt-in

The probe that reads knowledge base, knowledge source, index and skillset definitions runs only with
`foundry-doctor doctor --iq-metadata` (`app.DoctorRequest.IQMetadata`, `RunInput.IQMetadataProbe`), never with `--local`, and
never by default. This keeps the ADR-010 command surface honest: a plain `doctor` run reads only local files and the ARM model.

- The probe reads definitions only (ADR-006 decision 1). Requests go through `internal/runtime.DoJSON` (GET only, https only,
  host suffix `.search.windows.net` on a label boundary, no redirects), as the Phase 3 Search probe (FND-RUN-006) does; they are
  not in `internal/azure` `AllowedOperations()`. The paths, api-versions and required roles are listed in
  `docs/permissions-matrix.md`.
- Only the api-versions 2026-04-01, 2026-05-01-preview and 2026-08-01-preview are sent; any other version pinned in the
  azure.yaml connection target becomes a skip reason and no request is sent. Object names are validated before they enter a URL.
- Bounds: one 60 s budget for the whole probe, 20 s per request, at most 8 knowledge bases and 32 knowledge sources per
  knowledge base. The probe does not use the Phase 3 scheduler or `--vantage`; `doctor` has no `--timeout` flag, so the
  fixed budget is the bound.
- 401/403 (including services that accept keys only), 404, an exhausted budget and other failures are distinct skip reasons.
  Rules that need a plane that was not read skip; they never pass.

### 3. Package layout differs from PRD section 16

PRD section 16 names `internal/providers/{iq,search,apim}` and `internal/rules/gateway`. Phase 8 uses `internal/runtime/iq`
(Foundry IQ and Search adapter plus normaliser), `internal/runtime/gateway` (APIM policy and backend normaliser),
`internal/rules/iq` and `internal/rules/gw`. The runtime packages follow the Phase 3 `internal/runtime/*` convention and the
rule package name follows the catalogue group (`GW`). No behaviour depends on the path.

### 4. Deferred test items

- **Live metadata smoke tests** (PRD section 16 testing strategy) need a designated test subscription with a Foundry IQ
  knowledge base and are not delivered; they are deferred to the same pre-v1 work packet and listed in the hand-off.
- **Recorded responses**: the contract fixtures in `internal/runtime/iq/testdata/` are built from the Learn response examples
  (read 2026-10-18), not recorded from a live service. They are run against every accepted api-version.
- Whether Search GET responses redact `connectionString` and `apiKey` is unverified; FND-IQ-008 treats redacted or absent
  secrets as uncertain until a live recording shows the behaviour.

## Consequences

- The Phase 8 hand-off lists FND-IDN-002 (live), FND-NET-002 (live) and FND-SEC-005 as not delivered, with this ADR and the
  v1 gate. The Phase 9 audit must carry them.
- `doctor` without `--iq-metadata` makes no Search data-plane call; IQ rules that need data-plane objects report
  `input-unavailable` skips naming the unread plane.
- If the project lead prefers the probe on by default, a later ADR must amend decision 2 and ADR-010.
