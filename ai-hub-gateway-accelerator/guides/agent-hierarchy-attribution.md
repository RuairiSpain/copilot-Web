# Agent Hierarchy Attribution

**Status: sections 1-2 and part of 4 and 5 are now built** — the header
propagation scheme (section 1), where it's captured (section 2), the
trust-tiering distinction between an OBO-verified and a self-asserted
call (section 4, the attribution half — OBO's own token-exchange
mechanics are still a design description, not code this fork can
provide since it requires an Entra app registration this session
doesn't have), and the depth guard from section 5 (fan-out is built too,
enforcement-only, no log-only mode — see below). **See "What's actually
built" right after this status block for the precise, file-by-file
list.** Sections 3, 6 (the atomic multi-scope quota decrement), and 7
remain design-only. Read this whole document with that split in mind —
earlier sections describe what now exists in `bicep/infra/modules/apim/policies/`
and the ingestion pipeline; later ones (6 especially) describe a
larger, still-unbuilt piece of work.

## What's actually built (read this first)

- **`bicep/infra/modules/apim/policies/frag-resolve-agent-hierarchy.xml`**
  — captures `x-agent-root-id`/`x-agent-caller-type`/`x-agent-depth`,
  computes `agentTrustTier` (`Verified` when a validated JWT is present
  alongside the header, `SelfAsserted` when only the header is,
  `None` for ordinary non-hierarchical traffic), and emits a `<trace>`
  correlated to the request's own `llm-usage` metric via Application
  Insights' own `operation_Id` — not a bespoke key. Included in
  `azure-open-ai-api-policy.xml`, `universal-llm-api-policy-v2.xml`, and
  `unified-ai-api-policy.xml`, right alongside `set-llm-usage`.
