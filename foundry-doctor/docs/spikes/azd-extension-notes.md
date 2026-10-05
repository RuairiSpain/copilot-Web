# azd extension framework notes (Phase 0 spike)

lastVerified: 2026-10-04. Sources: `REFS/azure-dev` = `Azure/azure-dev` @ `afe4b2b4d262bab4c11f4937e7a7942557ab0ecd` (sparse: `cli/azd/docs`, `cli/azd/extensions`, `schemas`; `cli/version.txt` = `1.36.0-beta.1`). Files marked (raw-main) were fetched from `raw.githubusercontent.com/Azure/azure-dev/main/...` on the same date, not from the pinned clone, because `cli/azd/pkg/azdext` and `cli/azd/cmd` are not in the sparse clone. Go proxy facts from `proxy.golang.org`. Anything not stated with a source is marked **unverified**. Decisions: ADR-003, ADR-004, ADR-008. This is a research snapshot, not current compatibility certification.

## 1. Latest released azd and Foundry extension versions

| Item | Value | Source |
|---|---|---|
| Latest azd module tag seen | `v1.35.0` (2026-09-30, tag `cli/azd/v1.35.0`; `v1.34.2` was the latest when this spike ran) | `proxy.golang.org/github.com/azure/azure-dev/cli/azd/@v/v1.35.0.info` |
| Repo head version | `1.36.0-beta.1` | `cli/version.txt` |
| `azure.ai.agents` | 1.0.0-beta.18, requires azd `>=1.34.2`, namespace `ai.agent` | `extensions/azure.ai.agents/extension.yaml`, `version.txt` |
| `azure.ai.projects` | 1.0.0-beta.13, requires azd `>=1.34.2`, namespace `ai.project` | `extensions/azure.ai.projects/extension.yaml`, `version.txt` |
| `microsoft.foundry` (pack) | 1.0.0-beta.3, requires azd `>=1.32.0`, depends on agents `~1.0.0-beta.16`, projects `~1.0.0-beta.11`, etc.; no commands | `extensions/microsoft.foundry/extension.yaml`, `CHANGELOG.md` |
| Learn install article | says azd 1.25.2 or later (stale vs manifests) | `azure-ai-docs/articles/foundry/agents/how-to/install-cli-foundry-extensions.md` |

## 2. Manifest (`extension.yaml`)

Schema: `cli/azd/extensions/extension.schema.json` (draft-07). Required: `id`, `version`, `displayName`, `description`, plus either `capabilities` or `dependencies` (packs) (`docs/extensions/extension-framework.md`, "Schema Properties"). Optional: `namespace`, `entryPoint`, `usage`, `examples`, `tags`, `dependencies`, `providers`, `platforms`, `mcp`, `requiredAzdVersion`.

- `version` pattern `^\d+\.\d+\.\d+(-[A-Za-z0-9-.]+)?$` (schema).
- `requiredAzdVersion`: semver constraint; azd filters incompatible versions on install/upgrade; empty or unparsable is treated as compatible (fail-open) (`docs/extensions/extension-resolution-and-versioning.md`, "requiredAzdVersion Field").
- `capabilities` enum (schema): `custom-commands`, `lifecycle-events`, `mcp-server`, `service-target-provider`, `framework-service-provider`, `provisioning-provider`, `validation-provider`, `metadata`. Foundry Doctor needs `custom-commands` and `metadata`. A validation provider (`validation-provider`, check type `provision`) could later contribute preflight checks inside `azd provision`; not required for Phase 0-1 (`extension-framework.md`, "Validation Provider").
- Namespace: dot-separated, nested into command groups by `bindExtension` (`cli/azd/cmd/extensions.go`, raw-main).
- Example first-party manifest with `metadata`: `extensions/azure.ai.connections/extension.yaml`.

## 3. Registry and packaging

- Registry entries need `id` (`^[a-z0-9-.]+$`), `namespace`, `displayName`, `description`, and `versions[]` with `version`, `capabilities`, `usage`, `examples`, `artifacts`; each artifact needs a `checksum` (`sha256` or `sha512`) and URL; minimum platforms `linux/amd64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64` (`docs/extensions/extension-resolution-and-versioning.md`, "Requirements"; schema `extensions/registry.schema.json`). Build targets are windows/linux/darwin on amd64 and arm64 (`extension-framework.md`, "Cross-Platform Support").
- Workflow with the developer extension: `azd x init`, `azd x build`, `azd x pack`, `azd x release --repo`, `azd x publish --repo` (`extension-framework.md`, "Developer Extension", "Publishing Workflow"). `azd x init --namespace <ns> --capabilities ... --language go`.
- Custom sources: `azd extension source add -n <name> -t url|file -l <location>` (names: 1-64 lowercase letters, digits, `-`, `_`; `bundle` reserved) (`extension-framework.md`, "Extension Sources"). The official registry is `https://aka.ms/azd/extensions/registry` (Microsoft-owned). Third-party distribution therefore uses our own registry JSON hosted at a URL.
- Self-contained bundles (`.zip` path or HTTPS URL) can be installed with `azd extension install <url>` (azd-docs `extensions/overview.md`, "Manage extensions").
- Extensions installed from non-official sources: telemetry events are recorded only for official-registry sources (`extension-framework.md`, telemetry note). Foundry Doctor sends no telemetry in any case (read-only, no data egress).
- Official-registry publication requires Microsoft review (dev registry is "unsigned... no stability guarantees") (`extension-framework.md`, "Dev (Experimental) Registry"). Signing requirements for our own registry: **unverified**.

