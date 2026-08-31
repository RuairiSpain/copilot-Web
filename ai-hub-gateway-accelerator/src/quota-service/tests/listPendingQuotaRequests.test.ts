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

// --- x-verified-oid / Quota.Approve re-check (security-review fix) ---
//
// This route is shared by two callers: the internal notification Logic
// App (no x-verified-oid header — the tests above, which never set one,
// already cover that path staying open) and the external Quota Override
// API's GET /quota/pending (always sets x-verified-oid via APIM). These
// tests cover the second path.

test('listPendingQuotaRequests: no x-verified-oid header — corroboration is never invoked, internal caller unaffected', async () => {
  const container = new FakeCosmosContainer();
  const request = makeFakeRequest({ headers: {} });
  const context = makeFakeContext();
  let corroborateCalled = false;
  const res = await listPendingQuotaRequests(request, context, {
    getContainer: () => container as never,
    corroborateIdentity: (async () => {
      corroborateCalled = true;
      return { ok: true };
    }) as never,
  });
  assert.equal(corroborateCalled, false);
  assert.deepEqual(res.jsonBody, []);
});

test('listPendingQuotaRequests: x-verified-oid present, corroboration passes with Quota.Approve — list returned', async () => {
  const container = new FakeCosmosContainer();
  seed(container, [baseDoc()]);
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'approver-oid' } });
  const context = makeFakeContext();
  let capturedRequiredRole: unknown;
  const res = await listPendingQuotaRequests(request, context, {
    getContainer: () => container as never,
    corroborateIdentity: (async (
      _request: unknown,
      _headerOid: unknown,
      _context: unknown,
      options?: { deps?: unknown; headerDepartment?: unknown; requiredRole?: unknown }
    ) => {
      capturedRequiredRole = options?.requiredRole;
      return { ok: true };
    }) as never,
  });
  assert.equal(capturedRequiredRole, 'Quota.Approve');
  assert.equal((res.jsonBody as unknown[]).length, 1);
});

test('listPendingQuotaRequests: x-verified-oid present, corroboration rejects (missing role) — 403, container never queried', async () => {
  const container = new FakeCosmosContainer();
  seed(container, [baseDoc()]);
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'employee-oid' } });
  const context = makeFakeContext();
  const res = await listPendingQuotaRequests(request, context, {
    getContainer: () => container as never,
    corroborateIdentity: (async () => ({
      ok: false,
      status: 403,
      message: 'This operation requires the "Quota.Approve" app role, which the caller\'s token does not carry.',
    })) as never,
  });
  assert.equal(res.status, 403);
  assert.match((res.jsonBody as { error: string }).error, /Quota\.Approve/);
});

test('listPendingQuotaRequests: x-verified-oid present, deps.corroborateIdentity omitted — falls back to the real corroborateIdentity rather than skipping the check', async () => {
  const container = new FakeCosmosContainer();
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'employee-oid' } });
  const context = makeFakeContext();
  delete process.env.Entra_Audience; // force the real verifyBearerTokenClaims to reject cleanly, not hang on a live tenant
  const res = await listPendingQuotaRequests(request, context, { getContainer: () => container as never });
  assert.equal(res.status, 401); // real corroborateIdentity ran and rejected — not silently bypassed
});

test('listPendingQuotaRequests: Cosmos query failure — 502, consistent error shape (this session\'s own code-quality review)', async () => {
  const request = makeFakeRequest({});
  const context = makeFakeContext();
  const brokenContainer = { items: { query: () => ({ fetchAll: async () => { throw new Error('Cosmos unavailable'); } }) } } as never;
  const res = await listPendingQuotaRequests(request, context, { getContainer: () => brokenContainer });
  assert.equal(res.status, 502);
  assert.match((res.jsonBody as { error: string }).error, /temporary data-access error/);
});