- **`bicep/infra/modules/apim/policies/frag-enforce-agent-limits.xml`**
  — the depth guard (log-only by default, via `{{agent-limits-enforce}}`
  — see the fragment's own comment for the full rollout posture) and the
  fan-out guard (`quota-by-key`, keyed on `agentRootId + ":fanout"`,
  always enforcing — APIM's `quota-by-key` has no log-only mode, stated
  plainly rather than faked). Must run immediately after
  `resolve-agent-hierarchy`.
- **`src/usage-ingestion-logicapp/llm-usage-ingestion/workflow.json`**
  — `Run_query_and_list_results`'s KQL now joins the `traces` produced
  by the fragment above against `customMetrics` on `operation_Id` before
  summarizing, and includes `agentRootId`/`agentCallerType`/
  `agentDepth`/`agentTrustTier` in the `by` clause — so a root agent
  run's requests aggregate together per hour-bucket, but different runs
  (and ordinary non-hierarchical traffic, `agentTrustTier = "None"`)
  don't collapse into each other. Every `llm-usage-container` document
  now carries these four fields.
- **`powerbi-report-kit/23-agent-hierarchy-measures.dax`** — roll-up and
  drill-down measures on the new fields (see "Two mechanisms, two
  purposes" at the end of this doc — still applies).
- **Not built, still exactly as originally designed below**: the OBO
  token-exchange mechanics themselves (section 4's core, beyond the
  trust-tiering that now consumes its output), the atomic multi-scope
  Redis quota decrement (section 6 — though see the note there: this
  repo already has the primitives this would need), and the
  third-party/unmetered handling (section 7).

## The question this answers

## The question this answers

When an agent calls a sub-agent, or an MCP tool that itself makes a model
call, **who does that downstream token usage belong to** — the human who
started the original request, or whatever identity the sub-agent/MCP
server happens to authenticate with? Today, the honest answer is: neither,
reliably. It's whatever identity is presented on that specific nested
call, decided implicitly by however the calling code is configured, not
by a deliberate platform policy.

This guide covers three related but distinct outcomes: **attribution**
(sections 1-3 — a self-asserted header good enough for chargeback
reporting), **verified attribution with real-time enforcement**
(section 4 — Entra ID On-Behalf-Of token exchange, which actually gates
the original user's live token budget, not just reports on it
afterward), and the reverse direction — **what to do about responses
coming back in** from destinations outside your control, where neither
of the first two is achievable (section 7).

## Why this doesn't work today

Every attribution field in this system —`appId`, `productName`,
`customDimension1`/`customDimension2` — is resolved **per request**, from
whatever subscription key or header is presented on that one call. There
is no parent-child or causal relationship anywhere in the schema. Two
requests thirty seconds apart from the same logical agent run look
identical to two unrelated users, unless something explicitly says
otherwise.

This is the *same underlying gap* as
`guides/agent-trace-instrumentation-checklist.md` describes for
latency/tracing — solving either one needs a propagated identifier the
gateway can't infer on its own. The fix below is designed to share that
propagation mechanism with tracing rather than invent a second one.

## What exists today and doesn't solve this

The accelerator's Publish Contracts already track MCP tool calls and A2A
agent calls — separately from LLM usage:

- `mcp-usage-container` — fed by the `mcp-usage-ingestion` Logic App,
  aggregating `mcp-usage` Application Insights custom metrics.
- `agent-usage-container` — fed by `agent-usage-ingestion`, aggregating
  `a2a-usage` custom metrics.

Per `bicep/infra/citadel-publish-contracts/publish-contract-guide.md`
(upstream), these record **request counts per (tool/agent, product,
backend, hour)** — not tokens, not cost,
and with **no shared key back to `llm-usage-container`**. They tell you
"this MCP tool was called 40 times this hour," not "this MCP tool's
internal model calls cost $12 and belonged to these upstream agent runs."
This proposal doesn't replace that signal — it's the missing link that
would let it join to the token/cost data instead of standing alone.

## Proposed design

### 1. A propagated hierarchy header

Every call into the gateway carries, in addition to today's identity
headers:

- `x-agent-root-id` — a single ID generated once, at the top of an agent
  run, and forwarded unchanged by every downstream call that run makes
  (sub-agent calls, MCP tool calls that themselves hit a model). This is
  the chargeback anchor — "everything under this ID belongs to the same
  logical run."
- `x-agent-caller-type` — one of `root`, `sub-agent`, `mcp-tool` (extend
  as needed) — what kind of thing made this specific call.
- `x-agent-depth` — integer, how many hops deep this call is from the
  root — cheap to compute, useful for spotting runaway recursion (an
  agent calling a sub-agent calling a sub-agent...) as its own signal,
  independent of cost.

This is deliberately the same shape as W3C `traceparent` propagation
(root ID + per-hop context) — a team implementing one should implement
both together. A `traceparent` already in flight for tracing purposes
could carry `x-agent-root-id` as its trace ID directly, avoiding two
separate propagation mechanisms doing the same job.

### 2. Where it's captured

Add `rootRequestId`, `callerType`, and `depth` to the `llm-usage-container`
document shape (`frag-set-llm-usage.xml` → the ingestion Logic App →
Cosmos, same path every other field in this document already takes) —
not a new container. Every usage record already knows its own `appId`;
this adds "and which run, and what role in that run" alongside it.

### 3. How it resolves "original user or execution context"

**Both, by design, not a forced either/or:**

- **Chargeback rolls up to the root.** A Power BI measure sums
  `cost.totalCost` `GROUP BY rootRequestId`, then attributes that total to
  whichever user/department owns the *root* call — regardless of how many
  sub-agents or MCP tools ran underneath it, and regardless of what
  identity each of those authenticated with individually. This is what a
  Budget Holder actually wants: "how much did this agent run cost the
  business," not "how much did the MCP server's own service principal
  spend."
- **Sub-agent-level detail is preserved, not discarded.** The Developer
  reports (files 18-22 in `powerbi-report-kit`) can still filter/group by
  `callerType`/`appId` within a `rootRequestId` to answer "which specific
  sub-agent or tool inside this run was the expensive one" — the
  roll-up for chargeback and the drill-down for debugging are the same
  data, two different `GROUP BY` clauses, not two different pipelines.

### 4. Stronger alternative: Entra ID On-Behalf-Of (OBO) — verified propagation, and real-time quota, not just chargeback

Section 1's header scheme has a real weakness: `x-agent-root-id` is
**self-asserted**. Nothing stops a caller from presenting a root ID it
doesn't own. For internal, trusted services that may be an acceptable
risk; it isn't for a sub-agent/MCP tool you don't fully control. There's
a standard mechanism that closes this gap, and it does more than fix
attribution — it enables **real-time quota enforcement**, not just
after-the-fact chargeback reporting.

**The mechanism.** Entra ID's OAuth 2.0 On-Behalf-Of flow
(`grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer`): the parent
agent holds a token for the signed-in user; when it calls a sub-agent or
MCP tool, it exchanges that token for a new one scoped to the callee,
still carrying the original user's identity — not the parent's own
service identity. The sub-agent/MCP tool then calls the gateway with
*that* token. Microsoft has a dedicated product for exactly this
scenario, **Microsoft Entra Agent ID**, with its own OAuth flow docs
("Agent OAuth flows — On-behalf-of flow," "Agent's user account
impersonation protocol") — agent identities are confidential clients that
can themselves act as OBO resources, using Federated Identity Credentials
rather than a bare secret. Read the actual current docs before committing
engineering time to this; it's evolving and this guide only captures the
shape of it, not every mechanical detail.

**A claim detail that matters and is easy to get wrong.** Entra ID does
**not** produce full nested-actor-claim chains the way some other
identity providers do (Okta, per RFC 8693) — each hop's token carries
`sub` (the originating user) and `azp` (the immediate calling client),
but **`sub` is pairwise-pseudonymous per resource**: the same user gets a
*different* `sub` value depending which API/audience the token targets. A
quota key built on `sub` will see the sub-agent's call and the user's own
direct call as two different people. **Use `oid`** — Entra ID's
tenant-wide stable object ID — as the identity claim the gateway keys
quota on, not `sub`.

**Why this beats the header scheme, concretely:**
- The identity is cryptographically signed and verifiable (standard APIM
  `validate-jwt` policy — signature, issuer, audience), not trusted on
  faith.
- It doesn't require a new bespoke propagation mechanism — the gateway's
  existing identity-extraction step (functional spec §5, ADR-008) just
  needs to read `oid` from the token, including when that token arrived
  via an OBO chain.
- Because Redis-backed quota enforcement (FR-009/§9, ADR-007) already
  keys off the caller's identity, a correctly OBO-chained sub-agent call
  decrements the **same live budget bucket** the user's own direct calls
  decrement — at request time, before the call completes, not discovered
  later in a Power BI report. That's a materially stronger outcome than
  section 3's chargeback-only framing: enforcement, not just observation.

**Where it doesn't reach.** OBO only works for sub-agents/MCP tools that
actively participate — a third-party or marketplace tool you don't
control almost certainly won't perform the exchange. For those, fall back
to the section 1 header scheme, but **tag the resulting usage records
with which trust tier they came from** (`OBOVerified` vs.
`SelfAssertedHeader`) rather than treating a cryptographically verified
roll-up and a trust-on-faith one as the same signal in a report — a
Budget Holder or auditor should be able to tell the difference. OBO also
needs a per-hop app registration and consent grant, which is real
operational overhead that grows with agent hierarchy depth — not free,
and not something to take on for every tool without weighing it.

**For developers implementing this**: `samples/agent-obo-client/` has
copy-paste starting points (Python and C#) implementing the OBO exchange
and the section 1 hierarchy headers together — see that folder's README
for what's actually been verified in each versus what's written against
documented APIs but unverified in this environment.

### 5. Depth limits, and why depth alone isn't enough

**Where enforced and where the limit comes from**: an APIM policy,
deterministic per ADR-013 — a plain integer comparison against a
configured max, no runtime SQL/Graph call (ADR-002). The max-depth value
itself isn't a new governance mechanism: it's one more field in the
compiled Effective Rules already published to Redis (ADR-003/007,
alongside the `MinimumModel`/`RemainingBudget`-shaped values ADR-007
already shows), flowing through the *same* override hierarchy FR-007
already defines for quotas and routing — Global → Business Unit →
Department → User, most specific wins. No new precedence model, no new
compiler path.

**Rollout posture — observe before you enforce.** Don't launch straight
to rejecting calls: this project's own established pattern (ADR-12's
Shadow Evaluation Framework, and the same "measure before automating"
posture `guides/advanced-ptu-routing.md` takes toward capacity-aware
routing) applies here too. Phase 1: log/flag calls that would have
exceeded the cap, without blocking them, and look at the real depth
distribution your actual agent traffic produces. Phase 2: set the
threshold from that evidence and flip enforcement on. A guessed threshold
enforced from day one either blocks legitimate deep workflows or is set
so loose it catches nothing.

**Rejection semantics, once enforcing**: a depth-limit rejection is
**non-transient** — the calling agent retrying the identical call will
hit the identical rejection. Return a structured, distinctly-coded error
(e.g. `agent_depth_exceeded`), not a generic 429/5xx a naive retry loop
would treat as transient and hammer again. This mirrors the
transient-vs-non-transient error shaping already established as this
platform's convention (structured JSON errors distinguishing retryable
from non-retryable failures).

**Depth alone doesn't catch a wide, shallow explosion.** An agent that
fans out to 50 sub-agents in parallel, each at depth 2, sails under a
depth-5 cap while producing enormous cost. Depth needs a companion
control: `maxActiveChildrenPerRoot` — a live count of in-flight children
under one `rootRequestId`, checked alongside depth, not instead of it.
This is the *same* atomic-counter mechanism section 6 designs for quota
decrement, applied to a different governance dimension — build the
primitive once, reuse it for both.

**Orphaned counters need a safety net.** "Decrement the fan-out counter
when a child completes" assumes every child reports completion — a
crashed or abandoned sub-agent won't. Give every increment a TTL (set in
the same atomic operation that performs the increment, via the Lua
pattern in section 6) so an orphaned counter expires and self-heals
instead of permanently eating into that root's fan-out budget.

### 6. Atomic, multi-scope quota decrement

**A major simplification found while building sections 1/2/5 above,
worth stating before the rest of this section**: this design was
originally written assuming a bespoke Redis client + hand-rolled Lua
script would need to be built inside `quota-service` (a Node/Cosmos-based
service with no Redis integration at all) to get real atomicity. That
assumption was wrong, confirmed against this repo's actual bicep:

- **This accelerator already provisions Azure Managed Redis**
  (`bicep/infra/modules/redis/redis.bicep`) and already wires it as
  APIM's own external cache (`apim.bicep`'s `enableRedisCache`/
  `redisCacheConnectionString`, defaulted **on** — `enableManagedRedis`
  is `true` by default in both `main.bicep` and `resources.bicep`). A
  standard deployment of this accelerator already has the Redis backing
  this section wants, with nothing further to enable.
