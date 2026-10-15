# FND-RUN-008

## SYNOPSIS

Deployed properties versus compiled template drift

## DESCRIPTION

Placeholder for a future phase. Compare the properties of Foundry resources as deployed in Azure with the ARM JSON compiled from the project Bicep, and report differences on properties that Foundry Doctor rules inspect. Which differences count as drift versus expected service-populated values is a design decision; no primary source defines it.

## RECOMMENDATION

When implemented: redeploy from source control to reconcile drift, or update the Bicep to capture the intentional change. Until then, run azd provision --preview manually before provisioning.

## EXAMPLES

```
# Manual interim check (read-only preview of the planned change)
azd provision --preview
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Deployed resource GET or what-if compared with the compiled template; the catalogue says this is a documented placeholder.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/deployments/stable/2025-04-01/deployments.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/deployments/stable/2025-04-01/deployments.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/main/cli/azd/docs/extensions/extension-framework.md](https://github.com/Azure/azure-dev/blob/main/cli/azd/docs/extensions/extension-framework.md) (last verified 2026-10-04)
