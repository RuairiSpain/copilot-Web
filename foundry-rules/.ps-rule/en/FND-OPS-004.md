# FND-OPS-004

## SYNOPSIS

Required tags exist

## DESCRIPTION

Resources and resource groups in the Foundry stack (Foundry account, project, model deployments, Search, API Management, Storage, Cosmos DB, Application Insights, Log Analytics) should carry the tags that the organisation requires, such as environment, owner and cost centre. The required tag names, optional value formats and the resource types in scope come from the active profile. Tags support cost allocation and incident triage.

## RECOMMENDATION

Define the required tags once (for example in a Bicep user-defined type), pass them to every resource, and enforce them at runtime with Azure Policy (Require a tag on resources). Foundry Doctor reports where the template omits them.

## EXAMPLES

```
@description('Tags required by the organisation.')
param tags requiredTags
type requiredTags = { Env: string, CostCode: string }

resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
  name: accountName
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  tags: tags
}
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: static-ps. Status: evaluated-subset.
- Evaluated here: Tag names listed in the PSRule configuration key FOUNDRY_REQUIRED_TAGS (empty by default, so the rule is skipped), tag count above 50, tag name length (512, or 128 on storage accounts) and tag value length above 256. Tag value formats are not evaluated. Resource types in scope: Foundry accounts and projects, Search, Storage, Cosmos DB, API Management. The catalogue marks as unverified which resource types support tags.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Resource.RequiredTags, Azure.Resource.UseTags, Azure.Group.RequiredTags, Azure.Subscription.RequiredTags.

## LINKS

- [https://learn.microsoft.com/azure/azure-resource-manager/management/tag-resources](https://learn.microsoft.com/azure/azure-resource-manager/management/tag-resources) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/manage-costs](https://learn.microsoft.com/azure/foundry/concepts/manage-costs) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Resource.RequiredTags/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Resource.RequiredTags/) (last verified 2026-10-04)
