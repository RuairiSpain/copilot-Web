# quota-service

The real build of the design in `guides/quota-override-approval.md` — a
per-user/per-team dynamic sub-quota, sitting **underneath** the access
contract's own existing, unchanged, GitOps-owned `token-quota` ceiling
(that ceiling is a hard backstop this service can never exceed, not
something it manages).

Same shape as `src/pricing-service/` on purpose (Node.js v4 programming
model, TypeScript, system-assigned managed identity, no keys/connection
strings) — a sibling Function App, not a modification to pricing-service,
since the two are unrelated concerns that happen to share infrastructure
patterns.

## Functions

| Function | Route | Trigger | Called by |
| --- | --- | --- | --- |
| `getQuotaAllowance` | `getQuotaAllowance` (default) | HTTP | The new `frag-load-quota-allowance.xml` APIM policy fragment, on a cache miss only (APIM's own cache holds the result for 300s — see `guides/quota-override-approval.md` §4). On the synchronous request path: one Cosmos point read, nothing else. Internal only — function-key auth, unchanged. |
| `submitQuotaRequest` | `submit` | HTTP | The **Quota Override API** in APIM (`bicep/infra/modules/quota-service/quota-api.bicep`) — via the Power Apps custom connector, `power-platform/quota-connector/`. **No longer callable on the function key alone**: requires the `x-verified-oid` header the API's policy sets from a validated JWT (see "Authorization" below). Enforces the "one pending request per scope" guardrail and flags `requiresEscalation`. |
| `listPendingQuotaRequests` | `pending` | HTTP | Two callers, same route: the internal `quota-approval-notification` Logic App (function key, once per recurrence tick, only returns requests with no `notifiedAt` yet) **and** the Quota Override API's `GET /pending` (JWT + `Quota.Approve` role) for the "Approve/Deny Requests" canvas app screen. |
| `markQuotaRequestNotified` | `markQuotaRequestNotified` (default) | HTTP | The Logic App, right after its webhook post succeeds — an idempotent operational marker (not a `statusHistory` event), so a failed notification naturally gets retried on the next tick instead of being silently dropped. Internal only. |
| `decideQuotaRequest` | `decide` | HTTP | The Quota Override API's `POST /decide` (JWT + `Quota.Approve` role) — via the custom connector's "Approve/Deny Requests" screen. **No longer callable on the function key alone** — same `x-verified-oid` requirement as `submitQuotaRequest`. The one place in the whole flow that requires a genuine human decision. |
| `expireQuotaOverrides` | timer (n/a) | Timer, daily (`0 5 3 * * *`) | Nothing external — sweeps `quota-overrides` for anything past `expiresAt` and deletes it. Deleting the document **is** the revert to baseline; there's no separate "restore" step, because `getQuotaAllowance`'s fallback logic already treats "no override" and "expired override" identically. |
| `resetMonthlyQuotaOverrides` | timer (n/a) | Timer, monthly (`0 10 3 1 * *`) | Nothing external — see "Monthly reset" below. Clears every temporary override at the calendar-month boundary regardless of its own `expiresAt`; permanent overrides survive unless `QuotaOverride_MonthlyResetIncludesPermanent=true`. |
| `sendQuotaNotificationEmail` | `sendEmail` | HTTP | The Logic App, for both notification types — see "Email notifications" below. Content-agnostic: the caller says who and which template, this builds the actual email server-side from `emailTemplates.ts`. Internal only. |
| `listRecentlyDecidedQuotaRequests` | `decided` | HTTP | The Logic App's decided-requests loop, once per recurrence tick. Only returns Approved/Denied requests with a `requestedByEmail` and no `requesterNotifiedAt` yet. Internal only. |
| `markRequesterNotified` | `markDecidedNotified` | HTTP | The Logic App, right after the decision email succeeds — mirrors `markQuotaRequestNotified` exactly, for the other half of the notification lifecycle. Internal only. |

## Monthly reset — two different things, easy to conflate

**Usage resets automatically already — no code involved, nothing new
here.** Both tier-1 and tier-2 `llm-token-limit` are configured
`token-quota-period="Monthly"`; APIM manages that period boundary
internally. If "quota usage back to zero for all users" means the
token-count-so-far, that already happens every month on its own.

**What doesn't reset on its own: the override GRANT.** A budget holder's
`quota-overrides` document (the elevated ceiling itself, e.g. "300k
instead of 100k") persists until its own `expiresAt` — permanent grants
(`expiresAt: null`) never revert at all without this. `resetMonthlyQuotaOverrides`
is the piece that was actually missing: on the 1st of each month, every
temporary override is cleared outright (not just ones whose individual
window happened to end), so nobody quietly carries an elevated allowance
into a new budget cycle without asking again. Permanent overrides survive
by default — see `survivesMonthlyReset()`'s doc comment in
`quotaLogic.ts` for why silently downgrading a "permanent" grant every
month would defeat the point of calling it permanent; set
`QuotaOverride_MonthlyResetIncludesPermanent=true` if a genuine
no-exceptions reset is what you actually want.

## Email notifications

Two separate notifications, sent via SMTP (`nodemailer` — no new Azure
resource to provision; works with Exchange Online SMTP AUTH, a
SendGrid/Mailgun relay, or anything else your org already has, same
"point this at whatever you use" philosophy as the existing generic
webhook design):

1. **A new request was submitted** → emailed to `QuotaApproval_ApproverEmail`
   (a Logic App setting, one fixed address/distribution list — this
   stays un-personalized regardless of the claim-based approver-resolution
   now built for team-scoped decisions; see "Authorization" below). Runs
   alongside the existing generic
   webhook notification, not instead of it — both fire if both are
   configured; the email step is skipped cleanly (no error, no retry
   noise) if `QuotaApproval_ApproverEmail` is blank.
2. **A request was decided** → emailed to the original requester, at the
   email address captured from their own token at submission time
   (`requestedByEmail` — see "Authorization" below for exactly which
   claim). A request whose submitter's token carried none of
   `preferred_username`/`upn`/`email` simply has no address to notify —
   handled explicitly (`listRecentlyDecidedQuotaRequests.ts` excludes
   it), not guessed at.

Both share one content-agnostic function (`sendQuotaNotificationEmail`)
and one pair of pure, unit-tested template builders
(`src/lib/emailTemplates.ts` — `buildRequestCreatedEmail`/
`buildDecisionEmail`), so the actual subject/HTML logic lives in exactly
one place, tested, not duplicated into Logic App expressions.

**A real secret, unlike everything else in this service.** SMTP needs a
password — the one credential in `quota-service` that can't be turned
into managed-identity-only access the way Cosmos already is. Wired as a
Key Vault reference app setting (`quota-service.bicep`'s
`smtpPasswordKeyVaultSecretUri` param), never plaintext — see
`guides/enterprise-hardening-checklist.md` for this noted as a residual,
accepted tradeoff, and for the Azure Communication Services Email
alternative (fully managed-identity, no password, but a new Azure
resource to provision) if your org would rather not manage an SMTP
credential at all — `src/lib/email.ts`'s own comment explains exactly
what to swap.

## Authorization (fork addition — closes the gap flagged in `guides/enterprise-hardening-checklist.md` §1)

`submitQuotaRequest` and `decideQuotaRequest` used to trust whatever
`requestedBy`/`decidedBy` string the caller's JSON body contained — the
function key alone was enough to submit or decide "as" anyone. That's
fixed now, but **only when called through the new Quota Override API**,
not by calling these two functions directly:

1. `bicep/infra/modules/quota-service/quota-api.bicep` puts a standalone
   APIM API in front of just these two endpoints (plus `/pending`),
   requiring a validated Entra ID JWT on every call, and the
   `Quota.Approve` app role specifically for `/decide` and `/pending`.
2. Its policy (`quota-api-policy.xml`) then sets `x-verified-oid` from
   the **validated** token's `oid` claim — overriding, never trusting,
   anything the client sent — before forwarding to this service.
3. `resolveVerifiedIdentity()` (`src/lib/quotaLogic.ts`, unit-tested)
   requires that header — `submitQuotaRequest`/`decideQuotaRequest`
   return `401` without it, with no fallback to a body field. There is
   deliberately no way to call either endpoint "as" someone else anymore.
4. **Approver-resolution, claim-based**: the same policy also stamps a
   **verified** `x-verified-department` header from the JWT's
   `department` claim, independently re-verified the same way `oid` is
   (`tokenValidation.ts`/`requestAuth.ts`). `decideQuotaRequest.ts`
   rejects (403) a `team`-scoped decision whose `scopeId` doesn't equal
   the approver's own verified department
   (`approverAuthorizedForScope()`, unit-tested) — an approver with no
   department claim at all can never decide a team-scoped request.

**What this still does NOT close**: `user`-scoped requests remain
open to any `Quota.Approve` holder — there's no org-hierarchy/manager
data source anywhere in this fork to resolve "the right approver" for an
individual, and closing that would need the separate reference-table
approach flagged in "What's genuinely NOT built here" below, not
something claim-based matching alone can do.

The Entra app role itself is provisioned by
`bicep/infra/entra-id-setup/setup.ps1` (`Quota.Approve`, alongside the
four existing gateway roles) — assign it to whoever should be able to
approve quota increases.

### Impersonation defense-in-depth — closing the direct-call bypass

The design above has one real residual hole, flagged honestly rather
than glossed over: `x-verified-oid` is a **plain HTTP header** once it
leaves APIM. Anyone who has (or leaks) `quota-service`'s function key
can call `submitQuotaRequest`/`decideQuotaRequest` **directly**,
bypassing APIM and its JWT/role check entirely, and just set that header
to whatever oid they want — the function key proves "this is a caller
with the key," not "this call actually went through APIM's real
validation."

Fixed with `src/lib/tokenValidation.ts` + `src/lib/requestAuth.ts`: both
endpoints now **independently re-validate the caller's real bearer
token** (fetching the tenant's JWKS via the standard OIDC discovery
document — `jose`'s `createRemoteJWKSet`/`jwtVerify`, checking signature/
issuer/audience/expiry) and cross-check that token's own `oid` claim
against the `x-verified-oid` header before trusting either
(`identityMatchesToken()`, unit-tested — a mismatch is exactly what
catches a spoofed header). **This is deliberately not "minting our own
tokens"** — quota-service never signs or issues anything of its own;
self-issued tokens would mean owning key management, rotation, and
revocation that Entra ID already does correctly, trading one problem for
a worse one. It only re-verifies the *same* real Entra ID token the
original caller presented, which APIM already forwards unless something
explicitly strips it (nothing here does).

**Fail-closed, deliberately** — the opposite of `getQuotaAllowance`'s
fail-open design. A JWKS fetch failure, an expired token, an audience
mismatch: all reject the request. There's no equivalent safety net here
the way tier-1 quota backstops a `getQuotaAllowance` outage — if this
can't verify who's calling, letting the call through anyway is exactly
the hole it exists to close.

**Breaking change on upgrade, stated plainly**: `QuotaOverride_RequireTokenRevalidation`
defaults to `true`. Once deployed, `/submit` and `/decide` reject every
call until `Entra_TenantId`/`Entra_Audience`/`Entra_OpenIdConfigUrl` are
configured — set the flag to `false` only for local development against
a function key with no real tenant; doing so in a real deployment
silently reopens the exact gap this closes, and logs a loud warning
every time it's bypassed so that can't happen unnoticed.

### The identity re-check above was not the whole fix — role re-check, added later

A security review of this fork found that the section above only ever
closed half the direct-call bypass. Re-validating the caller's real
bearer token and matching its `oid` against `x-verified-oid` proves the
caller is a **genuine, authenticated person** — it never checked that
person actually held the `Quota.Approve` role. `Quota.Approve` was
required *only* by APIM's `quota-api-policy.xml`; nothing downstream in
this service ever re-derived or checked the token's `roles` claim. The
practical consequence: anyone who obtained the function key and held
**any** valid token for this app registration — any authenticated
employee, no special role needed — could call `/decide` or `/pending`
directly, skip APIM's role gate entirely, and approve/deny requests (or
list every pending one) as themselves.

Fixed the same way the oid/department checks already work:
`verifyBearerTokenClaims()` (`tokenValidation.ts`) now also extracts and
returns the token's `roles` claim (a JSON array of app-role strings on a
real Entra ID token, not the comma-separated string shape APIM's own
policy-expression `jwt-roles` variable uses — extracted and normalized
here, not assumed). `corroborateIdentity()` (`requestAuth.ts`) takes an
optional `requiredRole` parameter and rejects (403) when the
independently re-verified token doesn't carry it.
`decideQuotaRequest.ts` passes `'Quota.Approve'` unconditionally;
`listPendingQuotaRequests.ts` passes it only when `x-verified-oid` is
present — that header distinguishes a call that came through the
external Quota Override API (which always sets it) from the internal
`quota-approval-notification` Logic App's function-key-only poll
(which never does, and still needs unauthenticated internal access to
do its job). `submitQuotaRequest.ts` is deliberately unaffected: `/submit`
has no role requirement by design, any authenticated employee can
request quota for themselves. All new behavior is 100%-line-covered by
new unit tests in `tests/tokenValidation.test.ts`,
`tests/requestAuth.test.ts`, `tests/decideQuotaRequest.test.ts`, and
`tests/listPendingQuotaRequests.test.ts`.

