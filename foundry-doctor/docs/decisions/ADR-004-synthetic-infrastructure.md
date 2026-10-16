# ADR-004: Synthetic infrastructure (azure.yaml-only Foundry projects)

- Status: Accepted design; provider-template evidence remains provisional (ADR-008)
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
2. The azure.yaml reader contract is a lossless YAML AST (`internal/azureyaml`) that reports duplicate mapping keys, unresolved `${VAR}` (honouring `:-` defaults and `$$` escapes) and unresolved `uses` targets as findings with line/column. The executable spike `test/spikes/azd/spike_test.go` exercises the proposed behaviour against `expected.json` in `go test ./...`; it is spike code, not the Phase 1 reader. A second, build-tagged test validates the fixture against schemas from the exact Azure-dev commit `afe4b2b4d262bab4c11f4937e7a7942557ab0ecd` and proves a deliberately invalid agent is rejected (`schema_spike_test.go`). That test is conditional on `AZURE_DEV_DIR`, Python, PyYAML and jsonschema/referencing. It is not run by `verify-phase.sh`; the local Windows ARM64 tagged command passed, while successful current CI evidence remains open.
3. Rules needing ARM facts are `skipped` with reason `synthetic-infrastructure` in local mode; they never pass. Rules about azure.yaml, references, env keys and cross-service wiring run.
4. Env resolution for local mode: the selected azd environment file `.azure/<env>/.env` is the only source of variable values. No process environment variable is read for rule values, so a run is offline and deterministic and does not depend on the shell it starts from (this supersedes the earlier "then process environment, mirroring `resolveVars`" order, review item m6). The process environment is read only to choose the environment name, in the configuration layer: `FOUNDRY_DOCTOR_ENVIRONMENT` (config.EnvEnvironment) and, when that is unset, `AZURE_ENV_NAME`, which azd exports to extensions; `AZD_ENVIRONMENT` is not read. An explicit flag wins over both. Missing env file: references that depend on it are reported as uncertain with a named input-unavailable reason (`no-environment-selected`, `env-file-missing`, `default-environment-not-found`, `env-file-unreadable`, `env-file-unparseable`, `env-file-oversize`) and the skip says which env was selected.
5. Unknown versions fail safely, by tier:
   - Unknown `host` value: do not fail; emit informational `unsupported-host` and skip rules for that service (the schema treats hosts as open examples).
   - Unknown properties on a Foundry service: ignore for correctness rules (schema allows additional properties; synthesis "intentionally ignores unknown fields"), but record them so a rule never asserts absence of a property it has not verified.
   - `requiredVersions.azd` not satisfied by the *installed* azd: report as an error finding (azd itself fails to load the project).
   - Installed/declared Foundry extension version outside the rule's `compatibility` range: the rule is `skipped` with `unsupported-version`, never `passed`. The run-level summary lists every skipped-for-version rule.
   - `$schema` channel `alpha` or a schema URL we have not pinned: run, but mark confidence lowered and list in the report header.
   - Unparseable YAML: single `invalid-azure-yaml` error, all dependent rules skipped.
6. Pin supported versions in the catalogue `compatibility` fields, not in code. They are machine-comparable closed ranges that name exactly the versions whose schemas or source were read (azd `>=1.34.2 <=1.36.0` for most rules, and `<=1.36.0-beta.1` for the environment rules whose cited files changed in 1.36.0; azure.ai.projects, azure.ai.agents, connections, toolboxes and routines at the single beta version read), plus a `preview` flag. The catalogue validator enforces the format (`internal/catalog`). Widen a range only by reading the schema at the older or newer tag. A version outside the range makes the rule `skipped` with `unsupported-version`.

## Consequences

- Some security/networking rules will be mostly "skipped" for azure.yaml-only projects in `--local`; the report must make that visible (Skipped is not passed).
- Re-verify on every azure.ai.projects release: beta versions have already removed fields (README "Breaking migration", retired `network.mode/byo/managed`).

## Unverified

- Exact set of resources the embedded `main.bicep` creates per extension version (the files exist; they were not compiled or enumerated).
- Successful current CI evidence for the build-tagged schema and provider spike. The local tagged
  command passed against the exact pinned clone and workflow-pinned Python dependencies.
- Where azd records the default environment (`.azure/config.json` field names) and `.azure/<env>/config.json` schema (docs mention the files; fields not read).
- Alpha-schema gating specifics.

## Amendment 1 (2026-10-15): Phase 1 implementation status of the Decision 5 tiers

Review findings Azure N12/N13 and Foundry N13/N19 compared the tiers with the code.

