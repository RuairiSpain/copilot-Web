# FND-OPS-008

## SYNOPSIS

Evaluations run before promotion

## DESCRIPTION

An agent project that is promoted to test or production should have a repeatable evaluation recipe and a pipeline step that runs it before promotion. In azd the recipe is eval.yaml (agent target, dataset, evaluator references, thresholds) and the documented CI command is azd ai agent eval run. This is a Foundry Doctor recommendation built on the documented commands.

## RECOMMENDATION

Generate the evaluation recipe with azd ai agent eval generate, commit eval.yaml, and run azd ai agent eval run in the pipeline against the candidate version; promote to production only after the evaluation passes your thresholds.

## EXAMPLES

```
# in .github/workflows/azure-dev.yml, before the production promotion job
- run: azd ai agent eval run --no-prompt
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs eval.yaml and pipeline files, and a step ordering check.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/set-up-ci-cd-cli](https://learn.microsoft.com/azure/foundry/agents/how-to/set-up-ci-cd-cli) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/observability/how-to/azure-developer-cli-evaluation](https://learn.microsoft.com/azure/foundry/observability/how-to/azure-developer-cli-evaluation) (last verified 2026-10-04)