## What's genuinely tested here (not just built)

Every I/O boundary in this service (Cosmos reads/writes, SMTP send, JWKS
fetch/JWT verification) is now reached through a small, optional,
defaulted `deps` parameter on each handler/function — production call
sites (`app.http`/`app.timer`) never pass a third argument, so real
behavior is byte-for-byte unchanged, but tests can inject fakes instead
of needing a live Cosmos account, SMTP server, or Entra tenant. Every
`src/lib/*.ts` file and every `src/functions/*.ts` file now has a
matching `tests/*.test.ts` file — **149 tests total, run and passing in
this session** (`npm install && npm run build && npm test`, verified
clean, not just asserted; grew from 132 with the `Quota.Approve`
role-re-check fix above).

The `tokenValidation.ts` tests are worth calling out specifically: they
use `jose`'s own `generateKeyPair`/`SignJWT`/`exportJWK` to build real,
genuinely signed test tokens, and a real local `http.Server` (Node's
`http.createServer`, not a mocked `fetch` — `jose`'s Node runtime fetches
JWKS via `node:http`/`node:https` directly, confirmed by reading its
bundled source, so mocking `globalThis.fetch` would not have actually
intercepted anything) to serve a real discovery document and JWKS. This
exercises the actual signature/issuer/audience/expiry verification logic
against a spec-compliant OIDC provider — not stubbed-out shortcuts —
covering: valid token accepted, expired/wrong-audience/wrong-issuer/
tampered-signature/missing-oid tokens all rejected, and discovery-fetch
failure handled. Real Entra ID tenant behavior itself remains unverified
in this sandbox (no live tenant available) — this confirms the code's own
logic is correct against the OIDC spec, not that Microsoft's Entra ID
endpoints behave exactly as modeled here.

