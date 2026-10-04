# Roadmap notes by phase

Decisions and ideas recorded during Phase 1 so they are picked up in the right phase. Nothing
here is implemented yet, except where a section says so. Scope for each item is a starting point
to revisit when the phase begins.

## Scope rule: native, or a comment from `doctor`

A feature is **native** when the extension validates it and generates the infrastructure for it.
Anything else is a **comment**: the `doctor` command (Phase 6) writes a one-line description,
pseudo-Bicep or a link into `Azure-WAF-recommended.yaml`, and the user adds the infrastructure
themselves outside x-foundry. `promote` reuses the same comments for the target environment.

### Well-Architected (WAF) features

See `waf-profiles.md` for the current recommendations.

| Group | Examples | Handling |
| --- | --- | --- |
| Low | Zone redundancy, Cosmos continuous backup, ZRS storage, delete locks, Key Vault purge protection, Defender plans, Azure Policy assignments, budgets, alerts, diagnostic settings | Native |
| Medium | Customer-managed keys, Azure Monitor private link scope, per-resource diagnostic extras | Comment |
| High | Azure Firewall, UDR and forced tunnelling, NSGs, MCP subnet, DDoS protection, Application Gateway or Front Door with WAF, provisioned deployments with spillover, multi-region failover and DR, per-environment subscription isolation | Comment |

### Other features moved to comments or removed

Decided after the effort and risk review. The first five are removed from the native schema.

| Feature | Decision | What `doctor` comments say |
| --- | --- | --- |
| Service Bus events (`events`) | Removed from native scope | Not a core Foundry feature; add your own Service Bus Bicep if needed |
| Azure Cache for Redis / Managed Redis (`redis`) | Removed from native scope | Add your own Azure Managed Redis Bicep; Azure Cache for Redis can no longer be created |
| Hosted-agent runtime container app (`runtime`) | Removed from native scope | Hosted agents are declared in azd's own `services:` section (see below) |
| APIM chargeback (multi-dimension) and the quota reporting pipeline | Removed from native scope | Links to APIM AI gateway samples that do chargeback (see below) |
| Evaluation pipelines | Dataset and evaluator definitions stay native; running them does not | Run evaluations from your own CI; CI/CD recommendations as comments |
| Pipeline scaffolding | Not generated | Comments recommending a CI/CD setup per environment |
| Non-ARM parts of drift detection (agents, indexes) | Report only, never rewrite the YAML | See Phase 7 |

**Gateway (stays native):** JWT authentication, endpoints and a simple token limit.

**Hosted agents.** azd already covers these. In `azure.yaml`, a hosted agent is a service with
`host: azure.ai.agent` (built from a Docker image) that the `azure.ai.agents` azd extension
deploys, so x-foundry does not repeat it. The `doctor` comment points users there. Verify the exact
service fields against the current azd documentation when implementing:

- https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference
- https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml
- https://learn.microsoft.com/azure/developer/azure-developer-cli/extensions/azure-ai-foundry-extension

**APIM chargeback links.** The `doctor` comment for the gateway lists APIM guidance on token
tracking and chargeback: the `llm-emit-token-metric` and `llm-token-limit` policies, and the
AI-Gateway samples repository (`github.com/Azure-Samples/AI-Gateway`). The URLs were recalled, not
fetched, because learn.microsoft.com is not reachable from the development sandbox. Confirm each
link resolves, and pick the specific chargeback samples, before they are written into the code.

### Pending Phase 1 clean-up for these removals

The decisions above are not yet applied to the Phase 1 code. Doing so means removing the `events`,
`redis` and `runtime` sections (and the multi-dimension `chargeback` settings) from the schema, and
removing or revising the rules and tests that refer to them. A first look at
`docs/validation-rules.md` shows these are affected: XF014 and XF112 (runtime targets and hosted
agent sources), XF019 and XF106 and XF125 (Redis), XF108 and XF132 (runtime), XF111 (chargeback),
and the events rules. The examples and the published schemas also change. Do this as one change
before Phase 2 starts, so the Bicep generator does not build on fields that are going away.

## Phase 2: Bicep generator

- Generate the low-effort WAF resources: resource locks, zone-redundant and ZRS settings, Cosmos
  continuous backup, Key Vault purge protection, diagnostic settings to Log Analytics.
- Name generation uses `internal/azurenames`.
- Pseudo-Bicep for the medium and high groups is written here as reference material for `doctor`
  (Phase 6). It is unverified until it can be tested against Azure, and the comments must say so.
- Tag every resource the extension creates with `x-foundry-env` (the environment) and
  `x-foundry-id` (the graph node id). Drift detection (Phase 7) uses these to map Azure resources
  to YAML entries exactly. This must be in place from the first generated template, because it
  cannot be added to resources already deployed without redeploying them.

## Phase 3: provisioning and state

