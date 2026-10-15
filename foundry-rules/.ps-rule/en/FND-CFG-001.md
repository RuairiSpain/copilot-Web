# FND-CFG-001

## SYNOPSIS

Valid YAML, duplicate-key detection and compatible azure.ai.* schema

## DESCRIPTION

azure.yaml must parse as YAML with unique mapping keys and conform to the published azd schema (v1.0) for the azure.ai.* service hosts: azure.ai.project, azure.ai.agent, azure.ai.connection, azure.ai.toolbox, azure.ai.skill, azure.ai.routine (and azure.ai.eval). Constraints checked are those the schema states as required, const, enum, type or "not": top-level name (required, minimum length 2, pattern ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$), services (optional; at least one entry when present), host required on every service, project required on azure.ai.agent, prompt agents requiring model and instructions, azure.ai.project forbidding project/runtime/docker/image/config, and the unsupported voice authoring shapes (modelType hosted_agent, targetAgent). Unknown properties are not errors because the schema allows additional properties. requiredVersions extension constraints are parsed (>=, >, <=, <, =, ~, a bare version, latest, and space-separated ranges as in the schema examples). A constraint that allows versions below the tested range, excludes the tested range, or cannot be parsed makes the result uncertain; it is never an error because the tested range is a verification window, not a platform requirement.

## RECOMMENDATION

Fix the YAML syntax or the reported schema violation. Add the yaml-language-server schema comment to azure.yaml so the editor flags the same errors, and install azure.ai.agents and azure.ai.projects at the versions declared in requiredVersions.

## EXAMPLES

```
# yaml-language-server: $schema=https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json
name: my-agent-project
services:
  ai-project:
    host: azure.ai.project
  my-agent:
    host: azure.ai.agent
    project: src/my-agent
    uses: [ai-project]
    kind: hosted
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs the azd azure.yaml v1.0 JSON schema and the azure.ai.* extension schemas (vendored at a pinned revision) and a YAML parser that reports duplicate keys. PSRule does neither.
- How to feed it: Run a schema validator and a duplicate-key-aware parser outside PSRule.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-05)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json](https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json) (last verified 2026-10-04)
