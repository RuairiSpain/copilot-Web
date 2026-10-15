# FND-ENV-002

## SYNOPSIS

Environment-bound names/IDs/rules were not copied unchanged

## DESCRIPTION

Values that identify an environment's own resources (resource group, subscription, account and project names or endpoints, Key Vault and search service names, resource IDs, RAI policy IDs, connection targets) should differ between environments unless sharing is intended. Identical values across a dev and a prod environment usually mean the environment file or parameter file was copied without edits.

## RECOMMENDATION

Review each reported key and give the environment its own value, or suppress the finding with a reason and an expiry where the shared value is intentional. Prefer deriving names from AZURE_ENV_NAME.

## EXAMPLES

```
# .azure/prod/.env
AZURE_RESOURCE_GROUP=rg-myapp-prod
AZURE_AI_PROJECT_ENDPOINT=https://prod-account.services.ai.azure.com/api/projects/prod-project
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs per-environment .env and parameter files compared by key.
- How to feed it: Same as FND-ENV-001.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.

## LINKS

- [https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview](https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview) (last verified 2026-10-05)
- [https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/environment/local_file_data_store.go](https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/environment/local_file_data_store.go) (last verified 2026-10-05)
