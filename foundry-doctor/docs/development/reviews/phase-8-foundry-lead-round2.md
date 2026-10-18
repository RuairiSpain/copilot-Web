# Phase 8 review: Foundry lead, round 2

Reviewer: `foundry-lead` (independent; did not write the code). Date: 2026-10-18. Round 2.

Inputs read:
- the canonical repo `/mnt/workspace/working/canon` (`foundry-doctor/`, `.claude/`);
- `docs/development/reviews/phase-8-fixlog-loop1.md`;
- ADR-028;
- `docs/development/hand-offs/phase-8.md`;
- the changed code under `internal/runtime/iq`, `internal/rules/iq`, `internal/runtime/gateway`, `internal/rules/gw`, `internal/app/phase8.go`, `internal/app/app.go` and
  `cmd/foundry-doctor/main.go`, plus the callers in `internal/rules/cost` and `internal/rules/rel`;
- the tests and fixtures, the `rules/catalog/{iq,gw}` versions and `samples/iq-gateway/**`.

Environment: there is no Go toolchain in this sandbox (checked: `go` is not on PATH), so nothing was compiled or run. The compile assessment below
comes from reading the code. Gate policy for this loop: an honest "gates not run" hand-off is accepted, and code that visibly would not compile is blocking.
The round-1 platform sources (S1–S9 in round 1) were not re-fetched. Every shape that the fixes rely on matches what I quoted from them in round 1.

## Round-1 findings

| ID | Status | Evidence |
|---|---|---|
| B1 ADR-022/023 allocations | **CLOSED (by ADR-028)** | The hand-off (lines 83–91) lists FND-IDN-002 (live), FND-NET-002 (live) and FND-SEC-005 as **not delivered**, with the record and the v1 gate kept. ADR-028 decision 1 re-allocates them to a dated pre-v1 scope-policy work packet. Its exit criteria are unchanged (`scopeAllowed`, primary-source `properties.subnet.id`, positive and uncertain tests, catalogue `phases` updated in the same change). It waives nothing, and the catalogue `phases` are deliberately unchanged. See the judgement in (a) below. |
| B2 invented or misplaced shapes | **CLOSED** | The fixes match the round-1 sources: `aoaiParams.APIKey` / `AuthIdentity` sit inside `azureOpenAIParameters` (`runtime/iq/iq.go` `modelShape`); `ingestionParameters.{embeddingModel, chatCompletionModel, aiServices}` are parsed in `parseIngestion`; `createdResources.{index, skillset}` are read; and `vectorQueries` is removed (grep finds no remaining reference). Contract fixtures in `testdata/*.json` are tested in `TestKnowledgeSourceContractShapes` and `TestKnowledgeBaseAndIndexContractShapes`. |
| B3 SharePoint / OneLake parameters | **CLOSED** | `indexedSharePointParameters` and `indexedOneLakeParameters` are parsed. When `ParametersRead` or `IngestionRead` is false, IQ-009 gives uncertain, not a failure. `indexedSql` is not parsed and is disclosed as UNVERIFIED. It is not in `permissionIndexedSource`, so it cannot produce a false failure. |
| B4 version fail-safety | **CLOSED** | `HTTPClient.get` refuses any version outside `KnownAPIVersion` before sending; `buildPhase8Snapshot` records `unsupported-version` and does not probe; each IQ rule calls `knownVersion` per object. IQ-009 fails only on `2026-04-01`; `2026-05-01-preview` and unknown versions are uncertain. `TestIQRulesUnknownAPIVersionIsUncertain` uses `2027-01-01-preview`. |
| B5 `Collection(Edm.*)` as vector | **CLOSED** | `isVectorField` = the five S7 vector types, or `dimensions` / `vectorSearchProfile`. Negative test: `TestIQ002PermissionCollectionIsNotVector`. |
| B6 baseFilter tokenizer | **CLOSED** | The new OData tokenizer drops string literals, keywords, `search.*`/`geo.*` functions and lambda range variables, and returns `ok=false` (uncertain) for unknown constructs. I traced `tags/any(t: t eq 'x')` and the Learn example by hand; both give the right fields. Covered by `TestIQ001LearnBaseFilterExample` and `TestFilterFieldsFromBase`. |
| B7 IQ-009 header location | **CLOSED** | The `Connection` header fields are removed, and the azure.yaml header keys are no longer read. The header leg is always the `header-forwarding-configured-outside-repo` note. The sample uses only S9 keys (`target`, `authType`, `category`, `audience`), and a comment points to the agent MCP tool headers. |
| B8 absent plane reported as pass | **CLOSED** | `Snapshot.LiveRead` and `liveSnapshot()` gate the data-plane rules. IQ-008 without live data returns only the azure.yaml findings, otherwise it skips. IQ-012 skips without index-backed sources and keeps the template-only tier leg. GW-004 skips `no-apis` and notes `path-from-unresolved-parameter` for `[...]` paths. Covered by `TestIQRulesWithoutDataPlaneNeverPass`. |
| B9 unparsable policy / include-fragment | **CLOSED** | `scopeInbound` marks the policy unresolved on a parse error, an external link, an empty value or a missing inbound section. `expandFragments` resolves fragments that are in the template; a fragment that is missing, nested too deep or does not parse is unresolved. `<choose>`-only policies are uncertain in GW-001/002/005. Covered by `TestParsePolicyRejectsMalformedXML` and `TestEffectivePoliciesFragmentsAndUnresolved`. |
| B10 hand-off accuracy, evidence per plane, sample, tests | **CLOSED** | The DoD table now says "partly met" or "met in code, untested", and claims no test run. Findings carry a `Key`, and their evidence names the data-plane object and api-version, the ARM resource and api-version, and the azure.yaml service (`dataRef`/`armRef`/`yamlRef`, `gwEvidence`). The gateway sample has Bicep for the APIM service, logger, diagnostics, a policy fragment, the API, the API policy and RBAC. `runtime/gateway/gateway_test.go` is new. GW rules gate APIM versions through `versionGate`. Live smoke tests are deferred by ADR-028 decision 4 and disclosed. |
| N1 2025-11-01-preview | open, accepted | Fails safe as uncertain. |
| N2, N3, N4, N5, N6, N7, N8, N10, N11 | closed | Confirmed in code: identity `None` → `hasIdentity`; category and audience are checked in IQ-008; `ConnectionTargetUnresolved`; Foundry User and non-cognitiveservices audiences give uncertain; `iq_api.go` is deleted; `2024-07-01` is removed; redacted values give `ConnectionUnclassified` or the "absence of apiKey" note. |
| N9 package layout | closed by ADR-028 decision 3 | |

