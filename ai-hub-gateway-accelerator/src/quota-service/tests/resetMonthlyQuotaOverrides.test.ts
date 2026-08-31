import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Timer } from '@azure/functions';
import { resetMonthlyQuotaOverrides } from '../src/functions/resetMonthlyQuotaOverrides';
import { FakeCosmosContainer } from './helpers/fakeCosmosContainer';
import { makeFakeContext } from './helpers/fakeHttpRequest';

const fakeTimer = {} as Timer;

function overrideDoc(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    subscriptionId: 'sub-1',
    docType: 'quotaOverride',
    scopeType: 'user',
    scopeId: id,
    baselineQuota: 100000,
    effectiveQuota: 250000,
    tpmTier: 'standard',
    grantedBy: 'approver',
    requestId: `req-${id}`,
    expiresAt: null as string | null,
    updatedAt: '2026-08-01T00:00:00.000Z',
    ...overrides,
  };
}

test('resetMonthlyQuotaOverrides: deletes every temporary override regardless of remaining duration, keeps permanent ones by default', async () => {
  const original = process.env.QuotaOverride_MonthlyResetIncludesPermanent;
  delete process.env.QuotaOverride_MonthlyResetIncludesPermanent;
  const container = new FakeCosmosContainer();
  container.seed([
    overrideDoc('temp-almost-expired', { expiresAt: '2026-08-31T23:59:00.000Z' }),
    overrideDoc('temp-far-future', { expiresAt: '2099-01-01T00:00:00.000Z' }),
    overrideDoc('permanent', { expiresAt: null }),
  ] as never);
  const context = makeFakeContext();
  await resetMonthlyQuotaOverrides(fakeTimer, context, { getContainer: () => container as never });
  assert.deepEqual(container.all().map((d) => d.id), ['permanent']);
  if (original !== undefined) process.env.QuotaOverride_MonthlyResetIncludesPermanent = original;
});

test('resetMonthlyQuotaOverrides: QuotaOverride_MonthlyResetIncludesPermanent=true clears permanent overrides too', async () => {
  const original = process.env.QuotaOverride_MonthlyResetIncludesPermanent;
  process.env.QuotaOverride_MonthlyResetIncludesPermanent = 'true';
  const container = new FakeCosmosContainer();
  container.seed([overrideDoc('permanent', { expiresAt: null })] as never);
  const context = makeFakeContext();
  await resetMonthlyQuotaOverrides(fakeTimer, context, { getContainer: () => container as never });
  assert.equal(container.all().length, 0);
  if (original !== undefined) process.env.QuotaOverride_MonthlyResetIncludesPermanent = original;
  else delete process.env.QuotaOverride_MonthlyResetIncludesPermanent;
});

test('resetMonthlyQuotaOverrides: no overrides at all — no-op, no errors', async () => {
  const container = new FakeCosmosContainer();
  const context = makeFakeContext();
  await resetMonthlyQuotaOverrides(fakeTimer, context, { getContainer: () => container as never });
  assert.equal(container.all().length, 0);
  assert.equal(context.warns.length, 0);
});

test('resetMonthlyQuotaOverrides: a failing delete does not stop the rest of the sweep', async () => {
  const container = new FakeCosmosContainer();
  container.seed([overrideDoc('temp-1', { expiresAt: '2099-01-01T00:00:00.000Z' }), overrideDoc('temp-2', { expiresAt: '2099-01-01T00:00:00.000Z' })] as never);
  const realItem = container.item.bind(container);
  let calls = 0;
  const throwingContainer = {
    items: container.items,
    item: (id: string, pk: string) => {
      calls += 1;
      if (calls === 1) {
        return { delete: async () => { throw new Error('transient failure'); } };
      }
      return realItem(id, pk);
    },
  };
  const context = makeFakeContext();
  await resetMonthlyQuotaOverrides(fakeTimer, context, { getContainer: () => throwingContainer as never });
  assert.equal(context.warns.length, 1);
  assert.equal(container.all().length, 1);
});
