# FND-REL-005

## SYNOPSIS

Search sizing within platform limits

## DESCRIPTION

The replica and partition configuration of an Azure AI Search service must be a combination the platform allows: partitionCount one of 1, 2, 3, 4, 6 or 12; replicaCount within 1..12 (1..3 on basic); replicas x partitions within the 36 search unit maximum on standard and above; and at most 3 partitions when hostingMode is HighDensity. These are documented platform constraints, so violations are errors in every profile.

## RECOMMENDATION

Choose a partitionCount from 1, 2, 3, 4, 6 or 12 and a replicaCount of 12 or fewer (3 or fewer on basic), keep replicas x partitions at or below 36 search units, and use hostingMode HighDensity only with standard3 and at most 3 partitions. See the partition and replica combinations table in the capacity planning article.

## EXAMPLES

```
resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: searchName
  location: location
  sku: { name: 'standard' }
  properties: {
    replicaCount: 3
    partitionCount: 2   // 6 search units, within the 36 SU limit
  }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-ps. Status: evaluated-subset.
- Evaluated here: Checks (a) partitionCount in 1, 2, 3, 4, 6, 12, (b) replicaCount at most 12 and at most 3 on basic, (c) replicas x partitions at most 36 on SKUs above basic, (d) HighDensity only with standard3 and at most 3 partitions. free and serverless SKUs are skipped. Basic partitionCount above 1 is not asserted.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-limits-quotas-capacity](https://learn.microsoft.com/azure/search/search-limits-quotas-capacity) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-capacity-planning](https://learn.microsoft.com/azure/search/search-capacity-planning) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Search.SKU/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Search.SKU/) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/c415ef063cd4c1833ecceff7f856295115d80c0d/specification/search/resource-manager/Microsoft.Search/Search/preview/2026-09-01-preview/search.json](https://github.com/Azure/azure-rest-api-specs/blob/c415ef063cd4c1833ecceff7f856295115d80c0d/specification/search/resource-manager/Microsoft.Search/Search/preview/2026-09-01-preview/search.json) (last verified 2026-10-05)
