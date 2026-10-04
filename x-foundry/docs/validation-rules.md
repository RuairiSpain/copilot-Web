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
| XF020 | Foundry IQ and the standard agent setup need Azure AI Search; the resolved Search is shown in the normalised config | normaliser |
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
| XF120 | Project location differs from the Foundry resource or hub (w; error in private mode, where the VNet and workspace resources must share a region) |
| XF121 | IP rules: private ranges are invalid in service firewalls; `0.0.0.0/0` and `::/0` are rejected |
| XF122 | Outbound endpoints (MCP, connector, web source, federated issuer) must be https and not loopback or link-local |
| XF123 | Search sizing: per-SKU replica/partition limits, 36 search units, free tier has no private endpoints |
| XF124 | Gateway SKU in private mode: Consumption unsupported; BasicV2 cannot reach private backends (w) |
| XF125 | Azure Cache for Redis can no longer be created; use Azure Managed Redis |
| XF126 | Data residency forbids Global deployment SKUs (implicit deployments become `DataZoneStandard`) |
| XF127 | Network addressing: VNet settings need private mode; `addressSpace` must be a private range with room for the agent subnet; existing-VNet subnets must belong to the VNet and be supplied for private endpoints and the standard setup |
| XF128 | Agent service: `cosmos` needs the standard setup; the standard setup needs Storage, Cosmos DB and Search enabled; basic setup in private mode (w) |
| XF129 | Service tiers: Service Bus Premium and Container Registry Premium for private endpoints; capacity and zone redundancy are Premium only; Basic Service Bus has queues only |
| XF130 | Identity type: `systemAssigned` creates no user-assigned identity, so name, existing resource and federated credentials are rejected; image pull from the managed registry needs a user-assigned identity (w) |
| XF131 | Cosmos DB: throughput applies to provisioned capacity, not serverless |
| XF132 | Runtime image has no pinned tag or digest, or uses `latest` (w) |

## Well-Architected recommendations (XF3xx, warnings)

`XF301`-`XF330` are environment-profile recommendations for `test` and `prod`; they are
listed with their source in [waf-profiles.md](waf-profiles.md).
