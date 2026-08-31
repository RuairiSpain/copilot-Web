// Required by the "PTU / FIXED-CAPACITY COST" section of 01-measures.dax.
// Two custom columns, both added to the EXISTING llm-usage-container
// query (Transform Data -> select llm-usage-container -> Add Column ->
// Custom Column), same place as 05-cost-avoidance-column.m if you're
// using both.

// ============================================================
// Column name: billingMonth
// First-of-month date for the record's timestamp — makes "this
// deployment's fixed cost, this month" grouping simple instead of
// re-deriving YEAR()/MONTH() combinations in every measure.
// ============================================================
= Date.StartOfMonth(Date.From([timestamp]))

// ============================================================
// Column name: ptuWeightTokens
// Which token count to use as this record's share of a PTU/fixed-cost
// deployment's monthly bill — resolved from model-pricing rather than
// hardcoded, per the accelerator's own convention (guides/power-bi-dashboard.md,
// "Handling PTU and other fixed-cost services": whichever CostPerXUnit
// field is set to 1 IS the distribution weight). Returns null for
// non-percentage-priced records (harmless — [PTU Allocated Cost] only
// sums deployments where model-pricing[CalculationMethod] = "percentage").
// ============================================================
= let
    dep = [deploymentName],
    priceRow = Table.SelectRows(
        #"model-pricing",  // the existing model-pricing query in this .pbix
        each [deploymentName] = dep and [CalculationMethod] = "percentage"
    ),
    price = if Table.IsEmpty(priceRow) then null else priceRow{0}
  in
    if price = null then null
    else if price[CostPerOutputUnit] = 1 then [responseTokens]
    else if price[CostPerInputUnit] = 1 then [promptTokens]
    else if price[CostPerCachedInputUnit] = 1 then [promptCachedTokens]
    else if price[CostPerAudioInputUnit] = 1 then [promptAudioTokens]
    else if price[CostPerAudioOutputUnit] = 1 then [completionAudioTokens]
    else if price[CostPerReasoningOutputUnit] = 1 then [completionReasoningTokens]
    else [totalTokens]  // fallback if no field is set to exactly 1 — shouldn't
                         // happen per the guide's convention, but degrades to
                         // "weight by total volume" instead of erroring
