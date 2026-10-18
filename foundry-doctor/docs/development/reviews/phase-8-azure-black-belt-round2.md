# Phase 8 review: Azure black belt (round 2)

Reviewer: `azure-black-belt` (independent; did not write the code). Date: 2026-10-18. Round 2. Verdict: **APPROVE**.

Scope: the loop-1 fixes in `phase-8-fixlog-loop1.md` and ADR-028, checked against my round-1 findings (`phase-8-azure-black-belt-round1.md`) and the PRD section 16 Definition of Done.
Repository: `/mnt/workspace/working/canon` (`foundry-doctor/`).

Environment constraints:
- There is no Go toolchain or package registry in this sandbox, so nothing was compiled or tested. The hand-off says honestly that no gate was run, and under this loop's gate policy that is accepted.
- I read the code for compile errors and found none (see "Compile-risk check").
- Platform facts verified in round 1 are not repeated here; their URLs are in the round-1 file.

## Round-1 findings

| Id | Status | Evidence |
|---|---|---|
| B1 ADR-022/023 allocations | **CLOSED** (by disposition) | ADR-028 decision 1 records that FND-IDN-002 (live), FND-NET-002 (live) and FND-SEC-005 are not delivered. It moves them to a pre-v1 scope-policy work packet with the ADR-021 (e) and ADR-023 §5 exit criteria unchanged. It keeps the v1 gate and waives nothing. The hand-off (`phase-8.md` lines 85-90) lists all three and the gate. See question (a) below and N2. |
| B2 Version fail-safety | **CLOSED** | `app/phase8.go` sends only `runtimeiq.KnownAPIVersion` versions; any other version becomes an `unsupported-version` probe issue. `runtime/iq` `get()` also refuses unknown versions. Every IQ rule gates each object through `knownVersion` (uncertain). IQ-009 fails only `2026-04-01`; other versions are uncertain. GW rules gate APIM resource versions (`versionGate`). |
| B3 Unread plane reported as pass | **CLOSED** | `Snapshot.LiveRead` and `liveSnapshot()`. IQ-008 and IQ-012 return only the findings they can make from azure.yaml or ARM; otherwise `skipDataPlane()`. IQ-009 needs the live plane and always adds the header-forwarding uncertain note. GW-004 skips `no-apis`. |
| B4 `Collection(Edm.*)` treated as vector | **CLOSED** | `isVectorField` now requires a vector EDM type, `dimensions` or `vectorSearchProfile` (`rules/iq/iq.go:979`). |
| B5 Effective policy per operation and product | **CLOSED** | `EffectivePoliciesForAPI` builds the policy for each operation and each product (global, then product, then API, then operation). `expandBase` inherits nothing without `<base/>`. `evaluateAPI` fails an operation only when every variant fails; mixed results are reported as product-dependent uncertain. There is a test for each operation and for product variants. |
| B6 Policy that does not parse, and fragments | **CLOSED** | Strict `ParsePolicy`. A parse error, an external link, a missing inbound section, or an unresolved or too-deep `include-fragment` all go to `Unresolved`, which makes the variant `External`. Fragments in the template are expanded. Elements only inside `<choose>` are uncertain (`Conditional`). Tests: `gateway_test.go:40, 98, 135`. |
| B7 GW-005 RBAC leg relied on runtime-only IDs | **CLOSED** | `managedIdentityCheck` is uncertain for an identity given as an expression or not declared, a `client-id` set from a named value, an unmatched user-assigned identity, a Foundry target not in the template, and an identity with no visible assignment. `roleMissing` is reported only when the template assigns roles to that principal but none covers the target. See N3. |
| B8 `largeLanguageModel.enabled` | **CLOSED** | `gateway.go:299` reads `largeLanguageModel.logs == "enabled"`. The test checks both shapes. |
| B9 IQ-009 header location | **CLOSED** | azure.yaml header keys are no longer read. The header leg is always the uncertain note `header-forwarding-configured-outside-repo`. The sample comment matches Learn. |
| B10 Undocumented default data-plane calls | **CLOSED** | The `--iq-metadata` flag in `cmd/foundry-doctor/main.go:131` flows through `DoctorRequest.IQMetadata`, then `RunInput.IQMetadataProbe` (`app.go:428`), into the guard at `phase8.go:96`. It is ignored with `--local`. The probe has a 60 s total budget, 20 s per request, and object caps. 401/403, 404 and budget failures give distinct reasons, and the 401/403 reason names the role. `permissions-matrix.md` rows 58-61 list path, versions, role, the RBAC prerequisite and the token scope. They state honestly that "Reader alone" is unverified until a live replay. |
| B11 Invented `vectorQueries` | **CLOSED** | No `vectorQueries` symbol remains in `runtime/iq` or `rules/iq`. The IQ-006 weight leg was removed. |
| B12 `baseFilter` tokenizer | **CLOSED** | `tokenizeOData` and `filterFieldsFromBase` skip quoted literals (including `''` escapes), keywords, `search.*` and `geo.*` functions, and lambda range variables. Paths and unknown constructs return `ok=false`, which is reported as uncertain. |
| B13 Hand-off DoD and evidence per plane | **CLOSED** | `ev()`, `dataRef()`, `armRef()`, `yamlRef()` and `gwEvidence()` name the object, plane and api-version in each finding. Every finding sets `Key`. The DoD table now says "partly met" or "met in code, untested". The gateway sample has Bicep and policy XML. `runtime/gateway` has tests. Live smoke tests are deferred under ADR-028 §4 and disclosed. |

