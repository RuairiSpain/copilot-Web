FOUNDRY DOCTOR
Implementation-Ready Product Requirements Document
Azure Developer CLI extension and standalone Foundry rule platformGo implementation • phased one-shot Claude Code execution
| Document field | Value |
| Status | Approved architecture and phased implementation baseline |
| Audience | Claude Code implementation agents, maintainers, reviewers and Foundry platform teams |
| Primary implementation | Go azd extension with a reusable standalone CLI core |
| Primary command namespace | azd foundry |
| Core principle | Deterministic findings; optional LLM explanation only |
| Rule model | Versioned Foundry rule packs with dev, test and prod profiles |
| Date | 4 October 2026 |

This document is intentionally implementation-heavy. Each phase is written so it can be supplied to Claude Code as a self-contained execution prompt after the preceding phase artefacts have been merged.
# 1. Executive Summary
Foundry Doctor is a Foundry-aware rule platform delivered primarily as an Azure Developer CLI extension. It validates azure.yaml, compiled Bicep/ARM, azd environment configuration and selected deployed control-plane and data-plane state. It answers four operational questions: Is the project structurally valid? Is a deployment likely to succeed? Is the deployed workload healthy? What must change before promotion to a stricter environment?
The product differentiator is not generic tool orchestration. It is the versioned Foundry rule catalogue and the correlation model that joins Foundry declarations, infrastructure definitions, azd environment values, Azure resources, project connections and runtime dependencies.
## Product outcomes
- One command for local validation: azd foundry doctor.
- A preflight mode for deployment readiness: azd foundry doctor --preflight.
- A runtime diagnosis mode: azd foundry doctor --runtime.
- Environment comparison: azd foundry doctor --compare dev prod.
- Offline rule explanation: azd foundry explain FND-IDN-002.
- WAF assessments with owner, developer and security evidence outputs.
- Adoption support through baselines, expiring suppressions and stable CI exit codes.
# 2. Product Principles and Boundaries
| Principle | Requirement |
| Rule catalogue is the product | Rules, rationale, fixes, evidence and documentation are versioned first-class artefacts. |
| Deterministic first | Only deterministic engines create pass/fail findings. LLMs may explain approved findings after explicit opt-in. |
| Read-only doctor | Doctor reads source and Azure state. It does not take ownership from azd or mutate live resources. |
| No hidden dependencies | Core must-have offline rules run in the Go binary. Optional adapters report missing tools rather than silently skipping. |
| Adoptable by existing projects | Baselines hide only known fingerprints; new findings still fail. Suppressions require reason and expiry. |
| Evidence over scores | Findings include source, evidence, confidence and remediation. The tool must not claim complete WAF compliance. |
| Azure boundaries are explicit | Control-plane, data-plane and runtime checks are separate and declare required permissions. |
| Preview-aware | Rule packs declare compatible azd and Foundry extension versions and fail safely on unknown schema versions. |

# 3. User Personas and Jobs
| Persona | Primary job | Expected experience |
| Foundry developer | Catch configuration and dependency errors before deployment | Fast offline checks, actionable file/line findings and fixes. |
| Platform engineer | Apply consistent environment guardrails | Profiles, baselines, suppressions, CI and evidence. |
| Product development manager | Understand release readiness and top risks | One-page owner summary and environment comparison. |
| Security architect | Review controls and exceptions | Evidence pack, rule sources, expired suppressions and deployed-state evidence. |
| Support engineer | Diagnose agent failures | Runtime checks for identity, connections, DNS and service health. |
| Advanced user | Use organisation-specific low-level tools | Escape hatch for PSRule, Checkov, Bicep and policy configuration. |

# 4. Command Surface
azd foundry doctor [--profile dev|test|prod] [--local] [--preflight] [--runtime]                   [--security] [--rules <selector>] [--min-severity <level>]                   [--baseline <file>] [--suppressions <file>] [--strict]                   [--format console|json|markdown|sarif] [--output <path>]azd foundry explain <rule-id> [--format console|markdown]azd foundry compare <left-env> <right-env> [--format ...]azd foundry assess waf [--profile ...] [--llm-explain] [--evidence-pack]azd foundry graph [--source|--deployed|--combined] [--format mermaid|json|html]azd foundry annotate --profile test|prod --output <review-dir>azd foundry cost [--profile ...] [--currency EUR]azd foundry scaffold [--from-findings <report.json>]
If the selected azd extension namespace conflicts with a first-party command discovered during Phase 0, retain the same internal command model and rename the public prefix to azd guard or foundry-doctor. Namespace resolution is a Phase 0 release gate.
# 5. System Architecture
flowchart LR  CLI[azd foundry / standalone CLI] --> CFG[Unified config resolver]  CFG --> SRC[Source acquisition]  SRC --> AY[azure.yaml AST]  SRC --> BC[Bicep adapter]  BC --> ARM[Compiled ARM model]  AY --> CORR[Correlation graph]  ARM --> CORR  CORR --> ENGINE[Native Go rule engine]  ENGINE --> FIND[Normalised findings]  CFG --> ADAPTERS[Optional external adapters]  ADAPTERS --> FIND  FIND --> BASE[Baseline and suppression filter]  BASE --> REPORT[Console JSON SARIF Markdown HTML]  AZ[Azure SDK control plane] --> CORR  DP[Selected data-plane adapters] --> CORR  LLM[Optional LLM explainer] --> REPORT
## Core Go packages
| Package | Purpose |
| cmd/foundry | Cobra command registration for extension and standalone binary. |
| internal/app | Application composition, dependency injection and phase orchestration. |
| internal/config | Unified config, profiles, environment resolution and low-level escape hatches. |
| internal/source | Project discovery, source loading and source identity. |
| internal/azureyaml | Lossless YAML AST, schema/version handling and references. |
| internal/bicep | Bicep CLI discovery, compile adapter, ARM normalisation and diagnostic mapping. |
| internal/graph | Typed dependency and resource graph. |
| internal/rules | Rule registry, selectors, execution, versioning and metadata. |
| internal/findings | Finding model, fingerprints, severity and confidence. |
| internal/baseline | Baseline creation and matching. |
| internal/suppress | Expiry-aware suppressions. |
| internal/adapters | PSRule, Azure Policy, Defender, Advisor, Checkov and custom adapters. |
| internal/azure | Azure credential, ARM inventory and preflight checks. |
| internal/runtime | Read-only data-plane and network probes. |
| internal/report | Console, JSON, SARIF, Markdown, HTML and evidence packs. |
| internal/scaffold | Optional known-good remediation templates and references. |
| pkg/sdk | Stable public interfaces for third-party rules and adapters. |

# 6. Unified Configuration Model
Foundry Doctor presents curated, stable settings and generates transient low-level configuration for integrated tools. The generated files are stored under a temporary working directory and are never committed unless the user explicitly requests export.
version: 1profile: foundry-prodinputs:  azureYaml: ./azure.yaml  infraPath: ./infra  environment: prodrules:  include: [must-have, nice-to-have]  exclude: []  packs: [foundry-core]validation:  bicep: required  azure: optional  runtime: disabledpolicy:  resourceScope: same-resource-group  allowedExternalScopes: []adoption:  baseline: .foundry-doctor/baseline.yaml  suppressions: .foundry-doctor/suppressions.yamloutputs:  formats: [console, sarif]  directory: .foundry-doctor/outadvanced:  psrule:    enabled: auto    config: null  checkov:    enabled: false    config: null  bicep:    executable: auto    config: null  adapters: []
Precedence: command-line flags override environment-specific configuration, which overrides repository configuration, which overrides curated profile defaults. Unknown settings fail validation. Generated adapter configuration is displayed with --debug and exportable with --export-tool-config.
# 7. Findings, Rules and Adoption Contracts
type Finding struct {  RuleID string  RuleVersion int  Severity Severity  Category Category  Pillar string  Basis []string  Profile string  Resource ResourceRef  Location Location  Evidence string  Recommendation string  Fix string  DocsURL string  LastVerified string  Confidence Confidence  Suppressed *Suppression  Baselined bool  Fingerprint string  Adapter string}
| Exit code | Contract |
| 0 | No unsuppressed findings at or above --fail-on threshold. |
| 1 | One or more findings meet or exceed the failure threshold. |
| 2 | The requested validation could not run because input, dependency, authentication or permissions were unavailable. |
| 3 | At least one check was skipped and --strict was specified. |
| 4 | Internal error or adapter protocol failure. |

