import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decideQuotaRequest } from '../src/functions/decideQuotaRequest';
import { FakeCosmosContainer } from './helpers/fakeCosmosContainer';
import { makeFakeContext, makeFakeRequest } from './helpers/fakeHttpRequest';

type CorroborationResult = { ok: true; department?: string } | { ok: false; status: number; message: string };
// Typed with the same arity as the real corroborateIdentity (request,
// headerOid, context, deps, headerDepartment?, requiredRole?) so a spy
// that wants to inspect requiredRole (below) can be passed to deps()
// without a function-arity type error — a 0-arg stub like
// okCorroboration is still assignable to this (fewer params is fine).
type CorroborateFn = (
  request: unknown,
  headerOid: unknown,
  context: unknown,
  deps: unknown,
  headerDepartment?: unknown,
  requiredRole?: unknown
) => Promise<CorroborationResult>;
const okCorroboration: CorroborateFn = async () => ({ ok: true });
function okCorroborationWithDepartment(department: string | undefined): CorroborateFn {
  return async () => ({ ok: true, department });
}

function seedPendingRequest(container: FakeCosmosContainer, overrides: Record<string, unknown> = {}) {
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
      statusHistory: [{ status: 'Pending', at: '2026-08-30T00:00:00.000Z', by: 'oid-abc' }],
      createdAt: '2026-08-30T00:00:00.000Z',
      ...overrides,
    },
  ]);
}

function deps(requestsContainer: FakeCosmosContainer, overridesContainer: FakeCosmosContainer, corroborate = okCorroboration) {
  return {
    getRequestsContainer: () => requestsContainer as never,
    getOverridesContainer: () => overridesContainer as never,
    corroborateIdentity: corroborate,
  } as never;
}

test('decideQuotaRequest: missing x-verified-oid header — 401', async () => {
  const request = makeFakeRequest({ headers: {}, body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' } });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(request, context, deps(new FakeCosmosContainer(), new FakeCosmosContainer()));
  assert.equal(res.status, 401);
});

test('decideQuotaRequest: passes "Quota.Approve" as corroborateIdentity\'s requiredRole — security-review fix, closing the direct-call role-bypass', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer);
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  let capturedRequiredRole: unknown;
  const spyCorroborate = async (
    _request: unknown,
    _headerOid: unknown,
    _context: unknown,
    _deps: unknown,
    _headerDepartment: unknown,
    requiredRole: unknown
  ): Promise<{ ok: true; department?: string }> => {
    capturedRequiredRole = requiredRole;
    return { ok: true };
  };
  const res = await decideQuotaRequest(request, context, deps(requestsContainer, overridesContainer, spyCorroborate));
  assert.equal(res.status, undefined); // 200 default — corroboration passed
  assert.equal(capturedRequiredRole, 'Quota.Approve');
});

test('decideQuotaRequest: corroboration rejects for a missing app role — 403 propagates, request never decided', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer);
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'employee-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(
    request,
    context,
    deps(requestsContainer, overridesContainer, async () => ({
      ok: false,
      status: 403,
      message: 'This operation requires the "Quota.Approve" app role, which the caller\'s token does not carry.',
    }))
  );
  assert.equal(res.status, 403);
  assert.equal(requestsContainer.all()[0].status, 'Pending'); // never decided
  assert.equal(overridesContainer.all().length, 0);
});

test('decideQuotaRequest: corroboration failure — propagates', async () => {
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(
    request,
    context,
    deps(new FakeCosmosContainer(), new FakeCosmosContainer(), async () => ({ ok: false, status: 401, message: 'nope' }))
  );
  assert.equal(res.status, 401);
});

test('decideQuotaRequest: malformed JSON body — 400', async () => {
  const request = makeFakeRequest({ headers: { 'x-verified-oid': 'approver-oid' }, bodyThrows: true });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(request, context, deps(new FakeCosmosContainer(), new FakeCosmosContainer()));
  assert.equal(res.status, 400);
});

test('decideQuotaRequest: bad decision value — 400', async () => {
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Maybe' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(request, context, deps(new FakeCosmosContainer(), new FakeCosmosContainer()));
  assert.equal(res.status, 400);
});

test('decideQuotaRequest: request not found — 404', async () => {
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'no-such-request', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(request, context, deps(new FakeCosmosContainer(), new FakeCosmosContainer()));
  assert.equal(res.status, 404);
});

