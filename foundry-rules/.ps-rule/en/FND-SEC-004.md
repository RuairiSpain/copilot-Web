# FND-SEC-004

## SYNOPSIS

Storage shared key/anonymous access disabled; HTTPS/TLS required

## DESCRIPTION

A Storage account used by Foundry should block Shared Key authorization (properties.allowSharedKeyAccess false), block anonymous blob access (properties.allowBlobPublicAccess false), require HTTPS (properties.supportsHttpsTrafficOnly true) and set properties.minimumTlsVersion to TLS1_2 or TLS1_3. Each sub-check reports separately.

## RECOMMENDATION

Set allowSharedKeyAccess false, allowBlobPublicAccess false, supportsHttpsTrafficOnly true and minimumTlsVersion TLS1_2 on the account, and give the Foundry identity Storage data roles. Verify no client still uses account keys or SAS signed with the account key before disabling Shared Key.

## EXAMPLES

```
resource st 'Microsoft.Storage/storageAccounts@2026-06-01' = {
  name: storageName
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
  properties: {
    allowSharedKeyAccess: false
    allowBlobPublicAccess: false
    supportsHttpsTrafficOnly: true
    minimumTlsVersion: 'TLS1_2'
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Storage accounts: allowSharedKeyAccess false, allowBlobPublicAccess not true, supportsHttpsTrafficOnly not false, minimumTlsVersion present and not TLS1_0 or TLS1_1. The Foundry-scope test is not evaluated.
- Related PSRule for Azure rules from the catalogue research: Azure.Storage.LocalAuth, Azure.Storage.BlobPublicAccess, Azure.Storage.MinTLS, Azure.Storage.SecureTransfer.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-06-01/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-06-01/openapi.json) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.LocalAuth/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.LocalAuth/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.BlobPublicAccess/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.BlobPublicAccess/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.MinTLS/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.MinTLS/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.SecureTransfer/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.SecureTransfer/) (last verified 2026-10-04)
