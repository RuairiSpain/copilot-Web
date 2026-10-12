# Overlap fragment: OPS and COST

Research-state semantics are defined in `research-status.md`. Defender and Advisor are unresearched
here, not searched-no-match, so affected decisions are provisional.

Phase 0 research, verified 2026-10-04 against the local clones in `refs/` (Learn source repositories and GitHub specifications,
because learn.microsoft.com and prices.azure.com are blocked). Test scenario convention used in the catalogue files:
`positive` = the rule reports a violation, `negative` = compliant input with no finding, `skipped` = the rule cannot run and says why.

Clones added or extended for this group (not in the brief's table): `refs/azure-monitor-docs` (MicrosoftDocs/azure-monitor-docs; the
Azure Monitor articles are no longer in azure-docs), and in `refs/rest-api-specs` the `consumption`, `monitor` (Microsoft.Insights)
and `operationalinsights` specifications. In `refs/azure-docs` the `articles/search`, `cosmos-db`, `defender-for-cloud`, `advisor`
and `azure-monitor` folders listed in the sparse set do not exist in that repository any more; Search content was read from
`refs/azure-ai-docs/articles/search`. Cosmos DB, Defender for Cloud and Advisor Learn sources are not reachable (see Unverified).

Versions the facts were read against: Cognitive Services ARM stable 2026-09-01; Consumption budgets 2023-11-01 and 2026-06-01;
Search 2025-05-01; API Management 2024-05-01; Cosmos DB 2026-03-15; Log Analytics 2026-03-01; Insights metric alerts 2018-03-01 and
2026-01-01, activity log alerts 2020-10-01 and 2026-01-01, scheduled query rules 2026-03-01; azd schema `schemas/v1.0/azure.yaml.json`;
azure.ai.agents extension 1.0.0-beta.18 (clone), evaluation article minimum 0.1.40-preview.

## Rule table

| ID | Decision | Coverage | Mapped external IDs | Status |
|---|---|---|---|---|
| FND-OPS-001 | adapt | partial | Policy 55d1f543-d1b0-4811-9663-d6d0dbc6326d, 1b4d1c4e-934c-4703-944c-27c82c06bebb, 8def4bdd-4362-4ed6-a26f-7bf8f2c58839, b4330a05-a843-4bc8-bf9a-cacce50c67f4, 567c93f7-3661-494f-a30f-0a94d9bfebf8, 68ba9fc9-71b9-4e6f-9cf5-ecc07722324c, b4fe1a3b-0715-4c6c-a5ea-ffc33cf823cb (no PSRule rule for these types) | verified |
| FND-OPS-002 | native (provisional) | none | related resource-only controls: PSRule Azure.AppInsights.Workspace, Policy d550e854-df1a-4de9-bf44-cd894b39a95e | product-opinion |
| FND-OPS-003 | adapt | partial | PSRule Azure.Monitor.ServiceHealth; Policy 98903777-a9f6-47f5-90a9-acaf62ab01a8, b954148f-4c11-4c38-8221-be76711e194a | product-opinion |
| FND-OPS-004 | adapt | partial | PSRule Azure.Resource.RequiredTags, Azure.Resource.UseTags, Azure.Group.RequiredTags, Azure.Subscription.RequiredTags; Policy 871b6d14-10aa-478d-b590-94f262ecfa99, 1e30110a-5ceb-460c-a204-c1c3969c6d62, 96670d01-0a4d-4649-9c89-2d3abc0a5025, 8ce3da23-7156-49e4-b145-24f95f9dcb46 | product-opinion |
| FND-OPS-005 | adapt | partial | PSRule Azure.Storage.Name, Azure.APIM.Name, Azure.Search.Name, Azure.Cosmos.AccountName, Azure.KeyVault.Name, Azure.ACR.Name, Azure.Log.Name, Azure.AppInsights.Name, Azure.ContainerApp.Name, Azure.Group.Name, Azure.AI.FoundryNaming; Bicep BCP334, BCP335 | verified |
| FND-OPS-006 | adapt | partial | Checkov CKV_AZURE_37 (legacy activity log profile only, not equivalent) | product-opinion |
| FND-OPS-007 | native | none | none | product-opinion |
| FND-OPS-008 | native | none | none | product-opinion |
| FND-OPS-009 | adapt | partial | PSRule Azure.Resource.AllowedRegions; Policy e56962a6-4747-49cd-b67b-bf8b01975c4c, 0473574d-2d43-4217-aefe-941fcdf7e684 | verified |
| FND-OPS-010 | adapt | partial | Policy aafe3651-cb78-4f68-9f81-e7e41509110f, 8791d062-ba96-4c34-b604-8538f7e30ca0 | product-opinion |
| FND-COST-001 | native | none | none | product-opinion |
| FND-COST-002 | native | none | none (PSRule Azure.Search.SKU points the other way and is not mapped) | product-opinion |
| FND-COST-003 | native | none | references the APIM `llm-token-limit` policy only | product-opinion |
| FND-COST-004 | native | none | none (Advisor cost recommendations are separate deployed-state evidence) | product-opinion |

