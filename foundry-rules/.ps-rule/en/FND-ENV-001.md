# FND-ENV-001

## SYNOPSIS

Environments do not unintentionally share resource groups

## DESCRIPTION

Two azd environments of the same project (for example dev and prod) should not resolve to the same resource group. azd documents one resource group per environment for isolation. The Bicep provider reads only AZURE_RESOURCE_GROUP from the environment; the azure.yaml resourceGroup (project or service level) applies when azd resolves a service's deployment target. A resourceGroup literal without an environment-specific component makes every environment's services target the same group, so redeploying or tearing down one environment affects the other.

## RECOMMENDATION

Give each environment its own resource group, for example by including the environment name in the group name (resourceGroup: rg-${AZURE_ENV_NAME}), or record an explicit suppression with a reason and expiry when sharing is intentional.

## EXAMPLES

```
name: my-agent-project
resourceGroup: rg-${AZURE_ENV_NAME}
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs every .azure/<env>/.env file in the workspace.
- How to feed it: Convert each .env to JSON (see FND-CFG-006) and add a rule over the set. Not implemented.
- This rule never reports Pass in PSRule.

## LINKS

- [https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview](https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview) (last verified 2026-10-05)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json](https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/environment/local_file_data_store.go](https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/environment/local_file_data_store.go) (last verified 2026-10-05)
- [https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/project/resource_manager.go](https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/project/resource_manager.go) (last verified 2026-10-05)
