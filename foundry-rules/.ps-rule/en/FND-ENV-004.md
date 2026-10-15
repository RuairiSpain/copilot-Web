# FND-ENV-004

## SYNOPSIS

Informational difference summary

## DESCRIPTION

Summarise how two or more azd environments of the project differ so that reviewers can see intended and accidental differences before promotion: keys present in only some environments, keys whose values differ, and which differing keys are environment-bound. Informational only; never fails a run.

## RECOMMENDATION

Review the summary before promoting a release; use azd env get-values per environment for the details you are entitled to see.

## EXAMPLES

```
foundry-doctor compare --env dev --env prod
```

## NOTES

- Severity dev/test/prod: info/info/info (PSRule: Information/Information/Information).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs two or more .env files. Informational difference summary with no pass or fail outcome.
- How to feed it: Same as FND-ENV-001.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.

## LINKS

- [https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview](https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview) (last verified 2026-10-05)
- [https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/environment/local_file_data_store.go](https://github.com/Azure/azure-dev/blob/azure-dev-cli_1.35.0/cli/azd/pkg/environment/local_file_data_store.go) (last verified 2026-10-05)
