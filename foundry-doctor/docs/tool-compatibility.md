# Tool compatibility matrix (Phase 0)

Verified 2026-10-04 (updated 2026-10-16, see section 6). Sources: git tags and commit dates fetched from github.com (clone/ls-remote), the PyPI, npm and Go
proxy registries, and repository files read under `REFS` (shallow clones listed in
`docs/development/phase-0-research-brief.md`). `learn.microsoft.com`, `api.github.com` and GitHub release pages were not
reachable, so release dates below are tag commit dates (or registry upload dates) and can differ from the GitHub release
publication date by hours or days. Anything not verified is marked **Unverified**.

"Minimum version" values are proposals for Foundry Doctor (FD) and are labelled **verified floor** when a primary source
states the floor, or **proposed** when it is a recommendation that Phase 1 must confirm with a spike.
The table is structured research input, not a declaration that Foundry Doctor currently supports every listed tool.
No adapter compatibility claim is complete until a pinned-version executable fixture proves invocation, structured-output
parsing, external exit-code translation, redaction and missing-tool behaviour.

## 1. Summary

| Tool | Current stable (verified) | Tag / upload date | Proposed minimum for FD | FD dependency class |
|---|---|---|---|---|
| azd | 1.35.0 (`cli/azd/v1.35.0`); `main` is at 1.36.0-beta.1 (updated 2026-10-16: 1.36.0 is released and was diffed, see section 6) | 2026-09-30 | 1.34.2 (proposed; see 2.1) | Required when running as azd extension; optional for standalone CLI |
| Bicep CLI | 0.48.1 (`v0.48.1`) | 2026-09-29 | Current FD contract 0.48.1; hard floor 0.4.451 is PSRule-only, see 2.2 | Required when a requested Bicep check cannot otherwise run (exit 2); otherwise check is explicitly skipped |
| azd Foundry/AI agent extension | `azure.ai.agents` 1.0.0-beta.18 (beta, not GA); beta.19 and beta.20 exist (updated 2026-10-16, see section 6) | 2026-09-30 (CHANGELOG) | Not required; detect only | Optional (detect and report version) |
| PSRule for Azure | v1.47.0 stable; v1.48.0-B0228 pre-release | 2026-01-08 / 2026-06-22 | 1.47.0 (proposed) | Optional adapter |
| PSRule (engine module) | v2.9.0 stable; v3.0.0-B0783 pre-release | 2023-06-08 / 2026-04-28 | 2.9.0 (verified floor: `modules.json`) | Optional (via PSRule for Azure) |
| Checkov | 3.3.22 | 2026-10-01 (PyPI) | 3.3.x current at time of release; no verified floor | Optional adapter, disabled by default |
| KICS | v2.2.0 | 2026-09-17 | v2.2.0 (proposed) | Optional adapter |
| Terrascan | v1.19.9; **archived** | 2024-09-18 | Not supported | Do not integrate (see section 4) |
| Azure CLI | 2.90.0 | 2026-09-01 (PyPI) | Not required; optional, only as a Bicep or login fallback | Optional |
| Graphviz | 16.1.0 (gitlab.com/graphviz/graphviz tag) | 2026-09-04 | Not required; optional renderer for DOT | Optional |
| Mermaid (library) / mermaid-cli | mermaid 12.1.0 / @mermaid-js/mermaid-cli 12.0.0 | 2026-10-02 / 2026-09-24 (npm) | Not required; FD emits text only | Optional renderer |

## 2. Per-tool detail

### 2.1 Azure Developer CLI (azd)

- **Version**: stable tag `cli/azd/v1.35.0`, commit date 2026-09-30. `cli/version.txt` on main is `1.36.0-beta.1`.
  Repo: `Azure/azure-dev`, licence MIT.
- **Minimum**: 1.34.2 (proposed). Evidence: `azure.ai.agents` 1.0.0-beta.18 declares `requiredAzdVersion: ">=1.34.2"`
  (`cli/azd/extensions/azure.ai.agents/extension.yaml`, CHANGELOG entry "Require azd >=1.34.2"). The extension SDK helpers
  ship in azd 1.23.7 or later (`docs/extensions/extension-migration-guide.md`), which is the absolute floor for
  extension-hosted FD. The FD extension must declare its own `requiredAzdVersion` in `extension.yaml` (schema:
  `cli/azd/extensions/extension.schema.json`).
- **Non-interactive invocation**: `--no-prompt` (alias `--non-interactive`) or `AZD_NON_INTERACTIVE=true`. azd also enables
  no-prompt automatically in CI and when launched by a detected AI coding agent without an interactive terminal; explicit
  flags win over the variable (`cli/azd/docs/environment-variables.md`). `azd extension install` under `--no-prompt`
  cannot register a new source from a location, so sources must be added first with `azd extension source add`
  (`docs/extensions/extension-framework.md`).
