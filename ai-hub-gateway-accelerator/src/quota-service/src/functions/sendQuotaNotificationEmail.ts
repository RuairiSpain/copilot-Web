import { app, HttpRequest, HttpResponseInit, InvocationContext } from '@azure/functions';
import { sendNotificationEmail } from '../lib/email';
import { buildDecisionEmail, buildRequestCreatedEmail } from '../lib/emailTemplates';
import { QuotaOverrideRequest } from '../lib/types';

export interface SendQuotaNotificationEmailDeps {
  sendNotificationEmail: typeof sendNotificationEmail;
}

const defaultDeps: SendQuotaNotificationEmailDeps = {
  sendNotificationEmail,
};

/**
 * Called by the quota-approval-notification Logic App for both
 * notification types (guides/quota-override-approval.md §6) — the Logic
 * App decides WHO to send to (the configured approver contact for a new
 * request, or the request's own requestedByEmail for a decision) and
 * hands over the whole request document; the actual subject/html is
 * built HERE, server-side, from the pure, unit-tested functions in
 * emailTemplates.ts — so there's exactly one place that content logic
 * lives, not duplicated into Logic App expressions.
 *
 * Internal only — function-key auth, same trust tier as
 * getQuotaAllowance/listPendingQuotaRequests/markQuotaRequestNotified.
 */
export async function sendQuotaNotificationEmail(
  request: HttpRequest,
  context: InvocationContext,
  deps: SendQuotaNotificationEmailDeps = defaultDeps
): Promise<HttpResponseInit> {
  let body: { to?: string; templateType?: string; request?: QuotaOverrideRequest };
  try {
    body = (await request.json()) as typeof body;
  } catch {
    return { status: 400, jsonBody: { error: 'Request body must be JSON' } };
  }

  const { to, templateType, request: quotaRequest } = body;
  if (!to || (templateType !== 'requestCreated' && templateType !== 'decision') || !quotaRequest) {
    return {
      status: 400,
      jsonBody: { error: 'Request body must be { to, templateType: "requestCreated"|"decision", request: <the full QuotaOverrideRequest document> }' },
    };
  }

  const content = templateType === 'requestCreated' ? buildRequestCreatedEmail(quotaRequest) : buildDecisionEmail(quotaRequest);

  try {
    await deps.sendNotificationEmail({ to, subject: content.subject, html: content.html });
  } catch (err) {
    context.error(`sendQuotaNotificationEmail: send failed for ${to} (${templateType})`, err);
    return { status: 502, jsonBody: { error: 'Email send failed' } };
  }

  context.log(`sendQuotaNotificationEmail: sent ${templateType} to ${to} for ${quotaRequest.id}`);
  return { status: 202, jsonBody: { sent: true } };
}

app.http('sendQuotaNotificationEmail', {
  route: 'sendEmail',
  methods: ['POST'],
  authLevel: 'function',
  handler: sendQuotaNotificationEmail,
});
