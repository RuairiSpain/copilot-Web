# Foundry Doctor: rule catalog (draft 1)

This is the first list of rules for `azd foundry doctor`, plus the features around them that make
the findings usable (explain, baselines, suppressions, preflight, runtime diagnosis, cost,
environment comparison). It is a draft for review, not a specification.

**Status of the facts in this file.** Nothing here was checked against current Microsoft
documentation or a live subscription (the development sandbox cannot reach learn.microsoft.com and
has no Azure access). Every rule needs its source link and a property name confirmed before it is
implemented. The "Origin" column says which x-foundry rule (XFnnn) the check comes from, so the
logic and tests can be ported; `new` means it has no x-foundry ancestor.

## 1. How to read the tables

**Input** is what the rule needs, which decides which command can run it offline:

| Code | Input | Needs |
| --- | --- | --- |
| Y | `azure.yaml` (azd services) | nothing |
| A | Bicep compiled to ARM JSON (`bicep build`), or the azd-synthesised template | Bicep CLI |
| L | Live Azure state | Azure login with Reader (some need more, see section 7) |
| R | Runtime probe | network vantage point or data-plane access (section 8) |

**Severity** is `dev/test/prod`: `E` error (fails the run), `W` warning, `I` information,
`-` off. A profile is only a severity column; a rule never changes its logic by profile.

**Category** follows the PRD: `M` must have, `N` nice to have, `O` optional, `C` costly.

**Ph** is the phase the rule should ship in: 1 = MVP (offline, no Azure login), 2 = needs Azure
login (preflight and correlation), 3 = runtime probes and cost.

**Basis** is why the rule exists: **P** platform constraint (deployment or runtime fails or is
rejected), **S** security guidance, **W** Well-Architected recommendation, **H** hygiene or
convention. A rule can have two.

## 2. Rule catalog

### CFG: azure.yaml and configuration correctness

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-CFG-001 | `azure.yaml` is valid YAML with no duplicate keys, and each `azure.ai.*` service validates against its schema. Fix: the diagnostic names the key. | Y | E/E/E | M | P | XF100–102 | 1 |
| FND-CFG-002 | Service names are unique per service kind (agents, toolboxes, connections, skills, routines). | Y | E/E/E | M | P | XF002 | 1 |
| FND-CFG-003 | Every reference resolves: an agent's toolbox, connection, model deployment, or knowledge source exists in `azure.yaml` or is a known existing resource. | Y | E/E/E | M | P | XF005 | 1 |
| FND-CFG-004 | A prompt agent names a model that is declared on the project's deployments; a hosted agent has a buildable source or image. | Y | E/E/E | M | P | XF006, XF112 | 1 |
| FND-CFG-005 | No raw secret values in `azure.yaml`, Bicep parameters or environment blocks (key names and value patterns: SAS signatures, JWTs, connection strings, private keys). Fix: use a Key Vault reference. | Y, A | E/E/E | M | S | XF017 | 1 |
| FND-CFG-006 | Every `${VAR}` used in `azure.yaml` is set in the selected azd environment or produced by the infrastructure. | Y, L | W/E/E | M | P | new | 1 |
| FND-CFG-007 | Outbound URLs (MCP servers, connection endpoints, web sources, federated issuers) use https and do not target loopback, link-local or metadata addresses. | Y | E/E/E | M | S | XF122 | 1 |
| FND-CFG-008 | MCP tools are restricted with an allow list instead of exposing every tool. | Y | -/W/W | N | S | XF315 | 1 |
| FND-CFG-009 | Cron expressions (routines, refresh schedules) are valid five-field expressions. | Y | E/E/E | M | P | XF118 | 1 |
| FND-CFG-010 | Model deployments pin their version (no automatic upgrade) in test and prod. | Y, A | -/W/W | N | W | XF322 | 1 |
| FND-CFG-011 | A field, service host or API version that the installed azd Foundry extension marks as preview or retired is used. Version-aware: rule packs are tied to the extension version. | Y | I/W/W | N | H | new | 1 |
| FND-CFG-012 | Region names are valid Azure regions. | Y, A | E/E/E | M | P | XF023 | 1 |

