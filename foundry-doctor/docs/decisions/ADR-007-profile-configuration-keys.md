# ADR-007: Profile configuration keys

Status: Accepted (the project owner delegated the key choices to the implementer on 2026-10-05; the Foundry lead rejected the first draft and approved the revision)
Date: 2026-10-05

## Context

PRD section 6 defines the unified configuration (`version`, `profile`, `inputs`, `rules`, `validation`, `policy`, `adoption`, `outputs`,
`advanced`), says unknown settings fail validation, and gives a precedence order that includes "environment-specific configuration". It
defines only two `policy` keys (`resourceScope`, `allowedExternalScopes`).

Phase 0 research found rules that cannot be evaluated from the template or from Azure alone and need the organisation to state a
requirement or a fact. The PRD does not name the keys, so each rule left them open (see the `notes` of the rules in the table below).

## Decision

1. **All organisation-supplied requirements live under `policy:`**, next to the two keys the PRD defines. They are not CLI flags and not
   rule-pack data, because they differ per organisation and per environment.
2. **A rule that needs a key and does not find one is `skipped`** with the reason `profile-key-missing:<key>`, naming the key that is missing
   (for example `policy.dataResidency.regions` when only `scope` is set), listed in the report summary. It is never passed and never failed on the strength of a guessed value (PRD section 2 and `CLAUDE.md`).
3. **Where a rule already has a baseline of its own, the key only adjusts it.** The baseline is not a guess; it is the rule's documented behaviour.
   The effective value of every key, including baselines, is printed in the report header as "effective policy".
4. **Environment-specific values** go under `environments.<name>.policy`, which is the "environment-specific configuration" layer of the PRD
   precedence (flags, then environment, then repository, then curated profile defaults). The curated `foundry-dev|test|prod` profiles set
   severities only and supply no organisation-specific values.
5. **The schema is closed.** Phase 1 ships `schemas/config.schema.json`; any key not listed here fails validation (exit 2).
6. **Matching rules.** Model identifiers (`format/name[/version]`), region names, SKU names and tag names compare exactly. Model, region and
   SKU comparisons are case-sensitive, as the Azure Policy built-in the model rule mirrors is; tag names compare case-insensitively (a deliberate
   difference from PSRule's `Azure.Resource.UseTags`, which is case-sensitive).

### Keys

| Key | Type | Used by | When absent |
|---|---|---|---|
| `policy.resourceScope` | `same-resource-group` \| `same-subscription` \| `any` | scope rules (PRD) | `same-resource-group` (a product decision; the PRD does not state a default) |
| `policy.allowedExternalScopes` | list of resource IDs | scope rules (PRD) | `[]` |
| `policy.environments.production` | list of environment names | FND-ENV-002, FND-ENV-003 | rule skipped |
| `policy.environments.nonProduction` | list of environment names | FND-ENV-002, FND-ENV-003 | rule skipped |
| `policy.environments.development` | list of environment names | FND-COST-002 | rule skipped |
| `policy.tags.required` | list of `{name, format?}` (`format` is an optional regular expression for the value) | FND-OPS-004 | rule skipped |
| `policy.tags.resourceTypes` | list of resource types | FND-OPS-004 | the rule's own list of taggable types |
| `policy.logRetention.minimumDays` | integer | FND-OPS-006 | rule skipped |
| `policy.models.allow` / `policy.models.deny` | lists of `format/name[/version]` | FND-OPS-010 | rule skipped |
| `policy.dataResidency.scope` | `global` \| `datazone-us` \| `datazone-eu` \| `datazone-apac` \| `geography` | FND-OPS-009 (the rule runs only when this is set) | rule skipped |
| `policy.dataResidency.regions` | list of Azure region names that satisfy the scope | FND-OPS-009 | rule skipped |
| `policy.dataResidency.deploymentSkus` | list of allowed deployment SKU names | FND-OPS-009 | the SKUs whose documented processing scope satisfies `scope` |
| `policy.disasterRecovery.declared` | boolean, optional `reference` (URL or path) | FND-REL-007 | informational question only, never a finding |
| `policy.network.publicAccess` | `forbidden` \| `entra-only` \| `allowed` | FND-NET-001 | `forbidden` (the rule's own baseline) |
| `policy.monitoring.publicTelemetry` | boolean | FND-NET-010 | `false` (the rule's own baseline) |
| `policy.managedByAzurePolicy` | list of `private-dns-zone-group` \| `diagnostic-settings` | FND-NET-003, FND-OPS-001 | `[]` (the template must create them) |
| `policy.knowledge.requireDocumentLevelAccess` | boolean | FND-IQ-009 (Phase 8) | rule skipped; `false` means "not applicable" and is also skipped, never passed |
| `policy.cost.devMaxCosmosThroughput` | integer (RU/s) | FND-COST-002, Cosmos sub-check | that sub-check is skipped |
| `policy.cost.productionSizedSkuExemptions` | list of resource names or IDs | FND-COST-002 | `[]` |

`policy.network.publicAccess` is the only key that changes a finding without being a pre-condition. `entra-only` and `allowed` lower the
severity of FND-NET-001 for a deliberately public account to `warning` and `info`; they never skip it. FND-NET-002 depends only on the account's own
settings and ignores the key.

There is no name-based guess of which environment is production: the ENV rules and the dev sub-checks are skipped until the organisation lists its
environments. `foundry-doctor scaffold` may propose a first `policy.environments` block from the azd environment names, but the user commits it.

### Related decision: the output flag

The former report-path spelling becomes `--out <path>` in every command that writes a file, in the extension and in the standalone binary, so the two
command surfaces are identical. The output format is selected with `--format` in both. Under azd, `-o/--output` keeps azd's meaning and Foundry Doctor
ignores it (ADR-003).

## Consequences

- Phase 1 implements the schema, the closed-key validation and the "effective policy" header; rules read their inputs from one typed `Policy` struct.
- A fresh project shows many skipped rules (environments, tags, retention, models, residency) until the organisation states its policy. That is intended and visible, and `--strict` turns it into exit 3.
- The nine product-opinion MVP rules keep working under these keys. ENV-002 and ENV-003 are skipped until `policy.environments` is set.
- Adding a key is an ADR-sized change because it widens the closed schema and the catalogue's dependence on configuration.

## Not decided here

- The file layout for per-environment overrides beyond `environments.<name>.policy`; Phase 1 decides it with the schema.
- Whether the FND-IQ-009 key stays under `policy.knowledge` or moves into a Phase 8 knowledge block.
