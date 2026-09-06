import { test } from 'node:test';
import assert from 'node:assert/strict';
import { listRecentlyDecidedQuotaRequests } from '../src/functions/listRecentlyDecidedQuotaRequests';
import { FakeCosmosContainer } from './helpers/fakeCosmosContainer';
import { makeFakeContext, makeFakeRequest } from './helpers/fakeHttpRequest';

function seed(container: FakeCosmosContainer, docs: Record<string, unknown>[]) {
  container.seed(docs as never);
}

function baseDoc(overrides: Record<string, unknown> = {}) {
  return {
    id: 'req-1',
    subscriptionId: 'sub-1',
    docType: 'quotaOverrideRequest',
    scopeType: 'user',
    scopeId: 'oid-abc',
    requestedBy: 'oid-abc',
    currentQuota: 100000,
    requestedQuota: 250000,
    reason: 'reason',
    durationDays: 30,
    status: 'Approved',
    requiresEscalation: false,
    statusHistory: [],
    createdAt: '2026-08-30T00:00:00.000Z',
    requestedByEmail: 'user@example.com',
    ...overrides,
  };
}

test('listRecentlyDecidedQuotaRequests: no decided requests — empty array', async () => {
  const container = new FakeCosmosContainer();
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const res = await listRecentlyDecidedQuotaRequests(request, context, { getContainer: () => container as never });
  assert.deepEqual(res.jsonBody, []);
});

test('listRecentlyDecidedQuotaRequests: includes both Approved and Denied, excludes Pending', async () => {
  const container = new FakeCosmosContainer();
  seed(container, [
    baseDoc({ id: 'req-approved', status: 'Approved' }),
    baseDoc({ id: 'req-denied', status: 'Denied' }),
    baseDoc({ id: 'req-pending', status: 'Pending' }),
  ]);
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const res = await listRecentlyDecidedQuotaRequests(request, context, { getContainer: () => container as never });
  const body = res.jsonBody as Array<{ id: string }>;
  assert.deepEqual(
    body.map((d) => d.id).sort(),
    ['req-approved', 'req-denied']
  );
});

test('listRecentlyDecidedQuotaRequests: excludes requests with no requestedByEmail (nothing to notify)', async () => {
  const container = new FakeCosmosContainer();
  seed(container, [baseDoc({ id: 'req-no-email', requestedByEmail: undefined })]);
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const res = await listRecentlyDecidedQuotaRequests(request, context, { getContainer: () => container as never });
  assert.deepEqual(res.jsonBody, []);
});

test('listRecentlyDecidedQuotaRequests: excludes requests already marked requesterNotifiedAt', async () => {
  const container = new FakeCosmosContainer();
  seed(container, [baseDoc({ id: 'req-already-notified', requesterNotifiedAt: '2026-08-30T02:00:00.000Z' })]);
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const res = await listRecentlyDecidedQuotaRequests(request, context, { getContainer: () => container as never });
  assert.deepEqual(res.jsonBody, []);
});

test('listRecentlyDecidedQuotaRequests: sorts by createdAt ascending', async () => {
  const container = new FakeCosmosContainer();
  seed(container, [
    baseDoc({ id: 'req-new', createdAt: '2026-08-20T00:00:00.000Z' }),
    baseDoc({ id: 'req-old', createdAt: '2026-08-01T00:00:00.000Z' }),
  ]);
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const res = await listRecentlyDecidedQuotaRequests(request, context, { getContainer: () => container as never });
  const body = res.jsonBody as Array<{ id: string }>;
  assert.deepEqual(
    body.map((d) => d.id),
    ['req-old', 'req-new']
  );
});

test('listRecentlyDecidedQuotaRequests: Cosmos query failure — 502, consistent error shape (this session\'s own code-quality review)', async () => {
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const brokenContainer = { items: { query: () => ({ fetchAll: async () => { throw new Error('Cosmos unavailable'); } }) } } as never;
  const res = await listRecentlyDecidedQuotaRequests(request, context, { getContainer: () => brokenContainer });
  assert.equal(res.status, 502);
  assert.match((res.jsonBody as { error: string }).error, /temporary data-access error/);
});
