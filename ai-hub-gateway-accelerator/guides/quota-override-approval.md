# Quota Override & Approval Workflow (design only — not implemented)

## The question this answers

`guides/agent-hierarchy-attribution.md` and the plan-level gap analysis
behind it already established that the accelerator's quota enforcement is
**subscription-keyed only** — one shared bucket per access-contract
(use case), with no per-team or per-individual-user limit, and no
self-service path when a scope runs out. This document designs the
missing piece: a **two-tier quota model** (a static, GitOps-owned
contract ceiling, plus a dynamic, self-service sub-quota underneath it)
and the **request → approve → take-effect** workflow around it — the real
build behind your original PRD's FR-010 (User Quota Overrides) and FR-012
(Approval Workflow).

Same status as `advanced-ptu-routing.md` and `agent-hierarchy-attribution.md`
before their code samples were added: a design proposal, not a policy or
Bicep change yet. Nothing here alters `citadel-access-contracts/`.

## Why this doesn't work today

Verified directly against the policy source, not the PRD's aspirational
language:

- Every `llm-token-limit` / `rate-limit-by-key` / `quota-by-key` in the
  repo is `counter-key="@(context.Subscription.Id)"` (or a variant with a
  fixed suffix like `-default`/`:tool`) — one bucket per access contract,
  shared by everyone using that contract's key.
- The `user-id` variable the `security-handler` fragment extracts from a
  JWT's `azp` claim (`citadel-access-contracts-policy.md`) is used only
  for the debug `UAIG-User-Id` header and usage-log dimensions
  (`customDimension1/2`) — never as a quota counter-key.
- `quota-exceeded` (HTTP `403 AITokenQuotaExceeded`) is an alert category
  that's **on by default** (`throttling-events-handling.md`) — but it
  notifies ops/Application Insights, not the blocked caller, and there is
  no request-more mechanism anywhere in the repo. Raising a limit today
  means a human editing `token-quota` in a `.bicepparam`/policy XML and
  redeploying.

## What exists today that this design reuses, not reinvents

Three real patterns already in the codebase make this buildable without
inventing new infrastructure:

1. **`token-quota`, `token-quota-period`, and `counter-key` on
   `llm-token-limit` all accept APIM policy expressions** (Microsoft's own
   policy reference — confirmed from the published docs source, not
   independently exercised against a live APIM instance, flagged in
   "What's still unresolved" below). `tokens-per-minute` does **not**
   document expression support — it's evaluated as a literal. This
   asymmetry drives the two-tier design below: the **long-term quota** can
   be made dynamic per user/team; the **burst rate** stays a small, fixed
   number of contract-level tiers.
2. **The lazy-cache-load pattern already shipped** in
   `bicep/infra/modules/apim/policies/openai_api_policy_dynamic_throttling.xml`
   — `cache-lookup-value` on a computed key, and on miss, compute/fetch
   and `cache-store-value` with a TTL — is exactly the mechanism needed to
   resolve "what's this scope's current effective quota" without an
   external call on every request. Same pattern the earlier GitHub
   precedent research already recommended reusing for backend/version
   metadata (`metadata-config-v{version}`, TTL 300s) — APIM's own
   distributed cache, no Redis dependency for this feature either.
3. **JWT claim extraction for a business identity already has a working
   snippet** — `citadel-access-contracts-policy.md`'s "Source from a
   validated JWT claim" example pulls a `department` claim off the
   validated token. Individual-user scope reuses the same mechanism with
   `oid` instead (same claim `agent-hierarchy-attribution.md` section 4
   already establishes as the correct, non-pairwise-pseudonymous choice
   for a stable per-user key).

## Proposed design

### 1. Two-tier enforcement — contract ceiling (static) + scope sub-quota (dynamic)

Two independent `llm-token-limit` elements fire in the same product
policy `inbound` section:

```xml
<!-- Tier 1 — unchanged: the contract's own GitOps-owned ceiling.
     No override can ever cause a scope to exceed this. -->
<llm-token-limit counter-key="@(context.Subscription.Id)"
    tokens-per-minute="10000"
    token-quota="1000000"
    token-quota-period="Monthly" />

<!-- Tier 2 — new: per-user or per-team sub-quota, resolved dynamically. -->
<include-fragment fragment-id="resolve-quota-scope" />
<cache-lookup-value key="@((string)context.Variables["quotaScopeCacheKey"])"
    variable-name="resolvedQuota" />
<choose>
    <when condition="@(!context.Variables.ContainsKey("resolvedQuota"))">
        <include-fragment fragment-id="load-and-cache-quota-allowance" />
    </when>
</choose>
<llm-token-limit counter-key="@((string)context.Variables["quotaScopeCacheKey"])"
    tokens-per-minute="@((int)context.Variables["resolvedTpmTier"])"
    token-quota="@((int)((JObject)context.Variables["resolvedQuota"])["effectiveQuota"])"
    token-quota-period="Monthly" />
```

Both limits are evaluated by APIM independently; a request is throttled
by whichever is hit first. **The tier-2 quota is a sub-allocation, never
an escape hatch** — nothing in this design lets a scope's effective quota
exceed the contract's own `token-quota`; raising the contract ceiling
itself still goes through the existing GitOps flow.

`resolvedTpmTier` (the per-minute literal) is chosen from a small, fixed
set (e.g. `standard` / `elevated`, 3–4 tiers at most) via a `<choose>` on
the resolved scope's tier, not an arbitrary number — this is the
consequence of `tokens-per-minute` not supporting expressions.

### 2. Scope resolution — individual user or team, most-specific wins

`resolve-quota-scope` (new fragment) determines what `quotaScopeCacheKey`
resolves to, in this precedence order:

1. **Individual override exists for this `oid`** → scope = `user`,
   key = `context.Subscription.Id + "-user-" + oid`.
2. **Else, a team/department override exists** → scope = `team`,
   key = `context.Subscription.Id + "-team-" + departmentClaim`.
3. **Else** → falls through to the contract's own tier-1 quota only
   (tier 2 becomes a no-op by setting its `token-quota` equal to tier 1's,
   so it never binds tighter than the contract already does).

`oid` and `departmentClaim` are extracted from the validated JWT exactly
as `citadel-access-contracts-policy.md` already documents — this design
adds no new authentication mechanism, only a new **use** of claims already
being validated for authorization.

### 3. Data model — two new Cosmos containers, same account as `llm-usage-container`

**`quota-overrides`** — current effective state only, one document per
scope, this is what the runtime lookup reads:

```json
{
  "id": "user-a1b2c3d4-...",
  "scopeType": "user",
  "scopeId": "a1b2c3d4-...",
  "subscriptionId": "LLM-HR-ChatAgent-DEV-SUB-01",
  "baselineQuota": 100000,
  "effectiveQuota": 250000,
  "tpmTier": "elevated",
  "grantedBy": "approver-oid",
  "requestId": "req-...",
  "expiresAt": "2026-09-30T00:00:00Z",
  "updatedAt": "2026-08-31T10:00:00Z"
}
```

Partition key `/subscriptionId` — mirrors how quota is already scoped
(within one access contract), keeps lookups single-partition, and bounds
blast radius to one contract if a document is ever wrong.

**`quota-override-requests`** — append-only audit trail, one document per
request, status updated (never the document replaced) as it moves through
the workflow:

```json
{
  "id": "req-...",
  "scopeType": "user",
  "scopeId": "a1b2c3d4-...",
  "subscriptionId": "LLM-HR-ChatAgent-DEV-SUB-01",
  "requestedBy": "a1b2c3d4-...",
  "currentQuota": 100000,
  "requestedQuota": 250000,
  "reason": "Quarter-end batch reconciliation, temporary",
  "durationDays": 30,
  "status": "Approved",
  "statusHistory": [
    { "status": "Pending", "at": "2026-08-30T09:00:00Z", "by": "a1b2c3d4-..." },
    { "status": "Approved", "at": "2026-08-30T14:12:00Z", "by": "approver-oid" }
  ],
  "createdAt": "2026-08-30T09:00:00Z"
}
```

