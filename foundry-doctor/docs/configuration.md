# Repository configuration

Foundry Doctor reads repository settings from
`.foundry-doctor/config.yaml`. The file is versioned, validated strictly, and
merged with command-line flags, environment overrides, and curated profile
defaults.

- Schema file: [`../schemas/config.schema.json`](../schemas/config.schema.json)
- Current version: `1`
- Unknown keys: **validation error**
- Multiple YAML documents: **validation error**

## Precedence

Foundry Doctor resolves settings in this order:

1. command-line flags
2. environment-specific policy under `environments.<name>.policy`
3. repository config
4. curated profile defaults
5. built-in product defaults for a small set of policy keys

Profiles set severities and adapter defaults only. They do not supply
organisation-specific policy values.

## Minimal example

```yaml
version: 1
profile: prod
inputs:
  azureYaml: ./azure.yaml
  infraPath: ./infra
policy:
  environments:
    production: [prod]
    nonProduction: [test]
    development: [dev]
    tiers:
      dev: dev
      test: test
      prod: prod
  network:
    publicAccess: forbidden
    dnsManagedByPolicy: false
    centralDns: false
    cloud: public
  preflight:
    deploymentHistoryMargin: 80
    regionMatrixStalenessDays: 365
environments:
  prod:
    policy:
      network:
        publicAccess: entra-only
```

## Top-level schema

| Key | Purpose |
| --- | --- |
| `version` | Required schema version. Must be `1`. |
| `profile` | Curated default profile: `dev`, `test`, or `prod`. |
| `inputs` | Local file locations and default environment name. |
| `rules` | Rule pack and category selection. |
| `validation` | Whether Bicep, Azure, and runtime validations are required, optional, or disabled. |
| `policy` | Organisation policy inputs used by rules. |
| `adoption` | Baseline and suppression files. |
| `outputs` | Output formats and default output directory. |
| `advanced` | Optional adapter controls and Bicep executable override. |
| `environments` | Per-environment policy overrides only. |

## Accepted policy keys

All organisation-supplied requirements live under `policy:`. The schema is
closed: keys outside this list fail validation.

### Core keys

| Key | Type | Default / behaviour |
| --- | --- | --- |
| `policy.resourceScope` | `same-resource-group` \| `same-subscription` \| `any` | Default `same-resource-group`. |
| `policy.allowedExternalScopes` | list of Azure resource IDs | Default `[]`. |
| `policy.identity.deploymentPrincipalIds` | list of GUIDs | Optional. |
| `policy.managedByAzurePolicy` | list of `private-dns-zone-group` or `diagnostic-settings` | Default `[]`. |

### Environment keys

| Key | Type | Notes |
| --- | --- | --- |
| `policy.environments.production` | list of environment names | Required by rules that distinguish production. |
| `policy.environments.nonProduction` | list of environment names | Required by environment classification rules. |
| `policy.environments.development` | list of environment names | Used by cost/development checks. |
| `policy.environments.tiers` | map of environment name → `dev`\|`test`\|`prod` | Accepted V1 key for explicit tier mapping. |

### Network keys

| Key | Type | Default / behaviour |
| --- | --- | --- |
| `policy.network.publicAccess` | `forbidden` \| `entra-only` \| `allowed` | Default `forbidden`. |
| `policy.network.dnsManagedByPolicy` | boolean | Default `false`. Accepted V1 key. |
| `policy.network.centralDns` | boolean | Default `false`. Accepted V1 key. |
| `policy.network.cloud` | `public` \| `usgov` \| `china` | Optional. Accepted V1 key. |

### Monitoring, cost, tags, models, knowledge, residency

These keys remain part of the strict schema and follow ADR-007:

- `policy.monitoring.publicTelemetry`
- `policy.tags.required`
- `policy.tags.resourceTypes`
- `policy.logRetention.minimumDays`
- `policy.models.allow`
- `policy.models.deny`
- `policy.dataResidency.scope`
- `policy.dataResidency.regions`
- `policy.dataResidency.deploymentSkus`
- `policy.disasterRecovery.declared`
- `policy.disasterRecovery.reference`
- `policy.knowledge.requireDocumentLevelAccess`
- `policy.cost.devMaxCosmosThroughput`
- `policy.cost.productionSizedSkuExemptions`

### Preflight product-decision keys

| Key | Type | Default | Used by |
| --- | --- | --- | --- |
| `policy.preflight.deploymentHistoryMargin` | integer >= 1 | `80` | DEP-010 |
| `policy.preflight.regionMatrixStalenessDays` | integer >= 1 | `365` | DEP-012 |

These keys are accepted in V1 so operators can tune the documented baseline
without patching code.

## Environment overrides

Only policy may be overridden per environment:

```yaml
environments:
  prod:
    policy:
      network:
        publicAccess: entra-only
      preflight:
        deploymentHistoryMargin: 40
```

## Authentication and Azure access

Foundry Doctor deliberately uses an existing Azure login or an injected token.
The credential chain is:

1. `AZURE_ACCESS_TOKEN`
2. `azd auth token`
3. `az account get-access-token`

### Managed identity

Foundry Doctor does not acquire managed-identity tokens itself. In managed
identity environments, obtain a token outside the tool and pass it as
`AZURE_ACCESS_TOKEN` before invoking `preflight`, `runtime`, `graph`, or other
Azure-reading commands.

### GitHub Actions OIDC

For GitHub Actions:

1. log in with `azure/login` using OIDC
2. run Foundry Doctor in the same job
3. rely on the Azure CLI or azd token cache populated by the login step

Foundry Doctor never needs a client secret of its own.

## Version and compatibility reporting

`foundry-doctor version` prints:

- the stamped build version
- the repository config path
- minimum verified azd version
- minimum verified Bicep version

Current verified floors:

- azd: `1.34.2`
- Bicep: `0.48.1`
