# FND-REL-001

## SYNOPSIS

Search replica/SLA posture

## DESCRIPTION

An Azure AI Search service that backs a Foundry project should have enough replicas to qualify for the Azure AI Search SLA: two or more replicas for query workloads, three or more for query and indexing workloads, on a billable tier. The SLA replica counts are documented platform facts; the expectation that a given environment must meet them is a WAF/WARA recommendation, so severity is a warning at most.

## RECOMMENDATION

Set properties.replicaCount to at least 2 if the application only queries, or at least 3 if it also updates indexes online. Each replica adds search units and cost, so keep dev and test at 1 unless you are testing failover behaviour.

## EXAMPLES

```
resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: searchName
  location: location
  sku: { name: 'standard' }
  properties: {
    replicaCount: 3
    partitionCount: 1
  }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Search services: replicaCount of at least 2 unless sku.name is free. The scope 'referenced by a Foundry connection' is not evaluated, and the info note for exactly 2 replicas is not emitted.
- Related PSRule for Azure rules from the catalogue research: Azure.Search.QuerySLA, Azure.Search.IndexSLA.

## LINKS

- [https://learn.microsoft.com/azure/search/search-limits-quotas-capacity](https://learn.microsoft.com/azure/search/search-limits-quotas-capacity) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json) (last verified 2026-10-04)
- [https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Search/searchServices/recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Search/searchServices/recommendations.yaml) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Search.QuerySLA/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Search.QuerySLA/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Search.IndexSLA/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Search.IndexSLA/) (last verified 2026-10-04)