Counts: 14 rules. Status: verified 3 (OPS-001, 005, 009), product-opinion 11 (OPS-002, 003, 004, 006, 007, 008, 010; COST-001 to 004),
proposed 0, dropped 0. Decisions: native 7 (OPS-002, 007, 008; COST-001 to 004), adapt 7 (OPS-001, 003, 004, 005, 006, 009, 010),
reuse 0, wrap 0, drop 0. Implementation owner is `native` for all 14. No rule is `reuse` because the Foundry Doctor engine must run
offline without PSRule, Azure Policy or Defender being installed or assigned.

Severity: only OPS-005 and OPS-009 carry platform basis and are `error` in dev, test and prod. OPS-009 is evaluated only when the
profile declares a residency requirement, otherwise it is skipped. All other rules are recommendations (waf, operations, cost or
opinion basis) with graduated severities.

## Rules set to product-opinion (no platform source for the requirement)

- FND-OPS-003: the Foundry article tells users how to create Service Health and metric alerts and names the metrics; nothing says a
  deployment without them is invalid, and no source defines the baseline set.
- FND-OPS-004: only the tag limits and case rules are platform facts. Which tags are required is organisational.
- FND-OPS-006: retention defaults and ranges are verified; the minimum comes from the profile.
- FND-OPS-007, FND-OPS-008: azd and Foundry document how to create pipelines and run `azd ai agent eval run`; neither requires them.
- FND-OPS-010: policy parameter names and matching semantics are verified; what "consistent" means is a product choice.
- FND-COST-001 to 003: budget, "production-sized" and gateway token limits are cost recommendations; property and attribute facts are
  sourced. FND-COST-004 is informational only and its price source is unverified.

FND-OPS-002 is product-opinion: it runs only when an explicit observability or server-tracing
requirement exists. Production status alone does not imply the requirement, and absence of an
AppInsights connection is not a production error. Application Insights/Log Analytics ingestion,
sampling and retention create a cost tradeoff. FND-OPS-001 remains `verified` while its basis includes
`waf`. The Well-Architected guidance text could not be read
(MicrosoftDocs/well-architected is not public). The expectation rests on Microsoft documentation and built-in policy definitions,
and the files say so.

No rule was changed to `dropped`.

## Evidence differences where coverage is partial

- OPS-001: Azure Policy checks deployed resources after the fact, per type; Foundry Doctor checks the compiled template, validates
  categories against the supported-logs reference and correlates the whole Foundry stack to one workspace.
- OPS-003: PSRule and Policy cover Service Health and generic activity log alerts; none covers Foundry model-deployment metric alerts.
- OPS-004: PSRule `Azure.Resource.RequiredTags` is equivalent for required names and formats; Foundry Doctor adds profile-driven
  per-environment lists, Foundry scope and tag-limit checks, and runs without the PSRule runtime.
- OPS-005: PSRule covers most single types but not Foundry projects or azd environment values, and two PSRule documents differ from
  the Learn table (Log Analytics 3-63 versus 4-63; Application Insights 1-255 versus 1-260). The Learn table is used.
- OPS-006, 009: no tool compares retention or SKU residency scope against a declared requirement.
- OPS-010: Policy enforces at deployment time; Foundry Doctor checks azure.yaml and list consistency before deployment.

## Unverified

- Defender for Cloud and Advisor: their Learn sources (defender-for-cloud, advisor) are not in the azure-docs clone and Learn is blocked,
  so no recommendation ID was mapped for any rule. Every `defender` and `advisor` field is `[]` because nothing was verified, not
  because none exists.
