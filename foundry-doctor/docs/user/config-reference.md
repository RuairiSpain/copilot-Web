# Config file reference

The config file is optional. Its default location is `.foundry-doctor/config.yaml` in the project root (the folder that
holds `azure.yaml`). `--config <path>` overrides it; a relative `--config` path is relative to the current directory.
A missing default file is normal. A missing file named with `--config` is exit 2.

The machine-readable schema is [schemas/config.schema.json](../../schemas/config.schema.json) (for editors). The Go loader in
`internal/config` is authoritative, and tests (`TestSchemaKeysMatchGoStructTags`, `TestSchemaAndLoaderAgree`) keep the two equal.
A complete example is [schemas/examples/config.example.yaml](../../schemas/examples/config.example.yaml); the loader accepts it.

## Rules of the file

- `version: 1` is required. It is the only accepted value.
- The schema is closed: an unknown key, a duplicate key, a wrong type or a wrong value is exit 2. The message names the key path
  and line, and lists every problem found in one pass.
- One YAML document only. Anchors, aliases, merge keys and explicit tags are rejected.
- Limits: 256 KiB, 24 levels of nesting, 20,000 nodes, no NUL byte. The file must be a regular file; a symbolic link is refused.
- Path values in the file must be relative and stay inside the project root. (Absolute paths are accepted only for command-line flags.)
  Control characters in a path are rejected.

## Precedence

Highest first: command-line flags, then `environments.<name>.policy` for the selected environment, then the repository file,
then the built-in defaults and the curated profile ([ADR-010](../decisions/ADR-010-command-surface.md), PRD section 6).
Policy keys have no command-line flags. Only these settings have flags: profile, environment, rule selection, baseline,
suppressions and output format.

Selected environment: `--environment`, else `inputs.environment`, else the azd default environment from `.azure/config.json`
(`defaultEnvironment`). `environments.<name>` for a name that is not selected is ignored. If you name an environment with
`--environment` and `.azure/<name>/.env` cannot be read, the run is exit 2. If the environment came from the file or from azd, the
run continues and the rules that need `.env` values are skipped.

## Keys

Defaults are the values used when the key is absent (`defaultFile` in `internal/config/resolve.go`).

### Top level

| Key | Type | Default | Notes |
|---|---|---|---|
| `version` | integer | none | Required. Must be `1`. |
| `profile` | string | `foundry-dev` | `dev`, `test`, `prod`, `foundry-dev`, `foundry-test` or `foundry-prod`. `--profile` wins. See [profiles.md](profiles.md). |

### `inputs`

| Key | Type | Default | Notes |
|---|---|---|---|
| `inputs.azureYaml` | relative path | `./azure.yaml` | Accepted and validated; not used in this build. The project is found by searching for `azure.yaml` or `azure.yml` from the current directory upwards. |
| `inputs.infraPath` | relative path | `./infra` | Accepted and validated; not used in this build. The infra folder comes from `infra.path` in `azure.yaml` (default `infra`). |
| `inputs.environment` | name | none | Selects the azd environment and `environments.<name>`. Must match `[A-Za-z0-9][A-Za-z0-9._-]*`. |

### `rules`

| Key | Type | Default | Notes |
|---|---|---|---|
| `rules.include` | list of selectors | `[must-have, nice-to-have]` | Selectors narrow the packs. |
| `rules.exclude` | list of selectors | `[]` | Removes rules. |
| `rules.packs` | list of pack names | `[foundry-core]` | The only pack in this build is `foundry-core` (the 38 Phase 1 rules). |

A selector is a rule ID (`FND-SEC-001`), a glob (`FND-SEC-*`), a group (`sec`), a category (`must-have`, `nice-to-have`) or
`phase:<n>`, optionally with a leading `!`. `--rules` (repeatable, comma separated) replaces the configured selection; a
selector that matches no catalogued rule is an error. Entries must be unique.

### `validation`

