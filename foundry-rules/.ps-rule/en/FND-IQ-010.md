# FND-IQ-010

## SYNOPSIS

ADLS Gen2 hierarchical namespace enabled

## DESCRIPTION

A knowledge source or Search data source that is declared as Azure Data Lake Storage Gen2 (isADLSGen2 true on an azureBlob knowledge source, or data source type adlsgen2) must point at a storage account that has the hierarchical namespace enabled (isHnsEnabled true). The ADLS Gen2 indexer checks for hierarchical namespace through the Filesystem get-properties API, and ADLS Gen2 is defined as a storage account with hierarchical namespace enabled.

## RECOMMENDATION

Enable hierarchical namespace (ADLS Gen2) when creating the storage account, or use a plain Blob knowledge source or blob indexer for accounts without it. Hierarchical namespace must be chosen at account creation for new accounts; migration of an existing account is a separate Storage procedure.

## EXAMPLES

```
resource storage 'Microsoft.Storage/storageAccounts@2025-01-01' = {
  name: storageName
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
  properties: { isHnsEnabled: true }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Needs the storage account behind the knowledge source connectionString (data plane) and its isHnsEnabled value.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/search-how-to-index-azure-data-lake-storage](https://learn.microsoft.com/azure/search/search-how-to-index-azure-data-lake-storage) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-blob](https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-blob) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/storage/resource-manager/Microsoft.Storage](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/storage/resource-manager/Microsoft.Storage) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search) (last verified 2026-10-04)
