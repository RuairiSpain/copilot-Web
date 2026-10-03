# Vendored azd extension schemas

These JSON schemas are copied unchanged from the Azure Developer CLI repository
(`Azure/azure-dev`, MIT licence) so the contract test can validate `azure.yaml`
offline and in CI.

| File | Upstream path |
| --- | --- |
| `azure.ai.agent.json` | `cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json` |
| `azure.ai.project.json` | `cli/azd/extensions/azure.ai.projects/schemas/azure.ai.project.json` |
| `azure.ai.connection.json` | `cli/azd/extensions/azure.ai.connections/schemas/azure.ai.connection.json` |
| `azure.ai.toolbox.json` | `cli/azd/extensions/azure.ai.toolboxes/schemas/azure.ai.toolbox.json` |

Retrieved on 2026-10-03. Refresh them with `make refresh-schemas` when you upgrade the
`azd` Foundry extensions.
