# FND-CFG-004

## SYNOPSIS

Prompt model or hosted source/image is valid

## DESCRIPTION

A prompt agent (kind prompt) must carry a non-blank model (a model deployment name) and non-blank instructions, and that deployment name should be one the project declares. A hosted agent (kind hosted) must have an agent source (project path that exists, with a Dockerfile for container builds) or a prebuilt image, must not combine codeConfiguration with image-based container configuration, must give codeConfiguration both runtime and entryPoint, and must keep container resources (container.resources.cpu and container.resources.memory) inside the documented range (cpu 0.25 to 4.0 cores, memory 0.5Gi to 8.0Gi; the schema allows cpu such as 500m and memory with Ki, Mi, Gi and decimal suffixes, which are normalised). Voice agents require a model unless they use conversationEngine.

## RECOMMENDATION

Set model to the name of a deployment declared on the project, provide instructions for prompt agents, and for hosted agents provide either source with a Dockerfile, a codeConfiguration, or a prebuilt image.

## EXAMPLES

```
services:
  ai-project:
    host: azure.ai.project
    deployments:
      - name: gpt-5.4-mini
        model: {format: OpenAI, name: gpt-5.4-mini, version: "2026-03-17"}
        sku: {name: GlobalStandard, capacity: 10}
  helper:
    host: azure.ai.agent
    project: src/helper
    uses: [ai-project]
    kind: prompt
    name: helper
    model: gpt-5.4-mini
    instructions: Answer briefly.
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs file-system checks (project directory, Dockerfile) and the documented container resource bounds. PSRule reads the azure.yaml object only.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-05)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml](https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.agents/schemas/azure.ai.agent.json) (last verified 2026-10-15)
