import { test } from 'node:test';
import assert from 'node:assert/strict';
import { calculateCost } from '../src/lib/costCalculator';
import { PriceSnapshot, UsageMetricRecord } from '../src/lib/types';

function baseRecord(overrides: Partial<UsageMetricRecord> = {}): UsageMetricRecord {
  return {
    timestamp: '2026-08-31T00:00:00.000Z',
    appId: 'app-1',
    productName: 'product-1',
    deploymentName: 'gpt-4.1',
    backendId: 'backend-1',
    customDimension1: 'Finance',
    customDimension2: 'user@example.com',
    gatewayName: 'gw-1',
    gatewayRegion: 'eastus',
    promptTokens: 1000,
    responseTokens: 500,
    totalTokens: 1500,
    completionAcceptedPredictionTokens: 0,
    completionAudioTokens: 0,
    completionReasoningTokens: 0,
    completionRejectedPredictionTokens: 0,
    promptAudioTokens: 0,
    promptCachedTokens: 0,
    ...overrides,
  };
}

function tokenPrice(overrides: Partial<PriceSnapshot> = {}): PriceSnapshot {
  return {
    id: 'gpt-4.1-v1',
    modelFamily: 'gpt-4.1',
    deploymentName: 'gpt-4.1',
    isActive: true,
    CostPerInputUnit: 2,
    CostPerOutputUnit: 8,
    CostPerCachedInputUnit: 0.5,
    CostPerAudioInputUnit: 40,
    CostPerCachedAudioInputUnit: 2.5,
    CostPerAudioOutputUnit: 80,
    CostPerReasoningOutputUnit: 8,
    CostPerImageInputUnit: 0,
    CostPerCachedImageInputUnit: 0,
    CostUnit: 1_000_000,
    BaseCost: 0,
    Currency: 'USD',
    CalculationMethod: 'tokens',
    region: 'eastus',
    priceVersion: 1,
    effectiveFrom: '2026-08-01T00:00:00.000Z',
    effectiveTo: null,
    docType: 'priceSnapshot',
    ...overrides,
  };
}

test('calculateCost: no matching price — $0 cost, totalCost null, calculationMethod "unknown" (fail loud in the data, not a throw)', () => {
  const result = calculateCost(baseRecord(), undefined);
  assert.equal(result.totalCost, null);
  assert.equal(result.calculationMethod, 'unknown');
  assert.equal(result.currency, 'UNKNOWN');
  assert.equal(result.inputCost, 0);
  assert.equal(result.outputCost, 0);
});

test('calculateCost: percentage (PTU) pricing — totalCost stays null, calculationMethod "percentage", every component $0', () => {
  const price = tokenPrice({ CalculationMethod: 'percentage', BaseCost: 5000 });
  const result = calculateCost(baseRecord(), price);
  assert.equal(result.totalCost, null);
  assert.equal(result.calculationMethod, 'percentage');
  assert.equal(result.inputCost, 0);
  assert.equal(result.outputCost, 0);
  assert.equal(result.audioCost, 0);
  assert.equal(result.reasoningCost, 0);
});

test('calculateCost: plain tokens — input/output cost computed from CostUnit-scaled rates', () => {
  const price = tokenPrice();
  const result = calculateCost(baseRecord({ promptTokens: 1_000_000, responseTokens: 1_000_000 }), price);
  assert.equal(result.inputCost, 2);
  assert.equal(result.outputCost, 8);
  assert.equal(result.calculationMethod, 'tokens');
  assert.equal(result.pricingVersion, 1);
  assert.equal(result.currency, 'USD');
});

test('calculateCost: cached prompt tokens are priced at the cached rate, subtracted from the plain input rate (no double-charging)', () => {
  const price = tokenPrice();
  // 1,000,000 prompt tokens, 300,000 of which are cached.
  const result = calculateCost(baseRecord({ promptTokens: 1_000_000, promptCachedTokens: 300_000, responseTokens: 0 }), price);
  // plain = 700,000 @ $2/M = $1.4 ; cached = 300,000 @ $0.5/M = $0.15
  assert.equal(result.inputCost, 1.4);
  assert.equal(result.cachedCost, 0.15);
  assert.equal(result.totalCost, 1.55);
});

