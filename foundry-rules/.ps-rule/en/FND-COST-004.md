# FND-COST-004

## SYNOPSIS

Informational monthly fixed-capacity estimate

## DESCRIPTION

Report an informational, per-environment monthly range for the fixed-capacity resources in the compiled plan (AI Search units, API Management units, Cosmos DB provisioned RU/s and similar verified meters) using public retail prices, with the assumptions, region, currency, price date and the resources that are excluded. It never produces a pass or fail and never estimates token consumption.

## RECOMMENDATION

Treat the figure as a planning aid for comparing dev, test and prod fixed capacity, and confirm with the Azure pricing calculator and your own agreement before committing spend.

## EXAMPLES

```
foundry-doctor cost --env prod --compare dev
```

## NOTES

- Severity dev/test/prod: info/info/info (PSRule: Information/Information/Information).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Needs a retail price file or price API and a supported-meter catalogue. Informational estimate with no pass or fail outcome.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/search/search-sku-tier](https://learn.microsoft.com/azure/search/search-sku-tier) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01/apimdeployment.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01/apimdeployment.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/manage-costs](https://learn.microsoft.com/azure/foundry/concepts/manage-costs) (last verified 2026-10-04)
- [https://learn.microsoft.com/rest/api/cost-management/retail-prices/azure-retail-prices](https://learn.microsoft.com/rest/api/cost-management/retail-prices/azure-retail-prices) (last verified 2026-10-04)
