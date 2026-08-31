import { app, InvocationContext, Timer } from '@azure/functions';
import { loadAllSnapshots, upsertSnapshot, upsertCurrentPointer } from '../lib/cosmos';
import { loadSourcePrices, pricesDiffer } from '../lib/priceSource';
import { writeCurrentPricingCache } from '../lib/priceCacheBlob';
import { PriceSnapshot, CurrentPricePointer } from '../lib/types';

/**
 * Daily (24h) price refresh:
 *   1. Diff the source price list against the latest snapshot per
 *      deployment.
 *   2. Write a new *versioned* snapshot only for deployments whose price
 *      actually changed (append-only — never edits an existing snapshot).
 *   3. Close out the previous snapshot's effectiveTo so point-in-time
 *      lookups in enrichPricing.ts resolve unambiguously.
 *   4. Update the current::{deploymentName} pointer.
 *   5. Refresh the cached current-pricing.json blob the price page reads.
 *
 * Default schedule: 03:00 daily (NCRONTAB: second minute hour day month
 * day-of-week). Change via the PricingRefresh_Schedule app setting if
 * you'd rather run it inline with one of the existing
 * llm-usage-ingestion runs (see guides/cost-attribution-guide.md).
 *
 * Keyed on `deploymentName` throughout — the real pricing join key (see
 * resolveEffectivePrice() in cosmos.ts) — not the display-only
 * `modelFamily` field.
 */
export async function refreshPricingCache(_timer: Timer, context: InvocationContext): Promise<void> {
  const now = new Date().toISOString();

  const [sourcePrices, existingSnapshots] = await Promise.all([
    loadSourcePrices(),
    loadAllSnapshots(),
  ]);

  const latestByDeployment = new Map<string, PriceSnapshot>();
  for (const snapshot of existingSnapshots) {
    const current = latestByDeployment.get(snapshot.deploymentName);
    if (!current || snapshot.priceVersion > current.priceVersion) {
      latestByDeployment.set(snapshot.deploymentName, snapshot);
    }
  }

  let changedCount = 0;
  const currentPointers: PriceSnapshot[] = [];

  for (const source of sourcePrices) {
    const latest = latestByDeployment.get(source.deploymentName);

    if (latest && !pricesDiffer(source, latest)) {
      // No change today — keep the existing snapshot as current.
      currentPointers.push(latest);
      continue;
    }

    const nextVersion = (latest?.priceVersion ?? 0) + 1;
    const newSnapshot: PriceSnapshot = {
      ...source,
      id: `${source.deploymentName}-v${nextVersion}`,
      priceVersion: nextVersion,
      effectiveFrom: now,
      effectiveTo: null,
      docType: 'priceSnapshot',
    };

    if (latest) {
      // Close out the previous snapshot so it stops matching
      // resolveEffectivePrice() from `now` onward.
      await upsertSnapshot({ ...latest, effectiveTo: now });
    }
    await upsertSnapshot(newSnapshot);

    const pointer: CurrentPricePointer = {
      ...newSnapshot,
      id: `current::${source.deploymentName}`,
      docType: 'currentPricePointer',
      snapshotId: newSnapshot.id,
    };
    await upsertCurrentPointer(pointer);

    currentPointers.push(newSnapshot);
    changedCount += 1;
  }

  await writeCurrentPricingCache(currentPointers);

  context.log(
    `refreshPricingCache: ${sourcePrices.length} models checked, ${changedCount} price change(s) written, cache refreshed.`
  );
}

app.timer('refreshPricingCache', {
  schedule: process.env.PricingRefresh_Schedule ?? '0 0 3 * * *',
  handler: refreshPricingCache,
});