- **APIM's own native `llm-token-limit`/`rate-limit-by-key`/
  `quota-by-key` policy elements are themselves atomic, distributed
  counters once backed by that external cache** — this is documented
  APIM behavior, not something built for this project. `llm-token-limit`
  specifically already implements exactly the "estimate-then-reconcile"
  token-accounting semantics this section originally set out to design
  from scratch (that's the entire point of the `llm-` prefix on that
  policy element vs. the generic `rate-limit-by-key`).
- **A correctly-performed OBO exchange (section 4) already solves the
  "decrement the right person's budget" problem with zero new code**:
  the resulting token carries the ORIGINAL user's own `oid`, so
  `frag-resolve-quota-scope.xml` resolves the same identity — and
  therefore the same tier-1/tier-2 `llm-token-limit` budget bucket — a
  sub-agent's call decrements exactly like the user's own direct call
  would. No new scope type, no new atomic primitive needed for this part.
- **The only genuinely new atomic primitive this fork actually needed
  was the fan-out counter** (section 5) — and that's just another
  `quota-by-key` element (`frag-enforce-agent-limits.xml`), the same
  pattern `default-multi-product-policy.xml` already uses for Tool/Agent
  traffic. No custom Lua script, because the primitive this section
  assumed didn't exist already did.

**What genuinely remains unbuilt, and why it's still real work**: the
depth/fan-out guards above operate on the *self-asserted or
OBO-verified `oid`-keyed* budget — they don't yet implement the
four-simultaneous-scope (User/Department/Business Unit/Organization)
check-and-decrement this section originally scoped, since that's a
different governance dimension than what sections 1/2/5 needed to close.
If you need that specific multi-scope Lua-script design, the reasoning
below still stands — it just isn't what this fork's OBO/hierarchy work
actually required to ship.

