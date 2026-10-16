# FND-OPS-002

## SYNOPSIS

Application Insights connected

## DESCRIPTION

When a workload explicitly requires Foundry server-side tracing, hosted-agent telemetry, or an Application Insights observability destination, the Foundry project (or its parent account) should have a connection of category AppInsights that points to an Application Insights resource. Foundry documents that server-side tracing is enabled after an Application Insights resource is connected, and that hosted agents receive the APPLICATIONINSIGHTS_CONNECTION_STRING setting only when project monitoring is configured. Foundry does not document an Application Insights connection as mandatory for every production project.

## RECOMMENDATION

If server-side tracing or hosted-agent telemetry is a declared requirement, connect an Application Insights resource to the Foundry project (a connection with category AppInsights) and link the component to a Log Analytics workspace. Keep exactly one AppInsights connection per scope. Choose sampling, retention and daily-cap settings deliberately: telemetry ingestion and retention incur cost, so a project that does not require this observability path should not be forced to enable it.

## EXAMPLES

```
# Safe remediation (no unverified ARM wiring):
# In the Foundry portal, open the project, Monitoring, and connect the intended
# workspace-based Application Insights resource. Re-run the live inventory and
# verify exactly one project connection whose category is AppInsights.
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Lists deployed project connections through the control plane and needs the profile observability requirement.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.AppInsights.Workspace.

## LINKS

- [https://learn.microsoft.com/azure/foundry/observability/how-to/trace-agent-setup](https://learn.microsoft.com/azure/foundry/observability/how-to/trace-agent-setup) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/configure-hosted-agent-telemetry](https://learn.microsoft.com/azure/foundry/agents/how-to/configure-hosted-agent-telemetry) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/observability/how-to/trace-ingestion-entra-authentication](https://learn.microsoft.com/azure/foundry/observability/how-to/trace-ingestion-entra-authentication) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-monitor/logs/data-retention-configure](https://learn.microsoft.com/azure/azure-monitor/logs/data-retention-configure) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