# 8. Phase 0 - Research, overlap analysis and technical spikes
Purpose: establish the evidence-backed rule strategy and prove the risky integration points before implementation.
## Purpose and context
Phase 0 prevents duplication of lower-level tools and validates the architectural assumptions that most affect implementation. The output is an approved, sourced rule catalogue and a set of executable prototypes. No production CLI feature work starts until this gate is complete.
## Dependencies and phase hand-off
- No code dependency. Uses the agreed draft catalogue and existing XF rule/test assets as inputs.
- Produces the canonical rule IDs, ownership decisions, documentation links and version compatibility matrix consumed by every later phase.
- Produces Bicep/source-map and azd namespace spike results that unblock package design.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| PSRule for Azure | Map each SEC, NET, REL and OPS proposal to existing rules and WAF metadata. | Reuse or wrap where semantics and evidence are sufficient; do not copy implementation. |
| Azure Policy built-ins | Map deploy-time governance coverage and deny/audit effects. | Policy is deployed-state governance, not an offline replacement. |
| Defender for Cloud | Identify post-deployment security recommendations and plans. | Use as optional evidence source in later phases. |
| Azure Advisor | Identify reliability, cost and operational recommendations. | Optional deployed-state adapter. |
| Bicep CLI | Prove Bicep-to-ARM compilation and diagnostic/source mapping. | Required local dependency unless a compatible embedded route is proven. |
| azd and Foundry extensions | Confirm command namespace, schema versions, synthetic infrastructure behaviour and extension SDK contracts. | Do not assume preview field names. |
| Checkov/KICS/Terrascan | Compare generic IaC security coverage and structured output. | Optional adapters only unless a unique high-value control is found. |
| WARA/WAF guidance | Separate machine-verifiable rules from review questions. | Use sourced guidance, never claim complete compliance. |

## Functional scope
- Inventory the 108 proposed rules and 59 XF-origin checks.
- For every rule record: source, current Microsoft documentation URL, last verified date, existing-tool coverage, decision (reuse/wrap/adapt/native/drop), environment severities and testability.
- Discover beneficial new rules for developers and product development managers, including deployment feasibility, supportability, schema lifecycle, release readiness and cost visibility.
- Run Bicep source-map spike on root templates, modules, loops, conditions and generated infrastructure.
- Run azure.yaml-only spike for projects where azd synthesises infrastructure.
- Confirm public command namespace and packaging constraints.
- Document licences and redistribution conditions for every dependency.
## Command flow
Research agents -> source matrix -> rule overlap decision -> architecture decision records                         |                         +-> Bicep source-map spike                         +-> azure.yaml-only spike                         +-> namespace/package spike                         v                 approved rule-catalog v1
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/research | Temporary scripts that generate coverage matrices; not shipped. |
| docs/decisions | Architecture decision records for engine, adapters, namespace and dependencies. |
| rules/catalog | Machine-readable catalogue seeds. |
| test/spikes | Disposable integration fixtures and recorded results. |

## Required deliverables
- docs/overlap-analysis.md
- docs/rule-catalog.md with verified links
- docs/tool-compatibility.md
- docs/licence-inventory.md
- ADR-001 rule engine strategy
- ADR-002 Bicep analysis/source maps
- ADR-003 command namespace
- ADR-004 synthetic infrastructure handling
- Prototype fixtures and spike test results
## Testing strategy
- Unit tests for catalogue validation and duplicate IDs.
- Golden tests for overlap matrix generation.
- Spike tests using representative Bicep modules and azure.yaml-only projects.
- Licence and dependency audit in CI.
## Definition of Done
- Every proposed rule has a verified source or is explicitly labelled product opinion.
- Every rule has a reuse/wrap/adapt/native/drop decision.
- At least one real or representative Foundry project validates the azure.yaml-only path.
- Bicep source mapping is either proven for MVP or documented as a deferred limitation.
- No unresolved namespace conflict.
- The revised MVP contains 35-40 high-value rules.
## Key risks and mitigations
| Risk | Mitigation |
| Documentation changes during development | Store lastVerified and compatible version ranges in every rule pack. |
| Tool overlap is higher than expected | Reduce native implementation and strengthen adapters. |
| Source mapping cannot be made reliable | Ship SARIF only for azure.yaml and ARM-level locations; defer annotated Bicep copies. |
| Preview namespace/schema changes | Pin supported versions and fail with unsupported-version diagnostics. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| Research coordinator agent | Own source collection, citation quality and overlap matrix. |
| Foundry lead agent | Validate azure.yaml semantics and extension compatibility. |
| Azure black belt agent | Validate security, networking, WAF/WARA and deployed-state sources. |
| Go architect agent | Run SDK, namespace and packaging spikes. |
| Bicep specialist agent | Prove compile, normalisation and source-map strategy. |
| Independent reviewer agents | Two separate reviews: Foundry lead and Azure black belt; disagreements become ADR issues. |

# 9. Phase 1 - Offline Foundry rule platform MVP
Purpose: deliver a useful, adoptable doctor that runs without Azure login and catches structural, platform and high-confidence security errors.
## Purpose and context
This phase establishes the durable product core: project discovery, lossless YAML parsing, Bicep compilation, graph construction, rule execution, profiles, baselines, suppressions and reporting. It intentionally excludes live Azure correlation, runtime probes, HTML, graphs as user-facing output, cost and LLM summaries.
## Dependencies and phase hand-off
- Requires approved Phase 0 catalogue and ADRs.
- Consumes verified azure.yaml schema/version information and Bicep strategy.
- Produces stable finding, adapter and report contracts used by all later phases.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| azd extension SDK | Read project and selected environment context and expose command namespace. | Core logic remains library-independent so standalone CLI can share it. |
| Bicep CLI | Compile Bicep and emit diagnostics/ARM representation. | Doctor detects version; missing required dependency returns exit code 2. |
| Bicep linter | Use compiler diagnostics rather than duplicating language rules. | Normalise diagnostics into Finding records. |
| PSRule for Azure | Optional adapter and overlap verification, not a core dependency. | If installed/enabled, merge findings and de-duplicate by mapped rule. |
| GitHub Code Scanning | Consume SARIF produced by doctor. | No GitHub API required locally. |
| GitHub Actions | Run offline doctor during pull requests. | Use pinned actions and least-privilege permissions. |

## Functional scope
- Implement 35-40 Phase 1 rules selected from CFG, SEC, NET, IDN and ENV, prioritising platform constraints and must-have security checks.
- Support azure.yaml-only projects and Bicep-backed projects.
- Compile Bicep to ARM and normalise resources/outputs without implementing a custom parser.
- Resolve services.*.uses and variable producer/consumer relationships.
- Implement dev, test and prod severity profiles. Platform-basis rules remain errors in all profiles.
- Implement baseline generation/fingerprints, suppression reason/expiry and --min-severity/--fail-on.
- Implement explain, compare and console/JSON/Markdown/SARIF outputs.
- Report skipped checks explicitly; never convert skipped to pass.
## Command flow
project discovery  -> config/profile resolution  -> azure.yaml lossless parse + schema check  -> optional Bicep compile/lint -> ARM normalisation  -> dependency/correlation graph  -> native rules + optional adapters  -> finding fingerprints  -> baseline/suppression resolution  -> console | JSON | Markdown | SARIF  -> stable exit code
## Go packages and implementation responsibilities
| Package | Responsibility |
| cmd/foundry | doctor, explain and compare commands. |
| internal/project | Project and environment discovery. |
| internal/config | Profiles, precedence and escape hatch. |
| internal/azureyaml | AST, duplicate-key checks, references and source locations. |
| internal/bicep | CLI adapter, ARM normaliser and diagnostics. |
| internal/graph | Typed local dependency graph. |
| internal/rules | Registry, selectors and execution. |
| internal/findings | Model, fingerprints and redaction. |
| internal/baseline | Baseline read/write/match. |
| internal/suppress | Suppression schema, expiry and ownership. |
| internal/report | Console, JSON, Markdown and SARIF. |
| internal/adapters/psrule | Optional PSRule adapter. |

