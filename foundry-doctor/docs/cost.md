# Cost estimation (`foundry-doctor cost`)

`cost` builds an **advisory** monthly estimate for fixed-capacity resources in
the compiled ARM plan. It uses public Azure retail prices and a local cache.
It does **not** estimate token usage, traffic, storage growth, discounts,
reservations, savings plans, contracted pricing, taxes, or invoice-only meters.
Totals cover **the estimated lines only** ("covered resources only"). Cost
output never fails CI: the command exits 0 when it can render a report, and the
COST rules use info or warning severities, below the default `--fail-on error`.

Design decisions and deferrals: [ADR-025](decisions/ADR-025-phase-7-cost-decisions-and-deferrals.md).

## How every resource is classified

Each deployed resource of the compiled plan lands in exactly one list:

| List | Meaning | In the total | Marks the estimate incomplete |
|---|---|---|---|
| Estimated | Priced with a public retail meter | yes | no |
| Excluded | Consumption-based, or a resource type the estimator does not model | no | no |
| Unsupported | Fixed cost that cannot be priced (with the reason) | no | yes |

A resource whose condition is the literal `false` is not deployed and is not
listed. A non-literal condition or a copy loop (the instance count is not
evaluated) makes a modelled resource **unsupported**. `sku.name`, `sku.capacity`,
regions and properties are read after parameter-file and module-parameter
substitution; a value that stays an ARM expression is **unsupported**.

## What is estimated

| Driver | Quantity | Meter (Retail Prices API) |
|---|---|---|
| Azure AI Search dedicated tiers (`basic`, `standard`, `standard2`, `standard3`, `storage_optimized_l1/l2`), default compute and hosting mode | `replicaCount x partitionCount` | `Azure Cognitive Search` / `Azure AI Search`, `<Tier> Unit` |
| API Management fixed tiers (primary region) | `sku.capacity` | `API Management` / `API Management`, `<Tier> Unit` |
| Cosmos DB standard (manual) provisioned throughput on `throughputSettings` (`properties.resource.throughput`) and on SQL databases and containers (`properties.options.throughput`) | `throughput / 100`, one line per region of the parent account's `properties.locations` | `Azure Cosmos DB`, `100 RU/s` |

Meter names seen in a recorded API response on 2026-10-18: Search `Basic Unit`,
`Standard S1 Unit` (sku `standard`), `Standard S3 Unit`; API Management
`Developer Unit`, `Standard Unit`, `Basic v2 Unit`. Every other mapped meter
name (Search `Standard S2 Unit`, `Storage Optimized L1/L2 Unit`; API Management
`Basic Unit`, `Premium Unit`, `Isolated Unit`, `Standard v2 Unit`; Cosmos
`100 RU/s`) follows the naming pattern only and is **UNVERIFIED**; such a line
carries an UNVERIFIED note, and a wrong name gives "no retail price"
(unsupported), never a guessed price.

## Excluded (not in the total)

- Token-based model deployments, matched case-insensitively against the
  documented deployment types
  ([deployment types](https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types),
  verified 2026-10-18): pay-per-token `GlobalStandard`, `DataZoneStandard`,
  `Standard`; batch `GlobalBatch`, `DataZoneBatch` (billed per token at a 50%
  discount); `DeveloperTier`. For these, `sku.capacity` is a throughput
  allocation and is not priced.
- Search `free` and `serverless`, API Management `Consumption`, serverless
  Cosmos DB accounts.
- Cosmos DB account-level costs (storage, backup, bandwidth) and databases or
  containers without dedicated throughput.
- Every other resource type ("not modelled by the fixed-capacity estimator";
  any cost it has, such as App Service plans, private endpoints, gateways,
  firewalls or Log Analytics, is not included).

## Unsupported (fixed cost listed but not priced)

- Provisioned model deployments (`GlobalProvisionedManaged`,
  `DataZoneProvisionedManaged`, `ProvisionedManaged`): the note gives the PTU
  count (`sku.capacity`), the deployment type and the parent account region.
  PTUs bill hourly per deployed PTU, but the PTU retail meter mapping is
  **UNVERIFIED**; reservations are not modelled.
- A deployment with no `sku.name`, an unresolved `sku.name`, or a value that is
  not a documented deployment type ("unknown or unresolved deployment SKU").
- Cosmos DB autoscale (`autoscaleSettings.maxThroughput`; billed at 10-100% of
  the maximum each hour at the autoscale rate), multi-region-write accounts,
  free-tier accounts, zone-redundant regions, other Cosmos APIs (MongoDB,
  Cassandra, Gremlin, Table), and a throughput resource whose parent account is
  not in the plan or has unresolved `locations`.
- API Management `additionalLocations` units (count and region are listed),
  `PremiumV2` and other unmapped tiers.
