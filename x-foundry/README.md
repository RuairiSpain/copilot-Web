# x-foundry

A declarative `x-foundry` extension for `azure.yaml`. Teams describe Microsoft Foundry
projects, Foundry IQ, search, a gateway, security and governance as intent; the
extension turns that into infrastructure and Foundry resources.

`x-foundry` is **not** a native azd capability. It is implemented by this custom
extension, and the schema contains no session-pool settings (session and agent pooling
are separate Foundry capabilities).

## Status

| Phase | Scope | State |
| --- | --- | --- |
| 1 | Schema engine and validation | **Implemented here** |
| 2 | Bicep infrastructure generator | Not started |
| 3 | Foundry provisioning engine | Not started |
| 4 | Foundry IQ and Search engine | Not started |
| 5 | Gateway and governance | Not started |
| 6 | azd integration and developer experience | Not started |

Phase 1 turns an `azure.yaml` into a validated `DeploymentPlan`. Nothing is deployed.

## Quick start

```bash
cd x-foundry
python -m pip install -e ".[dev]"

xfoundry validate examples/hub-spoke.yaml     # exit 0 when valid, 1 otherwise
xfoundry plan examples/hub-spoke.yaml         # deployment steps, dependencies first
xfoundry plan examples/hub-spoke.yaml --json  # full normalised plan
xfoundry schema                               # print the JSON Schema

python -m pytest --cov                        # fails below 90% coverage
```

From Python:

```python
from xfoundry.plan import analyse_file, build_plan

plan = build_plan("azure.yaml")        # raises ValidationFailed listing every error
analysis = analyse_file("azure.yaml")  # never raises; analysis.plan is None on errors
plan.order                             # node ids, dependencies first
plan.layers                            # groups that can deploy in parallel
plan.config.projects[0].models         # effective models after inheritance
```

## Pipeline

```text
azure.yaml
  -> parser          YAML (duplicate keys rejected), version check, session-pool check
  -> JSON Schema     structure of the document as authored            (XF102)
  -> Pydantic        typed model with defaults                        (XF110)
  -> declared rules  uniqueness, references, secrets, naming, ...     (validate_declared)
  -> normaliser      defaults, implicit resources, inheritance
  -> effective rules cross-references on the resolved configuration   (validate_effective)
  -> graph           dependency graph and deployment order
  -> DeploymentPlan
```

A phase that reports errors stops the pipeline, so later phases can rely on earlier
guarantees. Warnings never stop it and are carried on `plan.warnings`.

## Layout

```text
src/xfoundry/
  schema/       x-foundry.schema.json (source of truth) and Pydantic models
  parser/       YAML loading, schema validation, version and session-pool checks
  validators/   semantic rules (declared, effective, knowledge, gateway, secrets, locations)
  normalise/    normaliser and inheritance engine
  graph/        dependency graph and builder
  azure_names.py  Azure naming constraints (reused by Phase 2)
  plan.py       DeploymentPlan and entry points
  cli/          xfoundry command
schemas/        published schemas and examples (generated; see below)
examples/       standalone-minimal, standalone-private, hub-spoke, foundry-iq,
                hosted-agent-runtime, apim-ai-gateway, enterprise
tests/
```

### Published schemas

`schemas/` is generated from the packaged schema. After editing
`src/xfoundry/schema/x-foundry.schema.json` run:

```bash
python scripts/build_schemas.py          # regenerate
python scripts/build_schemas.py --check  # fail if stale (also run by the tests)
```

* `schemas/x-foundry.schema.json` validates the `x-foundry` subtree.
* `schemas/azure-yaml-x-foundry.schema.json` validates a whole `azure.yaml`: it
  requires `x-foundry` and allows every other key. In an editor, combine it with the
  native azure.yaml schema using `allOf`.
* `schemas/versions/1.0/` pins the version. `schemaVersion` defaults to `1.0`; a
  different major version is rejected (`XF103`).

## Normalisation

* **Implicit resources.** Anything the normaliser derives is listed in
  `plan.config.implicit` with the reason: a Search service when Foundry IQ is used and
  none is declared, storage for blob/ADLS sources and evaluation datasets, deployments
  for models named in `models.allowed` and for embedding models, a managed identity, a
  Key Vault when secret references are used, observability for the gateway and runtimes,
  and the VNet, private DNS and private endpoints for private network mode.
* **Absent means not created** unless something requires it. A missing `storage`,
  `redis`, `events` or `governance` section creates nothing.
* **Inheritance** (`root < hub < project`, hub only when `inheritHub` is true and the
  matching `hub.inheritance` flag is on). A project item replaces an inherited one with
  the same name; additions merge. `denied` models accumulate. A project `allowed` set
  replaces the inherited one but may only narrow it (`XF115`).
* Items declared at root with a `project` field move into that project. Root items
  without one are shared by every project.
* The index is materialised: default `id`, `content`, `title` and `contentVector`
  fields are added when you do not declare them, so every field reference can be checked.

## Decisions to review

* **The JSON Schema is authoritative where the PRD's YAML examples differ.** For example
  the PRD shows `hub.mcps: [graph]`, `gateway.endpoints` as a map, `quotas.users`, and
  project-level `developers:`; the schema uses object lists, `quotas.profiles`, and
  `roles`. The examples in this repository follow the schema.
* **Schema changes from the supplied draft:** the resource-name and storage-container
  patterns were truncated in the draft and are fixed; `optionalResourceName` is merged
  into `resourceName` (identical); `projects[].roles` uses a new `projectRoles` shape
  because the draft required `admins` on every project.
* **Deployment order** follows the PRD, with two deliberate deviations (see
  `graph/builder.py`): MCPs, connectors and knowledge bases come before toolboxes and
  agents because those reference them, and the observability workspace is created early
  because runtimes and the gateway log to it. Alerts are the late "Monitoring" step.
* **One Foundry resource per configuration.** A project `location` that differs from the
  account location is reported as a warning (`XF120`), not honoured.
* **Bare Key Vault secret names** in `secretRef` and `runtime.secrets` are accepted as
  names in the extension's Key Vault; a literal value that happens to look like a name
  cannot be told apart. Values that look like keys, tokens or connection strings are
  rejected anywhere in the document.

## Not in Phase 1

* Rule 25 (destructive changes need approval) needs deployed state to diff against; it
  belongs with state tracking in Phase 3.
* Rule 24 checks names you write explicitly. Names the extension generates are checked
  when the naming engine exists (Phase 2); `azure_names.NAME_RULES` is ready for it.
* Rule 23 checks region names, data residency and hub/spoke distance. Per-region
  availability of models and SKUs needs a provider lookup (Phase 2/3).

See [docs/validation-rules.md](docs/validation-rules.md) for every diagnostic code.
