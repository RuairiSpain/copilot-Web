# Validation rules and diagnostic codes

`XF001`-`XF025` are the numbered rules from the specification's "Required semantic
validation outside JSON Schema". `XF1xx` codes cover parsing and checks beyond that list.
Severity is *error* unless marked (w).

## Specification rules

| Code | Rule | Phase |
| --- | --- | --- |
| XF001 | Project names are unique (and differ from the hub name) | declared |
| XF002 | Names unique within their effective scope: agents, deployments, toolboxes, MCPs, knowledge bases, connectors, and nested lists | declared |
| XF003 | `hub` only when `topology.mode` is `hub-spoke`, and required then | declared |
| XF004 | Explicit `inheritHub: true` needs a hub (the default resolves to false without one) | declared |
| XF005 | Referenced projects, models, toolboxes, MCPs, knowledge bases, connectors, embedding deployments exist | declared + effective |
| XF006 | `models.default` is a deployment or allowed model, and not denied | effective |
| XF007 | `vector.dimensions` matches the embedding deployment (`3-large` up to 3072, `3-small` up to 1536, `ada-002` exactly 1536) and vector field | effective |
| XF008 | `filterFields` and `filterClaims` reference filterable fields | effective |
| XF009 | `keyField`, `contentField`, `titleField`, `vectorField` and semantic fields exist with usable types; one key field | effective |
| XF010 | Vector fields are `Collection(Edm.Single)` with dimensions and a vector profile | effective |
| XF011 | `chunking.overlap` < `chunking.size` | effective |
| XF012 | Hybrid weights sum to 1.0 | effective |
| XF013 | Gateway endpoint paths (and project registrations) are unique | declared + effective |
| XF014 | Gateway endpoint targets resolve to an agent, runtime, model, Search or knowledge base | effective |
| XF015 | Gateway quota, limit and routing (backend) profile references exist | effective |
| XF016 | Allowed and denied model sets do not overlap; deployments and agents respect them | declared + effective |
| XF017 | Raw secret values are rejected; only Key Vault references | declared |
| XF018 | Session-pool or agent-pool settings are rejected | parser |
| XF019 | Redis is application caching only (no session settings) | parser |
| XF020 | Foundry IQ needs Azure AI Search; the resolved Search is shown in the normalised config | normaliser |
| XF021 | Private mode: no public access, identity enabled, DNS, internal gateway, no external ingress | declared + effective |
| XF022 | Existing resources cannot carry creation settings; resource IDs must match the component type; registry modes | declared |
| XF023 | Region names, data residency, cross-region inheritance (w) | declared |
| XF024 | Explicit names meet each Azure provider's constraints | declared |
| XF025 | Destructive changes need approval | **Phase 3** (needs deployed state) |

## Other codes

| Code | Meaning |
| --- | --- |
| XF100 | File unreadable or invalid YAML (including duplicate keys) |
| XF101 | Missing or malformed `x-foundry` section |
| XF102 | JSON Schema violation |
| XF103 | Unsupported `schemaVersion` major |
| XF104 | A tag in `governance.requiredTags` is missing for a project |
| XF105 | Network mode `restricted` needs `allowedIps` or an existing VNet |
| XF106 | Component public access or local auth contradicts the global setting; Redis service/SKU mismatch |
| XF107 | A component is disabled but required (storage for sources, ADLS needs hierarchical namespace) |
| XF108 | Runtime: replicas, registry authentication, CPU/memory combination (w) |
| XF109 | Event entity rules (parents, provider support) |
| XF110 | Pydantic model validation (backstop behind JSON Schema) |
| XF111 | Gateway: registration needs an enabled gateway, chargeback needs tracking, telemetry sink |
| XF112 | Agent: prompt agents need a model, hosted agents need a source or runtime |
| XF113 | Governance model policy (allowed/denied models, SKUs) |
| XF114 | No administrators configured (w) |
| XF115 | A project widens the allowed models it inherits |
| XF117 | Retrieval needs vectors / semantic ranking that the index or Search service does not provide |
| XF118 | Invalid cron expression; explicit routing without routes |
| XF119 | `apiKey` authentication without `secretRef` |
| XF120 | Project location differs from the Foundry resource or hub (w) |
