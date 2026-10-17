# FND-SEC-010

## SYNOPSIS

Customer-managed keys informational

## DESCRIPTION

Informational summary of whether customer-managed key (CMK) encryption is configured on the Foundry account, AI Search, Cosmos DB and Storage resources of a solution. It never fails; it surfaces the posture so teams with a CMK requirement can see the gaps.

## RECOMMENDATION

If your compliance regime requires customer-managed keys, configure them on each resource listed and use a Key Vault with soft delete and purge protection (see FND-SEC-005). Otherwise no action is needed.

## EXAMPLES

```
// Informational. Example for a Search service that enforces CMK compliance reporting:
properties: {
  encryptionWithCmk: { enforcement: 'Enabled' }
}
```

## NOTES

- Severity dev/test/prod: info/info/info (PSRule: Information/Information/Information).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Informational. A fail result at level Information means customer-managed keys are not configured on a Foundry account, Search service, Cosmos DB account or Storage account. The catalogue defines no fail state; treat the result as a report line.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-06-01/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-06-01/openapi.json) (last verified 2026-10-04)
