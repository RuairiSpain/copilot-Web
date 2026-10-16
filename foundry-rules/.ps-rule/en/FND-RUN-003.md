# FND-RUN-003

## SYNOPSIS

Capability host and project connections healthy

## DESCRIPTION

The project and its capability host must be provisioned successfully, and the connections the capability host binds (AI services, storage, thread storage and vector store) must exist on the project or be shared to it and report no connection error. The rule reads ARM provisioning state and connection metadata only; connection credentials are never requested.

## RECOMMENDATION

Inspect the failing capability host or project in the Foundry portal or with az rest, correct the underlying cause (for example a missing role or a deleted bound resource), and redeploy the capability host. Recreate any missing connection with the same name the capability host references.

## EXAMPLES

```
resource capHost 'Microsoft.CognitiveServices/accounts/projects/capabilityHosts@2026-09-01' = {
  parent: project
  name: 'default'
  properties: {
    capabilityHostKind: 'Agents'
    storageConnections: [ storageConnection.name ]
    threadStorageConnections: [ cosmosConnection.name ]
    vectorStoreConnections: [ searchConnection.name ]
  }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM reads of project, capability host and connection provisioning state.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/capability-hosts](https://learn.microsoft.com/azure/foundry/agents/concepts/capability-hosts) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/cmd/doctor/checks_connections.go](https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/cmd/doctor/checks_connections.go) (last verified 2026-10-04)