## Required deliverables
- Installable azd extension and standalone binary.
- Machine-readable rules/pack-v1 catalogue and generated human documentation.
- Curated dev/test/prod profiles.
- Baseline and suppression schemas and examples.
- GitHub Actions reusable workflow and starter workflow.
- Good, bad, azure.yaml-only and Bicep-backed sample repositories.
- Security threat model and dependency inventory.
## Testing strategy
- Table-driven unit tests per rule: one positive, one negative and edge cases.
- Golden tests for all reporters.
- Fuzz tests for YAML parsing and secret redaction.
- Fixture tests for Bicep compilation and ARM normalisation.
- CLI contract tests for flags and exit codes.
- GitHub Actions smoke test against sample projects.
- Minimum coverage target applies to core engine; rule tests require explicit scenario coverage rather than a single aggregate percentage.
## Definition of Done
- All selected 35-40 rules implemented, sourced and versioned.
- Offline doctor completes without Azure credentials.
- Baseline hides only matching old fingerprints; changed/new findings remain visible.
- Expired suppressions generate an error.
- Console, JSON, Markdown and SARIF outputs are deterministic.
- GitHub pull request workflow uploads SARIF and enforces exit codes.
- No secrets appear in findings, debug logs or test snapshots.
- Cross-platform binaries produced for supported operating systems.
## Key risks and mitigations
| Risk | Mitigation |
| Bicep source locations are incomplete | Return compiler diagnostics where available and confidence=likely for derived ARM matches. |
| Optional tools change output | Namespace adapter findings and ensure deterministic output for a pinned adapter version. |
| Rules conflict | Define precedence and de-duplication mappings in rule metadata. |
| Large finding count blocks adoption | Baselines, thresholds and owner summary are mandatory in MVP. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| Implementation coordinator | Own DAG, integration branches and acceptance checks. |
| Go core agent | Implement contracts and CLI. |
| YAML/Bicep agent | Implement parsing and normalisation. |
| Rule agents by domain | Implement CFG, SEC, NET, IDN and ENV independently. |
| Reporting/CI agent | Implement output contracts and GitHub Actions. |
| Foundry lead reviewer | Review platform semantics. |
| Azure black belt reviewer | Review security and networking semantics. |
| QA adversarial agent | Create malformed, ambiguous and security-sensitive fixtures. |

# 10. Phase 2 - Azure deployment preflight and control-plane correlation
Purpose: answer whether the selected environment is likely to deploy successfully before azd performs writes.
## Purpose and context
This phase adds authenticated, read-only Azure checks. It correlates the local deployment plan with subscription, resource-group, policy, quota, provider, role, naming, lock and subnet state. It does not promise certainty where Azure APIs do not expose a definitive pre-check.
## Dependencies and phase hand-off
- Requires Phase 1 stable finding/graph/config contracts.
- Requires Phase 0 verification for model availability, quota, policy and soft-delete APIs.
- Feeds later runtime, WAF and drift phases with deployed inventory.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| Azure SDK for Go | Primary typed access to ARM, Resource Graph and management APIs. | Use azd/Azure credential chain; no stored secrets. |
| Azure Resource Graph | Efficient read-only inventory and scope correlation. | Fallback to management clients where ARG lacks properties. |
| ARM What-If | Predict planned control-plane changes where a deployable template exists. | What-if findings retain uncertainty and do not claim policy evaluation. |
| Azure Policy | Read assignments/exemptions/compliance and detect likely deny conflicts. | Best effort; distinguish proven denial from potential conflict. |
| azd environment APIs | Resolve target tenant, subscription, location and resource group. | Never mutate environment values. |
| GitHub OIDC / azure/login | Authenticate preflight workflow without long-lived credentials. | Document required federated permissions and read scopes. |

## Functional scope
- Implement DEP-001 through DEP-012 after verification.
- Check tenant/subscription alignment, provider registration and effective deployer permissions.
- Check model/version/SKU regional availability and quota when APIs permit.
- Check soft-deleted/taken names, locks, subnet conflicts and regional setup support.
- Run what-if only with explicit --preflight/--what-if.
- Build same-resource-group policy with configurable approved external scopes.
- Generate a deployment readiness summary: ready, blocked, uncertain, skipped.
## Command flow
Phase 1 local plan  -> Azure authentication + context check  -> planned-resource inventory  -> provider / permission / quota / naming checks  -> Resource Graph and management-plane correlation  -> policy + lock + subnet analysis  -> optional ARM what-if  -> readiness findings + uncertainty  -> reports / CI exit code
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/azure/auth | Credential and context acquisition. |
| internal/azure/inventory | Resource Graph and ARM inventory. |
| internal/preflight | DEP rule orchestration. |
| internal/azure/permissions | Role and action feasibility. |
| internal/azure/models | Model/SKU/quota adapters. |
| internal/azure/policy | Policy and exemption evidence. |
| internal/azure/whatif | What-if execution and result mapping. |
| internal/azure/names | Name and soft-delete checks. |

## Required deliverables
- Preflight command and GitHub reusable workflow.
- Permission matrix documenting Reader and elevated read requirements.
- Recorded Azure API fixtures for deterministic tests.
- Deployment readiness report section.
- Sample OIDC configuration and least-privilege role guidance.
## Testing strategy
- Mock Azure SDK tests and recorded-response tests.
- Integration tests in disposable subscriptions for provider, quota, lock and policy cases.
- Permission-degradation tests using restricted identities.
- What-if fixture tests for create/modify/delete/replace outcomes.
- Concurrency/throttling tests for resource inventory.
## Definition of Done
- All DEP rules report pass/fail/skipped/uncertain correctly.
- No preflight check writes or changes Azure resources.
- Lack of permission identifies the exact skipped capability.
- What-if destructive changes are surfaced with resource IDs.
- Cross-resource-group references follow explicit scope policy.
- CI supports OIDC and no client secrets are required.
## Key risks and mitigations
| Risk | Mitigation |
| Azure APIs cannot fully predict success | Use confidence and uncertainty; never state guaranteed deployment. |
| Quota APIs vary | Provider adapters and skipped-with-reason. |
| Policy evaluation is incomplete | Label potential conflicts separately from observed denials. |
| Broad permissions requested | Default read-only and publish per-check permissions. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| Azure integration agent | Implement SDK clients and inventory. |
| Preflight rule agent | Implement DEP catalogue. |
| Identity specialist agent | Review role feasibility checks. |
| Azure black belt reviewer | Review policy, networking, quota and scope logic. |
| Security reviewer | Verify read-only behaviour and credential handling. |
| QA integration agent | Own replay fixtures and live test subscription scenarios. |

