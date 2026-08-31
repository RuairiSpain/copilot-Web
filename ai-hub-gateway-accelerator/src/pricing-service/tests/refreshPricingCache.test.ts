import { test } from 'node:test';
import assert from 'node:assert/strict';
import { refreshPricingCache } from '../src/functions/refreshPricingCache';
import { makeFakeContext, makeFakeTimer } from './helpers/fakeHttpRequest';
import { PriceSnapshot } from '../src/lib/types';
import { SourcePriceEntry } from '../src/lib/priceSource';

function sourceEntry(overrides: Partial<SourcePriceEntry> = {}): SourcePriceEntry {
  return {
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
    ...overrides,
  };
}

function snapshotFrom(entry: SourcePriceEntry, overrides: Partial<PriceSnapshot> = {}): PriceSnapshot {
  return {
    ...entry,
    id: `${entry.deploymentName}-v1`,
    priceVersion: 1,
    effectiveFrom: '2026-08-01T00:00:00.000Z',
    effectiveTo: null,
    docType: 'priceSnapshot',
    ...overrides,
  };
}

function deps(overrides: Partial<{
  loadSourcePrices: () => Promise<SourcePriceEntry[]>;
  loadAllSnapshots: () => Promise<PriceSnapshot[]>;
  upsertSnapshot: (s: PriceSnapshot) => Promise<void>;
  upsertCurrentPointer: (p: unknown) => Promise<void>;
  writeCurrentPricingCache: (p: PriceSnapshot[]) => Promise<void>;
}> = {}) {
  return {
    loadSourcePrices: async () => [],
    loadAllSnapshots: async () => [],
    upsertSnapshot: async () => undefined,
    upsertCurrentPointer: async () => undefined,
    writeCurrentPricingCache: async () => undefined,
    ...overrides,
  } as never;
}

test('refreshPricingCache: no existing snapshots — every source entry gets a new v1 snapshot', async () => {
  const context = makeFakeContext();
  const upserted: PriceSnapshot[] = [];
  await refreshPricingCache(
    makeFakeTimer(),
    context,
    deps({
      loadSourcePrices: async () => [sourceEntry()],
      upsertSnapshot: async (s) => {
        upserted.push(s);
      },
    })
  );
  assert.equal(upserted.length, 1);
  assert.equal(upserted[0]!.priceVersion, 1);
  assert.equal(upserted[0]!.id, 'gpt-4.1-v1');
  assert.equal(upserted[0]!.effectiveTo, null);
});

test('refreshPricingCache: unchanged price — no new snapshot written, existing one carried forward as current', async () => {
  const entry = sourceEntry();
  const existing = snapshotFrom(entry);
  const context = makeFakeContext();
  let upsertCalls = 0;
  let cachedPrices: PriceSnapshot[] = [];
  await refreshPricingCache(
    makeFakeTimer(),
    context,
    deps({
      loadSourcePrices: async () => [entry],
      loadAllSnapshots: async () => [existing],
      upsertSnapshot: async () => {
        upsertCalls += 1;
      },
      writeCurrentPricingCache: async (p) => {
        cachedPrices = p;
      },
    })
  );
  assert.equal(upsertCalls, 0); // no change, nothing written
  assert.equal(cachedPrices.length, 1);
  assert.equal(cachedPrices[0]!.id, existing.id); // the existing snapshot, unchanged
});

test('refreshPricingCache: changed price — writes a new versioned snapshot AND closes out the old one\'s effectiveTo', async () => {
  const oldEntry = sourceEntry({ CostPerInputUnit: 2 });
  const existing = snapshotFrom(oldEntry, { priceVersion: 1, id: 'gpt-4.1-v1' });
  const newEntry = sourceEntry({ CostPerInputUnit: 3 }); // price changed
  const context = makeFakeContext();
  const upserted: PriceSnapshot[] = [];
  await refreshPricingCache(
    makeFakeTimer(),
    context,
    deps({
      loadSourcePrices: async () => [newEntry],
      loadAllSnapshots: async () => [existing],
      upsertSnapshot: async (s) => {
        upserted.push(s);
      },
    })
  );
  // Two writes: the closed-out old snapshot, then the new v2 snapshot.
  assert.equal(upserted.length, 2);
  const closedOut = upserted.find((s) => s.id === 'gpt-4.1-v1');
  const newSnapshot = upserted.find((s) => s.id === 'gpt-4.1-v2');
  assert.notEqual(closedOut, undefined);
  assert.notEqual(closedOut!.effectiveTo, null); // closed out, not still current
  assert.notEqual(newSnapshot, undefined);
  assert.equal(newSnapshot!.priceVersion, 2);
  assert.equal(newSnapshot!.effectiveTo, null); // the new one is current
  assert.equal(newSnapshot!.CostPerInputUnit, 3);
});

test('refreshPricingCache: changed price — also writes the current::{deploymentName} pointer', async () => {
  const entry = sourceEntry();
  const context = makeFakeContext();
  let pointer: { id: string; snapshotId: string } | undefined;
  await refreshPricingCache(
    makeFakeTimer(),
    context,
    deps({
      loadSourcePrices: async () => [entry],
      upsertCurrentPointer: async (p) => {
        pointer = p as { id: string; snapshotId: string };
      },
    })
  );
  assert.equal(pointer?.id, 'current::gpt-4.1');
  assert.equal(pointer?.snapshotId, 'gpt-4.1-v1');
});

test('refreshPricingCache: multiple deployments — picks the latest priceVersion per deployment when diffing, not an arbitrary one', async () => {
  const entry = sourceEntry({ CostPerInputUnit: 2 });
  const v1 = snapshotFrom(sourceEntry({ CostPerInputUnit: 1 }), { id: 'gpt-4.1-v1', priceVersion: 1 });
  const v2 = snapshotFrom(sourceEntry({ CostPerInputUnit: 2 }), { id: 'gpt-4.1-v2', priceVersion: 2 }); // matches current source — no change
  const context = makeFakeContext();
  let upsertCalls = 0;
  await refreshPricingCache(
    makeFakeTimer(),
    context,
    deps({
      loadSourcePrices: async () => [entry],
      // Deliberately out of order — the function must pick the highest
      // priceVersion, not just the last one in the array.
      loadAllSnapshots: async () => [v2, v1],
      upsertSnapshot: async () => {
        upsertCalls += 1;
      },
    })
  );
  assert.equal(upsertCalls, 0); // correctly recognized v2 (matching) as latest, not v1 (stale)
});

test('refreshPricingCache: writes the current-pricing cache blob exactly once per run, containing every deployment\'s current snapshot', async () => {
  const context = makeFakeContext();
  let cachedPrices: PriceSnapshot[] = [];
  let writeCalls = 0;
  await refreshPricingCache(
    makeFakeTimer(),
    context,
    deps({
      loadSourcePrices: async () => [sourceEntry({ deploymentName: 'gpt-4.1' }), sourceEntry({ deploymentName: 'gpt-4.1-mini', modelFamily: 'gpt-4.1-mini' })],
      writeCurrentPricingCache: async (p) => {
        writeCalls += 1;
        cachedPrices = p;
      },
    })
  );
  assert.equal(writeCalls, 1);
  assert.equal(cachedPrices.length, 2);
});

test('refreshPricingCache: logs a summary with the models-checked and changes-written counts', async () => {
  const context = makeFakeContext();
  await refreshPricingCache(makeFakeTimer(), context, deps({ loadSourcePrices: async () => [sourceEntry()] }));
  assert.equal(context.logs.length, 1);
  assert.match(String(context.logs[0]![0]), /1 models checked, 1 price change\(s\) written/);
});
