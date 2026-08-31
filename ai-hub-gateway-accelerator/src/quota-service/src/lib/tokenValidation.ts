import { createRemoteJWKSet, jwtVerify } from 'jose';

/**
 * Defense-in-depth against the impersonation gap identified in
 * guides/quota-override-approval.md's "Impersonation" section: today,
 * quota-api-policy.xml validates the caller's JWT and stamps a verified
 * `x-verified-oid` header before forwarding to quota-service — but that
 * header is a PLAIN HTTP HEADER once it leaves APIM. Anyone who has (or
 * leaks) quota-service's function key can call submitQuotaRequest/
 * decideQuotaRequest DIRECTLY, bypassing APIM entirely, and simply set
 * x-verified-oid to any oid they like. The function key proves nothing
 * about identity; it was only ever meant to prove "this caller is APIM,"
 * and it can't actually enforce that.
 *
 * This module closes that hole WITHOUT quota-service minting or signing
 * anything of its own (self-issued tokens would be a worse idea — see
 * the doc section above for why: it means owning key management,
 * rotation, and revocation that Entra ID already does correctly). It
 * instead independently re-validates the SAME real Entra ID access
 * token the original caller presented (which APIM already forwards to
 * the backend unless something explicitly strips it — nothing here
 * does), and cross-checks that token's own `oid` claim against the
 * `x-verified-oid` header APIM set. A caller with only the function key
 * and a spoofed header, but no genuinely valid, correctly-scoped Entra
 * token whose oid matches, gets rejected — spoofing the header alone is
 * no longer sufficient.
 *
 * FAIL-CLOSED BY DESIGN — the opposite of getQuotaAllowance's fail-open
 * posture, deliberately. getQuotaAllowance fails open because losing
 * tier-2 enforcement during an outage still leaves tier-1 governing
 * traffic (degraded, not unsafe). There is no equivalent safety net
 * here: if this can't verify who's calling, the only safe thing to do
 * is refuse the call, not let a plausible-looking header through.
 *
 * VERIFICATION STATUS: the actual JWKS fetch + signature/issuer/
 * audience/expiry verification path IS exercised in this session — see
 * tests/tokenValidation.test.ts, which runs a real local HTTP server
 * serving a genuinely generated keypair's discovery document and JWKS,
 * and real jose-signed tokens through this exact code (not a mock of
 * jose's verification logic). What's still unverified: a real Microsoft
 * Entra ID tenant's actual discovery document/JWKS shape and any
 * tenant-specific quirks — the tests confirm this code's OWN logic is
 * correct against a spec-compliant OIDC provider, not that Entra ID
 * itself behaves exactly like the test's local stand-in. Verify against
 * a real tenant before trusting this in production.
 */

interface OidcDiscoveryDocument {
  issuer: string;
  jwks_uri: string;
}

let cachedDiscovery: OidcDiscoveryDocument | undefined;
let cachedJwks: ReturnType<typeof createRemoteJWKSet> | undefined;

async function getDiscoveryDocument(): Promise<OidcDiscoveryDocument> {
  if (!cachedDiscovery) {
    const configUrl = process.env.Entra_OpenIdConfigUrl;
    if (!configUrl) {
      throw new Error('Entra_OpenIdConfigUrl app setting is required for token re-validation');
    }
    // Fetched once per cold start and cached — not doing OIDC discovery
    // on every single request. Deliberately NOT hardcoding Microsoft's
    // jwks_uri URL convention (login.microsoftonline.com/.../discovery/v2.0/keys)
    // — reading it from the real discovery document is the technically
    // correct way to do this and doesn't assume a URL shape that could
    // legitimately differ (e.g. a national cloud, or B2C).
    const response = await fetch(configUrl);
    if (!response.ok) {
      throw new Error(`Failed to fetch OIDC discovery document from ${configUrl}: HTTP ${response.status}`);
    }
    const doc = (await response.json()) as Partial<OidcDiscoveryDocument>;
    if (!doc.issuer || !doc.jwks_uri) {
      throw new Error(`OIDC discovery document at ${configUrl} is missing issuer/jwks_uri`);
    }
    cachedDiscovery = { issuer: doc.issuer, jwks_uri: doc.jwks_uri };
  }
  return cachedDiscovery;
}

async function getJwks(jwksUri: string): Promise<ReturnType<typeof createRemoteJWKSet>> {
  if (!cachedJwks) {
    // jose's own remote JWK set already caches fetched keys internally
    // (with a cooldown between re-fetches on a cache miss) — this
    // module-level singleton just avoids recreating the fetcher itself
    // on every invocation, same pattern as email.ts's cachedTransporter
    // and cosmos.ts's cachedClient.
    cachedJwks = createRemoteJWKSet(new URL(jwksUri));
  }
  return cachedJwks;
}

