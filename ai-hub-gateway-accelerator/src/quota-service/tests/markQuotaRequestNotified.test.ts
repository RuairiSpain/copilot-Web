import { test } from 'node:test';
import assert from 'node:assert/strict';
import { markQuotaRequestNotified } from '../src/functions/markQuotaRequestNotified';
import { FakeCosmosContainer } from './helpers/fakeCosmosContainer';
import { makeFakeContext, makeFakeRequest } from './helpers/fakeHttpRequest';

function seedRequest(container: FakeCosmosContainer, overrides: Record<string, unknown> = {}) {
  container.seed([
    {
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
    },
  ] as never);
}

test('markQuotaRequestNotified: malformed JSON body — 400', async () => {
  const request = makeFakeRequest({ bodyThrows: true });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await markQuotaRequestNotified(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 400);
});

test('markQuotaRequestNotified: missing requestId/subscriptionId — 400', async () => {
  const request = makeFakeRequest({ body: { requestId: 'req-1' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await markQuotaRequestNotified(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 400);
});

test('markQuotaRequestNotified: request not found — 404 (not a raw exception)', async () => {
  const request = makeFakeRequest({ body: { requestId: 'no-such-request', subscriptionId: 'sub-1' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await markQuotaRequestNotified(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 404);
});

test('markQuotaRequestNotified: happy path — sets notifiedAt and persists it', async () => {
  const request = makeFakeRequest({ body: { requestId: 'req-1', subscriptionId: 'sub-1' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  seedRequest(container);
  const res = await markQuotaRequestNotified(request, context, { getContainer: () => container as never });
  const body = res.jsonBody as { notifiedAt?: string };
  assert.ok(body.notifiedAt);
  assert.equal(container.all()[0].notifiedAt, body.notifiedAt);
});

test('markQuotaRequestNotified: already notified — idempotent no-op, does not overwrite notifiedAt', async () => {
  const request = makeFakeRequest({ body: { requestId: 'req-1', subscriptionId: 'sub-1' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  seedRequest(container, { notifiedAt: '2026-08-30T01:00:00.000Z' });
  const res = await markQuotaRequestNotified(request, context, { getContainer: () => container as never });
  const body = res.jsonBody as { notifiedAt?: string };
  assert.equal(body.notifiedAt, '2026-08-30T01:00:00.000Z');
});