### SEC: security

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-SEC-001 | Foundry account has local (key) authentication disabled (`disableLocalAuth`). | A | W/E/E | M | S | XF106, XF311 | 1 |
| FND-SEC-002 | AI Search has API-key authentication disabled (or role-based only). | A | W/E/E | M | S | XF311 | 1 |
| FND-SEC-003 | Cosmos DB has local authentication disabled. | A | W/E/E | M | S | XF311 | 1 |
| FND-SEC-004 | Storage disables shared-key access, anonymous blob access, and requires https and TLS 1.2 or later. | A | W/E/E | M | S | XF311, XF207 | 1 |
| FND-SEC-005 | Key Vault has soft delete and purge protection on, and uses RBAC authorisation. | A | -/W/E | M | S | XF312 | 1 |
| FND-SEC-006 | Firewall IP rules contain no private ranges (service firewalls reject them) and no `0.0.0.0/0` or `::/0`. | A | E/E/E | M | P, S | XF121 | 1 |
| FND-SEC-007 | A content-filter (RAI) policy is bound to every chat deployment. | A | -/W/W | N | S | XF314 | 1 |
| FND-SEC-008 | Microsoft Defender for Cloud plans enabled for the relevant services (AI, Cosmos DB, App Service, Servers). | L | -/-/W | N | S | XF317 | 2 |
| FND-SEC-009 | Azure Policy assignments at the scope cover no local auth, explicit network rules, encryption and allowed regions; policy compliance state is clean. | L | -/-/W | N | S | XF316 | 2 |
| FND-SEC-010 | Customer-managed keys for Foundry, Storage and Cosmos DB. | A | -/-/I | O | S | roadmap comment | 1 |
| FND-SEC-011 | Outbound traffic from the agent subnet is routed through a firewall or controlled egress. | A, L | -/-/I | O | S | XF313 | 2 |
| FND-SEC-012 | Gateway telemetry does not log prompts or completions unless explicitly opted in; user identifiers are pseudonymised. | A | W/W/E | M | S | review item 15 | 1 |
| FND-SEC-013 | Gateway restricts callers by tenant ID (`tid`), not by email domain alone. | A | -/W/E | M | S | review item 15 | 1 |
| FND-SEC-014 | Secrets and keys are not emitted as Bicep outputs or deployment outputs. | A | E/E/E | M | S | review item 5 | 1 |

### NET: networking

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-NET-001 | Foundry account has public network access disabled and a private endpoint (prod). | A | -/W/E | M | S | XF310 | 1 |
| FND-NET-002 | AI Search, Cosmos DB, Storage and Key Vault have public access disabled or default-deny network rules, with private endpoints in prod. One finding per service. | A | -/W/E | M | S | XF310 | 1 |
| FND-NET-003 | Every private endpoint has a DNS zone group, and the zone exists and is linked to the VNet. Expected zones: cognitive services, OpenAI, Foundry services, Search, Blob, Key Vault, Cosmos DB. | A | W/E/E | M | P | normaliser | 1 |
| FND-NET-004 | The agent subnet is delegated to `Microsoft.App/environments`, meets the minimum size, and is /24 or larger as recommended. One agent subnet per Foundry resource. | A | W/E/E | M | P, W | XF127, XF324 | 1 |
| FND-NET-005 | The agent subnet, the private-endpoint subnet and every workspace resource are in the same VNet and region in private mode. | A | E/E/E | M | P | XF120, XF127 | 1 |
| FND-NET-006 | The VNet address space is a private range with room for both subnets; it does not overlap peered or on-premises ranges. | A, L | E/E/E | M | P | XF127 | 1 |
| FND-NET-007 | Restricted mode has at least one public allowed IP range. | A | E/E/E | M | P | XF105 | 1 |
| FND-NET-008 | Search SKU supports private endpoints (not the free tier) when private access is required. | A | E/E/E | M | P | XF123 | 1 |
| FND-NET-009 | APIM SKU can reach private backends (not Consumption; BasicV2 has no outbound VNet integration). | A | E/E/E | M | P | XF124 | 1 |
| FND-NET-010 | Application Insights and Log Analytics ingestion stays public unless an Azure Monitor Private Link Scope is used; report as information so the choice is explicit. | A | I/I/W | N | S | roadmap (medium) | 1 |
| FND-NET-011 | Network security groups on subnets and DDoS protection on the VNet in prod. | A | -/-/I | O | S | roadmap (high) | 1 |

