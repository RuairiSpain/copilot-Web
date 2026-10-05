# ADR-007: Profile configuration keys

Status: Proposed (the project owner delegated the key choices to the implementer on 2026-10-05; independent review pending)
Date: 2026-10-05

## Context

PRD section 6 defines the unified configuration (`version`, `profile`, `inputs`, `rules`, `validation`, `policy`, `adoption`, `outputs`,
`advanced`) and says unknown settings fail validation. It defines only two `policy` keys (`resourceScope`, `allowedExternalScopes`).

Phase 0 research found rules that cannot be evaluated from the template or from Azure alone and need the organisation to state a
requirement or a fact: required tags, the model allow and deny lists, the data-residency region list, the minimum log retention,
whether a disaster-recovery plan exists, whether public access is intended, which settings an Azure Policy assignment creates after
deployment, which environments are non-production, whether document-level access control is required, which fixed-capacity SKUs are
acceptable in dev, and whether telemetry may leave the private network (the `notes` of the rules named below). The PRD does not name these
keys, so each rule left them open.

## Decision

1. **All organisation-supplied requirements live under `policy:`** in the repository or environment configuration, next to the two
   keys the PRD already defines. They are not CLI flags and not rule-pack data, because they differ per organisation and per environment.
2. **A rule that needs a key and does not find one is `skipped`** with the reason `profile-key-missing:<key>` and is listed in the report
   summary. It is never passed and never failed on the strength of a guessed default (PRD section 2, "Evidence over scores", and `CLAUDE.md`).
3. **Keys may be set per environment** under `environments.<name>.policy`, with the PRD precedence: flags, then environment, then repository, then
   curated profile defaults. The curated `foundry-dev|test|prod` profiles set severities only and supply no organisation-specific values.
4. **The schema is closed.** Phase 1 ships `schemas/config.schema.json`; any key not listed here fails validation (exit 2).

### Keys

| Key | Type | Used by | When absent |
|---|---|---|---|
| `policy.resourceScope` | `same-resource-group` \| `same-subscription` \| `any` | scope rules (PRD) | `same-resource-group` |
| `policy.allowedExternalScopes` | list of resource IDs | scope rules (PRD) | `[]` |
| `policy.requiredTags` | list of tag names (matched case-insensitively) | FND-OPS-004 | rule skipped |
| `policy.logRetention.minimumDays` | integer | FND-OPS-006 | rule skipped |
| `policy.models.allow` / `policy.models.deny` | lists of `format/name[/version]` | FND-OPS-010 | rule skipped |
| `policy.dataResidency.regions` | list of Azure region names | FND-OPS-009 | rule skipped |
| `policy.dataResidency.deploymentSkus` | list of allowed deployment SKU names | FND-OPS-009 | rule skipped |
| `policy.disasterRecovery.declared` | boolean, with optional `reference` (URL or path) | FND-REL-007 | informational question only, never a finding |
| `policy.network.publicAccess` | `forbidden` \| `entra-only` \| `allowed` | FND-NET-001, FND-NET-002 | `forbidden` in test and prod, `allowed` in dev |
| `policy.monitoring.publicTelemetry` | boolean | FND-NET-010 | `false` |
| `policy.managedByAzurePolicy` | list of `private-dns-zone-group` \| `diagnostic-settings` | FND-NET-003, FND-OPS-001 | `[]` (the template must create them) |
| `policy.environments.nonProduction` | list of environment names | FND-ENV-002, FND-ENV-003 | `[dev, test]` by name only; any other name makes the rule `skipped` |
| `policy.knowledge.requireDocumentLevelAccess` | boolean | FND-IQ-009 (Phase 8) | rule skipped |
| `policy.cost.productionSizedSkuExemptions` | list of resource names or IDs | FND-COST-002 | `[]` |

Defaults that change a result (`policy.network.publicAccess`, `policy.environments.nonProduction`) are deliberate: they are the conservative reading
of the rule, they are printed in the report header as "effective policy", and an explicit value always wins.

### Related decision: the output flag

The PRD's `--output <path>` becomes `--out <path>` in every command that writes a file, in the extension and in the standalone binary, so the
two command surfaces are identical. `-o/--output` keeps its azd meaning (output format) when running under azd, as ADR-003 records.

## Consequences

- Phase 1 implements the schema, the closed-key validation and the "effective policy" header; the rules above read their inputs from one typed `Policy` struct.
- Rules that depend on an absent key are skipped, so a fresh project shows many skips until the organisation states its policy. That is intended and visible.
- Adding a key is an ADR-sized change because it widens the closed schema and the catalogue's dependence on configuration.

## Not decided here

- The file format and location of per-environment overrides beyond the PRD's `environments.<name>` precedence; Phase 1 decides it with the schema.
- Whether the FND-IQ-009 key stays under `policy.knowledge` or moves into a Phase 8 knowledge block.
