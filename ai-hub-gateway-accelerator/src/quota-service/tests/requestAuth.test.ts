import { test } from 'node:test';
import assert from 'node:assert/strict';
import { corroborateIdentity } from '../src/lib/requestAuth';
import { TokenValidationError } from '../src/lib/tokenValidation';
import { makeFakeContext, makeFakeRequest } from './helpers/fakeHttpRequest';

const originalRequireRevalidation = process.env.QuotaOverride_RequireTokenRevalidation;

function restoreEnv() {
  if (originalRequireRevalidation === undefined) {
    delete process.env.QuotaOverride_RequireTokenRevalidation;
  } else {
    process.env.QuotaOverride_RequireTokenRevalidation = originalRequireRevalidation;
  }
}

test('corroborateIdentity: matching oid, no department claim — ok, department undefined', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(request, 'oid-abc', context, {
    verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: undefined, roles: [] }),
  });
  assert.equal(result.ok, true);
  if (result.ok) {
    assert.equal(result.department, undefined);
  }
  restoreEnv();
});

test('corroborateIdentity: mismatched oid — 401, the actual anti-impersonation check', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(request, 'attacker-claimed-oid', context, {
    verifyBearerTokenClaims: async () => ({ oid: 'real-token-oid', department: undefined, roles: [] }),
  });
  assert.equal(result.ok, false);
  if (!result.ok) {
    assert.equal(result.status, 401);
  }
  restoreEnv();
});

test('corroborateIdentity: verification throws — 401 with the TokenValidationError message', async () => {
  const request = makeFakeRequest({ headers: {} });
  const context = makeFakeContext();
  const result = await corroborateIdentity(request, 'oid-abc', context, {
    verifyBearerTokenClaims: async () => {
      throw new TokenValidationError('Missing or malformed Authorization header — expected "Bearer <token>"');
    },
  });
  assert.equal(result.ok, false);
  if (!result.ok) {
    assert.equal(result.status, 401);
    assert.match(result.message, /Missing or malformed Authorization header/);
  }
  restoreEnv();
});

test('corroborateIdentity: a non-TokenValidationError throw still yields a safe generic 401 message, not the raw error', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(request, 'oid-abc', context, {
    verifyBearerTokenClaims: async () => {
      throw new Error('some internal network error with potentially sensitive detail');
    },
  });
  assert.equal(result.ok, false);
  if (!result.ok) {
    assert.equal(result.message, 'Token re-validation failed');
  }
  restoreEnv();
});

test('corroborateIdentity: QuotaOverride_RequireTokenRevalidation=false skips verification entirely, trusts the header department, and logs a warning', async () => {
  process.env.QuotaOverride_RequireTokenRevalidation = 'false';
  const request = makeFakeRequest({ headers: {} });
  const context = makeFakeContext();
  let verifyCalled = false;
  const result = await corroborateIdentity(
    request,
    'oid-abc',
    context,
    {
      verifyBearerTokenClaims: async () => {
        verifyCalled = true;
        return { oid: 'oid-abc', department: undefined, roles: [] };
      },
    },
    'Finance'
  );
  assert.equal(result.ok, true);
  if (result.ok) {
    assert.equal(result.department, 'Finance');
  }
  assert.equal(verifyCalled, false, 'verifyBearerTokenClaims must not be called when re-validation is disabled');
  assert.equal(context.warns.length, 1);
  assert.match(String(context.warns[0][0]), /reopens the impersonation gap/);
  restoreEnv();
});

test('corroborateIdentity: default (deps omitted, real verifyBearerTokenClaims) rejects cleanly when misconfigured, not a crash', async () => {
  delete process.env.Entra_Audience;
  const request = makeFakeRequest({ headers: { authorization: 'Bearer not-a-real-token' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(request, 'oid-abc', context);
  assert.equal(result.ok, false);
  restoreEnv();
});

test('corroborateIdentity: matching header/token department — ok, returns the verified department', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(
    request,
    'oid-abc',
    context,
    { verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: 'Finance', roles: [] }) },
    'Finance'
  );
  assert.equal(result.ok, true);
  if (result.ok) {
    assert.equal(result.department, 'Finance');
  }
  restoreEnv();
});