# 11. Phase 3 - Runtime diagnosis and selected data-plane probes
Purpose: diagnose why an already deployed agent or project dependency fails at runtime.
## Purpose and context
Runtime mode is explicit and read-only. Checks requiring a particular network vantage point or data-plane permission are skipped with precise instructions when unavailable. The initial scope is identity, capability host/project state, model deployment state, Foundry connections, Search, Storage and private endpoint DNS.
## Dependencies and phase hand-off
- Requires Phase 2 Azure authentication and inventory.
- Uses the same graph nodes to attach runtime evidence to planned and deployed resources.
- Creates reusable probe interface for later Foundry IQ and gateway work.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| Azure SDK for Go | Read management state, role assignments, monitor metrics and service metadata. | Read-only operations. |
| Foundry data-plane SDK/REST | Read project, model, connection and capability state. | Pin API versions and isolate preview clients. |
| Azure AI Search SDK/REST | Check index existence/schema/document count/indexer state. | Never retrieve document content. |
| Network Watcher | Optional connectivity evidence. | If no valid vantage point exists, report skipped. |
| Azure Monitor / Log Analytics | Check recent diagnostics and 429/error signals. | Queries must be bounded and redact identifiers. |
| GitHub Actions self-hosted runner | Optional VNet-based probe execution. | Not required for standard offline/preflight workflows. |

## Functional scope
- Implement RUN-001 through RUN-007; defer full drift to later phase.
- Resolve effective access for project identity on Search, Storage and Cosmos where applicable.
- Check capability host/project connections and model deployment provisioning state.
- Check Search index existence, vector-profile compatibility, non-zero document presence and indexer state.
- Check private DNS only when a valid network vantage point exists.
- Classify probes as safe local, Azure control-plane, data-plane or VNet-only.
## Command flow
validated correlation graph  -> runtime probe planner  -> capability/permission discovery  -> parallel read-only probes  -> evidence redaction  -> correlate failure to YAML/Bicep/deployed node  -> developer diagnosis + next action
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/runtime | Probe scheduler, timeouts and classifications. |
| internal/runtime/foundry | Project/capability/model/connection probes. |
| internal/runtime/search | Search index/schema/indexer probes. |
| internal/runtime/network | DNS/connectivity probes and vantage-point handling. |
| internal/runtime/monitor | Bounded metrics/log queries. |
| internal/runtime/rbac | Effective-access correlation. |

## Required deliverables
- Runtime command documentation and permissions table.
- Probe safety contract.
- Recorded data-plane fixtures.
- Troubleshooting report template.
- VNet probe deployment/runbook only if required by verified tests.
## Testing strategy
- Unit tests with fake clients.
- Recorded HTTP response tests with secret scrubbing.
- Integration tests in public and private sample environments.
- Timeout, throttling and partial-permission tests.
- Privacy test ensuring data content is never emitted.
## Definition of Done
- Every probe is read-only, time-bounded and redacts data.
- Unavailable vantage point is skipped, never passed.
- A common missing-RBAC case identifies identity, target and expected role family.
- Runtime output connects evidence back to source/deployed graph nodes.
- No document content or prompt/completion bodies are collected.
## Key risks and mitigations
| Risk | Mitigation |
| Private network testing is environment-specific | Make VNet probes optional and explicit. |
| Preview APIs change | Versioned provider adapters and compatibility gates. |
| Data-plane permissions expose data | Use metadata-only calls and deny content retrieval in interface. |
| Logs contain sensitive content | Select only required fields and redact before persistence. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| Runtime platform agent | Implement scheduler and probe contracts. |
| Foundry data-plane agent | Implement capability/project/model probes. |
| Search specialist agent | Implement safe Search probes. |
| Networking black belt agent | Review DNS and private endpoint logic. |
| Privacy/security reviewer | Audit data minimisation. |
| QA runtime agent | Execute controlled fault scenarios. |

# 12. Phase 4 - WAF assessment and evidence reporting
Purpose: transform deterministic findings into pillar-based architecture assessment outputs for three audiences.
## Purpose and context
This phase does not introduce a separate pass/fail engine. It maps verified rule metadata to WAF pillars and creates owner, developer and security evidence views. Controls that require business or operational context remain UNKNOWN or QUESTION rather than falsely passing.
## Dependencies and phase hand-off
- Requires stable rule catalogue, findings and deployed evidence from Phases 1-3.
- Consumes Phase 0 WAF/WARA mapping.
- Does not require LLM support.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| PSRule for Azure | Optional additional WAF-aligned IaC findings. | Map external IDs to canonical rules and retain provenance. |
| Microsoft WAF guidance | Source for pillar mapping and remediation text. | Rule metadata stores URL and verification date. |
| WARA tooling/guidance | Reliability questions and evidence concepts. | Not treated as a generic five-pillar engine. |
| Azure Advisor | Optional deployed recommendations. | Imported as advisory evidence, not native rule pass/fail. |
| GitHub Pages/artefacts | Publish Markdown/HTML reports if enabled. | No mandatory external hosting. |

## Functional scope
- Implement azd foundry assess waf.
- Produce one-page owner summary with decision, top five actions and unassessed scope.
- Produce developer detail with evidence, fix and location.
- Produce security evidence pack including passed, failed, suppressed, baselined and skipped controls.
- Support PASS, FAIL, WARNING, UNKNOWN, QUESTION and SKIPPED states.
- Separate costly recommendations from mandatory controls.
## Command flow
findings + rule metadata + evidence  -> pillar mapping  -> control aggregation  -> unknown/question injection  -> audience-specific rendering  -> owner summary | developer report | evidence pack
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/assess | Assessment aggregation. |
| internal/assess/waf | Pillar/control mappings. |
| internal/assess/wara | Reliability question mappings. |
| internal/report/owner | One-page summary. |
| internal/report/developer | Detailed remediation view. |
| internal/report/evidence | Security evidence pack. |
| internal/report/html | Optional static HTML renderer. |

## Required deliverables
- WAF mapping file for every applicable rule.
- Owner Markdown report.
- Developer Markdown and optional HTML report.
- Security evidence pack in Markdown/JSON.
- Sample reports for dev, test and prod fixtures.
## Testing strategy
- Golden report tests.
- Pillar mapping completeness tests.
- Snapshot tests for unknown/question behaviour.
- Accessibility audit for HTML if included.
- Reviewer test with owner, developer and security personas.
## Definition of Done
- No report claims complete WAF compliance.
- Unknown/non-machine-verifiable controls are visible.
- Every recommendation links to a verified source or is labelled project opinion.
- All three audiences receive materially different, useful views.
- Suppressed and baselined controls remain visible in evidence pack.
## Key risks and mitigations
| Risk | Mitigation |
| Assessment interpreted as certification | Use explicit scope and limitations on every report. |
| Rule counts create misleading scores | Prefer status and evidence; make scores optional or omit. |
| External findings duplicate native ones | Canonical mapping and de-duplication. |
| WAF guidance evolves | Verification dates and versioned mappings. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| Assessment agent | Implement aggregation. |
| Technical writer agent | Author concise deterministic remediation text. |
| WAF black belt reviewer | Validate pillar and control mappings. |
| Security evidence agent | Design auditable evidence pack. |
| Product manager persona | Review owner view for go/no-go clarity. |
| QA report agent | Own golden and accessibility tests. |

# 13. Phase 5 - Dependency and deployment graph outputs
Purpose: make complex Foundry relationships visible without changing validation semantics.
## Purpose and context
Graphs are generated from the correlation graph already created in Phases 1-3. This phase exposes source, deployed and combined views and highlights missing, external, baselined and unhealthy nodes.
## Dependencies and phase hand-off
- Requires stable graph node/edge contracts from Phase 1.
- Uses Azure resource IDs from Phase 2 and health status from Phase 3.
- Does not block core doctor execution if rendering tools are absent.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| Mermaid | Primary portable text graph output. | Generate syntax only; no runtime dependency for basic output. |
| Graphviz | Optional PNG/SVG rendering adapter. | Missing binary reports optional feature unavailable. |
| GitHub Markdown | Renders Mermaid in supported contexts. | Store generated .md artefacts. |
| Azure Resource Graph | Provides deployed topology nodes and IDs. | Read-only inventory from Phase 2. |

