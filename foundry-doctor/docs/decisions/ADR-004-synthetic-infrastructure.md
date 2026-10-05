# ADR-004: Synthetic infrastructure (azure.yaml-only Foundry projects)

- Status: Accepted
- lastVerified: 2026-10-04
- Evidence base: `Azure/azure-dev` @ `afe4b2b4d262bab4c11f4937e7a7942557ab0ecd`; extensions `azure.ai.projects` 1.0.0-beta.13 (`version.txt`), `azure.ai.agents` 1.0.0-beta.18. Fixtures: `test/spikes/azd/`.

## Context

Foundry azd projects can have no `infra/` folder. Foundry Doctor's Bicep adapter (ADR-002) then has nothing to compile. We need to know what azd does in that case, what is still readable offline, and how to fail safely on versions we do not know.

## Findings

### How azd provisions with no infra folder

1. The provisioning provider is selected by `infra.provider` in azure.yaml. Built-ins are `bicep` and `terraform`; "Extensions may register additional named providers (for example, 'microsoft.foundry')" (`schemas/v1.0/azure.yaml.json`, `properties.infra.properties.provider`, pattern `^[a-z0-9.]+$`). Extension capability: `provisioning-provider`, which lists "Provisioning without an on-disk `infra/` directory (templates synthesized from `azure.yaml`)" as an example (`cli/azd/docs/extensions/extension-framework.md`, "Provisioning Providers").
2. `azure.ai.projects` owns `infra.provider: microsoft.foundry` and `host: azure.ai.project` (`extensions/azure.ai.projects/README.md`, "azure.yaml ownership"). When `endpoint` is omitted, `azd provision` creates a Foundry account and project; when set, it reuses that project.
3. Synthesis (`extensions/azure.ai.projects/internal/synthesis/synthesizer.go`): it reads only `services:` from the raw azure.yaml, picks the project service, and derives Bicep parameters (`deployments`, `includeAcr`, network parameters). It embeds a fixed template (`internal/synthesis/templates/main.bicep`, `main.arm.json`, `modules/`; existing-project variants `existing-project.*`). The user never sees these templates unless they eject (`azd ai project add --infra`, README "Eject existing-project infrastructure"). `endpoint:` makes `Synthesize` return `ErrEndpointBrownfield` and the provider switches to the existing-project template (`foundry_provisioning_provider.go`, ~lines 317-410).
4. On-disk wins: if `<infra path>/<module>.bicep` or `.bicepparam` exists the provider uses it instead (`onDiskTemplatePresent`, `foundry_provisioning_provider.go`). So "azure.yaml-only" is detected the same way: neither file exists under the configured infra path (default `infra`, module `main`, `schemas/v1.0/azure.yaml.json` `infra.path`/`infra.module`; layers via `infra.layers`).
5. Accepted provisioning hosts: `azure.ai.project`, plus legacy `azure.ai.agent` and `microsoft.foundry` when no project service exists (`internal/provisioning/provisioning_provider.go`, `FoundryProjectServiceHosts`, `FoundryLegacyProvisioningHosts`).
6. Synthesis ignores Connection and Toolbox payloads: "Project synthesis does not read Connection or Toolbox payloads, environments, or credentials" (azure.ai.projects README). Those are reconciled at deploy time by their own extensions.

### What Foundry Doctor can read offline

| Source | Readable | Evidence |
|---|---|---|
| `azure.yaml` (raw bytes, lossless) | yes: `name`, `metadata`, `infra`, `requiredVersions`, `services.*` (`host`, `project`, `uses`, `env`, `kind`, agent/project/connection/toolbox/skill/routine/eval bodies), `resources` | `schemas/v1.0/azure.yaml.json` |
| `$ref` files referenced from services | yes (file includes resolved against project root) | `synthesizer.go` `resolveServiceRefs`, `Input.ProjectRoot`; `schemas/examples/complex.azure.yaml` |
| `.azure/<env>/.env` values | yes (key=value file) | `azd-docs/.../environment-variables-faq.md` ("`.env` file in the `.azure/<environment name>` directory") |
| `${VAR}` resolution | yes, offline: env map first, then process env; unresolved without default is an error. The expander is drone/envsubst with the full shell parameter grammar (`${VAR:=x}`, `${VAR:+x}`, `${VAR:?}`, `${VAR#p}` and so on), `$${VAR}` is an escaped literal, `${{ ... }}` spans are reserved for Foundry expressions and masked before expansion, and a reference nested inside a `:-` default is refused rather than reported | `synthesizer.go` `resolveVars`; `synthesis/envrefs.go` `FindEnvReferences`, `ValidateEnvReferences` (read 2026-10-04) |
| Synthesised parameters (deployments, includeAcr, network mode) | reimplementable from azure.yaml; do **not** import the extension's internal package | `synthesizer.go` `Synthesize` |
| Embedded template content | present in the extension source (`internal/synthesis/templates/main.bicep`, `modules/`, `main.arm.json`, `existing-project.*`) and fixed per extension version | read access confirmed; the resources each version creates have not been enumerated |

