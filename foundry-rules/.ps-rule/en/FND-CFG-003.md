# FND-CFG-003

## SYNOPSIS

All inter-service references resolve

## DESCRIPTION

Every typed cross-service reference in azure.yaml must resolve inside the same file: agent toolboxes entries must be azure.ai.toolbox services that are also listed in the agent uses; toolbox tools[].connection and prompt-agent connections entries must name azure.ai.connection services; a voice conversationEngine.name must be a hosted azure.ai.agent service in the same project; and memory-store chatModel and embeddingModel must name model deployments. Generic uses entries are checked only when they name a service; the schema also permits resource names and the Foundry-specific resource resolution behaviour is not documented.

## RECOMMENDATION

Make each referenced name match a service key defined under services, and add toolbox and connection services to the consuming agent uses list as well as its toolboxes or connections list.

## EXAMPLES

```
# Partial fragment: category, target and authentication fields for the
# connection, plus toolbox-specific required fields, depend on the selected
# documented tool. Merge this graph into a schema-valid manifest.
services:
  ai-project:
    host: azure.ai.project
  github-conn:
    host: azure.ai.connection
    uses: [ai-project]
  agent-tools:
    host: azure.ai.toolbox
    uses: [ai-project, github-conn]
    tools:
      - type: mcp
        connection: github-conn
  my-agent:
    host: azure.ai.agent
    project: src/my-agent
    uses: [ai-project, github-conn, agent-tools]
    kind: hosted
    toolboxes: [agent-tools]
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-ps. Status: evaluated-subset.
- Evaluated here: Agent toolboxes (must be an azure.ai.toolbox service and also in uses), agent connections and toolbox tools[].connection (must be azure.ai.connection services). Voice conversationEngine.name, memory-store models and generic uses are not evaluated.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml](https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json](https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json) (last verified 2026-10-04)
