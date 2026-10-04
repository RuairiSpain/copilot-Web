# ADR-003: Command namespace

- Status: Accepted (Phase 0 release gate: PASS with conditions)
- lastVerified: 2026-10-04
- Evidence base: `Azure/azure-dev` @ `afe4b2b4d262bab4c11f4937e7a7942557ab0ecd` (cli/version.txt `1.36.0-beta.1`), local clone `REFS/azure-dev`; `MicrosoftDocs/azure-dev-docs` and `MicrosoftDocs/azure-ai-docs` clones. Details and SDK surface: `docs/spikes/azd-extension-notes.md`.

## Context

The PRD (section 4) names the public prefix `azd foundry` and says that if it conflicts with a first-party command, the prefix becomes `azd guard` or `foundry-doctor`. Namespace resolution is a Phase 0 release gate.

## Findings

1. The framework allows an arbitrary namespace. `namespace` is an optional string in `extension.yaml` ("Namespace used to group extension commands; optional") (`azure-dev/cli/azd/extensions/extension.schema.json`, property `namespace`). The registry schema marks it required for registry entries (`registry.schema.json`, `namespace`). No reserved-name list for namespaces was found in the docs or schemas. Only `bundle` is reserved, and that is for extension *source* names (`cli/azd/docs/extensions/extension-framework.md`, "extension source add").
2. Namespaces are split on dots into nested command groups; the last segment is the command. `bindExtension` does `strings.Split(extension.Namespace, ".")`, creating parent groups on demand (`cli/azd/cmd/extensions.go`, fetched from `main` via raw.githubusercontent.com, not pinned to the clone commit). So `ai.agent` becomes `azd ai agent`, and `foundry` becomes `azd foundry`. Subcommands (`doctor`, `explain`, ...) are the extension's own cobra tree (`azdext.NewExtensionRootCommand`, `docs/extensions/extension-framework.md` "Building a Root Command").
3. First-party extension namespaces in the official registry (`azure-dev/cli/azd/extensions/registry.json`): `demo`, `x`, `coding-agent`, `ai.agent`, `ai.finetuning`, `ai.models`, `appservice`, `ai.inspector`, `ai.connection`, `ai.project`, `ai.routine`, `ai.skill`, `ai.toolbox`, `ai.eval`, `ai.dataset`, `concurx`. The extension `microsoft.foundry` has no namespace: it is a pack that "doesn't contribute its own commands" (`extensions/microsoft.foundry/extension.yaml`; `azure-ai-docs/articles/foundry/agents/how-to/install-cli-foundry-extensions.md`). Dev registry (`registry.dev.json`): `ai.training`, `ai.rle`. In-tree but not in registries: `microsoft.azd.ai.builder` (`ai.builder`).
4. Core azd top-level commands (`azd-docs/articles/azure-developer-cli/reference.md`) include add, auth, completion, config, copilot, deploy, down, env, hooks, infra, init, mcp, monitor, package, pipeline, provision, publish, restore, show, template, up. None is `foundry`. None of `doctor|explain|compare|assess|graph|annotate|cost|scaffold` exists as a core command either (and they would sit below `foundry` anyway).
5. A string search for `azd foundry` (lowercase command form) across the three clones found no hit. The phrase "azd Foundry extensions" in Learn is prose for the `azd ai` family, not a command.
6. Functional overlap, not a syntactic conflict: `azd ai agent doctor` exists (`azure.ai.agents` 1.0.0-beta.18, `extensions/azure.ai.agents/internal/cmd/doctor.go`; Learn `agents/how-to/agent-doctor.md`). It diagnoses a hosted-agent azd project (azd version, azure.yaml has an `azure.ai.agent` service, env keys, endpoint, roles, deployed agent), exits 0/1/2, and is read-only. Foundry Doctor differs in scope (rule catalogue, profiles, baselines, SARIF, Bicep/ARM correlation, WAF, cost), but users will see two "doctor" commands.
7. Namespace collisions are not rejected by anything we could read. `bindExtension` looks up an existing child by name only for intermediate segments; behaviour when two extensions claim the same final segment is **unverified**. We therefore mitigate by CI check (below), not by relying on azd.

## Decision

