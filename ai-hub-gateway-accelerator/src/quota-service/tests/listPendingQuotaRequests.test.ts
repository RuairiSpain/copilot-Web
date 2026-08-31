import { test } from 'node:test';
import assert from 'node:assert/strict';
import { listPendingQuotaRequests } from '../src/functions/listPendingQuotaRequests';
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
    status: 'Pending',
    requiresEscalation: false,
    statusHistory: [],
    createdAt: '2026-08-30T00:00:00.000Z',
    ...overrides,
  };
}

test('listPendingQuotaRequests: no pending requests — empty array', async () => {
  const container = new FakeCosmosContainer();
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const res = await listPendingQuotaRequests(request, context, { getContainer: () => container as never });
  assert.deepEqual(res.jsonBody, []);
});

test('listPendingQuotaRequests: excludes non-Pending and already-notified requests', async () => {
  const container = new FakeCosmosContainer();
  seed(container, [
    baseDoc({ id: 'req-approved', status: 'Approved' }),
    baseDoc({ id: 'req-notified', notifiedAt: '2026-08-30T01:00:00.000Z' }),
    baseDoc({ id: 'req-pending-unnotified' }),
  ]);
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const res = await listPendingQuotaRequests(request, context, { getContainer: () => container as never });
  const body = res.jsonBody as Array<{ id: string }>;
  assert.equal(body.length, 1);
  assert.equal(body[0].id, 'req-pending-unnotified');
});

test('listPendingQuotaRequests: sorts escalation-required requests first, then by createdAt ascending', async () => {
  const container = new FakeCosmosContainer();
  seed(container, [
    baseDoc({ id: 'req-old', createdAt: '2026-08-01T00:00:00.000Z' }),
    baseDoc({ id: 'req-escalated', requiresEscalation: true, createdAt: '2026-08-15T00:00:00.000Z' }),
    baseDoc({ id: 'req-new', createdAt: '2026-08-20T00:00:00.000Z' }),
  ]);
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const res = await listPendingQuotaRequests(request, context, { getContainer: () => container as never });
  const body = res.jsonBody as Array<{ id: string }>;
  assert.deepEqual(
    body.map((d) => d.id),
    ['req-escalated', 'req-old', 'req-new']
  );
});
