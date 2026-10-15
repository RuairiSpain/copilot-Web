# FND-COST-002

## SYNOPSIS

Dev avoids production-sized fixed SKUs

## DESCRIPTION

Environments classified as dev should not provision fixed-capacity SKUs that bill continuously regardless of use and are sized for production, such as provisioned-throughput (PTU) model deployments, AI Search standard2, standard3 or storage-optimised tiers or multiple replicas and partitions, API Management Premium or Isolated, or Cosmos DB throughput above a profile limit. What counts as production-sized is a Foundry Doctor opinion; the SKU names and billing model are documented.

## RECOMMENDATION

In dev use pay-per-token deployments (GlobalStandard or Standard), the Search free or basic tier with one replica and one partition, an API Management Developer or consumption-style tier, and the lowest Cosmos DB throughput that the Foundry Agent Service setup allows. Reserve production-sized capacity for test and production environments.

## EXAMPLES

```
# azure.yaml - dev environment values
deployments:
  - name: chat
    model: { format: OpenAI, name: gpt-5.4-mini, version: "2026-03-17" }
    sku: { name: GlobalStandard, capacity: 10 }
```

## NOTES

- Severity dev/test/prod: warning/info/info (PSRule: Warning/Information/Information).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Compiled ARM only: provisioned deployment SKUs, Search standard2/standard3/storage_optimized_* or replica or partition count above 1, API Management Premium/Isolated or capacity above 1. Cosmos throughput (the catalogue ships no default limit), the azure.yaml side and exemption lists are not evaluated. The dev-only intent is carried by severity per profile (warning in dev, info in test and prod), not by environment detection.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types](https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-sku-tier](https://learn.microsoft.com/azure/search/search-sku-tier) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01/apimdeployment.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01/apimdeployment.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2025-04-15/cosmos-db.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2025-04-15/cosmos-db.json) (last verified 2026-10-15)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup](https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup) (last verified 2026-10-04)
