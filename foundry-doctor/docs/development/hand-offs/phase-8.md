# Phase 8 hand-off

Phase 8: Foundry IQ, Search and APIM gateway deep validation (PRD section 16).
Status: **round 1: REJECT (foundry-lead and azure-black-belt), fixes applied in loop 1, pending re-review.**
Fix log: `docs/development/reviews/phase-8-fixlog-loop1.md`. Decisions and deferrals: ADR-028.

## Delivered
- Rule packages `internal/rules/iq` (FND-IQ-001..012) and `internal/rules/gw` (FND-GW-001..006), registered in the
  standard `doctor` engine (`internal/app/phase8.go`, `internal/app/wire.go`).
- `internal/runtime/iq`: azure.yaml connection parsing, ARM projections, and a metadata-only Search data-plane client for
  knowledge bases, knowledge sources, indexes and skillsets. Response shapes follow the Learn examples read 2026-10-18
  (`azureOpenAIParameters.apiKey`, `<kind>Parameters.ingestionParameters.*`, `createdResources.{index,skillset}`,
  `indexedSharePointParameters`, `indexedOneLakeParameters`). Only api-versions 2026-04-01, 2026-05-01-preview and
  2026-08-01-preview are sent; object names are validated before they enter a URL.
- `internal/runtime/gateway`: APIM normaliser with per-operation, per-product effective inbound policy honouring `<base/>`,
  in-template `include-fragment` resolution, strict XML parsing (unparsable text, external links and unresolved fragments are
  unresolved, never "empty"), API-version gate, `largeLanguageModel.logs` parsing.
- Opt-in data-plane probe: `foundry-doctor doctor --iq-metadata` (never with `--local`), 60 s budget, 20 s per request,
  object caps, distinct 401/403, 404 and budget skip reasons (ADR-028).
- Docs: `docs/iq-gateway.md`, Phase 8 rows in `docs/permissions-matrix.md`, ADR-028.
- Sample: `samples/iq-gateway/` (azure.yaml knowledge base MCP connection; Bicep with Search, Foundry account, APIM gateway,
  policy XML, policy fragment and role assignment; README with expected results derived from the code).
- Removed dead code: `internal/azure/iq_api.go` (`SearchManagement`, `SearchServiceInfo` were never used).

## Changed contracts
- `app.DoctorRequest.IQMetadata` and `app.RunInput.IQMetadataProbe` (new, default false); CLI flag `doctor --iq-metadata`.
  Without it no Search data-plane call is made. This changes the round-1 behaviour, where every non-`--local` `doctor` run
  probed the data plane.
- `runtime/iq`: `Snapshot.LiveRead`; `KnowledgeSource` gains `ParametersRead`, `IngestionRead`, `GeneratedIndexName`,
  `ConnectionUnclassified`, `Models` and `BackingIndex()`; `KnowledgeBase.VectorQueries`, `AnswerSynthesis`,
  `DeclaredSemanticUsage` and the azure.yaml header fields of `Connection` are removed; `Connection` gains `Category`,
  `Audience`; new `KnownAPIVersion`, `ValidObjectName`, `ParametersKey`, `ErrUnsupportedAPIVersion`, `ErrInvalidObjectName`,
  `ConnectionTargetUnresolved`.
- `runtime/gateway`: new `EffectivePoliciesForAPI`, `ParsePolicy`, `TopLevel`, `Conditional`, `KnownAPIVersion`,
  `Service.UnverifiedVersions`, `Service.AIBackingUnknown`; `EffectivePolicy` gains `Operation`, `Product`, `Unresolved`.
  `EffectivePolicyForAPI` keeps its signature for the cost and reliability rules and now reports `External` when any
  operation/product variant is unresolved.
- Phase 8 findings now set `Finding.Key`, so their fingerprints differ from round 1 (every Phase 8 rule version was raised).

