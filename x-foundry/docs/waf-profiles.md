# Environment profiles and Well-Architected recommendations

`defaults.environment` is `dev`, `test` or `prod` (default `dev`). The environment selects
which **recommendations** the extension reports. Recommendations are warnings: they never
block validation or deployment, and they do not change what is generated. Each carries the
Well-Architected pillar it belongs to (`Diagnostic.pillar`, also in the plan JSON).

| Environment | What you get |
| --- | --- |
| `dev` | No recommendations. Minimal setup. |
| `test` | The security, operations and cost recommendations marked "test" below. Test should mirror production's shape at smaller scale. |
| `prod` | Every recommendation below. |

Preview a stricter environment without editing the file:

```bash
xfoundry validate azure.yaml --environment prod
```

When the environment is `dev`, `xfoundry validate` ends with a one-line note pointing at this
option. That is the golden-path nudge for now; prompting from inside azd comes with Phase 6.

## Source

The recommendations come from two Azure Architecture Center articles:

* [Baseline Microsoft Foundry chat reference architecture](https://learn.microsoft.com/en-us/azure/architecture/ai-ml/architecture/baseline-microsoft-foundry-chat)
  (production)
* [Basic Microsoft Foundry chat reference architecture](https://learn.microsoft.com/en-us/azure/architecture/ai-ml/architecture/basic-microsoft-foundry-chat)
  (proof of concept, and what it leaves out)

The article text was read from its source in the
[MicrosoftDocs/architecture-center](https://github.com/MicrosoftDocs/architecture-center)
repository because learn.microsoft.com was not reachable from the build environment. The
general [Well-Architected AI workload guidance](https://learn.microsoft.com/en-us/azure/well-architected/ai/get-started)
was only checked through search summaries, not read in full.

**The mapping of recommendations to dev, test and prod is a judgement call.** The articles
describe a basic (proof of concept) and a baseline (production) architecture and say to keep
experimentation, test and production in separate Foundry resources with their own
dependencies, but they do not publish a per-environment checklist.

## Recommendations

| Code | Pillar | Env | Recommendation | Article guidance |
| --- | --- | --- | --- | --- |
| XF301 | Reliability | prod | Search: standard tier or higher, at least three replicas | "at least three replicas" so the service spans zones; basic is a single instance without zones |
| XF302 | Reliability | prod | Storage: ZRS or GZRS, not LRS | "zone-redundant storage (ZRS)" or GZRS |
| XF303 | Reliability | prod | Cosmos DB: zone redundancy and continuous backup | zone redundancy; continuous backup, 7-day point-in-time restore |
| XF304 | Reliability | prod | Delete locks on Search, Cosmos DB and Storage | "delete resource lock to each service" |
| XF310 | Security | test, prod | Private mode: PaaS services over private endpoints, no public access | private endpoints; block public access to the Foundry data plane |
| XF311 | Security | test, prod | No local (key-based) authentication | Azure Policy for no key-based/local authentication; Entra ID for connections |
| XF312 | Security | prod | Purge protection | protect against catastrophic loss |
| XF313 | Security | prod | Egress through a firewall (`egress` not `azure-default`) | "force all outbound (egress) traffic through Azure Firewall" |
| XF314 | Security | test, prod | Bind a content-filter policy (`raiPolicy`) to chat deployments | "content-filter policy that screens prompts and model completions", managed as code |
| XF315 | Security | test, prod | Restrict MCP tools (`allowedTools`) | "Restrict MCP server available tools using allowed_tools" |
| XF316 | Security | prod | Azure Policy assignments | "Use Azure Policy to ensure all workload resources meet requirements" |
| XF317 | Security | prod | Defender for Cloud plans: servers, appService, cosmosDb, ai | plans for Servers, App Service, Cosmos DB and AI services |
| XF319 | Security | prod | Do not disable managed identities | distinct managed identities per component |
| XF320 | Operations | test, prod | Logs of every service to a Log Analytics workspace | "all available log categories for each service" |
| XF321 | Operations | prod | Keep alerts on | Azure Monitor baseline alerts |
| XF322 | Operations | test, prod | Pin model versions (`NoAutoUpgrade`) | "Set the deployment's version upgrade option to not auto-upgrade" |
| XF323 | Operations | test, prod | Run evaluations before promotion | evaluation framework; test suite of realistic questions |
| XF324 | Operations | prod | Agent subnet at least /24 | "/24 CIDR range" for the agent egress subnet |
| XF330 | Cost | test, prod | A budget with alerts | "set budgets and alerts early" (the basic architecture leaves cost controls out) |

XF301 replaces the earlier two-replica warning: the baseline article says three.

## What is not modelled yet

These appear in the articles but need schema support, generated infrastructure, or are
application concerns, so no recommendation can check them yet:

* Customer-managed keys; Azure Firewall, NSGs on every subnet, forced tunneling, and a
  dedicated MCP subnet; DDoS protection and Application Gateway WAF exclusions.
* Provisioned plus standard model deployments with spillover; Cosmos DB reserved capacity;
  `max_output_tokens` and truncation (application settings).
* A separate managed identity per Foundry project, project-level connections only, a Key
  Vault connection, assignment restrictions and regional isolation scope.
* Agent versioning, progressive rollout and rollback, source-controlled agent definitions
  (Phase 3 and 6), and application-layer per-user conversation isolation.
* Separate Foundry resources (ideally separate subscriptions) per environment. A single
  `azure.yaml` cannot see the other environments, so this is guidance only.

## Next step

Profiles currently only report. A later step could also supply defaults when you set nothing
(for example three Search replicas in prod). That changes cost, so it should be an explicit
decision rather than a side effect of the environment label.