- **Machine-readable output**: global `--output json` (documented for `azd auth status`, `azd env list`,
  `azd env get-values`, `azd template list` in `docs/authentication.md` and `docs/fig-spec.md`). A per-command JSON schema
  is not published; treat JSON shapes other than `azd env get-values`/`azd env list` as **Unverified**. Prefer reading
  `azure.yaml` and `.azure/<env>/.env` directly, and use azd only for version, extension and environment discovery.
- **Runtime prerequisites**: none for azd itself (Go binary). Extensions are separate binaries per platform. The azd
  module `github.com/azure/azure-dev/cli/azd` v1.35.0 declares `go 1.26.4` (see licence inventory for the consequence).
- **Extension contract stability**: protobuf contract has a `v1` stable channel and a long-lived `v1beta` channel
  (`docs/extensions/contract-versioning.md`). Use `pkg/azdext/contracts/v1` only; beta-only services must not be relied on.
- **Missing-tool behaviour**: when FD runs as an extension azd is by definition present. Standalone, `azd` absence is
  reported as "azd not found: environment and extension checks skipped" (an explicit skipped-check entry, never a pass).
  It is exit 2 only when the resolved configuration requests a check that needs azd environment data. Below the minimum
  version: report the detected and required versions and exit 2 for requested azd-dependent checks.

### 2.2 Bicep CLI

- **Version**: current contract `v0.48.1`, tag commit 2026-09-29; main HEAD 2026-10-02. Repo
  `Azure/bicep`, MIT. `docs/decisions/ADR-002-bicep-analysis.md` retains historical 0.47.16
  execution evidence. CI is configured to install and test 0.48.1, but no successful tagged CI run
  is evidenced; the local Windows ARM64 tagged 0.48.1 command passed.
- **Minimum**: verified floor 0.4.451 (PSRule for Azure `docs/en/setup/setup-bicep.md`). That is a floor for PSRule
  expansion, not for FD diagnostics. The current FD contract is 0.48.1; no lower supported FD
  version is established. **Unverified**: the earliest Bicep version that supports the flags
  FD relies on (`--diagnostics-format sarif`, `--stdout`, `jsonrpc`).
- **Non-interactive invocation** (flags confirmed in `src/Bicep.Cli` constants and `docs/experimental/docs-commands.md`):
  `bicep --version`; `bicep build <file> --stdout [--no-restore] [--diagnostics-format sarif]`;
  `bicep lint <file> [--diagnostics-format sarif]`; `bicep jsonrpc --stdio` for a persistent structured API
  (`docs/bicep-rpc-client.md`). `--diagnostics-format` accepts `default` or `sarif`; the enum has exactly those two values.
  `bicep build --stdout` emits the ARM JSON on stdout. External module restore needs network unless `--no-restore`.
- **Output**: ARM JSON template (stable, versioned by `$schema` and `languageVersion`); diagnostics as text or SARIF on
  stderr (for `docs generate`, documented as stderr; confirm for `build`/`lint` in the Phase 1 spike, **Unverified**).
  The SARIF format is a standard (OASIS 2.1.0); Bicep's own rule codes in it are stable by code (for example `BCP...`,
  `no-hardcoded-env-urls`), but wording is not.
- **Runtime prerequisites**: release binaries are .NET single-file, trimmed, self-contained
  (`PublishSingleFile`, `PublishTrimmed`; `src/Bicep.Cli/Bicep.Cli.csproj`, target `net10.0`), so no separate .NET install.
  ADR-002 records the case where the host lacks ICU (see its Bicep CLI discovery section); check there before running on minimal images.
  `az bicep` installs a copy under `~/.azure/bin` that is not on `PATH` (PSRule `setup-bicep.md`).
- **Missing-tool behaviour (PRD)**: a Bicep scan requested and the CLI missing, non-executable, or below the minimum
  returns **exit code 2** with an actionable message (PRD section 9; ADR-002 `ErrCLIMissing`). With `validation.bicep:
  required` it is a hard error; with an `azure.yaml`-only project Bicep checks are listed as skipped, not passed.

### 2.3 azd Foundry / AI agent extension

- **Identity**: the extension IDs live under `Azure/azure-dev` `cli/azd/extensions/`. The agents extension is
  `azure.ai.agents` (namespace `ai.agent`, usage `azd ai agent <command>`), version **1.0.0-beta.18**, CHANGELOG dated
  2026-09-30, `requiredAzdVersion >=1.34.2`. It depends on `azure.ai.inspector`, `azure.ai.projects`,
  `azure.ai.connections` and `azure.ai.toolboxes` (`~1.0.0-beta.x`). The meta-package `microsoft.foundry`
  (1.0.0-beta.3 in `extension.yaml`; `registry.json` lists up to 1.0.0-beta.2) bundles agents, connections, inspector,
  projects, routines, skills and toolboxes. Older tag series `azd-ext-azure-foundry-ai-agents_0.0.1/0.0.2` and
  `azd-ext-azure-ai-agents_0.1.x-preview` are superseded.
