# Enterprise Hardening Checklist

A full-platform security review — the base accelerator **and** this
fork's own additions (`pricing-service`, `quota-service`, the new policy
fragments, the new Cosmos containers, the new Logic App) — against a
standard enterprise security baseline: identity & access, network
isolation, secrets management, data protection, resiliency, audit/
logging, and abuse guards on new endpoints.

**How to read this doc.** Every domain below has three columns: what the
base accelerator already provides (cited to the real guide/param, not
assumed), what this fork's additions change, and residual risk/action
still needed. Where this session could verify something (a build, a unit
test, a grep against real source), it says so; where it couldn't (no live
Azure subscription, no APIM instance), it says that too, explicitly,
rather than asserting a checkbox that wasn't actually checked.

## 1. Identity & Access

| | Base accelerator | This fork's additions | Residual risk |
|---|---|---|---|
| Client authentication | API key (always) + optional JWT via Entra ID, opt-in per access contract (`entraAuth` param, `jwtAuth.enabled` per contract) — `entraid-auth-validation.md` | Reuses the identical mechanism — `frag-resolve-quota-scope.xml` reads `oid`/`department` from the same `validated-jwt` variable `security-handler` already produces, no new auth surface. **Now also** falls back to a self-asserted `x-quota-user-id` header (tagged `quotaTrustTier: "SelfAsserted"`, distinct from `"Verified"`) when no JWT is present, so a no-JWT contract gets fairness/noisy-neighbor partitioning instead of no per-caller signal at all | No longer silently no-ops on a contract without `jwtRequired` — it falls back to the `SelfAsserted` tier instead, which is a real behavior change worth reviewing before upgrading (a contract you relied on being strictly tier-1-only now gets a spoofable per-header quota bucket too, unless the client never sends `x-quota-user-id`). `SelfAsserted` is explicitly NOT a real per-person guarantee — any caller can set that header to any value; only `jwtRequired="true"` gives a cryptographic one |
| App-to-app auth (APIM → Function Apps) | `pricing-service`'s `enrichPricing` uses `authLevel: 'function'` (a function key, passed via APIM named value) | `quota-service`'s nine endpoints all still use `authLevel: 'function'` at the Function App layer — unchanged. `submit`/`decide`/`pending` sit behind the Quota Override API (JWT + role for two of the three) **and now also independently re-validate the caller's real bearer token in code** (`tokenValidation.ts`/`requestAuth.ts`, `jose`) rather than trusting the function-key-reachable header alone — including, as of this fix, re-checking `Quota.Approve` role membership itself, not just identity (see the `decideQuotaRequest` row below). The other six endpoints stay internal-only, function-key-alone, same as before — legitimately service-to-service, no end-user identity concept to spoof. | **The direct-call *identity*-spoofing hole on `submit`/`decide` was closed by identity re-validation alone; the direct-call *role*-bypass hole (a real, authenticated but non-approver employee calling `/decide` directly) needed the separate `requiredRole` fix below and is now closed too.** A caller with only the function key and their own genuinely valid token now fails unless that token's re-verified `roles` claim actually carries `Quota.Approve`. What's still NOT closed: the function key itself is still a long-lived shared secret, and the six internal-only endpoints have no equivalent identity check (they don't need one — no user identity flows through them). Full platform-level upgrade path unchanged for those six: Easy Auth (Entra ID) + `authentication-managed-identity` end to end, same pattern already used for Foundry's `ProjectManagedIdentity` mode (`ai-model-inference-api-policy.xml`). **Not implemented for the six internal endpoints** — flagged, not fixed; genuinely not needed there the way it was for submit/decide |
| Cosmos data-plane access | Cosmos DB Built-in Data Contributor (`00000000-0000-0000-0000-000000000002`), granted account-wide via `cosmos-sql-role-assignment.bicep`, to each Function App/Logic App's managed identity | `quota-service.bicep` reuses the same existing module — same account-wide grant, not narrower | **Least-privilege gap, pre-existing in the base accelerator, inherited here.** Cosmos SQL role assignments support scoping `scope` down to a specific database/container path; every identity in this accelerator (including the two new ones) is granted account-wide access instead, meaning `quota-service`'s identity can technically read/write `llm-usage-container` even though it only ever touches two containers. Worth a dedicated follow-up to scope every Function/Logic App identity down to just the containers it uses — not attempted in this pass since no existing pattern in this repo demonstrates the narrower scope string working end-to-end, and it touches every existing identity, not just the new ones |
| `decideQuotaRequest` authorization | N/A (new) | **Fixed, in three layers now — corrected after a later security review found layer 2 was never actually implemented.** `bicep/infra/modules/quota-service/quota-api.bicep` gates `/decide` and `/pending` behind a validated JWT + the `Quota.Approve` role and stamps verified `oid`/`department` onto the request (layer 1, APIM). `tokenValidation.ts`/`requestAuth.ts` independently re-validate the caller's real bearer token and cross-check `oid`/`department` against those headers (`identityMatchesToken`, `department` mismatch check, both unit-tested) — but until this fix, that re-check never extracted or checked the token's `roles` claim, so it only proved "a real person," never "a `Quota.Approve`-holding person." A caller with `quota-service`'s function key and ANY valid token for this app registration — any authenticated employee, no role needed — could call `/decide`/`/pending` directly, skip APIM's role gate entirely, and approve/deny as themselves. **Layer 2 is now real**: `verifyBearerTokenClaims` also returns `roles: string[]`, and `corroborateIdentity` takes an optional `requiredRole` — `decideQuotaRequest.ts` passes `'Quota.Approve'` unconditionally, `listPendingQuotaRequests.ts` passes it whenever `x-verified-oid` is present (i.e. called via the external API, not the internal notification Logic App's function-key-only path) — both now reject (403) a re-validated token that doesn't carry the role, fail-closed, 100%-covered by new unit tests. Layer 3 (`approverAuthorizedForScope()`, department-vs-scope for `team`-scoped requests) is unchanged and still unit-tested. | **What's still open, narrowed but not eliminated**: `user`-scoped requests remain all-or-nothing among role holders — any `Quota.Approve` holder can still decide one, since there's no org-hierarchy/manager data source in this fork to resolve "the right approver" for an individual (a deliberate scope choice, not an oversight — see guides/quota-override-approval.md). The Approve/Deny canvas app screen still shows every pending *individually-scoped* request to every role holder; team-scoped ones are now actually gated. `tokenValidation.ts` itself is also unverified against a live Entra tenant — real security logic that has not been exercised against a real token, worth prioritizing if this goes to production. |
| App roles | `Task.ReadWrite` / `Models.Read` / `MCP.Read` / `Agent.Read`, opt-in via `requiredRoles` per contract — `citadel-access-contracts-policy.md` | **Added**: `Quota.Approve` (`entra-id-setup/setup.ps1`, id `...006`), required by `quota-api-policy.xml` for `/decide` and `/pending`. Not exercised against a live tenant in this session — review before running the script. | Assign the role deliberately, not broadly — a holder can decide any *individually*-scoped pending request platform-wide (team-scoped ones are now department-gated, see the row above) until individual-request scope resolution exists too |

## 2. Network Isolation

| | Base accelerator | This fork's additions | Residual risk |
|---|---|---|---|
| Topology | Two full opt-in private-networking topologies (Hub-Based, Hub-Spoke-Hub), private endpoints for APIM/Cosmos/Storage/Key Vault, dedicated Logic App subnet — `network-approach.md` | **Fixed.** Both `pricing-service.bicep` and `quota-service.bicep` now take an optional `functionAppSubnetId` param (empty = today's unchanged Consumption-plan behavior). When set, the hosting plan switches to **EP1/ElasticPremium** (Y1/Consumption doesn't reliably support regional VNet integration) and the Function App gets the same `Microsoft.Web/sites/networkConfig` swift-connection + `vnetRouteAllEnabled`/`WEBSITE_VNET_ROUTE_ALL`/`WEBSITE_CONTENTOVERVNET` wiring `bicep/infra/modules/functionapp/functionapp.bicep` already uses for a working VNet-integrated Function App elsewhere in this repo — same pattern, not a new one | Deploying with `functionAppSubnetId` set is opt-in — pass it (pointed at the same Logic App Subnet `network-approach.md` documents, or a dedicated one) when deploying behind either private topology; the two Function Apps default to public-network behavior unchanged otherwise, same as every other optional param in this fork. Not exercised against a live subnet in this session — no Azure subscription available, same disclosure as every other bicep change here |
| Cosmos public access | `publicAccess` param, default recommendation `'Disabled'` in production (`full-deployment-guide.md` §9) | The two new containers inherit whatever the account-level setting already is — no per-container override, consistent with every other container | None beyond the Function App VNet-integration gap above — once that's fixed, Cosmos access from these identities is no different from any other component's |

## 3. Secrets Management

| | Base accelerator | This fork's additions | Residual risk |
|---|---|---|---|
| Client credentials | Entra ID client secret stored in Key Vault (`ENTRA-APP-CLIENT-SECRET`), auto-provisioned by `entra-id-setup/setup.ps1` | No new client secrets — both Function Apps use system-assigned managed identity for Cosmos, matching `pricing-service`'s own established precedent | None found |
| Function keys | N/A pattern already exists (`pricing-service`'s `PricingService_FunctionKey` app setting on the Logic App) | `quota-service` introduces the same pattern for four more endpoints (`QuotaService_FunctionKey` used by both the Logic App and, per the APIM named values, the policy fragment) | Same gap as row 2 of §1 — function keys are a real, if consistent-with-precedent, secret-management weakness. Rotating one means updating it in two places (the APIM named value and the Logic App app setting) with no coordinated rotation tooling, unlike this accelerator's own zero-downtime **subscription key** rotation (`access-contract-key-rotation-guide.md`) — no equivalent exists for function keys anywhere in this accelerator, base or fork |
| OBO client samples | N/A | `samples/agent-obo-client/` already documents preferring a certificate/Federated Identity Credential over a plain client secret — same guidance applies here and isn't re-litigated |
| SMTP credentials (new) | N/A | `quota-service`'s email notifications need an SMTP password — the one credential in this whole service that can't be turned into managed-identity-only access the way Cosmos already is | Wired as a Key Vault reference app setting (`quota-service.bicep`'s `smtpPasswordKeyVaultSecretUri`), never plaintext — consistent with this table's own row above on client credentials. If your org has Azure Communication Services already, `src/lib/email.ts`'s own comment documents the managed-identity-only swap-in (a new Azure resource this session didn't provision, so not built, only pointed at) |

## 4. Data Protection

| | Base accelerator | This fork's additions | Residual risk |
|---|---|---|---|
| PII handling | Full anonymization/deanonymization/blocking framework, opt-in per contract, Language Service-backed — `pii-masking-apim.md` | **Not applicable.** `quota-overrides`/`quota-override-requests` documents contain no prompt/response content — `reason` is a short free-text justification string (e.g. "Quarter-end batch reconciliation"), not user data | Worth a light content-policy note for whoever builds the `submitQuotaRequest` front-end: don't let `reason` become a dumping ground for anything sensitive, since it isn't covered by the PII pipeline and is retained indefinitely (append-only audit trail, by design) |
| Encryption at rest/in transit | Cosmos/Storage/Key Vault default Azure encryption at rest; `httpsOnly: true` on every Function App (`pricing-service.bicep`, now `quota-service.bicep` too) | Same, inherited automatically | None found |

## 5. Resiliency

| | Base accelerator | This fork's additions | Residual risk |
|---|---|---|---|
| Backend failover | Circuit breaker (3 failures/5min → cool-off), session affinity, weighted backend pools — `resiliency-guide.md` | `frag-load-quota-allowance.xml`'s `send-request` to `quota-service` uses `ignore-error="true"` + a 3s timeout + explicit fail-open fallback to the contract baseline (see `guides/quota-override-approval.md` Phase A step 1) — the same defensive posture as the circuit-breaker pattern, applied at the single-call level since a single downstream Function endpoint doesn't need pooled-backend failover | This is a **timeout-and-fallback**, not a circuit breaker — a sustained `quota-service` outage means every request pays the 3s timeout cost until someone notices and fixes it, rather than APIM proactively stopping trying. Acceptable for v1 (traffic still succeeds, just slower) but worth revisiting if `quota-service` availability turns out to be worse than expected |
| Ingestion resilience | `Enrich_With_Pricing`'s `Http` action retries 3× exponential (`llm-usage-ingestion/workflow.json`) | `quota-approval-notification`'s `List_Pending_Requests` action uses the same retry policy; `Notify_Approver` retries 2× — deliberately fewer, since a notification is time-sensitive in a way a batch pricing enrichment isn't (see the workflow's own comments) | None found — a failed notification isn't silently dropped either way: `markQuotaRequestNotified` only runs `after: [SUCCEEDED]`, so an un-notified request is picked up again on the next 5-minute tick automatically |

## 6. Audit & Logging

| | Base accelerator | This fork's additions | Residual risk |
|---|---|---|---|
| Alerting | Opt-in `raise-alert-events` fragment: `throttling`, `quota-exceeded` (on by default), `backend-failure`, `auth-failure`, `content-safety`, `pii-failure` — `throttling-events-handling.md` | Tier-2 `llm-token-limit` failures raise the **same** `quota-exceeded` alert category as tier-1 (it's the identical policy element, just with a different `counter-key`/`token-quota`) — no new alert plumbing needed | Consider a dedicated `quota-override-approved`/`quota-override-requested` alert category (same extension pattern `throttling-events-handling.md` already documents) so a spike in approval requests is itself an observable signal — not built here, a headline for a future pass, not a gap in what already ships |
| Audit trail | Immutable usage/pricing records — `llm-usage-container`, versioned `model-pricing` snapshots | `quota-override-requests`' `statusHistory` is append-only by construction (`decideQuotaRequest.ts` only ever `.push()`s and replaces, never truncates) — verified by reading the code, not just asserted. Now also records a **verified** `by` value (the JWT's `oid`), not a client-asserted string | None found in the data model itself. Every decision is attributable to a real person now (§1) — what the trail still can't show is whether that person *should* have been able to decide that specific request, since scope-specific authorization isn't built yet |

## 7. Abuse Guards on the New Endpoints

| | Residual risk / what exists |
|---|---|
| `submitQuotaRequest` | Guards against the specific abuse this design anticipated (spamming requests for the same scope — the "one pending request per scope" check, unit-tested indirectly via `hasOpenRequest`). **Now exposed to end users** (via the Power Apps kit), so this matters more than when the row was first written. Still does **not** rate-limit submission volume across *different* scopes from the same caller — `quota-api.bicep`'s `subscriptionRequired: true` gets you an APIM subscription-key-level throttle for free if you add `llm-token-limit`/`rate-limit-by-key` to `quota-api-policy.xml` (this accelerator already has the exact primitive — see the Tool/Agent `rate-limit-by-key` pattern in `default-multi-product-policy.xml`) — not added in this pass, since the one-pending-request guard already covers the sharpest abuse case |
| `getQuotaAllowance` | On the synchronous request path already, so its own "abuse guard" is really "don't let it become a bottleneck" — covered by §5's fail-open timeout, not a separate concern. Unaffected by this build — still internal-only |
| `decideQuotaRequest` / `pending` | **Authorization gap closed this pass** (§1) — JWT + `Quota.Approve` role required. Rate-limiting wasn't added here either, same reasoning as `submitQuotaRequest`'s row: the sharper risk (unauthorized decisions) is what got fixed; volumetric abuse from a legitimate, authenticated `Quota.Approve` holder is a much lower-severity residual concern |
| Agent/sub-agent/MCP-tool call chains | **New guards added**: `frag-enforce-agent-limits.xml` — a depth cap (log-only by default via `{{agent-limits-enforce}}`, per `guides/agent-hierarchy-attribution.md` section 5's "observe before enforce" posture) and a fan-out cap (`quota-by-key`, always enforcing — no log-only mode exists for that primitive). Both are keyed on the self-asserted-or-OBO-verified `agentRootId`, so a caller that never sets `x-agent-root-id` is completely unaffected — this only guards traffic that opts into the hierarchy scheme at all. **Residual risk**: the depth guard ships in log-only mode until an operator deliberately reviews the `Agent-Depth-Exceeded` trace source and flips `{{agent-limits-enforce}}` to `true` — a deployment that never does this has no depth enforcement, by design, not by oversight |

## 8. A note on policy XML well-formedness

While preparing this audit, running every changed/new `frag-*.xml` and
product policy file through a strict XML parser (`xml.dom.minidom`)
turned up parse errors — but so does `frag-central-cache-manager.xml`,
an **unmodified, pre-existing** file from the upstream accelerator (its
`GetValueOrDefault<string>(...)` C# generic-type syntax inside a
double-quoted attribute value isn't valid strict XML, since `<`/`>` are
technically required to be escaped inside attribute values even though
APIM's own policy tooling evidently accepts this style throughout the
real, shipped codebase). This fork's new fragments follow the exact same
established convention, not a new or worse one — flagging this here as
an **observation about the whole platform's tooling**, not a defect this
fork introduced: nothing in this repository's build/CI process validated
policy XML well-formedness before deployment, for any fragment, old or
new.

**Closed in a later pass**: `tools/validate_policy_xml.py` — masks every
`@(...)`/`@{...}` C# expression block (balanced-delimiter aware, so a
nested `(...)` doesn't truncate the match) before running a strict XML
parse on what's left, so it validates real structure (unclosed tags,
mismatched elements) without false-positiving on this repo's own
accepted authoring convention. Verified against the real repo in this
session: passes all 76 existing policy XML files clean, and correctly
catches a deliberately-broken test fixture (an unclosed `<choose>`) with
the right line number, exit code 1. Wire it into whatever pipeline runs
`az deployment` — it's read-only, local, needs no Azure access, and
takes a `root-dir` argument if you want to point it somewhere other than
the repo root. What it still doesn't do: validate the C# expressions'
own syntax, or check against APIM's actual policy XSD (element/attribute
validity) — both would need a real APIM instance or a C# parser this
tool doesn't have.

## What this audit could not verify

Consistent with every other guide in this fork: no live Azure
subscription or APIM instance was available in this session. Everything
above is grounded in reading real source (bicep, policy XML, Function
code) and, where code exists, in actually building and testing it
(`quota-service`'s 164 unit tests and `pricing-service`'s 51 — both now
100% line coverage on every `src/` file except a small, documented,
thin-SDK-wrapper edge in each — measured via
`node --test --experimental-test-coverage`, not estimated — plus both
services' clean `tsc` builds) — but
none of the following was exercised end-to-end: actual RBAC assignment
correctness once deployed, NSG/private-endpoint enforcement, the policy
fragments' behavior inside a real APIM gateway, or the Logic App's
behavior against a real webhook. Treat every "None found" above as "none
found by static review," not "verified safe in production."