## Functional scope
- Implement source, deployed and combined graph modes.
- Represent project, agent, model, toolbox, connection, identity, role, network, Search, Storage, APIM and monitoring nodes.
- Encode external-scope dependencies and missing references visually.
- Implement environment-difference graph.
- Export Mermaid and JSON; HTML/PNG optional.
## Command flow
local graph + deployed inventory + runtime states  -> graph view selector  -> node/edge filtering  -> status/style mapping  -> Mermaid | JSON | optional SVG/PNG/HTML
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/graph/view | Projection and filters. |
| internal/graph/mermaid | Mermaid renderer. |
| internal/graph/json | Stable machine graph. |
| internal/graph/graphviz | Optional rendering adapter. |
| internal/report/graph | HTML wrapper and legends. |

## Required deliverables
- Graph command.
- Mermaid and JSON schemas.
- Sample diagrams for supported architectures.
- Legend and accessibility guidance.
## Testing strategy
- Golden graph tests.
- Cycle and disconnected-node tests.
- Large graph performance test.
- Secret-redaction test.
- Optional adapter absence test.
## Definition of Done
- A graph can be generated without Azure login from source.
- Combined view correctly distinguishes local, Azure and external nodes.
- Graphs do not expose secrets or connection credentials.
- Rendering order is deterministic for stable diffs.
## Key risks and mitigations
| Risk | Mitigation |
| Graphs become unreadable | Filtering, grouping and deterministic layout. |
| Generated diagrams expose sensitive IDs | Configurable ID redaction and friendly labels. |
| Optional renderer dependency | Mermaid text is always available. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| Graph engine agent | Implement projections. |
| Visualisation agent | Design readable styles and legends. |
| Foundry reviewer | Validate relationship semantics. |
| Security reviewer | Check data disclosure. |
| QA graph agent | Golden and scale tests. |

# 14. Phase 6 - Annotated review copies and source remediation workflow
Purpose: provide safe, line-specific guidance in copies of azure.yaml and Bicep without modifying deployable originals.
## Purpose and context
This phase is deliberately deferred until source locations are proven. It writes review copies under a separate directory, inserts idempotent comments and creates a manifest mapping comments to findings.
## Dependencies and phase hand-off
- Requires reliable YAML AST locations from Phase 1.
- Requires accepted Bicep source-map capability from Phase 0/1.
- Uses remediation snippets from rule metadata.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| Bicep CLI diagnostics/source maps | Locate source expressions where supported. | Fallback to ARM-level finding when exact source is unavailable. |
| SARIF regions | Share the same location model as annotations. | One canonical location service. |
| Git | Optional diff generation for review copies. | Never commit or modify automatically. |

## Functional scope
- Implement azd foundry annotate.
- Copy azure.yaml and infra tree to review directory.
- Insert comments with rule ID, category, severity and report reference.
- Make reruns idempotent by replacing only tool-owned comments.
- Generate annotations-manifest.json and optional diff.
- Never write to originals without a separate future explicit command.
## Command flow
findings with precise locations  -> copy source tree  -> remove prior doctor-owned comments  -> insert ordered comments  -> structural validation  -> compile copied Bicep / parse copied YAML  -> manifest + diff
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/annotate | Workflow and ownership markers. |
| internal/annotate/yaml | Lossless YAML comments. |
| internal/annotate/bicep | Safe comment insertion. |
| internal/location | Canonical source regions. |
| internal/diff | Review diff generation. |

## Required deliverables
- Review-copy command.
- Annotation manifest schema.
- Round-trip safety tests.
- Examples and warnings.
## Testing strategy
- Round-trip YAML comment tests.
- Bicep recompile tests for every annotation fixture.
- Idempotence tests.
- File permission/symlink safety tests.
- Large repository performance tests.
## Definition of Done
- Original files are byte-for-byte unchanged.
- Annotated YAML parses and annotated Bicep compiles.
- Rerunning produces no duplicate comments.
- Findings without reliable locations are placed only in report/manifest.
- Output names include .review to prevent accidental deployment.
## Key risks and mitigations
| Risk | Mitigation |
| Incorrect comments break source | Validate copies and omit uncertain insertion. |
| User mistakes review copy for deployment source | Use review naming, banner comments and manifest. |
| Generated modules lack direct source | Annotate nearest owning module only when confidence is certain. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| Source transformation agent | Implement copy and ownership model. |
| YAML specialist agent | Lossless comments. |
| Bicep specialist agent | Source-map/comment safety. |
| Security reviewer | Path and symlink safety. |
| QA mutation agent | Idempotence and malformed-source tests. |

# 15. Phase 7 - Cost intelligence
Purpose: give environment-level estimates for fixed-capacity resources while clearly excluding unknown consumption.
## Purpose and context
Cost remains informational. The estimator does not pretend that source configuration predicts token usage, traffic, discounts or negotiated prices. It produces a range and assumptions.
## Dependencies and phase hand-off
- Requires ARM resource plan from Phase 1 and Azure location/SKU normalisation from Phase 2.
- Uses profile/environment comparison to show cost drivers.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| Azure Retail Prices API | Public price input for supported fixed-capacity meters. | Cache with retrieval date; no promise of invoice accuracy. |
| Azure Advisor cost recommendations | Optional deployed advisory evidence. | Separate from planned monthly estimate. |
| Azure Cost Management | Potential future actual-cost adapter. | Out of initial scope unless explicitly enabled. |

## Functional scope
- Estimate Search units, APIM units, Cosmos provisioned RU/s, fixed hosting capacity and other verified meters.
- Exclude or clearly parameterise token-based model consumption.
- Show assumptions, region, currency, meter date and unsupported resources.
- Compare dev/test/prod fixed-capacity drivers.
## Command flow
compiled resource plan  -> supported meter mapping  -> price cache/API  -> quantity and monthly assumptions  -> low/high estimate + exclusions  -> owner/developer reports
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/cost | Estimate orchestration. |
| internal/cost/catalog | Resource-to-meter mappings. |
| internal/cost/prices | Retail Prices API and cache. |
| internal/cost/report | Assumptions and environment comparison. |

## Required deliverables
- Informational cost command and report section.
- Supported-meter catalogue.
- Price-cache schema.
- Explicit limitations statement.
## Testing strategy
- Recorded price API tests.
- Meter mapping unit tests.
- Rounding/currency tests.
- Unavailable/stale-price tests.
- Golden estimate report tests.
## Definition of Done
- No cost finding fails CI by default.
- Every estimate exposes assumptions and excluded costs.
- Stale or unavailable pricing is reported, not guessed.
- Currency and region handling are tested.
## Key risks and mitigations
| Risk | Mitigation |
| Estimate mistaken for bill | Prominent non-billing disclaimer and assumptions. |
| Pricing schema changes | Version parser and recorded contract tests. |
| Token cost uncertainty | Exclude unless user supplies explicit usage assumptions. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| Cost engine agent | Implement mappings and calculator. |
| FinOps reviewer | Validate limitations and report language. |
| Azure service agents | Verify meter mappings. |
| QA numeric agent | Test arithmetic and edge cases. |

# 16. Phase 8 - Foundry IQ, Search and APIM gateway deep validation
Purpose: extend the platform into high-value Foundry IQ retrieval and AI gateway semantics.
## Purpose and context
These rules are deferred because schemas and APIs evolve rapidly and because many checks require compound validation across YAML, ARM and data plane. This phase activates IQ and GW catalogue groups after Phase 0 re-verification.
## Dependencies and phase hand-off
- Requires generic runtime probe framework from Phase 3.
- Requires version-aware rule packs and schema compatibility gates.
- Requires verified gateway policy names and Search API behaviour.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| Azure AI Search APIs | Validate indexes, vector profiles, semantic configurations, indexers and knowledge objects. | Use metadata-only operations. |
| Foundry IQ APIs/tooling | Validate knowledge sources, knowledge bases and project integration. | Provider adapter isolates preview changes. |
| API Management policy model | Validate AI gateway policy intent and backend references. | Confirm policy names and schemas before implementation. |
| PSRule/Azure Policy | Reuse ARM-level Search/APIM controls where available. | Native rules focus on Foundry semantics. |

