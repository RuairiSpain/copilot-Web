# Phase 8 fix log, loop 1

Date: 2026-10-18. Reviews: `phase-8-foundry-lead-round1.md` (FL) and `phase-8-azure-black-belt-round1.md` (ABB).
No Go toolchain was available: nothing was compiled, vetted, formatted or tested in this loop.

Files changed (all under `foundry-doctor/`):
`internal/runtime/iq/iq.go`, `internal/runtime/iq/iq_test.go`, `internal/runtime/iq/testdata/*.json` (new),
`internal/runtime/gateway/gateway.go`, `internal/runtime/gateway/gateway_test.go` (new),
`internal/rules/iq/iq.go`, `internal/rules/iq/iq_test.go`, `internal/rules/gw/gw.go`, `internal/rules/gw/gw_test.go`,
`internal/app/phase8.go`, `internal/app/phase8_test.go`, `internal/app/app.go` (two fields, one mapping line),
`cmd/foundry-doctor/main.go` (`--iq-metadata` flag), `internal/azure/iq_api.go` (deleted),
`rules/catalog/iq/FND-IQ-0*.yaml`, `rules/catalog/gw/FND-GW-0*.yaml`, `docs/iq-gateway.md`,
`docs/permissions-matrix.md` (Phase 8 rows), `docs/decisions/ADR-028-phase-8-iq-gateway-decisions-and-deferrals.md` (new),
`docs/decisions/README.md`, `docs/development/hand-offs/phase-8.md`, `samples/iq-gateway/**`, `samples/README.md`.

## Blocking findings

| Finding | Status | Change |
|---|---|---|
| FL-B1 / ABB-B1 ADR-022/023 allocations (IDN-002 live, NET-002 live, SEC-005) | ADR-028 | Not delivered; re-allocated to a pre-v1 scope-policy work packet; v1 gate kept; listed in the hand-off. |
| FL-B2 / ABB-B11 invented or misplaced shapes (`apiKey`, `models`, `skillsetName`, `vectorQueries`) | FIXED | `runtime/iq`: `azureOpenAIParameters.apiKey`/`authIdentity`; `ingestionParameters.{embeddingModel, chatCompletionModel, aiServices}`; `createdResources.{index, skillset}`; `vectorQueries` removed (IQ-006 weights dropped). Contract fixtures per version. |
| FL-B3 indexed SharePoint / OneLake parameters | FIXED | `indexedSharePointParameters` and `indexedOneLakeParameters` parsed; absent parameters object is uncertain in IQ-009; `indexedSql` not parsed (UNVERIFIED). |
| FL-B4 / ABB-B2 version fail-safety | FIXED | Probe sends only accepted versions (`KnownAPIVersion`), otherwise a skip; every IQ rule gates each object's version (uncertain); IQ-009 fails only 2026-04-01; unknown-version test for every IQ rule. |
| FL-B5 / ABB-B4 `Collection(Edm.*)` as vector | FIXED | Vector field only for documented vector types or `dimensions`/`vectorSearchProfile`; negative test. |
| FL-B6 / ABB-B12 baseFilter tokenizer | FIXED | OData tokenizer ignoring literals, keywords, functions and lambda variables; unknown constructs uncertain; Learn example test. |
| FL-B7 / ABB-B9 IQ-009 header location | FIXED | azure.yaml header keys no longer read; header leg always uncertain (`header-forwarding-configured-outside-repo`); sample and docs corrected. |
| FL-B8 / ABB-B3 unread plane reported as pass (IQ-008/009/012, GW-004) | FIXED | `Snapshot.LiveRead`; data-plane rules skip without it; IQ-008 reports only azure.yaml findings then skips; IQ-012 skips without index-backed sources; GW-004 skips with no APIs and is uncertain for expression paths. |
| FL-B9 / ABB-B6 unparsable policy and include-fragment | FIXED | Strict parse failure, external links, missing fragments and `<choose>`-only elements are uncertain; fragments in the template are resolved. |
| ABB-B5 effective policy per operation / product | FIXED | `EffectivePoliciesForAPI` per operation and product honouring `<base/>`; an operation fails alone; product-dependent coverage uncertain; tests. |
| ABB-B7 GW-005 RBAC leg on runtime-only IDs | FIXED | Principal and scope matched by literal or reference expression; unmatched identity, client-id, scope or missing visible assignment is uncertain; Foundry User unverified gives uncertain. |
| ABB-B8 `largeLanguageModel.enabled` | FIXED | Reads `largeLanguageModel.logs == "enabled"` (verified template reference); tests. |
| ABB-B10 undocumented default data-plane calls | FIXED + ADR-028 | Probe opt-in (`--iq-metadata`), bounded (60 s / 20 s / caps); permissions-matrix rows with role, scope, version, RBAC prerequisite. |
| FL-B10 / ABB-B13 hand-off accuracy, evidence per plane, sample, missing tests | FIXED (partly ADR-028) | Findings carry `Key` and plane-tagged evidence; DoD table corrected to partly met / met-untested; gateway sample delivered; runtime/gateway tests and contract fixtures added; live smoke tests deferred (ADR-028); no test run claimed. |
| FL-B10 gateway version gate | FIXED | GW rules gate APIM resource api-versions (2024-05-01, 2025-09-01-preview); others uncertain. |

