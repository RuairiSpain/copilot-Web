import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { getQuotaOverrideRequestsContainer, readItemOrUndefined } from '../lib/cosmos';
import { QuotaOverrideRequest } from '../lib/types';

export interface MarkRequesterNotifiedDeps {
  getContainer: () => ReturnType<typeof getQuotaOverrideRequestsContainer>;
}

const defaultDeps: MarkRequesterNotifiedDeps = {
  getContainer: getQuotaOverrideRequestsContainer,
};

/**
 * Mirrors markQuotaRequestNotified.ts exactly, for the other half of the
 * notification lifecycle: sets requesterNotifiedAt once the "your
 * request was decided" email succeeds, so
 * listRecentlyDecidedQuotaRequests stops returning it. Same idempotency
 * and same "don't call this if the send failed" contract as its sibling
 * — a failed email leaves the request eligible for retry on the next
 * tick rather than being silently dropped.
 */
export async function markRequesterNotified(
  request: HttpRequest,
  context: InvocationContext,
  deps: MarkRequesterNotifiedDeps = defaultDeps
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
    // reasoning.
    context.error(`markRequesterNotified: Cosmos read failed for ${requestId}`, err);
    return { status: 502, jsonBody: { error: 'Failed to look up the quota request due to a temporary data-access error. Please retry.' } };
  }
  if (!quotaRequest) {
    return { status: 404, jsonBody: { error: `No request ${requestId} found for subscription ${subscriptionId}` } };
  }

  if (quotaRequest.requesterNotifiedAt) {
    return { jsonBody: quotaRequest };
  }

  quotaRequest.requesterNotifiedAt = new Date().toISOString();
  try {
    await container.item(requestId, subscriptionId).replace(quotaRequest);
  } catch (err) {
    context.error(`markRequesterNotified: Cosmos write failed for ${requestId}`, err);
    return { status: 502, jsonBody: { error: 'Failed to update the quota request due to a temporary data-access error. Please retry.' } };
  }
  context.log(`markRequesterNotified: ${requestId}`);
  return { jsonBody: quotaRequest };
}

app.http('markRequesterNotified', {
  route: 'markDecidedNotified',
  methods: ['POST'],
  authLevel: 'function',
  handler: markRequesterNotified,
});
