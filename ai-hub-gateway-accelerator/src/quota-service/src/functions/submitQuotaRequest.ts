import { randomUUID } from 'node:crypto';
import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { getQuotaOverrideRequestsContainer } from '../lib/cosmos';
import { hasOpenRequest, requiresEscalation, resolveVerifiedIdentity } from '../lib/quotaLogic';
import { corroborateIdentity } from '../lib/requestAuth';
import { QuotaOverrideRequest, QuotaScopeType } from '../lib/types';

export interface SubmitQuotaRequestDeps {
  getContainer: () => ReturnType<typeof getQuotaOverrideRequestsContainer>;
  corroborateIdentity: typeof corroborateIdentity;
  newRequestId: () => string;
}

const defaultDeps: SubmitQuotaRequestDeps = {
  getContainer: getQuotaOverrideRequestsContainer,
  corroborateIdentity,
  newRequestId: () => `req-${randomUUID()}`,
};

/**
 * §5 self-service request flow. Called through the new Quota Override
 * API in APIM (bicep/infra/modules/quota-service/quota-api-policy.xml),
 * which validates the caller's JWT and sets x-verified-oid from its oid
 * claim — REQUIRED below, and used as requestedBy. Any `requestedBy` in
 * the JSON body is ignored entirely; trusting it was exactly the gap
 * guides/enterprise-hardening-checklist.md §1 flagged (a caller could
 * submit "on behalf of" anyone). Direct calls with just the function key
 * and no x-verified-oid header are rejected with 401 — this endpoint
 * is no longer reachable on function-key trust alone, unlike
 * getQuotaAllowance/listPendingQuotaRequests/markQuotaRequestNotified/
 * expireQuotaOverrides, which stay internal-only and unchanged.
 *
 * Impersonation defense-in-depth (guides/quota-override-approval.md's
 * "Impersonation" section): the x-verified-oid header alone is a plain,
 * spoofable HTTP header once past APIM — anyone with the function key
 * could set it directly. corroborateIdentity() independently
 * re-validates the caller's real bearer token and cross-checks its own
 * oid claim against this header before either is trusted.
 * `deps.corroborateIdentity` is injectable the same way — see
 * decideQuotaRequest.ts's identical note on why that's tested
 * separately, not re-verified here.
 */
export async function submitQuotaRequest(
  request: HttpRequest,
  context: InvocationContext,
  deps: SubmitQuotaRequestDeps = defaultDeps
): Promise<HttpResponseInit> {
  const requestedBy = resolveVerifiedIdentity(request.headers.get('x-verified-oid'));
  if (!requestedBy) {
    return { status: 401, jsonBody: { error: 'Missing or empty x-verified-oid header — this endpoint must be called through the Quota Override API, not directly.' } };
  }

  const corroboration = await deps.corroborateIdentity(request, requestedBy, context);
  if (!corroboration.ok) {
    return { status: corroboration.status, jsonBody: { error: corroboration.message } };
  }

  // x-verified-email is best-effort, not required (unlike x-verified-oid
  // above) — a token missing preferred_username/upn/email still proves
  // identity for authorization purposes, it just means this particular
  // request can't be emailed later when it's decided (see
  // listRecentlyDecidedQuotaRequests.ts, which skips requests with no
  // requestedByEmail rather than guessing at one).
  const requestedByEmail = resolveVerifiedIdentity(request.headers.get('x-verified-email')) ?? undefined;

  let body: {
    scopeType?: string;
    scopeId?: string;
    subscriptionId?: string;
    currentQuota?: number;
    requestedQuota?: number;
    reason?: string;
    durationDays?: number | null;
  };
  try {
    body = (await request.json()) as typeof body;
  } catch {
    return { status: 400, jsonBody: { error: 'Request body must be JSON' } };
  }

  const { scopeType, scopeId, subscriptionId, currentQuota, requestedQuota, reason } = body;
  const durationDays = body.durationDays === undefined ? Number(process.env.QuotaOverride_DefaultDurationDays ?? '30') : body.durationDays;

  if (
    (scopeType !== 'user' && scopeType !== 'team') ||
    !scopeId ||
    !subscriptionId ||
    typeof currentQuota !== 'number' ||
    typeof requestedQuota !== 'number' ||
    requestedQuota <= currentQuota ||
    !reason ||
    reason.trim().length === 0
  ) {
    return {
      status: 400,
      jsonBody: {
        error:
          'Request body must be { scopeType: "user"|"team", scopeId, subscriptionId, currentQuota: number, requestedQuota: number > currentQuota, reason: non-empty string, durationDays?: number|null }. requestedBy is no longer read from the body — it comes from the verified x-verified-oid header.',
      },
    };
  }

  const container = deps.getContainer();

  // §7 guardrail — one Pending request per scope at a time.
  const { resources: pending } = await container.items
    .query<{ id: string }>({
      query:
        'SELECT c.id FROM c WHERE c.subscriptionId = @subscriptionId AND c.scopeType = @scopeType AND c.scopeId = @scopeId AND c.status = "Pending"',
      parameters: [
        { name: '@subscriptionId', value: subscriptionId },
        { name: '@scopeType', value: scopeType },
        { name: '@scopeId', value: scopeId },
      ],
    })
    .fetchAll();

  if (hasOpenRequest(pending.length)) {
    return {
      status: 409,
      jsonBody: {
        error: 'A pending quota request already exists for this scope. Wait for it to be decided before submitting another.',
        existingRequestId: pending[0].id,
      },
    };
  }

  const escalationMultiplier = Number(process.env.QuotaOverride_EscalationMultiplier ?? '3');
  const now = new Date().toISOString();
  const doc: QuotaOverrideRequest = {
    id: deps.newRequestId(),
    docType: 'quotaOverrideRequest',
    scopeType: scopeType as QuotaScopeType,
    scopeId,
    subscriptionId,
    requestedBy,
    ...(requestedByEmail ? { requestedByEmail } : {}),
    currentQuota,
    requestedQuota,
    reason: reason.trim(),
    durationDays: durationDays === null ? null : Number(durationDays),
    status: 'Pending',
    requiresEscalation: requiresEscalation(currentQuota, requestedQuota, escalationMultiplier),
    statusHistory: [{ status: 'Pending', at: now, by: requestedBy }],
    createdAt: now,
  };

  await container.items.create(doc);
  context.log(`submitQuotaRequest: created ${doc.id} for ${scopeType}:${scopeId} (escalation=${doc.requiresEscalation})`);

  return { status: 201, jsonBody: doc };
}

app.http('submitQuotaRequest', {
  // route: matches the friendly external path (/quota/submit) the new
  // Quota Override API in APIM exposes (bicep/infra/modules/quota-service/
  // quota-api.openapi.json) — the function's own internal name stays
  // "submitQuotaRequest" for logging/identification, but the HTTP route
  // it listens on is just "submit". Without this override, APIM would
  // forward to a path this Function App never registered.
  route: 'submit',
  methods: ['POST'],
  authLevel: 'function',
  handler: submitQuotaRequest,
});
