// Real jose crypto against a real local HTTP server — not a mocked-out
// shortcut. jose's Node runtime uses node:http/node:https directly (not
// globalThis.fetch), confirmed by reading its own source before writing
// this file, so a global-fetch mock would silently NOT intercept the
// JWKS fetch inside createRemoteJWKSet — a real local server is the
// actual, verified-working way to test this module's real verification
// logic end to end.
import { test, before, after, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import type { AddressInfo } from 'node:net';
import { generateKeyPair, SignJWT, exportJWK, KeyLike } from 'jose';
import { _resetCacheForTests, TokenValidationError, verifyBearerTokenClaims } from '../src/lib/tokenValidation';

const ISSUER = 'https://login.microsoftonline.com/test-tenant/v2.0';
const AUDIENCE = 'api://test-gateway';

let server: http.Server;
let baseUrl: string;
let privateKey: KeyLike;
let otherPrivateKey: KeyLike; // a key NOT in the served JWKS, for the tampered-signature test

before(async () => {
  const { publicKey, privateKey: pk } = await generateKeyPair('RS256');
  privateKey = pk;
  const other = await generateKeyPair('RS256');
  otherPrivateKey = other.privateKey;

  const jwk = await exportJWK(publicKey);
  jwk.kid = 'test-key-1';
  jwk.alg = 'RS256';
  jwk.use = 'sig';

  server = http.createServer((req, res) => {
    res.setHeader('Content-Type', 'application/json');
    if (req.url === '/discovery') {
      res.end(JSON.stringify({ issuer: ISSUER, jwks_uri: `${baseUrl}/jwks` }));
    } else if (req.url === '/jwks') {
      res.end(JSON.stringify({ keys: [jwk] }));
    } else if (req.url === '/discovery-broken') {
      res.end(JSON.stringify({ notAnIssuer: true })); // missing issuer/jwks_uri
    } else if (req.url === '/discovery-404') {
      res.statusCode = 404;
      res.end('{}');
    } else {
      res.statusCode = 404;
      res.end('{}');
    }
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  baseUrl = `http://127.0.0.1:${port}`;
});

after(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

beforeEach(() => {
  _resetCacheForTests();
  process.env.Entra_Audience = AUDIENCE;
  process.env.Entra_OpenIdConfigUrl = `${baseUrl}/discovery`;
});

async function signToken(
  overrides: {
    audience?: string;
    issuer?: string;
    expiresIn?: string;
    key?: KeyLike;
    oid?: string | null;
    department?: string;
    roles?: unknown;
  } = {}
): Promise<string> {
  const claims: Record<string, unknown> = {};
  if (overrides.oid !== null) {
    claims.oid = overrides.oid ?? 'abc-oid-123';
  }
  if (overrides.department !== undefined) {
    claims.department = overrides.department;
  }
  if (overrides.roles !== undefined) {
    claims.roles = overrides.roles;
  }
  const builder = new SignJWT(claims)
    .setProtectedHeader({ alg: 'RS256', kid: 'test-key-1' })
    .setIssuer(overrides.issuer ?? ISSUER)
    .setAudience(overrides.audience ?? AUDIENCE)
    .setExpirationTime(overrides.expiresIn ?? '1h');
  return builder.sign(overrides.key ?? privateKey);
}

test('verifyBearerTokenClaims: a genuinely valid token is accepted and its oid extracted', async () => {
  const token = await signToken({ oid: 'user-oid-42' });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.equal(claims.oid, 'user-oid-42');
});

test('verifyBearerTokenClaims: a token with a department claim returns it', async () => {
  const token = await signToken({ oid: 'user-oid-42', department: 'Finance' });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.equal(claims.department, 'Finance');
});

test('verifyBearerTokenClaims: a token with no department claim returns undefined, not a throw', async () => {
  const token = await signToken({ oid: 'user-oid-42' });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.equal(claims.department, undefined);
});

test('verifyBearerTokenClaims: an empty-string department claim is treated as absent (undefined)', async () => {
  const token = await signToken({ oid: 'user-oid-42', department: '' });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.equal(claims.department, undefined);
});

test('verifyBearerTokenClaims: missing Authorization header is rejected', async () => {
  await assert.rejects(() => verifyBearerTokenClaims(null), TokenValidationError);
  await assert.rejects(() => verifyBearerTokenClaims(undefined), TokenValidationError);
});

test('verifyBearerTokenClaims: malformed header (no "Bearer " prefix) is rejected', async () => {
  await assert.rejects(() => verifyBearerTokenClaims('NotBearer abc'), TokenValidationError);
});

test('verifyBearerTokenClaims: empty bearer token is rejected', async () => {
  await assert.rejects(() => verifyBearerTokenClaims('Bearer '), TokenValidationError);
  await assert.rejects(() => verifyBearerTokenClaims('Bearer    '), TokenValidationError);
});

test('verifyBearerTokenClaims: wrong audience is rejected — the actual anti-impersonation-relevant check', async () => {
  const token = await signToken({ audience: 'api://some-other-app' });
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), TokenValidationError);
});

test('verifyBearerTokenClaims: wrong issuer is rejected', async () => {
  const token = await signToken({ issuer: 'https://login.microsoftonline.com/wrong-tenant/v2.0' });
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), TokenValidationError);
});

