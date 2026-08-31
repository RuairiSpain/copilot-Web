import { app, InvocationContext, Timer } from '@azure/functions';
import { getQuotaOverridesContainer } from '../lib/cosmos';
import { survivesMonthlyReset } from '../lib/quotaLogic';
import { QuotaOverride } from '../lib/types';

export interface ResetMonthlyQuotaOverridesDeps {
  getContainer: () => ReturnType<typeof getQuotaOverridesContainer>;
}

const defaultDeps: ResetMonthlyQuotaOverridesDeps = {
  getContainer: getQuotaOverridesContainer,
};

/**
 * The counterpart to expireQuotaOverrides.ts's daily per-document sweep
 * (see that file — nothing here duplicates it). This is the "quota is
 * back to zero for everyone at the start of the month" job: it doesn't
 * wait for each override's own expiresAt, it clears every TEMPORARY
 * override at the calendar-month boundary regardless of how much of its
 * individual duration is left.
 *
 * Does NOT touch usage counters — those are tier-1/tier-2
 * `llm-token-limit`'s own token-quota-period="Monthly", which APIM
 * resets internally with no code involved on either side. This function
 * only ever deletes `quota-overrides` documents (the elevated GRANT, not
 * the usage counter it's checked against) — see
 * guides/quota-override-approval.md's "Implementation status" for why
 * these are two different things easy to conflate.
 *
 * Permanent overrides (expiresAt: null) survive by default — see
 * survivesMonthlyReset()'s own doc comment in quotaLogic.ts for the
 * reasoning. Set QuotaOverride_MonthlyResetIncludesPermanent=true to
 * clear those too, if a genuine "everyone starts the month at baseline,
 * no exceptions" policy is what you actually want.
 *
 * Schedule: 1st of the month, 03:10 UTC — a few minutes after
 * expireQuotaOverrides's daily 03:05 run and pricing-service's daily
 * 03:00 refresh, so the three jobs don't contend for the same Cosmos RU
 * budget in the same instant. Not a real concern at these document
 * volumes, same nicety-not-necessity framing as expireQuotaOverrides's
 * own comment on this.
 */
export async function resetMonthlyQuotaOverrides(
  timer: Timer,
  context: InvocationContext,
  deps: ResetMonthlyQuotaOverridesDeps = defaultDeps
): Promise<void> {
  const includePermanent = (process.env.QuotaOverride_MonthlyResetIncludesPermanent ?? 'false').toLowerCase() === 'true';

  const container = deps.getContainer();
  const { resources: allOverrides } = await container.items
    .query<Pick<QuotaOverride, 'id' | 'subscriptionId' | 'expiresAt'>>({
      query: 'SELECT c.id, c.subscriptionId, c.expiresAt FROM c',
    })
    .fetchAll();

  const toDelete = allOverrides.filter((o) => !survivesMonthlyReset(o.expiresAt, includePermanent));

  let deleted = 0;
  for (const doc of toDelete) {
    try {
      await container.item(doc.id, doc.subscriptionId).delete();
      deleted += 1;
    } catch (err) {
      // Same reasoning as expireQuotaOverrides.ts: one bad delete
      // shouldn't stop the whole monthly reset from running.
      context.warn(`resetMonthlyQuotaOverrides: failed to delete ${doc.id}`, err);
    }
  }

  context.log(
    `resetMonthlyQuotaOverrides: ${allOverrides.length} override(s) found, ${toDelete.length} targeted for reset (includePermanent=${includePermanent}), ${deleted} deleted.`
  );
}

app.timer('resetMonthlyQuotaOverrides', {
  schedule: '0 10 3 1 * *',
  handler: resetMonthlyQuotaOverrides,
});
