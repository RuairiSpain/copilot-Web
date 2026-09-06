// Pure, framework-free business logic — deliberately separated from the
// Cosmos/HTTP-handling functions so it can be unit-tested directly (see
// test/quotaLogic.test.ts) without mocking Azure SDKs. Mirrors
// guides/quota-override-approval.md §1, §2, §7.

import { QuotaAllowanceResponse, QuotaOverride, QuotaScopeType } from './types';

/**
 * §1 — resolves what the APIM policy fragment should cache and use.
 * An override past its expiresAt is treated exactly like no override at
 * all (the scheduled sweep, expireQuotaOverrides.ts, deletes these
 * eventually, but resolution must not depend on the sweep having already
 * run — a request arriving in the gap between expiry and the next sweep
 * must still fall back to baseline).
 */
export function resolveAllowance(
  override: QuotaOverride | undefined,
  baselineQuota: number
): QuotaAllowanceResponse {
  if (!override) {
    return { scopeType: 'none', effectiveQuota: baselineQuota, tpmTier: 'standard' };
  }
  if (override.expiresAt && new Date(override.expiresAt).getTime() <= Date.now()) {
    return { scopeType: 'none', effectiveQuota: baselineQuota, tpmTier: 'standard' };
  }
  // §1's hard backstop: an override can never let a scope exceed the
  // contract's own baseline ceiling. Clamp defensively here too, not just
  // at request-submission time, in case baselineQuota was lowered (a
  // contract-level GitOps change) after an override was already granted.
  // A baseline of 0 correctly clamps every override to 0 too — a
  // contract with no quota left grants nothing, regardless of any
  // previously-approved override.
  const effectiveQuota = Math.min(override.effectiveQuota, baselineQuota);
  return { scopeType: override.scopeType, effectiveQuota, tpmTier: override.tpmTier };
}

/**
 * §7 guardrail — an override request past this multiple of the scope's
 * current quota needs a second-level approver rather than a single-click
 * approval. baselineQuota <= 0 is treated as "always escalate" (a
 * degenerate/misconfigured baseline should never silently auto-approve).
 */
export function requiresEscalation(
  currentQuota: number,
  requestedQuota: number,
  escalationMultiplier: number
): boolean {
  if (currentQuota <= 0) {
    return true;
  }
  return requestedQuota > currentQuota * escalationMultiplier;
}

/**
 * §7 guardrail — a scope may have at most one Pending request open at a
 * time. Callers pass in whatever Pending requests already exist for that
 * scope (a Cosmos query result); kept as a pure predicate so the
 * decision logic itself doesn't need a live Cosmos container to test.
 */
export function hasOpenRequest(pendingCountForScope: number): boolean {
  return pendingCountForScope > 0;
}

/**
 * Monthly reset — the counterpart to expireQuotaOverrides.ts's daily
 * per-document sweep. That sweep only clears an override once ITS OWN
 * expiresAt has passed, which does nothing for a permanent grant
 * (expiresAt: null) and doesn't align a temporary grant's lifetime to
 * calendar-month boundaries. This predicate decides, for the monthly
 * reset job, whether a given override should survive it.
 *
 * NOTE on what actually needs this: `llm-token-limit`'s own
 * token-quota-period="Monthly" already resets tier-1/tier-2 USAGE every
 * month automatically — that's built into APIM, no code involved. This
 * function is about the OVERRIDE GRANT itself (the elevated allowance a
 * budget holder approved), which nothing resets on a calendar boundary
 * today — see guides/quota-override-approval.md's "Implementation
 * status" for the distinction, since conflating the two is an easy
 * mistake.
 *
 * Design: every temporary override (expiresAt set) is cleared at the
 * month boundary regardless of how much of its own duration is left —
 * a request approved for "30 days" starting mid-month doesn't get to
 * quietly straddle into the next month's budget cycle. A permanent
 * override (expiresAt: null) was a deliberate, presumably-considered
 * decision to grant it indefinitely — silently downgrading it every
 * month would defeat the point of "permanent" and force needless
 * re-approval, so it survives by default. includePermanent flips that
 * default for organizations that want a genuine hard reset for
 * everyone, permanent grants included.
 */