| Key | Type | Default | Notes |
|---|---|---|---|
| `validation.bicep` | `required`, `optional`, `disabled` | `required` | Used. `required`: exit 2 when a Bicep project cannot be compiled because the CLI is missing, too old or broken (the report is still written). `optional`: the dependent rules are skipped with `tool-missing:bicep`. `disabled`: Bicep is not run and no ARM is available. Ignored for ARM JSON and projects without Bicep. |
| `validation.azure` | same | `optional` | Accepted and validated; not used in this build (no Azure access). |
| `validation.runtime` | same | `disabled` | Accepted and validated; not used in this build. |

### `policy` and `environments.<name>.policy`

`environments.<name>` accepts only `policy`, with exactly the keys below. A key set there replaces the repository value for that
environment, key by key. `<name>` must match `[A-Za-z0-9][A-Za-z0-9._-]*`.

Every key is optional. An absent key makes the rules that need it skip with `profile-key-missing:<key>`, unless the table gives a
baseline. The report header ("effective policy") prints every key with its value and its source: `repository`,
`environment:<name>`, `baseline` (the rule's own behaviour, not a configured value) or `unset`. Values are redacted like all
output.

"Read by" lists the Phase 1 rules whose code reads the key, taken from `internal/rules/`. "No Phase 1 rule" means the key is
validated and shown but nothing in the `foundry-core` pack uses it yet.

| Key | Type and validation | Read by | When absent |
|---|---|---|---|
| `policy.resourceScope` | `same-resource-group`, `same-subscription`, `any` | No Phase 1 rule | Baseline `same-resource-group`. |
| `policy.allowedExternalScopes` | list of unique resource IDs; each starts with `/`, no spaces | No Phase 1 rule | Baseline `[]`. |
| `policy.environments.production` | list of unique environment names | FND-ENV-002, FND-ENV-003 | Rules skipped. |
| `policy.environments.nonProduction` | list of unique environment names | FND-ENV-002, FND-ENV-003 | Rules skipped. |
| `policy.environments.development` | list of unique environment names | FND-COST-002 | Rule skipped. |
| `policy.tags.required` | list of `{name, format?}`; names unique ignoring case, no leading or trailing space; `format` is a Go (RE2) regular expression up to 1024 characters | FND-OPS-004 | Rule skipped. |
| `policy.tags.resourceTypes` | list of unique resource types such as `Microsoft.CognitiveServices/accounts` | FND-OPS-004 | The rule's own list. |
| `policy.logRetention.minimumDays` | integer, 0 or more | No Phase 1 rule | Nothing runs on it. |
| `policy.models.allow`, `policy.models.deny` | lists of `format/name` or `format/name/version`; a model may not be in both | FND-OPS-010 | Rule skipped when neither is set. |
| `policy.dataResidency.scope` | `global`, `datazone-us`, `datazone-eu`, `datazone-apac`, `geography` | FND-OPS-009 | Rule skipped. |
| `policy.dataResidency.regions` | list of lower-case region names | FND-OPS-009 | Rule skipped. |
| `policy.dataResidency.deploymentSkus` | list of SKU names | FND-OPS-009 | The SKUs whose documented processing scope satisfies the scope (baseline). |
| `policy.disasterRecovery.declared` | boolean | No Phase 1 rule | Nothing runs on it. |
| `policy.disasterRecovery.reference` | non-empty URL or path; travels with `declared` | No Phase 1 rule | The reference is never echoed into the report header (it prints "reference set"). |
| `policy.network.publicAccess` | `forbidden`, `entra-only`, `allowed` | FND-NET-001 | Baseline `forbidden`. The other values lower the severity of FND-NET-001 to warning and info; they never skip it. |
| `policy.monitoring.publicTelemetry` | boolean | No Phase 1 rule | Baseline `false`. |
| `policy.managedByAzurePolicy` | list of `private-dns-zone-group`, `diagnostic-settings` (unique) | FND-NET-003 | Baseline `[]`: the template must create them. |
| `policy.knowledge.requireDocumentLevelAccess` | boolean | No Phase 1 rule | Nothing runs on it. |
| `policy.cost.devMaxCosmosThroughput` | integer (RU/s), 0 or more | FND-COST-002, Cosmos sub-check | That sub-check is skipped. |
| `policy.cost.productionSizedSkuExemptions` | list of unique, non-blank resource names or IDs | FND-COST-002 | Baseline `[]`. |

Also: an environment name may not be in both `production` and `nonProduction`, or both `production` and `development`.
Model, region and SKU values compare exactly and case-sensitively; tag names compare case-insensitively
([ADR-007](../decisions/ADR-007-profile-configuration-keys.md) decision 6). There is no guess of which environment is
production from its name. An empty list (`[]`) counts as set: it is different from leaving the key out.

### `adoption`

| Key | Type | Default | Notes |
|---|---|---|---|
| `adoption.baseline` | relative path | `.foundry-doctor/baseline.yaml` | `--baseline` wins. See [baseline-and-suppressions.md](baseline-and-suppressions.md). |
| `adoption.suppressions` | relative path | `.foundry-doctor/suppressions.yaml` | `--suppressions` wins. |

### `outputs`

| Key | Type | Default | Notes |
|---|---|---|---|
| `outputs.formats` | list of `console`, `json`, `markdown`, `sarif` (unique) | `[console]` | `--format` wins. With several formats and no `--out`, `console` goes to stdout and the others to the directory below. |
| `outputs.directory` | relative path | `.foundry-doctor/out` | Files are named `foundry-doctor.json`, `.md`, `.sarif`. Created inside the project only. |

### `advanced`

| Key | Type | Default | Notes |
|---|---|---|---|
| `advanced.bicep.executable` | `auto` or a bare command name (no path) | `auto` | Used. Names the command looked up on `PATH` instead of `bicep`. A path is rejected so that a repository file cannot point the doctor at an arbitrary binary. |
| `advanced.bicep.config` | relative path or null | none | Accepted and validated; not used in this build. |
| `advanced.psrule.enabled` | `auto`, `true`, `false` (YAML booleans accepted) | `auto` | Accepted; not used in this build. No PSRule adapter exists yet. |
| `advanced.psrule.config` | relative path or null | none | Accepted; not used. |
| `advanced.checkov.enabled` | `auto`, `true`, `false` | `false` | Accepted; not used. |
| `advanced.checkov.config` | relative path or null | none | Accepted; not used. |
| `advanced.adapters` | list of unique names | `[]` | Accepted; not used. |

## Keys accepted but not used in this build

These keys pass validation, so a config written for a later phase does not fail now, but changing them has no effect on a run:
`inputs.azureYaml`, `inputs.infraPath`, `validation.azure`, `validation.runtime`, `advanced.psrule.*`, `advanced.checkov.*`,
`advanced.bicep.config`, `advanced.adapters`. They were checked by searching the application code for any read of them
(`internal/app` only reads `inputs.environment`, `validation.bicep`, `advanced.bicep.executable`, `outputs.*`, `adoption.*`,
`rules.*`, `policy` and `environments`).

The policy keys marked "No Phase 1 rule" above are in the same state.

## Minimal example

```yaml
version: 1
profile: prod
policy:
  environments:
    production: [prod]
    nonProduction: [dev, test]
    development: [dev]
  tags:
    required:
      - name: owner
validation:
  bicep: optional
```

With this file the four rules stop skipping for a missing key. Run on a copy of `test/fixtures/project/arm-json` (one
environment, no `.azure` folder), FND-ENV-002, FND-ENV-003 and FND-COST-002 were then skipped as `not-applicable` and FND-OPS-004
ran. A skip for a missing key means the key is missing; a skip for `not-applicable` means the key is set but the project does not
have the situation the rule checks.
