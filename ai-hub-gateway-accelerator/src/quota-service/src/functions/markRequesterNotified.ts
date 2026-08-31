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
  const quotaRequest = await readItemOrUndefined<QuotaOverrideRequest>(container, requestId, subscriptionId);
  if (!quotaRequest) {
    return { status: 404, jsonBody: { error: `No request ${requestId} found for subscription ${subscriptionId}` } };
  }

  if (quotaRequest.requesterNotifiedAt) {
    return { jsonBody: quotaRequest };
  }

  quotaRequest.requesterNotifiedAt = new Date().toISOString();
  await container.item(requestId, subscriptionId).replace(quotaRequest);
  context.log(`markRequesterNotified: ${requestId}`);
  return { jsonBody: quotaRequest };
}

app.http('markRequesterNotified', {
  route: 'markDecidedNotified',
  methods: ['POST'],
  authLevel: 'function',
  handler: markRequesterNotified,
});