Round-1 non-blocking findings:
- Closed as stated in the fix log: N1, N2, N3, N4, N6 (documentation only), N7, N8, N10, N11, N12 and N14.
- Not fixed, and accepted as non-blocking: N5 (diagnostic-setting categories), N9 (any probe issue skips all IQ rules; the reasons are now distinct) and N13 (IQ-005 and IQ-012 facts not re-verified).

## The six checks requested for this round

**(a) ADR-028 disposition.** Acceptable. ADR-023 §2 requires a dated ADR that "pick[s] another phase or waive[s] them". ADR-028 places the three checks in a pre-v1 work packet that the Phase 9 / v1 gate owns. It keeps the ADR-022/023 v1 gate in full and waives nothing, so the unmet scope cannot ship silently. The reasons are honest: there is no toolchain for a change to a shared preflight package, and FND-NET-002's subnet path is still unverified. Two caveats are recorded as N2: the ADR is "for confirmation by the project lead delegate", and the work packet has no named owner.

**(b) Flag wiring.** Correct end to end, as traced under B10. The default is `false`, the flag is ignored with `--local`, and the rules skip with a reason naming `--iq-metadata`.

**(c) Other callers of `EffectivePolicyForAPI`.** These are FND-COST-003 (`cost.go:332`) and the REL rule `eval006` (`rel.go:501`). Neither gains false findings:
- The signature is unchanged. `ok` is still false only when no policy document applies.
- `External` now also covers XML that does not parse and unresolved fragments. In those cases COST-003 skips; before, empty parsed elements made it fail. The REL rule adds a note.
- The `Inbound` union across variants keeps the previous API-level "present anywhere" behaviour.
- `TestCost003GatewayTokenLimit` still holds: `<inbound />` is present and empty, which gives a finding, and the valid limit passes.

The behaviour of these two rules nevertheless changed without a rule-version bump, and the REL rule still fails when its retry evidence is unresolved. See N1.

**(d) `iq_api.go` removed.** The file is gone. A text search for `iq_api`, `SearchManagement` and `SearchServiceInfo` over `internal/`, `cmd/` and `pkg/` found no references. The importers of `runtime/gateway` are only gw, cost and rel.

**(e) Compile risks named by the fixer.**
- `type scopeInbound` and the method `(*Service).scopeInbound` at `gateway.go:461/467` are legal Go. Method names live in the type's method set, not in the package block. Inside the method, `scopeInbound{}` resolves to the type.
- `switch cov, why := cognitiveRoleCoverage(...); cov {` at `gw.go:540` is a valid switch with an init statement. `why` is used in the `roleUncertain` case, so there is no unused-variable error.
- Also checked:
  - `sdk.Skip.MissingCapability` and `sdk.SkipUnsupportedVersion` exist.
  - `phase8.go` imports `errors`, `fmt` and the `rt` alias, and uses all three.
  - The `break` inside the quote loop of `tokenizeOData` exits the inner `for`, as intended.
  - `slices` is imported in `gateway.go`.
- I saw no compile error. gofmt alignment cannot be checked without the toolchain.

**(f) Rule versions and `Finding.Key`.**
- All 18 catalogue entries are bumped: IQ-001..012 to v2 (IQ-005 and IQ-009 to v3), GW-001..006 to v2 (GW-005 to v4).
- Every IQ finding goes through `acc.add(..., key, ...)`. Every GW finding (`gw.go:201, 362, 430, 458, 604`) passes a stable key, made per operation by `opKey`.
- `findings.Fingerprint` includes `RuleVersion` and `Key`, so the bump resets baselines for these rules as intended. The rules are new and unreleased, so no migration is needed. This is consistent with ADR-008.

## New findings

No blocking findings.

Non-blocking:
- **N1.** FND-COST-003 and the REL rule `eval006` changed behaviour through the shared helper: no inheritance without `<base/>`, and parse or fragment failures now unresolved. Their catalogue versions were not bumped; `rules/catalog/cost/FND-COST-003.yaml` is still `version: 2`. `.claude/rules/foundry-doctor-rule-catalog.md:25` says "Changing rule behaviour increments ruleVersion".
  - Fix: bump both rules.
  - Fix: in `eval006`, report uncertain rather than fail when retry evidence is unresolved.
  - Fix: give COST-003 a reason other than `policy-uses-external-link` for XML that does not parse.
- **N2.** ADR-028 should record confirmation by the project lead delegate, as ADR-023 was decided, and name an owner for the pre-v1 scope-policy work packet. Add it to the Phase 9 hand-off checklist.
- **N3.** GW-005 reports `roleMissing` when the template assigns any role to the APIM identity but no Cognitive Services role on the target. The Cognitive role might be assigned in another deployment. Consider reporting uncertain, or state this inference in the catalogue.
- **N4.** `EffectivePoliciesForAPI` always evaluates the no-product variant. When an API requires a subscription and token validation lives only at product scope, the rule can never pass; it reports uncertain instead. Consider using `subscriptionRequired` to drop the no-product variant.
- **N5.** `snap.LiveRead` is set before the knowledge-base GET succeeds (`phase8.go`). There is no effect, because a failed read adds a probe issue and every rule skips. Set it after success for clarity.
- **N6.** The round-1 N5, N9 and N13 findings remain open, as recorded above.

VERDICT: APPROVE
