# Foundry IQ and AI gateway rules

Phase 8 adds:

- `FND-IQ-001` .. `FND-IQ-012` for Azure AI Search / Foundry IQ metadata
- `FND-GW-001` .. `FND-GW-006` for API Management AI gateway policy and routing checks

## Safety contract

- Metadata only. The implementation never reads document bodies, prompts,
  completions, or connection secrets. Secret-bearing values (`connectionString`,
  `apiKey`) are classified in memory and never kept or echoed.
- Findings name the object, the JSON path and the plane of each piece of
  evidence (data-plane object with its api-version, ARM resource with its
  api-version, azure.yaml service), never values.
- Missing metadata, unresolved expressions, unparsable or external policy text,
  unread planes and unknown API versions give skipped or uncertain results,
  never a pass.

## Inputs and planes

| Plane | Source | When it is read |
|---|---|---|
| azure.yaml | `azure.ai.connection` services whose `target` is a Search knowledge base MCP endpoint (`https://{search}.search.windows.net/knowledgebases/{kb}/mcp?api-version=...`) or a Search service root; `authType`, `category`, `audience` | always |
| ARM | compiled Bicep / ARM: `Microsoft.Search/searchServices`, `Microsoft.Storage/storageAccounts`, `Microsoft.CognitiveServices/accounts[/deployments]`, `Microsoft.ApiManagement/service/*`, role assignments | always (when infrastructure compiles) |
| Search data plane | `GET /knowledgebases/{kb}`, `GET /knowledgesources/{ks}`, `GET /indexes('{i}')`, `GET /skillsets('{s}')` | only with `foundry-doctor doctor --iq-metadata`, never with `--local` (ADR-028) |

### Data-plane probe (opt-in)

- Enable with `doctor --iq-metadata`. The api-version is the one pinned in the
  azure.yaml MCP connection target and must be one of `2026-04-01`,
  `2026-05-01-preview` or `2026-08-01-preview`; any other version is not sent
  and every IQ rule skips with an `unsupported-version` reason.
- Required access: **Reader** (Learn: "can view ... object definitions") or
  Search Service Contributor on the search service, and role-based access
  enabled on the service ("Both" or "Role-based"; key-only services deny
  bearer requests). Search Index Data Reader, which Foundry gives the project
  identity, cannot read definitions. Token scope `https://search.azure.com/.default`.
  Source: Learn `search-security-rbac`, read 2026-10-18. Whether Reader alone
  authorises these data-plane GETs is not yet proven by a replay.
- Bounds: 60 s for the whole probe, 20 s per request, at most 8 knowledge bases
  and 32 knowledge sources per knowledge base. 401/403, 404 and an exhausted
  budget are distinct skip reasons.
- See `docs/permissions-matrix.md` for the rows.

### Response shapes parsed (Learn, read 2026-10-18)

- Knowledge base: `knowledgeSources[].name`, `models[].azureOpenAIParameters.{resourceUri, deploymentId, modelName, apiKey, authIdentity}`, `retrievalReasoningEffort.kind`, `outputMode`.
- Knowledge source: `searchIndexParameters.{searchIndexName, semanticConfigurationName, baseFilter, queryHints.filters[].field}`;
  `azureBlobParameters`, `indexedSharePointParameters` and `indexedOneLakeParameters` with
  `connectionString` (blob, SharePoint), `isADLSGen2` (blob), `ingestionParameters.{ingestionPermissionOptions, assetStore,
  ingestionSchedule, embeddingModel, chatCompletionModel, aiServices}` and `createdResources.{index, skillset}`.
  `indexedSql` parameters are **UNVERIFIED** and not parsed.
- Index: fields, `semantic`, `vectorSearch.{profiles, algorithms, vectorizers[].azureOpenAIParameters.apiKey, compressions}`.
- A field is a vector field only for `Collection(Edm.Single|Half|Int16|SByte|Byte)` or when it declares
  `dimensions` / `vectorSearchProfile` (Learn `supported-data-types`).

## Rule notes

- FND-IQ-001 extracts `baseFilter` fields with an OData tokenizer that ignores
  literals, keywords, `search.*` / `geo.*` functions and lambda range variables;
  complex-field paths and unknown functions are uncertain.
- FND-IQ-008: `apiKey` is read inside `azureOpenAIParameters` and from
  `ingestionParameters.aiServices`. Whether GET redacts or omits secrets is
  **UNVERIFIED**, so a redacted connection string, or a model without `apiKey`
  and without `authIdentity`, is uncertain.
- FND-IQ-009 checks ingestion permission options from the knowledge-source
  plane. `x-ms-query-source-authorization` is set on the agent's MCP tool
  `headers` with a structured-input placeholder such as `{{search_auth_token}}`
  (Learn `foundry-iq-connect`), not on the project connection, and Foundry
  Doctor does not read agent tool definitions, so that leg is always reported
  uncertain (`header-forwarding-configured-outside-repo`) and the rule never
  passes. Only `2026-04-01` fails the version leg; `2026-05-01-preview` is
  uncertain.
- FND-IQ-011: the request format of `ingestionSchedule.interval` is
  **UNVERIFIED**; values the XSD duration parser does not recognise are
  uncertain.
- FND-IQ-012 skips (does not pass) when no index-backed knowledge source was
  read; an absent `semanticSearch` is uncertain.
- Gateway rules resolve the effective inbound policy per operation and per
  product (global, product, API, operation), honouring `<base/>` at each scope
  (Learn `set-edit-policies`). An operation that drops `<base/>` is evaluated on
  its own; coverage that differs by product is uncertain. `include-fragment` is
  resolved from `Microsoft.ApiManagement/service/policyFragments` in the
  template; other fragments, unparsable policy XML (for example `rawxml` with
  policy expressions), external `*-link` policies and policies only inside
  `<choose>` are uncertain.
- An API with no policy document at any scope in the template is uncertain
  for the policy rules (its policies may be deployed elsewhere).
- Gateway rules accept APIM resources at api-version `2024-05-01` or
  `2025-09-01-preview` (the catalogue versions); any other version is uncertain.
- FND-GW-005 matches role assignments to the APIM identity by literal principal
  ID or by a reference expression naming the APIM service or the user-assigned
  identity, and the scope by literal ID or an expression naming the account.
  Anything it cannot match is uncertain, never a failure. Foundry User
  (formerly Azure AI User) is recognised from third-party catalogues only
  (**UNVERIFIED**) and gives uncertain.
- FND-GW-006 reads `properties.largeLanguageModel.logs == 'enabled'` (Learn
  template reference `Microsoft.ApiManagement/service/diagnostics`
  2025-09-01-preview).

## Sample

`samples/iq-gateway/` holds an azure.yaml with a knowledge base MCP connection
and Bicep for Search, a Foundry account and an API Management gateway with
policy XML, a policy fragment and a role assignment. Its README lists the
expected result per rule.

## Unverified

- The `azure.ai.connection` keys `category` and `audience` are taken from the
  azd `azure.ai.connection` service schema as cited by the Phase 8 round-1
  review; they are checked only when present.
- Search GET redaction of `connectionString` / `apiKey`.
- The request format of `ingestionSchedule.interval`.
- `indexedSql` knowledge source parameters.
- The Foundry User role GUID (53ca6127-db72-4b80-b1b0-d745d6d5456d).
- Whether built-in Reader alone authorises the data-plane definition GETs.
