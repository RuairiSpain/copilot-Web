# Foundry Doctor

Foundry Doctor checks a Microsoft Foundry azd project against a catalogue of rules and reports what it finds, what it
skipped and why. The standalone `foundry-doctor` binary in this directory is the Phase 1 build: it is offline,
read-only and deterministic. It reads `azure.yaml`, Bicep (compiled with the Bicep CLI) and `.azure/<env>/.env`. It
never calls Azure and never changes your project.

Status: Phase 1 (offline rule platform). This is preview software. It is built against preview Foundry extension
schemas (`azure.ai.*`, beta versions), and no workflow in this repository has run on GitHub yet
([docs/development/ci.md](docs/development/ci.md)).

What it does not do:

- It does not tell you that a project is WAF compliant. A run with no findings means the checks that ran found nothing.
  Checks that were skipped say nothing about the project.
- It does not tell you that a deployment will succeed. Phase 1 has no preflight, no what-if and no live Azure access.
- It does not estimate cost or billing.
- It does not use an LLM. Only deterministic checks create findings.

## Install and build

There is no published release yet. Build from source. The module needs Go 1.25.8 or newer (`go.mod`).

```
cd foundry-doctor
go build -o foundry-doctor ./cmd/foundry-doctor
./foundry-doctor version
```

`scripts/ci/build-matrix.sh` builds the six release targets (linux, darwin, windows on amd64 and arm64) with the version
set through `-ldflags`. The supported operating-system list is still marked Proposed in
[ADR-011](docs/decisions/ADR-011-phase-1-engineering-scope.md).

