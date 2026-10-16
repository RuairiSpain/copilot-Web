# FND-OPS-007

## SYNOPSIS

CI/CD pipeline exists

## DESCRIPTION

A project that targets test or production environments should have a CI/CD pipeline definition that provisions and deploys it, instead of relying on developer workstations. For azd projects the documented pipeline definition is azure-dev.yml created by azd pipeline config for GitHub Actions or Azure Pipelines. Whether a pipeline is required is a Foundry Doctor recommendation, not a platform rule.

## RECOMMENDATION

Run azd pipeline config to generate azure-dev.yml and the federated credential for GitHub Actions or Azure Pipelines, and use one pipeline per azd environment.

## EXAMPLES

```
azd pipeline config --provider github
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs the pipeline files under .github/workflows, .azdo/pipelines and .azuredevops/pipelines from the git root, and shell command tokenising.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/developer/azure-developer-cli/configure-devops-pipeline](https://learn.microsoft.com/azure/developer/azure-developer-cli/configure-devops-pipeline) (last verified 2026-10-05)
- [https://github.com/Azure/azure-dev/blob/main/schemas/v1.0/azure.yaml.json](https://github.com/Azure/azure-dev/blob/main/schemas/v1.0/azure.yaml.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/set-up-ci-cd-cli](https://learn.microsoft.com/azure/foundry/agents/how-to/set-up-ci-cd-cli) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/pipeline/pipeline.go](https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/pipeline/pipeline.go) (last verified 2026-10-05)
