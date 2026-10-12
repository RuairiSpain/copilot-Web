# Foundry Doctor licence and dependency inventory

Audited 2026-10-05 from `go.mod`, `go.sum`, the complete output of
`go list -m -json all`, and the licence files in checksum-verified module
archives. CI reproduces the audit with:

```text
python scripts/dependency_audit.py --check-inventory
```

The command is equally usable from PowerShell, Command Prompt, Bash, and CI.
It fails closed for an unversioned module, a missing or unknown licence, a
module not represented below, malformed module/SPDX metadata, ambiguous
licensing, an unapproved SPDX exception, or a stale version/licence marker.
Every root licence file is evaluated. GPL, AGPL, or LGPL identifiers or text
are rejected before approved grants are considered, so mixed MIT/prohibited
material cannot be misclassified as MIT.

## Exact selected Go module graph

The main module is
`github.com/ruairispain/copilot-web/foundry-doctor` (`go 1.25.12`). It is
project code rather than a third-party dependency and is not listed as a
redistributed dependency.

| Module | Selected version | Relationship | Licence | Evidence |
|---|---:|---|---|---|
| `go.yaml.in/yaml/v3` | `v3.0.5` | direct; no transitive modules | MIT AND Apache-2.0 | Root `LICENSE`: project is split between MIT-licensed libyaml-derived files and Apache-2.0 files |
| `github.com/cpuguy83/go-md2man/v2` | `v2.0.6` | module graph only (cobra doc generator); not linked into the binary | MIT | Root `LICENSE.md`: MIT License |
| `github.com/inconshreveable/mousetrap` | `v1.1.0` | indirect via `github.com/spf13/cobra`; Windows-only | Apache-2.0 | Root `LICENSE`: Apache License 2.0 |
| `github.com/spf13/cobra` | `v1.9.1` | direct | Apache-2.0 | Root `LICENSE.txt`: Apache License 2.0 |
| `github.com/spf13/pflag` | `v1.0.6` | indirect via `github.com/spf13/cobra` | BSD-3-Clause | Root `LICENSE`: BSD 3-Clause |
| `github.com/russross/blackfriday/v2` | `v2.1.0` | module graph only (via go-md2man); not linked into the binary | BSD-2-Clause | Root `LICENSE.txt`: Simplified BSD License |
| `gopkg.in/check.v1` | `v0.0.0-20161208181325-20d25e280405` | module graph only (test dependency of yaml.v3); not linked into the binary | BSD-2-Clause | Root `LICENSE`: BSD 2-Clause |
| `gopkg.in/yaml.v3` | `v3.0.1` | module graph only (via cobra); not linked into the binary | MIT AND Apache-2.0 | Root `LICENSE`: project is split between MIT-licensed libyaml-derived files and Apache-2.0 files |

<!-- dependency-audit: module=github.com/cpuguy83/go-md2man/v2 version=v2.0.6 licence=MIT -->
<!-- dependency-audit: module=github.com/russross/blackfriday/v2 version=v2.1.0 licence=BSD-2-Clause -->
<!-- dependency-audit: module=gopkg.in/check.v1 version=v0.0.0-20161208181325-20d25e280405 licence=BSD-2-Clause -->
<!-- dependency-audit: module=gopkg.in/yaml.v3 version=v3.0.1 licence=MIT AND Apache-2.0 -->
<!-- dependency-audit: module=go.yaml.in/yaml/v3 version=v3.0.5 licence=MIT AND Apache-2.0 -->
<!-- dependency-audit: module=github.com/inconshreveable/mousetrap version=v1.1.0 licence=Apache-2.0 -->
<!-- dependency-audit: module=github.com/spf13/cobra version=v1.9.1 licence=Apache-2.0 -->
<!-- dependency-audit: module=github.com/spf13/pflag version=v1.0.6 licence=BSD-3-Clause -->

`go.sum` contains exactly the module and `go.mod` hashes for this selected
version. There are currently no indirect or test-only Go modules. Any graph
change therefore makes the CI inventory check fail until it is reviewed and
recorded here.

## CI and development tools

Tools are installed for analysis and are not linked into or redistributed
with Foundry Doctor.

| Tool/input | Pinned state in CI | Licence / role |
|---|---|---|
| Go | `go.mod` directive `1.25.12`, `GOTOOLCHAIN=local` | Toolchain and standard library; BSD-3-Clause |
| `govulncheck` | `golang.org/x/vuln/cmd/govulncheck@v1.7.0` | BSD-3-Clause; strict reachable-code vulnerability gate; Go 1.25 compatible |
| Staticcheck | `honnef.co/go/tools/cmd/staticcheck@v0.6.1` | MIT; static analysis; Go 1.25 compatible |
| Bicep CLI | `v0.48.1` | MIT; tagged offline spike only, not shipped |
| Azure Developer CLI schema/source monitor | `afe4b2b4d262bab4c11f4937e7a7942557ab0ecd` | MIT; exact commit used for local schema resolution and the ADR-003 namespace collision monitor |
| Python schema environment | PyYAML `6.0.3`; jsonschema `4.25.1`; referencing `0.36.2`; attrs `25.3.0`; jsonschema-specifications `2025.4.1`; rpds-py `0.27.1` | Pinned, CI-only schema spike dependencies; not shipped |

Release artefacts must not bundle these external tools. If the product graph
later embeds additional modules, this inventory and generated third-party
notices must be updated before the strict licence gate can pass.

`THIRD_PARTY_NOTICES.md` is distributable notice material generated from every
root licence/notice file in the checksum-verified selected module graph. The
inventory audit compares it byte-for-byte and fails if it is missing or stale.
The same audit also verifies that this inventory, the Foundry Doctor CI
workflow, and `scripts/install-dev-tools.sh` retain the approved tool pins.

## Policy

- Allowed linked dependency licences are currently MIT, Apache-2.0,
  BSD-2-Clause, BSD-3-Clause, ISC, and combinations of those licences.
- GPL, AGPL, LGPL, unknown, missing, and custom licences require explicit
  legal review and are denied by the automated gate.
- GitHub dependency review checks pull-request deltas at moderate severity or
  higher and applies the denied-licence policy with read-only permissions.
- `govulncheck` in CI has no database-unavailable or skipped-success path.
  Local `verify-phase.sh` may report unavailable tooling, but strict CI invokes
  the scanner directly and fails on every non-zero result.
