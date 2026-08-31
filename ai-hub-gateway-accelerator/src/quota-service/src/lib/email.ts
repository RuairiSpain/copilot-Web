import nodemailer, { Transporter } from 'nodemailer';

/**
 * SMTP, not Azure Communication Services or Microsoft Graph sendMail —
 * a deliberate choice, not a default reached for without thinking:
 *
 *   - No new Azure resource to provision (ACS Email needs a Communication
 *     Services resource + a verified sender domain — real setup this
 *     fork doesn't ask you to take on just to get notifications working).
 *   - Works with whatever your org already has: Exchange Online SMTP AUTH
 *     client submission, a SendGrid/Mailgun SMTP relay, an on-prem relay
 *     — same "point this at whatever you already use" philosophy already
 *     applied to the generic QuotaApproval_NotificationWebhookUrl design.
 *   - nodemailer is a mature, widely-used library — low risk to depend on.
 *
 * The real tradeoff, stated plainly: SMTP needs a password, which is a
 * secret this design can't turn into a managed-identity-only credential
 * the way Cosmos/Storage access already is elsewhere in this service —
 * see quota-service.bicep's Smtp_Password param, which is wired as a
 * Key Vault reference app setting rather than a plaintext one, and
 * guides/enterprise-hardening-checklist.md for this noted as a residual
 * secret-management item. If your org has Azure Communication Services
 * already set up and would rather use managed identity end to end,
 * swap getTransporter()/sendNotificationEmail() below for
 * @azure/communication-email's EmailClient(endpoint, credential) — the
 * two call sites in sendQuotaNotificationEmail.ts don't need to change,
 * only this file would.
 */

let cachedTransporter: Transporter | undefined;

/**
 * Exported (not just internal) so tests/email.test.ts can exercise this
 * construction logic directly — missing-env-var errors, the `secure`
 * flag derived from port 465 — without triggering a real network
 * connection: nodemailer's createTransport(), like @azure/cosmos's
 * CosmosClient and jose's createRemoteJWKSet, is lazy and doesn't
 * connect until a send/verify call actually happens.
 */
export function getTransporter(): Transporter {
  if (!cachedTransporter) {
    const host = process.env.Smtp_Host;
    const port = Number(process.env.Smtp_Port ?? '587');
    const user = process.env.Smtp_User;
    const pass = process.env.Smtp_Password;
    if (!host || !user || !pass) {
      throw new Error('Smtp_Host / Smtp_User / Smtp_Password app settings are required to send email');
    }
    cachedTransporter = nodemailer.createTransport({
      host,
      port,
      secure: port === 465,
      auth: { user, pass },
    });
  }
  return cachedTransporter;
}

export interface NotificationEmail {
  to: string;
  subject: string;
  html: string;
}

export interface SendNotificationEmailDeps {
  getTransporter: () => Pick<Transporter, 'sendMail'>;
}

const defaultDeps: SendNotificationEmailDeps = {
  getTransporter,
};

export async function sendNotificationEmail(email: NotificationEmail, deps: SendNotificationEmailDeps = defaultDeps): Promise<void> {
  const from = process.env.Smtp_FromAddress;
  if (!from) {
    throw new Error('Smtp_FromAddress app setting is required to send email');
  }
  await deps.getTransporter().sendMail({
    from,
    to: email.to,
    subject: email.subject,
    html: email.html,
  });
}

/**
 * TEST-ONLY. Clears the cached transporter so tests.email.test.ts can
 * exercise getTransporter()'s own env-var-driven construction logic
 * (missing-setting errors, the `secure` flag derived from port 465)
 * repeatedly with different env values, instead of getting the first
 * test's cached instance forever.
 */
export function _resetTransporterCacheForTests(): void {
  cachedTransporter = undefined;
}