- **Stability**: every extension is `beta`/`preview`; the 1.0.0-beta.18 CHANGELOG lists **breaking changes**
  (unified `azure.yaml` required, standalone agent manifests removed). Rule property checks must stay version-aware
  (FND-CFG-011). Treat the extension as **unstable** for compatibility purposes.
- **Detection (non-interactive)**: `azd extension list --installed --output json` is the expected call (`--installed` and
  `--output json` are both documented; JSON shape **Unverified**). Report installed id and version, never install.
- **Overlap note**: `azd ai agent doctor` already exists (README: checks project managed-identity storage permissions).
  FD must not claim to replace it; decide in the overlap matrix whether to wrap or reference it.
- **Non-interactive contract for extension commands**: `cli/azd/extensions/ai-non-interactive.md` documents that every
  prompt has a flag or environment equivalent (for example `AZURE_SUBSCRIPTION_ID`, `AZURE_LOCATION`, `--force`,
  `--project-endpoint`).
- **Output**: no stable JSON schema published for `azd ai agent` commands. FD should read `azure.yaml` instead of parsing
  extension output. **Unverified**: any `--output json` support on individual `azd ai agent` subcommands.
- **Prerequisites**: Go binary per platform, no external runtime.
- **Missing-tool behaviour**: not installed is **not an error** for offline rules (they read `azure.yaml`). Report
  `extension azure.ai.agents not installed` as an informational "not detected" item. Installed but outside the
  verified-compatible range: warning plus rules downgraded to `confidence: likely`, never silent. Exit 2 only if the user
  explicitly asks for a check that needs the extension.

### 2.4 PSRule for Azure (and the PSRule module)

- **Versions**: PSRule.Rules.Azure stable **v1.47.0** (tag commit 2026-01-08); the pre-release line v1.48.0-B0228
  (2026-06-22) is ahead, and main HEAD is 2026-10-04, so the repo is actively maintained. PSRule engine stable **v2.9.0**
  (2023-06-08); v3.0.0 pre-releases exist (v3.0.0-B0783 2026-04-28) with no stable 3.0.0 tag. Both MIT.
  The PSRule for Azure repo's `modules.json` pins `PSRule` 2.9.0, so 2.9.0 is the **verified floor** for the engine.
  NuGet and PowerShell Gallery metadata could not be read (NuGet flat container returned no index; PSGallery not reached),
  so PSGallery publish dates are **Unverified**.
- **Minimum for FD**: PSRule for Azure 1.47.0 and PSRule 2.9.0 (proposed). Do not require pre-release builds.
- **Non-interactive invocation**: `Assert-PSRule -InputPath <path> -Module PSRule.Rules.Azure -Format File
  -OutputFormat Sarif -OutputPath out.sarif` (pattern from `docs/creating-your-pipeline.md`, which uses
  `-OutputFormat 'Sarif'`). Bicep expansion is enabled with the `AZURE_BICEP_FILE_EXPANSION: true` option in
  `ps-rule.yaml` and needs the Bicep CLI (`PSRULE_AZURE_BICEP_PATH` overrides discovery; minimum Bicep 0.4.451).
  Invoke through `pwsh -NoProfile -NonInteractive -Command ...`.
- **Output**: `-OutputFormat` supports None, Yaml, Json, Markdown, NUnit3 (read in `Assert-PSRule.md`) and Sarif (used in the
  pipeline doc). SARIF and JSON are the supported machine formats; the rule IDs (`Azure.*`) are the stable key. Rule
  names are mostly stable but the changelog shows deprecations (for example `Azure.ACR.GeoReplica`), so IDs must be
  mapped by version in the overlap matrix.
- **Prerequisites**: PowerShell 7.4 or newer on Windows, macOS, Linux (upstream statement; this project only supports Windows and has not tested the others); Windows PowerShell 5.1 with .NET Framework
  4.7.2+ also supported (`docs/install.md`). The module manifest sets `PowerShellVersion = '5.1'`. Needs the PSRule
  module installed (`Install-Module PSRule.Rules.Azure`). Bicep CLI for `.bicep` input.
- **Missing-tool behaviour**: `advanced.psrule.enabled: auto` (PRD config): if `pwsh` or the module is absent, emit an
  explicit "adapter unavailable: PSRule (reason)" entry in the report and in SARIF notifications, mark mapped checks
  skipped, continue native rules, exit 0/1 as usual. `enabled: true` with the tool missing: exit **2**. With `--strict`
  and any skipped check: exit **3** (PRD exit code table). Findings alone are exit 1; inability to run a
  required requested check is exit 2.

### 2.5 Checkov

- **Version**: 3.3.22, PyPI upload 2026-10-01T09:01; repo `bridgecrewio/checkov` HEAD 2026-10-01. Licence Apache-2.0.
  `python_requires >=3.9` (`setup.py`; PyPI metadata also `>=3.9`). Bicep parsing relies on `pycep-parser==0.5.1`.