## 4. Invocation contract

From `docs/extensions/extension-framework.md`, "Invoking Extension Commands":

1. azd starts a gRPC server on a random local port; sets `AZD_SERVER` and `AZD_ACCESS_TOKEN` (JWT carrying extension id and capabilities). Other azd env values are injected into the process environment.
2. A positive extension exit code is propagated by azd. Foundry Doctor does not inherit another
   extension's sample classification: it emits 1 for findings and 2 when the requested validation
   cannot run, per PRD section 7.
3. Service calls need the declared capability, otherwise "permission denied".
4. Go SDK entry points: `azdext.Run`, `azdext.NewExtensionRootCommand` (registers `--debug`, `--no-prompt`, `-C/--cwd`, `-e/--environment`, `-o/--output`; exposes `ExtensionContext{Debug, NoPrompt, Cwd, Environment, OutputFormat}`), `azdext.NewAzdClient` (reads `AZD_SERVER`) (`docs/extensions/extension-sdk-reference.md`; `pkg/azdext/extension_command.go`, `pkg/azdext/azd_client.go`, both raw-main).
5. Reserved flags that extensions must not redeclare: environment (`-e`), cwd, debug, no-prompt, output, help, docs, trace-log-file, trace-log-url (`docs/extensions/extensions-style-guide.md`). Foundry Doctor uses `--out` for a file or directory destination in both extension and standalone modes. `-o/--output` remains azd's output-format flag. This resolves the former PRD conflict (ADR-003 and ADR-007).
6. Contract channels: stable `azd.extensions.v1` and beta `azd.extensions.v1beta`; `ComposeService`, `CopilotService`, `TelemetryService` are beta-only (`docs/extensions/contract-versioning.md`). Use stable `v1` only.

## 5. gRPC service surface relevant to Foundry Doctor

From `extension-framework.md` "gRPC Services" and `pkg/azdext/azd_client.go` (raw-main) accessors:

| Service (`AzdClient` accessor) | Useful for | Notes |
|---|---|---|
| `Project()` | project name, path, services map, infra options; raw service config via `GetServiceConfigSection` | `ServiceConfig.environment` holds **expanded** values; raw `${VAR}` templates require the config-section RPCs (doc text under "gRPC Services") |
| `Environment()` | current env, list, get by name, key-values | |
| `UserConfig()` | azd user config | |
| `Prompt()` | subscription, location, resource group, confirm | optional only |
| `Account()` | subscriptions, tenants, tokens | candidate for Azure auth in extension mode; standalone uses `azidentity` |
| `Deployment()` | deployment info | usage **unverified** |
| `Events()`, `ServiceTarget()`, `FrameworkService()`, `Provisioning()`, `Validation()` | provider/event capabilities | not needed Phase 0-1 |
| `Ai()`, `Container()`, `Workflow()`, `Extension()` | out of scope | |

Read-only guarantee: Foundry Doctor must call only getters (no `AddService`, `Set*`, `Prompt` mutations); a test with a fake `AzdContext` must assert no write method exists on the interface.

## 6. Can the SDK be used from a standalone binary?

No, not as a replacement for azd. `NewAzdClient` dials `AZD_SERVER` and every call needs `AZD_ACCESS_TOKEN` (`azd_client.go`, lines near `os.Getenv("AZD_SERVER")`, `os.Getenv("AZD_ACCESS_TOKEN")`). The server only exists while azd runs the extension. A standalone `foundry-doctor` binary therefore needs its own filesystem-based implementation of the same interface (ADR-003 table). `AzdContext` should abstract: project root and azure.yaml bytes, raw vs expanded service config, environment name/list/values, subscription/tenant/token (optional), prompts (optional), user config (optional), and the working directory. Core packages must not import `azdext`.

Dependency weight: `azdext` lives in the single Go module `github.com/azure/azure-dev/cli/azd` (`cli/azd/go.mod`, go 1.26.4), which pulls the whole azd dependency tree (including many `armXXX` SDK modules). First-party extensions depend on the module at the azd version, for example `azure.ai.agents/go.mod` requires `github.com/azure/azure-dev/cli/azd v1.34.2`. To keep the standalone binary light, confine the import to `cmd/foundry` (extension host adapter) in its own Go build target; the standalone `cmd/foundry-doctor` must not link it. Whether a separate Go module is needed to avoid the transitive dependencies in `go.mod` of the root: recommend a separate module for the extension host adapter (decision for Phase 1; binary-size effect **unverified**).