**The assumption worth naming first**: "atomic decrement" implicitly
assumes you already know the exact amount to decrement at request time.
That's true for **request-count** limits (FR-009's third limit type,
alongside token and cost limits) — trivially atomic, decrement exactly 1,
pre-flight, no reconciliation needed. It is **not** true for token/cost
limits: actual usage isn't known until the model finishes responding, and
for streaming responses it's known even later — the same gap the very
first review of this project flagged ("no streaming-response accounting
model") now surfaces again here, sharper, because it blocks a clean
pre-flight gate rather than just a chargeback record.

**Design for token/cost limits — estimate, gate, reconcile:**
1. **Pre-flight**: decrement a conservative *estimate*, not the real
   figure — from the caller's `max_tokens` if supplied, else a
   per-model/complexity-tier heuristic (a fixed default per model to
   start; once FR-006's complexity scoring exists, or once
   `llm-usage-container` has enough history, a model's own typical
   response size is a better estimate than a flat constant).
2. **Gate on the estimate**: if decrementing the estimate would push any
   in-scope budget negative, reject before the model call happens — this
   is the actual enforcement moment.
3. **Post-response reconciliation**: once `pricing-service` resolves the
   real `cost.totalCost` (same enrichment step `guides/cost-attribution-guide.md`
   already defines), apply the *delta* (real − estimated) back to the
   same Redis key — a refund if the estimate ran high, a further
   decrement if it ran low. A further decrement can retroactively reveal
   the caller went over budget on a request that was already, correctly,
   allowed pre-flight. That's an honest edge case, not a bug: the
   governance response is blocking their *next* request, not undoing the
   one that already completed.

**The atomic primitive itself.** A bare `DECRBY` is atomic in isolation,
but the real requirement — check *and* decrement as one unit, across
**four simultaneous scopes** (User, Department, Business Unit,
Organization — spec §9's own validation order) — needs more than one
atomic command:

- A naive `GET` → compare → `DECRBY` has a race window: two concurrent
  requests can both read "100 remaining" before either writes back.
- `DECRBY` unconditionally, then refund if it went negative, is atomic
  and avoids that race — but it lets a concurrent reader (a monitoring
  dashboard, say) observe a transient negative value before the refund
  lands.
- **Recommended**: one Lua script (`EVAL`), given all four scope keys as
  `KEYS[]`. The script reads all four current values, checks all four
  against the requested amount, and **only if all four would remain
  non-negative** decrements all four — atomically, all-or-nothing. If any
  single scope would go negative, the script decrements *none* of them
  and returns which scope was the blocker — both for the caller's error
  message and for a governance dashboard showing exactly which budget
  stopped the request. Redis executes a Lua script to completion without
  interruption, which is what makes a genuinely atomic multi-key
  check-and-decrement possible at all — this isn't achievable by
  composing separate single-key commands, atomic or not.

The depth/fan-out counter in section 5 should use this same Lua-script
pattern (with a TTL set in the same script) rather than a separate
bespoke mechanism — one atomic primitive, two governance dimensions.

### 7. Third-party responses crossing back into the trust boundary — deliberately unmetered

Everything above (sections 1-6) is about calls **you make outward** to
sub-agents/MCP tools, where propagation — a header or an OBO token — is
possible because you control at least one end of the exchange. This
section is the opposite direction: a response **coming back in** from a
third party outside the Microsoft ecosystem, where no such control
exists. Neither MCP nor Google's A2A protocol standardizes usage/token
reporting in their spec — A2A's own project description calls what it
connects "opaque agentic applications." That's not a gap in this design;
it's a real limit on what's observable from outside a boundary you don't
operate.

**Scope decision for this fork: third parties outside the Microsoft
ecosystem are not metered, and no chargeback is attempted for them.**
This is a deliberate simplification, not an oversight — it was weighed
against trying to reconstruct their internal cost (self-reported `usage`
blocks with no way to verify them, or estimating from response text,
which only counts final output and can badly undercount anything
agentic on their end) and rejected as more engineering effort than the
result would be trustworthy. If a specific vendor relationship later
justifies the effort (a high-spend partner, a contractual usage-reporting
term), that's a scoped exception to design deliberately, not a default to
build for every external call.

**"MS ecosystem" vs. "external"** is a per-destination classification,
not something inferred per-request — it's one more field
(`meteringPolicy: Metered | Unmetered`) on the Destination catalog record
from the hub-and-spoke gateway design, set when a Platform Admin onboards
that destination through the existing Provider Management workflow (spec
§14) — the same workflow already used to onboard Foundry projects and
downstream/spoke APIMs. Foundry, Azure OpenAI, GitHub Models called
directly, and Copilot Studio's own model connections are `Metered` — the
whole `pricing-service`/cost-attribution pipeline already applies to
them. Anything outside that — a partner's opaque agent, a marketplace
service — defaults to `Unmetered` unless explicitly configured otherwise.

**What still gets logged for an unmetered call, and why.** Not attempting
chargeback doesn't mean logging nothing — there's a separate, non-
financial reason to record these calls: knowing *who* sent *what* to a
destination outside your own trust boundary is a data-governance/security
question (the same category as the content-safety/PII gap flagged in
this project's very first review), independent of cost. Log:
- Vendor identity, timestamp, the calling `rootRequestId`/`oid` (so "who
  sent data to this external service" is answerable from the same
  hierarchy fields sections 1-4 already define — reuse, not a parallel
  scheme).
- Success/failure, latency, request/response size — operational health,
  the same shape as the failures/latency work already in the Power BI
  kit (file 18), just without a cost figure attached.
- **Explicitly no `cost`/token fields — not zero, absent.** A `$0`
  reads as "this was free," which is false; the truth is "we chose not
  to measure this." Tag the record's `cost.calculationMethod` as
  `unmetered_external` — a third state alongside the existing `tokens`
  and the error-condition `unknown` from `pricing-service` — so a report
  can tell "deliberately excluded by policy" apart from "should have
  had a price and didn't resolve," which today would look identical.

**Effect on the Power BI kit.** Every cost measure built so far
(`[Total Cost (incl. PTU)]` and everything downstream of it) should
`EXCLUDE` `cost.calculationMethod = "unmetered_external"` rows the same
way it already leaves PTU/percentage-priced rows out of the token-priced
sum — and file 07's data-quality report should add a distinct KPI for
"unmetered external call volume" so that traffic is visible as its own
tracked category, not silently missing from the totals or confused with
the `unknown` error state that already exists there.

### 8. What's still unresolved, on purpose

- **Cross-gateway propagation.** If a sub-agent's MCP tool call goes to a
  *downstream/spoke* APIM (see the hub-and-spoke gateway design from
  earlier in this project's history) rather than staying within one
  gateway instance, the header needs to survive that hop too — the spoke
  must forward it, not just the hub honor it. The same applies to an OBO
  token: the spoke's own identity-extraction policy needs to read `oid`
  the same way the hub's does, not assume the hub already resolved it.
  The same is true of the Redis atomic-decrement scripts in section 6 —
  if hub and spoke use separate Redis instances, the quota keys need to
  live somewhere both can reach, or the roll-up silently splits across
  two counters that never see each other.
- **Estimate accuracy is a tuning problem, not a solved one.** Section
  6's pre-flight gate is only as good as the estimate it decrements — too
  conservative and legitimate requests get rejected against budget that
  was never really going to be spent; too loose and the gate doesn't
  actually protect anything. This needs real traffic data to tune, the
  same "observe before enforce" posture as section 5, not a number picked
  in this document.

## Relationship to other fork documents

- `guides/agent-trace-instrumentation-checklist.md` — same propagation
  problem, different consumer (latency/causality vs. cost attribution).
  Implement together if you're touching agent instrumentation at all.
- `guides/cost-attribution-guide.md` — this design extends
  `llm-usage-container`'s schema the same way that guide's `cost` field
  did; the `pricing-service` enrichment step is a natural place to also
  validate/normalize the hierarchy fields, not just price the record.
- `samples/agent-obo-client/` — working Python and C# starting points for
  section 4's OBO exchange and section 1's hierarchy headers, for
  developers implementing this rather than just reading about it.

## Two mechanisms, two purposes — don't conflate them

- **Section 1's header scheme** → cost attribution / chargeback
  reporting, self-asserted, works for any caller including ones you don't
  fully trust, answered after the fact in Power BI.
- **Section 4's OBO token exchange** → the same attribution *plus*
  real-time Redis quota enforcement, cryptographically verified, only
  works for callers that actively participate in the exchange.

Pick per sub-agent/MCP tool based on whether you control it and whether
you need to *enforce* a budget on it or just *report* on it — they're not
mutually exclusive, and a record's trust tier should say which one
produced it.