### What it cannot read in that case

- No Bicep source, so no source-map lines for ARM resources, no user-authored resource properties, no module graph. The ARM template is generated in memory by the extension at provision time and is not on disk (`foundry_provisioning_provider.go`: "armTemplate ... embedded ARM JSON; nil when on-disk Bicep is configured").
- Therefore rules that need ARM resource properties (network ACLs, RBAC assignments, diagnostic settings, SKU, API versions) cannot run in `--local`. They must report **skipped** (reason: no on-disk IaC; synthetic infrastructure is provider-defined) and not pass. A later phase may compile the embedded `main.bicep` (or read `main.arm.json`) for the pinned extension version, which would let many SEC and NET rules run on the default Foundry path instead of being skipped; the files exist in the extension source, so this is feasible to assess and is a Phase 1 spike item. `--preflight`/`--runtime` evidence is the other route.
- Values that exist only after provisioning (endpoints, project ID) appear in `.azure/<env>/.env` only if the user has provisioned; `AZURE_AI_PROJECT_ID` is the documented key for an existing project (azure.ai.projects README).

### azure.yaml facts for Foundry

- Schema location: `schemas/v1.0/azure.yaml.json` (`$id https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json`) and `schemas/alpha/azure.yaml.json` (adds `layers`, `deploymentStacksConfig`). Referenced by editors with a `# yaml-language-server: $schema=...` comment. Required top-level field: `name`; `additionalProperties: true` at root and service level.
- There is **no schema/format version field** in azure.yaml. The only version-like fields are `requiredVersions.azd` (semver range) and `requiredVersions.extensions` (map of extension id to constraint; "If the version of azd is outside this range, the project will fail to load") and `services.*.apiVersion` (not Foundry-specific). The schema channel (`v1.0` vs `alpha`) is implied by the `$schema` comment only, and `alpha` features are gated by `azd config` alpha flags (`cli/azd/docs/alpha-features.md`; unverified details).
- `services.<key>.host` is required. Documented values: `appservice`, `containerapp`, `function`, `springapp`, `staticwebapp`, `aks`, `ai.endpoint`, `azure.ai.agent`, `microsoft.foundry` (legacy compatibility), `azure.ai.project`, `azure.ai.connection`, `azure.ai.toolbox`, `azure.ai.skill`, `azure.ai.routine`, `azure.ai.eval` (`schemas/v1.0/azure.yaml.json`, `services...host.examples`). The list is `examples`, not an enum, so unknown hosts are schema-valid.
- Host bodies are composed from per-extension schemas: `azure.ai.agent` requires `project` (`extensions/azure.ai.agents/schemas/azure.ai.agent.json`; `kind` enum `hosted|prompt|prompt-voice|voice`; `config` deprecated); `azure.ai.project` has `endpoint`, `deployments`, `network` and forbids `project/runtime/docker/image/config` (`azure.ai.projects/schemas/azure.ai.project.json`). Connections, toolboxes, skills, routines and evals are "code-less resource services; the service key is the name".
- Cross-service references use `uses: [<name>]` where the schema says the name is a *service name or resource name*, so a `uses` entry may name a `resources:` entry as well as a service. Env substitution uses `${VAR}`. Foundry runtime expressions are written `${{...}}` in the Learn reference (passed through untouched) and `$${{...}}` in the official `complex.azure.yaml` example ("the extra $ preserves this Foundry expression through azd"). **The sources conflict**; the reader must accept both and the rules that mention the syntax (FND-CFG-005, FND-CFG-006, FND-CFG-011) must not treat either as an unresolved reference.
- Other `resources:` types exist (`ai.project`, `ai.openai.model`, `ai.search`, ...) in the core schema's `resources` map and are a separate (non-service) mechanism.