export class TokenValidationError extends Error {}

/** The claims this service's own logic needs out of a verified bearer
 *  token — never the raw payload, so a call site can't accidentally
 *  trust an unverified claim. `department` is best-effort: `oid` is
 *  required (the token is rejected without it), but a token missing
 *  `department` still verifies fine — it just means the caller has no
 *  claim-based approver scope resolvable from it (see
 *  requestAuth.ts/decideQuotaRequest.ts, which handle an empty
 *  department explicitly, not by assuming one).
 *
 *  `roles` closes the gap flagged by this session's security review of
 *  this fork: quota-api-policy.xml already requires the `Quota.Approve`
 *  Entra app role for /decide and /pending at the APIM layer, but
 *  nothing downstream of APIM ever re-checked it — a caller who leaked
 *  quota-service's function key and held ANY genuinely valid token for
 *  this app registration (i.e. any authenticated employee, no special
 *  role required) could call decideQuotaRequest directly and approve/
 *  deny as themselves, since corroborateIdentity only verified oid/
 *  department, never role membership. `roles` is extracted here (best-
 *  effort, like `department`: a token with none still verifies fine,
 *  it just carries `[]`) so requestAuth.ts's corroborateIdentity can
 *  independently re-check role membership the same defense-in-depth
 *  way it already re-checks oid, instead of trusting that a call which
 *  reached this far necessarily came through APIM's own role gate. */
export interface VerifiedTokenClaims {
  oid: string;
  department: string | undefined;
  roles: string[];
}

/**
 * Verifies `authorizationHeader` (the raw "Bearer xyz..." header value)
 * as a genuine Entra ID access token for this gateway's own app
 * registration, and returns its `oid`/`department` claims.
 *
 * Throws TokenValidationError — never returns a "maybe" — on anything
 * that isn't a fully valid token: missing header, bad signature, wrong
 * audience/issuer, expired, or no oid claim present. Callers should
 * treat any throw as "reject this request," full stop.
 *
 * Named `verifyBearerTokenClaims` (not just "...Oid") since it now also
 * resolves `department` — used by decideQuotaRequest.ts to scope an
 * approver to their own department's requests
 * (guides/quota-override-approval.md's approver-resolution section) the
 * same defense-in-depth way `oid` is already independently re-verified,
 * not merely trusted from an APIM-forwarded header.
 */
export async function verifyBearerTokenClaims(authorizationHeader: string | null | undefined): Promise<VerifiedTokenClaims> {
  if (!authorizationHeader || !authorizationHeader.startsWith('Bearer ')) {
    throw new TokenValidationError('Missing or malformed Authorization header — expected "Bearer <token>"');
  }
  const token = authorizationHeader.slice('Bearer '.length).trim();
  if (token.length === 0) {
    throw new TokenValidationError('Empty bearer token');
  }

  const audience = process.env.Entra_Audience;
  if (!audience) {
    throw new TokenValidationError('Entra_Audience app setting is required for token re-validation');
  }

  const { issuer, jwks_uri: jwksUri } = await getDiscoveryDocument();
  const jwks = await getJwks(jwksUri);

  let payload;
  try {
    ({ payload } = await jwtVerify(token, jwks, { issuer, audience }));
  } catch (err) {
    throw new TokenValidationError(`Bearer token failed signature/issuer/audience/expiry validation: ${(err as Error).message}`);
  }

  const oid = payload.oid;
  if (typeof oid !== 'string' || oid.length === 0) {
    throw new TokenValidationError('Bearer token has no oid claim');
  }
  const department = typeof payload.department === 'string' && payload.department.length > 0 ? payload.department : undefined;
  // Entra ID app-role assignments arrive as a JSON array of role-value
  // strings under the `roles` claim (not a comma-separated string — that
  // string shape is an APIM policy-expression convention used elsewhere
  // in this fork, e.g. frag-security-handler.xml's jwt-roles variable,
  // not the raw JWT's own claim shape). A token with no roles assigned
  // still verifies fine; it just carries an empty array, same
  // best-effort posture as `department` above.
  const rolesClaim = payload.roles;
  const roles = Array.isArray(rolesClaim) ? rolesClaim.filter((r): r is string => typeof r === 'string') : [];
  return { oid, department, roles };
}

/**
 * TEST-ONLY. Clears the module-level discovery-document/JWKS caches so
 * each test in tests/tokenValidation.test.ts starts from a clean slate
 * regardless of what a previous test already cached — production code
 * never calls this (there's no legitimate reason to invalidate these
 * caches mid-process; a genuine key rotation is handled by jose's own
 * internal JWKS refresh-on-miss, not by this).
 */
export function _resetCacheForTests(): void {
  cachedDiscovery = undefined;
  cachedJwks = undefined;
}
