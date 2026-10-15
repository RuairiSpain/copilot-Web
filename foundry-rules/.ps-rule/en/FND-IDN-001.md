# FND-IDN-001

## SYNOPSIS

Managed identities used for Foundry and workloads

## DESCRIPTION

The Foundry account, each Foundry project and the compute workloads the template deploys beside them must have a managed identity (system-assigned, user-assigned or both) so that access to Storage, Cosmos DB, Search and Container Registry is granted to an identity instead of a key or a secret. The Foundry documentation states that the project managed identity is what running agents use to reach Cosmos DB, Search and Storage, and that a user-assigned project identity can only be chosen when the project is created. The rule reads identity.type on the account, the project and the workload resources and reports resources whose identity is absent or None. API-key based connections are handled by the SEC group; gateway and Search-source identity by FND-GW-005 and FND-IQ-008.

## RECOMMENDATION

Enable a system-assigned or user-assigned managed identity on the Foundry account, on each project and on every workload that calls Foundry, then grant that identity data-plane roles on the dependent resources (see FND-IDN-002). Choose the user-assigned option at project creation if you need it, because the project identity type cannot be changed later.

## EXAMPLES

```
resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
  name: accountName
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  identity: { type: 'SystemAssigned' }
  properties: { /* ... */ }
}
resource project 'Microsoft.CognitiveServices/accounts/projects@2026-09-01' = {
  parent: account
  name: projectName
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {}
}
```

## NOTES

- Severity dev/test/prod: warning/warning/error (PSRule: Warning/Warning/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Foundry accounts and projects: identity.type present and not None, and userAssignedIdentities non-empty when UserAssigned. Workload resources (container apps, web apps) are not evaluated because their selection depends on a Foundry endpoint setting. The catalogue marks as unverified whether the platform rejects an account without identity.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.AI.ManagedIdentity, Azure.ContainerApp.ManagedIdentity, Azure.AppService.ManagedIdentity.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup](https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/hosted-agent-permissions](https://learn.microsoft.com/azure/foundry/agents/concepts/hosted-agent-permissions) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.AI.ManagedIdentity/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.AI.ManagedIdentity/) (last verified 2026-10-04)
