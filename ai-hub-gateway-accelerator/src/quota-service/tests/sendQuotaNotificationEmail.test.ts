import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sendQuotaNotificationEmail } from '../src/functions/sendQuotaNotificationEmail';
import { makeFakeContext, makeFakeRequest } from './helpers/fakeHttpRequest';

function baseRequest(overrides: Record<string, unknown> = {}) {
  return {
    id: 'req-1',
    subscriptionId: 'sub-1',
    docType: 'quotaOverrideRequest',
    scopeType: 'user',
    scopeId: 'oid-abc',
    requestedBy: 'oid-abc',
    currentQuota: 100000,
    requestedQuota: 250000,
    reason: 'reason',
    durationDays: 30,
    status: 'Pending',
    requiresEscalation: false,
    statusHistory: [],
    createdAt: '2026-08-30T00:00:00.000Z',
    ...overrides,
  };
}

test('sendQuotaNotificationEmail: malformed JSON body — 400', async () => {
  const request = makeFakeRequest({ bodyThrows: true });
  const context = makeFakeContext();
  const res = await sendQuotaNotificationEmail(request, context, { sendNotificationEmail: async () => {} });
  assert.equal(res.status, 400);
});

test('sendQuotaNotificationEmail: missing "to" — 400', async () => {
  const request = makeFakeRequest({ body: { templateType: 'requestCreated', request: baseRequest() } });
  const context = makeFakeContext();
  const res = await sendQuotaNotificationEmail(request, context, { sendNotificationEmail: async () => {} });
  assert.equal(res.status, 400);
});

test('sendQuotaNotificationEmail: unknown templateType — 400', async () => {
  const request = makeFakeRequest({ body: { to: 'a@b.com', templateType: 'bogus', request: baseRequest() } });
  const context = makeFakeContext();
  const res = await sendQuotaNotificationEmail(request, context, { sendNotificationEmail: async () => {} });
  assert.equal(res.status, 400);
});

test('sendQuotaNotificationEmail: missing request document — 400', async () => {
  const request = makeFakeRequest({ body: { to: 'a@b.com', templateType: 'requestCreated' } });
  const context = makeFakeContext();
  const res = await sendQuotaNotificationEmail(request, context, { sendNotificationEmail: async () => {} });
  assert.equal(res.status, 400);
});

test('sendQuotaNotificationEmail: injected send failure — 502, error logged', async () => {
  const request = makeFakeRequest({ body: { to: 'approver@example.com', templateType: 'requestCreated', request: baseRequest() } });
  const context = makeFakeContext();
  const res = await sendQuotaNotificationEmail(request, context, {
    sendNotificationEmail: async () => {
      throw new Error('SMTP unreachable');
    },
  });
  assert.equal(res.status, 502);
  assert.equal(context.errors.length, 1);
});

test('sendQuotaNotificationEmail: requestCreated happy path — 202, sends the requestCreated content', async () => {
  const req = baseRequest();
  const request = makeFakeRequest({ body: { to: 'approver@example.com', templateType: 'requestCreated', request: req } });
  const context = makeFakeContext();
  let sentArgs: { to: string; subject: string; html: string } | undefined;
  const res = await sendQuotaNotificationEmail(request, context, {
    sendNotificationEmail: async (email) => {
      sentArgs = email;
    },
  });
  assert.equal(res.status, 202);
  assert.equal(sentArgs?.to, 'approver@example.com');
  assert.match(sentArgs?.subject ?? '', /New request pending/);
});

test('sendQuotaNotificationEmail: decision happy path — 202, sends the decision content', async () => {
  const req = baseRequest({ status: 'Approved', statusHistory: [{ status: 'Approved', at: '2026-08-30T05:00:00.000Z', by: 'approver-oid' }] });
  const request = makeFakeRequest({ body: { to: 'requester@example.com', templateType: 'decision', request: req } });
  const context = makeFakeContext();
  let sentArgs: { to: string; subject: string; html: string } | undefined;
  const res = await sendQuotaNotificationEmail(request, context, {
    sendNotificationEmail: async (email) => {
      sentArgs = email;
    },
  });
  assert.equal(res.status, 202);
  assert.equal(sentArgs?.to, 'requester@example.com');
  assert.match(sentArgs?.subject ?? '', /approved/);
});
