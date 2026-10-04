# Phase 2: Bicep generator

`xfoundry generate azure.yaml --out infra` turns the plan into a Bicep project. This page records
what it generates, the decisions behind it, and what it does not do.

## What is generated

```
infra/
  main.bicep             subscription scope: creates the resource group, calls resources.bicep
  resources.bicep        resource-group scope: one module call per plan node, in plan order
  main.parameters.json   Azure Developer CLI parameter mapping
  modules/*.bicep        static, hand-written modules (only the ones the plan needs)
  README.md              what was generated, warnings, and what is not generated yet
```

The modules are ordinary Bicep files in `internal/bicep/modules`. They are compiled and linted
in CI with the Bicep CLI (`XFOUNDRY_REQUIRE_BICEP`), and so is the generated output for every
example, so a type error, an unknown property or a linter warning fails the build. Only
`resources.bicep` and `main.bicep` are produced by Go code, and they contain no logic beyond
parameter values and `dependsOn`.

| Plan node | Module |
| --- | --- |
| identity | `identity` (user-assigned identity) |
| observability | `observability` (Log Analytics, Application Insights) |
| network | `network` (VNet, delegated agent subnet, private endpoint subnet) |
| private-dns | `private-dns` (zones and VNet links) |
| storage, key-vault, cosmos, search | `storage`, `keyvault`, `cosmos`, `search` |
| private-endpoint | `private-endpoint` (one per sub-resource, with a DNS zone group) |
| foundry-account | `foundry-account`, `foundry-appinsights-connection` |
| model-deployment | `foundry-deployments` (one module, deployments created one at a time) |
| foundry-project | `foundry-project` (with connections to Cosmos DB, Storage and Search in the standard setup) |
| capability-host | `foundry-capability-host` |
| (role assignments) | `ra-*` |

Existing resources (`existingResourceId`) are referenced with `existing`, never created. Private
endpoints, connections and role assignments still target them, in their own subscription and
resource group.

## Decisions

* **Network access.** `private` disables public access on every service and uses private
  endpoints. `restricted` keeps public access with a default-deny firewall and the `allowedIps`
  rules. `public` leaves endpoints open to the network. In every mode access is Microsoft Entra
  ID only unless a local-authentication setting is on. The per-component `publicNetworkAccess`
  settings are validated in Phase 1 but do not change the result: the mode decides.
* **Names.** Generated names are `<abbreviation>-<prefix>-<unique token>` (storage and Key Vault
  are shortened to their limits), where the token comes from the subscription, environment name
  and location, so names are stable across redeployments and unique across subscriptions. An
  explicit `name` is used as written. The prefix is `defaults.namingPrefix`, or `xf`.
* **Tags.** Every resource carries the shared tags plus `x-foundry-env` and `x-foundry-id` (the
  plan node id), so Azure resources can be matched to the configuration later (drift detection,
  Phase 7).
* **Roles.** Principals need object IDs because Bicep cannot look up a display name. A name
  without an ID produces `XF201` and no assignment.

  | Configuration | Role | Scope |
  | --- | --- | --- |
  | `security.roles.admins` | Foundry Account Owner | Foundry resource |
  | project `roles.admins` (not already a root admin) | Foundry Project Manager | project |
  | `developers` (root and project) | Foundry User | project |
  | `operators` | Reader and Monitoring Reader | resource group |
  | `consumers` | none (they use the gateway, Phase 5) | |
  | the deploying identity (`principalId` parameter) | Foundry User | Foundry resource |
  | each project's identity (standard setup) | Storage Blob Data Contributor and Owner (container-limited), Cosmos DB Operator and Data Contributor, Search Index Data Contributor and Search Service Contributor | its Storage, Cosmos DB and Search |

  The Foundry role GUIDs come from the Azure built-in roles reference; the others from the
  Foundry sample templates. The ordering of the project identity's assignments around the
  capability host follows those samples.
* **Serial changes to the Foundry resource.** Projects, capability hosts, connections and model
  deployments are chained one after the other because the resource provider rejects concurrent
  changes to an account's children.
* **Locks.** `governance.resourceLocks` adds `CanNotDelete` locks to Storage, Search and Cosmos DB
  (only the ones the generator creates).
* **Diagnostics.** When observability is on, the Foundry resource, Search, Cosmos DB, Key Vault
  and Storage send logs and metrics to the Log Analytics workspace.
* **Agent state.** `cosmos.throughput` is validated but not applied: Foundry creates the database
  and containers itself when the capability host is set up.

## Diagnostics

| Code | Meaning |
| --- | --- |
| XF201 | A principal has no object ID, so no role assignment is generated (w) |
| XF202 | A deployment `location` is ignored: deployments run in the Foundry resource's region (w) |
| XF203 | The same deployment name is declared in several scopes with different settings; the first is used (w) |
| XF204 | `resourceGroup` on the hub or a project is ignored: everything is deployed to one resource group (w) |
| XF205 | `restricted` mode with an existing VNet: virtual network rules are not generated (w) |
| XF206 | The address space cannot be split into the agent and private endpoint subnets (error) |
| XF207 | A container asks for anonymous access, which is disabled; it is created private (w) |

## Not generated

* The data plane: agents, toolboxes, MCPs, connectors, knowledge bases and evaluations
  (Phases 3 and 4).
* API Management and everything in the gateway section (Phase 5). The gateway needs outbound VNet
  integration and policies that are designed together, so the instance is not created early.
* Azure Policy assignments, Defender plans, budgets and alerts (Phase 5).
* The Well-Architected items in the medium and high groups in `roadmap.md` (customer-managed
  keys, firewalls, an Azure Monitor Private Link Scope and so on). `doctor` (Phase 6) comments on
  them.
* Sovereign clouds: private DNS zone names are the public cloud's, except for the storage suffix.

## Validation

`bicep build` checks every generated file against the Bicep type definitions and linter. The
repository has no Azure subscription, so nothing has been deployed. Two kinds of problem can
therefore remain: properties the type definitions accept but the service rejects at runtime, and
ordering or quota problems that only a real deployment shows. Run `az deployment sub what-if`
(and then a test deployment) before relying on a template.