- Search `computeType: Confidential` (separate "CC" meters) and
  `hostingMode: HighDensity`; an absent `replicaCount` or `partitionCount` (the
  specification gives no default, so none is assumed).
- Regions in sovereign clouds: the Retail Prices API covers the Commercial cloud
  only.
- A retail price that is unavailable, ambiguous (more than one distinct
  pay-as-you-go price), or returned in another currency.

## Price selection

The client calls `https://prices.azure.com/api/retail/prices` with
`api-version=2023-01-01-preview` (filter values are case sensitive from this
version), `currencyCode='<CODE>'` (documented quoted form) and a `$filter` on
`serviceName`, `armRegionName`, `meterName`, `productName` and
`priceType eq 'Consumption'`
([Retail Prices API](https://learn.microsoft.com/rest/api/cost-management/retail-prices/azure-retail-prices),
verified 2026-10-18). Then:

- only `type == Consumption` rows in the requested currency are used;
  `DevTestConsumption` and `Reservation` rows (a Reservation price is the term
  price even when its unit is `1 Hour`) never are;
- tiered rows (`tierMinimumUnits > 0`), expired rows (`effectiveEndDate` in the
  past) and rows not yet effective are dropped;
- primary meter rows (`isPrimaryMeterRegion`) are preferred; if a region only
  has non-primary rows, the line says so;
- more than one remaining distinct price is **ambiguous** and the line is
  unsupported; no minimum is taken;
- a returned `currencyCode` that differs from the request makes the line
  unsupported; it is never relabelled. Non-USD prices are reference values
  (Microsoft prices Azure services in USD); the report says so;
- `NextPageLink` is followed only on the origin of the first request, at most
  20 pages; redirects are not followed; responses are capped at 4 MiB.

## Range and rounding

Low equals high: every estimated line is a fixed hourly meter, and the drivers
where a range would apply (autoscale at 10-100% of the maximum, PTU) are listed
as unsupported. Each line is `quantity x unit price x hours`, rounded to two
decimals; totals are the sum of the rounded lines. Currencies without minor
units are still shown with two decimals.

## Flags

| Flag | Purpose |
|---|---|
| `--environment` | Resolve one azd environment before estimating. |
| `--compare <env>` | Estimate additional environments and show deltas against the primary environment (covered resources only; not comparable when either side is incomplete). |
| `--currency` | ISO 4217 currency code (default `USD`); the API's answer is verified. |
| `--hours-per-month` | Hourly-meter assumption (default `730`). |
| `--offline` | Use only the local cache; do not call the Retail Prices API. |
| `--price-cache` | Cache file path (default `<dir>/.foundry-doctor/cache/prices.json`). |
| `--format` | `console`, `json`, or `markdown`. |

## Cache behaviour

Live results are written to the price cache (schema version 2; keys include the
API version, price type, currency and hourly flag). A version 1 cache is
ignored. Cached prices older than 30 days are shown, but the report is marked
incomplete and says so. If the live API no longer lists a meter or the call
fails, the cached price is used with a notice that names the cause; an
ambiguous or wrong-currency live answer is never replaced by a cached price. A
cache that cannot be written does not discard the live price; the report notes
it. The cache file is untrusted input and is described by `schemas/price-cache.schema.json`. A record is used only if its
price is finite and not negative, its currency and type match, its unit is exactly one hour for an hourly meter (`1 Hour` or
`1/Hour`) and its retrieval date is not in the future; otherwise that line is reported as unsupported ("cached retail price
rejected"), never priced. A cached price older than 30 days is still shown, marked STALE on its line, and flagged on the total
with "(includes stale prices) (estimate incomplete)"; the report shows the oldest retrieval date. API Management Premium with
more than one unit is not priced, because units after the first are billed at a lower rate and that meter is not verified.
`--hours-per-month` must be 0 (the default, 730) to 744.

Add `.foundry-doctor/cache/` to `.gitignore`.

## Limitations

- The estimate is for **planning comparison**, not billing truth.
- Environment comparisons are only as good as the compiled plan: values that
  stay parameterised are unsupported, never invented.
- Azure Advisor cost recommendations and actual spend are not read (ADR-025).
- `samples/cost-fixed` is illustrative only; its model names, versions and PTU
  count are placeholders.

## Related rules

- `FND-COST-001` checks Cost Management budgets and notifications in the
  compiled plan (offline only; the control-plane budget list is deferred by
  ADR-025).
- `FND-COST-002` warns when development environments use production-sized fixed
  SKUs (compiled template only).
- `FND-COST-004` reports, at check time, what makes this estimate incomplete,
  using the same extraction as the command.
- `FND-COST-003` (gateway token limits) is a Phase 8 rule that is registered
  early; it is not part of the estimate.
