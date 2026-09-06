import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { loadSourcePrices, pricesDiffer, SourcePriceEntry } from '../src/lib/priceSource';
import { PriceSnapshot } from '../src/lib/types';

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

test('pricesDiffer: identical entries — false', () => {
  const entry = sourceEntry();
  assert.equal(pricesDiffer(entry, snapshotFrom(entry)), false);
});

test('pricesDiffer: a changed rate field — true', () => {
  const entry = sourceEntry();
  const snapshot = snapshotFrom(entry, { CostPerInputUnit: 3 }); // was 2 in the source
  assert.equal(pricesDiffer(entry, snapshot), true);
});

test('pricesDiffer: a changed non-rate field (isActive) — still counted as a real change', () => {
  const entry = sourceEntry({ isActive: false });
  assert.equal(pricesDiffer(entry, snapshotFrom(sourceEntry({ isActive: true }))), true);
});

test('pricesDiffer: a field NOT in the tracked list (e.g. modelFamily) changing does NOT trigger a diff', () => {
  const entry = sourceEntry({ modelFamily: 'gpt-4.1-renamed' });
  const snapshot = snapshotFrom(sourceEntry({ modelFamily: 'gpt-4.1' }));
  // modelFamily is display-only, deliberately not in pricesDiffer's tracked
  // field list — a rename alone shouldn't version-bump the price catalog.
  assert.equal(pricesDiffer(entry, snapshot), false);
});

test('pricesDiffer: a changed CalculationMethod (tokens -> percentage) — true', () => {
  const entry = sourceEntry({ CalculationMethod: 'percentage', BaseCost: 5000 });
  const snapshot = snapshotFrom(sourceEntry());
  assert.equal(pricesDiffer(entry, snapshot), true);
});

test('loadSourcePrices: reads and parses the file at PriceSource_FilePath', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'pricing-service-test-'));
  const filePath = path.join(dir, 'prices.json');
  const entries = [sourceEntry(), sourceEntry({ deploymentName: 'gpt-4.1-mini', modelFamily: 'gpt-4.1-mini' })];
  await writeFile(filePath, JSON.stringify(entries), 'utf-8');

  const original = process.env.PriceSource_FilePath;
  process.env.PriceSource_FilePath = filePath;
  try {
    const loaded = await loadSourcePrices();
    assert.equal(loaded.length, 2);
    assert.equal(loaded[0]!.deploymentName, 'gpt-4.1');
    assert.equal(loaded[1]!.deploymentName, 'gpt-4.1-mini');
  } finally {
    if (original === undefined) delete process.env.PriceSource_FilePath;
    else process.env.PriceSource_FilePath = original;
    await rm(dir, { recursive: true, force: true });
  }
});

test('loadSourcePrices: a missing file rejects rather than silently returning an empty list', async () => {
  const original = process.env.PriceSource_FilePath;
  process.env.PriceSource_FilePath = path.join(tmpdir(), 'this-file-does-not-exist-12345.json');
  try {
    await assert.rejects(() => loadSourcePrices());
  } finally {
    if (original === undefined) delete process.env.PriceSource_FilePath;
    else process.env.PriceSource_FilePath = original;
  }
});
