import { test } from 'node:test';
import assert from 'node:assert/strict';
import { getQuotaAllowance } from '../src/functions/getQuotaAllowance';
import { FakeCosmosContainer } from './helpers/fakeCosmosContainer';
import { makeFakeContext, makeFakeRequest } from './helpers/fakeHttpRequest';

function validBody(overrides: Record<string, unknown> = {}) {
  return {
    scopeType: 'user',
    scopeId: 'oid-abc',
    subscriptionId: 'sub-1',
    baselineQuota: 100000,
    ...overrides,
  };
}

test('getQuotaAllowance: malformed JSON body — 400', async () => {
  const request = makeFakeRequest({ bodyThrows: true });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await getQuotaAllowance(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 400);
});

test('getQuotaAllowance: invalid body shape — 400', async () => {
  const request = makeFakeRequest({ body: { scopeType: 'not-user-or-team' } });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await getQuotaAllowance(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 400);
});

test('getQuotaAllowance: negative baselineQuota — 400', async () => {
  const request = makeFakeRequest({ body: validBody({ baselineQuota: -1 }) });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await getQuotaAllowance(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 400);
});

test('getQuotaAllowance: no override document exists — falls back to baseline, tier standard', async () => {
  const request = makeFakeRequest({ body: validBody() });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await getQuotaAllowance(request, context, { getContainer: () => container as never });
  assert.equal(res.status, undefined); // 200 default (no explicit status)
  assert.deepEqual(res.jsonBody, { scopeType: 'none', effectiveQuota: 100000, tpmTier: 'standard' });
});

test('getQuotaAllowance: an active override under baseline is honored', async () => {
  const request = makeFakeRequest({ body: validBody() });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  container.seed([
    {
      id: 'user-oid-abc',
      subscriptionId: 'sub-1',
      docType: 'quotaOverride',
      scopeType: 'user',
      scopeId: 'oid-abc',
      baselineQuota: 100000,
      effectiveQuota: 60000,
      tpmTier: 'elevated',
      grantedBy: 'approver',
      requestId: 'req-1',
      expiresAt: null,
      updatedAt: new Date().toISOString(),
    },
  ]);
  const res = await getQuotaAllowance(request, context, { getContainer: () => container as never });
  assert.deepEqual(res.jsonBody, { scopeType: 'user', effectiveQuota: 60000, tpmTier: 'elevated' });
});

test('getQuotaAllowance: a Cosmos error other than 404 fails open — 502, not a crash', async () => {
  const request = makeFakeRequest({ body: validBody() });
  const context = makeFakeContext();
  const throwingContainer = {
    item: () => ({
      read: async () => {
        const err = new Error('throttled') as Error & { code: number };
        err.code = 429;
        throw err;
      },
    }),
  };
  const res = await getQuotaAllowance(request, context, { getContainer: () => throwingContainer as never });
  assert.equal(res.status, 502);
  assert.equal(context.errors.length, 1);
});
