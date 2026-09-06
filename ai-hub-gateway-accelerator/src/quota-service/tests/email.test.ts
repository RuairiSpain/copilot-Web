import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { _resetTransporterCacheForTests, getTransporter, sendNotificationEmail } from '../src/lib/email';

const ENV_KEYS = ['Smtp_Host', 'Smtp_Port', 'Smtp_User', 'Smtp_Password', 'Smtp_FromAddress'] as const;
const originalEnv: Record<string, string | undefined> = {};
for (const key of ENV_KEYS) {
  originalEnv[key] = process.env[key];
}

function clearSmtpEnv() {
  for (const key of ENV_KEYS) {
    delete process.env[key];
  }
}

beforeEach(() => {
  clearSmtpEnv();
  _resetTransporterCacheForTests();
});

test('getTransporter: missing Smtp_Host/User/Password throws a clear error', () => {
  assert.throws(() => getTransporter(), /Smtp_Host.*Smtp_User.*Smtp_Password/);
});

test('getTransporter: constructs cleanly with valid config (construction only — no connection attempted)', () => {
  process.env.Smtp_Host = 'smtp.example.com';
  process.env.Smtp_Port = '587';
  process.env.Smtp_User = 'user@example.com';
  process.env.Smtp_Password = 'a-password';
  const transporter = getTransporter();
  assert.ok(transporter);
  assert.equal(typeof transporter.sendMail, 'function');
});

test('getTransporter: derives secure=true for port 465, secure=false otherwise', () => {
  process.env.Smtp_Host = 'smtp.example.com';
  process.env.Smtp_User = 'user@example.com';
  process.env.Smtp_Password = 'a-password';

  process.env.Smtp_Port = '465';
  _resetTransporterCacheForTests();
  const secureTransporter = getTransporter();
  assert.equal((secureTransporter.options as { secure?: boolean }).secure, true);

  process.env.Smtp_Port = '587';
  _resetTransporterCacheForTests();
  const insecureTransporter = getTransporter();
  assert.equal((insecureTransporter.options as { secure?: boolean }).secure, false);
});

test('sendNotificationEmail: missing Smtp_FromAddress throws before even needing a transporter', async () => {
  // Deliberately no Smtp_Host/User/Password set either — if getTransporter()
  // were reached first, we'd get the wrong error message. Confirms the
  // From-address check runs first.
  await assert.rejects(() => sendNotificationEmail({ to: 'x@example.com', subject: 's', html: '<p>h</p>' }), /Smtp_FromAddress/);
});

test('sendNotificationEmail: happy path — calls the injected transporter with the right envelope, no real network involved', async () => {
  process.env.Smtp_FromAddress = 'notifications@example.com';
  let capturedArgs: unknown;
  await sendNotificationEmail(
    { to: 'requester@example.com', subject: 'Test subject', html: '<p>Test body</p>' },
    {
      getTransporter: () => ({
        sendMail: async (args: unknown) => {
          capturedArgs = args;
          return {} as never;
        },
      }),
    }
  );
  assert.deepEqual(capturedArgs, {
    from: 'notifications@example.com',
    to: 'requester@example.com',
    subject: 'Test subject',
    html: '<p>Test body</p>',
  });
});

test('sendNotificationEmail: a send failure propagates (callers decide how to respond — see sendQuotaNotificationEmail.ts)', async () => {
  process.env.Smtp_FromAddress = 'notifications@example.com';
  await assert.rejects(
    () =>
      sendNotificationEmail(
        { to: 'x@example.com', subject: 's', html: '<p>h</p>' },
        {
          getTransporter: () => ({
            sendMail: async () => {
              throw new Error('SMTP connection refused');
            },
          }),
        }
      ),
    /SMTP connection refused/
  );
});
