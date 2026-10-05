# Phase 0 hand-off

Branch `claude/foundry-doctor-phase-0`, stacked on `claude/foundry-doctor-harness` (PR #15, not yet merged).
Date: 2026-10-05. Reviewers are independent agents that did not author the work they reviewed.

## Delivered

| Deliverable (PRD section 8) | Where | State |
|---|---|---|
| Rule catalogue, machine-readable | `rules/catalog/<group>/FND-*.yaml` (108 files) | All 108 rules researched. Validates under the Phase 0 gate. |
| Rule catalogue, human-readable | `docs/rule-catalog.md` | Generated; includes the 38-rule Phase 1 MVP set. |
| Overlap analysis | `docs/overlap-analysis.md` (generated matrix) and `docs/overlap/*.md` (per-group research, unverified lists, proposed new rules) | Done. |
| Tool compatibility | `docs/tool-compatibility.md` | Done. |
| Licence inventory | `docs/licence-inventory.md` | Done; transitive licence audit of `azdext`, viper, goccy and go-sarif **not** done. |
| ADR-001 rule engine strategy | `docs/decisions/ADR-001-*.md` | Accepted. |
| ADR-002 Bicep analysis and source maps | `docs/decisions/ADR-002-*.md` | Accepted; source mapping **not proven**, documented as a deferred limitation. |
| ADR-003 command namespace | `docs/decisions/ADR-003-*.md` | Accepted: keep `azd foundry`, with conditions. |
| ADR-004 synthetic infrastructure | `docs/decisions/ADR-004-*.md` | Accepted. |
| ADR-005 azd extension module boundary (added) | `docs/decisions/ADR-005-*.md` | Accepted. |
| ADR-006 read-only semantics (added) | `docs/decisions/ADR-006-*.md` | Accepted. |
| Spike fixtures and results | `test/spikes/bicep/`, `test/spikes/azd/`, `docs/spikes/azd-extension-notes.md` | Executable; see Tests. |
| Catalogue validator and generators (needed for the DoD tests) | `cmd/rulecatalog`, `internal/catalog` | Done. |
| CI | `.github/workflows/foundry-doctor-ci.yml` | Done; not yet run on GitHub for this branch's latest commit. |

## Changed contracts

- **Rule schema** (`internal/catalog.Rule`) is the contract for every later phase. Notable fields: `status` (`verified`, `product-opinion`, ...),
  `overlap.decision` (reuse, wrap, adapt, native, drop), `implementation.owner`, `severity` per profile, `sources[].verifiedVia`, `tests.{positive,negative,skipped,uncertain}`.
- **Compatibility is machine-comparable**: `azd` and `extensions` are closed semver ranges naming the versions actually read; `apiVersions` are `<service> <version>`;
  `preview` must be true when a prerelease or preview API is involved. A version outside a range means the rule is skipped (`unsupported-version`), never passed.
- **`adapt` and `native` decisions require owner `native`**; `wrap` and `reuse` require an external owner. Enforced by the validator.
- **Read-only has a definition** (ADR-006): not "no POST", but "cannot create, modify, delete, start, stop, purge or recover a resource and cannot read secrets or content".
- **Core and extension are separate Go modules** (ADR-005) because `azdext` needs Go 1.26.4 while the core is `go 1.25.8`.

## Rule catalogue changes

108 rules (the PRD's count), none renumbered. **85 verified, 23 product opinion, 0 dropped, 0 left proposed.**
Decisions: native 68, adapt 38, wrap 1, reuse 1, drop 0. Overlap coverage: none 58, partial 48, full 2.
Platform-basis rules: 33 (all `error` in every profile, enforced). Rules flagged `preview`: 76 (mostly because the azd range ends at a prerelease build).
Retitled: FND-DEP-008 ("delete/replace" to "no unexpected delete"; the what-if enum has no Replace). Relabelled after review: FND-IQ-009 (product opinion).
Corrected after review: FND-GW-004, FND-IQ-003, FND-IQ-012, FND-DEP-001, 002, 003, 008, 009, 011, 012, and others (see the git history of `rules/catalog`).

**Phase 1 MVP set: 38 rules** (target 35 to 40): 29 verified and 9 product opinion; 17 adapt, 20 native, 1 wrap.
The nine product-opinion MVP rules are FND-CFG-007, CFG-012, COST-002, ENV-002, ENV-003, ENV-004, OPS-004, OPS-007, OPS-010. Phase 1 needs a decision on whether opinion rules are in the MVP.

Proposed new rules (not created as files) are listed at the end of each `docs/overlap/*.md` fragment, three per group.

## Commands implemented

`go run ./cmd/rulecatalog validate [--phase0]`, `generate-docs [--check]`, `generate-overlap [--check]`. Exit codes: 0 ok; 1 catalogue invalid or generated file stale; 2 usage or I/O error.
No product CLI exists yet (Phase 1).

## Tests executed and results

`foundry-doctor/scripts/claude/verify-phase.sh` at the final commit:

| Gate | Result |
|---|---|
| gofmt, git diff --check HEAD, go vet, go test, go test -race, staticcheck | PASS |
| check-rule-catalog.sh (validate, docs and overlap up to date) | PASS |
| check-no-secrets.sh | PASS |
| harness unit tests (Bash guard) | PASS |
| govulncheck | **SKIPPED locally**: the vulnerability database (`vuln.go.dev`) is blocked in this sandbox. CI runs it, and its first run **failed** on GO-2026-4602 (Go 1.24.13 standard library, reachable from the catalogue loader). Fixed by moving the core to `go 1.25.8`; CI is the confirmation. |
| go build ./cmd/foundry-doctor | **SKIPPED**: the product binary does not exist until Phase 1. |

Coverage: `internal/catalog` 91.7%, `cmd/rulecatalog` 90.7%. Tests were mutation-checked: breaking the date parser, the multi-document check, the symlink check, the adapt-owner rule,
the golden files, the guard's bundled `-c`, heredoc expansion and leading-flag handling each made tests fail.
Spike tests: `test/spikes/azd` runs in `go test ./...`. With the build tag `spike`: the Bicep spike (`BICEP_PATH=<abs path to bicep 0.47.16>`) passed in about 21 s,
and the azure.yaml schema spike (`AZURE_DEV_DIR=<azure-dev clone>`) passed and shows a fixture without the agent `project` is rejected. Neither spike runs in `verify-phase.sh`.

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
5. **Bicep was tested at 0.47.16.** The `v0.48.1` tag exists but its Linux binary returned 404, so the newest version was not tested.
6. **Preview platform**: 76 rules depend on prerelease azd or preview APIs. Their ranges are closed to the exact versions read; widening needs reading older or newer tags.
7. **Unverified facts** are listed per rule in `notes` and per group in `docs/overlap/*.md` (for example: which built-in role grants `checkDomainAvailability`, whether what-if leaves a deployment record, whether `checkPolicyRestrictions` honours exemptions).
8. Role GUIDs for Cosmos DB Built-in Data Contributor and Foundry Agent Consumer are unverified and appear only in notes.

## Deferred issues with rationale

**Resolved by the project owner on 2026-10-05:** (1) the nine product-opinion rules stay in the Phase 1 MVP set; (2) the profile configuration keys are chosen by the implementer and recorded in ADR-007; (3) `--out` replaces `--output` (ADR-003, ADR-007). The items below are the ones still open.

- ~~Whether product-opinion rules ship in the MVP~~: decided, they stay (see above).
- ~~Profile configuration keys~~: decided in ADR-007.
- Dependency pins for the Azure SDK at `go 1.25.8` (the core directive is now decided, ADR-005); `golang.org/x/sync` stays on a 0.19.x pin until the core can move to Go 1.26.
- ~~`--output` flag clash~~: decided, use `--out`.
- Whether the `azd ai agent doctor` overlap (FND-RUN-001, 003, 005) should be wrapped later; decided `adapt` for now.

## Compatibility matrix

| Component | Verified at | Source |
|---|---|---|
| azd | `>=1.34.2 <=1.36.0-beta.1` (azure-dev clone 1.36.0-beta.1; latest tag seen v1.35.0) | `docs/tool-compatibility.md`, `docs/spikes/azd-extension-notes.md` |
| azure.ai.agents / projects / connections / toolboxes / routines | 1.0.0-beta.18 / beta.13 / beta.9 / beta.9 / beta.8, exactly | azure-dev clone |
| Bicep CLI | 0.47.16 (MIT) | `docs/decisions/ADR-002-*.md` |
| Go | core `go 1.25.8` (set after `govulncheck` in CI found GO-2026-4602 on Go 1.24.13); `azdext` needs 1.26.4 | ADR-005 |
| PSRule for Azure, Checkov, KICS | see `docs/tool-compatibility.md`; Terrascan is archived and excluded | |

## Artefacts for next phase

The catalogue and its schema, the 38-rule MVP set, ADR-001 to 006, the executable azure.yaml spike (the seed for `internal/azureyaml`), the Bicep spike and `BicepCompiler` interface notes,
`AzdContext` abstraction (ADR-003), the verification gates and Claude harness, and the proposed new rules in `docs/overlap/*.md`.

## Exact next-phase prerequisites

1. Merge PR #15 (the harness) and this branch's PR.
2. Run `foundry-doctor/scripts/install-dev-tools.sh` and make sure CI can reach `vuln.go.dev`; turn on `--strict` in CI once `cmd/foundry-doctor` exists.
3. (Done 2026-10-05) The nine product-opinion MVP rules stay and the profile keys are in ADR-007.
4. Phase 1 first tasks: re-run the Bicep spike on 0.48.1 or later; implement the CI check that fails when `foundry` appears in the azd registry or command reference (ADR-003);
   vendor the azd schemas from a tagged release; implement `internal/azureyaml` from the spike; implement the `GOWORK=off` core build and import-boundary check (ADR-005).
5. Give agent sessions a read-only Azure identity (or none) before Phase 2 work.

## Definition of Done audit

| DoD item | Status | Evidence |
|---|---|---|
| Every proposed rule has a verified source or is labelled product opinion | **Met** | `rulecatalog validate --phase0`: 85 verified, 23 product opinion, 0 proposed. Caveat: sources read in the Learn source repositories, not on Learn (limitation 1). |
| Every rule has a reuse/wrap/adapt/native/drop decision | **Met** | `docs/overlap-analysis.md`: 108 of 108. |
| One representative project validates the azure.yaml-only path | **Met** | `test/spikes/azd` (runs in `go test`); schema validation with the `spike` tag. |
| Bicep source mapping proven or documented as deferred | **Met as a documented deferral** | ADR-002; reproduced by the Foundry lead. Not proven. |
| No unresolved namespace conflict | **Met, with conditions** | ADR-003: keep `azd foundry`; CI check and own extension source required; flag conflicts recorded. |
| Revised MVP contains 35 to 40 rules | **Met** | 38 (`docs/rule-catalog.md`). Nine are product opinion. |
| Unit tests for catalogue validation and duplicate IDs | **Met** | `internal/catalog` tests. |
| Golden tests for overlap matrix generation | **Met** | `internal/catalog/testdata/*.golden`. |
| Spike tests on representative Bicep modules and azure.yaml-only projects | **Met** | `test/spikes/`. |
| Licence and dependency audit in CI | **Partly met** | `docs/licence-inventory.md` and the dependency-review workflow requirement are written; the CI job `dependency-review.yml` is a Phase 1 deliverable. Transitive audit not done. |
| Two independent expert reviews (Foundry lead, Azure black belt) | **Met** | Both approved after the fixes above. |
| No unresolved critical or high security findings | **Met** | Security reviewer approved; accepted risks recorded. |
