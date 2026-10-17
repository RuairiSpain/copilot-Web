# FND-CFG-002

## SYNOPSIS

Unique service names by kind

## DESCRIPTION

Within one azure.yaml, two azure.ai.agent services in the same project should not declare the same agent name unless creating a new version of that one logical agent is intentional. The Foundry reference documents only the agent consequence: reusing a name creates a new version. No overwrite/collision behaviour is asserted for connections, toolboxes, or routines.

## RECOMMENDATION

Give separate logical agents distinct names. If the services intentionally represent versions of one agent, keep one service definition and deploy the intended version through that service.

## EXAMPLES

```
services:
  ai-project:
    host: azure.ai.project
  triage:
    host: azure.ai.agent
    project: src/triage
    uses: [ai-project]
    kind: hosted
    name: triage-agent
  summarizer:
    host: azure.ai.agent
    project: src/summarizer
    uses: [ai-project]
    kind: hosted
    name: summarizer-agent
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-ps. Status: evaluated-subset.
- Evaluated here: azure.ai.agent services with a literal project and name only. Expressions (${...}) are left out; the catalogue reports them as uncertain.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json) (last verified 2026-10-04)