export function survivesMonthlyReset(expiresAt: string | null, includePermanent: boolean): boolean {
  if (expiresAt === null) {
    return !includePermanent;
  }
  return false;
}

/**
 * Authorization gap fix — guides/quota-override-approval.md's
 * "Implementation status" and enterprise-hardening-checklist.md §1.
 *
 * submitQuotaRequest and decideQuotaRequest no longer trust a
 * client-supplied identity field in the request body — they require the
 * `x-verified-oid` header, set ONLY by the new quota-api-policy.xml
 * (bicep/infra/modules/quota-service/) from a signature-validated JWT's
 * `oid` claim, never copyable/spoofable by the calling client. This
 * function is the pure "is there a usable value here" check; the actual
 * trust boundary is the APIM policy that sets the header, not this
 * function — this just guards against an empty/missing header reaching
 * the rest of the handler as if it were a real identity.
 *
 * Returns null (reject with 401) for missing/blank/whitespace-only
 * values rather than silently falling back to anything from the
 * request body — there is deliberately no fallback path, since a
 * fallback is exactly what reopened the impersonation hole this exists
 * to close.
 */
export function resolveVerifiedIdentity(headerValue: string | null | undefined): string | null {
  if (!headerValue) {
    return null;
  }
  const trimmed = headerValue.trim();
  return trimmed.length > 0 ? trimmed : null;
}

/**
 * The actual anti-spoofing check behind tokenValidation.ts's
 * verifyBearerTokenOid() — pure, so it's testable without a live Entra
 * tenant. headerOid is what quota-api-policy.xml already set from a
 * validated token; tokenOid is what THIS function's own independent
 * re-validation of the raw bearer token found. They must match exactly:
 * a caller with a spoofed header but a genuinely valid token for a
 * DIFFERENT person fails this just as surely as a caller with no valid
 * token at all.
 */
export function identityMatchesToken(headerOid: string, tokenOid: string): boolean {
  return headerOid.length > 0 && headerOid === tokenOid;
}

/**
 * Approver-resolution — claim-based, per
 * guides/quota-override-approval.md's approver-resolution section.
 * `Quota.Approve` (the APIM app role) proves the caller is *a* real
 * approver; this decides whether they're the right one for THIS
 * specific request.
 *
 * `user`-scoped requests are deliberately left unscoped — any
 * `Quota.Approve` holder can still decide them. There is no
 * org-hierarchy/manager data source anywhere in this fork to resolve
 * "who is the right approver for this individual," and adding a new
 * reference table for it was explicitly out of scope for this design
 * (see decideQuotaRequest.ts's own comment) — stated here plainly as a
 * residual gap, not silently narrowed by this function pretending to
 * cover a case it doesn't.
 *
 * `team`-scoped requests require the approver's own (independently
 * re-verified — see requestAuth.ts) JWT `department` claim to exactly
 * equal the request's `scopeId`. An approver with no resolvable
 * department (`approverDepartment: undefined`) can never decide a
 * team-scoped request — fails closed, not open.
 */
export function approverAuthorizedForScope(
  approverDepartment: string | undefined,
  requestScopeType: QuotaScopeType,
  requestScopeId: string
): boolean {
  if (requestScopeType !== 'team') {
    return true;
  }
  return approverDepartment !== undefined && approverDepartment === requestScopeId;
}

/**
 * §7 — computes expiresAt from a submission-time durationDays, defaulting
 * to the configured default (short-lived by design) rather than an
 * open-ended grant when the requester doesn't specify one.
 */
export function computeExpiresAt(durationDays: number | null, now: Date = new Date()): string | null {
  if (durationDays === null) {
    return null; // explicit permanent request — still subject to admin review, not blocked here
  }
  const expires = new Date(now.getTime());
  expires.setUTCDate(expires.getUTCDate() + durationDays);
  return expires.toISOString();
}
