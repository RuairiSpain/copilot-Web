# Overlap fragment: NET (FND-NET-001..011) and IDN (FND-IDN-001..006)

Researched 2026-10-04. NET-001 to NET-009 were researched by an earlier agent and re-read here for
consistency (no changes). NET-010, NET-011 and all IDN rules were researched in this pass.
Property names and enums come from `Azure/azure-rest-api-specs`; role GUIDs from the Azure built-in
roles reference (`MicrosoftDocs/azure-docs`, ms.date 07/01/2026); overlap IDs from PSRule for Azure rule
YAML and docs, Azure Policy built-in GUIDs and Checkov `check_id` values read in the clones.
Learn is blocked, so Azure Monitor private link articles came from `MicrosoftDocs/azure-monitor-docs`
and Entra workload identity articles from `MicrosoftDocs/entra-docs` (public clones). Defender
recommendation text and Advisor IDs were not in any clone; the two Defender entries are assessment
keys read from the policy definitions.

## Decision table

| ID | Decision | Coverage | PSRule | Azure Policy built-in (GUID prefix) | Defender (assessment key) | Checkov / Bicep linter | Status |
|---|---|---|---|---|---|---|---|
| FND-NET-001 | adapt | partial | Azure.AI.PublicAccess, Azure.AI.PrivateEndpoints | 037eea7a, d6759c02, 47ba1dd7, db630ad5 | - | CKV_AZURE_134 | verified |
| FND-NET-002 | adapt | partial | Azure.Storage.Firewall, KeyVault.Firewall, Cosmos.PublicAccess, ACR.Firewall | ee980b6d, b2982f36, 797b37f7, 405c5871, 0fdf0491 | - | CKV_AZURE_35, 101, 109, 189 | verified |
| FND-NET-003 | native | partial | none | c4bc6f10, fbc14a67, a63cc0bd, 75973700, ac673a9a | - | none | verified |
| FND-NET-004 | native | none | none | none | - | none | verified |
| FND-NET-005 | native | none | none | none | - | none | verified |
| FND-NET-006 | native | none | none | none | - | none | verified |
| FND-NET-007 | adapt | partial | Azure.AI.PublicAccess | 037eea7a | - | CKV_AZURE_134 | verified |
| FND-NET-008 | adapt | partial | Azure.Search.SKU | a049bf77 | - | none | verified |
| FND-NET-009 | adapt | partial | none | 73ef9241, ef619a2c | - | CKV_AZURE_107, 174 | verified |
| FND-NET-010 | adapt | partial | none (no AMPLS rule) | 0fc55270, a499fed8, bec5db8e, e8185402, 437914ee, 1bc02227, 6c53d030 | - | none | product-opinion |
| FND-NET-011 | adapt | partial | Azure.VNET.UseNSGs, Azure.NSG.AnyInboundSource (no DDoS rule) | e71308d3, 94de2ad3, a7aca53f, 752154a7 | eade5b56-eefd-444f-95c8-23f29e5d93cb, e3de1cc0-f4dd-3b34-e496-8b5381ba2d70 | CKV_AZURE_9, 10, 160 | verified |
| FND-IDN-001 | adapt | partial | Azure.AI.ManagedIdentity (kind TextAnalytics only), Azure.ContainerApp.ManagedIdentity, Azure.AppService.ManagedIdentity | fe3fd216 | - | CKV_AZURE_238 | verified |
| FND-IDN-002 | native | none | none | none | - | none | verified |
| FND-IDN-003 | native | none | none | none (25237d14 reads principalType but is not equivalent) | - | none | verified |
| FND-IDN-004 | native | none | none | none | - | none (CKV_AZURE_39 tests custom role definitions, not assignments) | verified |
| FND-IDN-005 | adapt | partial | none | 4f11b553, 09024ccc, 339353f6, 0cfea604, 25237d14 | 6f90a6d6-d4d6-0794-0ec1-98fa77878c2e, 2c79b4af-f830-b61e-92b9-63dfa30f16e4, 20606e75-05c4-48c0-9d97-add6daa2109a, 050ac097-3dda-4d24-ab6d-82568e7a50cf | none | verified |
| FND-IDN-006 | adapt | partial | none | 2571b7c3, fd1a8e20, ae62c456 (all preview) | - | none | verified |

Counts: 16 verified, 1 product-opinion, 0 dropped, 0 proposed. Decisions: adapt 10, native 7, reuse 0,
wrap 0, drop 0.

## Honest overlap assessment

- NET-001, 002, 007, 008, 009 and IDN-001 rest on existing single-property tests. The adapt decision depends on the
  Foundry-specific correlation named in each rule. If Phase 1 only delivers the property test, IDN-001 and NET-007
  should become `reuse` (map the policy and PSRule IDs).
- IDN-001: the PSRule rule `Azure.AI.ManagedIdentity` is restricted by `where: kind in TextAnalytics` in
  `Azure.AI.Rule.yaml`, so it does not cover kind AIServices accounts or projects. The Azure Policy built-in
  (fe3fd216) and Checkov CKV_AZURE_238 cover all accounts but not projects.