**Real, measured coverage** (`node --test --experimental-test-coverage
dist/tests/*.js`, run in this session, not estimated):

| Scope | Line | Branch | Function |
| --- | --- | --- | --- |
| Every file under `src/` | **100.00%** | 97–100% (per file) | 100% (except one file's un-invoked production-only default, see below) |
| All files (incl. test files themselves) | **98.84%** | **93.67%** | **93.98%** |

Every single `src/**/*.ts` file is at 100% line coverage. The only
residual gap worth naming: `submitQuotaRequest.js` reports 50% function
coverage — its `defaultDeps.newRequestId: () => \`req-${randomUUID()}\``
real-ID generator is never invoked directly (every test injects a fixed
id via `deps`, deliberately, so test runs are deterministic), the same
"thin, un-faked edge" residual flagged as an accepted possibility when
this coverage effort was planned. `pricing-service` still has **zero**
test files at all — 0% automated coverage, compile-only verification, a
real gap this fork hasn't closed (not asked to, but worth knowing if
anyone assumes otherwise from `quota-service`'s numbers).

The I/O-touching code now has real, executed test coverage (not just a
clean `npm run build`) but was still **not** exercised against a real
Cosmos account, SMTP server, Entra tenant, or a running Functions host in
this session — the fakes and the local JWKS server are a faithful,
scoped substitute for that, not a replacement for integration testing
against live services. `pricing-service` remains at the older,
compile-only honesty tier.

## What's genuinely NOT built here

- **Individual-request approver-resolution.** Team-scoped requests are
  now department-gated (see "Authorization" above — claim-based, no new
  reference table). `user`-scoped requests are not: the `Quota.Approve`
  role alone still proves the caller is *a* real approver, not that
  they're *the right one* for a specific individual. Closing that would
  need a `Budgets`/`AuthorizedCostCenters`-shaped reference table (or an
  org-hierarchy/manager data source) this fork deliberately didn't add —
  until it exists, any `Quota.Approve` role holder can still decide any
  pending *individually*-scoped request, platform-wide.
- **Managed-identity/Entra auth between APIM and this Function App —
  partially addressed.** `authLevel: 'function'` (a function key) is
  still what actually reaches every endpoint here; that hasn't changed.
  What *has* changed: `submitQuotaRequest`/`decideQuotaRequest` no
  longer trust the function key's presence as proof of anything by
  itself — see "Impersonation defense-in-depth" above. The other seven
  endpoints (`getQuotaAllowance`, the notification-pipeline ones,
  `expireQuotaOverrides`, `resetMonthlyQuotaOverrides`) are unaffected —
  legitimately service-to-service calls with no end-user identity
  concept, where Easy Auth/managed-identity-to-managed-identity remains
  the documented, not-yet-built upgrade path (see
  `guides/enterprise-hardening-checklist.md`).
- **`Approve/Deny Requests`'s gallery isn't filtered to the signed-in
  approver's own department** — same root cause as the first bullet.
  `power-platform/quota-connector/canvas-app/` shows every pending
  request to anyone holding the role, and says so in its own README
  rather than pretending otherwise.

## Configuration

See `local.settings.json.example`. `CosmosDB_Endpoint` /
`CosmosDB_Database` / the two container name settings point this service
at the same Cosmos account `pricing-service` already uses (two new
containers, `quota-overrides` and `quota-override-requests` — see
`bicep/infra/modules/cosmos-db/cosmos-db.bicep`).
`QuotaOverride_DefaultDurationDays` (default 30) and
`QuotaOverride_EscalationMultiplier` (default 3) implement §7's
"temporary by default" and "escalate past a multiplier" guardrails.

The **Logic App** (`quota-approval-notification`, deployed through this
accelerator's existing Logic App module — the same one hosting
`llm-usage-ingestion` and its siblings, not a new hosting model) needs
**its own** app settings, separate from this Function App's:
`QuotaService_ListPendingUrl` / `QuotaService_MarkNotifiedUrl` /
`QuotaService_ListDecidedUrl` / `QuotaService_MarkDecidedNotifiedUrl` /
`QuotaService_SendEmailUrl` (this service's `listPendingQuotaRequests` /
`markQuotaRequestNotified` / `listRecentlyDecidedQuotaRequests` /
`markRequesterNotified` / `sendQuotaNotificationEmail` URLs, respectively
— note the URLs are the *routes*: `.../api/pending`, `.../api/decided`,
etc., not the function names — see the "Functions" table's Route
column), `QuotaService_FunctionKey` (shared by all five calls),
`QuotaApproval_NotificationWebhookUrl` (your own Teams/ServiceNow
webhook, unrelated to email — **configure this before deploying the
Logic App**, not after: left unset, the notification step fails every
5-minute tick on a bad URI rather than silently skipping, which is
deliberate — see the Logic App's own `Notify_Approver` action — but
means an unconfigured deployment is loudly broken, not quietly inert),
and `QuotaApproval_ApproverEmail` (optional — blank cleanly skips the
email-to-approver step instead of failing, unlike the webhook above,
since email is additive to the webhook rather than a required channel).

The **APIM policy fragments** need two named values of their own:
`{{quota-service-url}}` (this service's `getQuotaAllowance` URL) and
`{{quota-service-function-key}}` — see
`bicep/infra/modules/apim/policies/frag-load-quota-allowance.xml`.
`{{quota-service-function-key}}` is reused by
`quota-api-policy.xml` too (same key, one named value, two consumers).

## Deployment

`bicep/infra/modules/quota-service/quota-service.bicep`, modeled directly
on `pricing-service.bicep`'s identity + RBAC block, with one difference:
the RBAC role assignment is **Cosmos DB Built-in Data Contributor**
(native Cosmos SQL role, not a standard Azure `roleDefinition`), scoped
via its `scope` property to just the two new containers — see the
module's comments for the exact resource shape and why it's not the same
`Microsoft.Authorization/roleAssignments` pattern `pricing-service.bicep`
uses for its Storage grant. Same "wiring into your main orchestration is
left to you" note as `pricing-service` — this module is deployable
standalone.

**Also deploy** `bicep/infra/modules/quota-service/quota-api.bicep` — the
authorization-gated APIM front door (see "Authorization" above), and run
`bicep/infra/entra-id-setup/setup.ps1` again to provision the new
`Quota.Approve` role (idempotent — it only adds what's missing, per the
script's own existing "add missing app role(s)" logic). Then see
`power-platform/quota-connector/README.md` for wiring the Power Apps side
up to it.
