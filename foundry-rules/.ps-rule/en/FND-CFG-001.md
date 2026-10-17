# FND-CFG-001

## SYNOPSIS

Valid YAML, duplicate-key detection and compatible azure.ai.* schema

## DESCRIPTION

azure.yaml must parse as YAML with unique mapping keys and conform to the published azd schema (v1.0) for the azure.ai.* service hosts: azure.ai.project, azure.ai.agent, azure.ai.connection, azure.ai.toolbox, azure.ai.skill, azure.ai.routine (and azure.ai.eval). Constraints checked are those the schema states as required, const, enum, type or "not": top-level name (required, minimum length 2, pattern ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$), services (optional; at least one entry when present), host required on every service, project required on azure.ai.agent, prompt agents requiring model and instructions, azure.ai.project forbidding project/runtime/docker/image/config, and the unsupported voice authoring shapes (modelType hosted_agent, targetAgent). Unknown properties are not errors because the schema allows additional properties. requiredVersions.azd is parsed with the grammar of blang/semver/v4 ParseRange (operators < <= > >= = == ! !=, space-separated AND, || OR, lowercase x wildcards; comma, ~, ^, =>, =<, X, latest, the empty string and a leading or trailing || are not accepted; one-character tokens, x outside the exact N.x and N.M.x forms and "!" followed by a space behave unexpectedly in the library, as do a dangling operator, an operator and a version separated by a space that does not parse, and repeated ||, which the library accepts but panics on when matching; all of these are reported as uncertain, not as errors): a value that is not a valid range is an error because azd fails to load the project, and so is a known azd version (flag, environment variable or probe) outside the range; a local dev build (0.0.0-dev.0) is exempt from the range comparison, as in azd, but not from range validity: azd parses the range (project.go:64) before it skips a dev build (:70), so an invalid range is reported for a dev build too. The exemption is unit-level only: outside the CFG-001 azd window the whole rule is skipped (unsupported-version). An unknown azd version makes the result uncertain (azd-version-unknown). requiredVersions.extensions values are parsed with the grammar of Masterminds/semver/v3 NewConstraint (= != > < >= => <= =< ~ ~> ^, a bare version, space or comma as AND, || as OR, hyphen ranges, x X * wildcards, latest or empty for any). A value that is not valid is uncertain, never an error, because azd falls back to an exact tag match. A constraint that allows versions below the tested range, excludes the tested range, requires versions above it, or lacks a pre-release (so azd rejects the pre-release tested versions) makes the result uncertain or informational; it is never an error because the tested range is a verification window, not a platform requirement.

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
- [https://raw.githubusercontent.com/blang/semver/v4.0.0/v4/range.go](https://raw.githubusercontent.com/blang/semver/v4.0.0/v4/range.go) (last verified 2026-10-16)
- [https://raw.githubusercontent.com/Masterminds/semver/v3.4.0/constraints.go](https://raw.githubusercontent.com/Masterminds/semver/v3.4.0/constraints.go) (last verified 2026-10-16)
- [https://github.com/Azure/azure-dev/blob/1bd2ca4699c7b1f371db85dd867f18aa4fbc4730/cli/azd/pkg/project/project.go](https://github.com/Azure/azure-dev/blob/1bd2ca4699c7b1f371db85dd867f18aa4fbc4730/cli/azd/pkg/project/project.go) (last verified 2026-10-16)
