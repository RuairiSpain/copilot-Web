# FND-RUN-005

## SYNOPSIS

Agent tools/connections respond

## DESCRIPTION

The deployed agent versions and project connections that an agent depends on should respond as healthy to the project data plane. The rule reads agent version status (Active, Creating, Failed) and connection metadata through the project endpoint. It never invokes a tool, MCP server or model and never requests connection credentials, because no source describes a side-effect-free tool probe. The tool-responsiveness design is a product opinion.

## RECOMMENDATION

Open the failing agent version or connection in the Foundry portal, read its error, and redeploy the agent or recreate the connection. Test tool calls with an explicit, operator-run test; the doctor does not call tools.

## EXAMPLES

```
# azure.yaml: keep the agent service and the connection names it uses aligned with the project
services:
  support-agent:
    host: azure.ai.agent
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Foundry project endpoint data-plane reads of agents and connections.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/cmd/doctor/checks_agent_status.go](https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/cmd/doctor/checks_agent_status.go) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/cmd/doctor/checks_foundry_endpoint.go](https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/cmd/doctor/checks_foundry_endpoint.go) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/pkg/azure/foundry_projects_client.go](https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/pkg/azure/foundry_projects_client.go) (last verified 2026-10-04)