test('decideQuotaRequest: already-decided request — 409, never re-decided', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer, { status: 'Approved' });
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Denied' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(request, context, deps(requestsContainer, new FakeCosmosContainer()));
  assert.equal(res.status, 409);
});

test('decideQuotaRequest: deny — 200, statusHistory appended with the verified oid (not a body-supplied value), no override document written', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer);
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'real-approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Denied', note: 'Not justified' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(request, context, deps(requestsContainer, overridesContainer));
  assert.equal(res.status, undefined); // 200 default
  const stored = requestsContainer.all()[0];
  assert.equal(stored.status, 'Denied');
  const history = stored.statusHistory as Array<{ status: string; by: string; note?: string }>;
  assert.equal(history[history.length - 1].by, 'real-approver-oid');
  assert.equal(history[history.length - 1].note, 'Not justified');
  assert.equal(overridesContainer.all().length, 0);
});

test('decideQuotaRequest: approve — writes the override document with the requested quota, clamped tier default, correct expiry', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer, { durationDays: 14 });
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'real-approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(request, context, deps(requestsContainer, overridesContainer));
  assert.equal(res.status, undefined);
  const override = overridesContainer.all()[0];
  assert.equal(override.id, 'user-oid-abc');
  assert.equal(override.effectiveQuota, 250000);
  assert.equal(override.baselineQuota, 100000);
  assert.equal(override.tpmTier, 'standard'); // default when tpmTier omitted from body
  assert.equal(override.grantedBy, 'real-approver-oid');
  assert.notEqual(override.expiresAt, null); // 14-day duration -> not permanent
});

test('decideQuotaRequest: approve with an explicit tpmTier honors it', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer);
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'real-approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved', tpmTier: 'elevated' },
  });
  const context = makeFakeContext();
  await decideQuotaRequest(request, context, deps(requestsContainer, overridesContainer));
  assert.equal(overridesContainer.all()[0].tpmTier, 'elevated');
});

test('decideQuotaRequest: a permanent request (durationDays: null) approves to a null expiresAt', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer, { durationDays: null });
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'real-approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  await decideQuotaRequest(request, context, deps(requestsContainer, overridesContainer));
  assert.equal(overridesContainer.all()[0].expiresAt, null);
});

test('decideQuotaRequest: user-scoped request — any Quota.Approve holder decides it, department irrelevant (the accepted residual gap)', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer, { scopeType: 'user', scopeId: 'oid-abc' });
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(
    request,
    context,
    deps(requestsContainer, overridesContainer, okCorroborationWithDepartment('SomeUnrelatedDepartment'))
  );
  assert.equal(res.status, undefined); // 200 default — not blocked
});

test('decideQuotaRequest: team-scoped request — approver department matches scopeId, decision proceeds', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer, { scopeType: 'team', scopeId: 'Finance' });
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(
    request,
    context,
    deps(requestsContainer, overridesContainer, okCorroborationWithDepartment('Finance'))
  );
  assert.equal(res.status, undefined);
  assert.equal(overridesContainer.all()[0].scopeId, 'Finance');
});

test('decideQuotaRequest: team-scoped request — approver department does not match scopeId — 403, not decided', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer, { scopeType: 'team', scopeId: 'Finance' });
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(
    request,
    context,
    deps(requestsContainer, overridesContainer, okCorroborationWithDepartment('Engineering'))
  );
  assert.equal(res.status, 403);
  assert.equal(requestsContainer.all()[0].status, 'Pending'); // never decided
  assert.equal(overridesContainer.all().length, 0);
});

test('decideQuotaRequest: team-scoped request — approver has no resolvable department — 403, fails closed', async () => {
  const requestsContainer = new FakeCosmosContainer();
  seedPendingRequest(requestsContainer, { scopeType: 'team', scopeId: 'Finance' });
  const overridesContainer = new FakeCosmosContainer();
  const request = makeFakeRequest({
    headers: { 'x-verified-oid': 'approver-oid' },
    body: { requestId: 'req-1', subscriptionId: 'sub-1', decision: 'Approved' },
  });
  const context = makeFakeContext();
  const res = await decideQuotaRequest(
    request,
    context,
    deps(requestsContainer, overridesContainer, okCorroborationWithDepartment(undefined))
  );
  assert.equal(res.status, 403);
  assert.equal(requestsContainer.all()[0].status, 'Pending');
});
