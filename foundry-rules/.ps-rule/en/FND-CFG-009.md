# FND-CFG-009

## SYNOPSIS

Cron expressions are valid

## DESCRIPTION

In the reviewed azure.ai.routines 1.0.0-beta.8 schema shape, a trigger whose type is recurring and whose schedule key is cron_expression must carry a valid 5-field cron expression with an interval of at least five minutes. Other type/key spellings and time_zone requirements conflict across the reviewed sources and are not treated as equivalent.

## RECOMMENDATION

Use a standard five-field cron expression that fires no more often than every five minutes and set time_zone to a valid identifier.

## EXAMPLES

```
# Partial routine fragment using the azure.ai.routines 1.0.0-beta.8 schema spelling.
services:
  daily-summary:
    host: azure.ai.routine
    uses: [summarizer]
    triggers:
      weekday-morning:
        type: recurring
        cron_expression: "0 7 * * 1-5"
        time_zone: UTC
    action:
      type: invoke_agent_responses_api
      agent_name: summarizer
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: The placement of the recurring trigger in azure.yaml is a preview shape not carried in the catalogue evidence; a five-field cron parser with firing simulation is also needed.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/use-routines](https://learn.microsoft.com/azure/foundry/agents/how-to/use-routines) (last verified 2026-10-04)
- [https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.routines/schemas/azure.ai.routine.json](https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/azure.ai.routines/schemas/azure.ai.routine.json) (last verified 2026-10-04)
