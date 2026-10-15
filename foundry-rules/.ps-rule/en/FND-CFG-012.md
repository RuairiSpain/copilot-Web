# FND-CFG-012

## SYNOPSIS

Azure region values are valid

## DESCRIPTION

Azure region values supplied to the project (AZURE_LOCATION in the azd environment, location parameters in parameter files, and location fields in Bicep/ARM) must be valid Azure location names available to the subscription. azure.yaml itself has no location field; the location comes from the azd environment and infrastructure parameters.

## RECOMMENDATION

Use a location name returned by az account list-locations for your subscription (the name field, such as eastus2), and set it with azd env set AZURE_LOCATION. Check model and resource availability for the region before provisioning.

## EXAMPLES

```
azd env set AZURE_LOCATION eastus2
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Needs Subscriptions_ListLocations (control plane) and AZURE_LOCATION from .azure/<env>/.env. The catalogue says the list membership check is skipped, never passed, without the control plane.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Resource.AllowedRegions, Azure.Template.ResourceLocation, Azure.Template.UseLocationParameter.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/subscriptions/stable/2022-12-01/subscriptions.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/subscriptions/stable/2022-12-01/subscriptions.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables](https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables) (last verified 2026-10-04)