Optional: install the Bicep CLI (0.24.24 or newer) and put `bicep` on `PATH`. It is needed for projects that have a
`.bicep` template (see [Limits](#limits-and-honest-limitations)). `az bicep` is never used.

## Quick start

Run it with the project as the current directory, or any folder below the one that holds `azure.yaml`:

```
foundry-doctor doctor                                   # dev profile, console report
foundry-doctor doctor --profile prod --min-severity warning
foundry-doctor doctor --format sarif --out foundry-doctor.sarif
foundry-doctor explain FND-SEC-001
foundry-doctor compare dev prod                         # key names that differ between two azd environments
```

All commands and flags are listed in [docs/user/cli.md](docs/user/cli.md). Read the console report from the top:
the effective policy, the findings, then the skipped checks and the owner summary. A skipped check was not evaluated
(see [docs/user/rules.md](docs/user/rules.md)).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | No unsuppressed, non-baselined finding at or above `--fail-on` (default `error`). |
| 1 | At least one such finding. |
| 2 | The run could not start or finish: no `azure.yaml`, bad flag or config file, an unusable baseline file, a flag that is not available in this version, or a required tool (Bicep with `validation.bicep: required`) is missing. |
| 3 | A check was skipped and `--strict` was given, and nothing else applies. |
| 4 | Internal error: a rule panicked or returned an error, or the Bicep CLI failed without a usable diagnostic. |

If several apply, the order is 4, 2, 1, 3, 0 ([ADR-010](docs/decisions/ADR-010-command-surface.md)). With `--strict`, expect
exit 3 on almost every real project in this build, because some checks are always skipped (git tracked files, installed
extension versions, live Azure access). See [troubleshooting](docs/user/troubleshooting.md).

## Profiles

`--profile dev|test|prod` (default `dev`; `foundry-dev|foundry-test|foundry-prod` also work). A profile sets the severity
of each rule. It does not hold any organisation-specific value. Rules whose basis is `platform` are errors in every
profile. See [docs/user/profiles.md](docs/user/profiles.md).

## Configuration file

Optional file `.foundry-doctor/config.yaml` in the project root (or `--config <path>`). Unknown or duplicate keys fail
with exit 2. Precedence: command-line flags, then `environments.<name>.policy`, then the repository file, then curated
profile defaults. Every key is described in [docs/user/config-reference.md](docs/user/config-reference.md); the machine-readable
schema is [schemas/config.schema.json](schemas/config.schema.json) and an example is
[schemas/examples/config.example.yaml](schemas/examples/config.example.yaml).

### Policy keys (ADR-007)

Rules that need a fact from your organisation read it from `policy:`. If the key is missing, the rule is skipped with the
reason `profile-key-missing:<key>`. It is never passed and never failed on a guessed value
([ADR-007](docs/decisions/ADR-007-profile-configuration-keys.md)). The table shows what each key does in this build.

| Key | Type | Read by (Phase 1) | When the key is missing |
|---|---|---|---|
| `policy.resourceScope` | `same-resource-group`, `same-subscription`, `any` | No Phase 1 rule yet | Value `same-resource-group` is shown in the effective policy as a baseline. |
| `policy.allowedExternalScopes` | list of resource IDs | No Phase 1 rule yet | `[]`. |
| `policy.environments.production` | list of environment names | FND-ENV-002, FND-ENV-003 | Those rules are skipped. |
| `policy.environments.nonProduction` | list of environment names | FND-ENV-002, FND-ENV-003 | Those rules are skipped. |
| `policy.environments.development` | list of environment names | FND-COST-002 | FND-COST-002 is skipped. |
| `policy.tags.required` | list of `{name, format?}` | FND-OPS-004 | FND-OPS-004 is skipped. |
| `policy.tags.resourceTypes` | list of resource types | FND-OPS-004 | The rule's own list of taggable types. |
| `policy.logRetention.minimumDays` | integer | No Phase 1 rule yet (FND-OPS-006) | Nothing runs on it. |
| `policy.models.allow`, `policy.models.deny` | lists of `format/name[/version]` | FND-OPS-010 | FND-OPS-010 is skipped when both are missing. |
| `policy.dataResidency.scope` | `global`, `datazone-us`, `datazone-eu`, `datazone-apac`, `geography` | FND-OPS-009 | FND-OPS-009 is skipped. |
| `policy.dataResidency.regions` | list of region names | FND-OPS-009 | FND-OPS-009 is skipped. |
| `policy.dataResidency.deploymentSkus` | list of SKU names | FND-OPS-009 | The SKUs whose documented processing scope satisfies the scope. |
| `policy.disasterRecovery.declared` (+ `reference`) | boolean, optional URL or path | No Phase 1 rule yet (FND-REL-007) | Nothing runs on it. |
| `policy.network.publicAccess` | `forbidden`, `entra-only`, `allowed` | FND-NET-001 (changes severity, never skips) | `forbidden`. |
| `policy.monitoring.publicTelemetry` | boolean | No Phase 1 rule yet (FND-NET-010) | `false`. |
| `policy.managedByAzurePolicy` | list of `private-dns-zone-group`, `diagnostic-settings` | FND-NET-003 | `[]`: the template must create them. |
| `policy.knowledge.requireDocumentLevelAccess` | boolean | No Phase 1 rule yet (FND-IQ-009) | Nothing runs on it. |
| `policy.cost.devMaxCosmosThroughput` | integer (RU/s) | FND-COST-002, Cosmos sub-check | The Cosmos sub-check is skipped. |
| `policy.cost.productionSizedSkuExemptions` | list of names or IDs | FND-COST-002 | `[]`. |

"Read by" was taken from the rule code under `internal/rules/` in this build (`grep` for the `Policy` fields). Keys
marked "No Phase 1 rule yet" are accepted, validated and shown in the effective policy, but no rule in the `foundry-core`
pack reads them.

A fresh project therefore shows many skipped rules until the organisation sets its policy. That is intended.

## Baseline and suppressions

For a project with existing findings:

```
foundry-doctor doctor --write-baseline .foundry-doctor/baseline.yaml   # accept today's findings
foundry-doctor doctor                                                  # later runs read that file and fail only on new findings
```

- A baseline hides a finding only when its fingerprint matches exactly. A new or changed finding stays visible. Engine
  diagnostics (`FND-SYS-*`, `bicep/*`) are never baselined.
- A suppression needs a reason, an owner and an expiry date. It is valid through the end of the expiry day (UTC). After
  that the finding is visible again and `FND-SYS-SUPPRESSION-EXPIRED` (error) is reported.
- Both files default to `.foundry-doctor/baseline.yaml` and `.foundry-doctor/suppressions.yaml`. A missing file at the
  default path is not an error. A missing file named with `--baseline` or `--suppressions` is exit 2.

Details and file formats: [docs/user/baseline-and-suppressions.md](docs/user/baseline-and-suppressions.md).

## Output formats

`--format console|json|markdown|sarif` (repeatable). Reports contain no timestamp and no absolute path, so identical
input gives identical output. `--min-severity` hides lower findings from the display only; `--fail-on` decides exit 1.
JSON has a schema ([schemas/report.schema.json](schemas/report.schema.json)). SARIF is 2.1.0 for GitHub code scanning;
skipped checks are notifications, never results. Details are in [docs/user/cli.md](docs/user/cli.md).

## GitHub Actions

- Reusable workflow: [`.github/workflows/foundry-doctor-offline.yml`](../.github/workflows/foundry-doctor-offline.yml)
  (`workflow_call`). It installs the binary (release download with checksum and attestation check, or a source build), runs
  `foundry-doctor doctor --format sarif`, keeps the SARIF as an artifact and uploads it to code scanning.
- Sample caller: [samples/github/foundry-doctor-starter.yml](samples/github/foundry-doctor-starter.yml), with
  [instructions](samples/github/README.md). Copy it by hand; it is not a registered GitHub starter workflow.
- Fork pull requests get a read-only token. The upload job is skipped there, the SARIF stays a workflow artifact, and the
  exit code is still enforced. Do not call the workflow from `pull_request_target`.
- The workflow does not install the Bicep CLI. With the default `validation.bicep: required`, a Bicep-backed project fails
  with exit 2 on a runner that has no `bicep` on `PATH`. Whether GitHub-hosted runners have it was not checked.
- None of the workflows has run on GitHub. See [docs/development/ci.md](docs/development/ci.md).

## Limits and honest limitations

- **Bicep CLI needed for ARM rules.** For a project with `infra/main.bicep`, the rules that read ARM properties need the
  compiled template. The default `validation.bicep: required` exits 2 when `bicep` is missing. With `optional`, those rules
  are skipped with `tool-missing:bicep`. The CLI is run with `--no-restore`, an empty environment and a temporary home,
  so a template that needs downloaded modules can fail to compile. The Bicep CLI was not installed where this
  documentation was written, so the compile path was not run here.
- **Synthetic infrastructure.** A Foundry project with no `infra/` folder (provider `microsoft.foundry`) has no template
  on disk. Rules that need ARM are skipped with `synthetic-infrastructure` ([ADR-004](docs/decisions/ADR-004-synthetic-infrastructure.md),
  [ADR-012](docs/decisions/ADR-012-arm-rules-under-synthetic-infrastructure.md)). In a run on a copy of the fixture
  `test/fixtures/project/synthetic-foundry`, 32 checks were skipped, 21 of them with this reason.
- **Git tracked-file information is unavailable.** The doctor runs no `git` process. FND-CFG-005 therefore skips its check
  of whether `.env` is tracked (`unresolved-value:git-tracked-files-unavailable`). Only the root `.gitignore` is read.
- **Installed extension versions are unavailable.** FND-CFG-011 is skipped (`unresolved-value:installed-extension-versions-unavailable`).
- **No live Azure.** Checks that need role assignments or deployed state are skipped (`control-plane-unavailable`).
  `--preflight`, `--runtime`, `--security`, `--what-if`, `--export-tool-config` and `--llm-explain` exit 2 with
  `not available in this version`.
- **Learn pages were unreachable during research.** `learn.microsoft.com` was blocked in the research environment. Rule
  facts were verified from the source repositories behind the documentation (Azure/azure-dev, Azure REST API specs,
  MicrosoftDocs content, Azure/bicep) at the dates in each rule's `lastVerified` field. The `docsUrl` links point to
  Learn but were not fetched. Unverified items are listed in [docs/development/phase-1-fact-check.md](docs/development/phase-1-fact-check.md)
  and in the Notes of each rule (`foundry-doctor explain <rule-id>`).
- **No Bicep source mapping.** The compiler gives no mapping from ARM resources back to Bicep lines. Findings derived from
  the compiled template carry the entry file and a JSON pointer, with confidence `likely` or `uncertain`, and no line number.
  Compiler diagnostics keep the compiler's own location.
- **Preview platform.** The `azure.ai.*` schemas are beta. Each rule lists the azd and extension versions it was checked
  against (`foundry-doctor explain <rule-id>`). Other versions are not covered.
- **Rule coverage.** The `foundry-core` pack runs the 38 rules allocated to Phase 1 out of 108 catalogued rules
  ([docs/rule-catalog.md](docs/rule-catalog.md), generated). `--rules` can select a subset only.
- **Heuristics.** FND-CFG-005 (raw secrets) and the output redaction are pattern based. A secret in an unusual shape can
  be missed.
- **`--strict`** exits 3 on any skipped check, including "not applicable" ones.
- Some config keys are accepted but not used yet; see [config-reference](docs/user/config-reference.md#keys-accepted-but-not-used-in-this-build).
- **No azd extension yet.** Only the standalone binary exists; the extension module is not in the repository
  ([ADR-005](docs/decisions/ADR-005-azd-extension-module-boundary.md)). The `samples/` folder holds only the GitHub workflow
  sample; test projects are under `test/fixtures/project/`.

## Documentation

| Topic | Page |
|---|---|
| Commands and flags | [docs/user/cli.md](docs/user/cli.md) |
| Reading a finding, skipped checks, confidence | [docs/user/rules.md](docs/user/rules.md) |
| Profiles | [docs/user/profiles.md](docs/user/profiles.md) |
| Config file | [docs/user/config-reference.md](docs/user/config-reference.md) |
| Baseline and suppressions | [docs/user/baseline-and-suppressions.md](docs/user/baseline-and-suppressions.md) |
| Troubleshooting | [docs/user/troubleshooting.md](docs/user/troubleshooting.md) |
| Threat model | [docs/security/threat-model.md](docs/security/threat-model.md) |
| Dependencies and licences | [docs/licence-inventory.md](docs/licence-inventory.md) |
| Rule catalogue (generated) | [docs/rule-catalog.md](docs/rule-catalog.md), [docs/overlap-analysis.md](docs/overlap-analysis.md) |
| Decisions | [docs/decisions/](docs/decisions/) |
| Contributing, security reports | [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) |