test('corroborateIdentity: header department contradicts the independently-verified token department — 401, rejected', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(
    request,
    'oid-abc',
    context,
    { verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: 'Engineering', roles: [] }) },
    'Finance'
  );
  assert.equal(result.ok, false);
  if (!result.ok) {
    assert.equal(result.status, 401);
    assert.match(result.message, /department/);
  }
  restoreEnv();
});

test('corroborateIdentity: header department present but token has none — trusts the verified (undefined) value, not rejected', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(
    request,
    'oid-abc',
    context,
    { verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: undefined, roles: [] }) },
    'Finance'
  );
  assert.equal(result.ok, true);
  if (result.ok) {
    assert.equal(result.department, undefined);
  }
  restoreEnv();
});

// --- requiredRole (security-review fix: role re-verification, closing
// the direct-call bypass where corroborateIdentity proved "a real
// person" but never "the RIGHT kind of real person") ---

test('corroborateIdentity: no requiredRole given — unaffected regardless of the token\'s roles (backward compatible, e.g. submitQuotaRequest.ts)', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(request, 'oid-abc', context, {
    verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: undefined, roles: [] }),
  });
  assert.equal(result.ok, true);
  restoreEnv();
});

test('corroborateIdentity: requiredRole set, token carries it — ok', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(
    request,
    'approver-oid',
    context,
    { verifyBearerTokenClaims: async () => ({ oid: 'approver-oid', department: undefined, roles: ['Quota.Approve', 'Models.Read'] }) },
    undefined,
    'Quota.Approve'
  );
  assert.equal(result.ok, true);
  restoreEnv();
});

test('corroborateIdentity: requiredRole set, token has other roles but not it — 403, the actual bypass this closes', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(
    request,
    'employee-oid',
    context,
    { verifyBearerTokenClaims: async () => ({ oid: 'employee-oid', department: undefined, roles: ['Models.Read'] }) },
    undefined,
    'Quota.Approve'
  );
  assert.equal(result.ok, false);
  if (!result.ok) {
    assert.equal(result.status, 403);
    assert.match(result.message, /Quota\.Approve/);
  }
  restoreEnv();
});

test('corroborateIdentity: requiredRole set, token has zero roles — 403, fails closed', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(
    request,
    'employee-oid',
    context,
    { verifyBearerTokenClaims: async () => ({ oid: 'employee-oid', department: undefined, roles: [] }) },
    undefined,
    'Quota.Approve'
  );
  assert.equal(result.ok, false);
  if (!result.ok) {
    assert.equal(result.status, 403);
  }
  restoreEnv();
});

test('corroborateIdentity: the oid check still runs first — a spoofed oid is rejected 401 even with the right role, role check never reached', async () => {
  const request = makeFakeRequest({ headers: { authorization: 'Bearer whatever' } });
  const context = makeFakeContext();
  const result = await corroborateIdentity(
    request,
    'attacker-claimed-oid',
    context,
    { verifyBearerTokenClaims: async () => ({ oid: 'real-token-oid', department: undefined, roles: ['Quota.Approve'] }) },
    undefined,
    'Quota.Approve'
  );
  assert.equal(result.ok, false);
  if (!result.ok) {
    assert.equal(result.status, 401); // identity mismatch, not the 403 a role failure would give
  }
  restoreEnv();
});

test('corroborateIdentity: QuotaOverride_RequireTokenRevalidation=false also skips the role check (documented, not silently narrowed) and logs that it did', async () => {
  process.env.QuotaOverride_RequireTokenRevalidation = 'false';
  const request = makeFakeRequest({ headers: {} });
  const context = makeFakeContext();
  const result = await corroborateIdentity(
    request,
    'oid-abc',
    context,
    { verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: undefined, roles: [] }) },
    undefined,
    'Quota.Approve'
  );
  assert.equal(result.ok, true);
  assert.match(String(context.warns[0][0]), /skips the "Quota\.Approve" role check/);
  restoreEnv();
});
