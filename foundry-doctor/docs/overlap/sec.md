# Overlap fragment: SEC (FND-SEC-001..014)

Research-state semantics are defined in `research-status.md`; unavailable Defender/Advisor sources
are unresearched, not searched-no-match, and affected decisions are provisional.

Researched 2026-10-04 against the local clones listed in `docs/development/phase-0-research-brief.md`.
Property names and enums come from `Azure/azure-rest-api-specs` (versions in each rule's
`compatibility.apiVersions`). Overlap IDs come from PSRule for Azure docs and rule YAML, Azure Policy
built-in definition GUIDs, and Checkov `check_id` values read in the clones. Defender recommendation
IDs and Advisor IDs were not available in any clone and are left empty.

## Decision table

| ID | Decision | Coverage | PSRule | Azure Policy built-in (GUID) | Defender | Checkov / Bicep linter | Status |
|---|---|---|---|---|---|---|---|
| FND-SEC-001 | adapt | partial | Azure.AI.DisableLocalAuth | 71ef260a, 14de9e63, 55eff01b | - | CKV_AZURE_236 | verified |
| FND-SEC-002 | adapt | partial | none (no Search local-auth rule) | 6300012e, 4eb216f2, d45520cb | - | none | verified |
| FND-SEC-003 | adapt | partial | Azure.Cosmos.NoSQLLocalAuth | 5450f5bd, dc2d41d1 | - | CKV_AZURE_140 | verified |
| FND-SEC-004 | adapt | partial | Azure.Storage.LocalAuth, BlobPublicAccess, MinTLS, SecureTransfer | 8c6a50c6, 4fa4b6c0, 13502221, fe83a0eb, 404c3081, f81e3117 | - | CKV_AZURE_59, 34, 44, 3 | verified |
| FND-SEC-005 | reuse | full | Azure.KeyVault.SoftDelete, PurgeProtect, RBAC | 1e66c121, 0b60c0b2, 12d4fa5e | - | CKV_AZURE_111, 110, 42 | verified |
| FND-SEC-006 | native | partial | none (firewall rules check defaultAction only) | 12339a85 (Cosmos 0.0.0.0 only) | - | none | verified |
| FND-SEC-007 | native | partial | none | [Preview] data-plane: af253d37, f3a9c2e0, c1ad46c6, 930f48f9, 595f98a5, 9224c1cc | - | none | verified |
| FND-SEC-008 | adapt | partial | Azure.Defender.Storage, KeyVault, CosmosDb (no AI rule) | c2c0c6d8, 7e92882a, 0e6763cc, 640d2586, adbe85b5 | Microsoft.Security/pricings | CKV_AZURE_84, 87 | verified |
| FND-SEC-009 | native | none | none (Azure.Policy.* lint descriptors only) | none | - | none | product-opinion |
| FND-SEC-010 | adapt | partial | none | 67121cc7, 76a56461, 356da939, 1f905d99, 6fac406b | - | CKV_AZURE_100 | verified |
| FND-SEC-011 | native | none | none | none | - | none | verified |
| FND-SEC-012 | native | none | none | none | - | none | verified |
| FND-SEC-013 | native | none | none | none | - | none | verified |
| FND-SEC-014 | wrap | full | none | none | - | Bicep: outputs-should-not-contain-secrets | verified |

Counts: 13 verified, 1 product-opinion, 0 dropped. Decisions: adapt 6, native 6, reuse 1, wrap 1, drop 0.

## Honest overlap assessment

SEC-001, 002, 003, 004 and 005 test properties that PSRule, Azure Policy and Checkov already test. For 001 to 004 the
adapt decision rests on three stated Foundry-specific values: scoping to resources a Foundry account, project,
connection or capability host actually uses (verified property names: `CapabilityHostProperties.threadStorageConnections`,
`vectorStoreConnections`, `storageConnections`), one evidence model across compiled Bicep and the live account, and
per-profile severity. If those are not delivered in Phase 1, downgrade 001, 003 and 004 to `reuse` (map the PSRule
IDs only). SEC-005 is already `reuse` because no Foundry-specific semantics were verified. SEC-002 is the only one of
the five with no PSRule rule in the reviewed clone.

## Rules where the basis or severity was constrained

