// Required by 08-payg-ptu-breakeven-measures.dax.
// Custom column on the EXISTING llm-usage-container query, same place as
// 05-cost-avoidance-column.m and 06-ptu-allocation-columns.m if you're
// using those too.
//
// Column name: paygEquivalentCost
//
// For EVERY record (whether it actually ran on a PTU or a PAYG
// deployment), computes what that record's tokens would have cost on the
// PAYG rate for the *same underlying model* — resolved at the price
// effective at the record's own timestamp, same principle as
// pricing-service's enrichPricing.
//
// ASSUMPTION: PTU deployments are named "{baseDeployment}-PTU" and a
// matching tokens-priced entry whose `deploymentName` equals
// "{baseDeployment}" exists in model-pricing (true for gpt-5.2 /
// gpt-5.2-PTU in the seed data; gpt-5.1-PTU has no matching gpt-5.1 PAYG
// entry in the seed data as shipped — add one, or this column returns
// null for that deployment, same as any other unresolved lookup, not an
// error). Adjust the naming convention below if your deployments don't
// follow "-PTU". Matches on `deploymentName` — the real pricing join key
// (see guides/power-bi-dashboard.md's "model-pricing field naming"
// section) — not the display-only `modelFamily` field.

= let
    dep = [deploymentName],
    ts = [timestamp],
    isPTU = Text.EndsWith(dep, "-PTU"),
    baseDeployment = if isPTU then Text.Range(dep, 0, Text.Length(dep) - 4) else dep,
    matchingPrices = Table.SelectRows(
        #"model-pricing",
        each [deploymentName] = baseDeployment
            and [CalculationMethod] = "tokens"
            and [effectiveFrom] <= ts
            and ([effectiveTo] = null or [effectiveTo] > ts)
    ),
    price = if Table.IsEmpty(matchingPrices) then null else matchingPrices{0},
    plainPromptTokens =
        List.Max({0, [promptTokens] - [promptCachedTokens] - [promptAudioTokens]}),
    plainCompletionTokens =
        List.Max({0, [responseTokens] - [completionAudioTokens] - [completionReasoningTokens]}),
    unit = if price = null then 1000000 else price[CostUnit],
    // Mirrors src/pricing-service/src/lib/costCalculator.ts's calculateCost()
    // exactly — all 5 cost components. Audio and reasoning terms were
    // missing here in an earlier revision, understating paygEquivalentCost
    // (and therefore PTU vs PAYG Savings and the breakeven calculation) for
    // any deployment whose traffic includes audio or reasoning tokens.
    result =
        if price = null then null else
        (plainPromptTokens / unit * price[CostPerInputUnit])
        + (plainCompletionTokens / unit * price[CostPerOutputUnit])
        + ([promptCachedTokens] / unit * price[CostPerCachedInputUnit])
        + ([promptAudioTokens] / unit * price[CostPerAudioInputUnit])
        + ([completionAudioTokens] / unit * price[CostPerAudioOutputUnit])
        + ([completionReasoningTokens] / unit * price[CostPerReasoningOutputUnit])
  in
    result