- **Minimum**: no verified compatibility floor; pin an exact tested version in CI and require `>=3.3` (proposed).
- **Non-interactive invocation**: `checkov -d <dir> --framework arm bicep -o sarif --output-file-path <dir> --quiet
  --skip-download` (flags `--framework`, `-o/--output`, `--quiet`, `--skip-download` appear in
  `docs/2.Basics/CLI Command Reference.md`; use `--skip-download` to avoid platform contact; **Unverified**: whether it
  fully prevents all network calls). Frameworks `arm` and `bicep` are both listed.
- **Output**: `-o` accepts `cli, csv, cyclonedx, cyclonedx_json, spdx, json, junitxml, github_failed_only, gitlab_sast,
  sarif`; repeatable. JSON and SARIF are documented. Checkov JSON shape is widely used but has no formal schema:
  **semi-stable**. Check IDs `CKV_AZURE_*` are stable.
- **Exit codes**: 0 pass, 1 failed checks (`Hard and soft fail.md`); `--soft-fail` forces 0; exit 2 is a crash/integration
  failure (`--no-fail-on-crash` forces 0 instead). FD maps Checkov 1 to parsed findings (FD exit 1 if they meet the
  threshold). If explicitly requested Checkov cannot start or a required input/permission is unavailable, FD exits 2.
  If Checkov starts but violates the expected structured-output protocol, FD exits 4. It is never mapped to "no findings".
- **Prerequisites**: Python 3.9+ with pip (`pip install checkov==x.y.z`), or the Docker image. No .NET or PowerShell.
- **Missing-tool behaviour**: disabled by default (`advanced.checkov.enabled: false`). When enabled and absent: exit 2.
  When `auto` is added later: explicit "adapter unavailable" entry, never a silent skip.

### 2.6 KICS

- **Version**: `v2.2.0`, tag commit 2026-09-17; repo `Checkmarx/kics` HEAD 2026-10-01. Licence Apache-2.0.
- **Minimum**: v2.2.0 proposed (no verified floor). **Unverified**: query-set compatibility between minors.
- **Platform support**: `docs/platforms.md` states ARM templates (`.json`) and Bicep (`.bicep`) are scanned.
- **Non-interactive invocation**: `kics scan -p <path> --report-formats sarif,json -o <outdir> --no-progress --ci`
  (flags in `docs/commands.md`: `-p/--path`, `--report-formats` default `json` with `sarif` available, `-o/--output-path`
  directory, `--no-progress`, `--ci`, `--fail-on`, `--exclude-severities`, `--disable-secrets`).
- **Exit codes** (`docs/results.md`): 0 no results; 60 critical, 50 high, 40 medium, 30 low, 20 info; 70 remediation
  error, 126 engine error, 130 interrupt. FD must not treat 20-60 as adapter failure; treat 126/70 as adapter failure.
- **Output**: JSON and SARIF documented; JSON reports include a query ID per result. Treat as semi-stable.
- **Prerequisites**: single Go binary or container image; no runtime. Binary needs the query assets directory (`assets/queries`)
  alongside, or pass `-q`; **Unverified**: relocatability of the released binary without the assets folder.
- **Missing-tool behaviour**: same as Checkov (opt-in adapter; exit 2 if requested and missing).

### 2.7 Terrascan

See section 4. Short answer: **archived, do not integrate**.

### 2.8 Azure CLI

- **Version**: 2.90.0, PyPI upload 2026-09-01; repo `Azure/azure-cli` HEAD 2026-09-29; licence MIT; `python_requires >=3.10`
  (bundled Python in the MSI/deb/Homebrew packages).
- **Minimum**: none required. FD should use the Go SDK credential chain (azidentity, which can read azd and `az` login
  state) instead of shelling out. Optional uses: `az bicep ...` as a Bicep source fallback (installs a copy not on `PATH`),
  `az account show -o json` to detect login.
- **Non-interactive invocation**: `az <cmd> -o json --only-show-errors`; `AZURE_CORE_NO_COLOR=1`; never `az login` without
  a flag in automation (`az login` is interactive). `az version -o json` returns the CLI and core module versions
  (output shape from general knowledge; **Unverified** against the repo here).
- **Output**: `-o json` is the stable contract (documented across the CLI); `table`/`tsv` are for humans.
- **Missing-tool behaviour**: absent is informational unless the resolved configuration explicitly requires
  Azure CLI authentication or the Azure CLI Bicep fallback; then exit 2. No corresponding product CLI flag is committed yet.

### 2.9 Graphviz

- **Version**: 16.1.0, tag commit 2026-09-04 on `gitlab.com/graphviz/graphviz` (releases 15.1.1, 16.0.0, 16.1.0 observed).
  Licence **EPL-2.0** (LICENSE and COPYING files in the repo read as "Eclipse Public License - v 2.0").
  The github.com mirror `graphviz/graphviz` had no tags readable from here.
