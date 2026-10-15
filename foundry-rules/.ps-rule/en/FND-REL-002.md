# FND-REL-002

## SYNOPSIS

Storage redundancy posture

## DESCRIPTION

A Storage account used by Foundry (project storage, or Agent Service standard-mode thread storage) should use a zone-redundant or geo-redundant SKU. Foundry guidance recommends zone-redundant configuration for the customer-managed state stores and GZRS for the Agent Service storage account. This is a WAF/WARA recommendation, not a platform constraint.

## RECOMMENDATION

Use Standard_ZRS at minimum for storage that holds project or agent data, and Standard_GZRS for Agent Service standard-mode storage as recommended by the Foundry resiliency guidance. SKU changes on an existing account are a migration, so decide at design time. Note the Foundry multi-region section says default storage failover with GRS/GZRS is not supported for Foundry projects, so geo-redundancy there protects data recovery, not automatic project failover.

## EXAMPLES

```
resource st 'Microsoft.Storage/storageAccounts@2026-09-01' = {
  name: storageName
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_GZRS' }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Storage sku.name must not be Standard_LRS or Premium_LRS. The GZRS note for Agent Service standard mode is not evaluated. The catalogue notes two Foundry statements about GZRS that still need reconciling.
- Related PSRule for Azure rules from the catalogue research: Azure.Storage.UseReplication.

## LINKS

- [https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency](https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-09-01/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-09-01/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Storage/storageAccounts/recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Storage/storageAccounts/recommendations.yaml) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.UseReplication/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Storage.UseReplication/) (last verified 2026-10-04)
- [https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/Storage/GeoRedundant_StorageAccounts_Audit.json](https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/Storage/GeoRedundant_StorageAccounts_Audit.json) (last verified 2026-10-04)