### IDN: identity and access

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-IDN-001 | Foundry account and projects use a managed identity; app workloads use managed identity, not keys. | A | W/E/E | M | S | XF130, XF319 | 1 |
| FND-IDN-002 | In the standard agent setup, the project identity has the roles it needs on its Storage, Cosmos DB and Search (data roles, with the right scope). Missing roles are the most common reason a deployment works but agents fail. | A, L | W/E/E | M | P | new | 1 |
| FND-IDN-003 | Role assignments set `principalType` and use object IDs; display names without IDs are flagged. | A | W/W/W | N | H | XF201 | 1 |
| FND-IDN-004 | No Owner or broad Contributor granted to application identities; built-in Foundry roles used instead. | A, L | W/W/E | M | S | review item 4 | 1 |
| FND-IDN-005 | Human principals with standing administrator roles in prod; recommend just-in-time access. | L | -/-/I | N | S | XF114 | 2 |
| FND-IDN-006 | Federated credentials use an https issuer and a specific subject. | A | E/E/E | M | S | XF122, XF130 | 1 |

### REL: reliability

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-REL-001 | Search in prod uses a tier and replica count that meet the availability target (two replicas for read, three for read-write); one replica has no SLA. | A | -/W/W | N | W | XF301 | 1 |
| FND-REL-002 | Storage uses zone- or geo-redundant replication in prod. Falls back to LRS with a note where ZRS is unavailable in the region. | A | -/W/W | N | W | XF302 | 1 |
| FND-REL-003 | Cosmos DB is zone-redundant with continuous backup in prod. | A | -/W/W | N | W | XF303 | 1 |
| FND-REL-004 | Delete locks on stateful resources (Storage, Search, Cosmos DB, Key Vault). Note: locks block `azd down` until removed. | A | -/W/W | N | W | XF304 | 1 |
| FND-REL-005 | Search sizing is within platform limits (per-SKU replicas and partitions, 36 search units). | A | E/E/E | M | P | XF123 | 1 |
| FND-REL-006 | Gateway retry and circuit-breaker settings exist for model backends. | A | -/I/W | N | W | gateway | 1 |
| FND-REL-007 | Multi-region failover and disaster recovery plan for prod. | A | -/-/I | C | W | roadmap (high) | 1 |
| FND-REL-008 | Provisioned-throughput deployments with spillover to standard. | A | -/-/I | C | W | roadmap (high) | 1 |
| FND-REL-009 | Zone-redundant or premium APIM for the gateway in prod. | A | -/-/I | C | W | new | 1 |

### OPS: operations and governance

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-OPS-001 | Diagnostic settings send logs and metrics to Log Analytics for each resource. | A | -/W/E | M | W | XF320 | 1 |
| FND-OPS-002 | Application Insights is connected to the Foundry project. | A | -/W/W | N | W | new | 1 |
| FND-OPS-003 | Baseline alerts exist (gateway errors, Search throttling, model throttling, Cosmos DB RU). | A, L | -/-/W | N | W | XF321 | 1 |
| FND-OPS-004 | Required tags are present on resources (for example `environment`, `owner`, `cost-center`). | A | W/W/E | M | H | XF104 | 1 |
| FND-OPS-005 | Explicit resource names meet each provider's constraints (length, characters, case, global uniqueness pattern). | Y, A | E/E/E | M | P | XF024 | 1 |
| FND-OPS-006 | Log retention is at least the policy minimum in prod. | A | -/-/W | N | W | new | 1 |
| FND-OPS-007 | A CI/CD pipeline exists per environment (for example `.github/workflows` or the azd pipeline config). | Y | -/I/W | O | W | roadmap (pipeline scaffolding) | 1 |
| FND-OPS-008 | Evaluation datasets or evaluators are defined and run before promotion. Applies only if azd exposes evaluation in `azure.yaml`; otherwise a file-convention check. | Y | -/W/W | N | W | XF323 | 1 |
| FND-OPS-009 | Region is inside the allowed data-residency list; no global deployment SKUs when residency is restricted. | A | E/E/E | M | S | XF023, XF126 | 1 |
| FND-OPS-010 | Model allow/deny lists are consistent across projects and the gateway; a project does not widen an inherited list. | Y, A | E/E/E | M | H | XF016, XF113, XF115 | 1 |

