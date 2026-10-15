# FND-NET-008

## SYNOPSIS

Search SKU supports private endpoints

## DESCRIPTION

Azure AI Search private endpoints are not supported on the Free tier; Basic or higher is required. The rule fails when a search service with sku.name free is used where a private connection is required: it has a private endpoint or it is the vectorStore of a private Foundry account. Capability limits on other tiers (for example, indexer private connections and skillsets) are real but tier specific and are reported as informational notes only. Disabling public access on Free without a private endpoint is not by itself proof that private connectivity was intended; that inaccessible or intentionally disabled state is handled by NET-002 correlation.

## RECOMMENDATION

If private access is required, choose Basic or higher and account for the additional fixed cost; otherwise keep Free public rather than creating a false expectation that Free supports Private Link. Plan the tier against indexer needs using the Search limits article.

## EXAMPLES

```
resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: searchName
  location: location
  sku: { name: 'standard' } // 'free' does not support private endpoints
  properties: { publicNetworkAccess: 'Disabled' }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs private endpoints and capabilitySettings correlated with the Search service.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Search.SKU.

## LINKS

- [https://learn.microsoft.com/azure/search/service-create-private-endpoint](https://learn.microsoft.com/azure/search/service-create-private-endpoint) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-sku-tier](https://learn.microsoft.com/azure/search/search-sku-tier) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-limits-quotas-capacity](https://learn.microsoft.com/azure/search/search-limits-quotas-capacity) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json) (last verified 2026-10-04)
