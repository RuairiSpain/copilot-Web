# FND-ENV-003

## SYNOPSIS

Prod does not reference non-prod resource IDs

## DESCRIPTION

A production environment's configuration should not point at resources that belong to a non-production environment of the same project: resource IDs, endpoints or names that match another environment's resource group, project endpoint or provisioned resources (azure.ai.project endpoint, connection targets, ARM IDs in parameters).

## RECOMMENDATION

Point the production environment at production resources, or provision the dependency in production. Re-run provisioning for the production environment so its outputs are written to its own .env.

## EXAMPLES

```
# .azure/prod/.env
AZURE_AI_PROJECT_ENDPOINT=https://prod-account.services.ai.azure.com/api/projects/prod-project
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs the environment marked prod in configuration and the .env values of every other environment.
- How to feed it: Same as FND-ENV-001.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.

## LINKS

- [https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview](https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview) (last verified 2026-10-05)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.projects/schemas/azure.ai.project.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.projects/schemas/azure.ai.project.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/environment/local_file_data_store.go](https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/environment/local_file_data_store.go) (last verified 2026-10-05)
