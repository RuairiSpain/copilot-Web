import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { getQuotaOverrideRequestsContainer, getQuotaOverridesContainer, readItemOrUndefined } from '../lib/cosmos';
import { approverAuthorizedForScope, computeExpiresAt, resolveVerifiedIdentity } from '../lib/quotaLogic';
import { corroborateIdentity } from '../lib/requestAuth';
import { QuotaOverride, QuotaOverrideRequest } from '../lib/types';

export interface DecideQuotaRequestDeps {
  getRequestsContainer: () => ReturnType<typeof getQuotaOverrideRequestsContainer>;
  getOverridesContainer: () => ReturnType<typeof getQuotaOverridesContainer>;
  corroborateIdentity: typeof corroborateIdentity;
}

const defaultDeps: DecideQuotaRequestDeps = {
  getRequestsContainer: getQuotaOverrideRequestsContainer,
  getOverridesContainer: getQuotaOverridesContainer,
  corroborateIdentity,
};

/**
 * §6 approval decision. This is the one step in the whole workflow that
 * must be a deliberate human action — nothing upstream of this endpoint
 * (the recurrence-triggered notification Logic App, listPendingQuotaRequests)
 * decides anything on its own.
 *
 * Called through the Quota Override API in APIM
 * (bicep/infra/modules/quota-service/quota-api-policy.xml), which
 * requires the Quota.Approve app role AND sets x-verified-oid from the
 * validated JWT's oid claim — REQUIRED below, used as decidedBy. Any
 * `decidedBy` in the JSON body is ignored: trusting it was exactly the
 * authorization gap flagged in guides/enterprise-hardening-checklist.md
 * §1 (anyone holding the function key could approve/deny AS anyone).
 * This closes "is the caller a verified, role-holding person."
 *
 * Impersonation defense-in-depth — same corroborateIdentity() call as
 * submitQuotaRequest.ts, same reasoning: x-verified-oid alone is a
 * spoofable header once past APIM, so this independently re-validates
 * the caller's real bearer token before trusting who "decidedBy" is.
 * `deps.corroborateIdentity` is injectable (a fake ok/fail stub in this
 * function's own tests — corroborateIdentity's actual crypto logic gets
 * its own dedicated real-token tests in tests/requestAuth.test.ts and
 * tests/tokenValidation.test.ts, not re-verified redundantly here).
 *
 * Role re-check, closing this session's security-review finding: passes
 * `'Quota.Approve'` as corroborateIdentity's `requiredRole` so a direct
 * call to this endpoint (bypassing APIM with a leaked function key, but
 * a genuinely valid token for SOME authenticated employee) is rejected
 * with 403 unless that employee's own token actually carries the
 * `Quota.Approve` app role — previously this endpoint only confirmed
 * the caller was a real person, never that they were the right KIND of
 * real person. APIM's quota-api-policy.xml still enforces this role too
 * (defense-in-depth: the fix here means quota-service no longer relies
 * on APIM alone to have been the only path in).
 *
 * Approver-resolution (claim-based) — closes "is this the RIGHT
 * approver for this specific request," the gap the paragraph above used
 * to flag as still open. For a `team`-scoped request, the approver's
 * own independently-re-verified JWT `department` claim (returned by
 * corroborateIdentity, never the raw x-verified-department header —
 * see requestAuth.ts) must equal the request's `scopeId`
 * (approverAuthorizedForScope() in quotaLogic.ts) — otherwise 403.
 * `user`-scoped requests are deliberately UNCHANGED: any `Quota.Approve`
 * holder can still decide them, since there's no org-hierarchy/manager
 * data source in this fork to resolve "the right approver" for an
 * individual — a residual, explicitly accepted gap, not silently
 * papered over. See guides/quota-override-approval.md's
 * approver-resolution section.
 */
