import { test } from 'node:test';
import assert from 'node:assert/strict';
import { getContainerClient, writeCurrentPricingCache } from '../src/lib/priceCacheBlob';
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
    priceVersion: 3,
    effectiveFrom: '2026-08-01T00:00:00.000Z',
    effectiveTo: null,
    docType: 'priceSnapshot',
    ...overrides,
  };
}

/** Fake ContainerClient — scoped exactly to the two calls
 *  writeCurrentPricingCache actually makes. */
function fakeContainerClient(capture: { payload?: string; blobName?: string }) {
  return {
    createIfNotExists: async () => undefined,
    getBlockBlobClient: (blobName: string) => {
      capture.blobName = blobName;
      return {
        upload: async (payload: string) => {
          capture.payload = payload;
        },
      };
    },
  } as never;
}

test('writeCurrentPricingCache: uploads a JSON payload with generatedAt and one entry per price', async () => {
  const capture: { payload?: string; blobName?: string } = {};
  await writeCurrentPricingCache([snapshot()], { getContainerClient: () => fakeContainerClient(capture) });
  const body = JSON.parse(capture.payload!);
  assert.equal(typeof body.generatedAt, 'string');
  assert.equal(body.prices.length, 1);
});

test('writeCurrentPricingCache: maps every field the customer-facing price page reads, from the real PriceSnapshot field names', async () => {
  const capture: { payload?: string } = {};
  await writeCurrentPricingCache([snapshot()], { getContainerClient: () => fakeContainerClient(capture) });
  const price = JSON.parse(capture.payload!).prices[0];
  assert.equal(price.modelFamily, 'gpt-4.1');
  assert.equal(price.deploymentName, 'gpt-4.1');
  assert.equal(price.currency, 'USD');
  assert.equal(price.calculationMethod, 'tokens');
  assert.equal(price.costPerInputUnit, 2);
  assert.equal(price.costPerOutputUnit, 8);
  assert.equal(price.costPerCachedInputUnit, 0.5);
  assert.equal(price.costUnit, 1_000_000);
  assert.equal(price.priceVersion, 3);
  assert.equal(price.effectiveFrom, '2026-08-01T00:00:00.000Z');
});

test('writeCurrentPricingCache: still writes the deprecated `model` alias, equal to modelFamily, for a transition-period customer page', async () => {
  const capture: { payload?: string } = {};
  await writeCurrentPricingCache([snapshot({ modelFamily: 'gpt-5.2' })], { getContainerClient: () => fakeContainerClient(capture) });
  const price = JSON.parse(capture.payload!).prices[0];
  assert.equal(price.model, 'gpt-5.2');
  assert.equal(price.model, price.modelFamily);
});

test('writeCurrentPricingCache: multiple prices — one entry per snapshot, in the given order', async () => {
  const capture: { payload?: string } = {};
  await writeCurrentPricingCache(
    [snapshot({ deploymentName: 'gpt-4.1' }), snapshot({ deploymentName: 'gpt-4.1-mini', modelFamily: 'gpt-4.1-mini' })],
    { getContainerClient: () => fakeContainerClient(capture) }
  );
  const prices = JSON.parse(capture.payload!).prices;
  assert.equal(prices.length, 2);
  assert.equal(prices[0].deploymentName, 'gpt-4.1');
  assert.equal(prices[1].deploymentName, 'gpt-4.1-mini');
});

test('writeCurrentPricingCache: uploads to the configured blob name, defaulting to current-pricing.json', async () => {
  const capture: { payload?: string; blobName?: string } = {};
  const original = process.env.PricingCache_BlobName;
  delete process.env.PricingCache_BlobName;
  try {
    await writeCurrentPricingCache([snapshot()], { getContainerClient: () => fakeContainerClient(capture) });
    assert.equal(capture.blobName, 'current-pricing.json');
  } finally {
    if (original !== undefined) process.env.PricingCache_BlobName = original;
  }
});

test('writeCurrentPricingCache: empty price list — still uploads a valid payload with an empty prices array', async () => {
  const capture: { payload?: string } = {};
  await writeCurrentPricingCache([], { getContainerClient: () => fakeContainerClient(capture) });
  const body = JSON.parse(capture.payload!);
  assert.deepEqual(body.prices, []);
});

test('getContainerClient: missing PricingCache_StorageAccountUrl app setting throws a clear error, not a crash deep in the SDK', () => {
  const original = process.env.PricingCache_StorageAccountUrl;
  delete process.env.PricingCache_StorageAccountUrl;
  try {
    assert.throws(() => getContainerClient(), /PricingCache_StorageAccountUrl app setting is required/);
  } finally {
    if (original !== undefined) process.env.PricingCache_StorageAccountUrl = original;
  }
});

test('getContainerClient: resolves the container name from env, with a documented default', () => {
  const originalUrl = process.env.PricingCache_StorageAccountUrl;
  const originalContainer = process.env.PricingCache_ContainerName;
  process.env.PricingCache_StorageAccountUrl = 'https://fakeaccount.blob.core.windows.net';
  delete process.env.PricingCache_ContainerName;
  try {
    // BlobServiceClient construction is lazy (no network call until an
    // actual operation runs), so this resolves the container name
    // without needing a live storage account.
    const container = getContainerClient();
    assert.equal(container.containerName, 'pricing-cache');
  } finally {
    if (originalUrl === undefined) delete process.env.PricingCache_StorageAccountUrl;
    else process.env.PricingCache_StorageAccountUrl = originalUrl;
    if (originalContainer !== undefined) process.env.PricingCache_ContainerName = originalContainer;
  }
});