This is the same "never edit history, append a new fact" principle
`cost-attribution-guide.md` already established for price snapshots
(dated, immutable documents) — applied here to approval decisions instead
of prices, for the same reason: audit integrity for something that will
end up in a compliance review.

### 4. Runtime enforcement flow

1. Request arrives with a valid JWT → `oid`/department already resolved
   by existing `security-handler`.
2. `resolve-quota-scope` picks the cache key (section 2).
3. `cache-lookup-value` — cache hit (the common case, TTL keeps this
   cheap): use the cached `effectiveQuota`/`tpmTier` directly, no external
   call on this request.
4. Cache miss: a single fast lookup — an internal `send-request` to a
   small read-only endpoint (a third function alongside `pricing-service`'s
   existing `enrichPricing`/`refreshPricingCache`, e.g. `getQuotaAllowance`,
   same Function App, same Cosmos account, same managed-identity auth
   pattern) that reads the `quota-overrides` document for that scope, or
   returns the contract baseline if none exists. Result is
   `cache-store-value`'d with a 300s TTL — same value used for the
   metadata-cache pattern the GitHub precedent research already
   recommended elsewhere in this fork.
5. Both `llm-token-limit` elements evaluate; APIM does the actual
   counting internally exactly as it does today for the existing
   subscription-level check — this design changes *what value and key*
   feed the policy, not how APIM enforces it.

An approved override therefore takes effect within **one cache TTL
window (≤5 minutes)**, with **no APIM redeploy** — a materially faster
and lower-risk path than the GitOps flow contract-level quota changes
still require, which is the entire point of separating the two tiers.

### 5. Self-service request flow

When tier-2 (or tier-1) returns `403 AITokenQuotaExceeded`, the response
body is extended with a `requestMoreUrl` (or equivalent structured field)
pointing at a small self-service surface — out of scope for this doc to
design as a UI, but the API shape it needs is simple: `POST` scope,
requested amount, reason, optional duration → writes a `Pending`
`quota-override-requests` document. This can be a thin Function endpoint,
a Teams bot, or a Power Platform form — the workflow below doesn't care
which fronts it, only that it lands as that one document shape.

### 6. Approval flow

1. **Trigger**: Cosmos DB **change feed** on `quota-override-requests`
   (new `Pending` documents) — the same trigger primitive Azure Functions
   already supports natively, no new infrastructure concept for this
   accelerator (which already uses Cosmos + Functions/Logic Apps
   throughout).
2. **Resolve the approver**: look up the scope's budget holder. This
   depends on the small reference table (`Budgets`/`AuthorizedCostCenters`
   -shaped) already flagged as a gap in the plan's gap-analysis follow-up
   — this design assumes that table exists rather than re-solving it
   here; if it doesn't yet, the nearest fallback is the access contract's
   own owner/business-unit field already captured at onboarding time
   (`citadel-access-contracts` naming: `<serviceCode>-<BU>-<UseCase>-<ENV>`).
3. **Notify**: a Teams Adaptive Card (your original PRD's FR-012
   mechanism) is one option, not a requirement of this design — email or
   a Power Platform approval both fit the same trigger→notify→decide
   shape. Nothing in the accelerator today ships Teams integration, so
   this is new either way; pick whichever your org already has approval
   tooling for.
4. **Decision**: approve → write/update the matching `quota-overrides`
   document (section 3) with `effectiveQuota`, `tpmTier`, `expiresAt` (if
   temporary), `grantedBy`; deny → update the request's `statusHistory`
   only, no `quota-overrides` write. Either way, append to
   `statusHistory`, never overwrite it.
5. **Take effect**: nothing further to do — the next cache-miss on that
   scope (within 5 minutes) picks up the new value, per section 4.

### 7. Guardrails