- **Minimum**: not required. The PRD lists `mermaid|json|html` graph outputs, not DOT, so Graphviz is optional and
  should only be used if a DOT export or SVG rendering is added. If added, floor is proposed at 2.x for `-Tsvg`; **Unverified**.
- **Invocation**: `dot -Tsvg in.dot -o out.svg`; `dot -V` prints the version to stderr. Exit non-zero on syntax errors.
- **Output**: DOT is a stable text language; SVG/PNG are render targets. Machine-readable layout via `-Tjson` exists
  (**Unverified**, not read here).
- **Prerequisites**: native binary and shared libraries from the OS package (apt, brew, choco). No runtime.
- **Missing-tool behaviour**: because FD output without rendering is still valid (Mermaid/JSON text), a missing `dot`
  is reported as "rendering skipped: graphviz not found" and the textual graph is still written. It is exit 2 only if the user
  requested an image format that needs it.

### 2.10 Mermaid

- **Versions**: `mermaid` 12.1.0 (npm, 2026-10-02T11:14Z), requires Node `>=22.12.0`; `@mermaid-js/mermaid-cli` 12.0.0
  (npm, 2026-09-24), requires Node `>=22.13.0`; both MIT.
- **Role in FD**: FD **emits** Mermaid text (`azd foundry graph --format mermaid`). That needs no tool at all. Rendering to
  SVG/PNG is a documented optional convenience: `mmdc -i input.mmd -o output.svg` (README). mermaid-cli drives a headless
  Chromium through Puppeteer, which is a heavy prerequisite (Chromium download, sandbox flags in CI); in containers it
  commonly needs a Puppeteer config, **Unverified** here.
- **Output stability**: Mermaid syntax changes across majors (v10 to v12 changed several diagram types). Restrict FD output
  to `flowchart` with plain IDs and quoted labels, and add a golden-file test for the syntax. GitHub renders Mermaid natively
  in Markdown, so no tool is needed for the primary use case.
- **Missing-tool behaviour**: not applicable to emission. If a future image-rendering option is explicitly requested
  without Node/mmdc, it must return exit 2; no `--render` flag is currently part of the command contract.

## 3. Missing-tool behaviour required by the PRD

PRD sources: section 1 ("Optional adapters report missing tools rather than silently skipping"), section 9 tool table
("missing required dependency returns exit code 2"), the exit code table (0 pass, 1 findings, 2 could not run because
input/dependency/authentication/permissions unavailable, 3 skipped with `--strict`, 4 internal or adapter protocol
failure), and Phase 1 ("Report skipped checks explicitly; never convert skipped to pass").

Uniform contract (ADR-001 and ADR-008):

| Situation | Behaviour | Exit code |
|---|---|---|
| Required dependency (Bicep when Bicep checks requested, or `validation.bicep: required`) missing or below minimum | Error naming tool, detected version, required version, install hint. No partial pass. | 2 |
| Optional adapter enabled `auto` and missing | Report entry `adapter-unavailable` with reason; dependent checks `skipped`; native rules still run. | per findings (0/1); 3 with `--strict` |
| Optional adapter explicitly enabled (`enabled: true` in configuration) and missing | Same as required. | 2 |
| Adapter present but crashes or emits unparseable output | Adapter protocol failure with captured stderr (redacted). | 4 |
| Adapter present and exits with its "findings found" code (Checkov 1, KICS 20-60) | Normal path; parse output. | per FD findings |
| Version below minimum | Treat as missing, with detected version in the message. | 2 (required) / skipped + 3 under `--strict` (optional) |
| Rendering tool missing (Graphviz, mmdc) | Textual graph still written; image skipped with explicit notice. | 2 only if image requested |

The report must carry a `tools` section (name, path, detected version, status `ok|missing|too-old|failed`, purpose) in
JSON and as SARIF `invocations[].toolExecutionNotifications`, so CI consumers can distinguish "passed" from "not run".

### Compatibility evidence required before an adapter is called supported

For each tool/version/platform tuple, retain a redacted fixture containing the exact command, detected version,
stdout/stderr channels, native exit code and structured output. Contract tests must demonstrate at least: no findings,
findings, malformed output, timeout/crash, missing tool and unsupported version. The adapter maps native rule IDs to
catalogue `FND-*` IDs through catalogue overlap metadata; prose mappings alone are provisional. Required executable
permissions are the ability to launch the local tool and read the selected project files. Azure-backed checks separately
list ARM/data-plane actions and report missing permission as unavailable/skipped rather than passed.

No such complete fixture set exists yet for PSRule, Checkov or KICS. The historical Bicep spike
covers a subset of the transport contract at 0.47.16; the current 0.48.1 tagged test passed locally and is configured
but has no successful run evidence. azd JSON discovery shapes remain unverified.

