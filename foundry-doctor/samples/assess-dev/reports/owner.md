# Foundry Doctor WAF owner summary

> Scope-limited architecture summary. This is not a certification or a complete Well-Architected review.

- Profile: `foundry-dev`
- Decision: Proceed with conditions\: mandatory controls need remediation or manual review\.
- Assessed scope: azure\.yaml and compiled infrastructure evidence where available

## Summary

| State | Count |
|---|---|
| FAIL | 0 |
| WARNING | 0 |
| UNKNOWN | 6 |
| QUESTION | 2 |
| SKIPPED | 64 |
| ATTESTED | 0 |
| INFO | 0 |
| NOT_APPLICABLE | 6 |
| PASS | 0 |

## Top five actions

- **Valid YAML, duplicate\-key detection and compatible azure\.ai\.\* schema** (UNKNOWN, Operational Excellence, mandatory): Fix the YAML syntax or the reported schema violation\. Add the yaml\-language\-server schema comment to azure\.yaml so the editor flags the same errors, and install azure\.ai\.agents and azure\.ai\.projects at the versions declared in requiredVersions\. Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-05`).
- **Unique service names by kind** (UNKNOWN, Operational Excellence, mandatory): Give separate logical agents distinct names\. If the services intentionally represent versions of one agent, keep one service definition and deploy the intended version through that service\. Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-04`).
- **Preview/retired fields are version\-aware** (UNKNOWN, Operational Excellence, mandatory): Move to the unified service\-level shape\: put env on the agent service, use kind voice, use host azure\.ai\.project instead of microsoft\.foundry, drop fields the hosted\-agent create request ignores, and declare requiredVersions for extension versions verified by this catalogue\. Do not infer a field\'s introduction version from the current schema\. Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-04`).
- **All inter\-service references resolve** (UNKNOWN, Reliability, mandatory): Make each referenced name match a service key defined under services, and add toolbox and connection services to the consuming agent uses list as well as its toolboxes or connections list\. Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml> (verified `2026-10-04`).
- **No raw secrets in YAML/Bicep/environment blocks** (UNKNOWN, Security, mandatory): Store the secret in the azd environment as a secret \(azd env set\-secret\) or in Key Vault, and reference it with \$\{VAR\} in azure\.yaml\. In Bicep, mark secret parameters \@secure\(\) with no default and never output them\. Prefer Entra ID or managed identity authentication where the target supports it\. Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-05`).

## Costly recommendations (optional)

These optional controls add Azure cost. They are kept out of the top actions; decide them against your requirements.

- **Application Insights connected** (QUESTION, Operational Excellence): If server\-side tracing or hosted\-agent telemetry is a declared requirement, connect an Application Insights resource to the Foundry project \(a connection with category AppInsights\) and link the component to a Log Analytics workspace\. Keep exactly one AppInsights connection per scope\. Choose sampling, retention and daily\-cap settings deliberately\: telemetry ingestion and retention incur cost, so a project that does not require this observability path should not be forced to enable it\. Cost basis: Workspace\-based Application Insights data is billed for ingestion and retention by the Log Analytics workspace that stores it\. (<https://learn.microsoft.com/azure/azure-monitor/logs/cost-logs>, verified `2026-10-18`). Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/observability/how-to/trace-agent-setup> (verified `2026-10-04`).
- **Log retention meets policy** (SKIPPED, Operational Excellence): Set workspace retentionInDays to at least the value your policy requires, or set totalRetentionInDays on the tables that hold Foundry diagnostics and traces to keep older data in low\-cost long\-term retention\. Retention beyond the free period is billed\. Cost basis: Log Analytics charges for data retention in addition to ingestion; longer retention adds cost\. (<https://learn.microsoft.com/azure/azure-monitor/logs/cost-logs>, verified `2026-10-18`). Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/logs/data-retention-configure> (verified `2026-10-04`).
- **Search replica/SLA posture** (SKIPPED, Reliability): Set properties\.replicaCount to at least 2 if the application only queries, or at least 3 if it also updates indexes online\. Each replica adds search units and cost, so keep dev and test at 1 unless you are testing failover behaviour\. Cost basis: Dedicated Azure AI Search bills per search unit \(replicas x partitions\); adding replicas raises cost\. (<https://learn.microsoft.com/azure/search/search-sku-manage-costs>, verified `2026-10-18`). Source: <https://learn.microsoft.com/azure/search/search-limits-quotas-capacity> (verified `2026-10-04`).
- **Storage redundancy posture** (SKIPPED, Reliability): Use Standard\_ZRS at minimum for storage that holds project or agent data, and Standard\_GZRS for Agent Service standard\-mode storage as recommended by the Foundry resiliency guidance\. SKU changes on an existing account are a migration, so decide at design time\. Note the Foundry multi\-region section says default storage failover with GRS/GZRS is not supported for Foundry projects, so geo\-redundancy there protects data recovery, not automatic project failover\. Cost basis: LRS is the lowest\-cost redundancy option; ZRS and GZRS trade higher cost for availability\. (<https://learn.microsoft.com/azure/storage/common/storage-redundancy>, verified `2026-10-18`). Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`).
- **Cosmos zone redundancy and continuous backup** (SKIPPED, Reliability): Set isZoneRedundant true on each location in regions that support availability zones, and set backupPolicy\.type Continuous \(7\-day or 30\-day tier, per the Foundry guidance\) before an incident occurs\. Continuous backup adds cost\. Also consider a second location with automatic failover enabled \(see FND\-REL\-007\)\. Cost basis: Continuous backup at the 30\-day or 35\-day tier carries a monthly backup storage charge and every restore is charged; the 7\-day tier has no backup storage charge\. The cost of zone redundancy is not cited here\. (<https://learn.microsoft.com/azure/cosmos-db/continuous-backup-restore-introduction>, verified `2026-10-18`). Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`).
- **Azure Monitor private\-link choice is explicit** (SKIPPED, Security): Decide the telemetry path\. For private telemetry, create one AMPLS per DNS boundary, add the Application Insights component and its workspace as scoped resources, create the private endpoint with group ID azuremonitor and link the Azure Monitor private DNS zones, and set both access modes explicitly\. Use Open while resources are still being added and switch to Private Only only when all Azure Monitor resources are in the scope\. If you accept public telemetry, record that decision in the Foundry Doctor config so the finding is suppressed with a reason\. Cost basis: An Azure Monitor Private Link Scope is reached through a private endpoint, and Azure Monitor refers its cost to Azure Private Link pricing\. (<https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-security>, verified `2026-10-18`). Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-design> (verified `2026-10-04`).
- **NSG/DDoS recommendations** (SKIPPED, Security): Associate an NSG with each subnet that supports one and review inbound allow rules\. If the design exposes public IP addresses, enable Azure DDoS Protection \(IP Protection on the public IP, or Network Protection on the VNet with a plan\)\. For a fully private design, rely on the default infrastructure protection and do not buy a plan for it\. Cost basis: DDoS Network Protection and DDoS IP Protection are priced per protected IP or per 100 protected IPs; only DDoS infrastructure protection is at no additional cost\. (<https://learn.microsoft.com/azure/ddos-protection/ddos-protection-sku-comparison>, verified `2026-10-18`). Source: <https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.VNET.UseNSGs/> (verified `2026-10-04`).
- **Relevant Defender plans enabled** (SKIPPED, Security): Enable the Defender plan for each resource type in use on the subscription \(Standard tier\), or confirm it is enforced at a parent scope\. Enabling Defender plans has a cost; review pricing before enabling in non\-production\. Cost basis: Defender for Cloud plans beyond foundational CSPM are paid plans; charges begin after the 30\-day trial or usage limit\. (<https://learn.microsoft.com/azure/defender-for-cloud/connect-azure-subscription>, verified `2026-10-18`). Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/security/resource-manager/Microsoft.Security/Security/stable/2026-07-01/pricings.json> (verified `2026-10-04`).
- **Customer\-managed keys informational** (SKIPPED, Security): If your compliance regime requires customer\-managed keys, configure them on each resource listed and use a Key Vault with soft delete and purge protection \(see FND\-SEC\-005\)\. Otherwise no action is needed\. Cost basis: Customer\-managed keys require a billable Azure Key Vault \(stated for Azure AI Search; the same Key Vault dependency applies to each service listed\)\. (<https://learn.microsoft.com/azure/search/search-sku-manage-costs>, verified `2026-10-18`). Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`).
- **Controlled outbound egress informational** (SKIPPED, Security): If you need to control data exfiltration, set the managed network to AllowOnlyApprovedOutbound or restrict outbound access with an allowed FQDN list, and confirm required dependencies are reachable\. Otherwise record the decision\. Cost basis: FQDN outbound rules in Allow Only Approved Outbound mode create a managed Azure Firewall, which adds Azure Firewall charges\. (<https://learn.microsoft.com/azure/foundry/how-to/managed-virtual-network>, verified `2026-10-18`). Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`).

## Unassessed scope

- Budget and alerts exist (SKIPPED, Cost Optimization): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Dev avoids production\-sized fixed SKUs (SKIPPED, Cost Optimization): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Valid YAML, duplicate\-key detection and compatible azure\.ai\.\* schema (UNKNOWN, Operational Excellence): No evidence was recorded for this control\.
- Unique service names by kind (UNKNOWN, Operational Excellence): No evidence was recorded for this control\.
- Every variable has an environment or infrastructure producer (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: no\-environment\-selected\.
- Preview/retired fields are version\-aware (UNKNOWN, Operational Excellence): No evidence was recorded for this control\.
- Signed into matching tenant/subscription (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- Required providers registered (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- Likely policy denials identified (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- Diagnostic settings to Log Analytics (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Application Insights connected (QUESTION, Operational Excellence): No requirement or objective is declared \(no\-explicit\-observability\-or\-server\-tracing\-requirement\); the control needs a team decision\.
- Baseline alerts exist (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Required tags exist (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Resource\-name provider constraints (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Log retention meets policy (SKIPPED, Operational Excellence): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Diagnostic logs are arriving (SKIPPED, Operational Excellence): Skipped\: not\-implemented\.
- All inter\-service references resolve (UNKNOWN, Reliability): No evidence was recorded for this control\.
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
- Multi\-region/DR plan question (QUESTION, Reliability): No answer recorded \(disasterRecovery\.declared\)\.
- Provisioned throughput spillover recommendation (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- APIM availability tier recommendation (SKIPPED, Reliability): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Private DNS resolves from an appropriate vantage point (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Capability host and project connections healthy (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Model deployment state and throttling signal (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Agent tools/connections respond (SKIPPED, Reliability): Skipped\: not\-implemented\.
- Search index/documents/vector/indexer health (SKIPPED, Reliability): Skipped\: not\-implemented\.
- No raw secrets in YAML/Bicep/environment blocks (UNKNOWN, Security): No evidence was recorded for this control\.
- Outbound endpoints use HTTPS and reject unsafe local/metadata targets (UNKNOWN, Security): No evidence was recorded for this control\.
- Deployer can create resources and role assignments (SKIPPED, Security): Skipped\: not\-implemented\.
- Managed identities used for Foundry and workloads (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project identity has required data roles (SKIPPED, Security): Skipped\: not\-implemented\.
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
- Relevant Defender plans enabled (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Policy coverage and compliance evidence (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Customer\-managed keys informational (SKIPPED, Security): Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Controlled outbound egress informational (SKIPPED, Security): Skipped\: unsupported\-version\.
- Secrets not emitted as Bicep outputs (SKIPPED, Security): Skipped\: unsupported\-version\.

## Limitations

- This report maps available Foundry Doctor evidence to selected Azure Well\-Architected aligned controls\. It is not a certification and does not claim complete WAF compliance\.
- Controls without complete deterministic evidence remain visible as UNKNOWN, QUESTION or SKIPPED rather than being treated as passing; a partly skipped evaluation is UNKNOWN, never PASS\.
- ATTESTED controls rest on an answer the team recorded in Foundry Doctor configuration\. Foundry Doctor did not verify the answer\.
- Control IDs are Foundry Doctor rule IDs\. They are not mapped to WAF checklist codes in this release, and pillar assignments come from the rule catalogue\.
- The decision wording and the top\-action ranking are project opinion, not Microsoft guidance\.
- PSRule for Azure, Azure Advisor and Microsoft Defender for Cloud recommendations were not imported\. Overlapping external check IDs are listed for reference only \(ADR\-020, ADR\-026\)\.
- Deployed Azure state was not queried\: assess waf runs offline\. Controls that need deployment or runtime evidence and were not evaluated\: FND\-COST\-001, FND\-DEP\-001, FND\-DEP\-002, FND\-DEP\-003, FND\-DEP\-004, FND\-DEP\-005, FND\-DEP\-006, FND\-DEP\-007, FND\-DEP\-008, FND\-DEP\-009, FND\-DEP\-010, FND\-DEP\-011, FND\-DEP\-012, FND\-IDN\-002, FND\-IDN\-005, FND\-NET\-006, FND\-OPS\-009, FND\-RUN\-001, FND\-RUN\-002, FND\-RUN\-003, FND\-RUN\-004, FND\-RUN\-005, FND\-RUN\-006, FND\-RUN\-007, FND\-SEC\-008, FND\-SEC\-009\.
- Compiled infrastructure \(Bicep to ARM\) was not available, so template\-dependent controls are SKIPPED\.
- 30 catalogue rules are excluded from the WAF view with a recorded reason \(internal/assess/waf/mapping\.yaml\)\.
