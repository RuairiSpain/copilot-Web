import { test } from 'node:test';
import assert from 'node:assert/strict';
import { submitQuotaRequest } from '../src/functions/submitQuotaRequest';
import { FakeCosmosContainer } from './helpers/fakeCosmosContainer';
import { makeFakeContext, makeFakeRequest } from './helpers/fakeHttpRequest';

const okCorroboration = async () => ({ ok: true as const });

function validBody(overrides: Record<string, unknown> = {}) {
  return {
    scopeType: 'user',
    scopeId: 'oid-abc',
    subscriptionId: 'sub-1',
    currentQuota: 100000,
    requestedQuota: 250000,
    reason: 'Quarter-end reconciliation',
    ...overrides,
  };
}

function deps(container: FakeCosmosContainer, overrides: Record<string, unknown> = {}) {
  return {
    getContainer: () => container as never,
    corroborateIdentity: okCorroboration,
    newRequestId: () => 'req-fixed-id',
    ...overrides,
  } as never;
}

test('submitQuotaRequest: missing x-verified-oid header — 401, corroboration never even attempted', async () => {
  const request = makeFakeRequest({ headers: {}, body: validBody() });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  let corroborationCalled = false;
  const res = await submitQuotaRequest(
    request,
    context,
    deps(container, { corroborateIdentity: async () => ((corroborationCalled = true), { ok: true }) })
  );
  assert.equal(res.status, 401);
  assert.equal(corroborationCalled, false);
});

test('submitQuotaRequest: corroboration failure — propagates its status/message', async () => {
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'oid-abc' }, body: validBody() });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await submitQuotaRequest(
    request,
    context,
    deps(container, { corroborateIdentity: async () => ({ ok: false, status: 401, message: 'mismatch' }) })
  );
  assert.equal(res.status, 401);
  assert.equal((res.jsonBody as { error: string }).error, 'mismatch');
});

test('submitQuotaRequest: malformed JSON body — 400', async () => {
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'oid-abc' }, bodyThrows: true });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await submitQuotaRequest(request, context, deps(container));
  assert.equal(res.status, 400);
});

test('submitQuotaRequest: requestedQuota <= currentQuota — 400', async () => {
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'oid-abc' }, body: validBody({ requestedQuota: 50000 }) });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await submitQuotaRequest(request, context, deps(container));
  assert.equal(res.status, 400);
});

test('submitQuotaRequest: blank reason — 400', async () => {
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'oid-abc' }, body: validBody({ reason: '   ' }) });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await submitQuotaRequest(request, context, deps(container));
  assert.equal(res.status, 400);
});

test('submitQuotaRequest: an existing Pending request for the same scope — 409', async () => {
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'oid-abc' }, body: validBody() });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  container.seed([
    {
      id: 'req-existing',
      subscriptionId: 'sub-1',
      docType: 'quotaOverrideRequest',
      scopeType: 'user',
      scopeId: 'oid-abc',
      requestedBy: 'oid-abc',
      currentQuota: 100000,
      requestedQuota: 150000,
      reason: 'earlier request',
      durationDays: 30,
      status: 'Pending',
      requiresEscalation: false,
      statusHistory: [],
      createdAt: new Date().toISOString(),
    },
  ]);
  const res = await submitQuotaRequest(request, context, deps(container));
  assert.equal(res.status, 409);
  assert.equal((res.jsonBody as { existingRequestId: string }).existingRequestId, 'req-existing');
});

test('submitQuotaRequest: happy path — 201, requestedBy from the verified header (never the body), correct document written', async () => {
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'oid-abc', 'x-verified-email': 'user@example.com' },
    body: validBody(),
  });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await submitQuotaRequest(request, context, deps(container));
  assert.equal(res.status, 201);
  const body = res.jsonBody as Record<string, unknown>;
  assert.equal(body.id, 'req-fixed-id');
  assert.equal(body.requestedBy, 'oid-abc');
  assert.equal(body.requestedByEmail, 'user@example.com');
  assert.equal(body.status, 'Pending');
  assert.equal(container.all().length, 1);
});

test('submitQuotaRequest: x-verified-email absent — requestedByEmail is omitted entirely (not stored as undefined/null)', async () => {
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'oid-abc' }, body: validBody() });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await submitQuotaRequest(request, context, deps(container));
  const body = res.jsonBody as Record<string, unknown>;
  assert.equal('requestedByEmail' in body, false);
});

test('submitQuotaRequest: requiresEscalation is computed from the configured multiplier, defaulting to 3x', async () => {
  const originalMultiplier = process.env.QuotaOverride_EscalationMultiplier;
  delete process.env.QuotaOverride_EscalationMultiplier;
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'oid-abc' },
    body: validBody({ currentQuota: 100000, requestedQuota: 400000 }), // 4x > default 3x
  });
  const context = makeFakeContext();
  const container = new FakeCosmosContainer();
  const res = await submitQuotaRequest(request, context, deps(container));
  assert.equal((res.jsonBody as { requiresEscalation: boolean }).requiresEscalation, true);
  if (originalMultiplier !== undefined) process.env.QuotaOverride_EscalationMultiplier = originalMultiplier;
});