- Provisioning engine for Foundry data-plane items: agents, toolboxes, MCPs and evaluation
  datasets and evaluators (definitions only; the user runs evaluations from their own CI).
- State tracking, and validation rule XF025 (needs deployed state).

## Phase 4: Foundry IQ and Search

No change from the original plan.

## Phase 5: gateway and governance

- Gateway: JWT authentication, endpoints and a simple token limit.
- Generate the governance-related low-effort items: Azure Policy assignments, Defender plans,
  budgets and alerts.
- Chargeback is not built here; see the comments decision above.

## Phase 6: azd integration and developer experience

### `xfoundry doctor`

- Runs the `prod` profile against `azure.yaml` and writes `Azure-WAF-recommended.yaml`. The
  original file is not changed.
- Uses `yaml.Node` from `go.yaml.in/yaml/v3` so key order and the user's comments are preserved.
- For each `XF3xx` warning:
  - If the key exists, the comment goes above it.
  - If the key is missing, the comment goes above the nearest existing parent, with the suggested
    YAML commented out.
- For gaps with no schema field (the medium and high groups, and the features in the table
  above), the comment is a one-line description, pseudo-Bicep or a link the user can follow.
- Also adds comments recommending a CI/CD pipeline per environment (pipeline scaffolding is not
  generated).
- Comments are prefixed (for example `# WAF[Reliability] XF303:`) so they are distinguishable from
  user comments and the command is idempotent.
- Tests: golden files per example, and a check that the annotated copy still parses and validates.
- Needs only Phase 1 output, so it can be pulled forward if wanted.

### Suggest test and prod after dev

After a successful dev deployment, the extension suggests creating `test` and then `prod`
environments so the SDLC follows the Well-Architected golden path. See `xfoundry promote` below.

### `xfoundry promote` (environment migration)

Creates the YAML for the next environment from an existing one. This works on YAML only and
does not need deployed state. It is not interactive.

- `xfoundry promote --from dev --to test`, and `--from test --to prod`. Writes a new file; the
  source file is never modified.
- The new file is a plain copy with `defaults.environment` set, plus the target profile's
  recommendations as comments (reusing `doctor`).
- Settings tied to one deployment are left out of the copy, or commented out with a note:
  names and `namingPrefix`, region, existing resource IDs (VNet, subnets, Search, Storage), IP
  rules, secret references and budgets. Everything else is copied as is.
- Interactive selection of what to carry over is deferred until after the first release.
- Decision: each environment has its own file (for example `azure.dev.yaml`, `azure.test.yaml`,
  `azure.prod.yaml`). There is no `environments` overlay in the schema, so no schema change or
  merge rule is needed. The cost is that files can drift apart after promotion; Phase 6 should
  decide how azd selects the file per environment and whether a `diff` between environment
  files is worth adding.

## Phase 7 (optional, after the first release): drift detection

### `xfoundry drift` (Azure back to YAML)

Compares what is deployed in Azure with an environment's YAML file and proposes updates. Drift
between the YAML and Azure is separate from drift between environment files. Nothing else in
Phases 1–6 depends on this phase.

- Scope: a resource group, a tag set (azd's `azd-env-name` is a natural default), or both. Lists
  resources with Azure Resource Graph or ARM list calls, then reads each resource's properties.
  Reader access is enough.
- Reverse-maps each modelled resource type to `x-foundry` values, then classifies each finding:
  - Matches the YAML: nothing to do.
  - A native property differs: propose the Azure value for the YAML.
  - A modelled resource exists in Azure but not in the YAML: propose adding it, or referencing it
    as an existing resource.
  - An Azure resource has no schema equivalent (Azure Firewall, extra storage accounts, Front
    Door, unmanaged VMs): add a comment such as
    `# Non-native dependency in Azure: Microsoft.Network/azureFirewalls/fw-hub`.
  - Declared in the YAML but missing in Azure: report as not deployed or deleted.
- Output: a drift report and an updated copy of the YAML. The file is never edited in place by
  default. `--apply` writes to the file after the user confirms each change.
- Matching uses the `x-foundry-id` tags from Phase 2. Resources without them (created earlier or
  by other tools) fall back to matching by type and name; anything ambiguous goes to the
  non-native comments.
- Values equal to what the normaliser would derive are not written to the YAML as explicit
  settings, so the file does not fill with noise. Reuse the normaliser to tell them apart.
- Secrets are never read or written; references only.
- Non-ARM content (agents, toolboxes, knowledge bases, indexes, evaluation datasets) is reported
  only. The command lists differences but does not rewrite the YAML for them.
- Optional extension: let `promote` take its source from what is deployed, using this reverse
  mapping, rather than from a possibly stale YAML file.
- Testing: recorded API fixtures for unit tests, and a test subscription for real validation.
  This cannot be exercised in the development sandbox, which has no Azure access.