## Functional scope
- Implement IQ-001 through IQ-012 after verification.
- Implement GW-001 through GW-006 after verification.
- Validate filter fields, key/content/vector fields, dimensions, semantic mode, chunk overlap and source identity.
- Validate APIM JWT, managed identity, backend references, unique routes and telemetry sink.
- Add rule packs tied to compatible Foundry/Search/APIM versions.
## Command flow
version compatibility gate  -> YAML/ARM metadata extraction  -> optional data-plane metadata probes  -> compound IQ/GW rules  -> evidence + remediation  -> WAF and runtime reports
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/providers/iq | Foundry IQ versioned adapter. |
| internal/providers/search | Search schema normaliser. |
| internal/providers/apim | Policy/back-end normaliser. |
| internal/rules/iq | IQ checks. |
| internal/rules/gateway | Gateway checks. |

## Required deliverables
- Verified IQ/GW rule catalogue.
- Version compatibility matrix.
- Metadata-only integration fixtures.
- Foundry IQ and gateway sample projects.
## Testing strategy
- Schema contract tests by API version.
- Search index fixture tests.
- APIM policy XML fixture tests.
- Live metadata smoke tests.
- Preview-version compatibility tests.
## Definition of Done
- All field/property names verified against current sources.
- Unsupported versions fail safely.
- No content is retrieved or logged.
- Compound findings show evidence from each plane.
- All IQ/GW rules have positive and negative integration scenarios.
## Key risks and mitigations
| Risk | Mitigation |
| Rapid preview churn | Provider version adapters and lastVerified metadata. |
| Data exposure | Metadata-only interfaces. |
| Incorrect generic model dimension assumptions | Source model metadata/version rather than hard-coded claims where possible. |
| APIM policy variants | Normalise policy intent and report unknown constructs. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| IQ domain agent | Implement knowledge validation. |
| Search specialist agent | Review schema and retrieval semantics. |
| APIM gateway agent | Implement policy/back-end validation. |
| Foundry lead reviewer | Review preview compatibility. |
| Azure black belt reviewer | Review identity/network/security. |
| QA integration agent | Own compound fixtures. |

# 17. Phase 9 - Optional LLM explanations and executive narrative
Purpose: improve readability without allowing an LLM to invent findings or change enforcement.
## Purpose and context
LLM support is disabled by default and opt-in per run. The model receives only redacted structured findings and approved remediation snippets. Deterministic reports remain available and authoritative.
## Dependencies and phase hand-off
- Requires stable finding and report schemas from Phases 1 and 4.
- Requires privacy and redaction controls.
- Does not block any other phase.
## Microsoft and GitHub tool integrations
| Tool or project | Role in this phase | Integration boundary |
| Azure OpenAI / Foundry model endpoint | Optional narrative generation. | Use Entra authentication and explicit endpoint configuration. |
| GitHub Models or other providers | Possible future adapter. | Not part of core MVP. |
| Prompt templates in repository | Versioned, testable prompts. | Model cannot alter finding status or severity. |

## Functional scope
- Implement --llm-explain opt-in.
- Send only rule ID, approved explanation, redacted evidence, recommendation and audience.
- Generate owner narrative and grouped developer explanation.
- Add provenance marker, model/deployment metadata and fallback to deterministic text.
## Command flow
deterministic findings  -> redaction + allow-list projection  -> consent and endpoint check  -> versioned prompt  -> model response  -> safety/structure validation  -> clearly labelled narrative appendices
## Go packages and implementation responsibilities
| Package | Responsibility |
| internal/llm | Provider-neutral interface. |
| internal/llm/redact | Allow-list projection and redaction. |
| internal/llm/prompts | Versioned templates. |
| internal/llm/validate | Response schema and prohibited-claim checks. |

## Required deliverables
- Opt-in command flag and config.
- Threat model and data flow documentation.
- Prompt and response schemas.
- Deterministic fallback.
## Testing strategy
- Prompt snapshot tests.
- Redaction tests.
- Mock-provider tests.
- Malformed/hallucinated response rejection tests.
- No-network/default-off tests.
## Definition of Done
- Default execution makes no model call.
- No raw source files, secrets, prompts or document content are sent.
- LLM output cannot add/remove findings or alter severity.
- Reports label generated content and provider metadata.
## Key risks and mitigations
| Risk | Mitigation |
| Sensitive data sent to model | Strict allow-list projection and explicit opt-in. |
| Narrative contradicts findings | Schema validation and status immutability. |
| Provider dependency | Optional adapter and deterministic fallback. |

## Agent-swarm assignments
| Agent | Primary responsibility |
| LLM integration agent | Implement provider adapter. |
| Privacy reviewer | Approve data projection. |
| Prompt engineer agent | Create constrained templates. |
| Foundry lead reviewer | Validate explanations. |
| QA adversarial agent | Test contradiction and leakage attempts. |

# 18. GitHub Actions and Release Engineering
The repository must ship reusable workflows and starter workflows. Offline validation is safe for pull requests from forks. Azure preflight/runtime workflows require OIDC and protected environments.
| Workflow | Trigger | Capabilities |
| ci.yml | Pull request and push | Go lint/test, rule catalogue validation, sample doctor runs, SARIF generation. |
| doctor-offline.yml | Reusable workflow_call | Install extension/binary, run profile, upload SARIF and reports. |
| doctor-preflight.yml | Manual/protected branch | OIDC login, preflight, what-if and evidence artefacts. |
| runtime-diagnostics.yml | Manual dispatch | Explicit environment, optional self-hosted VNet runner. |
| release.yml | Version tag | Cross-platform binaries, checksums, SBOM, signatures, extension manifest and provenance. |
| dependency-review.yml | Pull request | Licence and supply-chain review. |

## Supply-chain requirements
- Pin GitHub Actions to immutable commit SHAs in release branches.
- Produce SBOM and provenance attestations.
- Sign binaries and checksums.
- Run dependency and licence scans.
- Do not bundle PowerShell/Python runtimes solely for optional adapters.
- Document minimum compatible versions for azd, Bicep and Foundry extensions.
# 19. Agent Swarm Execution Model for Claude Code
Claude Code must treat this PRD as the product specification and create a phase execution plan before modifying code. The swarm works as a directed acyclic graph with independent implementation and review branches. No reviewer persona may author the same component it approves.
orchestrator  -> research/source agents  -> architecture/contracts agent  -> parallel domain implementation agents  -> integration agent  -> QA/adversarial agent  -> Foundry lead review  -> Azure black belt review  -> remediation agent  -> final acceptance runner
| Agent | Persistent responsibility |
| Orchestrator | Break phase into work packets, enforce dependencies, merge and run acceptance gates. |
| Lead architect | Own architecture, contracts, dependency boundaries and ADRs. |
| Foundry lead engineer | Validate azd/Foundry schema, project semantics and compatibility. |
| Azure black belt | Validate security, identity, networking, WAF, WARA and Azure service integration. |
| Go principal engineer | Review idiomatic Go, interfaces, concurrency, performance and error handling. |
| Rule catalogue owner | Own IDs, metadata, overlap decisions, versions, sources and deprecation. |
| Bicep specialist | Own compile, ARM normalisation and source mapping. |
| DevOps/release agent | Own GitHub Actions, packaging, signing and extension manifests. |
| QA/adversarial agent | Own negative tests, fuzzing, fault injection and exit-code contracts. |
| Documentation agent | Generate user, maintainer and rule documentation from source metadata. |

