# FND-DEP-001

## SYNOPSIS

Signed into matching tenant/subscription

## DESCRIPTION

The azd environment names a subscription (AZURE_SUBSCRIPTION_ID) and a tenant (AZURE_TENANT_ID). The signed-in identity must be able to see that subscription, the subscription must belong to that tenant, and the subscription must be in a usable state. A token issued by a tenant that cannot manage the subscription cannot deploy to it.

## RECOMMENDATION

Sign in to the tenant that owns the subscription (azd auth login --tenant-id <tenant-id>), then confirm AZURE_SUBSCRIPTION_ID and AZURE_TENANT_ID in the selected azd environment (azd env get-values). If the subscription is in another tenant, create a separate azd environment for that tenant.

## EXAMPLES

```
azd auth login --tenant-id <tenant-id>
azd env set AZURE_TENANT_ID <tenant-id>
azd env set AZURE_SUBSCRIPTION_ID <subscription-id>
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM GET /subscriptions/{id} and GET /tenants, plus AZURE_SUBSCRIPTION_ID and AZURE_TENANT_ID from the azd environment.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/subscriptions/stable/2022-12-01/subscriptions.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/subscriptions/stable/2022-12-01/subscriptions.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-resource-manager/management/authenticate-multi-tenant](https://learn.microsoft.com/azure/azure-resource-manager/management/authenticate-multi-tenant) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables](https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/developer/azure-developer-cli/reference](https://learn.microsoft.com/azure/developer/azure-developer-cli/reference) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/built-in-roles/general](https://learn.microsoft.com/azure/role-based-access-control/built-in-roles/general) (last verified 2026-10-04)