- NET-011 is the weakest adapt: NSG and DDoS tests exist as PSRule, policy and Defender assessments. It must show
  its scoping (Foundry subnets, DDoS only when public IPs exist) in Phase 4 or fall back to `reuse`.

## Rules set to product-opinion

- FND-NET-010: the platform documents AMPLS as the way Application Insights stays private in a private Foundry
  setup, and documents the Private Only side effects, but nothing requires a private telemetry path. The rule asks
  for an explicit, internally consistent choice (warning in test and prod, info in dev). Property names and access
  modes are verified (`privateLinkScopes@2021-09-01`, `scopedResources.linkedResourceId`, components
  `publicNetworkAccessForIngestion` default Enabled).

## Basis choices to review (not dropped)

- FND-IDN-002 is `platform` (error everywhere): the Foundry docs state 403 errors without the roles. It applies only
  to stores the project uses.
- FND-IDN-006 is `security`, not `platform`, because no source read states the https scheme requirement.
- FND-IDN-005 and FND-NET-011 include product choices inside verified rules (Foundry Owner and Foundry Account Owner
  in the inventory; the agent subnet reported at info). They are called out in each rule's notes.

## Unverified

- FND-IDN-002: the Cosmos DB Built-in Data Contributor SQL role GUID. The Azure RBAC built-in roles reference does not
  list Cosmos data-plane roles; `azure-databases-docs` was not accessible. Tried: grep of REFS and rest-api-specs
  (the Cosmos spec shows only `roleDefinitionId` placeholders).
- FND-IDN-002: Foundry Agent Consumer role GUID `eed3b665-ab3a-47b6-8f48-c9382fb1dad6` appears only in
  `rbac-foundry.md`, not in the built-in roles reference. Not used in any rule evidence.
- FND-IDN-002: container naming differs between the standard agent setup article (`<workspaceId>-azureml-blobstore`,
  `<workspaceId>-azureml-agent`) and the administrator guide (`azureml-blobstore`, `agents-blobstore`); Storage
  Account Contributor is listed for the system-assigned identity in one article only (reported as info).
- FND-IDN-003: whether omitting principalType always fails (sources say "can fail in some cases"); which
  principalType a Foundry agent identity needs in a template (spec has AgentServicePrincipal; Foundry docs use the
  CLI `--assignee agentIdentityId`).
- FND-IDN-006: an explicit https requirement for the issuer. Entra docs say "a URL that complies with the OIDC
  Discovery spec" and all examples are https; openid.net was blocked by the egress proxy. Subject formats for Azure
  Pipelines, GitLab and Terraform Cloud issuers are not checked beyond non-empty and no wildcard.
- FND-IDN-005: Defender for Cloud recommendation reference and Advisor equivalents; whether schedule instances are
  readable without a PIM licence.
- FND-IDN-004: Defender CSPM overprivileged identity recommendations (no Defender reference in the clones).
- FND-NET-010: Foundry Doctor config key for declaring public telemetry (Phase 4); full DNS zone set for the
  azuremonitor group ID (the Foundry article lists four zones; `private-link-configure.md` not reviewed).
- FND-NET-011: NSG support or requirements on the agent subnet (delegated to Microsoft.App/environments); the
  effect of `privateEndpointNetworkPolicies` on NSG rules for private endpoints (value
  `NetworkSecurityGroupEnabled` exists in the 2026-01-01 spec; the explaining article was not available).
- The Azure Monitor private link articles moved from `azure-docs` to `azure-monitor-docs`; the Learn URLs recorded
  in NET-010 are the public paths and could not be fetched (Learn is blocked).

## Proposed new rules (fragment only, no catalogue files)

NET:
1. Hosted agent private registry feasibility (deployment feasibility). The hosted agent permissions article says a
   private container registry (private endpoint with public access disabled) is supported only for projects created
   after June 25, 2026; the agent endpoint itself stays publicly addressable in the virtual network preview. Check the
   project creation date and ACR network settings, and surface the documented limitation list.
2. Foundry managed network target readiness (supportability). Managed virtual network private endpoints to customer
   resources list supported targets (including Application Insights through AMPLS); flag a private endpoint outbound
   rule whose target type is not in the documented list.
3. Private DNS single-owner check for Azure Monitor (supportability). The design article warns that multiple AMPLS
   objects sharing DNS override each other; a standalone live check of zone link ownership across subscriptions would
   extend NET-010.

IDN:
1. Managed network connection approver (deployment feasibility). The managed virtual network article requires the
   Foundry account managed identity to hold Azure AI Enterprise Network Connection Approver
   (`b556d68e-0be0-4f35-a333-ad7ee1ce17ea`, the role name is in the built-in roles reference, but this GUID appears only in the Foundry articles) on a customer target to create
   and approve managed private endpoints. Check the assignment before deployment.
2. Federated credential serial deployment (deployment feasibility). The Entra considerations article says creating
   several federated credentials under one user-assigned identity concurrently returns 409; check `dependsOn` chains or
   `mode: serial` in the template.
3. Agent identity role carry-over after publish (release readiness). The agent identity article says a published agent
   receives a new `agentIdentityId` and project identity roles do not carry over; compare role assignments before and
   after publish in live mode (overlaps FND-RUN-001, so it may become a RUN rule).