- Cosmos DB Learn articles (including docs on diagnostic settings and throughput) are not in any clone; Cosmos facts come from the
  Azure Monitor supported-logs page, the REST specification and the Foundry standard agent setup article.
- OPS-001: whether Foundry projects emit logs independently of the account setting; the Cosmos supported-logs page is dated
  2026-09-15, the others 2026-08-21.
- OPS-002: ARM and azure.yaml wiring was removed from the rule. When an explicit observability or
  server-tracing requirement exists, it inventories only deployed project connection categories
  through the control plane; target/metadata/auth wiring remains externally blocked and is not
  remediation evidence. With no requirement the result is Skipped, not a production finding.
- OPS-003: activity log alert condition layout for a stable API version (only a preview ServiceHealth example read) and scheduled query
  rule property names.
- OPS-004: which resource types support tags (tag-support.md not read per type); PSRule says tag names are case-sensitive while the Learn
  tag article says names are case-insensitive for operations, and the rule follows Learn.
- OPS-005: Foundry account names with underscore, period or trailing hyphen (specification pattern and naming-rules reference
  disagree, so only violations rejected by both are enforced); model deployment names (no pattern in the specification); the length of
  azd resource tokens; Search naming is from the portal quickstart, not from the naming-rules table.
- OPS-006: allowed workspace `retentionInDays` per pricing plan; table inheritance behaviour in ARM; classic Application Insights.
- OPS-007: pipelines outside GitHub Actions and Azure Pipelines; central or external pipeline templates.
- OPS-008: where thresholds live in `eval.yaml` (no dedicated key found in the extension source at 1.0.0-beta.18); the extension version
  scheme (0.1.40-preview in Learn versus 1.0.0-beta.18 in the clone).
- OPS-009: region membership of each data zone and geography; whether DeveloperTier and batch SKUs are valid in azure.yaml; storage
  and processing locations for agent threads; spillover interaction with residency. `sku.name` is a free string in the specification, so
  the SKU names come from Learn only.
- OPS-010: whether the approved-models definition blocks ARM or Bicep deployments of standard model deployments (the definition targets
  `Microsoft.CognitiveServices.Data/accounts/deployments`; only the model router article states ARM rejection); mapping from azure.yaml
  model fields to the policy `model.publisher` and `model.assetId`; the model router model-subset property (Phase 8).
- COST-001: contact requirement per scope (specification says contactEmails is required, description allows action groups at
  subscription and resource-group scope).
- COST-002: minimum PTU per model; Bicep resource shapes for account-level versus container-level Cosmos throughput.
- COST-003: the older `azure-openai-token-limit` policy; effective policy resolution across scopes in compiled ARM; policy availability by
  APIM tier.
- COST-004: the Azure Retail Prices API contract (endpoint, filters, versions), meter naming, PTU meters. Only a web-search summary
  was available; it is recorded in the file as not used.

## Proposed new rules (fragment only, no catalogue files)

OPS
1. Azd discovery tags - `azd-env-name` on the resource group and `azd-service-name` on each service resource must exist and match the
   environment and `azure.yaml` service names (deployment feasibility). Verified source: azd docs `tooling-environment-faq.md` states that
   azd discovers resource groups by `azd-env-name` and resources by `azd-service-name`. Likely fits the DEP group.
2. Trace content capture in production - warn when tracing is configured to record prompt or response content outside dev (privacy
   and supportability). Starting point: `refs/azure-ai-docs/articles/foundry/observability/how-to/traces-sensitive-content.md`; not yet read in detail.
3. Model retirement horizon - warn when a deployed model version has a documented retirement date within a configured window (release
   readiness). Starting point: `refs/azure-ai-docs/articles/foundry/openai/includes/concepts-model-retirements-content.md`; not yet read in detail.

COST
1. PTU utilisation (Phase 3 runtime) - report provisioned-throughput deployments with persistently low `ProvisionedUtilization` (the
   metric name exists in the Azure Monitor accounts metrics reference). Cost visibility for fixed capacity.
2. Cost attribution tags on Foundry deployments - require tags on model deployments (the Foundry cost article filters cost by
   deployment tags and the `project` tag) so spend can be split per deployment. Overlaps OPS-004; would be a profile preset rather than a new engine.
3. Quota feasibility - compare requested `sku.capacity` against subscription quota from the usages API before deployment (deployment
   feasibility, avoids failed provisioning). Needs the quota API read from the specification (usages operations exist in the 2026-09-01 file; not analysed).