- **One pending request per scope at a time** — a partial-unique check
  (query `quota-override-requests` for an existing `Pending` doc for that
  `scopeType`+`scopeId` before inserting a new one) prevents a blocked
  caller from spamming requests while waiting.
- **Escalation threshold** — an override past some multiplier of baseline
  (e.g. >3×) routes to a second-level approver (platform admin, not just
  the immediate budget holder) rather than being a single-click approval
  — mirrors the "large ask" escalation pattern already used elsewhere in
  this engagement's own review process, applied here to spend risk instead
  of code review risk.
- **Temporary by default** — `durationDays` should default to something
  short (e.g. 30 days) rather than open-ended; a scheduled sweep (same
  Timer-trigger shape as `refreshPricingCache`) finds `quota-overrides`
  documents past `expiresAt` and deletes them, which reverts that scope to
  the contract baseline on its next cache miss — no separate "revert"
  logic needed, expiry is just "stop finding an override."
- **Contract ceiling is the hard backstop** — restated from section 1:
  whatever tier 2 resolves to, it can never let a scope exceed what the
  access contract itself allows. A genuinely larger need means raising
  the contract's own `token-quota`, which stays a deliberate, reviewed
  GitOps change — this design speeds up the common case (someone within
  the contract's existing envelope needs a bigger personal/team slice),
  not the rare case (the whole use case needs more capacity than it was
  ever provisioned for).

### 8. Reporting hook (headline only, not built)

Once this exists, the request/approval history in
`quota-override-requests` is a natural small addition to the Platform
Admin Power BI page already scoped in the plan's gap-analysis follow-up
(request volume, approval rate, time-to-decision, most-requested scopes)
— flagging this as a future headline, not building it now, consistent
with how every other reporting addition in this fork has been sequenced
behind its underlying data existing first.

## Impersonation — why a header alone isn't enough, and why the fix isn't "mint our own tokens"

Sections 4 and 6 above rely on `quota-api-policy.xml` validating the
caller's JWT and stamping `x-verified-oid` onto the forwarded request.
That header is real protection at the point APIM sets it — but once it
leaves APIM, it's a **plain HTTP header**, not a cryptographic proof.
Anyone who has (or leaks) `quota-service`'s function key can call
`submitQuotaRequest`/`decideQuotaRequest` **directly**, skip APIM and
its JWT/role check entirely, and set that header to whatever oid they
like. The function key was only ever meant to prove "this caller is
APIM" — it can't actually enforce that, and nothing before this section
did.

**The fix is not for quota-service to mint/sign its own tokens.**
Rolling your own token issuance is a well-known anti-pattern here: it
means owning key management, rotation, and revocation — exactly what
Entra ID already does correctly — and trades a header-spoofing problem
for a "now we also run our own PKI" problem, a strictly worse trade.

**The actual fix**: `src/lib/tokenValidation.ts` independently
re-validates the **same real Entra ID access token** the original
caller presented (APIM already forwards the original `Authorization`
header to the backend unless something explicitly strips it, and
nothing in this design does) — fetching the tenant's JWKS via standard
OIDC discovery, checking signature/issuer/audience/expiry — and
cross-checks that token's own `oid` claim against `x-verified-oid`
before either is trusted (`identityMatchesToken()` in `quotaLogic.ts`,
unit-tested). A caller with a spoofed header but no genuinely valid,
correctly-scoped Entra token whose oid matches gets rejected. This is
defense-in-depth on top of APIM's own check, not a replacement for it —
both layers exist, and either one alone would leave a gap the other
closes.

**Fail-closed, deliberately** — unlike `getQuotaAllowance`'s fail-open
design (§4). There is no equivalent safety net for an identity check the
way tier-1 quota backstops a `getQuotaAllowance` outage: if this can't
verify who's calling, letting the request through anyway is exactly the
hole it exists to close.

**What this still doesn't close**: scope-specific authorization (is this
verified person the *right* approver for *this* department) — same
already-documented gap, needing the same still-missing reference table.
Cryptographic identity verification and business-rule authorization are
different problems; this section only solves the first one.

