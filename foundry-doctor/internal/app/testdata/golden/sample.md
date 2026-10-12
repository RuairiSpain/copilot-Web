# foundry-doctor report

- Tool: foundry-doctor v0.0.0-test
- Profile: `foundry-test`

## Effective policy

| Key | Value |
| --- | --- |
| `policy.allowedExternalScopes` | \[\] \[baseline\] |
| `policy.cost.devMaxCosmosThroughput` | \(not set\) \[unset\] - not set; the Cosmos sub-check is skipped |
| `policy.cost.productionSizedSkuExemptions` | \[\] \[baseline\] |
| `policy.dataResidency.deploymentSkus` | the SKUs whose documented processing scope satisfies the scope \[baseline\] |
| `policy.dataResidency.regions` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.dataResidency.scope` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.disasterRecovery.declared` | \(not set\) \[unset\] - not set; informational question only |
| `policy.environments.development` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.environments.nonProduction` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.environments.production` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.knowledge.requireDocumentLevelAccess` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.logRetention.minimumDays` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.managedByAzurePolicy` | \[\] \[baseline\] |
| `policy.models.allow` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.models.deny` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.monitoring.publicTelemetry` | false \[baseline\] |
| `policy.network.publicAccess` | forbidden \[baseline\] |
| `policy.resourceScope` | same-resource-group \[baseline\] |
| `policy.tags.required` | \(not set\) \[unset\] - not set; dependent rules are skipped with profile-key-missing |
| `policy.tags.resourceTypes` | the rule's own list \[baseline\] |

## Findings (2)

### `azure.yaml`

| Severity | Rule | Position | Resource | Details | Status |
| --- | --- | --- | --- | --- | --- |
| error | `FND-CFG-001` | 3 | api | Evidence: azure.yaml has a problem<br>Recommendation: Fix the YAML syntax or the reported schema violation. Add the yaml-language-server schema comment to azure.yaml so the editor flags the same errors, and install azure.ai.agents and azure.ai.projects at the versions declared in requiredVersions.<br>Fix: \# yaml-language-server: $schema=https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json name: my-agent-project services: ai-project: host: azure.ai.project my-agent: host: azure.ai.agent project: src/my-agent uses: \[ai-project\] kind: hosted<br>[docs](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) | certain |
| warning | `FND-ENV-001` | 3 | api | Evidence: an environment value is missing<br>Recommendation: Give each environment its own resource group, for example by including the environment name in the group name \(resourceGroup: rg-${AZURE\_ENV\_NAME}\), or record an explicit suppression with a reason and expiry when sharing is intentional.<br>Fix: name: my-agent-project resourceGroup: rg-${AZURE\_ENV\_NAME}<br>[docs](https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview) | certain |

## Skipped checks (1)

| Rule | Reason | Missing capability | Detail |
| --- | --- | --- | --- |
| `FND-NET-001` | synthetic-infrastructure | compiled ARM template | iac: synthetic |

## Owner summary

| Errors | Warnings | Info | Suppressed | Baselined | Skipped | Hidden |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 1 | 0 | 0 | 0 | 1 | 0 |

Top rules:

- `FND-CFG-001`: 1 (error)
- `FND-ENV-001`: 1 (warning)

Exit code: 1

**Skipped is not passed: a skipped check was not evaluated and says nothing about the project.**
