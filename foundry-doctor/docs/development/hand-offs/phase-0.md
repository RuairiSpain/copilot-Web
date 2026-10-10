# Phase 0 hand-off

Branch `claude/foundry-doctor-phase-0`, stacked on `claude/foundry-doctor-harness` (PR #15, not yet merged).
Date: 2026-10-05. Reviewers are independent agents that did not author the work they reviewed.
Checked-out base revision for the final documentation review:
`21745cc8e97e431b71d6fc5da84cd08625debf3f`. Local validation covered the working tree, including
changes not represented by that base SHA; the SHA is not a claim that the resulting documentation
was committed.

## Current status: reopened

This file preserves the evidence reported by the original Phase 0 work, but Phase 0 was reopened on
2026-10-05 for ADR-008 remediation. Labels such as "PASS", "Done", "Approved" and "Met" in the
historical-review sections describe that dated run/review only. A later local review ran the current
non-tagged validation commands and tagged spikes listed below. It did not produce a race result or a
successful CI result, so Phase 0 remains open.

Open completion gates are:

- no authoritative repository assets were found for the PRD's 59 XF-origin checks;
- PSRule, Checkov and KICS do not yet have the required pinned structured-output contract fixtures;
- the build-tagged azure.yaml and Bicep spikes passed locally, but the hardened Azure-dev test now
  requires Linux network-namespace isolation and no successful tagged CI run is evidenced;
- synthetic provider ARM templates have not been compiled or enumerated;
- the selected Go graph has been audited locally, but release-artefact licence/NOTICE assembly is
  still open;
- no successful current-revision CI run is recorded; and
- the race test is unsupported on the local Windows ARM64 host, so a strict all-gates local result
  cannot be claimed.

## Delivered

| Deliverable (PRD section 8) | Where | State |
|---|---|---|
| Rule catalogue, machine-readable | `rules/catalog/<group>/FND-*.yaml` (108 files) | Current local `validate --phase0` and generated-document checks passed. |
| Rule catalogue, human-readable | `docs/rule-catalog.md` | Generated from catalogue metadata; includes the provisional 38-rule Phase 1 MVP set. Do not hand-edit. |
| Overlap analysis | `docs/overlap-analysis.md` (generated matrix) and `docs/overlap/*.md` (per-group research, unverified lists, proposed new rules) | Metadata mapping complete for 108 rules; executable adapter evidence and XF provenance remain open. |
| Tool compatibility | `docs/tool-compatibility.md` | Research matrix present; adapter support is provisional. |
| Licence inventory | `docs/licence-inventory.md` | Current selected Go graph and inventory consistency passed the local fail-closed audit. Release-artefact coverage and NOTICE assembly remain open. |
| ADR-001 rule engine strategy | `docs/decisions/ADR-001-*.md` | Accepted. |
| ADR-002 Bicep analysis and source maps | `docs/decisions/ADR-002-*.md` | Accepted; source mapping **not proven**, documented as a deferred limitation. |
| ADR-003 command namespace | `docs/decisions/ADR-003-*.md` | Accepted: keep `azd foundry`, with conditions. |
| ADR-004 synthetic infrastructure | `docs/decisions/ADR-004-*.md` | Accepted. |
| ADR-005 azd extension module boundary (added) | `docs/decisions/ADR-005-*.md` | Accepted. |
| ADR-006 read-only semantics (added) | `docs/decisions/ADR-006-*.md` | Accepted. |
| Spike fixtures and results | `test/spikes/bicep/`, `test/spikes/azd/`, `docs/spikes/azd-extension-notes.md` | Conditional spike code exists; see dated results and current limitations under Tests. |
| Catalogue validator and generators (needed for the DoD tests) | `cmd/rulecatalog`, `internal/catalog` | Implemented; current local validation and stale-document checks passed. |
| CI | `.github/workflows/foundry-doctor-ci.yml`, `.github/workflows/foundry-doctor-dependency-review.yml` | Workflows configure Linux/Windows tests, strict Linux security/quality gates, selected-graph licence audit, tagged spikes and pull-request dependency review. No successful run on the current revision is evidenced. |

## Changed contracts

- **Rule schema** (`internal/catalog.Rule`) is the contract for every later phase. Notable fields: `status` (`verified`, `product-opinion`, ...),
  `overlap.decision` (reuse, wrap, adapt, native, drop), `implementation.owner`, `severity` per profile, `sources[].verifiedVia`, `tests.{positive,negative,skipped,uncertain}`.
- **Compatibility is machine-comparable**: `azd` and `extensions` are closed semver ranges naming the versions actually read; `apiVersions` are `<service> <version>`;
  `preview` must be true when a prerelease or preview API is involved. A version outside a range means the rule is skipped (`unsupported-version`), never passed.
- **`adapt` and `native` decisions require owner `native`**; `wrap` and `reuse` require an external owner. Enforced by the validator.
- **Read-only has a definition** (ADR-006): not "no POST", but "cannot create, modify, delete, start, stop, purge or recover a resource and cannot read secrets or content".
- **Core and extension are separate Go modules** (ADR-005) because `azdext` needs Go 1.26.4 while the core is `go 1.25.12`.

## Rule catalogue changes

108 rules (the PRD's count), none renumbered. **Current: 82 verified, 26 product opinion,
0 dropped, 0 left proposed.** The original Phase 0 snapshot was **85 verified and 23 product
opinion**; that split is historical rather than the current catalogue state.
Decisions: native 68, adapt 38, wrap 1, reuse 1, drop 0. Overlap coverage: none 58, partial 48, full 2.
Platform-basis rules: 33 (all `error` in every profile, enforced). Rules flagged `preview`: 76 (mostly because the azd range ends at a prerelease build).
Retitled: FND-DEP-008 ("delete/replace" to "no unexpected delete"; the what-if enum has no Replace). Relabelled after review: FND-IQ-009 (product opinion).
Corrected after review: FND-GW-004, FND-IQ-003, FND-IQ-012, FND-DEP-001, 002, 003, 008, 009, 011, 012, and others (see the git history of `rules/catalog`).

**Provisional Phase 1 MVP set: 38 rules** (target 35 to 40): 29 source-labelled verified and 9 product opinion; 17 adapt, 20 native, 1 wrap.
The nine product-opinion MVP rules are FND-CFG-007, CFG-012, COST-002, ENV-002, ENV-003, ENV-004, OPS-004, OPS-007, OPS-010. The project owner decided on 2026-10-05 that they remain in the provisional MVP.

Proposed new rules (not created as files) are listed at the end of each `docs/overlap/*.md` fragment, three per group.

## Commands implemented

`go run ./cmd/rulecatalog validate [--phase0]`, `generate-docs [--check]`, `generate-overlap [--check]`. Exit codes: 0 ok; 1 catalogue invalid or generated file stale; 2 usage or I/O error.
No product CLI exists yet (Phase 1).

`scripts/claude/verify-phase.sh [--strict] [--phase0]` exits 0 when all runnable gates pass,
1 when a gate fails, 2 for invalid arguments, 3 when a gate was skipped under `--strict`, and
4 when it cannot enter the repository root. `--phase0` marks only the absent product-command build
as `EXEMPT`; it does not waive any other skip or failure.

`python3 scripts/dependency_audit.py --check-inventory` exits 0 when the selected immutable Go
module graph has recognised approved root licences and exactly matches the inventory, 1 for an
inventory mismatch, and 2 for usage, tool, filesystem, download/metadata, duplicate-marker,
unversioned-module, or missing/unknown/unapproved-licence errors. It needs Python, Go, inventory read
access and either cached modules or network access to download them.

## Current local validation

The following commands completed successfully on the Windows ARM64 working tree:

| Area | Commands/result |
|---|---|
| Formatting and patch hygiene | `gofmt -l .` returned no files; `git diff --check HEAD` passed. |
| Module consistency | `go mod tidy -diff` and `go mod verify` passed. |
| Go correctness and analysis | `go test ./...`, `go vet ./...`, `staticcheck ./...` and `govulncheck ./...` passed. The vulnerability database was reachable for this local run. |
| Build | `go build ./...` passed. There is still no Phase 1 `cmd/foundry-doctor` product command. |
| Catalogue and generated docs | `go run ./cmd/rulecatalog validate --phase0`, `generate-docs --check`, `generate-overlap --check`, and `bash scripts/claude/check-rule-catalog.sh` passed. Generated rule/overlap docs were checked, not hand-edited. |
| Dependency/licence | `python scripts/dependency_audit.py --check-inventory` passed for the selected Go graph. This is not release NOTICE assembly or successful CI evidence. |
| Python tests | The dependency-audit tests under `scripts/` and harness tests under `scripts/claude/` passed. |
| Secret/catalogue scripts | `bash scripts/claude/check-no-secrets.sh` and the catalogue script passed. |

Blocked or unavailable evidence:

- `go test -race ./...` was not runnable because the Go race detector is unsupported on Windows ARM64.
- No GitHub Actions run was performed or supplied. Workflow configuration, including the pinned-spike
  job, is not successful CI evidence.

The ordinary examples are the commands above and were run. The tagged spikes were also run locally:

- Windows ARM64, Bicep `0.48.1`, `BICEP_PATH` set to the official-source executable built from commit
  `cec7951cf694f1067eb17b5b062209fb66c710a9`: `go test -count=1 -tags spike ./test/spikes/bicep`
  passed.
- Windows ARM64, Go `1.26.4`, Python `3.13` with the workflow-pinned schema packages, and
  `AZURE_DEV_DIR` at exact commit `afe4b2b4d262bab4c11f4937e7a7942557ab0ecd`:
  `go test -count=1 -tags spike ./test/spikes/azd` passed. The test executes Microsoft's synthesis
  and provider package tests with target-function coverage, validates the embedded ARM assets
  structurally, and accepts/rejects the representative schema fixtures; it does not claim to compile
  or enumerate a newly returned provider template.

## Historical Phase 0 run

The original hand-off reported `foundry-doctor/scripts/claude/verify-phase.sh` at its then-final
commit. The commit identifier and complete output were not embedded here, so these results are
historical and were not reproduced during remediation:

| Gate | Result |
|---|---|
| gofmt, git diff --check HEAD, go vet, go test, go test -race, staticcheck | PASS |
| check-rule-catalog.sh (validate, docs and overlap up to date) | PASS |
| check-no-secrets.sh | PASS |
| harness unit tests (Bash guard) | PASS |
| govulncheck | **SKIPPED in the historical local run**: the vulnerability database (`vuln.go.dev`) was blocked. CI reported GO-2026-4602 under Go 1.24.13, and the later rooted path implementation exposed GO-2026-4970 under Go 1.25.8. The module now requires Go 1.25.12, the first 1.25 release fixing both issues. Current successful CI evidence is still required. |
| go build ./cmd/foundry-doctor | **SKIPPED**: the product binary does not exist until Phase 1. |

Coverage: `internal/catalog` 91.7%, `cmd/rulecatalog` 90.7%. Tests were mutation-checked: breaking the date parser, the multi-document check, the symlink check, the adapt-owner rule,
the golden files, the guard's bundled `-c`, heredoc expansion and leading-flag handling each made tests fail.
The untagged tests under `test/spikes/azd` are included in `go test ./...`. The original run also
reported that, with `-tags spike`, the Bicep spike
(`BICEP_PATH=<abs path to bicep 0.47.16>`) passed in about 21 s and the azure.yaml schema spike
(`AZURE_DEV_DIR=<absolute azure-dev clone>`) accepted the valid fixture and rejected one without agent
`project`. The command, clone commit and complete output were not retained in this hand-off. Neither
tagged spike runs in `verify-phase.sh`. Both are now configured in the current CI workflow with Bicep
0.48.1 and exact azure-dev commit `afe4b2b4d262bab4c11f4937e7a7942557ab0ecd`. The current local
tagged runs passed as recorded above, but no successful current workflow run is evidenced.

## Security and privacy review

Independent security reviewer, four rounds. **Approved.** Findings fixed: the Bash guard was a bypassable regex that failed open (replaced by a tokenising guard that fails closed, with
unit tests for every bypass found), hooks failed open, allow rules used wildcards, the secret scan was narrow and missed common secret shapes, tool installs were unpinned,
the Bicep spike passed the whole environment to the compiler. ADR-006 now requires fixed aggregate-only Log Analytics queries and discarding of response payloads that can carry secrets or content.
**Accepted risks** (recorded in `agent-swarm.md` and `mcp-setup.md`): the guard is command-text matching and cannot see inside interpreters or scripts, so the credential the session holds
is the real control; the Context7 MCP server (pinned 4.1.1) runs with the session environment; GitHub Actions are tag-pinned, not SHA-pinned (release workflows will pin SHAs).
Not reviewable yet (no code exists): the redactor, path and symlink handling, LLM data flow, adapter trust, SARIF output.

## Foundry lead review

Rejected the first submission (five blocking findings: FND-GW-004 inverted version-set exemption, FND-IQ-003 wrong dimensions minimum, FND-IQ-012 ignored the API version,
non-machine-comparable compatibility data, no executable azure.yaml-only spike). **Approved after fixes**, scope CFG, ENV, IQ, GW, RUN, ADR-001, 002, 003, 004, 005 and the azd spike.
Open non-blocking items: `${{ }}` versus `$${{ }}` syntax in azd core is unverified; a Phase 1 spike to compile the extension's embedded Bicep for synthetic-infrastructure projects.

## Azure black belt review

Rejected the first submission (five blocking findings: FND-DEP-008 Modify false positive, FND-DEP-011 CGNAT contradiction with NET-006, FND-DEP-001 delegated tenants,
ADR-006 not enforceable for log queries and payloads, unfulfilled DEP-002/003 cross-reference). **Approved after fixes**, scope SEC, NET, IDN, REL, OPS, COST, DEP, ADR-001, ADR-006.
Open non-blocking items: Cosmos name length 3 to 44 (Learn) versus 3 to 50 (spec); OPS-002 prod severity; the PSRule `reuse` rule FND-SEC-005 never runs without PSRule installed.

## Go principal review

Rejected the first submission (loader accepted multi-document and empty files, date validation too weak, nondeterministic error order, wrong exit-code classes, `--dir` could not be absolute).
**Approved after fixes.** Optional items taken: spike timeouts, relative-path computation. Not taken: watching the broad secret regex for false positives (Phase 1: widen the allowlist only with a reason), and running the spikes in `verify-phase.sh` (they need a Bicep binary and an azure-dev clone, so the commands are listed under Tests instead).

## Known limitations

1. **Microsoft Learn was unreachable** (egress proxy). Every fact was verified in the public source repositories behind Learn and the REST API specs, recorded in `sources[].verifiedVia`.
   Learn pages can differ from their source on the day they are published. Some Learn articles live in repositories that were not reachable (Cosmos DB, Key Vault soft delete, WAF guidance).
2. **Defender for Cloud and Advisor mappings are empty** for nearly every rule (3 Defender mappings, 0 Advisor). Empty means not verified, not "no equivalent".
3. **WAF/WARA guidance text was not available**; 17 rules lost their `waf` basis for lack of evidence, and the WAF mapping is a Phase 4 task.
4. **Bicep source mapping is not available**: the compiler emits no resource-to-line map. Compiler diagnostics have exact file, line and column; ARM-derived findings carry the file plus an ARM pointer.
   This limits Phase 6 (annotated review copies) to diagnostics or labelled heuristics unless Bicep stabilises its experimental `sourceMapping`.
5. **The current Bicep contract is 0.48.1.** The current Windows ARM64 tagged spike passed with the
   official-source executable identified above. Current CI installs 0.48.1 through `az bicep`, but no
   successful current workflow run is evidenced.
6. **Preview platform**: 76 rules depend on prerelease azd or preview APIs. Their ranges are closed to the exact versions read; widening needs reading older or newer tags.
7. **Unverified facts** are listed per rule in `notes` and per group in `docs/overlap/*.md` (for example: which built-in role grants `checkDomainAvailability`, whether what-if leaves a deployment record, whether `checkPolicyRestrictions` honours exemptions).
8. Role GUIDs for Cosmos DB Built-in Data Contributor and Foundry Agent Consumer are unverified and appear only in notes.

## Deferred issues with rationale

**Resolved by the project owner on 2026-10-05:** (1) the nine product-opinion rules stay in the Phase 1 MVP set; (2) the profile configuration keys are chosen by the implementer and recorded in ADR-007; (3) `--out` replaces `--output` (ADR-003, ADR-007). The items below are the ones still open.

- ~~Whether product-opinion rules ship in the MVP~~: decided, they stay (see above).
- ~~Profile configuration keys~~: decided in ADR-007.
- Dependency pins for the Azure SDK at `go 1.25.12` (the core directive is now decided, ADR-005); `golang.org/x/sync` stays on a 0.19.x pin until the core can move to Go 1.26.
- ~~`--output` flag clash~~: decided, use `--out`.
- Whether the `azd ai agent doctor` overlap (FND-RUN-001, 003, 005) should be wrapped later; decided `adapt` for now.

## Compatibility matrix

| Component | Research snapshot | Source |
|---|---|---|
| azd | `>=1.34.2 <=1.36.0-beta.1` (azure-dev clone 1.36.0-beta.1; latest tag seen v1.35.0) | `docs/tool-compatibility.md`, `docs/spikes/azd-extension-notes.md` |
| azure.ai.agents / projects / connections / toolboxes / routines | 1.0.0-beta.18 / beta.13 / beta.9 / beta.9 / beta.8, exactly | azure-dev clone |
| Bicep CLI | Current contract 0.48.1 (MIT); current local tagged spike passed | `docs/decisions/ADR-002-*.md` |
| Go | core declares `go 1.25.12`; a successful current local and CI `govulncheck ./...` run is required; `azdext` needs 1.26.4 | ADR-005, ADR-008 |
| PSRule for Azure, Checkov, KICS | see `docs/tool-compatibility.md`; Terrascan is archived and excluded | |

## Artefacts for next phase

The catalogue and its schema, the 38-rule MVP set, ADR-001 to 006, the executable azure.yaml spike (the seed for `internal/azureyaml`), the Bicep spike and `BicepCompiler` interface notes,
`AzdContext` abstraction (ADR-003), the verification gates and Claude harness, and the proposed new rules in `docs/overlap/*.md`.

## Exact next-phase prerequisites

1. Close ADR-008's Phase 0 evidence gaps before treating Phase 0 as a completed prerequisite.
2. Obtain successful current CI evidence, including the pinned tagged-spike job, and run the race
   gate on a supported host. The local reachable-database `govulncheck ./...` has passed. Use
   `verify-phase.sh --strict --phase0` for a final Phase 0 harness result; `--phase0` exempts only the
   intentionally absent product command.
3. (Done 2026-10-05) The nine product-opinion MVP rules stay and the profile keys are in ADR-007.
4. Phase 1 first tasks: retain successful Bicep 0.48.1 tagged-spike evidence and the implemented
   namespace-collision monitor; vendor the azd schemas from a tagged release; implement
   `internal/azureyaml` from the spike; implement the `GOWORK=off` core build and import-boundary
   check (ADR-005).
5. Give agent sessions a read-only Azure identity (or none) before Phase 2 work.

## Definition of Done audit

| DoD item | Status | Evidence |
|---|---|---|
| Every proposed rule has a verified source or is labelled product opinion | **Current local validation passed; Phase 0 still open** | Current `rulecatalog validate --phase0`: 82 verified, 26 product opinion, 0 proposed. Original historical split: 85/23. Caveat: sources read in Learn source repositories, not on Learn (limitation 1). |
| Every rule has a reuse/wrap/adapt/native/drop decision | **Metadata present; executable evidence provisional** | `docs/overlap-analysis.md`: 108 of 108. This does not prove adapters or XF mapping. |
| One representative project validates the azure.yaml-only path | **Current local tagged spike passed; CI evidence open** | Exact azure-dev commit, Microsoft synthesis/provider package coverage, structural embedded-ARM validation, and positive/negative schema fixtures. This does not claim compilation of newly returned provider output. |
| Bicep source mapping proven or documented as deferred | **Documented deferral; historical review** | ADR-002; the dated Foundry review reproduced it. Not rerun here. |
| No unresolved namespace conflict | **Current local check passed; CI evidence open** | ADR-003 keeps `azd foundry`; the exact-commit registry/reference monitor is implemented in the tagged azd test and CI workflow. |
| Revised MVP contains 35 to 40 rules | **Current generated-document check passed** | 38 in generated `docs/rule-catalog.md`; nine are product opinion. |
| Unit tests for catalogue validation and duplicate IDs | **Current local tests passed** | `go test ./...`; `internal/catalog` tests. |
| Golden tests for overlap matrix generation | **Current local tests passed** | `go test ./...`; `internal/catalog/testdata/*.golden`. |
| Spike tests on representative Bicep modules and azure.yaml-only projects | **Current local tagged runs passed; CI evidence open** | Exact Bicep 0.48.1 and azure-dev commit commands/results are recorded above. |
| Licence and dependency audit in CI | **Mechanisms and local evidence present; CI evidence open** | Local selected-graph audit and generated `THIRD_PARTY_NOTICES.md` byte check passed; CI has a licence job and pull-request dependency review. |
| Two independent expert reviews (Foundry lead, Azure black belt) | **Current reviews rerun; Phase 0 still open** | Current remediation reviews identified and remediated rule/evidence drift; open DoD gates are listed below. |
| No unresolved critical or high security findings | **Current review rerun; final recheck required** | Current security findings on subprocess environment/output and SARIF sanitisation were remediated; acceptance and reviewer rechecks must pass before hand-off. |

## Final review validation status

The current non-tagged local validation listed above passed. Generated catalogue documents were
checked through their generator and were not hand-edited. Both tagged spikes passed locally. The
supported-host race gate, successful current CI evidence, adapter structured-output fixtures, and XF
provenance remain open, so Phase 0 remains reopened.
