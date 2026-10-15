# FND-CFG-007

## SYNOPSIS

Outbound endpoints use HTTPS and reject unsafe local/metadata targets

## DESCRIPTION

Outbound endpoint URLs configured in azure.yaml (azure.ai.connection target, azure.ai.toolbox tools entries and endpoint, azure.ai.project endpoint, MCP server URLs, agent env values that are URLs) should use https. Targets that point at loopback, link-local or cloud-metadata addresses (localhost, 127.0.0.0/8, ::1, 169.254.0.0/16 including 169.254.169.254, and private-network literals) are reported because a hosted agent or toolbox runs remotely and cannot reach a developer machine, and such targets are server-side request forgery risks.

## RECOMMENDATION

Use https endpoints reachable from the service that calls them. For internal servers, publish them on a private endpoint or an internal Container Apps ingress and use their private DNS name over https instead of an IP literal. Remove localhost and metadata addresses from deployed configuration.

## EXAMPLES

```
services:
  github-conn:
    host: azure.ai.connection
    category: RemoteTool
    target: https://api.githubcopilot.com/mcp
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: The URL-typed fields are described by description only and their azure.yaml paths are not carried in the catalogue evidence.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/tools/model-context-protocol](https://learn.microsoft.com/azure/foundry/agents/how-to/tools/model-context-protocol) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/a64ca8fe04d0bc1b0375c32fed1b76573e2a91ef/cli/azd/extensions/azure.ai.connections/schemas/azure.ai.connection.json](https://github.com/Azure/azure-dev/blob/a64ca8fe04d0bc1b0375c32fed1b76573e2a91ef/cli/azd/extensions/azure.ai.connections/schemas/azure.ai.connection.json) (last verified 2026-10-05)
