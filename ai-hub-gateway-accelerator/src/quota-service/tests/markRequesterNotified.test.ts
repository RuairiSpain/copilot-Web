import { test } from 'node:test';
import assert from 'node:assert/strict';
import { markRequesterNotified } from '../src/functions/markRequesterNotified';
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
      status: 'Approved',
      requiresEscalation: false,
      statusHistory: [],
      createdAt: '2026-08-30T00:00:00.000Z',
      requestedByEmail: 'user@example.com',
      ...overrides,
    },
  ] as never);
}

test('markRequesterNotified: malformed JSON body — 400', async () => {
  const request = makeFakeRequest({ bodyThrows: true });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await markRequesterNotified(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 400);
});

test('markRequesterNotified: missing requestId/subscriptionId — 400', async () => {
  const request = makeFakeRequest({ body: { subscriptionId: 'sub-1' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await markRequesterNotified(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 400);
});

test('markRequesterNotified: request not found — 404', async () => {
  const request = makeFakeRequest({ body: { requestId: 'no-such-request', subscriptionId: 'sub-1' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await markRequesterNotified(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 404);
});

test('markRequesterNotified: happy path — sets requesterNotifiedAt and persists it', async () => {
  const request = makeFakeRequest({ body: { requestId: 'req-1', subscriptionId: 'sub-1' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  seedRequest(container);
  const res = await markRequesterNotified(request, context, { getContainer: () => container as never });
  const body = res.jsonBody as { requesterNotifiedAt?: string };
  assert.ok(body.requesterNotifiedAt);
  assert.equal(container.all()[0].requesterNotifiedAt, body.requesterNotifiedAt);
});

test('markRequesterNotified: already notified — idempotent no-op, does not overwrite requesterNotifiedAt', async () => {
  const request = makeFakeRequest({ body: { requestId: 'req-1', subscriptionId: 'sub-1' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  seedRequest(container, { requesterNotifiedAt: '2026-08-30T02:00:00.000Z' });
  const res = await markRequesterNotified(request, context, { getContainer: () => container as never });
  const body = res.jsonBody as { requesterNotifiedAt?: string };
  assert.equal(body.requesterNotifiedAt, '2026-08-30T02:00:00.000Z');
});