## Rule catalogue changes
- Versions raised for all 18 rules (behaviour changed): IQ-001..004, 006..008, 010..012 to 2; IQ-005 to 3; IQ-009 to 3;
  GW-001..004, 006 to 2; GW-005 to 4. New dated sources (2026-10-18) and a "Round-1 fix" note on each entry.
- FND-IQ-006: vector-weight test cases removed (the knowledge-base `vectorQueries` member does not exist).
- FND-IQ-009: header leg is uncertain (header-forwarding-configured-outside-repo); 2026-05-01-preview is uncertain.
- `docs/rule-catalog.md` and `docs/overlap-analysis.md`: no generated column changed (title, phases, status, decision,
  severities and the first source are unchanged), so the files were not edited. **`go run ./cmd/rulecatalog generate-docs
  --check` and `generate-overlap --check` must be re-run with Go to confirm.**

## Commands implemented
- `foundry-doctor doctor --iq-metadata` (opt-in data-plane metadata probe for the IQ rules).

## Tests executed and results
**`go build`, `go vet`, `go test`, `scripts/claude/verify-phase.sh --strict` and `gofmt -l` have NOT been run in fix loop 1:
there is no Go toolchain in this environment and none can be installed.** The maintainer must run them. No earlier test run
is claimed for the current code.

Tests added or rewritten (not executed):
- `internal/runtime/iq`: contract tests per accepted api-version from Learn-shaped fixtures (`testdata/`: azureBlob,
  indexedSharePoint, indexedOneLake, searchIndex knowledge sources; knowledge base; index), unknown version and unsafe names
  send nothing, azure.yaml target parsing including `${VAR}` targets.
- `internal/runtime/gateway` (new): strict parsing, per-operation `<base/>`, product variants, fragments resolved and
  unresolved, `<choose>`, `largeLanguageModel.logs`, API-version gate, service declared after its children.
- `internal/rules/iq`: unknown api-version for every IQ rule, no data plane never passes, filterable `Collection(Edm.String)`,
  Learn `baseFilter` example and OData table, IQ-009 header leg and version legs, IQ-008 model keys and redaction, IQ-012
  skips.
- `internal/rules/gw`: operation dropping `<base/>`, product dropping `<base/>`, rawxml/external/fragment/`<choose>`
  uncertainty, fragment resolution, unknown APIM version, unresolved path, realistic GW-005 templates without principal IDs,
  `largeLanguageModel.logs`, tenant hints.
- `internal/app`: opt-in and `--local` gating, unknown api-version, generated index/skillset reads, permission failure text.

## Security and privacy review
- Data-plane reads are GET-only through `internal/runtime.DoJSON` (https, host suffix bound, no redirects), opt-in, bounded,
  and limited to object definitions. Connection strings and API keys are classified in memory and never stored or emitted.
- The probe is outside `internal/azure` `AllowedOperations()`, like the Phase 3 Search probe; recorded in ADR-028.

## Foundry lead review
- Round 1: REJECT (`phase-8-foundry-lead-round1.md`), fixes applied in loop 1, pending re-review.

## Azure black belt review
- Round 1: REJECT (`phase-8-azure-black-belt-round1.md`), fixes applied in loop 1, pending re-review.

## ADR-022 / ADR-023 allocations
| Rule | Allocated to Phase 8 by | Status | Record |
|---|---|---|---|
| FND-IDN-002 (live role-assignment scope check) | ADR-023 | **not delivered** | ADR-028 decision 1: re-allocated to the pre-v1 scope-policy work packet |
| FND-NET-002 (live private-endpoint subnet check) | ADR-023 | **not delivered**; `properties.subnet.id` still unverified | ADR-028 decision 1 |
| FND-SEC-005 (Key Vault references) | ADR-022 | **not delivered** | ADR-028 decision 1 |

**v1 release gate (kept):** the Phase 2 DoD item "cross-resource-group references follow an explicit scope policy" stays
PARTLY MET and blocks the Phase 9 / v1 hand-off until each ADR-022 reference kind is covered with the ADR-021 (e) positive and
uncertain tests, or waived by a new dated ADR. Catalogue `phases` of these rules are unchanged.

