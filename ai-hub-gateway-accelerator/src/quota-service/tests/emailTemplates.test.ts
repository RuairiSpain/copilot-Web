import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildDecisionEmail, buildRequestCreatedEmail } from '../src/lib/emailTemplates';
import { QuotaOverrideRequest } from '../src/lib/types';

function makeRequest(overrides: Partial<QuotaOverrideRequest> = {}): QuotaOverrideRequest {
  return {
    id: 'req-1234',
    docType: 'quotaOverrideRequest',
    scopeType: 'user',
    scopeId: 'a1b2c3d4-...',
    subscriptionId: 'LLM-HR-ChatAgent-DEV-SUB-01',
    requestedBy: 'a1b2c3d4-...',
    requestedByEmail: 'requester@example.com',
    currentQuota: 100000,
    requestedQuota: 250000,
    reason: 'Quarter-end batch reconciliation',
    durationDays: 30,
    status: 'Pending',
    requiresEscalation: false,
    statusHistory: [{ status: 'Pending', at: '2026-08-30T09:00:00.000Z', by: 'a1b2c3d4-...' }],
    createdAt: '2026-08-30T09:00:00.000Z',
    ...overrides,
  };
}

test('buildRequestCreatedEmail: includes the key facts', () => {
  const { subject, html } = buildRequestCreatedEmail(makeRequest());
  assert.match(subject, /New request pending/);
  assert.match(subject, /100000/);
  assert.match(subject, /250000/);
  assert.match(html, /Quarter-end batch reconciliation/);
  assert.match(html, /req-1234/);
});

test('buildRequestCreatedEmail: flags escalation in both subject and body', () => {
  const { subject, html } = buildRequestCreatedEmail(makeRequest({ requiresEscalation: true }));
  assert.match(subject, /ESCALATION/);
  assert.match(html, /second-level review/);
});

test('buildRequestCreatedEmail: does not mention escalation when not required', () => {
  const { subject, html } = buildRequestCreatedEmail(makeRequest({ requiresEscalation: false }));
  assert.doesNotMatch(subject, /ESCALATION/);
  assert.doesNotMatch(html, /second-level review/);
});

test('buildRequestCreatedEmail: escapes HTML in user-supplied fields (reason)', () => {
  const { html } = buildRequestCreatedEmail(makeRequest({ reason: '<script>alert(1)</script>' }));
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;/);
});

test('buildDecisionEmail: approved — mentions the new quota', () => {
  const request = makeRequest({
    status: 'Approved',
    statusHistory: [
      { status: 'Pending', at: '2026-08-30T09:00:00.000Z', by: 'a1b2c3d4-...' },
      { status: 'Approved', at: '2026-08-30T14:00:00.000Z', by: 'approver-oid', note: 'Looks reasonable' },
    ],
  });
  const { subject, html } = buildDecisionEmail(request);
  assert.match(subject, /approved/i);
  assert.match(html, /increased to/i);
  assert.match(html, /250000/);
  assert.match(html, /Looks reasonable/);
});

test('buildDecisionEmail: denied — does not claim an increase happened', () => {
  const request = makeRequest({
    status: 'Denied',
    statusHistory: [
      { status: 'Pending', at: '2026-08-30T09:00:00.000Z', by: 'a1b2c3d4-...' },
      { status: 'Denied', at: '2026-08-30T14:00:00.000Z', by: 'approver-oid' },
    ],
  });
  const { subject, html } = buildDecisionEmail(request);
  assert.match(subject, /denied/i);
  assert.doesNotMatch(html, /increased to/i);
  assert.match(html, /not approved/i);
});

test('buildDecisionEmail: no note on the decision event — no "Note from the approver" line', () => {
  const request = makeRequest({
    status: 'Approved',
    statusHistory: [
      { status: 'Pending', at: '2026-08-30T09:00:00.000Z', by: 'a1b2c3d4-...' },
      { status: 'Approved', at: '2026-08-30T14:00:00.000Z', by: 'approver-oid' },
    ],
  });
  const { html } = buildDecisionEmail(request);
  assert.doesNotMatch(html, /Note from the approver/);
});