### COST: cost

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-COST-001 | A budget with alerts exists at the resource group or subscription. | L | -/-/W | N | W | XF330 | 2 |
| FND-COST-002 | A dev profile does not use prod-sized SKUs (Search standard with several replicas, provisioned throughput, geo-redundant storage). | A | W/I/- | N | W | new | 1 |
| FND-COST-003 | Token quotas or limits exist at the gateway. | A | -/W/W | N | W | gateway | 1 |
| FND-COST-004 | Monthly cost estimate for the planned resources, per profile (section 6). Informational, never fails. | A | I/I/I | O | W | new | 3 |

### PERF and IQ: retrieval and knowledge

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-IQ-001 | `filterFields` and `filterClaims` reference index fields marked filterable. | Y | E/E/E | M | P | XF008 | 1 |
| FND-IQ-002 | Key, content, title, vector and semantic fields exist, with one key field. | Y | E/E/E | M | P | XF009 | 1 |
| FND-IQ-003 | Vector fields are `Collection(Edm.Single)` with dimensions and a vector profile. | Y | E/E/E | M | P | XF010 | 1 |
| FND-IQ-004 | Vector dimensions match the embedding model (`text-embedding-3-large` up to 3072, `-3-small` up to 1536, `ada-002` exactly 1536). | Y | E/E/E | M | P | XF007 | 1 |
| FND-IQ-005 | Retrieval modes (vector, hybrid, semantic) are enabled in the index and on the Search service tier. | Y, A | E/E/E | M | P | XF117 | 1 |
| FND-IQ-006 | Chunk overlap is smaller than chunk size; hybrid weights sum to 1.0. | Y | E/E/E | M | H | XF011, XF012 | 1 |
| FND-IQ-007 | Knowledge source type is supported by the Search API version in use; an LLM deployment exists for query planning. | Y | W/E/E | M | P | review item 10 | 1 |
| FND-IQ-008 | Source connections use managed identity, not keys. | Y, A | W/E/E | M | S | new | 1 |
| FND-IQ-009 | Document-level access control is configured when a source has per-user permissions (SharePoint, ACL-aware Blob), so retrieval cannot expose documents the caller may not read. | Y | W/W/E | M | S | new | 1 |
| FND-IQ-010 | ADLS Gen2 sources need hierarchical namespace on the storage account. | Y, A | E/E/E | M | P | XF107 | 1 |
| FND-IQ-011 | Index refresh schedule is valid and not more frequent than the source can support. | Y | E/E/E | M | P | XF118 | 1 |
| FND-IQ-012 | Search semantic ranking is enabled when retrieval uses it. | A | E/E/E | M | P | XF117 | 1 |

### GW: API Management AI gateway

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-GW-001 | JWT validation policy checks issuer, audience and tenant. | A | W/E/E | M | S | gateway | 1 |
| FND-GW-002 | Token limit and token metric policies are present on model routes (policy names to confirm: `llm-token-limit`, `llm-emit-token-metric`). | A | -/W/W | N | W | gateway | 1 |
| FND-GW-003 | Gateway routes resolve to existing backends (model, Search, knowledge base, agent). | A | E/E/E | M | P | XF014 | 1 |
| FND-GW-004 | Route paths are unique. | A | E/E/E | M | P | XF013 | 1 |
| FND-GW-005 | The gateway calls Foundry with managed identity, not keys. | A | W/E/E | M | S | new | 1 |
| FND-GW-006 | Gateway telemetry sink exists when token tracking is used. | A | E/E/E | M | P | XF111 | 1 |

### DEP: deployment preflight (answers "can I deploy this safely?")

