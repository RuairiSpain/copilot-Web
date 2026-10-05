# Licence and dependency inventory (Phase 0)

Verified 2026-10-04. Method: `go list -m -json <mod>@latest` and `go mod download -json` through the Go proxy
(version, release time, `go` directive, LICENSE file text read from the module zip), `git fetch` of upstream HEAD
(last commit date), and LICENSE files read from the clones in `REFS`. SPDX identifiers are my reading of the
licence text, not an automated scan (no `go-licenses`/`scancode` run), so confirm with a scanner in CI before release.
`api.github.com` was blocked, so archived/deprecated status comes from README banners, not the GitHub flag.
**Unverified** marks facts I could not confirm.

Project context: `foundry-doctor/go.mod` currently declares `go 1.24` and requires only `gopkg.in/yaml.v3 v3.0.1`;
the local toolchain is go1.24.7. FD is a Go binary that will ship under the repo's own licence (not yet decided here)
and may be redistributed as an azd extension binary, so only permissive licences are acceptable.

## 0. Critical finding: the Go language version

Several current dependency versions require a newer `go` directive than the repo has. With `GOTOOLCHAIN=auto` the
build silently downloads a newer toolchain, which is blocked in this sandbox and undesirable for reproducible CI.

| Module | Latest | `go` directive of latest | Last version that fits `go 1.24` (verified) |
|---|---|---|---|
| azidentity | v1.14.1 (2026-08-27) | 1.25.0 | v1.13.1 (2025-11-10, go 1.23.0) |
| azcore | v1.23.2 (2026-09-28) | 1.25.0 | v1.20.0 (go 1.23.0) |
| armresourcegraph | v0.10.0 (2026-05-26) | 1.25.0 | v0.9.0 (2023-11-23, go 1.18) |
| golang.org/x/sync | v0.23.0 (2026-08-31) | 1.26.0 | v0.19.0 (2025-12-04, go 1.24.0) |
| github.com/Azure/azure-dev/cli/azd (azdext) | v1.35.0 (2026-09-30) | **1.26.4** | none checked |
| armpolicy | v1.0.0 (2026-03-19) | 1.24.0 | fits |
| armresources/v2 | v2.1.0 (2025-05-21) | 1.23.0 | fits |
| go-sarif/v3 | v3.3.1 (2026-07-29) | 1.24 | fits |

Consequences and recommendation (needs an ADR):

1. The azd extension SDK (`pkg/azdext`) lives inside the module `github.com/azure/azure-dev/cli/azd`, so any importer
   inherits `go 1.26.4` and its 152 requirements (92 direct, 60 indirect) (`go.mod` at tag `cli/azd/v1.35.0`, includes gRPC, mcp-go,
   many `arm*` packages). The azd team's own extensions are separate modules with their own `go.mod` at `go 1.26.4`
   (for example `azure.ai.agents`, which requires `github.com/azure/azure-dev/cli/azd v1.34.2`).
2. Proposed structure: keep the core (`internal/...`, standalone binary) free of `azdext`, and put the azd extension
   host in a separate small module (or build-tagged package) so the core can stay on the lowest workable Go version, while
   the extension module follows azd's Go version. Alternatively raise the whole repo to `go 1.26.x` and accept it.
   Decide before Phase 1; do not let `go get` raise the directive implicitly.
3. If the repo stays on 1.24, pin the table's right-hand column versions for Azure SDK packages and `x/sync`, and add
   `GOTOOLCHAIN=local` to CI to catch accidental bumps.

## 1. Go dependencies

