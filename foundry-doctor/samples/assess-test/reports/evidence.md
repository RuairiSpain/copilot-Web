# Foundry Doctor WAF evidence pack

> Audit-oriented output. Passed, failed, suppressed, baselined, attested, not-applicable and skipped evidence remains visible. This is not a certification.

- Profile: `foundry-test`
- Decision: Proceed with conditions\: mandatory controls need remediation or manual review\.
- Tool version: `0.1.0-dev`
- Generated: `2026-10-18T00:00:00Z`
- Assessed scope: azure\.yaml and compiled infrastructure evidence where available

## Summary

| State | Count |
|---|---|
| FAIL | 0 |
| WARNING | 1 |
| UNKNOWN | 6 |
| QUESTION | 2 |
| SKIPPED | 65 |
| ATTESTED | 0 |
| INFO | 0 |
| NOT_APPLICABLE | 4 |
| PASS | 0 |

## FAIL

None.

## WARNING

### Evaluations run before promotion

- Rule: `FND-OPS-008` (version 3)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: deterministic
- Why this state: An active warning\-severity finding remains for the selected profile\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/agents/how-to/set-up-ci-cd-cli> (verified `2026-10-04`)
- finding: no pipeline run step runs azd ai agent eval run in \.github/workflows, \.azdo/pipelines and \.azuredevops/pipelines \(\.yml and \.yaml\) (severity warning) fingerprint `fef1dea773859487c216cd6e3b8e3b1e`
- finding: eval\.yaml is missing from the project directory (severity warning) fingerprint `aaf47d8582ace29fe0b38d960dc2b856`

## UNKNOWN

### Valid YAML, duplicate\-key detection and compatible azure\.ai\.\* schema

- Rule: `FND-CFG-001` (version 7)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: none
- Why this state: No evidence was recorded for this control\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-05`)

### Unique service names by kind

- Rule: `FND-CFG-002` (version 2)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: none
- Why this state: No evidence was recorded for this control\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-04`)

### Preview/retired fields are version\-aware

- Rule: `FND-CFG-011` (version 5)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: none
- Why this state: No evidence was recorded for this control\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-04`)

### All inter\-service references resolve

- Rule: `FND-CFG-003` (version 4)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: No evidence was recorded for this control\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml> (verified `2026-10-04`)

### No raw secrets in YAML/Bicep/environment blocks

- Rule: `FND-CFG-005` (version 4)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: No evidence was recorded for this control\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-05`)

### Outbound endpoints use HTTPS and reject unsafe local/metadata targets

- Rule: `FND-CFG-007` (version 2)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: No evidence was recorded for this control\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/agents/how-to/tools/model-context-protocol> (verified `2026-10-04`)

## QUESTION

### Application Insights connected

- Rule: `FND-OPS-002` (version 1)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: none
- Why this state: No requirement or objective is declared \(no\-explicit\-observability\-or\-server\-tracing\-requirement\); the control needs a team decision\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/observability/how-to/trace-agent-setup> (verified `2026-10-04`)
- skipped: no\-explicit\-observability\-or\-server\-tracing\-requirement

### Multi\-region/DR plan question

- Rule: `FND-REL-007` (version 1)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: No answer recorded \(disasterRecovery\.declared\)\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`)
- skipped: not\-implemented

## SKIPPED

### Budget and alerts exist

- Rule: `FND-COST-001` (version 4)
- Pillar: Cost Optimization
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project opinion, not Microsoft guidance; context source: <https://github.com/Azure/azure-rest-api-specs/tree/main/specification/consumption/resource-manager/Microsoft.Consumption/Consumption/stable/2023-11-01> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Dev avoids production\-sized fixed SKUs

- Rule: `FND-COST-002` (version 4)
- Pillar: Cost Optimization
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types> (verified `2026-10-18`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Every variable has an environment or infrastructure producer

- Rule: `FND-CFG-006` (version 5)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: no\-environment\-selected\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-04`)
- skipped: input\-unavailable\: no\-environment\-selected

### Signed into matching tenant/subscription

- Rule: `FND-DEP-001` (version 1)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/subscriptions/stable/2022-12-01/subscriptions.json> (verified `2026-10-04`)
- skipped: not\-implemented

### Required providers registered

- Rule: `FND-DEP-002` (version 3)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/resources/stable/2025-04-01/resources.json> (verified `2026-10-04`)
- skipped: not\-implemented

### Likely policy denials identified

- Rule: `FND-DEP-009` (version 3)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/policyinsights/resource-manager/Microsoft.PolicyInsights/PolicyInsights/stable/2024-10-01/openapi.json> (verified `2026-10-04`)
- skipped: not\-implemented