## Claude Code phase prompt contract
For the selected phase:1. Read this PRD, all ADRs, current rule catalogue and prior phase hand-off.2. Inspect the repository before planning changes.3. Launch the prescribed specialist agents in parallel where dependencies permit.4. Produce a concrete implementation plan mapped to Definition of Done.5. Implement production code, tests, samples, docs and release artefacts.6. Run all local acceptance commands.7. Obtain independent reviews from:   a. Foundry lead architect/developer persona;   b. Azure black belt persona for integration, security, networking, WAF/WARA.8. Resolve findings or record an ADR with evidence.9. Produce phase hand-off: changed artefacts, commands run, test evidence, known limitations and next-phase prerequisites.10. Do not mark the phase complete unless every DoD item is met or explicitly waived in an ADR.
# 20. Agreed Rule Catalogue and Phase Allocation
The complete draft catalogue remains subject to Phase 0 verification. The following list is the agreed implementation scope and ordering. Rule titles are stable identifiers; exact property names, documentation links and adapter ownership must be finalised in Phase 0.
## CFG - configuration correctness
| Rule | Agreed check | Phase |
| FND-CFG-001 | Valid YAML, duplicate-key detection and compatible azure.ai.* schema | 1 |
| FND-CFG-002 | Unique service names by kind | 1 |
| FND-CFG-003 | All inter-service references resolve | 1 |
| FND-CFG-004 | Prompt model or hosted source/image is valid | 1 |
| FND-CFG-005 | No raw secrets in YAML/Bicep/environment blocks | 1 |
| FND-CFG-006 | Every variable has an environment or infrastructure producer | 1 |
| FND-CFG-007 | Outbound endpoints use HTTPS and reject unsafe local/metadata targets | 1 |
| FND-CFG-008 | MCP tools use allow lists | Later |
| FND-CFG-009 | Cron expressions are valid | Later |
| FND-CFG-010 | Model versions pinned in stricter profiles | Later |
| FND-CFG-011 | Preview/retired fields are version-aware | 1 |
| FND-CFG-012 | Azure region values are valid | 1 |

## SEC - security
| Rule | Agreed check | Phase |
| FND-SEC-001 | Foundry local authentication disabled | 1 |
| FND-SEC-002 | AI Search key authentication disabled or RBAC-only | 1 |
| FND-SEC-003 | Cosmos DB local authentication disabled | 1 |
| FND-SEC-004 | Storage shared key/anonymous access disabled; HTTPS/TLS required | 1 |
| FND-SEC-005 | Key Vault soft delete, purge protection and RBAC | Later |
| FND-SEC-006 | Firewall rules reject private/open ranges | 1 |
| FND-SEC-007 | RAI/content filter bound to chat deployments | Later |
| FND-SEC-008 | Relevant Defender plans enabled | 2/4 |
| FND-SEC-009 | Policy coverage and compliance evidence | 2/4 |
| FND-SEC-010 | Customer-managed keys informational | 4 |
| FND-SEC-011 | Controlled outbound egress informational | 4 |
| FND-SEC-012 | Gateway telemetry avoids raw prompt/completion logging | 8 |
| FND-SEC-013 | Gateway tenant validation is robust | 8 |
| FND-SEC-014 | Secrets not emitted as Bicep outputs | 1 |

## NET - networking
| Rule | Agreed check | Phase |
| FND-NET-001 | Foundry public access/private endpoint posture | 1 |
| FND-NET-002 | Dependent services public access/default-deny/private endpoints | 1 |
| FND-NET-003 | Private endpoint DNS zone groups and VNet links | 1 |
| FND-NET-004 | Agent subnet delegation and sizing | 1 |
| FND-NET-005 | Private-mode VNet/region consistency | 1 |
| FND-NET-006 | Private address space and overlap | 2 |
| FND-NET-007 | Restricted mode has allowed IP range | 1 |
| FND-NET-008 | Search SKU supports private endpoints | 1 |
| FND-NET-009 | APIM SKU supports required private backend connectivity | 8 |
| FND-NET-010 | Azure Monitor private-link choice is explicit | 4 |
| FND-NET-011 | NSG/DDoS recommendations | 4 |

## IDN - identity and access
| Rule | Agreed check | Phase |
| FND-IDN-001 | Managed identities used for Foundry and workloads | 1 |
| FND-IDN-002 | Project identity has required data roles | 1/2 |
| FND-IDN-003 | Role assignments use object IDs and principalType | 1 |
| FND-IDN-004 | No broad Owner/Contributor for application identities | 1 |
| FND-IDN-005 | Standing human admin access reviewed | 2/4 |
| FND-IDN-006 | Federated credentials use HTTPS issuer and specific subject | 1 |

## REL - reliability
| Rule | Agreed check | Phase |
| FND-REL-001 | Search replica/SLA posture | 4 |
| FND-REL-002 | Storage redundancy posture | 4 |
| FND-REL-003 | Cosmos zone redundancy and continuous backup | 4 |
| FND-REL-004 | Delete locks on stateful services | 4 |
| FND-REL-005 | Search sizing within platform limits | 1 |
| FND-REL-006 | Gateway retry/circuit breaker | 8 |
| FND-REL-007 | Multi-region/DR plan question | 4 |
| FND-REL-008 | Provisioned throughput spillover recommendation | 4 |
| FND-REL-009 | APIM availability tier recommendation | 4/8 |

## OPS - operations and governance
| Rule | Agreed check | Phase |
| FND-OPS-001 | Diagnostic settings to Log Analytics | 4 |
| FND-OPS-002 | Application Insights connected | 4 |
| FND-OPS-003 | Baseline alerts exist | 4 |
| FND-OPS-004 | Required tags exist | 1 |
| FND-OPS-005 | Resource-name provider constraints | 1 |
| FND-OPS-006 | Log retention meets policy | 4 |
| FND-OPS-007 | CI/CD pipeline exists | 1 |
| FND-OPS-008 | Evaluations run before promotion | 4 |
| FND-OPS-009 | Data-residency regions and deployment SKU | 1/2 |
| FND-OPS-010 | Model allow/deny lists remain consistent | 1/8 |

## COST - cost
| Rule | Agreed check | Phase |
| FND-COST-001 | Budget and alerts exist | 2/7 |
| FND-COST-002 | Dev avoids production-sized fixed SKUs | 1 |
| FND-COST-003 | Gateway token limits exist | 8 |
| FND-COST-004 | Informational monthly fixed-capacity estimate | 7 |

## IQ - retrieval and knowledge
| Rule | Agreed check | Phase |
| FND-IQ-001 | Filter fields/claims reference filterable fields | 8 |
| FND-IQ-002 | Required key/content/title/vector/semantic fields | 8 |
| FND-IQ-003 | Vector field type, dimensions and profile | 8 |
| FND-IQ-004 | Vector dimensions match embedding deployment metadata | 8 |
| FND-IQ-005 | Retrieval mode supported by index and service tier | 8 |
| FND-IQ-006 | Chunk overlap and hybrid weights valid | 8 |
| FND-IQ-007 | Knowledge source type/API and planning model supported | 8 |
| FND-IQ-008 | Source connection uses managed identity | 8 |
| FND-IQ-009 | Document-level access control configured when required | 8 |
| FND-IQ-010 | ADLS Gen2 hierarchical namespace enabled | 8 |
| FND-IQ-011 | Refresh schedule valid and supportable | 8 |
| FND-IQ-012 | Semantic ranking enabled when used | 8 |

## GW - API Management gateway
| Rule | Agreed check | Phase |
| FND-GW-001 | JWT validates issuer, audience and tenant | 8 |
| FND-GW-002 | Verified token-limit/metric policies are present | 8 |
| FND-GW-003 | Routes resolve to existing backends | 8 |
| FND-GW-004 | Route paths unique | 8 |
| FND-GW-005 | Gateway calls Foundry using managed identity | 8 |
| FND-GW-006 | Telemetry sink exists for token tracking | 8 |

## DEP - deployment preflight
| Rule | Agreed check | Phase |
| FND-DEP-001 | Signed into matching tenant/subscription | 2 |
| FND-DEP-002 | Required providers registered | 2 |
| FND-DEP-003 | Deployer can create resources and role assignments | 2 |
| FND-DEP-004 | Model/version/SKU offered in target region | 2 |
| FND-DEP-005 | Quota covers requested capacity | 2 |
| FND-DEP-006 | No blocking soft-deleted resource/name | 2 |
| FND-DEP-007 | Globally unique names available | 2 |
| FND-DEP-008 | What-if has no unexpected delete/replace | 2 |
| FND-DEP-009 | Likely policy denials identified | 2 |
| FND-DEP-010 | Locks do not block change | 2 |
| FND-DEP-011 | Subnet delegation/capacity/use is compatible | 2 |
| FND-DEP-012 | Target region supports selected setup | 2 |

