# FND-GW-002

## SYNOPSIS

Verified token-limit/metric policies are present

## DESCRIPTION

An API Management API that fronts a model endpoint should apply the AI gateway token governance policies with the verified names llm-token-limit (inbound) and llm-emit-token-metric (inbound), configured validly: counter-key and estimate-prompt-tokens present, at least one of tokens-per-minute or token-quota with token-quota-period, and no more than five dimensions on the metric. The earlier azure-openai-token-limit and azure-openai-emit-token-metric names are not documented in the current policy reference and are not accepted as evidence.

## RECOMMENDATION

Add llm-token-limit to cap tokens per caller and llm-emit-token-metric to record token consumption in Application Insights, keyed on a caller identity (for example subscription ID or client ID) rather than a shared constant.

## EXAMPLES

```
<inbound>
  <base />
  <llm-token-limit counter-key="@(context.Subscription.Id)"
      tokens-per-minute="5000" estimate-prompt-tokens="false" />
  <llm-emit-token-metric namespace="llm-metrics">
    <dimension name="Subscription ID" />
    <dimension name="API ID" />
  </llm-emit-token-metric>
</inbound>
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs effective API Management policy XML (llm-token-limit, llm-emit-token-metric).
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/api-management/llm-token-limit-policy](https://learn.microsoft.com/azure/api-management/llm-token-limit-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/llm-emit-token-metric-policy](https://learn.microsoft.com/azure/api-management/llm-emit-token-metric-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/genai-gateway-capabilities](https://learn.microsoft.com/azure/api-management/genai-gateway-capabilities) (last verified 2026-10-04)
