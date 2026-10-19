# ADR-025: Phase 7 cost intelligence decisions and deferrals

Status: Accepted
Date: 2026-10-18
Decided by: the Phase 7 fixer (go-implementer, fix loop 1), from the Phase 7 round 1 reviews (`foundry-lead` and `azure-black-belt`, both REJECT)

## Context

The round 1 reviews of Phase 7 (PRD section 15) asked for code fixes and, where a fix belongs to a later phase or needs facts that
cannot be verified in this loop, a recorded decision. Fix loop 1 had no Go toolchain and no direct network access. Microsoft Learn
pages and a search-engine snapshot of the public Retail Prices endpoint could be read (2026-10-18). This ADR records the design
choices and the items that are not delivered. It does not waive any PRD section 15 DoD item.

## Decision

1. **FND-COST-001 control-plane budget discovery is deferred.** The catalogue entry promised a Phase 7 Budgets API list on the
   subscription and resource group. That check is not delivered. It needs a new allow-listed operation in the shared
   `internal/azure` transport, `Microsoft.Consumption/budgets/read` (Reader or Cost Management Reader), and a rule that budgets
   at billing scopes are invisible to a subscription Reader, so "none found" must be uncertain, never a FAIL
   (https://learn.microsoft.com/azure/cost-management-billing/costs/assign-access-acm-data, cited by the reviewer, not re-fetched).
   Target: the live-check work in Phase 8 or later, owned by the `internal/azure` owner. FND-COST-001 is amended to version 4:
   `phases: ['2']`, `inputs: [bicep-arm]`, the control-plane sentence and the `control-plane-unavailable` test are removed, and the
   evidence names this ADR. Phase 7 also adds the documented 0 to 1000 threshold range check (`budget-threshold-out-of-range`).
2. **FND-COST-002 does not read `azure.yaml` deployments.** The entry is amended to version 4: `inputs: [bicep-arm]`, the evidence
   says "in the compiled template", the `fix` is a Bicep deployment. Matching of the documented deployment types is
   case-insensitive and uses one shared table (`internal/cost/armcost`). `sku.name` now follows parameter-file and module-parameter
   substitution (`internal/armmodel`, `SKUName` derived from the substituted `SKU["name"]`).
3. **Retail Prices API transport exception.** The client in `internal/cost/prices` is not routed through the `internal/azure`
   allow-list (ADR-006 item 6) in this loop: `internal/azure` is a shared package owned by other phases, and this endpoint is
   unauthenticated (no token, no tenant data). Equivalent mitigations are in the client: GET only; fixed origin
   `https://prices.azure.com/api/retail/prices`; pinned `api-version=2023-01-01-preview` (filter values are case sensitive from that
   version); `priceType eq 'Consumption'` in the filter and `type == Consumption` checked client side; quoted `currencyCode='XXX'`
   (documented form) and the returned `currencyCode` verified; redirects are never followed; a `NextPageLink` is followed only on the
   origin of the first request; at most 20 pages; 4 MiB body cap and 20 s timeout per request; `--offline` makes no request.
   Registering an operation `cost.retailprices.list` in the `internal/azure` allow-list with a replay test is deferred to the owner
   of `internal/azure` (Phase 9 hardening). Source: https://learn.microsoft.com/rest/api/cost-management/retail-prices/azure-retail-prices
   (verified 2026-10-18).
4. **Price selection never guesses.** Candidates are current (`effectiveStartDate` not in the future, `effectiveEndDate` not past),
   `tierMinimumUnits == 0`, hourly when the meter is hourly, in the requested currency. Primary meter rows (`isPrimaryMeterRegion`)
   are preferred; when a region only has non-primary rows (seen for `Standard S1 Unit` in `koreacentral` on 2026-10-18), the row is
   used and the line says so. More than one distinct remaining price is reported as unsupported (ambiguous meter). A live
   ambiguous, wrong-currency or refused-paging answer is never replaced by a cached price; a live "not found" or transport failure
   falls back to the cache with a visible notice.
5. **Fixed costs that are listed, not priced (unsupported, estimate incomplete):**
   - Foundry provisioned deployments (`GlobalProvisionedManaged`, `DataZoneProvisionedManaged`, `ProvisionedManaged`): the PTU
     count, deployment type and region are reported; the PTU retail meter mapping is UNVERIFIED; reservations are not modelled.
   - Cosmos DB autoscale (billed at 10-100% of the maximum per hour at the autoscale rate), multi-region-write accounts, free-tier
     accounts, zone-redundant regions, other Cosmos API types (MongoDB, Cassandra, Gremlin, Table), and any account whose
     `locations`, `enableMultipleWriteLocations`, `enableFreeTier` or `capabilities` is unresolved.
     Provisioned RU/s are billed in every account region (azure.microsoft.com Cosmos DB standard-provisioned pricing page; the same
     table on www.azure.cn), so a single-region-write account gets one line per region.
   - API Management `additionalLocations` units (per-region billing is not mapped); `PremiumV2` and other unmapped SKUs.
   - Azure AI Search confidential compute (`computeType: Confidential`, separate "CC" meters) and `hostingMode: HighDensity`;
     an absent `replicaCount` or `partitionCount` (the template reference gives no default: "If specified, ...").
   - Resources whose condition is not a literal, and every copy-looped resource (the instance count is not evaluated). A literal
     false condition means the resource is not deployed and it is not listed.
   - Regions in sovereign clouds (the API "only supports prices on Commercial Cloud").
6. **Unknown resource types are excluded, not unsupported.** Every deployed resource that the estimator does not model is listed
   under Excluded ("resource type not modelled ...") so nothing disappears from the report. This does not mark the estimate
   incomplete (otherwise no real template could ever be compared); instead every total and comparison is labelled "covered
   resources only".
7. **Low equals high.** Every estimated line is a fixed hourly meter; the drivers where a range would apply (autoscale, PTU) are
   unsupported. The PRD "range" therefore collapses; `docs/cost.md` and the report say why.
8. **Meter names.** Verified from a recorded Retail Prices API response (2026-10-18): Search `Basic Unit`, `Standard S1 Unit`,
   `Standard S3 Unit`; APIM `Developer Unit`, `Standard Unit`, `Basic v2 Unit` (serviceName and productName `API Management`).
   UNVERIFIED (naming pattern only): Search `Standard S2 Unit`, `Storage Optimized L1 Unit`, `Storage Optimized L2 Unit`; APIM
   `Basic Unit`, `Premium Unit`, `Isolated Unit`, `Standard v2 Unit`; Cosmos `100 RU/s`. A wrong name yields "no retail price"
   for the line (unsupported), never a guessed price; estimated lines on an unverified name carry an UNVERIFIED note.
9. **FND-COST-003 registration.** `internal/rules/cost.Register()` ships FND-COST-003, a Phase 8 rule (gateway token limits) that
   was implemented early. Its semantics are owned by Phase 8; Phase 7 only discloses the early registration. The catalogue entry is
   unchanged.
10. **Azure Advisor cost recommendations** (PRD tool table, "optional deployed advisory evidence") are not integrated. Deferred to the
    live-check work with item 1.

## Consequences

- `foundry-doctor cost` and FND-COST-004 read the plan through the same `internal/cost/armcost` helpers.
- The price cache schema is version 2; a version 1 cache is ignored (its keys lacked price type and API version).
- The maintainer must run `go build ./...`, `go test ./...` and `scripts/claude/verify-phase.sh --strict`, and regenerate the
  catalogue docs with `go run ./cmd/rulecatalog generate-docs` and `generate-overlap`; fix loop 1 had no toolchain.

## Post-merge gate loop 1 additions (2026-10-19)
- The price cache is untrusted input (`prices.Record.Valid`): a record is used only if its price is finite and not negative, its
  currency, unit and type match the query and its retrieval date is not in the future. An hourly meter means exactly one hour
  (`1 Hour` or `1/Hour`). `schemas/price-cache.schema.json` documents the file.
- APIM Premium with more than one unit is not priced: units after the first cost less, and the retail meter for them is unverified.
- Stale prices stay in the total but are marked on each line and on the total, and the oldest retrieval date is reported.