Keep `azd foundry` as the public prefix.

- Extension `namespace: foundry` (single segment, so `azd foundry doctor|explain|compare|assess waf|graph|annotate|cost|scaffold`).
- Extension `id` must not be `microsoft.foundry` (taken; also the name of the provisioning provider and of a legacy `host` value, `schemas/v1.0/azure.yaml.json`). Use a publisher-owned id, for example `<publisher>.foundry-doctor` (matches registry id pattern `^[a-z0-9-.]+$`). Publisher is a product decision, not decided here.
- Standalone binary `foundry-doctor` ships from the same command model; it is the permanent fallback.
- The prefix lives in one place (the command layer), per CLAUDE.md, so renaming to `azd guard` is a one-line change plus docs.

## Release-gate conclusion

PASS with conditions. No first-party command or extension uses or conflicts with `foundry`. Conditions:

1. CI check (Phase 1): fail the build if the pinned `registry.json` / `registry.dev.json` of `Azure/azure-dev` contains `namespace` equal to `foundry` or starting with `foundry.`, or a core command named `foundry` appears in the azd reference. Re-run at each release.
2. Distribution is through a custom extension source (`azd extension source add -t url`), because the official registry is Microsoft-owned (`extension-framework.md`, "Extension Sources"). Listing in the official registry is out of scope and would need Microsoft agreement on the name.
3. Product naming risk (Microsoft Foundry is a Microsoft brand and `azd ai agent doctor` exists): README and `--help` must say Foundry Doctor is a community/independent tool and not part of `azd ai`. Fallback order if Microsoft claims `foundry`: `azd guard`, then standalone `foundry-doctor`.
4. Documentation must tell users which command to use when: `azd ai agent doctor` for agent-project health; `azd foundry doctor` for rule-based validation of a whole Foundry project.

## Core abstraction: AzdContext

The extension SDK cannot be used from a standalone binary. `azdext.NewAzdClient` reads `AZD_SERVER` and `AZD_ACCESS_TOKEN`, which azd sets only when it launches the extension (`cli/azd/pkg/azdext/azd_client.go`, `NewAzdClient`; `docs/extensions/extension-framework.md`, "Invoking Extension Commands"). Core logic therefore must not import `azdext`. `AzdContext` (defined in the consumer, `internal/source` or `internal/config`) abstracts:

| Capability | Extension implementation (gRPC) | Standalone implementation |
|---|---|---|
| Project root and azure.yaml bytes | `Project().Get` gives `path`, `services`, `infra` (parsed, env-expanded) | walk up from cwd to `azure.yaml`; read raw bytes |
| Raw (unexpanded) service config | `GetServiceConfigSection` (raw); `ServiceConfig.environment` is already expanded | the file itself |
| Environments and selected env | `Environment().GetCurrent/List/Get`, `AZD_ENVIRONMENT` / `-e` | list `.azure/*`, honour `-e` / `AZURE_ENV_NAME` |
| Environment values | `Environment` key-value RPCs | parse `.azure/<env>/.env` |
| User config | `UserConfig()` | optional, `~/.azd/config.json` (unverified path) |
| Prompts | `Prompt()` (subscription, location, confirm) | none; `--no-prompt` semantics, fail with a clear skip reason |
| Subscription/tenant/token | `Account()` | `azidentity` credential chain |

Rule: the doctor never needs prompts to produce findings; prompts are optional convenience in extension mode only.

## Consequences

- One cobra command model, two host adapters (`cmd/foundry` extension, `cmd/foundry-doctor` standalone), as in the PRD package table.
- The extension must declare capabilities `custom-commands` and `metadata` (see notes) and must use `requiredAzdVersion` (>= 1.34.2 recommended; see notes).
- Exit codes: azd propagates a positive extension exit code (`extension-framework.md`, "Invoking Extension Commands"), so `0` pass, `1` operational failure, `2` gate breach can be preserved under `azd foundry`.

## Unverified

- Behaviour when two extensions register the same namespace (core `bindExtension` collision handling).
- Whether azd core reserves any extension namespace string beyond built-in command names (core command registration code is not in the sparse clone).
- Whether Microsoft plans a `foundry` command group (no signal in the clones).