### Diagnostic settings to Log Analytics

- Rule: `FND-OPS-001` (version 2)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/azure-monitor/reference/supported-logs/microsoft-cognitiveservices-accounts-logs> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Baseline alerts exist

- Rule: `FND-OPS-003` (version 2)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/how-to/stay-informed-service-health> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Required tags exist

- Rule: `FND-OPS-004` (version 2)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-resource-manager/management/tag-resources> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Resource\-name provider constraints

- Rule: `FND-OPS-005` (version 2)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Log retention meets policy

- Rule: `FND-OPS-006` (version 2)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/logs/data-retention-configure> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### CI/CD pipeline exists

- Rule: `FND-OPS-007` (version 3)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: uncertain\: a pipeline run step uses shell constructs that were not followed \(heredoc, function, eval or sh \-c, computed azd arguments\), so it may invoke azd provision, deploy or up\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/configure-devops-pipeline> (verified `2026-10-05`)
- skipped: uncertain\: a pipeline run step uses shell constructs that were not followed \(heredoc, function, eval or sh \-c, computed azd arguments\), so it may invoke azd provision, deploy or up

### Diagnostic logs are arriving

- Rule: `FND-RUN-007` (version 4)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/foundry/how-to/diagnostic-logging> (verified `2026-10-04`)
- skipped: not\-implemented

### Prompt model or hosted source/image is valid

- Rule: `FND-CFG-004` (version 4)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: uncertain\: assistant\: no Dockerfile under \"\./agents/assistant\" and no image, codeConfiguration, runtime, startupCommand or skill; the agent source is unclear\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-05`)
- skipped: uncertain\: assistant\: no Dockerfile under \"\./agents/assistant\" and no image, codeConfiguration, runtime, startupCommand or skill; the agent source is unclear

### Azure region values are valid

- Rule: `FND-CFG-012` (version 4)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: no\-environment\-selected\.
- Project opinion, not Microsoft guidance; context source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/subscriptions/stable/2022-12-01/subscriptions.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: no\-environment\-selected

### Model/version/SKU offered in target region

- Rule: `FND-DEP-004` (version 1)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- skipped: not\-implemented

### Quota covers requested capacity

- Rule: `FND-DEP-005` (version 2)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/foundry/openai/how-to/quota> (verified `2026-10-04`)
- skipped: not\-implemented

### No blocking soft\-deleted resource/name

- Rule: `FND-DEP-006` (version 2)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/ai-services/recover-purge-resources> (verified `2026-10-04`)
- skipped: not\-implemented

### Globally unique names available

- Rule: `FND-DEP-007` (version 2)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- skipped: not\-implemented

### What\-if has no unexpected delete

- Rule: `FND-DEP-008` (version 4)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/deployments/stable/2026-06-01/deployments.json> (verified `2026-10-04`)
- skipped: not\-implemented

### Locks do not block change

- Rule: `FND-DEP-010` (version 3)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/azure-resource-manager/management/lock-resources> (verified `2026-10-04`)
- skipped: not\-implemented

### Subnet delegation/capacity/use is compatible

- Rule: `FND-DEP-011` (version 2)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- skipped: not\-implemented

### Target region supports selected setup

- Rule: `FND-DEP-012` (version 2)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/limits-quotas-regions> (verified `2026-10-04`)
- skipped: not\-implemented

### Role assignments use object IDs and principalType

- Rule: `FND-IDN-003` (version 5)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/role-based-access-control/role-assignments-template> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Private endpoint DNS zone groups and VNet links

- Rule: `FND-NET-003` (version 3)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/private-link/private-endpoint-dns> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Agent subnet delegation and sizing

- Rule: `FND-NET-004` (version 3)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Private\-mode VNet/region consistency

- Rule: `FND-NET-005` (version 4)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Private address space and overlap

- Rule: `FND-NET-006` (version 3)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Search SKU supports private endpoints

- Rule: `FND-NET-008` (version 3)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/search/service-create-private-endpoint> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Search replica/SLA posture

- Rule: `FND-REL-001` (version 1)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/search/search-limits-quotas-capacity> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Storage redundancy posture

- Rule: `FND-REL-002` (version 1)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Cosmos zone redundancy and continuous backup

- Rule: `FND-REL-003` (version 1)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Delete locks on stateful services

- Rule: `FND-REL-004` (version 2)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Search sizing within platform limits

- Rule: `FND-REL-005` (version 1)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Provisioned throughput spillover recommendation

- Rule: `FND-REL-008` (version 1)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/openai/how-to/spillover-traffic-management> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### APIM availability tier recommendation

- Rule: `FND-REL-009` (version 1)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/api-management/api-management-features> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Private DNS resolves from an appropriate vantage point