## Known limitations
- FND-IQ-009 never passes: header forwarding is configured on the agent MCP tool headers, which are not read.
- FND-IQ-008 is uncertain for models without `apiKey` and without `authIdentity` and for redacted connection strings until
  Search GET redaction behaviour is verified.
- FND-GW-005 is uncertain whenever principal, client ID, role or scope cannot be matched from the template (runtime-only
  values); Foundry User role GUID is unverified (uncertain).
- Gateway rules are uncertain for APIM resources outside api-versions 2024-05-01 and 2025-09-01-preview, and the policy
  rules are uncertain for an API with no policy document in the template.
- `indexedSql` knowledge source parameters and the `ingestionSchedule.interval` request format are unverified.
- Backend `credentials.authorization` remains a FND-GW-005 finding even alongside `authentication-managed-identity`
  (header-based authorization is not managed identity); this replaces the round-1 hand-off text that called it uncertain.

## Deferred issues with rationale
- Live metadata smoke tests and recorded live responses (ADR-028 decision 4): need a designated subscription.
- FND-IDN-002 / FND-NET-002 live and FND-SEC-005 (ADR-028 decision 1).
- Package layout differs from PRD section 16 (ADR-028 decision 3).
- Non-blocking review items not addressed: knowledge-base version 2025-11-01-preview not accepted (fails safe); probe issues
  still skip all IQ rules rather than only the affected object (now with distinct reasons).

## Compatibility matrix
| Plane | Versions accepted | Behaviour outside |
|---|---|---|
| Search data plane (knowledge bases, knowledge sources, indexes, skillsets) | 2026-04-01, 2026-05-01-preview, 2026-08-01-preview | not requested; IQ rules skip (`unsupported-version`) or report uncertain |
| Microsoft.Search management (FND-IQ-012) | 2025-05-01, 2026-03-01-preview, 2026-09-01-preview | uncertain |
| Microsoft.ApiManagement (service, APIs, policies) | 2024-05-01, 2025-09-01-preview | uncertain |
| `largeLanguageModel.logs` | read at any diagnostics version; property verified in 2025-09-01-preview | absent property is not a sink |

## Artefacts for next phase
- Learn-shaped contract fixtures in `internal/runtime/iq/testdata/`; gateway policy fixtures in the rule and runtime tests;
  `samples/iq-gateway/`.

## Exact next-phase prerequisites
- Run `gofmt -l`, `go vet ./...`, `go test -race ./...`, `scripts/claude/verify-phase.sh --strict` and the rule-catalogue
  check; fix any compile or format issue from this loop.
- Phase 9 audit must carry FND-IDN-002 (live), FND-NET-002 (live), FND-SEC-005 and the v1 gate (ADR-022, ADR-023, ADR-028).

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| All field/property names verified against current sources | partly met | Search shapes re-read from Learn 2026-10-18 and corrected (B2, B3, B11); APIM `largeLanguageModel.logs`, policy fragments, include-fragment re-read; invented `vectorQueries` and azure.yaml header keys removed. Remaining UNVERIFIED items are listed in `docs/iq-gateway.md` and give uncertain results. Not confirmed by tests (no toolchain). |
| Unsupported versions fail safely | met in code, untested | Probe sends only accepted versions; every IQ rule gates each object's version; GW rules gate APIM resource versions; unknown-version tests added but not run. |
| No content is retrieved or logged | met in code, untested | Definition GETs only, opt-in; secrets classified, never emitted. |
| Compound findings show evidence from each plane | met in code, untested | Findings carry `Key` and evidence naming the data-plane object and api-version, ARM resource and api-version, and azure.yaml service; index findings use the referencing connection location. |
| All IQ/GW rules have positive and negative integration scenarios | partly met | Positive, negative, uncertain and skipped unit scenarios for every rule and Learn-shaped contract tests per version; live metadata smoke tests deferred (ADR-028 decision 4); no test has been run in this loop. |
