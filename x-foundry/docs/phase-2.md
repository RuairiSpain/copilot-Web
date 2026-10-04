# Phase 2: Bicep generator

`xfoundry generate azure.yaml --out infra` turns the plan into a Bicep project for the shared
infrastructure around an azd Foundry project. This page records what it generates, the decisions
behind it, and what it does not do. What azd owns instead is in [azd-boundary.md](azd-boundary.md):
the generator creates no Foundry resource, project, model deployment, capability host, connection
or agent.

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
| (role assignments) | `ra-resource-group` (operators: Reader and Monitoring Reader) |

The generated `resources.bicep` ends with outputs that the azd project consumes
(`VNET_RESOURCE_ID`, `PE_SUBNET_NAME`, `AGENT_SUBNET_NAME`, `STORAGE_ACCOUNT_RESOURCE_ID`,
`COSMOS_DB_RESOURCE_ID`, `AI_SEARCH_RESOURCE_ID`, `KEY_VAULT_RESOURCE_ID`, `LOG_ANALYTICS_WORKSPACE_ID`,
`APPLICATIONINSIGHTS_RESOURCE_ID`, `PRIVATE_DNS_*`).

Existing resources (`existingResourceId`) are never created. Private endpoints and the outputs
still use their resource IDs, in their own subscription and resource group.

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
  without an ID produces `XF201` and no assignment. Only `operators` get roles (Reader and
  Monitoring Reader on the resource group). Roles for `admins` and `developers` on the Foundry
  resource and projects need resources that azd creates, so they are listed as "not generated"
  (see [azd-boundary.md](azd-boundary.md)). `consumers` get no role; they use the gateway (Phase 5).
  Each role assignment in `resources.bicep` has a comment naming the role and who gets it, and
  the same text is the description of the role assignment in Azure.
* **Locks.** `governance.resourceLocks` adds `CanNotDelete` locks to Storage, Search and Cosmos DB
  (only the ones the generator creates).
* **Diagnostics.** When observability is on, Search, Cosmos DB, Key Vault
  and Storage send logs and metrics to the Log Analytics workspace.
* **Agent state.** `cosmos.throughput` is validated but not applied: Foundry creates the database
  and containers itself when the capability host is set up.

## Diagnostics

| Code | Meaning |
| --- | --- |
| XF201 | A principal has no object ID, so no role assignment is generated (w) |
| XF205 | `restricted` mode with an existing VNet: virtual network rules are not generated (w) |
| XF206 | The address space cannot be split into the agent and private endpoint subnets (error) |
| XF207 | A container asks for anonymous access, which is disabled; it is created private (w) |

## Not generated

* Everything azd owns: the Foundry resource and project, model deployments, capability hosts,
  agents, toolboxes, MCP servers, connections, skills, routines and evaluation.
* Knowledge bases on the Search service (Phase 4).
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