## 7. Minimum azd version

- Framework features we rely on (custom-commands, metadata, exit-code propagation, `NewExtensionRootCommand`, `RegisterFlagOptions`) have no recorded minimum in the docs we read; **unverified** per feature.
- First-party Foundry extensions we interoperate with require `>=1.34.2` (agents, projects) (their `extension.yaml`). Recommended `requiredAzdVersion: ">=1.34.2"` for Foundry Doctor, so the SDK it compiles against (v1.34.2, the latest tag seen) is satisfied and the Foundry azure.yaml shapes match (`azure.ai.project` host). Revisit after testing older azd.

## 8. Licence

- Repository licence: MIT, "Copyright 2022 (c) Microsoft Corporation" (`REFS/azure-dev/LICENSE`). Go source headers: "Copyright (c) Microsoft Corporation. All rights reserved. Licensed under the MIT License." (e.g. `extensions/azure.ai.projects/internal/synthesis/synthesizer.go`). `azdext` is in the same module, so MIT applies (per-file header of `pkg/azdext` not read; **unverified** for that directory specifically).
- Third-party notices exist at repo root (`NOTICE.txt`); if we link `azdext` we inherit its transitive dependency licences. Run a licence scan (Phase 1) before release.
- Docs clones: MicrosoftDocs repos carry CC-BY-4.0 (`LICENSE`) and MIT for code (`LICENSE-CODE`); we cite, not copy.

## 9. Overlap with first-party commands

- `azd ai agent doctor` (agents 1.0.0-beta.18): local plus remote checks, exit 0/1/2, read-only (`azure-ai-docs/articles/foundry/agents/how-to/agent-doctor.md`; `extensions/azure.ai.agents/internal/cmd/doctor.go`, `internal/cmd/doctor/`). Candidate for the overlap matrix: Foundry Doctor should not duplicate its remote agent-liveness checks; wrap or reference instead. Not evaluated in depth here.
- `azd ai project`, `azd ai connection`, `azd ai toolbox`, `azd ai skill`, `azd ai routine`, `azd ai inspector` own resource-authoring commands (install-cli-foundry-extensions.md). Foundry Doctor is read-only and must never mutate what these manage.

## 10. Fixtures and schema-spike status

`test/spikes/azd/valid-azure-yaml-only/` and `test/spikes/azd/invalid-duplicate-and-unresolved/` with `expected.json`. Every field is from `schemas/v1.0/azure.yaml.json` or the official examples in `extensions/azure.ai.agents/schemas/examples/`. The azd env file is stored as `.azure/dev/env.fixture` because the harness blocks writing `.env`; the loader in tests should map `env.fixture` to the real `.env` name (copy at test time).

The original spike did not run schema validation. It was later augmented by
`test/spikes/azd/schema_spike_test.go` plus `validate_schema.py`. With `-tags spike`, an absolute
`AZURE_DEV_DIR`, and Python packages PyYAML and jsonschema/referencing, the test:

1. loads `schemas/v1.0/azure.yaml.json` from that clone;
2. resolves only `raw.githubusercontent.com/Azure/azure-dev/main/...` references back into the same clone;
3. requires the valid fixture to pass; and
4. removes the required agent `project` field and requires that modified fixture to fail.

The harness exits 2 for missing Python dependencies and the Go test records that case as **SKIPPED**.
The test is build-tagged and is not invoked by `verify-phase.sh`. The current CI workflow is
configured to run it against exact `Azure/azure-dev` commit `afe4b2b4d262bab4c11f4937e7a7942557ab0ecd` with pinned Python packages,
alongside the Bicep 0.48.1 spike. No successful run of that CI job is evidenced. The local tagged
command passed locally on Windows ARM64 with Go 1.26.4 and Python 3.13. Successful current CI evidence
remains open.
It structurally validates the pinned extension's embedded ARM assets but does not compile or enumerate
a newly returned `microsoft.foundry` template, or prove compatibility with another azure-dev revision.

Reproduction:

```sh
AZURE_DEV_DIR=/absolute/path/to/azure-dev go test -tags spike ./test/spikes/azd/
```

## Unverified summary

- Namespace collision handling in `bindExtension`; reserved namespaces beyond built-ins.
- Minimum azd per framework feature; signing requirements for third-party registries.
- `pkg/azdext` per-file licence header; binary-size effect of importing the azd module.
- `Deployment()` service usage; `.azure/config.json` fields.
- Successful current CI schema-spike result on the current revision; local execution passed, but no
  successful current CI run is evidenced.