test('verifyBearerTokenClaims: expired token is rejected', async () => {
  const token = await signToken({ expiresIn: '-1h' });
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), TokenValidationError);
});

test('verifyBearerTokenClaims: token signed by a key NOT in the served JWKS is rejected (tampered/wrong-key signature)', async () => {
  const token = await signToken({ key: otherPrivateKey });
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), TokenValidationError);
});

test('verifyBearerTokenClaims: a syntactically corrupted token is rejected', async () => {
  const token = await signToken();
  const corrupted = token.slice(0, -4) + 'xxxx';
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${corrupted}`), TokenValidationError);
});

test('verifyBearerTokenClaims: token with no oid claim is rejected', async () => {
  const token = await signToken({ oid: null });
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), TokenValidationError);
});

test('verifyBearerTokenClaims: missing Entra_Audience app setting is rejected', async () => {
  delete process.env.Entra_Audience;
  const token = await signToken();
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), TokenValidationError);
});

test('verifyBearerTokenClaims: missing Entra_OpenIdConfigUrl app setting is rejected', async () => {
  delete process.env.Entra_OpenIdConfigUrl;
  const token = await signToken();
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), Error);
});

test('verifyBearerTokenClaims: discovery document fetch failure (404) is rejected', async () => {
  process.env.Entra_OpenIdConfigUrl = `${baseUrl}/discovery-404`;
  const token = await signToken();
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), /HTTP 404/);
});

test('verifyBearerTokenClaims: discovery document missing issuer/jwks_uri is rejected', async () => {
  process.env.Entra_OpenIdConfigUrl = `${baseUrl}/discovery-broken`;
  const token = await signToken();
  await assert.rejects(() => verifyBearerTokenClaims(`Bearer ${token}`), /missing issuer/);
});

test('verifyBearerTokenClaims: discovery document is cached — a second call with a valid token still succeeds without re-fetching (implicit: no error, same as first call)', async () => {
  const token1 = await signToken({ oid: 'first' });
  const token2 = await signToken({ oid: 'second' });
  assert.equal((await verifyBearerTokenClaims(`Bearer ${token1}`)).oid, 'first');
  assert.equal((await verifyBearerTokenClaims(`Bearer ${token2}`)).oid, 'second');
});

// --- roles claim (security-review fix: role re-verification) ---

test('verifyBearerTokenClaims: a token with a roles array returns it verbatim', async () => {
  const token = await signToken({ oid: 'user-oid-42', roles: ['Quota.Approve', 'Models.Read'] });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.deepEqual(claims.roles, ['Quota.Approve', 'Models.Read']);
});

test('verifyBearerTokenClaims: a token with a single-element roles array returns it', async () => {
  const token = await signToken({ oid: 'user-oid-42', roles: ['Quota.Approve'] });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.deepEqual(claims.roles, ['Quota.Approve']);
});

test('verifyBearerTokenClaims: a token with no roles claim returns an empty array, not undefined or a throw', async () => {
  const token = await signToken({ oid: 'user-oid-42' });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.deepEqual(claims.roles, []);
});

test('verifyBearerTokenClaims: a roles claim that is not an array (defensive — should never happen from real Entra ID) is treated as no roles, not a throw', async () => {
  const token = await signToken({ oid: 'user-oid-42', roles: 'Quota.Approve' });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.deepEqual(claims.roles, []);
});

test('verifyBearerTokenClaims: non-string entries in a roles array are filtered out rather than propagated', async () => {
  const token = await signToken({ oid: 'user-oid-42', roles: ['Quota.Approve', 42, null] });
  const claims = await verifyBearerTokenClaims(`Bearer ${token}`);
  assert.deepEqual(claims.roles, ['Quota.Approve']);
});