| Module (planned use) | Latest verified | Last release / HEAD | SPDX | Maintenance | Direct requires of latest |
|---|---|---|---|---|---|
| github.com/spf13/cobra (CLI commands) | v1.10.2 | 2025-12-03 / 2026-07-10 | Apache-2.0 | Active | pflag, mousetrap, go-md2man, go.yaml.in/yaml/v3 |
| github.com/spf13/viper (config) | v1.21.0 | 2025-09-08 / 2025-10-15 | MIT | Slow (no commits for ~11 months) | fsnotify, go-viper/mapstructure/v2, go-toml/v2, locafero, afero, cast, pflag, gotenv, yaml |
| github.com/knadh/koanf/v2 (viper alternative) | v2.3.7 | 2026-09-24 / 2026-09-24 | MIT | Active | not enumerated (**Unverified**) |
| gopkg.in/yaml.v3 (current dep) | v3.0.1 | 2022-05-27 / 2025-04-01 | MIT AND Apache-2.0 | **Unmaintained** (README: "THIS PROJECT IS UNMAINTAINED") | none |
| go.yaml.in/yaml/v3 (maintained fork) | v3.0.5 | 2026-07-26 / 2026-09-30 | MIT AND Apache-2.0 | Active (yaml org) | none |
| github.com/goccy/go-yaml (AST, positions) | v1.19.2 | 2026-01-08 / 2026-04-07 | MIT | Moderate | not enumerated (**Unverified**) |
| azidentity (auth) | v1.14.1 | 2026-08-27 / SDK repo 2026-10-02 | MIT | Active (Microsoft) | azcore, azidentity/cache, MSAL for Go, golang-jwt/jwt/v5, uuid, x/crypto |
| azcore (pipeline) | v1.23.2 | 2026-09-28 | MIT | Active | via azidentity |
| armresources | v1.2.0 (v1 line, 2023-11-23); **v2.1.0 current** (2025-05-21) | SDK repo 2026-10-02 | MIT | Active repo; use v2 | azcore |
| armresourcegraph | v0.10.0 (pre-1.0) | 2026-05-26 | MIT | Active but pre-1.0 | azcore |
| armpolicy | v1.0.0 | 2026-03-19 | MIT | Active | azcore |
| github.com/Azure/azure-dev/cli/azd (azdext) | v1.35.0 | 2026-09-30 / 2026-10-04 | MIT | Active | 152 requires (92 direct) (**not audited transitively**) |
| golang.org/x/sync/errgroup | v0.23.0 | 2026-08-31 / 2026-09-23 | BSD-3-Clause | Active (Go team) | none significant |
| github.com/owenrumney/go-sarif/v3 | v3.3.1 | 2026-07-29 / 2026-08-31 | Unlicense (public domain dedication) | Active, single maintainer | testify, gojsonschema (licence **Unverified**), uuid |
| github.com/stretchr/testify | v1.12.1 | 2026-08-17 / 2026-09-24 | MIT | Active | objx, go.yaml.in/yaml/v3 |
| github.com/google/go-cmp | v0.7.0 | 2025-01-14 | BSD-3-Clause | Stable, low churn | none |

SPDX evidence: LICENSE text read from the module zip or clone for cobra (Apache License 2.0), viper (The MIT License),
koanf (The MIT License), yaml.v3 and go.yaml.in/yaml/v3 (header: "covered by two different licenses: MIT and Apache"; the
libyaml-derived files are MIT, the rest Apache-2.0), goccy/go-yaml (MIT), armresources/armpolicy (MIT, Microsoft),
azidentity and armresourcegraph (MIT from the repo's per-module LICENSE.txt), azure-dev (MIT), x/sync ("Copyright 2009 The
Go Authors", BSD-3-Clause text), go-sarif v3 ("free and unencumbered software released into the public domain" =
Unlicense), testify (MIT), go-cmp ("Copyright (c) 2017 The Go Authors", BSD-3-Clause).

### 1.1 Per-module notes

#### cobra (Apache-2.0) - recommended
- Purpose: command tree, flags, shell completion. Required by PRD (`cmd/foundry` Cobra registration); azd extensions use it.
- Alternatives: `urfave/cli`, stdlib `flag`. Not needed.
- Security: none open known to me; **Unverified** (`vuln.go.dev` blocked, so `govulncheck` could not be run here).
  Run `govulncheck` in CI when network allows.
- Redistribution: Apache-2.0 requires including the licence text and NOTICE (if any) and stating changes. Add to `THIRD_PARTY_NOTICES`.
- Note: cobra's own docs generator pulls `go.yaml.in/yaml/v3`, so that fork is already in the module graph.

#### viper (MIT) - not recommended
- Purpose: layered config (file/env/flags). PRD defines its own precedence (profile, `foundry-doctor.yaml`, flags,
  escape hatch) and needs source-location-aware error messages, which viper hides behind `mapstructure`.
- Concerns: ten direct dependencies, case-insensitive keys, `.`-delimited key coercion, no commits since Oct 2025.
- Alternatives: koanf v2 (MIT, active, modular), or no library (parse with the YAML library + typed struct + explicit merge).
  Recommend the no-library approach; it keeps config validation testable and reduces the supply-chain surface.

