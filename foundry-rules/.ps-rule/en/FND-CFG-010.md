# FND-CFG-010

## SYNOPSIS

Model versions pinned in stricter profiles

## DESCRIPTION

Model deployments should state an explicit model version and an explicit version upgrade policy so that a default-version change does not alter production behaviour unnoticed. azure.yaml deployments require a model.version (schema-enforced); Bicep or ARM deployments carry versionUpgradeOption, where an absent value behaves as OnceCurrentVersionExpired and OnceNewDefaultVersionAvailable upgrades to a new default within two weeks.

## RECOMMENDATION

Pin model.version, and in Bicep set versionUpgradeOption explicitly (NoAutoUpgrade pins until retirement, OnceCurrentVersionExpired upgrades only at retirement). Track retirement dates so a pinned version is moved before it stops working.

## EXAMPLES

```
services:
  ai-project:
    host: azure.ai.project
    deployments:
      - name: gpt-5.4-mini
        model: {format: OpenAI, name: gpt-5.4-mini, version: "2026-03-17"}
        sku: {name: GlobalStandard, capacity: 10}
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Compiled ARM deployments only: model.version missing or latest, and versionUpgradeOption OnceNewDefaultVersionAvailable. The azure.yaml deployments side and model-router handling are not evaluated. The catalogue marks the API version that introduces versionUpgradeOption as unverified.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/openai/how-to/working-with-models](https://learn.microsoft.com/azure/foundry/openai/how-to/working-with-models) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.projects/schemas/azure.ai.project.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.projects/schemas/azure.ai.project.json) (last verified 2026-10-04)
