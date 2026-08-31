import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { getQuotaOverrideRequestsContainer } from '../lib/cosmos';
import { resolveVerifiedIdentity } from '../lib/quotaLogic';
import { corroborateIdentity } from '../lib/requestAuth';
import { QuotaOverrideRequest } from '../lib/types';

export interface ListPendingQuotaRequestsDeps {
  getContainer: () => ReturnType<typeof getQuotaOverrideRequestsContainer>;
  /** Optional so existing test call sites that only care about the
   *  Cosmos query (and never send an x-verified-oid header, so this
   *  branch is never reached) don't have to supply a stub. Falls back
   *  to the real corroborateIdentity when omitted. */
  corroborateIdentity?: typeof corroborateIdentity;
}

const defaultDeps: ListPendingQuotaRequestsDeps = {
  getContainer: getQuotaOverrideRequestsContainer,
  corroborateIdentity,
};

/**
 * Called by the recurrence-triggered quota-approval-notification Logic
 * App (src/usage-ingestion-logicapp/quota-approval-notification/workflow.json)
 * on each tick — the Logic App itself has no ability to run an arbitrary
 * Cosmos SQL query (its ServiceProvider connector, as used elsewhere in
 * this repo, only demonstrates CreateOrUpdateDocument/ReadDocument point
 * operations), so the actual "find new Pending requests" query lives here
 * instead, where it's typed and testable.
 *
 * Only returns requests with no notifiedAt yet — the Logic App calls
 * markQuotaRequestNotified once its webhook post succeeds, so a request
 * already sitting Pending across multiple recurrence ticks is not
 * re-notified every 5 minutes until someone decides it.
 *
 * This is a cross-partition query (no subscriptionId filter — the
 * notification sweep is intentionally gateway-wide, not per-contract) —
 * acceptable at the expected volume of *pending approval requests*
 * (a human-paced trickle, not the request-per-second usage-record
 * volume the rest of this accelerator's Cosmos containers see), but
 * flagged here rather than assumed: if approval volume is ever high
 * enough for this to matter, add a status+subscriptionId composite index
 * the way modelPricingContainer already does for its own hot query
 * (bicep/infra/modules/cosmos-db/cosmos-db.bicep).
 *
 * Role re-check, closing this session's security-review finding: this
 * route is shared by two genuinely different callers, distinguished the
 * same way submitQuotaRequest.ts/decideQuotaRequest.ts already
 * distinguish "came through APIM" from "internal call" — by whether
 * x-verified-oid is present.
 *   - The internal quota-approval-notification Logic App calls the raw
 *     function-key URL directly (QuotaService_ListPendingUrl) with no
 *     bearer token and no x-verified-oid header at all — that's a
 *     trusted, gateway-internal control-plane job, not a person, and is
 *     left untouched by this check.
 *   - The external Quota Override API's GET /quota/pending always
 *     routes through quota-api-policy.xml, which already requires the
 *     Quota.Approve role at the APIM layer and always sets
 *     x-verified-oid from the validated token before forwarding. Until
 *     now nothing downstream re-checked that role, so a caller who
 *     obtained the function key and any valid employee token could list
 *     every pending request (including requester identity) directly,
 *     bypassing APIM's role gate — the same class of bug decideQuotaRequest
 *     had. When x-verified-oid is present, this now independently
 *     re-verifies Quota.Approve the same way decideQuotaRequest does.
 */
export async function listPendingQuotaRequests(
  request: HttpRequest,
  context: InvocationContext,
  deps: ListPendingQuotaRequestsDeps = defaultDeps
): Promise<HttpResponseInit> {
  const verifiedOid = resolveVerifiedIdentity(request.headers.get('x-verified-oid'));
  if (verifiedOid) {
    const headerDepartment = resolveVerifiedIdentity(request.headers.get('x-verified-department')) ?? undefined;
    const corroborate = deps.corroborateIdentity ?? corroborateIdentity;
    const corroboration = await corroborate(request, verifiedOid, context, undefined, headerDepartment, 'Quota.Approve');
    if (!corroboration.ok) {
      return { status: corroboration.status, jsonBody: { error: corroboration.message } };
    }
  }

  const container = deps.getContainer();
  const { resources } = await container.items
    .query<QuotaOverrideRequest>({
      query:
        'SELECT * FROM c WHERE c.status = "Pending" AND NOT IS_DEFINED(c.notifiedAt) ORDER BY c.requiresEscalation DESC, c.createdAt ASC',
    })
    .fetchAll();

  context.log(`listPendingQuotaRequests: ${resources.length} pending request(s)`);
  return { jsonBody: resources };
}

app.http('listPendingQuotaRequests', {
  // route: matches BOTH callers now — the internal quota-approval-notification
  // Logic App (function-key only, via QuotaService_ListPendingUrl) and the
  // new Quota Override API's GET /quota/pending (JWT + Quota.Approve role,
  // for the "Approve/Deny Requests" canvas app screen — now re-verified in
  // code, not just at the APIM layer, see the handler's own doc comment
  // above). If you change this, update QuotaService_ListPendingUrl on the
  // Logic App too.
  route: 'pending',
  methods: ['GET'],
  authLevel: 'function',
  handler: listPendingQuotaRequests,
});
