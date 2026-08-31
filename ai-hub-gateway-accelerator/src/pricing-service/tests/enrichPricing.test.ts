import { test } from 'node:test';
import assert from 'node:assert/strict';
import { enrichPricing } from '../src/functions/enrichPricing';
import { makeFakeContext, makeFakeRequest } from './helpers/fakeHttpRequest';
import { PriceSnapshot, UsageMetricRecord } from '../src/lib/types';

function record(overrides: Partial<UsageMetricRecord> = {}): UsageMetricRecord {
  return {
    timestamp: '2026-08-15T00:00:00.000Z',
    appId: 'app-1',
    productName: 'product-1',
    deploymentName: 'gpt-4.1',
    backendId: 'backend-1',
    customDimension1: 'Finance',
    customDimension2: 'user@example.com',
    gatewayName: 'gw-1',
    gatewayRegion: 'eastus',
    promptTokens: 1_000_000,
    responseTokens: 500_000,
    totalTokens: 1_500_000,
    completionAcceptedPredictionTokens: 0,
    completionAudioTokens: 0,
    completionReasoningTokens: 0,
    completionRejectedPredictionTokens: 0,
    promptAudioTokens: 0,
    promptCachedTokens: 0,
    ...overrides,
  };
}

function snapshot(overrides: Partial<PriceSnapshot> = {}): PriceSnapshot {
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

test('enrichPricing: malformed JSON body — 400', async () => {
  const request = makeFakeRequest({ bodyThrows: true });
  const context = makeFakeContext();
  const res = await enrichPricing(request, context, { loadAllSnapshots: async () => [] });
  assert.equal(res.status, 400);
});

test('enrichPricing: body is not an array — 400', async () => {
  const request = makeFakeRequest({ body: { not: 'an array' } });
  const context = makeFakeContext();
  const res = await enrichPricing(request, context, { loadAllSnapshots: async () => [] });
  assert.equal(res.status, 400);
});

test('enrichPricing: empty array — 200 with an empty array, catalog still loaded once', async () => {
  const request = makeFakeRequest({ body: [] });
  const context = makeFakeContext();
  let loadCalls = 0;
  const res = await enrichPricing(request, context, {
    loadAllSnapshots: async () => {
      loadCalls += 1;
      return [];
    },
  });
  assert.deepEqual(res.jsonBody, []);
  assert.equal(loadCalls, 1);
});

test('enrichPricing: catalog is loaded exactly ONCE for a batch of N records, not N times', async () => {
  const request = makeFakeRequest({ body: [record(), record(), record()] });
  const context = makeFakeContext();
  let loadCalls = 0;
  await enrichPricing(request, context, {
    loadAllSnapshots: async () => {
      loadCalls += 1;
      return [snapshot()];
    },
  });
  assert.equal(loadCalls, 1);
});

test('enrichPricing: happy path — every record gets a resolved cost attached', async () => {
  const request = makeFakeRequest({ body: [record()] });
  const context = makeFakeContext();
  const res = await enrichPricing(request, context, { loadAllSnapshots: async () => [snapshot()] });
  const body = res.jsonBody as Array<{ cost: { totalCost: number | null; calculationMethod: string } }>;
  assert.equal(body.length, 1);
  assert.equal(body[0]!.cost.calculationMethod, 'tokens');
  assert.notEqual(body[0]!.cost.totalCost, null);
});

test('enrichPricing: unresolved price for one record — logged as a warning, batch still returned (fail loud in the data, not the response)', async () => {
  const request = makeFakeRequest({ body: [record({ deploymentName: 'unknown-deployment' }), record()] });
  const context = makeFakeContext();
  const res = await enrichPricing(request, context, { loadAllSnapshots: async () => [snapshot()] });
  const body = res.jsonBody as Array<{ cost: { totalCost: number | null } }>;
  assert.equal(body.length, 2);
  assert.equal(body[0]!.cost.totalCost, null); // unresolved
  assert.notEqual(body[1]!.cost.totalCost, null); // resolved
  assert.equal(context.warns.length, 1);
  assert.match(String(context.warns[0]![0]), /1\/2 record\(s\) had no matching price snapshot/);
});

test('enrichPricing: all records resolved — no warning logged', async () => {
  const request = makeFakeRequest({ body: [record(), record()] });
  const context = makeFakeContext();
  await enrichPricing(request, context, { loadAllSnapshots: async () => [snapshot()] });
  assert.equal(context.warns.length, 0);
});

test('enrichPricing: Cosmos catalog load failure — 502, consistent error shape (this session\'s own code-quality review)', async () => {
  const request = makeFakeRequest({ body: [record()] });
  const context = makeFakeContext();
  const res = await enrichPricing(request, context, {
    loadAllSnapshots: async () => {
      throw new Error('Cosmos unavailable');
    },
  });
  assert.equal(res.status, 502);
  assert.match((res.jsonBody as { error: string }).error, /temporary data-access error/);
});