#### gopkg.in/yaml.v3 vs go.yaml.in/yaml/v3 vs goccy/go-yaml
- **yaml.v3 v3.0.1**: README states the project is unmaintained; last commit to the `v3` branch 2025-04-01. Fine for
  stable parsing but no future fixes. `yaml.Node` exposes `Line`, `Column`, `HeadComment`, `LineComment`, `FootComment`
  (read in `yaml.go`), and map decode reports `mapping key ... already defined at line N` (read in `decode.go`),
  but that check applies when decoding into Go maps/structs, not when decoding into `yaml.Node`; **Unverified**
  that `yaml.Node` decoding rejects duplicates, so FD must walk `MappingNode` content itself for FND-CFG-001.
- **go.yaml.in/yaml/v3 v3.0.5**: the maintained continuation under the YAML organisation (HEAD 2026-09-30), same API and
  licences, used by cobra, viper, testify and azd. Lowest-risk drop-in replacement for `gopkg.in/yaml.v3` (change import path).
  I did not diff the two APIs, but cobra/testify/azd successfully use it as a replacement, so compatibility is likely.
- **goccy/go-yaml v1.19.2 (verified by executing a test against the module)**:
  - `parser.ParseBytes(src, parser.ParseComments)` returns `*ast.File` with `Docs[i].Body` nodes.
  - Every node has `GetToken()`; `token.Token.Position` has `Line`, `Column`, `Offset`, `IndentNum`, `IndentLevel`.
    Result for `name: a` on line 2: key at line 2 col 1, value at line 2 col 7.
  - Comments are attached: `GetComment()` returned the head comment `# top` for the first mapping value.
  - Duplicate keys are rejected by default at parse time: `[2:1] mapping key "a" already defined at [1:1]`;
    `parser.AllowDuplicateMapKey()` is the opt-in to allow them. This directly serves FND-CFG-001.
  - Round-trip caveat: printing the AST normalised `name: a  # trailing` to `name: a # trailing` (whitespace before an inline
    comment collapsed). So the AST is positional and comment-aware, but not byte-for-byte lossless. For a read-only doctor
    this is acceptable as long as source locations come from tokens and the original bytes stay the source of truth.
  - Verified `go 1.21.0` directive; last tag 2026-01-08, last commit 2026-04-07 (three months before the as-of date).
  - Risk: single primary maintainer; YAML 1.2 edge cases (anchors/merge keys `<<`) behave differently from yaml.v3 and need
    a fixture suite. **Unverified**: fuzz results and large-file performance.
- Recommendation: use goccy/go-yaml for `internal/azureyaml` (positions, comments, duplicate keys), with
  `go.yaml.in/yaml/v3` for simple struct decoding elsewhere (or only goccy to keep one parser). Replace `gopkg.in/yaml.v3`
  in `go.mod` (used today by `internal/catalog`). Decision record needed.

#### Azure SDK for Go: azidentity, armresources, armresourcegraph, armpolicy (MIT)
- Purpose: Phase 2+ Azure authentication (`DefaultAzureCredential`/azd credential chain), ARM inventory, Resource Graph
  correlation, policy assignment reads. The PRD says "Azure SDK for Go: primary typed access ... use Azure credential chain;
  no stored secrets".
- Use `armresources/v2` (v2.1.0), not the v1 line shown by older code (v1.2.0 dates from 2023-11-23 and is what some azd
  extensions still pin). Both are MIT.
- `armresourcegraph` is v0.x: expect breaking API changes; wrap it behind an internal interface (`internal/azure/inventory`)
  so a REST fallback via `azcore` is possible.
- `armpolicy` v1.0.0 exists since 2026-03-19; earlier versions were v0.x, so older docs and samples may be stale.
- Security: azidentity pulls MSAL for Go and `golang-jwt/jwt/v5`; keep updated. Tokens must never be logged (PRD: no secrets
  in output). **Unverified**: current advisories (govulncheck blocked).
- Alternatives: raw REST via `azcore/arm/runtime` for single endpoints (reduces binary size); Azure CLI shell-out (rejected:
  needs Python and breaks the "no hidden dependencies" principle).
- Redistribution: MIT; keep copyright notice in notices file.
- Go version: see section 0.

#### azd extension SDK (`github.com/azure/azure-dev/cli/azd/pkg/azdext`, MIT)
- Purpose: gRPC client to the azd host (project, environment, prompts, events), extension command scaffolding, MCP helpers.
  106 files in `pkg/azdext` at tag `cli/azd/v1.35.0`; stable contracts in `pkg/azdext/contracts/v1`, beta in a `preview`
  directory and `v1beta` channel (`docs/extensions/contract-versioning.md`).