All need Azure login (phase 2). Most use Reader; the permission check itself needs permission to
read role assignments at the target scope.

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-DEP-001 | Signed in to the tenant and subscription that match the azd environment. | L | E/E/E | M | P | new | 2 |
| FND-DEP-002 | Required resource providers are registered (Cognitive Services, Search, Cosmos DB, Storage, Key Vault, Network, App for subnet delegation, API Management, Insights, Operational Insights). | L | E/E/E | M | P | new | 2 |
| FND-DEP-003 | The deploying principal can create resources and role assignments at the scope (for example Contributor plus Role Based Access Control Administrator, or User Access Administrator), and has Foundry roles needed for data-plane steps. | L | E/E/E | M | P | review item 16 | 2 |
| FND-DEP-004 | Each model, version and SKU in the plan is offered in the target region. | L | E/E/E | M | P | new | 2 |
| FND-DEP-005 | Remaining quota (tokens per minute) covers each deployment's requested capacity in the region. | L | E/E/E | M | P | new | 2 |
| FND-DEP-006 | No soft-deleted Foundry account, Key Vault or APIM with the same name blocks creation (purge or choose a new name). | L | E/E/E | M | P | review item 13 | 2 |
| FND-DEP-007 | Globally unique names (Storage, Key Vault, Search, Cognitive Services custom subdomain) are available. | L | E/E/E | M | P | new | 2 |
| FND-DEP-008 | `what-if` (via `azd provision --preview` or ARM) reports no unexpected deletes or replacements. | L | W/E/E | M | P | new | 2 |
| FND-DEP-009 | Policy assignments with deny effect at the scope would not block the planned resources. Best effort: what-if does not evaluate policy, so this reads assignments and tests the main properties. | L | W/E/E | M | P | new | 2 |
| FND-DEP-010 | Existing locks on the resource group or resources would block the change. | L | E/E/E | M | P | review item 13 | 2 |
| FND-DEP-011 | An existing subnet has no conflicting delegation, enough free addresses, and is not already used by another Foundry resource's agent service. | L | E/E/E | M | P | XF127 | 2 |
| FND-DEP-012 | The target region supports the agent service setup selected (standard setup needs all workspace resources in one region). | L | E/E/E | M | P | XF120 | 2 |

### RUN: runtime diagnosis (answers "why does my agent fail?")

These are the checks a static linter cannot do. They need a data-plane or network vantage point,
so each reports `skipped` with the reason when the vantage point is missing, never a pass.

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-RUN-001 | Effective access: the project identity holds the roles on Search, Storage and Cosmos DB that the agent features need (role assignments read, no data call). | L | W/E/E | M | P | new | 2 |
| FND-RUN-002 | Private endpoint DNS names resolve to private addresses from inside the VNet (Network Watcher connectivity check or a probe run in the VNet). | R | W/E/E | M | P | new | 3 |
| FND-RUN-003 | The capability host is `Succeeded` and the project has its Cosmos DB, Storage and Search connections. | L | W/E/E | M | P | new | 3 |
| FND-RUN-004 | Model deployments are `Succeeded` and the throttling rate (429) over the last 24 hours is below a threshold. | L | I/W/W | N | P | new | 3 |
| FND-RUN-005 | Each agent's tools and connections exist in the project and respond. | R | W/E/E | M | P | new | 3 |
| FND-RUN-006 | Search index exists, has documents, its vector profile matches the definition, and the last indexer run succeeded. | R | W/E/E | M | P | new | 3 |
| FND-RUN-007 | Diagnostic logs are arriving in Log Analytics (latest record within a threshold). | L | I/W/W | N | W | new | 3 |
| FND-RUN-008 | Deployed properties match the compiled template (configuration drift between ARM and Azure). | A, L | I/W/E | N | W | roadmap Phase 7 | 3 |

### ENV: comparing environment files

Run with `azd foundry doctor --compare azure.dev.yaml azure.prod.yaml`.

| ID | Check and fix | Input | d/t/p | Cat | Basis | Origin | Ph |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FND-ENV-001 | Two environments do not target the same resource group. Separate subscriptions are stronger and are a recommendation only. | Y, A | E/E/E | M | S | roadmap (doctor) | 1 |
| FND-ENV-002 | Settings tied to one deployment (names, resource IDs, IP rules, resource groups, budgets) were not copied unchanged from another environment. | Y, A | W/W/W | M | H | roadmap (promote) | 1 |
| FND-ENV-003 | Existing-resource IDs in a prod file do not point at a non-prod subscription or resource group. | Y, A | -/E/E | M | S | new | 1 |
| FND-ENV-004 | Summary of differences between environments (informational): SKUs, redundancy, network mode, capacity. | Y, A | I/I/I | O | H | new | 1 |

## 3. The finding and the rule definition

