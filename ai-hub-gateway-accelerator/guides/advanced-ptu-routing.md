# Advanced PTU/PAYG Routing (documented, not implemented)

**Status: design proposal only.** Nothing in this document is implemented
in APIM or anywhere else in this repository. This guide exists to record
the feature so it can be picked up as its own scoped piece of work later —
deliberately kept separate from the reporting added in
`guides/cost-attribution-guide.md` and the Power BI report kit, which
*report on* PTU/PAYG economics but do not change routing behavior.

## The idea

Route requests to a PTU (reserved-capacity) deployment while it has spare
headroom, and overflow to PAYG once it's saturated or during off-peak
windows where keeping a reservation warm costs more than pay-as-you-go
would. Today, routing to a PTU vs. PAYG deployment for a given model is a
static choice (whichever destination the effective routing rules point
at) — there is no live, capacity-aware switch.

## Why it isn't implemented here

1. **It doesn't fit anywhere in the current routing model.** FR-007's
   override hierarchy (User → Department → Business Unit → Global →
   Routing Pack) has no time-of-day or capacity-state dimension. FR-006's
   complexity routing scores *the request*, not *current system load*.
   Capacity-aware routing is a genuinely new routing dimension, not a
   configuration of an existing one.
2. **ADR-013 requires APIM routing to stay deterministic and free of
   runtime lookups beyond Redis/cache** — a capacity-aware decision needs
   a live utilization signal, which does not exist yet anywhere in this
   architecture (see "What would need to exist" below).
3. **It needs evidence before it needs code.** ADR-012's Shadow Evaluation
   Framework exists precisely for this: validating a routing change
   against real traffic before it affects production, rather than trusting
   the theory that "switch at N% utilization" is actually the right
   threshold.
4. Building it blind, without first knowing your actual peak/off-peak
   shape (which the recommendation report below produces), risks tuning a
   switchover rule against a guess instead of your real traffic pattern.

## What exists today instead: a recommendation report

The Power BI report kit's day-of-week × hour-of-day heatmap and the
PAYG-vs-PTU breakeven report (`guides/cost-attribution-guide.md` and the
`powerbi-report-kit` deliverable) together answer the question a human
needs to act on this manually: *when is the PTU reservation idle, when is
it overflowing to PAYG, and does the reservation size make economic sense
given the actual peak shape we're seeing?* That's available now, requires
no architecture change, and is the recommended starting point — use it for
at least a full traffic cycle (multiple weeks, ideally a full month
including any weekly/monthly peak pattern) before considering automating
anything on top of it.

## What would need to exist for real automated routing

If the recommendation report shows a switchover rule would clearly pay
off, here's the shape the work would take — sized as a distinct addition
to Phase 3 (Advanced Routing) in the implementation plan, not a small
follow-on to the cost-attribution or reporting work already done:

### 1. A live capacity signal, published to Redis

Something needs to know current PTU utilization/headroom and publish it
somewhere APIM can read without a runtime call to SQL, Graph, or the
provider's own management API (ADR-002). The natural shape, reusing a
pattern already in this repo:

- A lightweight timer-triggered Function (same shape as
  `src/pricing-service/src/functions/refreshPricingCache.ts`), polling
  the PTU deployment's utilization on a short interval (seconds-to-low-
  minutes, not once a day like pricing) and writing a small value —
  `{deployment, utilizationPct, headroomTokens, asOf}` — into Redis under
  a well-known key per deployment.
- **Open question, not resolved by this document**: where utilization
  data actually comes from depends on the provider (Azure OpenAI/Foundry
  exposes deployment-level metrics via Azure Monitor; other providers may
  not expose an equivalent signal at all) — this needs provider-specific
  investigation before it can be scoped precisely.

### 2. A new routing dimension in the Rule Compiler

Extending ADR-003's Effective Rules compilation with something like: *for
label/model X, prefer destination pool `{model}-PTU` while
`headroomTokens > threshold`, else route to `{model}` (PAYG)* —
mechanically similar to the failover routing already planned for Phase 3
(same "try preferred pool, fall back to the next" shape), just triggered
by a capacity signal instead of a provider-down signal. The compiled rule
would still be a static, deterministic APIM policy at request time (per
ADR-013) — it's the Redis-side capacity value that changes dynamically
between compilations, not the policy logic itself doing live math.

### 3. Validation via Shadow Evaluation before trusting it in production

Per ADR-012: replay a sample of real traffic against both the PTU and
PAYG destinations under the actual switchover logic, and compare cost,
latency, and quality before it ever affects a real user's request. The
switchover threshold itself (what utilization % triggers a switch) should
come out of this evaluation, not be guessed upfront.

### 4. Operational considerations, not yet designed

- **Flapping**: a naive threshold-based switch could oscillate rapidly
  near the boundary under bursty traffic — likely needs hysteresis (e.g.
  distinct switch-away and switch-back thresholds) or a minimum dwell
  time, neither designed here.
- **Session affinity**: stateful models (per the resiliency patterns
  already documented in the ai-hub-gateway-solution-accelerator upstream
  this repo is based on) complicate a mid-conversation switch — a request
  that's part of an existing session may need to stay on its current
  destination regardless of capacity state.
- **Cost of the signal itself**: polling utilization frequently enough to
  be useful, at enterprise scale, is itself a cost/load consideration that
  needs sizing, not assumed to be free.

## Summary

| | Now | Later (if the data supports it) |
| --- | --- | --- |
| What | Recommendation report (heatmap + breakeven) | Automated capacity-aware routing |
| Where | Power BI, existing `llm-usage-container` data | New Redis signal + Rule Compiler dimension + Shadow Evaluation |
| Who acts | A human (Platform Admin), manually | APIM, automatically, once validated |
| Risk | None — read-only reporting | Real — routing behavior change, needs the safeguards above |
