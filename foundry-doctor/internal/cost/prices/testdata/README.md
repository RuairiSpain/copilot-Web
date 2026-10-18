# Recorded Retail Prices API responses

Trimmed real responses from the public Azure Retail Prices API
(`https://prices.azure.com/api/retail/prices`), captured 2026-10-18 through a
search-engine snapshot of the public endpoint (the build environment has no
direct network access). Items are copied verbatim; pages are trimmed to the
rows a test needs.

- `apim-basicv2-eastus.json`: complete response (Count 4) for
  `currencyCode=USD&$filter=serviceName eq 'API Management' and armRegionName eq 'eastus' and skuName eq 'Basic v2'`.
  Contains the hourly unit meter, a same-price secondary meter, and tiered call rows.
- `search-mixed.json`: rows from `$filter=serviceName eq 'Azure Cognitive Search'`
  (Standard S1 Unit in koreacentral and australiasoutheast, non-primary; Basic Unit
  in eastus2; Standard S3 Unit in westeurope; Basic CC Unit; a tiered
  Document Cracking row).
