import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { getQuotaOverrideRequestsContainer } from '../lib/cosmos';
import { QuotaOverrideRequest } from '../lib/types';

export interface ListPendingQuotaRequestsDeps {
  getContainer: () => ReturnType<typeof getQuotaOverrideRequestsContainer>;
}

const defaultDeps: ListPendingQuotaRequestsDeps = {
  getContainer: getQuotaOverrideRequestsContainer,
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
 */
export async function listPendingQuotaRequests(
  request: HttpRequest,
  context: InvocationContext,
  deps: ListPendingQuotaRequestsDeps = defaultDeps
): Promise<HttpResponseInit> {
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
  // for the "Approve/Deny Requests" canvas app screen). If you change this,
  // update QuotaService_ListPendingUrl on the Logic App too.
  route: 'pending',
  methods: ['GET'],
  authLevel: 'function',
  handler: listPendingQuotaRequests,
});
