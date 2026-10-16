# FND-CFG-011

## SYNOPSIS

Preview/retired fields are version-aware

## DESCRIPTION

azure.yaml uses preview schemas that change between extension betas. The rule reports fields and hosts that the sources mark as deprecated, replaced or compatibility-only (service-level config for the agent, microsoft.foundry host, kind prompt-voice alias, the two-file agent.manifest.yaml/agent.yaml model, ignored hosted-agent fields displayName/inputSchema/outputSchema). When requiredVersions allows extension versions older than the version that introduced the construct used, the result is uncertain, not an error.

## RECOMMENDATION

Move to the unified service-level shape: put env on the agent service, use kind voice, use host azure.ai.project instead of microsoft.foundry, drop fields the hosted-agent create request ignores, and declare requiredVersions for extension versions verified by this catalogue. Do not infer a field's introduction version from the current schema.

## EXAMPLES

```
requiredVersions:
  extensions:
    azure.ai.agents: ">=1.0.0-beta.18"
    azure.ai.projects: ">=1.0.0-beta.13"
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
```

## NOTES

- Severity dev/test/prod: warning/warning/error (PSRule: Warning/Warning/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-ps. Status: evaluated-subset.
- Evaluated here: config under an azure.ai.agent service, host microsoft.foundry, kind prompt-voice, displayName/inputSchema/outputSchema on hosted agents, env keys with a FOUNDRY_ or AGENT_ prefix. The requiredVersions comparison is not evaluated.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json](https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json) (last verified 2026-10-04)
