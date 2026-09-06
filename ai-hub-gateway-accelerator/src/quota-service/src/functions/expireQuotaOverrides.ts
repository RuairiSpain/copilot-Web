import { app, InvocationContext, Timer } from '@azure/functions';
import { getQuotaOverridesContainer } from '../lib/cosmos';
import { QuotaOverride } from '../lib/types';

export interface ExpireQuotaOverridesDeps {
  getContainer: () => ReturnType<typeof getQuotaOverridesContainer>;
}

const defaultDeps: ExpireQuotaOverridesDeps = {
  getContainer: getQuotaOverridesContainer,
};

/**
 * §7 — daily sweep. Deletes any quota-overrides document past its
 * expiresAt. There is deliberately no separate "revert to baseline"
 * step: resolveAllowance() in quotaLogic.ts already treats a missing
 * override the same as an expired one, so deleting the document IS the
 * revert — the next cache-miss lookup (within one APIM cache TTL window)
 * just returns the contract baseline again.
 *
 * Same "early hours" default window as pricing-service's
 * refreshPricingCache (0 0 3 * * *) — deliberately offset a few minutes
 * so the two don't contend for the same Cosmos RU budget at the exact
 * same second, though at these document volumes that's a nicety, not a
 * real concern.
 */
export async function expireQuotaOverrides(
  timer: Timer,
  context: InvocationContext,
  deps: ExpireQuotaOverridesDeps = defaultDeps
): Promise<void> {
  const container = deps.getContainer();
  const nowIso = new Date().toISOString();

  const { resources: expired } = await container.items
    .query<Pick<QuotaOverride, 'id' | 'subscriptionId'>>({
      query: 'SELECT c.id, c.subscriptionId FROM c WHERE c.expiresAt != null AND c.expiresAt <= @now',
      parameters: [{ name: '@now', value: nowIso }],
    })
    .fetchAll();

  let deleted = 0;
  for (const doc of expired) {
    try {
      await container.item(doc.id, doc.subscriptionId).delete();
      deleted += 1;
    } catch (err) {
      // Don't let one bad delete stop the sweep — log and keep going,
      // same "an ingestion outage is worse than one skipped record"
      // reasoning pricing-service's enrichPricing already applies.
      context.warn(`expireQuotaOverrides: failed to delete ${doc.id}`, err);
    }
  }

  context.log(`expireQuotaOverrides: swept ${expired.length} expired override(s), deleted ${deleted}`);
}

app.timer('expireQuotaOverrides', {
  schedule: '0 5 3 * * *',
  handler: expireQuotaOverrides,
});
