import { HttpRequest, InvocationContext } from '@azure/functions';
import { identityMatchesToken } from './quotaLogic';
import { TokenValidationError, verifyBearerTokenClaims } from './tokenValidation';

export interface CorroborateIdentityDeps {
  verifyBearerTokenClaims: typeof verifyBearerTokenClaims;
}

const defaultDeps: CorroborateIdentityDeps = {
  verifyBearerTokenClaims,
};

export type CorroborateIdentityResult =
  | { ok: true; department?: string }
  | { ok: false; status: number; message: string };

/**
 * Shared wiring for submitQuotaRequest.ts and decideQuotaRequest.ts —
 * the impersonation defense-in-depth described in tokenValidation.ts's
 * own doc comment, factored out once rather than duplicated at both
 * call sites.
 *
 * QuotaOverride_RequireTokenRevalidation defaults to "true" (secure by
 * default) — set to "false" ONLY for local development against a
 * function key with no real Entra tenant to validate against. Doing so
 * in a real deployment silently reopens the exact impersonation gap
 * this module exists to close, which is why it logs a loud warning
 * every single time it's bypassed, not just once.
 *
 * BREAKING CHANGE ON UPGRADE, stated plainly: once this ships, every
 * call to /submit or /decide requires a genuinely valid, correctly-
 * scoped Entra ID bearer token whose oid matches what APIM already
 * verified — not just the function key. If you deployed the quota-api
 * gateway from an earlier patch and haven't set Entra_TenantId /
 * Entra_Audience / Entra_OpenIdConfigUrl yet, these two endpoints will
 * start rejecting every request with a clear error until you do.
 *
 * Also independently re-verifies `department` the same way `oid` already
 * is, and returns it — decideQuotaRequest.ts uses this (not the raw
 * x-verified-department header) to scope an approver to their own
 * department's team-scoped requests (guides/quota-override-approval.md's
 * approver-resolution section). `headerDepartment` is optional and
 * best-effort: a token/caller with no department claim anywhere still
 * corroborates fine (returns `department: undefined`) — it just means
 * that caller can't be scope-checked against a team-scoped request,
 * handled explicitly by the caller, not assumed here.
 *
 * `requiredRole` closes the direct-call authorization bypass this
 * session's security review found: quota-api-policy.xml already
 * requires the `Quota.Approve` Entra app role for /decide and /pending
 * at the APIM layer, but until now nothing downstream re-checked it —
 * a caller who obtained quota-service's function key and held ANY
 * genuinely valid token for this app registration (any authenticated
 * employee, no special role needed) could call these endpoints
 * directly, bypassing APIM's role gate entirely, since oid/department
 * corroboration alone proves "this is a real person," not "this real
 * person is actually allowed to approve." When `requiredRole` is set,
 * a token whose independently re-verified `roles` claim doesn't include
 * it is rejected with 403 — fails closed, the same posture as every
 * other check in this function, never assumed present.
 */
export async function corroborateIdentity(
  request: HttpRequest,
  headerOid: string,
  context: InvocationContext,
  deps: CorroborateIdentityDeps = defaultDeps,
  headerDepartment?: string,
  requiredRole?: string
): Promise<CorroborateIdentityResult> {
  const requireRevalidation = (process.env.QuotaOverride_RequireTokenRevalidation ?? 'true').toLowerCase() !== 'false';
  if (!requireRevalidation) {
    context.warn(
      'corroborateIdentity: QuotaOverride_RequireTokenRevalidation=false — skipping independent bearer-token re-validation. ' +
        'This relies solely on the x-verified-oid/x-verified-department headers and reopens the impersonation gap this check exists to close. ' +
        (requiredRole ? `It also skips the "${requiredRole}" role check below entirely. ` : '') +
        'Only acceptable for local development against a function key with no real Entra tenant.'
    );
    return { ok: true, department: headerDepartment };
  }

  try {
    const {
      oid: tokenOid,
      department: tokenDepartment,
      roles: tokenRoles,
    } = await deps.verifyBearerTokenClaims(request.headers.get('Authorization'));
    if (!identityMatchesToken(headerOid, tokenOid)) {
      return { ok: false, status: 401, message: 'Bearer token identity does not match the gateway-verified identity — request rejected.' };
    }
    // Only reject on a genuine contradiction (both present, and
    // different) — the same suspicious-tampering signal the oid check
    // above guards against. A header/token that simply disagrees on
    // *presence* (one has a department claim, the other doesn't) isn't
    // rejected; the verified (token-derived) value always wins as the
    // source of truth returned to the caller, never the header's.
    if (headerDepartment && tokenDepartment && headerDepartment !== tokenDepartment) {
      return { ok: false, status: 401, message: 'Bearer token department claim does not match the gateway-verified department — request rejected.' };
    }
    if (requiredRole && !tokenRoles.includes(requiredRole)) {
      context.warn(`corroborateIdentity: ${tokenOid} rejected — bearer token is missing the required "${requiredRole}" app role.`);
      return { ok: false, status: 403, message: `This operation requires the "${requiredRole}" app role, which the caller's token does not carry.` };
    }
    return { ok: true, department: tokenDepartment };
  } catch (err) {
    const message = err instanceof TokenValidationError ? err.message : 'Token re-validation failed';
    context.warn(`corroborateIdentity: rejected — ${message}`);
    return { ok: false, status: 401, message };
  }
}