## Specific checks requested

**(a) ADR-028 disposition — acceptable.**
- ADR-023 decision 2 allows "a new dated ADR [to] pick another phase or waive them". ADR-028 picks a named pre-v1 work packet that the Phase 9 / v1 gate owns. It keeps the exit criteria of ADR-021 (e), ADR-022 and ADR-023 word for word, and it waives nothing. The Phase 2 DoD item stays PARTLY MET and still blocks v1.
- That is not the silence ADR-023 forbids. The reasons are factual: FND-NET-002's path is still unverified, and FND-SEC-005 is outside PRD s16.
- Two conditions, both non-blocking: N1 (the ADR still needs the project-lead delegate's confirmation) and N2 (the Phase 9 hand-off does not yet carry it).

**(b) `--iq-metadata` wiring — correct.**
- `cmd/foundry-doctor/main.go:131` registers the flag; line 115 maps it to `DoctorRequest.IQMetadata`.
- `internal/app/app.go:307` declares the field and line 428 maps it to `RunInput.IQMetadataProbe` (field at line 202).
- `phase8.go:96` returns the plain ARM model when `Local || !IQMetadataProbe || ARM == nil || AzureYAML == nil`.
- Bounds: a 60 s budget for the whole probe (`context.WithTimeout`), 20 s per request (`http.Client.Timeout`), at most 8 knowledge bases and 32 sources per knowledge base.
- Tests: `TestWithPhase8SnapshotIsOptIn` and `TestWithPhase8SnapshotSkipsLocalMode`.
- Only `doctor` exposes the flag, which is what ADR-028 says.

**(c) `EffectivePolicyForAPI` behaviour change — no false findings in the other callers.**
- `cost.go:332` (FND-COST-003) and `rel.go:501` (FND-REL-006) branch on `pol.External` first. COST-003 skips and REL-006 adds an uncertain note, so the wider definition of External (parse failure, missing fragment, missing inbound) only turns old "empty policy" outcomes into skips or uncertain results.
- Dropping inheritance when `<base/>` is absent is how APIM actually works.
- In COST-003 a finding still needs a resolved union of variants with no `llm-token-limit` anywhere. That union can only be empty when every applicable variant really omits it.
- `cost_test.go` `TestCost003GatewayTokenLimit` still holds: `<inbound />` parses as `HasInbound` with no elements, so the finding remains; the second case passes.
- Residual risk: the union can hide one operation that lacks the limit (a false pass). This existed before this loop, so it is non-blocking (N3), along with the now-misleading skip wording.

**(d) `internal/azure/iq_api.go` deleted — no dangling references.**
A grep for `SearchManagement`, `SearchServiceInfo` and `iq_api` over `*.go` and `*.md` (excluding reviews) finds only the hand-off note that records the deletion. The `knownSearchManagementVersion` hits are a different symbol.

**(e) Compile risks — none found.**
- `type scopeInbound` and method `(*Service).scopeInbound`: method names live in the method set, not the package block, so they don't clash. Inside the method, `scopeInbound{}` resolves to the type. This is legal.
- `switch cov, why := cognitiveRoleCoverage(...); cov {` in `gw.go:540`: an init statement plus a tag is legal, and `why` is used in `case roleUncertain`, so there is no "declared and not used" error.
- `cognitiveRoleCoverage` returns `(roleCoverage, string)`.
- I also checked imports against use in `rules/iq`, `runtime/iq` and `runtime/gateway` (all used), and grepped for fields removed by the fix (`ForwardSourceAuth`, `VectorQueries`, `VectorizerModelRefs`, `requestedAPIVersion`, `parsePolicy(`, and others): none remain in code or tests.
- Positional `failure{key, msg}` literals match the two-field struct, and `strings.CutPrefix` and `slices` are fine for `go 1.26.9`.
- `break` inside the string-literal loop of `tokenizeOData` exits the inner `for`, which is the intended effect.
- `Service.Fragments` is initialised in `ensureService`, so there is no nil-map write.
- Not verifiable without a toolchain: gofmt alignment.

**(f) Rule versions and `Finding.Key` — consistent with ADR-008.**
- Against `HEAD`, all 18 entries are bumped by one: IQ-001..012 except IQ-005 and IQ-009 go 1→2, IQ-005 and IQ-009 go 2→3, GW-001..004 and GW-006 go 1→2, and GW-005 goes 3→4.
- The rules changed meaning, so re-surfacing baselined findings is intended (ADR-008 decision 2, "a rule version bump therefore re-surfaces old findings on purpose").
- Every Phase 8 `acc.add` now takes a `key`, so fingerprints no longer depend on the `auto:<line>:<col>:<evidence>` fallback (ADR-008 Amendment 2).
- One weak key remains (N4).

## New findings

### Blocking

None. I found no false pass, wrong API shape, compile error or undisclosed DoD gap in the changed code that is genuinely blocking.

### Non-blocking

- **N1 ADR-028 status.** It is "Decided by: the Phase 8 implementer ... for confirmation by the project lead delegate". ADR-023, which it amends, was decided by the project lead delegate. Record that confirmation in the ADR status line before the Phase 9 audit relies on it.
- **N2 The Phase 9 hand-off does not mention ADR-028, FND-IDN-002 (live), FND-NET-002 (live) or FND-SEC-005.** The Phase 8 hand-off (line 127) says the Phase 9 audit must carry them. Make sure the Phase 9 hand-off and audit list them; that belongs to Phase 9, not Phase 8.
- **N3 Cost and reliability callers of `EffectivePolicyForAPI`.**
  - The skip and note wording ("policy-uses-external-link", "policy fragments are external") now also covers XML that does not parse and fragments missing from the template. Reword it, or pass `pol.Unresolved` through.
  - COST-003 still takes the union of variants, so one operation without `llm-token-limit` can be hidden by another that has it. This predates the loop. Consider `EffectivePoliciesForAPI` per variant, as GW-002 does.
- **N4 The `Finding.Key` of IQ-002 index-level findings (`key-field`, `content-field`, `semantic-configuration`) does not include the knowledge source.** When two sources read one index, both findings share rule, resource, location file and key, but their evidence differs, so the engine raises the ADR-008 "Key does not separate" diagnostic. Deduplicate per index or add the source name to the key.
- **N5 The GW version gate is narrow.** It accepts only `2024-05-01` and `2025-09-01-preview`, so templates on `2023-05-01-preview`, `2024-06-01-preview` and similar versions get uncertain from every GW rule. This fails safe, but coverage is low. Widen the list once the properties used are re-verified per version.
- **N6 Effect on `--strict`.** Any project with Microsoft.Search or IQ metadata in the template but no `--iq-metadata` now gets `input-unavailable` skips from the data-plane IQ rules, so `doctor --strict` exits 3. That is consistent with "skip, never pass", but document it in `docs/iq-gateway.md` and the `--strict` help text so users know to add `--iq-metadata` or deselect the IQ rules.
- **N7 Round-1 unsupported assumptions remain.** These are the IQ-005 claims (`auto` only on 2026-08-01-preview, the medium-reasoning region list, the per-tier knowledge-source limits) and the IQ-011 interval format. The fix log lists them as NOT-FIXED (ABB-N13). Keep their catalogue confidence at `likely` and list them under Unverified in `docs/iq-gateway.md`. They don't block because they fail toward a finding only on documented values.

## Verdict against the Phase 8 DoD (with ADR-028)

| DoD item | Status |
|---|---|
| All field/property names verified against current sources | met for the shapes used. The remaining UNVERIFIED items give uncertain results and are disclosed. |
| Unsupported versions fail safely | met in code (untested, toolchain unavailable) |
| No content is retrieved or logged | met in code. Definition GETs only, opt-in. |
| Compound findings show evidence from each plane | met in code |
| All IQ/GW rules have positive and negative integration scenarios | partly met. Unit and contract scenarios exist for every rule; live smoke tests are deferred by ADR-028 decision 4, which is accepted. |
| ADR-022/023 scope extension | not delivered. It is re-allocated with the v1 gate by ADR-028, which is accepted (see (a)). |

Blocking: none. Non-blocking: N1–N7.

VERDICT: APPROVE
