# Foundry Doctor WAF owner summary

> Scope-limited architecture summary. This is not a certification or a complete Well-Architected review.

- Profile: `foundry-prod`
- Decision: Proceed with conditions\: mandatory controls need remediation or manual review\.
- Assessed scope: azure\.yaml and compiled infrastructure evidence where available

## Summary

| State | Count |
|---|---|
| FAIL | 0 |
| WARNING | 1 |
| UNKNOWN | 0 |
| QUESTION | 0 |
| SKIPPED | 68 |
| ATTESTED | 1 |
| INFO | 0 |
| NOT_APPLICABLE | 4 |
| PASS | 6 |

## Top five actions

- **Evaluations run before promotion** (WARNING, Operational Excellence, optional): Generate the evaluation recipe with azd ai agent eval generate, commit eval\.yaml, and run azd ai agent eval run in the pipeline against the candidate version; promote to production only after the evaluation passes your thresholds\. Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/agents/how-to/set-up-ci-cd-cli> (verified `2026-10-04`).

## Costly recommendations (optional)

These optional controls add Azure cost. They are kept out of the top actions; decide them against your requirements.

- **Application Insights connected** (SKIPPED, Operational Excellence): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: Workspace\-based Application Insights data is billed for ingestion and retention by the Log Analytics workspace that stores it\. (<https://learn.microsoft.com/azure/azure-monitor/logs/cost-logs>, verified `2026-10-18`). Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/observability/how-to/trace-agent-setup> (verified `2026-10-04`).
- **Log retention meets policy** (SKIPPED, Operational Excellence): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: Log Analytics charges for data retention in addition to ingestion; longer retention adds cost\. (<https://learn.microsoft.com/azure/azure-monitor/logs/cost-logs>, verified `2026-10-18`). Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/logs/data-retention-configure> (verified `2026-10-04`).
- **Search replica/SLA posture** (SKIPPED, Reliability): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: Dedicated Azure AI Search bills per search unit \(replicas x partitions\); adding replicas raises cost\. (<https://learn.microsoft.com/azure/search/search-sku-manage-costs>, verified `2026-10-18`). Source: <https://learn.microsoft.com/azure/search/search-limits-quotas-capacity> (verified `2026-10-04`).
- **Storage redundancy posture** (SKIPPED, Reliability): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: LRS is the lowest\-cost redundancy option; ZRS and GZRS trade higher cost for availability\. (<https://learn.microsoft.com/azure/storage/common/storage-redundancy>, verified `2026-10-18`). Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`).
- **Cosmos zone redundancy and continuous backup** (SKIPPED, Reliability): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: Continuous backup beyond the 7\-day tier carries a monthly backup storage charge and every restore is charged; the 7\-day tier has no backup storage charge\. The cost of zone redundancy is not cited here\. (<https://learn.microsoft.com/azure/cosmos-db/continuous-backup-restore-introduction>, verified `2026-10-18`). Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`).
- **Azure Monitor private\-link choice is explicit** (SKIPPED, Security): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: An Azure Monitor Private Link Scope is reached through a private endpoint, and Azure Monitor refers its cost to Azure Private Link pricing\. (<https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-security>, verified `2026-10-18`). Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-design> (verified `2026-10-04`).
- **NSG/DDoS recommendations** (SKIPPED, Security): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: DDoS Network Protection and DDoS IP Protection are priced per protected IP or per 100 protected IPs; only DDoS infrastructure protection is at no additional cost\. (<https://learn.microsoft.com/azure/ddos-protection/ddos-protection-sku-comparison>, verified `2026-10-18`). Source: <https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.VNET.UseNSGs/> (verified `2026-10-04`).
- **Relevant Defender plans enabled** (SKIPPED, Security): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: Defender for Cloud plans beyond foundational CSPM are paid plans; charges begin after the 30\-day trial or usage limit\. (<https://learn.microsoft.com/azure/defender-for-cloud/connect-azure-subscription>, verified `2026-10-18`). Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/security/resource-manager/Microsoft.Security/Security/stable/2026-07-01/pricings.json> (verified `2026-10-04`).
- **Customer\-managed keys informational** (SKIPPED, Security): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: Customer\-managed keys require a billable Azure Key Vault \(stated for Azure AI Search; the same Key Vault dependency applies to each service listed\)\. (<https://learn.microsoft.com/azure/search/search-sku-manage-costs>, verified `2026-10-18`). Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`).
- **Controlled outbound egress informational** (SKIPPED, Security): Obtain evidence for this control before relying on it\. Skipped\: input\-unavailable\: compiled ARM model unavailable\. Cost basis: FQDN outbound rules in Allow Only Approved Outbound mode create a managed Azure Firewall, which adds Azure Firewall charges\. (<https://learn.microsoft.com/azure/foundry/how-to/managed-virtual-network>, verified `2026-10-18`). Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`).

## Team-attested answers

Recorded by the team in Foundry Doctor configuration, including settings delegated to Azure Policy. Foundry Doctor did not verify them. A mandatory control that rests on an attestation keeps the decision at "Proceed with conditions".

- Multi\-region/DR plan question (ATTESTED, optional): `true; reference: https://contoso.example/dr-plan`

## Unassessed scope

- Budget and alerts exist (SKIPPED, Cost Optimization): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Dev avoids production\-sized fixed SKUs (SKIPPED, Cost Optimization): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Every variable has an environment or infrastructure producer (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: no\-environment\-selected\.
- Signed into matching tenant/subscription (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- Required providers registered (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- Likely policy denials identified (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- Diagnostic settings to Log Analytics (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Application Insights connected (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Baseline alerts exist (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Required tags exist (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Resource\-name provider constraints (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Log retention meets policy (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- CI/CD pipeline exists (SKIPPED, Operational Excellence): Skipped\: uncertain\: a pipeline run step uses shell constructs that were not followed \(heredoc, function, eval or sh \-c, computed azd arguments\), so it may invoke azd provision, deploy or up\.
- Diagnostic logs are arriving (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- Deployed properties versus compiled template drift (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- Prompt model or hosted source/image is valid (SKIPPED, Reliability): Skipped\: uncertain\: assistant\: no Dockerfile under \"\./agents/assistant\" and no image, codeConfiguration, runtime, startupCommand or skill; the agent source is unclear\.
- Azure region values are valid (SKIPPED, Reliability): Skipped\: input\-unavailable\: no\-environment\-selected\.
- Model/version/SKU offered in target region (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Quota covers requested capacity (SKIPPED, Reliability): Skipped\: not\-implemented\.
- No blocking soft\-deleted resource/name (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Globally unique names available (SKIPPED, Reliability): Skipped\: not\-implemented\.
- What\-if has no unexpected delete (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Locks do not block change (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Subnet delegation/capacity/use is compatible (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Target region supports selected setup (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Role assignments use object IDs and principalType (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Private endpoint DNS zone groups and VNet links (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Agent subnet delegation and sizing (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Private\-mode VNet/region consistency (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Private address space and overlap (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Search SKU supports private endpoints (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Search replica/SLA posture (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Storage redundancy posture (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Cosmos zone redundancy and continuous backup (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Delete locks on stateful services (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Search sizing within platform limits (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Provisioned throughput spillover recommendation (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- APIM availability tier recommendation (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Private DNS resolves from an appropriate vantage point (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Capability host and project connections healthy (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Model deployment state and throttling signal (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Agent tools/connections respond (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Search index/documents/vector/indexer health (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Deployer can create resources and role assignments (SKIPPED, Security): Skipped\: not\-implemented\.
- Managed identities used for Foundry and workloads (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project identity has required data roles (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- No broad Owner/Contributor for application identities (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Standing human admin access reviewed (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Federated credentials use HTTPS issuer and specific subject (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Foundry public access/private endpoint posture (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Dependent services public access/default\-deny/private endpoints (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Restricted mode has allowed IP range (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Azure Monitor private\-link choice is explicit (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- NSG/DDoS recommendations (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Data\-residency regions and deployment SKU (SKIPPED, Security): Skipped\: profile\-key\-missing\:dataResidency\.scope\.
- Model allow/deny lists remain consistent (SKIPPED, Security): Skipped\: profile\-key\-missing\:models\.allow\.
- Effective project identity access (SKIPPED, Security): Skipped\: not\-implemented\.
- Foundry local authentication disabled (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- AI Search key authentication disabled or RBAC\-only (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Cosmos DB local authentication disabled (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Storage shared key/anonymous access disabled; HTTPS/TLS required (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Firewall rules reject private/open ranges (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- RAI/content filter bound to chat deployments (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Relevant Defender plans enabled (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Policy coverage and compliance evidence (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Customer\-managed keys informational (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Controlled outbound egress informational (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Secrets not emitted as Bicep outputs (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.

## Limitations

- This report maps available Foundry Doctor evidence to selected Azure Well\-Architected aligned controls\. It is not a certification and does not claim complete WAF compliance\.
- Controls without complete deterministic evidence remain visible as UNKNOWN, QUESTION or SKIPPED rather than being treated as passing; a partly skipped evaluation is UNKNOWN, never PASS\.
- ATTESTED controls rest on an answer the team recorded in Foundry Doctor configuration\. Foundry Doctor did not verify the answer\.
- Control IDs are Foundry Doctor rule IDs\. They are not mapped to WAF checklist codes in this release\. Pillar assignments come from the rule catalogue and are project judgement, not Microsoft guidance\.
- A suppressed or baselined finding is shown as WARNING and stays visible in the evidence pack\. It does not by itself produce a No\-go decision, so the decision can be Proceed with conditions while accepted failures remain\.
- The decision wording and the top\-action ranking are project opinion, not Microsoft guidance\.
- PSRule for Azure, Azure Advisor and Microsoft Defender for Cloud recommendations were not imported\. Overlapping external check IDs are listed for reference only \(ADR\-020, ADR\-026\)\.
- Deployed Azure state was not queried\: assess waf runs offline\. Controls that need deployment or runtime evidence and were not evaluated\: FND\-COST\-001, FND\-DEP\-001, FND\-DEP\-002, FND\-DEP\-003, FND\-DEP\-004, FND\-DEP\-005, FND\-DEP\-006, FND\-DEP\-007, FND\-DEP\-008, FND\-DEP\-009, FND\-DEP\-010, FND\-DEP\-011, FND\-DEP\-012, FND\-IDN\-002, FND\-IDN\-005, FND\-NET\-006, FND\-OPS\-009, FND\-RUN\-001, FND\-RUN\-002, FND\-RUN\-003, FND\-RUN\-004, FND\-RUN\-005, FND\-RUN\-006, FND\-RUN\-007, FND\-RUN\-008, FND\-SEC\-008, FND\-SEC\-009\.
- Compiled infrastructure \(Bicep to ARM\) was not available, so template\-dependent controls are SKIPPED\.
- 28 catalogue rules are excluded from the WAF view with a recorded reason \(internal/assess/waf/mapping\.yaml\)\.