## What's still unresolved, on purpose

- **Policy-expression support on `token-quota`/`counter-key`/
  `token-quota-period` is sourced from Microsoft's published policy
  reference, not exercised against a live APIM instance in this session**
  — same honesty caveat as everything else in this fork that couldn't be
  run: verify against a real APIM Premium instance before trusting the
  dynamic-quota policy shown in section 1, particularly whether a
  `JObject` variable can be indexed the way section 1's snippet assumes
  inside a `token-quota` expression, or whether it needs to be flattened
  to a plain number variable first.
- **Multi-region / mirrored-gateway consistency.** The Business Continuity
  & Resiliency guide (`citadel-access-contracts/access-contract-resiliency-guide.md`)
  lets one access contract mirror across multiple APIM gateways sharing
  one subscription key. Each gateway has its **own** internal cache, so an
  approved override could be "live" on one mirrored gateway before
  another's TTL expires — a bounded (≤5 minute), self-healing
  inconsistency given the design here, but worth stating rather than
  assuming away, especially if a shorter TTL or a cache-invalidation push
  is later judged necessary for a specific use case.
- **Approver-resolution table doesn't exist yet** — section 6 explicitly
  depends on the `Budgets`/`AuthorizedCostCenters`-shaped reference data
  already flagged as missing in the plan's gap analysis. This design
  doesn't invent that table; it assumes it gets built (likely alongside
  the Platform Admin reporting page) before the approval flow can resolve
  a real approver automatically.
- **Abuse via many small, sub-threshold requests** — the escalation
  guardrail (section 7) only catches large single asks; a scope that
  files several small approved increases over time could still end up far
  past baseline without ever triggering escalation. Worth a periodic
  admin review of cumulative drift (baseline vs. current effective quota
  per scope) rather than assuming per-request guardrails alone are
  sufficient — flagged here rather than solved, since it's a policy
  question (how much drift is acceptable) more than a technical one.

## Relationship to other fork documents

- **`cost-attribution-guide.md`** — this design's append-only
  `quota-override-requests` audit trail and dated `quota-overrides`
  current-state split follow the exact same "immutable history +
  current-state pointer" shape `pricing-service` already implements for
  price snapshots. Same account, same pattern, different concern.
- **`agent-hierarchy-attribution.md` section 6 (atomic multi-scope quota
  decrement)** — that design assumes a resolved quota ceiling exists to
  decrement against at each scope (User/Department/BU/Org); this document
  is what actually sets and raises that ceiling for the User/Team scopes.
  The two are complementary, not overlapping: section 6 there is about
  *counting down correctly under concurrency*, this document is about
  *what the count starts from and how it can grow*.
- **`advanced-ptu-routing.md`** — same "report/design now, don't wire
  live automation into APIM without being asked" posture: this document
  specifies the policy/data model shape but doesn't add or change
  anything in `citadel-access-contracts/` or `bicep/infra/`.

## Implementation status

Built, as of this fork's next patch:

- **Policy fragments**: `bicep/infra/modules/apim/policies/frag-resolve-quota-scope.xml`
  and `frag-load-quota-allowance.xml`, registered in
  `bicep/infra/modules/apim/policy-fragments.bicep`, wired into both
  `default-ai-product-policy.xml` and `default-multi-product-policy.xml`
  as an opt-in tier-2 `llm-token-limit` alongside the unchanged tier-1.
  **Not exercised against a live APIM instance** — see the fragments'
  own inline HONESTY NOTE.
