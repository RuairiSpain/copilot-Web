# pricing-service

New component added for **per-record cost attribution with a versioned,
24h-refreshed price catalog** (see `guides/cost-attribution-guide.md` for the
full design). This is the one genuinely new piece of infrastructure the
change introduces — everything else is a modification to what already ships
in this accelerator.

Two Azure Functions (Node.js v4 programming model, TypeScript), meant to run
as an Azure Functions app alongside the existing `llm-usage-ingestion` Logic
App and sharing its Cosmos DB account:

## `refreshPricingCache` — Timer trigger, daily

Runs once every 24h (default `0 0 3 * * *`, same "early hours" window the
existing ingestion Logic App uses for its first run of the day).

1. Reads your source-of-truth price list — by default a checked-in JSON file
   (`priceSource.json`, same shape as
   `src/usage-reports/model-pricing-generated-extended.json`), swap
   `loadSourcePrices()` in `src/lib/priceSource.ts` for a call to your real
   pricing feed (Azure Retail Prices API, an internal rate-card service,
   whatever you actually bill against).
2. Diffs it against the latest `docType: "priceSnapshot"` document per model
   in the `model-pricing` container. Unchanged models are skipped entirely
   (no write, no new version).
3. For every model whose price changed: writes a **new** dated snapshot
   document (`{model}-v{n+1}`, `effectiveFrom = now`, previous snapshot's
   `effectiveTo` is set to `now`) — snapshots are **append-only**, never
   edited in place, so historical chargeback stays correct even after a
   price change.
4. Updates the `current::{model}` pointer document to the new snapshot.
5. Writes one small `current-pricing.json` blob to the `pricing-cache`
   Storage container — this is what the customer-facing price page reads.
   It never queries Cosmos directly, so a page load is one cheap blob read,
   not a live query.

## `enrichPricing` — HTTP trigger, called once per Logic App run

Called from `llm-usage-ingestion/workflow.json`'s new `Enrich_With_Pricing`
step, **once per batch** (not once per record) — see the `Http` action added
right after `Parse_Metrics_Logs` and before `For_each`.

Input: the same array `Parse_Metrics_Logs` already produces (one object per
usage record, each with a `timestamp` and `deploymentName`).

For each record, resolves the price snapshot **effective at that record's
own timestamp** (a real Cosmos SQL query — `effectiveFrom <= timestamp AND
(effectiveTo IS NULL OR effectiveTo > timestamp)` — not "today's price"),
loading the full pricing catalog into memory once per invocation rather than
querying per record. Returns the same array with a `cost` object merged
onto every item:

```json
{
  "inputCost": 0.00123,
  "outputCost": 0.00456,
  "cachedCost": 0.00012,
  "audioCost": 0,
  "reasoningCost": 0,
  "totalCost": 0.00591,
  "currency": "USD",
  "pricingVersion": 3,
  "calculationMethod": "tokens"
}
```

**PTU / `percentage` models are intentionally left at `totalCost: null`.**
Proportionally distributing a fixed reserved-capacity cost needs the
denominator of *all* consumers' usage share over the billing period, which a
single request can't know in isolation — that calculation still belongs in
Power BI at report time, exactly as the accelerator does it today. Only
`tokens`-priced models get their cost baked in at ingestion.

## Wiring into the Logic App

See the diff in `src/usage-ingestion-logicapp/llm-usage-ingestion/workflow.json`:
a new `Enrich_With_Pricing` `Http` action calls this function's endpoint,
`For_each` now iterates over its output instead of the raw parsed metrics,
and `Create_Usage_Log`'s document template includes the new `cost` fields.

## Deployment

`bicep/infra/modules/pricing-service/pricing-service.bicep` provisions a
Node 20 Function App on EP1/ElasticPremium (matching
`quota-service.bicep`'s sibling module — Consumption doesn't reliably
support regional VNet integration, which this module's optional
`functionAppSubnetId` param needs) with system-assigned managed
identity, granted:

- `Cosmos DB Built-in Data Contributor` on the existing Cosmos account (read
  `model-pricing`, write `model-pricing`) — via the accelerator's own
  reusable `cosmos-sql-role-assignment.bicep` module, the same one
  `quota-service.bicep` calls. **Corrected**: earlier revisions of this
  module described this grant in a comment without actually creating
  it — every Cosmos call in `enrichPricing`/`refreshPricingCache` would
  have failed with an authorization error until this was fixed. Pass
  the Cosmos account's **name** (not just its endpoint) via the
  module's `cosmosDbAccountName` param for this to work.
- `Storage Blob Data Contributor` on a new `pricing-cache` blob container

Wire it into `bicep/infra/apim-gateway-upgrade/supporting-services.bicep`
(or your main orchestration) the same way the existing Logic App module is
wired in — pass through the Cosmos account name/endpoint and a new storage
account (or reuse the existing one from the Logic App's `services/logic-app.bicep`
if you'd rather not stand up a second storage account).

**Not attempted here**: the exact module wiring into the full deployment
orchestration wasn't guessed blind — the bicep module below is complete and
deployable standalone, but connecting its parameters into the specific
orchestration file (subscription IDs, existing resource references, naming
conventions your fork may already have customized) needs a pass by whoever
owns that deployment, since guessing those risks producing bicep that looks
right but silently fails to compile against your actual parameter files.

## Testing

**Corrected** (this session's own code-quality review found this service
had zero test files and a dead `npm test` script — `node --test dist/test`
pointed at a directory that never existed): both handlers, and every
`src/lib/*.ts` file except the thin direct-Cosmos wrapper functions (see
below), now have real, executed test coverage — **51 tests**, run and
passing (`npm install && npm run build && npm test`, verified clean, not
just asserted).

Same DI seam pattern as `src/quota-service` throughout: `enrichPricing`
and `refreshPricingCache` each take an optional, defaulted `deps`
parameter (production `app.http`/`app.timer` call sites never pass a
third argument, so real behavior is byte-for-byte unchanged); `cosmos.ts`
exports `_resetClientCacheForTests()`; `priceCacheBlob.ts` exports
`getContainerClient()` as its own DI seam (mirroring
`quota-service/src/lib/email.ts`'s `getTransporter()` — `BlobServiceClient`
construction is lazy, so it can be exercised directly in tests without a
live storage account).

**Real, measured coverage** (`node --test --experimental-test-coverage
dist/tests/*.js`, run in this session, not estimated): **100% line
coverage** on both Function handlers and `costCalculator.ts`/
`priceSource.ts`/`priceCacheBlob.ts`; **97.30%/90.91%/93.66%**
line/branch/function overall. The one residual gap, stated plainly: four
one-line delegator functions in `cosmos.ts` (`loadAllSnapshots`,
`loadCurrentPointers`, `upsertSnapshot`, `upsertCurrentPointer`) call
straight into the Cosmos SDK's own query/upsert methods, which — unlike
client *construction* — actually attempt a network call the moment
they're invoked, so they can't be exercised without either a live Cosmos
account or mocking the SDK itself. Every call site that uses them
(`enrichPricing`/`refreshPricingCache`) is fully tested via the `deps`
seam instead; this is the same class of accepted, disclosed edge
`quota-service/README.md` already names for its own thin, un-faked
boundary.

```bash
cd src/pricing-service
npm install
npm run build
npm test
```

## Local development

```bash
cd src/pricing-service
npm install
cp local.settings.json.example local.settings.json   # fill in your Cosmos/Storage connection info
npm run build
func start
```