- It is not a standalone module: the whole azd module (`go 1.26.4`, 152 requirements (92 direct, 60 indirect)) comes with it. The extension's
  own go.mod must be able to build on that Go version. See section 0.
- Compatibility: the importer declares `requiredAzdVersion` in `extension.yaml`; `azure.ai.agents` shows the pattern
  (`>=1.34.2`).
- Alternative: no SDK; the extension binary could shell out to `azd env get-values --output json` and `azd` flags. This loses
  the lifecycle events and `service-target-provider` capability but removes the 152-requirement dependency. Keep core logic
  library-independent (PRD Phase 1 says exactly this).
- Redistribution: MIT; the binary embeds many transitive licences, so a generated NOTICE (`go-licenses report`) is required.
  Transitive licence audit not done here (**Unverified**).

#### golang.org/x/sync/errgroup (BSD-3-Clause)
- Purpose: bounded parallel execution of rules/adapters and Azure reads (`SetLimit`).
- Alternative: stdlib `sync.WaitGroup` plus semaphore channel. errgroup is tiny and maintained by the Go team; use it.
- Version: pin v0.19.0 if staying on Go 1.24, else v0.23.0 (needs Go 1.26.0).
- Redistribution: BSD-3-Clause, keep copyright notice and disclaimer in notices.

#### go-sarif (Unlicense)
- Purpose: build SARIF 2.1.0 output for GitHub Code Scanning. v3 is the current major (v2.3.3 was last released 2024-07-10).
- Concerns: single maintainer; pulls `gojsonschema` (licence not verified here), `uuid`, testify. SARIF is a fixed OASIS
  schema and FD needs a small subset (runs, tool.driver.rules, results, locations with regions, partialFingerprints,
  invocations/notifications).
- Alternative: a 200-line internal SARIF writer with schema validation in tests against the OASIS 2.1.0 JSON schema.
  Recommended given FD wants one canonical location model (PRD: SARIF regions share the location model). If the library
  is used, restrict it to `internal/report/sarif` behind an interface.
- Redistribution: Unlicense, no obligations (still list it in notices for completeness).

#### testify (MIT) vs stdlib testing
- Existing tests in this repo (`cmd/rulecatalog`, `internal/catalog`) use the standard library only (table tests with
  `t.Run`), which keeps `go.mod` minimal. testify adds `objx` and `go.yaml.in/yaml/v3` to the graph and encourages
  assertion-style tests whose failures hide the diff.
- Recommendation: stdlib `testing` plus `github.com/google/go-cmp/cmp` for structural diffs; golden files for reports
  (PRD: deterministic reports). Allow testify only if the team prefers it, in `_test.go` files so it never ships in the binary
  (test-only dependencies do not affect the redistributed artefact).

## 2. External tools (not linked, invoked as processes)

These are not redistributed by FD unless noted. Any licence obligations arise only if FD bundles a binary or container
image; the default plan is **detect, never bundle**.