### Version fields available for gating

| Field | Where | Use |
|---|---|---|
| `requiredVersions.azd` | azure.yaml | compare with running azd; unsupported range -> diagnostic |
| `requiredVersions.extensions.<id>` | azure.yaml | pins extension versions |
| azd version | `azd version` / extension host | gate by tested range |
| `azure.ai.projects` / `azure.ai.agents` versions | `azd extension list`, `extension.yaml` `version` | gate by tested range (installed versions are not in azure.yaml) |
| schema channel | `$schema` comment | informational |

## Decision

1. Foundry Doctor treats azure.yaml-only projects as first-class. Source acquisition reports `iac: synthetic` when no `<infra.path>/<infra.module>.bicep|.bicepparam` exists (and no `infra.layers` path with one) and `infra.provider` is `microsoft.foundry` (or a legacy Foundry host drives it).
2. The azure.yaml reader is a lossless YAML AST (`internal/azureyaml`) that reports duplicate mapping keys, unresolved `${VAR}` (honouring `:-` defaults and `$$` escapes) and unresolved `uses` targets as findings with line/column. The executable spike `test/spikes/azd/spike_test.go` pins this behaviour against `expected.json` and runs in `go test ./...`. Verified with `go.yaml.in/yaml/v3`: decoding into a map or typed struct rejects a duplicate key with an error that carries no usable location, while decoding into a node AST succeeds, so the AST route is the one that yields every occurrence and its line. Some other YAML libraries keep the last value silently, which is why the AST route is the portable choice. A second, build-tagged test validates the valid fixture against the real azd JSON schema and proves the check can fail (`schema_spike_test.go`).
3. Rules needing ARM facts are `skipped` with reason `synthetic-infrastructure` in local mode; they never pass. Rules about azure.yaml, references, env keys and cross-service wiring run.
4. Env resolution order for local mode: selected azd environment file, then process environment, mirroring `resolveVars`. Missing env file: references are `unresolved` and the finding says which env was selected.
5. Unknown versions fail safely, by tier:
   - Unknown `host` value: do not fail; emit informational `unsupported-host` and skip rules for that service (the schema treats hosts as open examples).
   - Unknown properties on a Foundry service: ignore for correctness rules (schema allows additional properties; synthesis "intentionally ignores unknown fields"), but record them so a rule never asserts absence of a property it has not verified.
   - `requiredVersions.azd` not satisfied by the *installed* azd: report as an error finding (azd itself fails to load the project).
   - Installed/declared Foundry extension version outside the rule's `compatibility` range: the rule is `skipped` with `unsupported-version`, never `passed`. The run-level summary lists every skipped-for-version rule.
   - `$schema` channel `alpha` or a schema URL we have not pinned: run, but mark confidence lowered and list in the report header.
   - Unparseable YAML: single `invalid-azure-yaml` error, all dependent rules skipped.
6. Pin supported versions in the catalogue `compatibility` fields, not in code. They are machine-comparable closed ranges that name exactly the versions whose schemas or source were read (azd `>=1.34.2 <=1.36.0-beta.1`; azure.ai.projects, azure.ai.agents, connections, toolboxes and routines at the single beta version read), plus a `preview` flag. The catalogue validator enforces the format (`internal/catalog`). Widen a range only by reading the schema at the older or newer tag. A version outside the range makes the rule `skipped` with `unsupported-version`.

## Consequences

- Some security/networking rules will be mostly "skipped" for azure.yaml-only projects in `--local`; the report must make that visible (Skipped is not passed).
- Re-verify on every azure.ai.projects release: beta versions have already removed fields (README "Breaking migration", retired `network.mode/byo/managed`).

## Unverified

- Exact set of resources the embedded `main.bicep` creates per extension version (the files exist; they were not compiled or enumerated).
- Where azd records the default environment (`.azure/config.json` field names) and `.azure/<env>/config.json` schema (docs mention the files; fields not read).
- Alpha-schema gating specifics.
