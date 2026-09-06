import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { getQuotaOverrideRequestsContainer, readItemOrUndefined } from '../lib/cosmos';
import { QuotaOverrideRequest } from '../lib/types';

export interface MarkQuotaRequestNotifiedDeps {
  getContainer: () => ReturnType<typeof getQuotaOverrideRequestsContainer>;
}

const defaultDeps: MarkQuotaRequestNotifiedDeps = {
  getContainer: getQuotaOverrideRequestsContainer,
};

/**
 * Called by quota-approval-notification once its webhook post for a
 * given request succeeds. Sets notifiedAt (an operational marker, NOT a
 * statusHistory event — this isn't a decision, just "don't notify again")
 * so listPendingQuotaRequests stops returning it on future ticks.
 *
 * If the webhook call fails, the Logic App simply doesn't call this —
 * the request stays notifiedAt-less and gets picked up again on the next
 * tick. A missed notification is recoverable; a silently-dropped one
 * isn't.
 */
export async function markQuotaRequestNotified(
  request: HttpRequest,
  context: InvocationContext,
  deps: MarkQuotaRequestNotifiedDeps = defaultDeps
): Promise<HttpResponseInit> {
  let body: { requestId?: string; subscriptionId?: string };
  try {
    body = (await request.json()) as typeof body;
  } catch {
    return { status: 400, jsonBody: { error: 'Request body must be JSON' } };
  }

  const { requestId, subscriptionId } = body;
  if (!requestId || !subscriptionId) {
    return { status: 400, jsonBody: { error: 'Request body must be { requestId, subscriptionId }' } };
  }

  const container = deps.getContainer();
  let quotaRequest: QuotaOverrideRequest | undefined;
  try {
    quotaRequest = await readItemOrUndefined<QuotaOverrideRequest>(container, requestId, subscriptionId);
  } catch (err) {
    // Consistent error-response shape fix — see
    // listPendingQuotaRequests.ts's identical comment for the full
    // reasoning. readItemOrUndefined already turns a genuine 404 into
    // `undefined` (handled below); only a real Cosmos failure reaches here.
    context.error(`markQuotaRequestNotified: Cosmos read failed for ${requestId}`, err);
    return { status: 502, jsonBody: { error: 'Failed to look up the quota request due to a temporary data-access error. Please retry.' } };
  }
  if (!quotaRequest) {
    return { status: 404, jsonBody: { error: `No request ${requestId} found for subscription ${subscriptionId}` } };
  }

  if (quotaRequest.notifiedAt) {
    // Already marked — idempotent no-op, not an error (a Logic App retry
    // on a transient failure of ITS OWN downstream step could otherwise
    // double-call this).
    return { jsonBody: quotaRequest };
  }

  quotaRequest.notifiedAt = new Date().toISOString();
  try {
    await container.item(requestId, subscriptionId).replace(quotaRequest);
  } catch (err) {
    context.error(`markQuotaRequestNotified: Cosmos write failed for ${requestId}`, err);
    return { status: 502, jsonBody: { error: 'Failed to update the quota request due to a temporary data-access error. Please retry.' } };
  }
  context.log(`markQuotaRequestNotified: ${requestId}`);
  return { jsonBody: quotaRequest };
}

app.http('markQuotaRequestNotified', {
  methods: ['POST'],
  authLevel: 'function',
  handler: markQuotaRequestNotified,
});
