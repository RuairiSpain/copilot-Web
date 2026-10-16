# FND-CFG-006

## SYNOPSIS

Every variable has an environment or infrastructure producer

## DESCRIPTION

Every ${VAR} reference in azure.yaml (service env, connection target and credentials, project network fields, policies and similar) must have a producer: a value in the selected azd environment (.azure/<env>/.env), an azd-set standard variable (AZURE_ENV_NAME, AZURE_LOCATION, AZURE_SUBSCRIPTION_ID, AZURE_RESOURCE_GROUP, AZURE_TENANT_ID, AZURE_PRINCIPAL_ID, AZURE_PRINCIPAL_TYPE; names defined in azd's environment.go), a Bicep output that provisioning writes to the environment, a value written by a previous deploy (AGENT_<SERVICE>_NAME, _VERSION, _ENDPOINT), or an inline default (${VAR:-default}). ${{...}} Foundry server-side expressions and escaped $${VAR} forms are not azd variables.

## RECOMMENDATION

Set the variable in the active azd environment (azd env set), emit it from a Bicep output, give it an inline default, or remove the reference. Do not declare FOUNDRY_ or AGENT_ prefixed names in the azure.yaml service env (FND-CFG-011).

## EXAMPLES

```
# Produce the value
#   azd env set MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME gpt-5.4-mini
services:
  ai-project:
    host: azure.ai.project
  my-agent:
    host: azure.ai.agent
    project: src/my-agent
    uses: [ai-project]
    kind: hosted
    env:
      MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME: ${MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME}
      LOG_LEVEL: ${LOG_LEVEL:-info}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs the selected .azure/<env>/.env keys and compiled template outputs.
- How to feed it: Convert .env to JSON, for example: Get-Content .azure/dev/.env | ConvertFrom-StringData | ConvertTo-Json > env.dev.json. No rule consumes the file yet.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml](https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables](https://learn.microsoft.com/azure/developer/azure-developer-cli/manage-environment-variables) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/internal/synthesis/synthesizer.go](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/internal/synthesis/synthesizer.go) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/pkg/environment/environment.go](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/pkg/environment/environment.go) (last verified 2026-10-15)
