// OPTIONAL — powers the "Cost Avoidance" section of the Org page
// (PRD FR-022: what routing saved vs. sending everything to a premium
// model). Skip this file entirely if you don't need that section; nothing
// else in this kit depends on it.
//
// Unlike 03-supporting-tables.m (new blank queries), this is a CUSTOM
// COLUMN added to your EXISTING llm-usage-container query:
//   Transform Data -> select the llm-usage-container query -> Add Column
//   -> Custom Column -> name it "PremiumEquivalentCost" -> paste the
//   function body below (everything after "=") -> OK.
//
// It computes, for every request, what that same token count would have
// cost if it had been routed to your organization's premium/benchmark
// model instead — resolved against the versioned model-pricing table at
// the SAME point in time as the request (a real date-range join, not
// "today's price"), same principle as the pricing-service enrichPricing
// function this whole change is built around.

let
    // Set this to your organization's premium/benchmark model name —
    // the one every "what if we hadn't routed intelligently" comparison
    // is measured against. Must match a `modelFamily` value in
    // model-pricing (the display-only field — NOT the `deploymentName`
    // pricing join key; see guides/power-bi-dashboard.md's "model-pricing
    // field naming" section). If more than one deployment shares this
    // modelFamily, matchingPrices{0} below picks whichever one Power
    // Query returns first — fine for a one-deployment premium model,
    // worth a tighter filter (add a specific deploymentName) if you have
    // more than one.
    premiumModel = "gpt-5.2",

    ts = [timestamp],

    matchingPrices = Table.SelectRows(
        #"model-pricing",  // the existing model-pricing query in this .pbix
        each [modelFamily] = premiumModel
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
    // exactly — all 5 cost components (input/output/cached/audio/reasoning),
    // not just the first 3. The audio and reasoning terms were missing here
    // in an earlier revision, which understated PremiumEquivalentCost (and
    // therefore Cost Avoidance $/%) for any request that used audio or
    // reasoning tokens against the premium benchmark model.
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

// After adding the column: Close & Apply, then add the three measures
// from the "COST AVOIDANCE" section of 01-measures.dax.
//
// Note: this recomputes for every row on every refresh (a Power Query
// step, not baked in at ingestion like the real `cost` field is) — fine
// for a report-time "what if" comparison, since unlike actual chargeback
// it's a hypothetical, not something that needs to stay fixed once a
// billing period closes.
