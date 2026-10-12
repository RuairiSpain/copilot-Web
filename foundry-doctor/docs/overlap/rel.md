# Overlap fragment: REL (FND-REL-001..009)

Research-state semantics are defined in `research-status.md`. Defender and Advisor are unresearched,
and overlap decisions remain provisional until configured-tool searches and expert reviews complete.

Researched 2026-10-04 against the local clones in `docs/development/phase-0-research-brief.md`, plus raw GitHub
files for the Azure AI Search articles (`MicrosoftDocs/azure-ai-docs/main/articles/search/`), which are not in the
clones. Property names and enums come from `Azure/azure-rest-api-specs` (versions in each rule's
`compatibility.apiVersions`). The WARA clone (`Azure/Azure-Proactive-Resiliency-Library-v2`) only has `docs/` in its
working tree; recommendations were read with `git show HEAD:azure-resources/<provider>/<type>/recommendations.yaml`.
Defender recommendation IDs and Advisor IDs were not available in any clone and are left empty.

## Decision table

| ID | Decision | Coverage | PSRule | Azure Policy (GUID) | Checkov / other | WARA (aprlGuid prefix) | Basis | Status |
|---|---|---|---|---|---|---|---|---|
| FND-REL-001 | adapt | partial | Azure.Search.QuerySLA, Azure.Search.IndexSLA | none | CKV_AZURE_209, CKV_AZURE_208 | b376281d, dff62efe | reliability, waf, wara | verified |
| FND-REL-002 | adapt | partial | Azure.Storage.UseReplication | bf045164 (geo-redundant audit only) | none | e6c7e1cc | reliability, waf, wara | verified |
| FND-REL-003 | adapt | partial | Azure.Cosmos.AvailabilityZone, Azure.Cosmos.ContinuousBackup | none | none | 921631f6, e544520b | reliability, waf, wara | verified |
| FND-REL-004 | native | none | none | related only: 78460a36 (denyAction delete) | none | none | reliability, waf | verified |
| FND-REL-005 | native | none | none (Azure.Search.SKU is a different check) | none | none | none | platform | verified |
| FND-REL-006 | native | none | none | none | none | none | reliability, waf | verified |
| FND-REL-007 | native | none | none | none | none | 43663217, 9cabded7, dff62efe, 61187af4 (context only) | opinion, reliability, waf, wara | product-opinion |
| FND-REL-008 | native | none | none | none | none | 0c193899 | reliability, waf, wara | verified |
| FND-REL-009 | adapt | partial | Azure.APIM.AvailabilityZone, Azure.APIM.MultiRegion | none | none | baf3bfc0, 740f2c1c, af4f88cb | reliability, waf, wara | product-opinion |

Counts: 7 verified, 2 product-opinion, 0 dropped, 0 proposed. Decisions: adapt 4, native 5, reuse 0, wrap 0, drop 0.

## Basis and severity

- Only FND-REL-005 has basis `platform` (error in every profile). Its sources state the constraints: the ARM spec
  (`replicaCount` 1..12, 1..3 on basic; `partitionCount` 1, 2, 3, 4, 6 or 12; HighDensity only on standard3 and at most
  3 partitions) and the Learn capacity article (36 search unit maximum, N/A cells in the combinations table).
- All other REL rules are WAF/WARA recommendations. Severity is info in dev and test, warning in prod at most
  (REL-007 is info in every profile because it is a question). No REL rule is an error outside REL-005.
- REL-009 evaluates only against a declared SLA/zone/region resilience objective. It does not infer
  an objective or assume APIM zone availability in a region.
- The Search SLA replica counts (2 for query, 3 for query and indexing) are documented Learn facts, but a lower count
  deploys fine, so REL-001 is not platform basis.

## Changed to product-opinion

- FND-REL-007 (Multi-region/DR plan question): the Foundry resiliency article documents DR options (hot/hot, hot/warm,
  hot/cold) but no source says every project needs multi-region. No deterministic pass/fail is defined; the rule emits
  info and records a declared answer. The configuration key in the `fix` example is illustrative only.

## Honest overlap assessment