- **`src/quota-service/`** — the real Function App backing this design:
  `getQuotaAllowance`, `submitQuotaRequest`, `decideQuotaRequest`,
  `listPendingQuotaRequests`, `markQuotaRequestNotified`,
  `expireQuotaOverrides`. All the actual decision logic (the hard
  backstop clamp, expiry handling, the escalation threshold, the
  one-pending-request guard, verified-identity resolution, monthly-reset
  eligibility, and impersonation cross-check) lives in a pure
  `quotaLogic.ts` module, plus `emailTemplates.ts` for notification
  content — **117 unit tests total, run and passing in this session**
  (`npm test`), **100% line coverage on every `src/` file, 98.72%
  line / 92.84% branch / 94.22% function overall** (measured via
  `node --test --experimental-test-coverage`, not estimated — see
  `src/quota-service/README.md` for the full breakdown and the one
  named residual gap). Every I/O boundary (Cosmos, SMTP, JWKS/JWT
  verification) is now reached through an injectable `deps` parameter,
  tested with in-memory fakes and — for JWT verification — real
  `jose`-signed tokens served by a local HTTP server, not mocked-away
  shortcuts. Still not integration-tested against a real Cosmos account,
  SMTP server, or Entra tenant — that remains a real, stated gap.
- **Impersonation defense-in-depth** — see the dedicated "Impersonation"
  section above: `submitQuotaRequest`/`decideQuotaRequest` now
  independently re-validate the caller's real bearer token
  (`tokenValidation.ts`, `jose`) rather than trusting the `x-verified-oid`
  header alone, closing the direct-call-bypass-the-function-key hole.
  Fail-closed by design. **Breaking change on upgrade**: requires
  `Entra_TenantId`/`Entra_Audience`/`Entra_OpenIdConfigUrl` configured on
  `quota-service`, or every `/submit`/`/decide` call is rejected.
- **Email notifications** — both directions now send real email (SMTP,
  `nodemailer`): a new request notifies the configured approver
  contact, and a decision notifies the original requester at the email
  captured from their own token at submission time. Additive to the
  existing generic webhook, not a replacement for it — see
  `src/quota-service/README.md`'s "Email notifications" section.
- **The authorization gap is closed** (the "not built" item this section
  used to list): `bicep/infra/modules/quota-service/quota-api.bicep` puts
  a standalone, JWT-validated APIM API in front of `submit`/`decide`/
  `pending`, requiring the new `Quota.Approve` Entra app role
  (`bicep/infra/entra-id-setup/setup.ps1`) for the latter two, and
  stamping a **verified** `oid` onto every request — `submitQuotaRequest`/
  `decideQuotaRequest` now reject (401) any call missing it, with no
  fallback to a client-supplied identity field. Scope, stated precisely:
  this proves the caller is a real, role-holding person.
- **Approver-resolution — claim-based, now built**: `Quota.Approve` used
  to be genuinely all-or-nothing platform-wide (the gap the previous
  bullet used to end on). It no longer is, for **team-scoped** requests:
  `quota-api-policy.xml` also stamps a **verified** `x-verified-department`
  header from the validated JWT's `department` claim;
  `tokenValidation.ts`/`requestAuth.ts` independently re-verify it
  against the raw bearer token (the same defense-in-depth `oid` already
  gets, not a header trusted on faith); and `decideQuotaRequest.ts`
  rejects (403, `approver_not_authorized_for_scope`) any attempt to
  decide a `team`-scoped request whose `scopeId` doesn't equal the
  approver's own verified department (`approverAuthorizedForScope()` in
  `quotaLogic.ts`, unit-tested). An approver with no resolvable
  department claim fails closed — can never decide a team-scoped
  request, not "falls back to allowed." **Deliberately unchanged, and
  named as a residual gap, not silently narrowed**: `user`-scoped
  requests are still open to any `Quota.Approve` holder — there is no
  org-hierarchy/manager data source anywhere in this fork to resolve
  "the right approver" for an individual, and this design (claim-based,
  no new reference table) was chosen specifically to avoid building one.
  If your org needs individual-request scoping too, that still needs the
  reference-table approach originally sketched here — a real, separate
  piece of work, not something claim-based matching can do on its own.
