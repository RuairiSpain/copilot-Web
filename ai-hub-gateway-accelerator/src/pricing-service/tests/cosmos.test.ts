import { test } from 'node:test';
import assert from 'node:assert/strict';
import { _resetClientCacheForTests, getPricingContainer, resolveEffectivePrice } from '../src/lib/cosmos';
import { PriceSnapshot } from '../src/lib/types';

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

test('resolveEffectivePrice: no snapshot for the deployment at all — undefined', () => {
  const result = resolveEffectivePrice([snapshot({ deploymentName: 'other-deployment' })], 'gpt-4.1', '2026-08-15T00:00:00.000Z');
  assert.equal(result, undefined);
});

test('resolveEffectivePrice: timestamp before effectiveFrom — undefined (not yet effective)', () => {
  const result = resolveEffectivePrice([snapshot({ effectiveFrom: '2026-08-10T00:00:00.000Z' })], 'gpt-4.1', '2026-08-05T00:00:00.000Z');
  assert.equal(result, undefined);
});

test('resolveEffectivePrice: timestamp exactly at effectiveTo — undefined (half-open interval, effectiveTo is exclusive)', () => {
  const result = resolveEffectivePrice(
    [snapshot({ effectiveFrom: '2026-08-01T00:00:00.000Z', effectiveTo: '2026-08-15T00:00:00.000Z' })],
    'gpt-4.1',
    '2026-08-15T00:00:00.000Z'
  );
  assert.equal(result, undefined);
});

test('resolveEffectivePrice: timestamp exactly at effectiveFrom — matches (inclusive start)', () => {
  const result = resolveEffectivePrice([snapshot({ effectiveFrom: '2026-08-01T00:00:00.000Z' })], 'gpt-4.1', '2026-08-01T00:00:00.000Z');
  assert.equal(result?.id, 'gpt-4.1-v1');
});

test('resolveEffectivePrice: still-current snapshot (effectiveTo: null) matches any timestamp after effectiveFrom', () => {
  const result = resolveEffectivePrice([snapshot({ effectiveFrom: '2026-08-01T00:00:00.000Z', effectiveTo: null })], 'gpt-4.1', '2027-01-01T00:00:00.000Z');
  assert.equal(result?.id, 'gpt-4.1-v1');
});

test('resolveEffectivePrice: picks the correct historical version for a timestamp between two price changes', () => {
  const catalog = [
    snapshot({ id: 'gpt-4.1-v1', priceVersion: 1, effectiveFrom: '2026-07-01T00:00:00.000Z', effectiveTo: '2026-08-01T00:00:00.000Z', CostPerInputUnit: 3 }),
    snapshot({ id: 'gpt-4.1-v2', priceVersion: 2, effectiveFrom: '2026-08-01T00:00:00.000Z', effectiveTo: '2026-09-01T00:00:00.000Z', CostPerInputUnit: 2 }),
    snapshot({ id: 'gpt-4.1-v3', priceVersion: 3, effectiveFrom: '2026-09-01T00:00:00.000Z', effectiveTo: null, CostPerInputUnit: 1.5 }),
  ];
  // Confirms the whole point of versioned pricing: a record from mid-July
  // resolves against the OLD (v1) rate, not today's (v3) rate — no
  // retroactive drift when a price changes later.
  assert.equal(resolveEffectivePrice(catalog, 'gpt-4.1', '2026-07-15T00:00:00.000Z')?.id, 'gpt-4.1-v1');
  assert.equal(resolveEffectivePrice(catalog, 'gpt-4.1', '2026-08-15T00:00:00.000Z')?.id, 'gpt-4.1-v2');
  assert.equal(resolveEffectivePrice(catalog, 'gpt-4.1', '2026-12-01T00:00:00.000Z')?.id, 'gpt-4.1-v3');
});

test('resolveEffectivePrice: matches on deploymentName, never on modelFamily — the real pricing join key', () => {
  // Two deployments sharing the same modelFamily but different deployment
  // names (and different rates) — matching on the wrong field would
  // silently price a request against the wrong deployment's rate.
  const catalog = [
    snapshot({ id: 'a', deploymentName: 'gpt-4.1-eastus', modelFamily: 'gpt-4.1', CostPerInputUnit: 2 }),
    snapshot({ id: 'b', deploymentName: 'gpt-4.1-westus', modelFamily: 'gpt-4.1', CostPerInputUnit: 5 }),
  ];
  assert.equal(resolveEffectivePrice(catalog, 'gpt-4.1-westus', '2026-08-15T00:00:00.000Z')?.CostPerInputUnit, 5);
});

test('resolveEffectivePrice: overlapping snapshots (shouldn\'t happen if effectiveTo is maintained correctly) — prefers the most recently effective', () => {
  const catalog = [
    snapshot({ id: 'older', effectiveFrom: '2026-08-01T00:00:00.000Z', effectiveTo: null }),
    snapshot({ id: 'newer', effectiveFrom: '2026-08-10T00:00:00.000Z', effectiveTo: null }),
  ];
  assert.equal(resolveEffectivePrice(catalog, 'gpt-4.1', '2026-08-15T00:00:00.000Z')?.id, 'newer');
});

test('getPricingContainer: missing CosmosDB_Endpoint app setting throws a clear error, not a crash deep in the SDK', () => {
  _resetClientCacheForTests();
  const original = process.env.CosmosDB_Endpoint;
  delete process.env.CosmosDB_Endpoint;
  try {
    assert.throws(() => getPricingContainer(), /CosmosDB_Endpoint app setting is required/);
  } finally {
    if (original !== undefined) process.env.CosmosDB_Endpoint = original;
    _resetClientCacheForTests();
  }
});

test('getPricingContainer: resolves database/container ids from env vars, with documented defaults', () => {
  _resetClientCacheForTests();
  const originalEndpoint = process.env.CosmosDB_Endpoint;
  const originalDb = process.env.CosmosDB_Database;
  const originalContainer = process.env.CosmosDB_PricingContainer;
  process.env.CosmosDB_Endpoint = 'https://fake-account.documents.azure.com:443/';
  delete process.env.CosmosDB_Database;
  delete process.env.CosmosDB_PricingContainer;
  try {
    // CosmosClient construction is lazy (no network call until a data
    // operation runs), so this resolves the database/container ids
    // without needing a live account — same "construction never throws
    // by itself" property this whole DI pattern already relies on.
    const container = getPricingContainer();
    assert.equal(container.database.id, 'ai-usage-db');
    assert.equal(container.id, 'model-pricing');
  } finally {
    if (originalEndpoint === undefined) delete process.env.CosmosDB_Endpoint;
    else process.env.CosmosDB_Endpoint = originalEndpoint;
    if (originalDb !== undefined) process.env.CosmosDB_Database = originalDb;
    if (originalContainer !== undefined) process.env.CosmosDB_PricingContainer = originalContainer;
    _resetClientCacheForTests();
  }
});