The PRD's `Finding` struct is too small to be useful. Proposed fields:

```go
type Finding struct {
    RuleID         string   // FND-NET-004
    RuleVersion    int      // bumped when the check logic changes
    Severity       Severity // after profile resolution
    Category       Category // MustHave, Nice, Optional, Costly
    Pillar         string   // Security, Reliability, ...
    Basis          []string // Platform, Security, WAF, Hygiene
    Profile        string
    Resource       ResourceRef // ARM ID or azure.yaml service name
    Location       Location    // file, line, column (azure.yaml or Bicep source map)
    Evidence       string      // the value found, secrets redacted
    Recommendation string
    Fix            string      // a YAML or Bicep snippet where one exists
    DocsURL        string
    Confidence     string      // certain, likely, skipped (with reason)
    Suppressed     *Suppression
    Baselined      bool
}
```

A rule is declared as data, with the check in Go:

```yaml
id: FND-NET-004
title: Agent subnet is delegated and large enough
input: [arm]
category: must
basis: [platform, waf]
severity: { dev: warn, test: error, prod: error }
docs: https://...        # to be filled and verified
explain: |
  The agent service injects its containers into this subnet...
fix: |
  subnet delegations: Microsoft.App/environments, prefix /24 or larger
```

Rules ship in a **versioned rule pack** tied to the azd Foundry extension version, so a preview
schema change does not silently break a rule (see FND-CFG-011).

## 4. Features that make findings usable

### 4.1 `explain`

`azd foundry doctor --explain FND-IDN-002` prints: what the rule checks, why it matters, how it
fails in practice, the fix, and the source link. It works offline, because the text ships in the
rule pack. This is the main way to make Foundry less opaque for developers who do not know PSRule,
Policy or the WAF vocabulary.

### 4.2 Baselines and suppressions

Without these a team with existing findings cannot enable the doctor in CI.

```yaml
# .foundry-doctor/baseline.yaml  (generated by: doctor --write-baseline)
generated: 2026-10-04
findings:
  - rule: FND-REL-004
    resource: Microsoft.Storage/storageAccounts/stfoundry
    fingerprint: 3f9a...      # rule + resource + normalised evidence
```

```yaml
# .foundry-doctor/suppressions.yaml
- rule: FND-NET-001
  resource: Microsoft.CognitiveServices/accounts/foundry-dev
  reason: "Dev only; public access accepted by security (ticket SEC-123)"
  expires: 2027-03-31        # required; expired suppressions fail the run
  owner: platform-team
```

Rules: a suppression needs a reason and an expiry; a baseline only hides findings that existed when
it was written; new findings still fail; the report always lists suppressed and baselined counts so
they stay visible.

### 4.3 Exit codes and thresholds

| Exit code | Meaning |
| --- | --- |
| 0 | No findings at or above the threshold |
| 1 | Findings at or above the threshold (`--fail-on error` by default) |
| 2 | The doctor could not run (invalid input, missing Bicep CLI, no login for a phase-2 rule set) |
| 3 | Some checks were skipped and `--strict` was set |

A skipped check is never reported as passed. The report shows how many checks ran, passed, failed
and were skipped, and why.

### 4.4 Reports

* **Console:** grouped by severity, one line per finding with `file:line`.
* **JSON and SARIF:** for CI and GitHub code scanning. SARIF needs accurate source locations,
  which depend on the Bicep source map (section 9, point 1).
* **Markdown and HTML:**
  * *Owner summary* (one page): go or no-go for the profile, the top five fixes, counts by pillar,
    estimated monthly cost, and what was not checked.
  * *Developer detail*: every finding with evidence and fix.
  * *Evidence pack*: the passed controls with their evidence, for a security review.

### 4.5 Annotated copies

`--annotate-copy` writes review copies of `azure.yaml` and the Bicep with the finding as a comment
above the offending line (`# FND-NET-001 [MUST] ...`). Comment prefixes make the command
idempotent, so rerunning replaces its own comments and leaves the user's. This reuses the
`yaml.Node` approach designed for `doctor` in x-foundry.

## 5. Profiles

A profile is a severity column plus the categories it enables.