- No SEC rule is `platform` basis; none is forced to error in every profile. SEC-006 mixes a platform fact (private
  ranges unusable in IP rules on Storage per Learn, on Cosmos DB per spec) with a security judgement (open ranges).
  It is kept basis `security`, because listing `platform` would force error in dev for the open-range part.
- SEC-009 is `product-opinion`: the API facts are verified, but "an environment should assign these policies" is a
  design preference.
- SEC-010 and SEC-011 are informational by design (Info in all profiles, no fail state).

## Unverified

- Defender for Cloud recommendation/assessment IDs and Azure Advisor recommendation IDs for SEC-001..004, 008:
  no clone of Defender or Advisor docs exists in `REFS` (`azure-docs` has only api-management, resource-manager,
  dns, private-link, rbac, storage); left `[]`.
- Search, Key Vault, Cosmos DB and Cognitive Services Learn pages were not in the clones; facts come from REST specs.
  Search default when `disableLocalAuth` is absent is not stated in a primary source (the policy treats notEquals true as
  non-compliant, used as evidence).
- SEC-003: PSRule restricts its rule to the NoSQL API; how to detect API kind from ARM was not verified.
- SEC-004: Storage defaults depend on API version (spec: `supportsHttpsTrafficOnly` default true since 2019-04-01); older
  templates are classified `uncertain`.
- SEC-005: no Foundry source states that CMK Key Vaults must have purge protection; not claimed.
- SEC-006: private-range behaviour for Key Vault, Search and Cognitive Services IP rules is unverified and not checked.
- SEC-007: the set of Microsoft-managed RAI policy names beyond `Microsoft.DefaultV2` and `Microsoft.Default` was not
  enumerated; behaviour of an omitted `raiPolicyName` on a model deployment is documented only as "assigned
  Microsoft.DefaultV2 by default".
- SEC-009: `PolicyState.complianceState` enum values are not enumerated in the PolicyInsights 2024-10-01 spec (fetched from
  GitHub main; not in the local clone).
- SEC-011: `restrictOutboundNetworkAccess` has no description in the spec; the effect on a Foundry account is not
  verified, so the rule is informational only.
- SEC-012: `largeLanguageModel` diagnostic settings exist only in preview specs (2025-09-01-preview verified); stable
  2024-05-01 does not contain it.
- SEC-013: policy XML is not versioned; the check is based on the Learn policy reference (ms.date 08/18/2026).
- The seed schema has no field for "uncertain" scenarios; they are recorded as `uncertain:` entries under
  `tests.skipped`. `rule-template.yaml` referenced in the brief does not exist under `.claude/skills/add-foundry-rule/`;
  the Go `Rule` struct was used instead (input plane is `control-plane`, not `azure-control-plane`).

## PRD ambiguities

- SEC-006 title "reject private/open ranges" covers a platform constraint and a security recommendation in one rule.
- SEC-002 title "disabled or RBAC-only": the spec makes `disableLocalAuth: true` and `authOptions` mutually exclusive, and
  `aadOrApiKey` still accepts keys, so the rule treats only `disableLocalAuth: true` as a pass.
- SEC-005 and SEC-007 are phase `later`; SEC-005 delegates to PSRule, which is not scheduled anywhere before then.

## Proposed new rules (not added to the catalogue)

1. SEC-N1: Foundry connection `authType` vs target resource local-auth state (deployment feasibility). Report
   connections of authType ApiKey / AccountKey / SAS (`ConnectionAuthType` in the Cognitive Services spec) whose target
   Search, Cosmos DB or Storage account has local auth disabled. Primary-source support for the breakage itself was not
   verified, so it must be read from a live probe or docs before it is made a rule.
2. SEC-N2: Foundry account `publicNetworkAccess` and `networkAcls.defaultAction` consistency (supportability). The
   Storage Learn page warns that `publicNetworkAccess: Disabled` with `defaultAction: Allow` is still flagged by Defender
   and Advisor; an equivalent consistency check across Foundry, Search, Storage and Cosmos is a clear-evidence rule.
3. SEC-N3: Defender for AI `AIPromptEvidence` extension enabled and prompt-evidence privacy note (release readiness /
   cost visibility). The pricings spec lists the extension as available for the AI plan; the rule would pair SEC-008
   with SEC-012 so teams see that prompt snippets are exposed as alert evidence.