| Tier (Decision 5) | Phase 1 behaviour | Reason |
|---|---|---|
| `synthetic-infrastructure` skip (Decision 3) | Implemented in the engine (ADR-012 Amendment 1). | |
| Unknown azd version | Rules with an azd range skip as `azd-version-unknown` (was `input-unavailable`) with `MissingCapability: azd version`. The version comes from `--azd-version`, then `FOUNDRY_DOCTOR_AZD_VERSION`, then `azd version --output json`. The report header prints `versions not detected: azd`. | ADR-010 decision 10. |
| `requiredVersions.azd` | Read as evidence only: when azd is not detected and azure.yaml declares it, the header note quotes it. It never replaces the installed version and never passes a version-gated rule. The error finding for an unsatisfied range was **not implemented** in Phase 1 as first built; it is implemented since 2026-10-16 (see the update under Amendment 2). | Superseded 2026-10-16: the range grammar is now verified (`docs/spikes/azd-required-versions.md`, `docs/tool-compatibility.md`) and implemented in FND-CFG-001 version 6 and later. Originally the grammar was unverified, and a hand-written parser would have asserted an unverified product fact. azd itself refuses to load the project when the range is unmet (see Findings). |
| `unsupported-host` | **Not implemented.** Rules decide applicability per service host and skip with their own reason. No `FND-SYS-UNSUPPORTED-HOST` finding is emitted. | The host list in the schema is `examples`, not an enum; no rule yet needs a host allow-list. ADR-009 keeps the reserved ID. |
| `invalid-azure-yaml` | Unparseable azure.yaml stops the run with an error naming the parse problem and exit 2 (`ErrUnavailable`). No `FND-SYS-INVALID-AZURE-YAML` finding is emitted and no rule runs. | Exit 2 is the contract for "validation could not run" (PRD section 7, ADR-010 decision 2); a finding would add no information because there is nothing to report on. |
| Extension versions (`azure.ai.*`) | Not detected; ranges in the catalogue are not compared. The report cannot claim these rules passed for the installed extension. | Extension detection needs `azd extension list` output that is not yet verified. |

## Amendment 2 (2026-10-15, superseded 2026-10-16): `requiredVersions.azd` grammar verified and error finding implemented (Foundry lead R10)

The text below records the 2026-10-15 position, when the grammar was unverified and the finding was not implemented. The update at the end of this amendment supersedes it: the grammar is verified and the finding is implemented in FND-CFG-001.

Decision 5 says an unsatisfied `requiredVersions.azd` is reported as an error finding. Verified from the vendored azd schema (`schemas/vendor/azd/azure.yaml.json`): the property description reads
"A range of supported versions of `azd` for this project. If the version of `azd` is outside this range, the project will fail to load. Optional (allows all versions if absent)."
and the only example is `>= 0.6.0-beta.3`. The schema fixes that an unmet range makes the project fail to load, so the consequence would be platform-basis.
It does not define the grammar: operators, whitespace, `||` alternatives, wildcards and pre-release ordering are not stated, and no azd source file defining the range parser was read.
A hand-written parser would assert an unverified product fact, and a wrong parse would produce a false platform error in every profile. The finding is therefore **not implemented in Phase 1**.
Behaviour is unchanged from Amendment 1: the range is evidence only and is quoted in the report header when azd is not detected. Revisit when the azd range parser or its documentation is read and pinned in
`docs/tool-compatibility.md`. The Decision 5 bullet on `requiredVersions.azd` is a target, not Phase 1 behaviour.

**Update (2026-10-16): R10 is now implemented.** The grammar was read from the azd source and the two semver libraries it uses and is recorded in `docs/spikes/azd-required-versions.md` (grammar tables for `requiredVersions.azd`, which uses blang/semver/v4 `ParseRange`, and for `requiredVersions.extensions.<id>`, which uses Masterminds/semver/v3 `NewConstraint`). FND-CFG-001 (version 6) now:

- reports an error finding (`required-versions-azd-invalid`, platform basis) when `requiredVersions.azd` is not a valid blang range, because azd then fails to load the project (`project.go:63-77`);
- reports an error finding (`required-versions-azd-out-of-range`) when the azd version is known (`--azd-version`, `FOUNDRY_DOCTOR_AZD_VERSION` or the probe) and outside the range;
- is **exempt for a local dev build** (`0.0.0-dev.0`) for the out-of-range comparison only, as azd skips the comparison for a dev build (`internal.IsDevVersion()`, `project.go:70`). The exemption is unit-level: outside the FND-CFG-001 azd window the whole rule is skipped (`unsupported-version`), so a dev build gets no check at all there. An invalid range is still reported for a dev build, because azd runs `ParseRange` (`project.go:64`) before the dev-build skip (`project.go:70`) and therefore errors on an invalid range even for a dev build;
- reports an uncertain skip naming `azd-version-unknown` when the range is declared and the version is unknown, and an uncertain skip when the version is not valid semver or the range uses a form the note does not cover (an operator on a wildcard, a bare `x`, `||` glued to a version);
- parses `requiredVersions.extensions.<id>` with the Masterminds grammar; an invalid value is uncertain (azd falls back to an exact tag match), never an error, and extension constraints are never decided offline because installed extension versions live in azd's user config and cannot be read.

The parsers are small and hand-written (no new dependency). They are checked against the real libraries (blang/semver/v4 v4.0.0 and Masterminds/semver/v3 v3.4.0) by a differential test over a committed fixture generated from a one-off run of both; forms where the run disagreed with the first parsers (one-character tokens, `x` outside `N.x` and `N.M.x`, `!` followed by a space, a few Masterminds wildcard and zero-version forms) are reported as uncertain. Not verified: azd itself was not executed; older azd releases may differ. The Decision 5 bullet is therefore Phase 1 behaviour, and the statement above that the finding is not implemented in Phase 1 is superseded. Version 7 of FND-CFG-001 (Foundry lead re-review N4-1) narrows "invalid" further: a dangling operator, an operator and a non-version separated by a space, `! 1.35.0` and repeated `||` are uncertain, because `ParseRange` accepts them; only failures proven by the differential fixture are error findings.