## 4. Terrascan assessment

- **Status**: archived and unmaintained. The README on `master` of `tenable/terrascan` opens with: "Archived. This project
  is no longer maintained. The repository is archived and no further updates, issues, or pull requests will be accepted."
  Last commit "Added archival messages (#1740)" dated 2025-11-20; last release `v1.19.9` dated 2024-09-18 (tag commit).
  The GitHub "archived" flag itself could not be read (`api.github.com` blocked); the README banner is the evidence.
- **Azure/ARM coverage**: the README lists "Scanning of Azure Resource Manager (ARM)" and Azure policies, but with no
  Bicep support and no maintenance, its Azure/Foundry/Cognitive Services content cannot be current (Foundry projects,
  `Microsoft.CognitiveServices/accounts/projects`, agent services all post-date its last release).
- **Recommendation**: do not build or document a Terrascan adapter. The current catalogue schema has no Terrascan
  mapping field, so exclusion is recorded here and in ADR-001 rather than hand-edited into the generated overlap matrix.
  The same adapter slot is better used for KICS or Checkov. If a user
  asks, support only through the generic SARIF-import adapter (`advanced.adapters`) with a warning.

## 5. Open items for Phase 1 spikes

1. Revalidate at the selected current Bicep version that `build --stdout --diagnostics-format sarif` writes SARIF to
   stderr and `lint --diagnostics-format sarif` writes it to stdout, as ADR-002 observed at 0.47.16; add a warning-only
   exit-code fixture.
2. Pick and justify the Bicep minimum from a CI version matrix (suggest oldest and newest supported).
3. Verify `azd extension list --installed --output json` shape and `azd version --output json`.
4. Verify PSRule invocation end to end on Windows with PowerShell 7.4 (not run here: no `pwsh` in this environment).
5. Run Checkov (`pip install checkov==3.3.22`) and KICS binary against a Foundry Bicep sample to confirm rule coverage
   before agreeing any adapter ID mapping (this document only verified flags and formats from documentation).

## 6. Verification update 2026-10-16 (R11)

Method: `git diff` between tag commits of the public Azure/azure-dev repository (blobless clone) plus the extension CHANGELOG.md files on `main`.
Nothing was run against a live azd. The diffs cover the schemas (`schemas/v1.0/azure.yaml.json` and every `azure.ai.*/schemas/`), the azd packages the
catalogue cites (`pkg/environment`, `pkg/pipeline`, `pkg/project`, `pkg/infra`), the cited design documents and the cited extension source files.
GitHub release pages and api.github.com were not read; dates are tag commit dates and CHANGELOG headings.

| Component | Previously verified | Newly verified | Commit | Date (tag commit / CHANGELOG) |
|---|---|---|---|---|
| azd | 1.35.0 (`170ebb8`); main 1.36.0-beta.1 (`afe4b2b`) | 1.35.1, 1.36.0 (`azure-dev-cli_1.36.0`) | `0651d77`, `acebba1` | 2026-10-07; commit 2026-10-15 (+08:00), CHANGELOG heading 2026-10-14 |
| azure.ai.agents | 1.0.0-beta.18 (`a64ca8f`) | beta.19, beta.20 | `8fe608d`, `5667b6a` | 2026-10-08; 2026-10-15 |
| azure.ai.projects | 1.0.0-beta.13 | beta.14 (requires azd >=1.36.0) | `5667b6a` | 2026-10-15 |
| azure.ai.connections | 1.0.0-beta.9 | beta.10 (build fix only) | `5667b6a` | 2026-10-15 |
| azure.ai.toolboxes | 1.0.0-beta.9 | beta.10 (camelCase), beta.11 (build fix) | `5667b6a` | 2026-10-08; 2026-10-15 |
| azure.ai.routines | 1.0.0-beta.8 | beta.9 (camelCase), beta.10 | `5667b6a` | 2026-10-08; 2026-10-15 |

Findings that matter to rules:

- `schemas/v1.0/azure.yaml.json` is byte-identical at `azure-dev-cli_1.35.0`, `azure-dev-cli_1.36.0` and `main` (empty `git diff` between the tags; file hashes of the 1.35.0 tag and `main` compared on 2026-10-16).
  The azure.ai.projects and azure.ai.connections schemas are unchanged between beta.13 / beta.9 and the new tags.
- azd 1.36.0: environment and state path APIs now return errors; invalid environment names, unsafe state paths and a default environment with an
  invalid name are rejected (`azdcontext`, `local_file_data_store.go`, `environment.go`). `azd pipeline config` can install required extensions and the
  generated pipeline templates gain an install step. Layout constants (`.azure`, `.env`, `config.json`) and the pipeline file names are unchanged.
