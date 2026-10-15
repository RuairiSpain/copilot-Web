# FND-DEP-008

## SYNOPSIS

What-if has no unexpected delete

## DESCRIPTION

An ARM what-if of the compiled deployment against the target scope must not predict a Delete of an existing resource that the change did not intend to remove. Whether a change recreates a resource is not exposed by the what-if result (the changeType enum has no Replace value), so it is not asserted. what-if is the only evidence source that sees live state; it is opt-in because it is a POST that needs the same permissions as the deployment (see notes on read-only).

## RECOMMENDATION

Review each predicted Delete. If it is intended, record it in the baseline with a reason and expiry; if not, switch the deployment to incremental mode, add the missing resource to the template, or remove the conflicting change. Re-run what-if after fixing.

## EXAMPLES

```
# preview at resource group scope; nothing is deployed
az deployment group what-if --resource-group <rg> --template-file main.bicep --parameters main.bicepparam
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM deployment what-if (long-running POST).
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/deployments/stable/2026-06-01/deployments.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/deployments/stable/2026-06-01/deployments.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-resource-manager/templates/deploy-what-if](https://learn.microsoft.com/azure/azure-resource-manager/templates/deploy-what-if) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-resource-manager/templates/deploy-what-if](https://learn.microsoft.com/azure/azure-resource-manager/templates/deploy-what-if) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance](https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/main/cli/azd/docs/design/provision-validation.md](https://github.com/Azure/azure-dev/blob/main/cli/azd/docs/design/provision-validation.md) (last verified 2026-10-04)