- **Per-user quota basis for contracts with no JWT** — "per-user quotas
  for all traffic" runs into a hard, unavoidable constraint: the gateway
  can only cryptographically know WHO a caller is from a validated Entra
  ID JWT. `frag-resolve-quota-scope.xml` now has a third, fallback
  precedence tier for contracts with no JWT requirement: a self-asserted
  `x-quota-user-id` client header, tagged with a new `quotaTrustTier`
  output variable (`"Verified"` | `"SelfAsserted"` | `"None"`) so a
  downstream consumer never confuses the two. **Stated plainly, not
  glossed over**: a `SelfAsserted` decrement is a fairness/noisy-neighbor
  control (stops one caller starving others sharing a subscription's
  tier-1 budget), never a real per-person budget guarantee — any caller
  can set that header to any value. For an actual guarantee, the
  contract needs `jwtRequired="true"`; there is no way around this with
  an API key alone. `quotaScopeCacheKey` now includes the trust tier, so
  a `SelfAsserted` and a `Verified` decrement for what could coincidentally
  be the same string value never share one quota bucket.
- **Monthly reset**: `resetMonthlyQuotaOverrides` (Timer, 1st of each
  month) — clears every *temporary* `quota-overrides` document at the
  calendar-month boundary regardless of its own `expiresAt`, so an
  elevated allowance doesn't quietly carry into a new budget cycle.
  Permanent overrides survive by default (a deliberate choice, not an
  oversight — see `quotaLogic.ts`'s `survivesMonthlyReset()` doc
  comment); opt into clearing those too via
  `QuotaOverride_MonthlyResetIncludesPermanent`. Distinct from — and
  doesn't duplicate — the fact that tier-1/tier-2 `llm-token-limit`'s own
  `token-quota-period="Monthly"` already resets **usage** automatically,
  no code involved; this function resets the override **grant**, which
  nothing else touches.
- **`tools/validate_policy_xml.py`**: a real, APIM-dialect-aware
  structural validator for every policy XML file in this repo (not just
  this fork's own additions) — see
  `guides/enterprise-hardening-checklist.md` §8 for what it does and its
  real, verified results against this repo.
- **Both buttons, as Power Fx source**:
  `power-platform/quota-connector/` — a Swagger 2.0 custom connector
  (Entra ID delegated OAuth2) plus a canvas-app kit
  (`canvas-app/RequestMoreBudget.fx.txt`,
  `canvas-app/ApproveDenyRequests.fx.txt`) meant to be embedded directly
  into Power BI report pages via the Power Apps visual. Same honesty
  posture as this fork's Power BI report kit: real, reviewed Power Fx,
  not an executed/tested `.msapp` — no Power Apps Studio available in
  this session. Fabric/Translytical Task Flows (the alternative,
  newer write-back mechanism) were explicitly out of scope for this
  build.
- **Cosmos containers**: `quota-overrides` and `quota-override-requests`,
  added to `bicep/infra/modules/cosmos-db/cosmos-db.bicep`, same shape as
  specified in §3 above.
- **`bicep/infra/modules/quota-service/quota-service.bicep`** — deploys
  the Function App, reusing this accelerator's own existing
  `cosmos-sql-role-assignment.bicep` module for Cosmos RBAC rather than a
  new custom role assignment.
- **`src/usage-ingestion-logicapp/quota-approval-notification/workflow.json`**
  — the recurrence-triggered (5-minute) notification poll, calling
  `listPendingQuotaRequests` → your own webhook → `markQuotaRequestNotified`.

**Not built, still a real gap**: individual (`user`-scoped) request
approver-resolution — see the claim-based approver-resolution bullet
above for exactly what's now scoped (team-scoped requests) versus what
still isn't (individual ones), and why. The Approve/Deny screen still
shows every pending request to every `Quota.Approve` holder for
individually-scoped requests specifically — team-scoped ones are now
actually gated. See `guides/enterprise-hardening-checklist.md` for the
full security posture of everything now built (auth model, fail-open
tradeoffs, least-privilege gaps) — that hardening pass covers this
mechanism specifically, not just the base platform.
