# Cost estimation (`foundry-doctor cost`)

`cost` builds an **advisory** monthly estimate for fixed-capacity resources in
the compiled ARM plan. It uses public Azure retail prices and a local cache.
It does **not** estimate token usage, traffic, storage growth, discounts,
contracted pricing, taxes, or invoice-only meters.

## What is included

- Azure AI Search dedicated units (`replicaCount x partitionCount`)
- API Management fixed-capacity units (`sku.capacity`)
- Cosmos DB provisioned throughput (`throughput / 100 RU/s`)

## What is excluded or reported as unsupported

- Token-based model deployments such as `Standard` and `GlobalStandard`
- PTU / provisioned model deployments whose generic ARM SKU to retail-meter
  mapping is still **UNVERIFIED**
- Cosmos DB autoscale pricing
- Storage, bandwidth, backup, and other consumption-based meters

Unsupported or stale pricing is listed explicitly; it is never guessed.

## Flags

| Flag | Purpose |
|---|---|
| `--environment` | Resolve one azd environment before estimating. |
| `--compare <env>` | Estimate additional environments and show deltas against the primary environment. |
| `--currency` | Retail currency code (default `USD`). |
| `--hours-per-month` | Hourly-meter assumption (default `730`). |
| `--offline` | Use only the local cache; do not call the Retail Prices API. |
| `--price-cache` | Cache file path (default `.foundry-doctor/cache/prices.json`). |
| `--format` | `console`, `json`, or `markdown`. |

## Cache behaviour

The command writes public retail results to `.foundry-doctor/cache/prices.json`
by default. Cached prices older than 30 days are still shown, but the report is
marked incomplete and calls that out directly.

## Limitations

- The estimate is for **planning comparison**, not billing truth.
- ARM expressions that leave SKU, capacity, region, or throughput unresolved are
  reported as unsupported.
- Environment comparisons are only as good as the compiled plan. If a Bicep
  template keeps capacities parameterized, the report cannot invent per-env
  values.

## Related rules

- `FND-COST-001` checks for Cost Management budgets and notifications in the
  compiled plan.
- `FND-COST-002` warns when development environments use production-sized fixed
  SKUs.
- `FND-COST-004` is implemented as this informational command.