- Rule: `FND-RUN-002` (version 4)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- skipped: not\-implemented

### Capability host and project connections healthy

- Rule: `FND-RUN-003` (version 3)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-17`)
- skipped: not\-implemented

### Model deployment state and throttling signal

- Rule: `FND-RUN-004` (version 4)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/reference/supported-metrics/microsoft-cognitiveservices-accounts-metrics> (verified `2026-10-18`)
- skipped: not\-implemented

### Agent tools/connections respond

- Rule: `FND-RUN-005` (version 4)
- Pillar: Reliability
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Project opinion, not Microsoft guidance; context source: <https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/project/service_target_agent.go> (verified `2026-10-18`)
- skipped: not\-implemented

### Search index/documents/vector/indexer health

- Rule: `FND-RUN-006` (version 5)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/data-plane/Search/stable/2026-04-01/search.json> (verified `2026-10-17`)
- skipped: not\-implemented

### Deployer can create resources and role assignments

- Rule: `FND-DEP-003` (version 2)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleDefinitionsCalls.json> (verified `2026-10-04`)
- skipped: not\-implemented

### Managed identities used for Foundry and workloads

- Rule: `FND-IDN-001` (version 3)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Project identity has required data roles

- Rule: `FND-IDN-002` (version 1)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup> (verified `2026-10-04`)
- skipped: not\-implemented

### No broad Owner/Contributor for application identities

- Rule: `FND-IDN-004` (version 6)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/role-based-access-control/role-assignments-steps> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Standing human admin access reviewed

- Rule: `FND-IDN-005` (version 2)
- Pillar: Security
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/role-based-access-control/best-practices> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Federated credentials use HTTPS issuer and specific subject

- Rule: `FND-IDN-006` (version 1)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://openid.net/specs/openid-connect-discovery-1_0.html#IssuerDiscovery> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Foundry public access/private endpoint posture

- Rule: `FND-NET-001` (version 4)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Dependent services public access/default\-deny/private endpoints

- Rule: `FND-NET-002` (version 3)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Restricted mode has allowed IP range

- Rule: `FND-NET-007` (version 2)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/foundry/how-to/configure-private-link> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Azure Monitor private\-link choice is explicit

- Rule: `FND-NET-010` (version 3)
- Pillar: Security
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-design> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### NSG/DDoS recommendations

- Rule: `FND-NET-011` (version 3)
- Pillar: Security
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.VNET.UseNSGs/> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Data\-residency regions and deployment SKU

- Rule: `FND-OPS-009` (version 2)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: profile\-key\-missing\:dataResidency\.scope\.
- Source: <https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types> (verified `2026-10-04`)
- skipped: profile\-key\-missing\:dataResidency\.scope

### Model allow/deny lists remain consistent

- Rule: `FND-OPS-010` (version 2)
- Pillar: Security
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: profile\-key\-missing\:models\.allow\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/how-to/model-deployment-policy> (verified `2026-10-04`)
- skipped: profile\-key\-missing\:models\.allow

### Effective project identity access

- Rule: `FND-RUN-001` (version 4)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup> (verified `2026-10-04`)
- skipped: not\-implemented

### Foundry local authentication disabled

- Rule: `FND-SEC-001` (version 3)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### AI Search key authentication disabled or RBAC\-only

- Rule: `FND-SEC-002` (version 3)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Cosmos DB local authentication disabled

- Rule: `FND-SEC-003` (version 3)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Storage shared key/anonymous access disabled; HTTPS/TLS required

- Rule: `FND-SEC-004` (version 2)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-06-01/openapi.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Firewall rules reject private/open ranges

- Rule: `FND-SEC-006` (version 3)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://learn.microsoft.com/azure/storage/common/storage-network-security-limitations> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Relevant Defender plans enabled

- Rule: `FND-SEC-008` (version 2)
- Pillar: Security
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/security/resource-manager/Microsoft.Security/Security/stable/2026-07-01/pricings.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Policy coverage and compliance evidence

- Rule: `FND-SEC-009` (version 2)
- Pillar: Security
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Project opinion, not Microsoft guidance; context source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/policy/stable/2025-01-01/policyAssignments.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Customer\-managed keys informational

- Rule: `FND-SEC-010` (version 2)
- Pillar: Security
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- skipped: input\-unavailable\: compiled ARM model unavailable

### Controlled outbound egress informational

- Rule: `FND-SEC-011` (version 1)
- Pillar: Security
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: unsupported\-version\.
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- skipped: unsupported\-version

### Secrets not emitted as Bicep outputs

- Rule: `FND-SEC-014` (version 2)
- Pillar: Security
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: unsupported\-version\.
- Source: <https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter-rule-outputs-should-not-contain-secrets> (verified `2026-10-16`)
- skipped: unsupported\-version

## ATTESTED

None.

## INFO

None.

## NOT_APPLICABLE

### Environments do not unintentionally share resource groups

- Rule: `FND-ENV-001` (version 5)
- Pillar: Operational Excellence
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Not applicable\: not\-applicable\: fewer than two azd environments\.
- Source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview> (verified `2026-10-05`)
- skipped: not\-applicable\: fewer than two azd environments

### Environment\-bound names/IDs/rules were not copied unchanged

- Rule: `FND-ENV-002` (version 4)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: deterministic
- Why this state: Not applicable\: not\-applicable\: fewer than two azd environments\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview> (verified `2026-10-05`)
- skipped: not\-applicable\: fewer than two azd environments

### Informational difference summary

- Rule: `FND-ENV-004` (version 3)
- Pillar: Operational Excellence
- Mandatory: false
- Evidence basis: deterministic
- Why this state: Not applicable\: not\-applicable\: fewer than two azd environments\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview> (verified `2026-10-05`)
- skipped: not\-applicable\: fewer than two azd environments

### Prod does not reference non\-prod resource IDs

- Rule: `FND-ENV-003` (version 4)
- Pillar: Reliability
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Not applicable\: not\-applicable\: fewer than two azd environments\.
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview> (verified `2026-10-05`)
- skipped: not\-applicable\: fewer than two azd environments

## PASS

None.

## Costly recommendations (optional)

- `FND-OPS-002` Application Insights connected (QUESTION): Workspace\-based Application Insights data is billed for ingestion and retention by the Log Analytics workspace that stores it\. (<https://learn.microsoft.com/azure/azure-monitor/logs/cost-logs>, verified `2026-10-18`)
- `FND-OPS-006` Log retention meets policy (SKIPPED): Log Analytics charges for data retention in addition to ingestion; longer retention adds cost\. (<https://learn.microsoft.com/azure/azure-monitor/logs/cost-logs>, verified `2026-10-18`)
- `FND-REL-001` Search replica/SLA posture (SKIPPED): Dedicated Azure AI Search bills per search unit \(replicas x partitions\); adding replicas raises cost\. (<https://learn.microsoft.com/azure/search/search-sku-manage-costs>, verified `2026-10-18`)
- `FND-REL-002` Storage redundancy posture (SKIPPED): LRS is the lowest\-cost redundancy option; ZRS and GZRS trade higher cost for availability\. (<https://learn.microsoft.com/azure/storage/common/storage-redundancy>, verified `2026-10-18`)
- `FND-REL-003` Cosmos zone redundancy and continuous backup (SKIPPED): Continuous backup at the 30\-day or 35\-day tier carries a monthly backup storage charge and every restore is charged; the 7\-day tier has no backup storage charge\. The cost of zone redundancy is not cited here\. (<https://learn.microsoft.com/azure/cosmos-db/continuous-backup-restore-introduction>, verified `2026-10-18`)
- `FND-NET-010` Azure Monitor private\-link choice is explicit (SKIPPED): An Azure Monitor Private Link Scope is reached through a private endpoint, and Azure Monitor refers its cost to Azure Private Link pricing\. (<https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-security>, verified `2026-10-18`)
- `FND-NET-011` NSG/DDoS recommendations (SKIPPED): DDoS Network Protection and DDoS IP Protection are priced per protected IP or per 100 protected IPs; only DDoS infrastructure protection is at no additional cost\. (<https://learn.microsoft.com/azure/ddos-protection/ddos-protection-sku-comparison>, verified `2026-10-18`)
- `FND-SEC-008` Relevant Defender plans enabled (SKIPPED): Defender for Cloud plans beyond foundational CSPM are paid plans; charges begin after the 30\-day trial or usage limit\. (<https://learn.microsoft.com/azure/defender-for-cloud/connect-azure-subscription>, verified `2026-10-18`)
- `FND-SEC-010` Customer\-managed keys informational (SKIPPED): Customer\-managed keys require a billable Azure Key Vault \(stated for Azure AI Search; the same Key Vault dependency applies to each service listed\)\. (<https://learn.microsoft.com/azure/search/search-sku-manage-costs>, verified `2026-10-18`)
- `FND-SEC-011` Controlled outbound egress informational (SKIPPED): FQDN outbound rules in Allow Only Approved Outbound mode create a managed Azure Firewall, which adds Azure Firewall charges\. (<https://learn.microsoft.com/azure/foundry/how-to/managed-virtual-network>, verified `2026-10-18`)

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
