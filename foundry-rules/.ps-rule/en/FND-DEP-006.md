# FND-DEP-006

## SYNOPSIS

No blocking soft-deleted resource/name

## DESCRIPTION

A resource name planned for a new deployment must not be held by a soft-deleted Foundry (Cognitive Services) account or API Management service. Foundry documents that after deletion the same name cannot be reused for 48 hours unless the resource is purged, and API Management documents the same 48-hour soft-delete period during which the name cannot be reused. Soft-deleted Key Vault vaults are reported when the vault name matches, with lower confidence.

## RECOMMENDATION

Recover the soft-deleted resource if you still need it, or purge it (needs the delete permission at subscription scope) before provisioning, or choose a different name. Purging permanently deletes data.

## EXAMPLES

```
# purge a soft-deleted Foundry account so the name can be reused (permanently deletes the account)
az resource delete --ids /subscriptions/<sub-id>/providers/Microsoft.CognitiveServices/locations/<region>/resourceGroups/<rg>/deletedAccounts/<name>
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM deleted-resource lists for Cognitive Services, API Management and Key Vault.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/ai-services/recover-purge-resources](https://learn.microsoft.com/azure/ai-services/recover-purge-resources) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/soft-delete](https://learn.microsoft.com/azure/api-management/soft-delete) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/keyvault/resource-manager/Microsoft.KeyVault/KeyVault/stable/2026-05-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/keyvault/resource-manager/Microsoft.KeyVault/KeyVault/stable/2026-05-15/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01/apimdeletedservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01/apimdeletedservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/ai-machine-learning](https://learn.microsoft.com/azure/role-based-access-control/permissions/ai-machine-learning) (last verified 2026-10-04)
