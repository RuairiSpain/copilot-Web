# x-foundry

A declarative `x-foundry` extension for `azure.yaml`. It is the landing zone around an azd
Foundry project: shared network, Storage, Cosmos DB, AI Search, Key Vault and monitoring, plus
Foundry IQ, a gateway, security and governance, declared as intent. The Foundry project, model
deployments, agents, toolboxes and connections stay in azd's own services; x-foundry does not
repeat them (see [docs/azd-boundary.md](docs/azd-boundary.md)).

`x-foundry` is **not** a native azd capability. It is implemented by this custom extension,
and the schema contains no session-pool settings (session and agent pooling are separate
Foundry capabilities).

All extension code is Go, so it can use the azd extension SDK and ship as one static binary
per platform.

## Status

| Phase | Scope | State |
| --- | --- | --- |
| 1 | Schema engine and validation | **Implemented here** |
| 2 | Bicep infrastructure generator | **Implemented here** (see [docs/phase-2.md](docs/phase-2.md)) |
| 3 | Foundry provisioning engine | **Removed**: azd owns agents, toolboxes and connections ([docs/azd-boundary.md](docs/azd-boundary.md)) |
| 4 | Foundry IQ and Search engine | Not started |
| 5 | Gateway and governance | Not started |
| 6 | azd integration and developer experience | Not started |
| 7 | Drift detection (optional, after first release) | Not started |

Phase 1 turns an `azure.yaml` into a validated `DeploymentPlan`. Phase 2 turns the plan into
Bicep for the shared infrastructure (`xfoundry generate`).
Decisions and ideas for later phases are recorded in `docs/roadmap.md`.

## Quick start

```bash
cd x-foundry
go build -o bin/xfoundry ./cmd/xfoundry

bin/xfoundry validate examples/hub-spoke.yaml     # exit 0 when valid, 1 otherwise
bin/xfoundry plan examples/hub-spoke.yaml         # deployment steps, dependencies first
bin/xfoundry plan examples/hub-spoke.yaml --json  # full normalised plan
bin/xfoundry generate examples/hub-spoke.yaml --out infra   # write the Bicep project
bin/xfoundry schema                               # print the JSON Schema

go test -race ./...                               # unit and end-to-end tests
golangci-lint run ./...
go run ./tools/schemagen -check                   # published schemas are up to date
```

From Go (inside this module):

```go
analysis, err := plan.AnalyseFile("azure.yaml") // err is an internal failure only
if !analysis.OK() {
    fmt.Println(analysis.Err())                  // every diagnostic, one per line
}
p, err := plan.BuildFile("azure.yaml")          // returns a *diag.Failure when invalid
p.Order()                                        // node ids, dependencies first
p.Layers                                         // groups that can deploy in parallel
p.Config.Project("finance").Models               // effective models after inheritance
```

## Pipeline

```text
azure.yaml
  -> parser           YAML (duplicate keys rejected), version check, session-pool check
  -> JSON Schema      structure of the document as authored            (XF102)
  -> config.Decode    typed model; defaults from `default` tags; explicit keys tracked
  -> validate.Declared  uniqueness, references, secrets, naming, networking, tiers
  -> normalise        defaults, implicit resources, inheritance
  -> validate.Effective cross-references on the resolved configuration
  -> graph            dependency graph and deployment order
  -> plan.Plan
```

A phase that reports errors stops the pipeline so later phases can rely on earlier
guarantees. Warnings never stop it and are carried on `Plan.Warnings`.

## Layout

```text
cmd/xfoundry/        command line
schemas/             x-foundry.schema.json (source of truth, embedded in the binary),
                     generated azure-yaml wrapper, versions/ and examples/
examples/            standalone-minimal, standalone-private, hub-spoke, foundry-iq,
                     apim-ai-gateway, enterprise
internal/
  config/            typed model, defaults and explicit-key tracking, deep clone
  parser/            YAML loading, JSON Schema validation, version and session-pool checks
  validate/          semantic rules (declared, effective, knowledge, gateway, secrets, regions)
  normalise/         normaliser and inheritance engine
  graph/             dependency graph and builder
  plan/              Plan and entry points (also holds the end-to-end tests)
  azurenames/        Azure naming constraints (reused by the Phase 2 name generator)
  diag/ ids/         diagnostics; scope and node identifiers
  schemapub/ testutil/
tools/schemagen/     regenerates the published schema files
docs/
```

### Published schemas

`schemas/x-foundry.schema.json` is the source of truth and is embedded in the binary. After
editing it, run `go run ./tools/schemagen` to regenerate the rest (the tests fail while
they are stale):

* `schemas/x-foundry.schema.json` validates the `x-foundry` subtree.
* `schemas/azure-yaml-x-foundry.schema.json` validates a whole `azure.yaml`: it requires
  `x-foundry` and allows every other key. In an editor, combine it with the native
  azure.yaml schema using `allOf`.