export async function decideQuotaRequest(
  request: HttpRequest,
  context: InvocationContext,
  deps: DecideQuotaRequestDeps = defaultDeps
): Promise<HttpResponseInit> {
  const decidedBy = resolveVerifiedIdentity(request.headers.get('x-verified-oid'));
  if (!decidedBy) {
    return { status: 401, jsonBody: { error: 'Missing or empty x-verified-oid header — this endpoint must be called through the Quota Override API, not directly.' } };
  }

  const headerDepartment = resolveVerifiedIdentity(request.headers.get('x-verified-department')) ?? undefined;
  const corroboration = await deps.corroborateIdentity(request, decidedBy, context, undefined, headerDepartment, 'Quota.Approve');
  if (!corroboration.ok) {
    return { status: corroboration.status, jsonBody: { error: corroboration.message } };
  }
  const approverDepartment = corroboration.department;

  let body: { requestId?: string; subscriptionId?: string; decision?: string; note?: string; tpmTier?: string };
  try {
    body = (await request.json()) as typeof body;
  } catch {
    return { status: 400, jsonBody: { error: 'Request body must be JSON' } };
  }

  const { requestId, subscriptionId, decision } = body;
  if (!requestId || !subscriptionId || (decision !== 'Approved' && decision !== 'Denied')) {
    return {
      status: 400,
      jsonBody: {
        error:
          'Request body must be { requestId, subscriptionId, decision: "Approved"|"Denied", note?, tpmTier? }. decidedBy is no longer read from the body — it comes from the verified x-verified-oid header.',
      },
    };
  }

  const requestsContainer = deps.getRequestsContainer();
  const quotaRequest = await readItemOrUndefined<QuotaOverrideRequest>(requestsContainer, requestId, subscriptionId);
  if (!quotaRequest) {
    return { status: 404, jsonBody: { error: `No request ${requestId} found for subscription ${subscriptionId}` } };
  }
  if (quotaRequest.status !== 'Pending') {
    // Never re-decide — statusHistory is append-only and a request is
    // decided exactly once. See guides/quota-override-approval.md §3.
    return { status: 409, jsonBody: { error: `Request ${requestId} is already ${quotaRequest.status}, not Pending` } };
  }

  if (!approverAuthorizedForScope(approverDepartment, quotaRequest.scopeType, quotaRequest.scopeId)) {
    context.warn(
      `decideQuotaRequest: ${decidedBy} (department=${approverDepartment ?? 'none'}) rejected — not authorized for ${quotaRequest.scopeType}:${quotaRequest.scopeId}`
    );
    return {
      status: 403,
      jsonBody: {
        error: `approver_not_authorized_for_scope: your department does not match this request's scope (${quotaRequest.scopeType}:${quotaRequest.scopeId})`,
      },
    };
  }

  const now = new Date().toISOString();
  quotaRequest.status = decision;
  quotaRequest.statusHistory.push({ status: decision, at: now, by: decidedBy, note: body.note });
  await requestsContainer.item(requestId, subscriptionId).replace(quotaRequest);

  if (decision === 'Denied') {
    context.log(`decideQuotaRequest: ${requestId} denied by ${decidedBy}`);
    return { jsonBody: quotaRequest };
  }

  // Approved: write/update the current-state document the APIM policy
  // fragment actually reads. This is the only place that container is
  // ever written from this service.
  const overridesContainer = deps.getOverridesContainer();
  const override: QuotaOverride = {
    id: `${quotaRequest.scopeType}-${quotaRequest.scopeId}`,
    docType: 'quotaOverride',
    scopeType: quotaRequest.scopeType,
    scopeId: quotaRequest.scopeId,
    subscriptionId: quotaRequest.subscriptionId,
    baselineQuota: quotaRequest.currentQuota,
    effectiveQuota: quotaRequest.requestedQuota,
    tpmTier: (body.tpmTier as QuotaOverride['tpmTier']) ?? 'standard',
    grantedBy: decidedBy,
    requestId: quotaRequest.id,
    expiresAt: computeExpiresAt(quotaRequest.durationDays),
    updatedAt: now,
  };
  await overridesContainer.items.upsert(override);

  context.log(`decideQuotaRequest: ${requestId} approved by ${decidedBy}, effectiveQuota=${override.effectiveQuota}, expiresAt=${override.expiresAt}`);
  return { jsonBody: { request: quotaRequest, override } };
}

app.http('decideQuotaRequest', {
  // route: see submitQuotaRequest.ts's identical comment — matches the
  // Quota Override API's /quota/decide path.
  route: 'decide',
  methods: ['POST'],
  authLevel: 'function',
  handler: decideQuotaRequest,
});