REL-001, 002, 003 and 009 test properties PSRule already tests. The `adapt` decision rests on scoping to resources a
Foundry project actually uses, live control-plane evidence, and per-profile severity. If those are not delivered in
Phase 4, downgrade to `reuse` (map PSRule IDs only). REL-004 (locks), REL-005, REL-006 and REL-008 have no PSRule,
Policy or Checkov equivalent in the clones. For locks, Azure Policy `denyAction` (78460a36) is a different control and is
noted as related.

## Unverified

- Learn pages for Azure AI Search (`search-reliability`, the AZ section) could not be fetched
  (`raw.githubusercontent.com/MicrosoftDocs/azure-ai-docs/main/articles/search/search-reliability.md` returned 404 and
  `learn.microsoft.com` is blocked). Replicas-to-availability-zone placement therefore rests only on the WARA text
  (pgVerified false) and is not asserted by REL-001.
- REL-005 Basic tier partitions: ARM spec says values above 1 are valid only for standard SKUs; the limits article says
  Basic services created after April 3, 2024 (supported regions) can have 3 partitions and 9 SU; the same article's
  subscription table says 3 SU for Basic. The rule reports Basic with more than one partition as uncertain. Service
  creation date is not visible in ARM. Free and serverless replica/partition behaviour is documented only as N/A.
- REL-005 mapping of portal tier names (S1, S2, S3, L1, L2) to ARM `sku.name` values was not read in a source; the rule
  only depends on `basic` and `standard3`.
- REL-002: the Foundry article says both "use GZRS for Agent Service storage" and "Foundry projects don't support default
  storage account failover using GRS/GZRS/RA-*". Both are quoted in the rule; reviewer to reconcile. Newest stable storage
  spec in the clone is 2026-09-01.
- REL-003: stable Cosmos spec 2026-03-15 exists but only its examples are in the clone; property names were verified in
  2025-10-15. PSRule's claim that `isZoneRedundant` cannot change after creation is not in a primary source read and is
  not used. Region support for zones was not read.
- REL-006: `circuitBreaker` compatibility is recorded only for 2025-09-01-preview, the reviewed
  primary specification that contains the property. Reviewed stable 2024-05-01 has no
  `circuitBreaker`; it and every other unlisted version are unsupported evidence and produce
  Skipped, never a missing-property finding. The Learn example's different preview version is not
  used as schema-compatibility evidence. Whether a newer stable APIM version has it was not checked.
- REL-008: which provisioned SKUs and models support spillover beyond the "global and data zone provisioned deployments"
  recommendation was not read. Per-request header use is invisible to the doctor.
- REL-009 recommends the least tier and capacity satisfying the declared SLA, throughput, zone and
  regional objective; it does not prescribe Premium with two units. Premium/multi-region is relevant
  only for a declared regional-resilience objective, and the cost tradeoff is explicit. There is a
  conflict between the APIM features table (Standard v2: no availability zones) and the zones article
  (Standard v2 and Premium v2 can enable zone redundancy); StandardV2 is reported uncertain. SLA percentages per tier
  and regional zone availability were not read. `PremiumV2` appears in the 2025-09-01-preview `SkuType` enum only.
- Defender and Advisor mappings were not researched (no clone content); lists are empty.

## Proposed new rules (fragment only, no catalogue files)

1. Search quota and capacity feasibility (deployment feasibility): check planned Search services against the
   per-subscription, per-region service count by tier (for example 16 Basic or S1 per region, from the limits article),
   per-tier index and indexer counts, and storage per partition by creation date and region. Needs live subscription data.
2. Dedicated state stores (supportability): flag a Cosmos DB account, Search service or Storage account used by more
   than one Foundry account or by non-agent workloads. Source: the Foundry resiliency article's single-responsibility
   guidance. Evidence is connection and capability host correlation (Foundry-specific, no PSRule equivalent).
3. Recovery readiness (release readiness): Agent Service in Standard mode, project using a user-assigned managed identity,
   Cosmos `enableAutomaticFailover` true with at least two locations, and unique organisation-specific names for
   Cosmos and Search (restore recreates the original name). All stated in the same Foundry article; most are
   recommendations, so product-opinion or info severity unless further sourced.

## PRD ambiguity

- PRD section 20 gives REL-009 phase "4/8" and REL-006 phase 8; the seed phases were kept. REL-009's Phase 8 part depends
  on how the AI gateway relationship to a Foundry project is detected, which the PRD does not define in this section.
