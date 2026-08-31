import { readFile } from 'node:fs/promises';
import { PriceSnapshot } from './types';

/** The shape of a source-of-truth price entry, before versioning fields are
 *  attached — same as one entry in
 *  src/usage-reports/model-pricing-generated-extended.json. */
export type SourcePriceEntry = Omit<
  PriceSnapshot,
  'id' | 'priceVersion' | 'effectiveFrom' | 'effectiveTo' | 'docType'
>;

/**
 * Reads today's authoritative price list.
 *
 * Default implementation reads a checked-in JSON file — fine for a demo /
 * customer-managed rate card that changes by editing a file in source
 * control and redeploying. Replace this with a call to your real pricing
 * feed (Azure Retail Prices API, an internal FinOps service, whatever you
 * actually bill against) — everything downstream (diffing, versioning,
 * cache write) is agnostic to where the numbers come from.
 */
export async function loadSourcePrices(): Promise<SourcePriceEntry[]> {
  const filePath = process.env.PriceSource_FilePath ?? './priceSource.json';
  const raw = await readFile(filePath, 'utf-8');
  return JSON.parse(raw) as SourcePriceEntry[];
}

/** True if any priced field differs between the source entry and the
 *  latest known snapshot for that model — used to decide whether a new
 *  versioned snapshot needs writing, or the model is unchanged today. */
export function pricesDiffer(a: SourcePriceEntry, b: PriceSnapshot): boolean {
  const fields: (keyof SourcePriceEntry)[] = [
    'CostPerInputUnit',
    'CostPerOutputUnit',
    'CostPerCachedInputUnit',
    'CostPerAudioInputUnit',
    'CostPerCachedAudioInputUnit',
    'CostPerAudioOutputUnit',
    'CostPerReasoningOutputUnit',
    'CostPerImageInputUnit',
    'CostPerCachedImageInputUnit',
    'CostUnit',
    'BaseCost',
    'Currency',
    'CalculationMethod',
    'region',
    'isActive',
  ];
  return fields.some((f) => a[f] !== b[f]);
}