- azure.ai.agents beta.19 renames the prompt-memory keys (`chat_model`, `embedding_model`, `update_delay`, `max_memories` and the options keys) and the GitHub
  Copilot toolset key `default_config` to camelCase and rejects the old spellings. The hosted session idle maximum rises from 3600 to 14400 seconds and the
  invocation moderation `textField` becomes a field name, not a JSONPath. beta.20 adds file-backed agent definitions with validation, resolves prompt-agent
  Connection references by service key or effective resource name, adds the `AGENT_<NAME>_PROMPT_ENDPOINT_VERSION` key, adds `metadata.tags`, and removes the
  generated developer role assignment (`principalId`, `principalType` parameters) from synthesized infrastructure.
- azure.ai.toolboxes beta.10 renames `instance_name`, `rai_config` and `rai_policy_name`. azure.ai.routines beta.9 renames `cron_expression`, `time_zone`,
  `agent_name` and related keys, and its schema describes trigger type `schedule`. azure.ai.evaluations beta.2 (in no catalogue window) uses camelCase keys in `azure.eval.yaml`.

Windows widened in the catalogue `compatibility` blocks on 2026-10-16:

- azd upper bound `1.36.0-beta.1` to `1.36.0` for every rule that had one, ENV-001 to ENV-004 included (see the ENV paragraph below).
- azure.ai.projects to beta.14: CFG-001, CFG-003, CFG-004, CFG-010, CFG-011, DEP-004, DEP-005, DEP-011, DEP-012, ENV-003.
- azure.ai.agents to beta.20: CFG-002, CFG-004, CFG-005, CFG-007, CFG-008, DEP-012, OPS-007, OPS-008, RUN-001, RUN-003, RUN-005, SEC-007, SEC-011.
- azure.ai.connections to beta.10: CFG-002, CFG-005, CFG-007. azure.ai.toolboxes to beta.11: CFG-002, CFG-007, CFG-008.

Left unchanged because a change touches the rule:

