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
    verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: undefined }),
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
    verifyBearerTokenClaims: async () => ({ oid: 'real-token-oid', department: undefined }),
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
        return { oid: 'oid-abc', department: undefined };
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
    { verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: 'Finance' }) },
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
    { verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: 'Engineering' }) },
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
    { verifyBearerTokenClaims: async () => ({ oid: 'oid-abc', department: undefined }) },
    'Finance'
  );
  assert.equal(result.ok, true);
  if (result.ok) {
    assert.equal(result.department, undefined);
  }
  restoreEnv();
});