test('calculateCost: prompt audio tokens are priced at the audio rate and excluded from the plain input rate', () => {
  const price = tokenPrice();
  const result = calculateCost(baseRecord({ promptTokens: 1_000_000, promptAudioTokens: 200_000, responseTokens: 0 }), price);
  // plain = 800,000 @ $2/M = $1.6 ; audio = 200,000 @ $40/M = $8
  assert.equal(result.inputCost, 1.6);
  assert.equal(result.audioCost, 8);
});

test('calculateCost: completion audio + reasoning tokens are priced at their own rates and excluded from the plain output rate', () => {
  const price = tokenPrice();
  const result = calculateCost(
    baseRecord({ promptTokens: 0, responseTokens: 1_000_000, completionAudioTokens: 100_000, completionReasoningTokens: 200_000 }),
    price
  );
  // plain = 700,000 @ $8/M = $5.6 ; audio = 100,000 @ $80/M = $8 ; reasoning = 200,000 @ $8/M = $1.6
  assert.equal(result.outputCost, 5.6);
  assert.equal(result.audioCost, 8);
  assert.equal(result.reasoningCost, 1.6);
  assert.equal(result.totalCost, 15.2);
});

test('calculateCost: audioCost sums BOTH prompt-side and completion-side audio, not just one', () => {
  const price = tokenPrice();
  const result = calculateCost(
    baseRecord({ promptTokens: 1_000_000, promptAudioTokens: 100_000, responseTokens: 1_000_000, completionAudioTokens: 100_000 }),
    price
  );
  // prompt audio = 100,000 @ $40/M = $4 ; completion audio = 100,000 @ $80/M = $8
  assert.equal(result.audioCost, 12);
});

test('calculateCost: totalCost is exactly the sum of the five components', () => {
  const price = tokenPrice();
  const record = baseRecord({
    promptTokens: 500_000,
    promptCachedTokens: 100_000,
    promptAudioTokens: 50_000,
    responseTokens: 300_000,
    completionAudioTokens: 20_000,
    completionReasoningTokens: 30_000,
  });
  const result = calculateCost(record, price);
  const sum = result.inputCost + result.outputCost + result.cachedCost + result.audioCost + result.reasoningCost;
  assert.equal(result.totalCost, Math.round(sum * 1e8) / 1e8);
});

test('calculateCost: zero-token record against a real price — every component is exactly $0, not NaN/undefined', () => {
  const price = tokenPrice();
  const result = calculateCost(baseRecord({ promptTokens: 0, responseTokens: 0 }), price);
  assert.equal(result.inputCost, 0);
  assert.equal(result.outputCost, 0);
  assert.equal(result.cachedCost, 0);
  assert.equal(result.audioCost, 0);
  assert.equal(result.reasoningCost, 0);
  assert.equal(result.totalCost, 0);
});

test('calculateCost: cached/audio tokens exceeding the raw prompt/completion count clamp to zero plain tokens, not negative', () => {
  const price = tokenPrice();
  // A malformed/edge-case record where cached exceeds the raw prompt count
  // (shouldn't happen from a real Azure OpenAI response, but defends
  // against it rather than producing a negative "plain" token count).
  const result = calculateCost(baseRecord({ promptTokens: 100, promptCachedTokens: 500, responseTokens: 0 }), price);
  assert.equal(result.inputCost, 0); // clamped to 0, not negative
});

test('calculateCost: rounds to 8 decimal places, not full floating-point precision', () => {
  const price = tokenPrice({ CostPerInputUnit: 1, CostUnit: 3 }); // deliberately produces a repeating decimal
  const result = calculateCost(baseRecord({ promptTokens: 1, responseTokens: 0 }), price);
  const str = String(result.inputCost);
  const decimalPlaces = str.includes('.') ? str.split('.')[1].length : 0;
  assert.ok(decimalPlaces <= 8, `expected at most 8 decimal places, got ${decimalPlaces} (${str})`);
});