- CFG-001 and CFG-003: agents stays beta.18 (schema key renames; Connection reference resolution).
- CFG-006: agents and projects stay (new `AGENT_<NAME>_PROMPT_ENDPOINT_VERSION` key; provisioning inputs changed).
- CFG-009: routines stays beta.8 (cron key, trigger type and `time_zone` renamed).
- CFG-011: agents stays beta.18 (the hosted create path in `service_target_agent.go` changed; the unused-field behaviour was not re-verified).
ENV-001 to ENV-004 (azd widened to 1.36.0 on 2026-10-16, decided from `git diff azure-dev-cli_1.35.0 azure-dev-cli_1.36.0`, commits `170ebb8` and `acebba1`, in a blobless clone of https://github.com/Azure/azure-dev):

- Unchanged: `cli/azd/go.mod` has no godotenv change and `reloadLocked` still calls `godotenv.Read` on the env's `.env`; constants `.azure`, `.env`, `config.json`, `ConfigFileVersion` 1 and the `defaultEnvironment` field; `environment.go` has no change to any `AZURE_*` constant; `EnvironmentNameRegexp` is still `^[a-zA-Z0-9-\(\)_\.]{1,64}$` (now declared in `azdcontext/environment_name.go`); `pkg/project` resource group resolution (`resource_manager.go`, `service_manager.go`, `project_config.go`, `service_config.go`) and `pkg/infra/provisioning/bicep` have no diff.
- Changed in 1.36.0 (tightening only): `IsValidEnvironmentName` also rejects dot-only names, names that are not `filepath.IsLocal`, and, on Windows, names ending in a period; environment, `.azure` and state-file paths that are symbolic links, reparse points or outside the `.azure` tree are refused (`ErrUnsafeEnvironmentPath`) and `List` skips such entries; a `defaultEnvironment` with an invalid name is an error; a `config.json` that is not valid JSON is now an error instead of being silently replaced by an empty config; the path accessors return errors.
- Effect on the rules: Foundry Doctor's own reader (`internal/project`) already uses a stricter name pattern (first character alphanumeric, so dot-only names are skipped), skips symbolic links and ignores an invalid `defaultEnvironment` with a warning. The one gap: a name ending in a period (for example `dev.`) is accepted by Foundry Doctor on every OS but azd 1.36.0 rejects it on Windows; this only adds an environment azd would not load, and does not change any finding for valid names. Not verified at runtime (nothing was executed).
- ENV-003 was widened to azure.ai.projects beta.14, whose CHANGELOG says it requires azd >=1.36.0; its azd window now reaches 1.36.0, so the pair is consistent.

Rules whose catalogue `azd` field is empty carry no azd window and are not affected. `lastVerified` dates of existing sources were not changed by the window widening;
a source's date moves only when that source was re-read. The catalogue clock in `TestRealCatalogueInvariantsAndGeneratedDocs` is 2026-10-16.

Code constant: `internal/rules/cfg/versions.go` `testedMax` for `azure.ai.projects` is now `1.0.0-beta.14` (agents stays `1.0.0-beta.18`). It mirrors the
FND-CFG-001 and FND-CFG-011 windows and `TestTestedMaxMatchesCatalogueWindows` fails if they drift.

Not verified: the GitHub release pages for azd 1.36.0 and the extensions; runtime behaviour of azd 1.36.0 or the extensions (nothing was executed);
`MicrosoftDocs/azure-ai-docs`, which returned 404 for every path on raw.githubusercontent.com from this environment, so Learn page content was not re-read
for this update.

Catalogue maintenance follow-up (2026-10-16):

- The compatibility notes that still said the azd maximum was the clone read (1.36.0-beta.1) now state the real window: the maximum is azd 1.36.0 (`azure-dev-cli_1.36.0`,
  `acebba1e388b489a91ab8e2487a676359ab516a1`, released 2026-10-14) and the beta.1 clone is earlier evidence. The windows that really stay narrower are unchanged and
  described above (CFG-001 and CFG-003 agents beta.18, CFG-006, CFG-009 routines beta.8, CFG-011 agents beta.18). The "azd 1.36.0-beta.1 clone checked" remarks in the
  NET and IDN notes are statements about what was read and were left as written.
- Every `sources[].verifiedVia` value now starts with a URL. URLs in the form `https://raw.githubusercontent.com/<org>/<repo>/<ref>/<path>` and the equivalent `blob`
  URLs were requested on 2026-10-16 and returned HTTP 200 (status code only for most; content was read for blang/semver v4.0.0 `range.go`, Masterminds/semver v3.4.0
  `constraints.go`, and azd `project.go` at commit `1bd2ca4699c7b1f371db85dd867f18aa4fbc4730`). `learn.microsoft.com` and `openid.net` are blocked by the egress
  proxy and were not fetched. Branch (`main`) URLs are labelled as not commit-pinned.
- API versions: `Microsoft.CognitiveServices` stable `2026-09-01` and preview `2026-07-15-preview` exist in Azure/azure-rest-api-specs `main`
  (`specification/cognitiveservices/.../CognitiveServices/{stable,preview}/<version>/cognitiveservices.json`; `info.version` read from the file, and the paths for accounts,
  projects, deployments, capabilityHosts, connections, usages, modelCapacities, deletedAccounts and models are present). `Microsoft.Insights` stable `metricAlerts` `2026-01-01`
  (`specification/monitor/.../Insights/stable/2026-01-01/metricAlert.json`), `activityLogAlerts` `2026-01-01` and `scheduledQueryRules` `2026-03-01` exist in the same repository.
  This confirms the version labels in the specification repository, not their availability in every Azure cloud or region, and Learn's template reference was not read.

## azure.yaml `requiredVersions.azd` (2026-10-15, updated 2026-10-16)

The vendored azd schema states that a project fails to load when the running azd is outside the range, and gives `>= 0.6.0-beta.3` as its only example. The grammar is
not documented there. It was read from the azd source and the two semver libraries and is recorded in `docs/spikes/azd-required-versions.md`: `requiredVersions.azd`
uses `blang/semver/v4` v4.0.0 `ParseRange`, `requiredVersions.extensions.<id>` uses `Masterminds/semver/v3` v3.4.0 `NewConstraint`.

FND-CFG-001 now evaluates the range (ADR-004 Amendment 2, update 2026-10-16): an invalid range and a known azd version outside the range are error findings. The
hand-written parsers (`internal/rules/cfg/versions.go`) are checked against the real libraries by a differential test over a committed fixture
(`internal/rules/cfg/testdata/semver_diff.json`, generated once from both libraries in a scratch module; the libraries are not dependencies). Forms on which the
differential run disagreed with the first parsers are reported as uncertain, not as errors: one-character tokens (blang drops them, so `*` and the hyphen in
`1.0.0 - 2.0.0` do not behave as ranges), `x` outside the exact `N.x` and `N.M.x` forms, `!` followed by a space, and a few Masterminds wildcard and zero-version forms.
The dev-build exemption (`0.0.0-dev.0`) covers the range comparison only, and only inside the CFG-001 azd window; an invalid range is still an error for a dev
build because azd runs `ParseRange` (`project.go:64`) before the `IsDevVersion` skip (`project.go:70`).

Further forms are uncertain, not errors (2026-10-16, Foundry lead re-review N4-1): a dangling operator (`>=1.0.0 <`, `>1.0.0 <`), an operator, a space and a non-version (`> = 1.0.0`), a space-separated version
the library parses in ways not proven (`! 1.35.0`), and repeated `||` (`>=1.0.0 || || <2.0.0`; `ParseRange` accepts it and the returned range function panics when called). The parser
returns "invalid" only for a failure the differential fixture proves: the empty string, a version-shaped token that is not a strict semver, an unsupported operator, a comma, and a single
leading or trailing `||`. A test asserts that every string the parser calls invalid is also rejected by `ParseRange`.

Not verified: azd itself was not executed, and older azd releases may use another range parser.