| Tool | Purpose | SPDX | Last stable / HEAD | Maintenance | Alternatives | Security and redistribution notes |
|---|---|---|---|---|---|---|
| azd | Host CLI, extension framework | MIT | 1.35.0 (2026-09-30) / 2026-10-04 | Active (Microsoft) | Standalone FD binary | Extension binary is distributed via an azd registry; azd auth cache under `~/.azd` holds tokens: FD must not read cache files directly. |
| Bicep CLI | Compile Bicep to ARM, lint | MIT | v0.48.1 (2026-09-29) / 2026-10-02 | Active (Microsoft) | `az bicep`; embedded route (not proven; ADR-002) | Release binary embeds third-party code; includes embedded NOTICE/LICENSE (csproj `AddNoticeAsEmbeddedResource`). Module restore contacts registries; use `--no-restore` offline. |
| azd AI agent extension (`azure.ai.agents`) | Foundry agent lifecycle, `doctor` | MIT (azure-dev repo) | 1.0.0-beta.18 (2026-09-30) | Active, beta | none | Not redistributed. Treat as untrusted input source. Beta with breaking changes. |
| PSRule for Azure | ARM/Bicep rule engine, WAF-aligned rules | MIT | v1.47.0 (2026-01-08) / 2026-10-04 | Active | Checkov, KICS, Azure Policy | Maintained by Microsoft employee-led OSS project (community supported, no SLA: `SUPPORT.md`; **Unverified** wording). `ThirdPartyNotices.txt` ships with it. Do not copy implementation code (brief rule). |
| PSRule | Engine for PSRule for Azure | MIT | v2.9.0 (2023-06-08); v3.0.0 pre-release 2026-04-28 / HEAD 2026-09-12 | Active, long gap between stable releases | none | Runs PowerShell code from rule modules: execute only with trusted modules. |
| Checkov | Policy-as-code scanner (ARM, Bicep) | Apache-2.0 | 3.3.22 (2026-10-01) | Very active (Prisma Cloud / Palo Alto Networks) | KICS, PSRule | Python; large transitive tree; may contact the vendor platform unless `--skip-download` is used and no API key is set. Apache-2.0: if bundled, include licence and NOTICE. |
| KICS | Policy scanner (ARM, Bicep) | Apache-2.0 | v2.2.0 (2026-09-17) / 2026-10-01 | Very active (Checkmarx) | Checkov, PSRule | Go binary plus query assets; container image licence is separate. Redistribution of assets (queries) is Apache-2.0 per repo LICENSE. |
| Terrascan | Policy scanner | Apache-2.0 (README badge; LICENSE file text not separately read) | v1.19.9 (2024-09-18) / 2025-11-20 | **Archived** | KICS, Checkov | Do not use. No security fixes will land. |
| Azure CLI | Optional login/Bicep fallback | MIT | 2.90.0 (2026-09-01) | Active (Microsoft) | azidentity | Large Python distribution; no bundling. |
| Graphviz | Optional DOT rendering | EPL-2.0 | 16.1.0 (2026-09-04) | Active | Mermaid, D2 | EPL-2.0 is weak copyleft: shelling out and not linking is fine; bundling the binary needs source availability for modified EPL files. Keep it unbundled. |
| Mermaid | Optional diagram text/rendering | MIT | mermaid 12.1.0 (2026-10-02), mermaid-cli 12.0.0 (2026-09-24) | Very active | Graphviz | FD only emits text. mermaid-cli pulls Puppeteer and a Chromium download (Apache-2.0 / BSD mix; **Unverified**). Rendering untrusted labels can enable XSS in HTML output: use `securityLevel: strict` when embedding in the HTML report. |

Evidence for tool SPDX: LICENSE files read for azure-dev ("MIT License"), PSRule.Rules.Azure and PSRule ("MIT License,
Copyright (c) Microsoft Corporation"), checkov and kics ("Apache License Version 2.0"), graphviz (gitlab LICENSE file:
"Eclipse Public License - v 2.0"), mermaid and mermaid-cli (npm `license` field MIT), azure-cli (PyPI metadata MIT),
checkov (PyPI "Apache License 2.0"), Bicep (source headers: "Licensed under the MIT License"; the repo LICENSE file itself
was not opened, **Unverified** beyond that header).

## 3. Obligations summary for the FD distribution

1. Produce a generated third-party notices file in release builds (Apache-2.0, BSD-3-Clause and MIT all require keeping
   notices). Use `go-licenses` or `go mod vendor` plus a script; not run here.
2. Do not bundle external tools in the default distribution. If a container image bundles Bicep, KICS or Checkov,
   record versions and include their licence and NOTICE texts; Graphviz (EPL-2.0) needs the EPL notice and source offer.
3. No copyleft (GPL/AGPL/LGPL) dependency was found in the Go candidates reviewed. Transitive modules of azdext, viper,
   goccy/go-yaml and go-sarif were not enumerated, so this is **Unverified** for the full graph.
4. Add `govulncheck` to the verification script and treat unreachable-vulnerability findings as informational; it could
   not be run in this environment because `vuln.go.dev` is blocked.

## 4. Recommendations (for the Go architect to accept or override)

| Decision | Recommendation |
|---|---|
| CLI | cobra v1.10.2 |
| Config | No viper; typed structs + explicit precedence merge (koanf only if layering code grows) |
| YAML | goccy/go-yaml for `internal/azureyaml`; migrate remaining `gopkg.in/yaml.v3` use to `go.yaml.in/yaml/v3` |
| Azure SDK | azidentity, `armresources/v2`, armresourcegraph (behind interface), armpolicy v1; pin to the Go 1.24-compatible versions or raise Go |
| azd SDK | Separate extension module, never imported by core packages |
| Concurrency | `golang.org/x/sync/errgroup` |
| SARIF | Internal minimal writer validated against the SARIF 2.1.0 schema; go-sarif v3 acceptable fallback |
| Tests | stdlib `testing` + go-cmp + golden files; no testify requirement |
| Adapters | PSRule and Checkov and KICS optional; Terrascan dropped |
