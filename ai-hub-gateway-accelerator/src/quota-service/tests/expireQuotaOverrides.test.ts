import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Timer } from '@azure/functions';
import { expireQuotaOverrides } from '../src/functions/expireQuotaOverrides';
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

test('expireQuotaOverrides: deletes overrides whose expiresAt is in the past, keeps future and permanent ones', async () => {
  const container = new FakeCosmosContainer();
  container.seed([
    overrideDoc('past', { expiresAt: '2026-01-01T00:00:00.000Z' }),
    overrideDoc('future', { expiresAt: '2099-01-01T00:00:00.000Z' }),
    overrideDoc('permanent', { expiresAt: null }),
  ] as never);
  const context = makeFakeContext();
  await expireQuotaOverrides(fakeTimer, context, { getContainer: () => container as never });
  const remainingIds = container.all().map((d) => d.id).sort();
  assert.deepEqual(remainingIds, ['future', 'permanent']);
});

test('expireQuotaOverrides: nothing expired — no deletions', async () => {
  const container = new FakeCosmosContainer();
  container.seed([overrideDoc('future', { expiresAt: '2099-01-01T00:00:00.000Z' })] as never);
  const context = makeFakeContext();
  await expireQuotaOverrides(fakeTimer, context, { getContainer: () => container as never });
  assert.equal(container.all().length, 1);
});

test('expireQuotaOverrides: a failing delete does not stop the rest of the sweep', async () => {
  const container = new FakeCosmosContainer();
  container.seed([
    overrideDoc('past-1', { expiresAt: '2026-01-01T00:00:00.000Z' }),
    overrideDoc('past-2', { expiresAt: '2026-01-01T00:00:00.000Z' }),
  ] as never);
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
  await expireQuotaOverrides(fakeTimer, context, { getContainer: () => throwingContainer as never });
  assert.equal(context.warns.length, 1);
  // The second document (whichever the fake iterates to second) was deleted despite the first failing.
  assert.equal(container.all().length, 1);
});