| Profile | Fails on | Enabled categories | Intent |
| --- | --- | --- | --- |
| `foundry-dev` | nothing by default (warnings) | M, with N as information | Fast inner loop; catches things that would fail a deployment |
| `foundry-test` | errors | M and N | Closer to prod without the cost items |
| `foundry-prod` | errors | M, N; O and C as information | Production readiness |

Rules with Basis **P** (platform) are errors in every profile, because they fail the deployment
regardless of the environment.

## 6. Cost estimate (FND-COST-004)

* Inputs: the compiled template resources, SKUs, capacities and region.
* Prices: the Azure Retail Prices API (public, no login). Cache the results with a date.
* Output: a monthly range per resource and per profile, plus which assumptions were made (for
  example "model usage not included; deployments billed per token").
* Limits to state in the report: model token consumption cannot be estimated from configuration;
  only fixed-capacity items (Search units, APIM units, Cosmos DB RU/s, provisioned throughput) can.
* Never fails the run.

## 7. Preflight permissions

`doctor --preflight` needs the user to be signed in. Minimum access: Reader on the target scope for
most checks. FND-DEP-003 needs permission to read role assignments; FND-DEP-005 and FND-DEP-004 use
subscription-level model and quota listing. Each check that cannot run for lack of permission
reports `skipped (needs <permission>)`.

## 8. Runtime probes

Probes run only with `--runtime`. Static analysis and preflight remain the default so the tool is
safe to run in a pull request. A probe that needs to be inside the VNet (FND-RUN-002) can run as:

1. a Network Watcher connectivity check, which needs no host in the VNet; or
2. a documented command the user runs from a jump host or pipeline agent inside the VNet.

Probes read; they never write to the data plane, and they never print secrets or document content.

## 9. Open points to settle before coding

1. **Bicep analysis.** Compile with `bicep build` and analyse the ARM JSON. Findings need a source
   map back to Bicep lines for SARIF and annotated copies. Many azd Foundry projects have no
   Bicep, so decide whether the doctor analyses the template azd would synthesise
   (`azd provision --preview`) or only `azure.yaml`.
2. **Overlap check before writing rules.** For each `FND-SEC`, `NET`, `REL` and `OPS` rule,
   record whether PSRule for Azure, Azure Policy built-ins or Defender already covers it. Where
   they do, decide between wrapping and reimplementing. Possible outcomes: a self-contained Go
   engine for must-have rules, plus optional adapters for PSRule and Checkov when installed.
3. **Field names.** The `azure.yaml` field names in section 2 (`deployments`, `network`, agent,
   toolbox and connection fields) come from one azd reference that was read during x-foundry
   work. Confirm each against the installed azd Foundry extension before writing the rule.
4. **ARM property names and API versions** in the `A` rules (for example `disableLocalAuth`,
   `publicNetworkAccess`, `networkAcls`, `allowProjectManagement`) need confirming against the
   resource provider schemas.
5. **Gateway policy names** (`llm-token-limit`, `llm-emit-token-metric`) were recalled, not
   fetched; confirm.
6. **Evaluation in azd.** FND-OPS-008 depends on whether `azure.yaml` can declare evaluations.
7. **Runtime probes** (section 8) cannot be tested without a subscription; plan an integration
   test subscription before Phase 3.
8. **Source links.** Every rule needs a Microsoft documentation link and a "last verified" date in
   the rule pack.

## 10. What carries over from x-foundry

The catalog has 108 rules: 74 must have, 25 nice to have, 6 optional, 3 costly. By phase, 82 are
offline (phase 1), 18 need an Azure login (phase 2) and 8 need runtime access or cost data (phase 3).

| Origin | Meaning | Rules |
| --- | --- | --- |
| XFnnn code | Port the check logic and tests to the new input format | 59 |
| roadmap or review item | Designed or recommended earlier, no code | 22 |
| new | No ancestor | 27 |

The ported checks keep their unit tests: the XF tests become fixtures (an input that should fire
the rule, and one that should not) in the new test suite.

## 11. Suggested first release

Phase 1 (offline, no login): all `Ph 1` rules with Basis **P** or **S** and category **M**, the
profiles, explain, baselines and suppressions, console, JSON, Markdown and SARIF output, and the
environment comparison. Defer annotated copies, graph and HTML to a later release. Phase 2 adds
the DEP rules and FND-RUN-001. Phase 3 adds runtime probes, cost, and drift.