## Non-blocking findings

| Finding | Status | Note |
|---|---|---|
| FL-N1 2025-11-01-preview | NOT-FIXED | Property set not verified; fails safe (uncertain). |
| FL-N2 IQ-009 catalogue text | FIXED | 2026-05-01-preview now uncertain in code and catalogue. |
| FL-N3 identity `None` | FIXED | Treated as no identity. |
| FL-N4 category / audience | FIXED | Checked when present. |
| FL-N5 `${VAR}` targets | FIXED | Recorded as unresolved (uncertain in IQ-008). |
| FL-N6 / ABB-N7 / ABB-N14 role list and audience | FIXED | Foundry User recognised as uncertain; other audiences uncertain. |
| FL-N7 / ABB-N11 dead code | FIXED | `iq_api.go` deleted; unused constants removed. |
| FL-N8 management 2024-07-01 | FIXED | Removed from accepted set. |
| FL-N9 package layout | ADR-028 | Decision 3. |
| FL-N10 / ABB-N8 GET redaction | FIXED | Redacted or absent secrets are uncertain. |
| FL-N11 / deployment host label | FIXED | Documented in code and catalogue; no finding when it does not correlate. |
| ABB-N1 legacy policy names | FIXED | Uncertain when only the legacy name is present. |
| ABB-N2 tenant hint / issuer text | FIXED | GUID or domain segment; issuer expression check reads issuer text. |
| ABB-N3 GW-003 non-AI APIs | FIXED | `return-response` / `mock-response` count as an answer. |
| ABB-N4 GW-004 pass without APIM | FIXED | Skips `no-apis`. |
| ABB-N5 diagnostic setting categories | NOT-FIXED | Kept at catalogue confidence likely. |
| ABB-N6 backend authorization alongside MI | FIXED (doc) | Kept as a finding; hand-off text aligned. |
| ABB-N9 probe issue scope | NOT-FIXED | Still skips all IQ rules; reasons now distinguish 401/403, 404, budget, version. |
| ABB-N10 URL escaping and names | FIXED | `url.Values`, object-name validation, unescaped knowledge-base name. |
| ABB-N12 `Finding.Key` | FIXED | Stable keys on every Phase 8 finding. |
| ABB-N13 IQ-005 / IQ-012 unverified facts | NOT-FIXED | Not re-verified in this loop; unchanged. |

## Compile risks to check first
- `internal/runtime/gateway/gateway.go`: method `scopeInbound` shares its name with the type `scopeInbound` (legal in Go).
- `internal/rules/gw/gw.go`: `switch cov, why := cognitiveRoleCoverage(...); cov {` form.
- gofmt alignment of new struct and composite literals was checked by hand only.
