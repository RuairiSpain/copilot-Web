# FND-COST-003

## SYNOPSIS

Gateway token limits exist

## DESCRIPTION

When Azure API Management fronts Foundry or Azure OpenAI model endpoints (the AI gateway pattern, Phase 8), the LLM API should apply the llm-token-limit policy so that a single caller key cannot consume unbounded tokens. The policy limits tokens per minute, a token quota over a period, or both, and returns 429 for rate and 403 for quota breaches. This rule references the policy only; gateway topology rules belong to the GW group.

## RECOMMENDATION

Add an llm-token-limit policy to the inbound section of the LLM API, keyed by caller (for example subscription ID), with tokens-per-minute and, where a budget per period is needed, a token-quota with a token-quota-period.

## EXAMPLES

```
<inbound>
  <llm-token-limit counter-key="@(context.Subscription.Id)"
                   tokens-per-minute="5000"
                   token-quota="1000000"
                   token-quota-period="Monthly"
                   estimate-prompt-tokens="true" />
</inbound>
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs effective API Management policy XML across global, product, API and operation scope, with include-fragment resolution.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/api-management/llm-token-limit-policy](https://learn.microsoft.com/azure/api-management/llm-token-limit-policy) (last verified 2026-10-04)
