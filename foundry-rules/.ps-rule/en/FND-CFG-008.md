# FND-CFG-008

## SYNOPSIS

MCP tools use allow lists

## DESCRIPTION

For the direct inline MCP tool shape whose property is literally allowed_tools, remote MCP tools should restrict the tools the agent can call rather than expose every tool of the server. Foundry documents the allow-list recommendation and the all-tools default for its MCP tool definition, but no reviewed source confirms that azure.yaml accepts or forwards this spelling.

## RECOMMENDATION

Where the selected azure.yaml extension version documents support for it, add allowed_tools with the exact tool names the agent needs. Tool names are case-sensitive and must match what the MCP server returns from tools/list. Otherwise configure the allow list through a documented supported surface and do not guess the manifest property name.

## EXAMPLES

```
# Partial MCP tool fragment. Use only after the selected azure.yaml extension
# version documents this property spelling.
tools:
  - type: mcp
    connection: github-conn
    allowed_tools: [search_issues, get_issue]
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: static-ps. Status: evaluated-subset.
- Evaluated here: A literal empty allowed_tools list on an inline mcp tool. A missing allowed_tools is uncertain in the catalogue and is not reported.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/tools/model-context-protocol](https://learn.microsoft.com/azure/foundry/agents/how-to/tools/model-context-protocol) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/tools/toolbox](https://learn.microsoft.com/azure/foundry/agents/how-to/tools/toolbox) (last verified 2026-10-04)
