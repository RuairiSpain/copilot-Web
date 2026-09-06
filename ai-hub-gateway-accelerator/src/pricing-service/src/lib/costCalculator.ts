import { CostBreakdown, PriceSnapshot, UsageMetricRecord } from './types';

/**
 * Computes the cost breakdown for one usage record against the price
 * snapshot effective at the time the record was generated.
 *
 * `tokens`-priced models: real per-token-type cost, baked into the record.
 *
 * `percentage`-priced models (PTU / reserved capacity): totalCost is left
 * `null` here on purpose. Distributing a fixed capacity cost across
 * consumers needs each consumer's *share* of usage over the whole billing
 * period — a single request can't know that denominator in isolation. That
 * calculation stays in Power BI at report time, exactly as this
 * accelerator already does it (see guides/power-bi-dashboard.md,
 * "percentage" CalculationMethod) — this function only removes the live
 * *price* join for `tokens` models, it doesn't change how PTU chargeback
 * works.
 */
export function calculateCost(
  record: UsageMetricRecord,
  price: PriceSnapshot | undefined
): CostBreakdown {
  if (!price) {
    // No price snapshot covers this model/timestamp — fail loud in the
    // data, not silently. Surface as $0 with an explicit marker rather
    // than throwing and losing the whole batch; alert on this in
    // production (see guides/cost-attribution-guide.md, "operational
    // notes").
    return {
      inputCost: 0,
      outputCost: 0,
      cachedCost: 0,
      audioCost: 0,
      reasoningCost: 0,
      totalCost: null,
      currency: 'UNKNOWN',
      pricingVersion: null,
      calculationMethod: 'unknown',
    };
  }

  if (price.CalculationMethod === 'percentage') {
    return {
      inputCost: 0,
      outputCost: 0,
      cachedCost: 0,
      audioCost: 0,
      reasoningCost: 0,
      totalCost: null,
      currency: price.Currency,
      pricingVersion: price.priceVersion,
      calculationMethod: 'percentage',
    };
  }

  const unit = price.CostUnit || 1_000_000;

  // promptTokens from the accelerator's metric is the *total* prompt count;
  // promptCachedTokens/promptAudioTokens are subsets of it in the
  // Azure OpenAI usage shape, so subtract them out before pricing the
  // "plain" input tokens to avoid double-charging a cached/audio token at
  // both its own rate and the base input rate.
  const plainPromptTokens = Math.max(
    0,
    (record.promptTokens || 0) - (record.promptCachedTokens || 0) - (record.promptAudioTokens || 0)
  );
  const plainCompletionTokens = Math.max(
    0,
    (record.responseTokens || 0) -
      (record.completionAudioTokens || 0) -
      (record.completionReasoningTokens || 0)
  );

  const inputCost = (plainPromptTokens / unit) * price.CostPerInputUnit;
  const cachedCost = ((record.promptCachedTokens || 0) / unit) * price.CostPerCachedInputUnit;
  const promptAudioCost = ((record.promptAudioTokens || 0) / unit) * price.CostPerAudioInputUnit;

  const outputCost = (plainCompletionTokens / unit) * price.CostPerOutputUnit;
  const completionAudioCost =
    ((record.completionAudioTokens || 0) / unit) * price.CostPerAudioOutputUnit;
  const reasoningCost =
    ((record.completionReasoningTokens || 0) / unit) * price.CostPerReasoningOutputUnit;

  const audioCost = promptAudioCost + completionAudioCost;
  const totalCost = inputCost + outputCost + cachedCost + audioCost + reasoningCost;

  return {
    inputCost: round(inputCost),
    outputCost: round(outputCost),
    cachedCost: round(cachedCost),
    audioCost: round(audioCost),
    reasoningCost: round(reasoningCost),
    totalCost: round(totalCost),
    currency: price.Currency,
    pricingVersion: price.priceVersion,
    calculationMethod: 'tokens',
  };
}

// Cost values are tiny (fractions of a cent per request); keep 8 decimal
// places so per-request rounding error doesn't compound into a visible
// drift once millions of records are summed in Power BI.
function round(n: number): number {
  return Math.round(n * 1e8) / 1e8;
}
