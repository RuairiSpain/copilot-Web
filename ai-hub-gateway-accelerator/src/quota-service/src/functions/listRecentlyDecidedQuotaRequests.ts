import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { getQuotaOverrideRequestsContainer } from '../lib/cosmos';
import { QuotaOverrideRequest } from '../lib/types';

export interface ListRecentlyDecidedQuotaRequestsDeps {
  getContainer: () => ReturnType<typeof getQuotaOverrideRequestsContainer>;
}

const defaultDeps: ListRecentlyDecidedQuotaRequestsDeps = {
  getContainer: getQuotaOverrideRequestsContainer,
};

/**
 * Called by quota-approval-notification's decided-requests loop on each
 * recurrence tick — same shape as listPendingQuotaRequests.ts, different
 * filter: Approved/Denied requests that have a requestedByEmail to
 * notify AND haven't been notified yet (requesterNotifiedAt absent).
 * A request with no requestedByEmail (the submitter's token carried none
 * of the expected claims) is permanently excluded here — there's no
 * queue to retry into, no address to send to, nothing to do.
 */
export async function listRecentlyDecidedQuotaRequests(
  request: HttpRequest,
  context: InvocationContext,
  deps: ListRecentlyDecidedQuotaRequestsDeps = defaultDeps
): Promise<HttpResponseInit> {
  const container = deps.getContainer();
  let resources: QuotaOverrideRequest[];
  try {
    ({ resources } = await container.items
      .query<QuotaOverrideRequest>({
        query:
          'SELECT * FROM c WHERE (c.status = "Approved" OR c.status = "Denied") AND IS_DEFINED(c.requestedByEmail) AND NOT IS_DEFINED(c.requesterNotifiedAt) ORDER BY c.createdAt ASC',
      })
      .fetchAll());
  } catch (err) {
    // Consistent error-response shape fix — see listPendingQuotaRequests.ts's
    // identical comment for the full reasoning.
    context.error('listRecentlyDecidedQuotaRequests: Cosmos query failed', err);
    return { status: 502, jsonBody: { error: 'Failed to list recently decided quota requests due to a temporary data-access error. Please retry.' } };
  }

  context.log(`listRecentlyDecidedQuotaRequests: ${resources.length} decided-and-unnotified request(s)`);
  return { jsonBody: resources };
}

app.http('listRecentlyDecidedQuotaRequests', {
  route: 'decided',
  methods: ['GET'],
  authLevel: 'function',
  handler: listRecentlyDecidedQuotaRequests,
});
