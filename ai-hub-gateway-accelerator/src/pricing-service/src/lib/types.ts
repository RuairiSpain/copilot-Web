/**
 * Shared types for the pricing-service Functions app.
 *
 * PriceSnapshot mirrors the shape already used by
 * src/usage-reports/model-pricing-generated-extended.json, extended with
 * the versioning fields this change introduces (effectiveFrom/effectiveTo/
 * priceVersion/docType). Snapshots are append-only: a price change writes a
 * *new* document, never edits an existing one, so historical chargeback
 * stays correct after a price change.
 */
export interface PriceSnapshot {
  id: string; // "{deploymentName}-v{priceVersion}"
  /**
   * Informational/display only — e.g. "gpt-4.1", the canonical model
   * family name. NOT the pricing join key (see resolveEffectivePrice()
   * in cosmos.ts, which matches on `deploymentName`). Renamed from
   * `model` — that name invited exactly the confusion it caused: a
   * usage record's `deploymentName` was always matched against this
   * field's *old* name, `model`, silently requiring every operator to
   * seed it with their own Azure deployment name (not a model family
   * name) with nothing enforcing or explaining that. See
   * guides/cost-attribution-guide.md's "model-pricing field naming"
   * section for the full story and the breaking-change migration note.
   */
  modelFamily: string;
  deploymentName: string;
  isActive: boolean;
  CostPerInputUnit: number;
  CostPerOutputUnit: number;
  CostPerCachedInputUnit: number;
  CostPerAudioInputUnit: number;
  CostPerCachedAudioInputUnit: number;
  CostPerAudioOutputUnit: number;
  CostPerReasoningOutputUnit: number;
  CostPerImageInputUnit: number;
  CostPerCachedImageInputUnit: number;
  CostUnit: number;
  BaseCost: number;
  Currency: string;
  CalculationMethod: 'tokens' | 'percentage';
  region: string;
  priceVersion: number;
  effectiveFrom: string; // ISO 8601
  effectiveTo: string | null; // ISO 8601, null = still current
  docType: 'priceSnapshot';
}

/** The `current::{model}` pointer document — one per model, always overwritten
 *  in place (this is the *only* mutable document in the container; every
 *  historical snapshot it points at is immutable). */
export interface CurrentPricePointer extends Omit<PriceSnapshot, 'id' | 'docType'> {
  id: `current::${string}`;
  docType: 'currentPricePointer';
  snapshotId: string;
}

/** One row of the metrics array Parse_Metrics_Logs already produces in
 *  llm-usage-ingestion/workflow.json. */
export interface UsageMetricRecord {
  timestamp: string;
  appId: string;
  productName: string;
  deploymentName: string;
  backendId: string;
  customDimension1: string;
  customDimension2: string;
  gatewayName: string;
  gatewayRegion: string;
  promptTokens: number;
  responseTokens: number;
  totalTokens: number;
  completionAcceptedPredictionTokens: number;
  completionAudioTokens: number;
  completionReasoningTokens: number;
  completionRejectedPredictionTokens: number;
  promptAudioTokens: number;
  promptCachedTokens: number;
}

export interface CostBreakdown {
  inputCost: number;
  outputCost: number;
  cachedCost: number;
  audioCost: number;
  reasoningCost: number;
  totalCost: number | null; // null for percentage/PTU models — see README
  currency: string;
  pricingVersion: number | null;
  calculationMethod: 'tokens' | 'percentage' | 'unknown';
}

export type EnrichedUsageMetricRecord = UsageMetricRecord & { cost: CostBreakdown };