* `schemas/versions/1.0/` pins the version. `schemaVersion` defaults to `1.0`; a different
  major version is rejected (`XF103`).

## Environment profiles

`defaults.environment` is `dev` (default), `test` or `prod`. `dev` has no recommendations;
`test` and `prod` report Azure Well-Architected recommendations (reliability, security,
operations, cost) as warnings that never block. Preview a stricter profile with
`xfoundry validate azure.yaml --environment prod`. See
[docs/waf-profiles.md](docs/waf-profiles.md) for each recommendation and its source.

## Network: public unless a VNet is specified

* **No network settings means no VNet and public endpoints** (`mode: public`). Access is
  still Entra ID only: key-based authentication is off by default, and a component can only
  expose a public endpoint when the global intent allows it (`XF106`).
* **Specifying a VNet makes the deployment private.** When `security.network.mode` is
  omitted, any of `addressSpace`, `agentSubnetPrefixLength`, `existingVnetResourceId` or an
  existing subnet ID selects `private`. You can also write `mode: private` explicitly.
  `Plan.Config.Network.ModeSource` records which rule applied (`explicit`, `vnet-settings`
  or `default`).
* **Private mode** creates a VNet (`addressSpace`, default `10.20.0.0/16`), private
  endpoints and private DNS for every data-plane component, and uses the Foundry **standard
  agent setup**. VNet settings combined with `mode: public` are rejected (`XF127`).
* **`restricted`** keeps public endpoints but limits them to `allowedIps` (public ranges
  only; Azure service firewalls reject private ranges).
* **`agentService.setup: auto`** picks `standard` in private mode or when `cosmos` is
  configured, otherwise `basic`. The standard setup needs your own Storage, AI Search and
  Cosmos DB (created implicitly when you do not declare them) and an agent subnet delegated to
  `Microsoft.App/environments` (`agentSubnetPrefixLength`, default `/24`). x-foundry creates
  those; the Foundry project and its capability host are azd's.
* To use an existing VNet, give `existingVnetResourceId` plus
  `existingPrivateEndpointSubnetResourceId` and, for the standard setup,
  `existingAgentSubnetResourceId`.
* **Identity.** `managedIdentity.type` (`userAssigned`, `systemAssigned`,
  `systemAssignedAndUserAssigned`) controls the identity attached to the gateway and Search
  resources you create.

## Normalisation

* **Implicit resources.** Anything the normaliser derives is listed in
  `Plan.Config.Implicit` with the reason: a Search service for Foundry IQ or the standard
  setup, storage for blob/ADLS sources and evaluation datasets, Cosmos DB for the standard
  setup, a managed identity, a Key Vault when secret references are used, observability for the
  gateway, and the VNet, private DNS and private endpoints for private mode.
* **Absent means not created** unless something requires it. A missing `keyVault`, `cosmos` or
  `governance` section creates nothing unless a setting requires it.
* **Inheritance** (`root < hub < project`, hub only when `inheritHub` is true and the
  matching `hub.inheritance` flag is on). A project item replaces an inherited one with the
  same name; additions merge. `denied` models accumulate. A project `allowed` set replaces
  the inherited one but may only narrow it (`XF115`).
* Items declared at root with a `project` field move into that project. Root items without
  one are shared by every project.
* The index is materialised: default `id`, `content`, `title` and `contentVector` fields
  are added when you do not declare them, so every field reference can be checked.

## Decisions to review

* **The JSON Schema is authoritative where the PRD's YAML examples differ.** The PRD shows
  `hub.mcps: [graph]`, `gateway.endpoints` as a map, `quotas.users`, and project-level
  `developers:`; the schema uses object lists, `quotas.profiles`, and `roles`.
* **Schema changes from the supplied draft:** truncated name patterns fixed;
  `optionalResourceName` merged into `resourceName`; `projects[].roles` uses a
  `projectRoles` shape (the draft required `admins` on every project); Search SKUs use the
  ARM spelling `storage_optimized_*`; `PremiumV2` added to the gateway SKUs.
* **Deployment order** follows the PRD, with one deliberate deviation (see
  `internal/graph/builder.go`): the observability workspace is created early because the
  gateway logs to it. Alerts are the late "Monitoring" step.
* **Bare Key Vault secret names** in `secretRef` are accepted as names
  in the extension's Key Vault; a literal value that happens to look like a name cannot be
  told apart. Values that look like keys, tokens or connection strings are rejected anywhere
  in the document.

## Not in Phase 1

* Rule 24 checks names you write explicitly; the generator checks names it creates.
* Rule 23 checks region names and data residency. Per-region availability of models and SKUs
  needs a provider lookup.

See [docs/validation-rules.md](docs/validation-rules.md) for every diagnostic code and
[docs/production-readiness.md](docs/production-readiness.md) for the review backlog.