## RUN - runtime diagnosis
| Rule | Agreed check | Phase |
| FND-RUN-001 | Effective project identity access | 2/3 |
| FND-RUN-002 | Private DNS resolves from an appropriate vantage point | 3 |
| FND-RUN-003 | Capability host and project connections healthy | 3 |
| FND-RUN-004 | Model deployment state and throttling signal | 3 |
| FND-RUN-005 | Agent tools/connections respond | 3 |
| FND-RUN-006 | Search index/documents/vector/indexer health | 3 |
| FND-RUN-007 | Diagnostic logs are arriving | 3 |
| FND-RUN-008 | Deployed properties versus compiled template drift | Future |

## ENV - environment comparison
| Rule | Agreed check | Phase |
| FND-ENV-001 | Environments do not unintentionally share resource groups | 1 |
| FND-ENV-002 | Environment-bound names/IDs/rules were not copied unchanged | 1 |
| FND-ENV-003 | Prod does not reference non-prod resource IDs | 1 |
| FND-ENV-004 | Informational difference summary | 1 |

# 21. Phase Dependency Map
Phase 0 Research and spikes   |   +--> Phase 1 Offline rule platform MVP          |          +--> Phase 2 Azure preflight/control plane          |      |          |      +--> Phase 3 Runtime/data plane          |      |      |          |      |      +--> Phase 8 IQ and gateway deep validation          |      |          |      +--> Phase 7 Cost intelligence          |          +--> Phase 4 WAF and evidence reporting          |      |          |      +--> Phase 9 Optional LLM narrative          |          +--> Phase 5 Graph outputs          |          +--> Phase 6 Annotated review copies
# 22. Global Definition of Done
- All phase-specific DoD items pass.
- All rule facts used by implementation have a verified source and lastVerified date.
- Two independent expert reviews completed: Foundry lead and Azure black belt.
- No unresolved critical/high security findings in the implementation.
- All commands have help text, examples, stable exit codes and non-interactive mode.
- Offline commands require no Azure login. Authenticated commands are read-only unless a separately approved feature says otherwise.
- No secrets or sensitive data appear in outputs, telemetry, fixtures or logs.
- Cross-platform release artefacts, checksums, SBOM, signatures and provenance are produced.
- Sample projects cover azure.yaml-only, Bicep-backed, public, private and existing-resource patterns as appropriate to the phase.
- Phase hand-off document records test commands, evidence, limitations and next-phase prerequisites.
# 23. Key Risks Register
| Risk | Likelihood | Impact | Owner/Mitigation |
| Preview Foundry schemas change | High | High | Versioned provider adapters and rule packs; compatibility gate. |
| Generic tools overlap native rules | High | Medium | Phase 0 overlap decision; canonical mapping and adapters. |
| Bicep source mapping is incomplete | Medium | High | Compile/normalise first; confidence model; defer unsafe annotation. |
| Azure preflight cannot guarantee deployment | High | Medium | Use likely/uncertain/skipped and evidence, not guarantees. |
| Data-plane probes expose data | Low | High | Metadata-only interfaces, redaction and privacy review. |
| Tool installation contradicts simple UX | Medium | Medium | Core native Go rules; optional adapter detection and clear capabilities. |
| Existing projects have excessive findings | High | High | Baselines, expiring suppressions, thresholds and owner summary. |
| Rule catalogue becomes stale | High | High | lastVerified, compatibility CI, deprecation process and source owners. |
| LLM invents or leaks information | Medium | High | Default off, deterministic status immutability and allow-list payload. |
| Command namespace collision | Medium | High | Phase 0 namespace spike and fallback namespace. |

# 24. Microsoft and GitHub Integration Reference
| Integration | Default status | Purpose | Project-owned abstraction |
| azd extension SDK | Required | Environment/project context and command integration | AzdContext interface |
| Bicep CLI/linter | Required for Bicep projects | Compile and language diagnostics | BicepCompiler interface |
| Azure SDK for Go | Phase 2+ | Control-plane inventory and checks | AzureInventory interfaces |
| ARM What-If | Optional explicit | Planned change evidence | WhatIfProvider |
| PSRule for Azure | Optional adapter | Generic Azure/WAF IaC coverage | ExternalRuleAdapter |
| Azure Policy | Phase 2 optional/curated | Scope governance evidence | PolicyProvider |
| Defender for Cloud | Phase 4 optional | Deployed security recommendation evidence | RecommendationProvider |
| Azure Advisor | Phase 4/7 optional | Operational/reliability/cost recommendations | RecommendationProvider |
| Checkov/KICS/Terrascan | Optional advanced adapters | Additional generic IaC coverage | ExternalRuleAdapter |
| GitHub Actions | Supported distribution | CI, OIDC preflight and release | Reusable workflows |
| GitHub code scanning | Supported output | SARIF annotations | SarifReporter |
| Mermaid | Built-in text output | Portable dependency diagrams | GraphRenderer |
| Graphviz | Optional | Image rendering | GraphRenderer |
| Azure Retail Prices API | Phase 7 | Informational fixed-capacity estimates | PriceProvider |
| Azure OpenAI/Foundry model | Phase 9 opt-in | Narrative explanation only | LLMProvider |

# 25. References Used in This Requirements Baseline
- Microsoft Learn: Azure Developer CLI reference: https://learn.microsoft.com/en-us/azure/developer/azure-developer-cli/reference
- Microsoft Learn: Azure Developer CLI extension framework: https://learn.microsoft.com/en-us/azure/developer/azure-developer-cli/extension-framework
- Microsoft Learn: Foundry azure.yaml reference: https://learn.microsoft.com/en-us/azure/foundry/agents/concepts/azure-yaml-reference
- PSRule for Azure: Using Bicep source: https://azure.github.io/PSRule.Rules.Azure/using-bicep/
- PSRule for Azure: GitHub Actions quickstart: https://azure.github.io/PSRule.Rules.Azure/quickstarts/test-bicep-with-github/
- Microsoft Learn: Deploy Bicep with GitHub Actions: https://learn.microsoft.com/en-us/azure/azure-resource-manager/bicep/deploy-github-actions
- Microsoft identity blog: Securing Azure deployments with PSRule: https://devblogs.microsoft.com/identity/secure-azure-deployments-with-psrule/
Phase 0 must replace or augment this baseline with per-rule verified sources. This list supports the architecture only; it is not sufficient evidence for all rule property names or product constraints.
# Appendix A - Suggested Repository Layout
foundry-doctor/  cmd/foundry-doctor/  internal/    app/ azureyaml/ bicep/ config/ graph/ rules/ findings/    baseline/ suppress/ adapters/ azure/ preflight/ runtime/    assess/ report/ annotate/ cost/ llm/  pkg/sdk/  rules/    catalog/ packs/ profiles/ mappings/  schemas/  docs/    decisions/ rule-catalog.md overlap-analysis.md development/  samples/    yaml-only/ bicep-backed/ invalid/ secure/ private/  test/    fixtures/ golden/ integration/ e2e/  .github/    workflows/ actions/  CLAUDE.md  AGENTS.md  CONTRIBUTING.md  SECURITY.md  LICENSE
# Appendix B - Per-Phase Hand-off Template
# Phase hand-off## Delivered## Changed contracts## Rule catalogue changes## Commands implemented## Tests executed and results## Security and privacy review## Foundry lead review## Azure black belt review## Known limitations## Deferred issues with rationale## Compatibility matrix## Artefacts for next phase## Exact next-phase prerequisites