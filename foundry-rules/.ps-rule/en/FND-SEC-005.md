# FND-SEC-005

## SYNOPSIS

Key Vault soft delete, purge protection and RBAC

## DESCRIPTION

A Key Vault referenced by Foundry (for customer-managed keys or connections) should have soft delete on (properties.enableSoftDelete), purge protection on (properties.enablePurgeProtection) and the RBAC permission model (properties.enableRbacAuthorization true).

## RECOMMENDATION

Set enablePurgeProtection true (irreversible, check retention first) and enableRbacAuthorization true, and keep soft delete on. Move any access policies to Azure RBAC role assignments before switching the permission model.

## EXAMPLES

```
resource kv 'Microsoft.KeyVault/vaults@2026-05-15' = {
  name: kvName
  location: location
  properties: {
    tenantId: subscription().tenantId
    sku: { family: 'A', name: 'standard' }
    enableSoftDelete: true
    enablePurgeProtection: true
    enableRbacAuthorization: true
  }
}
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: verified. Catalogue decision: reuse.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Key Vault: enableSoftDelete not false, enablePurgeProtection true, enableRbacAuthorization true.
- Related PSRule for Azure rules from the catalogue research: Azure.KeyVault.SoftDelete, Azure.KeyVault.PurgeProtect, Azure.KeyVault.RBAC.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/keyvault/resource-manager/Microsoft.KeyVault/KeyVault/stable/2026-05-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/keyvault/resource-manager/Microsoft.KeyVault/KeyVault/stable/2026-05-15/openapi.json) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.KeyVault.SoftDelete/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.KeyVault.SoftDelete/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.KeyVault.PurgeProtect/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.KeyVault.PurgeProtect/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.KeyVault.RBAC/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.KeyVault.RBAC/) (last verified 2026-10-04)
