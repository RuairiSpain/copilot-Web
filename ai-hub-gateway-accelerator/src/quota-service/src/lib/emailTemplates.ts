// Pure, framework-free content-building functions — deliberately separated
// from src/lib/email.ts's actual SMTP send (I/O) so the content itself is
// unit-testable, same "separate pure logic from I/O" pattern as
// quotaLogic.ts. Building a subject/body string is a pure function of a
// QuotaOverrideRequest; nothing here touches a network or a clock beyond
// what's already on the record.

import { QuotaOverrideRequest } from './types';

export interface EmailContent {
  subject: string;
  html: string;
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/**
 * Sent to the configured approver contact (QuotaApproval_ApproverEmail)
 * when a new request is submitted — the "request created" half of
 * guides/quota-override-approval.md §6's notification design.
 */
export function buildRequestCreatedEmail(request: QuotaOverrideRequest): EmailContent {
  const escalationLine = request.requiresEscalation
    ? '<p style="color:#b91c1c;font-weight:bold;">⚠ This request exceeds the escalation threshold — recommend a second-level review.</p>'
    : '';
  const subject = `[Quota] New request pending — ${request.scopeType} ${request.scopeId} (${request.currentQuota} → ${request.requestedQuota})${request.requiresEscalation ? ' [ESCALATION]' : ''}`;
  const html = `
    <p>A new quota override request is pending approval.</p>
    <ul>
      <li><b>Scope:</b> ${escapeHtml(request.scopeType)} ${escapeHtml(request.scopeId)}</li>
      <li><b>Access contract:</b> ${escapeHtml(request.subscriptionId)}</li>
      <li><b>Requested by:</b> ${escapeHtml(request.requestedBy)}</li>
      <li><b>Change:</b> ${request.currentQuota} → ${request.requestedQuota} tokens/month</li>
      <li><b>Reason:</b> ${escapeHtml(request.reason)}</li>
    </ul>
    ${escalationLine}
    <p>Request id: <code>${escapeHtml(request.id)}</code></p>
  `.trim();
  return { subject, html };
}

/**
 * Sent to the ORIGINAL REQUESTER (request.requestedByEmail) once a
 * budget holder decides their request — the "decided" half.
 */
export function buildDecisionEmail(request: QuotaOverrideRequest): EmailContent {
  const decidedEvent = request.statusHistory[request.statusHistory.length - 1];
  const noteLine = decidedEvent?.note ? `<p><b>Note from the approver:</b> ${escapeHtml(decidedEvent.note)}</p>` : '';
  const subject = `[Quota] Your request was ${request.status.toLowerCase()}`;
  const outcomeLine =
    request.status === 'Approved'
      ? `<p>Your quota was increased to <b>${request.requestedQuota} tokens/month</b>.</p>`
      : `<p>Your request to increase your quota to ${request.requestedQuota} tokens/month was not approved.</p>`;
  const html = `
    <p>Your quota override request for <b>${escapeHtml(request.scopeType)} ${escapeHtml(request.scopeId)}</b> has been decided.</p>
    ${outcomeLine}
    ${noteLine}
    <p>Request id: <code>${escapeHtml(request.id)}</code></p>
  `.trim();
  return { subject, html };
}
