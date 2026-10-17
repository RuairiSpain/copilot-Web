# FND-SEC-007

## SYNOPSIS

RAI/content filter bound to chat deployments

## DESCRIPTION

Chat model deployments and hosted agents should be bound to a valid guardrail (RAI policy). A model deployment without properties.raiPolicyName is assigned Microsoft.DefaultV2 by the platform; the rule fails when raiPolicyName names a policy that is not Microsoft-provided or not declared, and when a hosted agent in azure.yaml has no rai_policy entry or a short name instead of the full ARM resource ID.

## RECOMMENDATION

Set properties.raiPolicyName on each deployment to a policy defined on the account and, for hosted agents, add a policies entry of type rai_policy with the full ARM resource ID in azure.yaml. After deployment, test that the guardrail blocks a known-bad prompt.

## EXAMPLES

```
# azure.yaml
services:
  my-agent:
    host: azure.ai.agent
    kind: hosted
    policies:
      - type: rai_policy
        raiPolicyName: ${RAI_POLICY_RESOURCE_ID}  # full ARM resource ID of the raiPolicies resource
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs raiPolicies resources correlated with deployments, and azure.yaml policies entries. The catalogue notes the two Foundry articles conflict.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.

## LINKS

- [https://learn.microsoft.com/azure/foundry/guardrails/guardrails-overview](https://learn.microsoft.com/azure/foundry/guardrails/guardrails-overview) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/add-hosted-agent-guardrails](https://learn.microsoft.com/azure/foundry/agents/how-to/add-hosted-agent-guardrails) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
