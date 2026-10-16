# Phase 1 hand-off

Branch `claude/foundry-doctor-phase-1`, draft PR #29. Code state described here: head 83e055e (2026-10-16).

Status: **implemented; not signed off as complete.** Two DoD items are unmet (independent review verdicts, GitHub workflow run evidence) and one is partly met (security review deferred by ADR-019). The reasons are in the audit table at the end. This file replaces the earlier hand-off, which described the archived implementation (29 rules) that the branch no longer contains.

## Delivered

This branch holds the implementation merged from `main` (PR #18, which delivered Phases 1 to 9 in one tree) plus the review-driven changes made on this branch. Phase 1 scope below is the offline `doctor` path.

- **Rules:** 37 of the 38 rules the catalogue allocates to Phase 1 are implemented and registered. `FND-IDN-002` stays catalogue-only (ADR-020); 37 is inside the PRD target of 35 to 40. Allocation: CFG 9, NET 7, SEC 6, IDN 5, OPS 5, ENV 4, COST 1, REL 1. Nine of the 38 are labelled product opinion (CFG-007, CFG-012, COST-002, ENV-002, ENV-003, ENV-004, OPS-004, OPS-007, OPS-010).
- **Engine and model:** `pkg/sdk` contracts; `internal/rules` engine (selectors, profiles, version gating, skip classification, derived finding keys); `internal/findings` (fingerprint v3); `internal/azureyaml` (YAML tree with alias and merge resolution); `internal/project` (discovery confined with `os.Root`, `.env` parser ported from godotenv v1.5.1); `internal/bicep` (compile through the Bicep CLI, SARIF diagnostics, `build-params`, module template hashing, file-load guard); `internal/armmodel` and `internal/armexpr` (ARM expression handling, module flattening with scope qualification and parameter substitution, parameter-file injection, variable folding); `internal/foundrylink`; `internal/graph`; `internal/baseline`, `internal/suppress`, `internal/config`, `internal/report` (console, JSON, Markdown, SARIF 2.1.0), `internal/app`, `cmd/foundry-doctor`.
- **Review-driven changes on this branch** (reports under `docs/development/reviews/`):
  - Foundry lead: CFG-004 reads `container.resources`, CFG-006 handles outputs and azd variables, CFG-005 token-aware names, CFG-009 uncertain for unknown trigger spellings, ENV rules classify values before digesting and report unusable environments, OPS-007 tokenises shell lines and reads the git root, `requiredVersions` implemented per the verified grammar (two parsers).
  - Azure black belt: conditional and looped modules, per-deployment scope qualification with caller parameter substitution, module outputs, resource-scoped role assignments, `networkInjections` expressions, parameter files (ADR-018), variable folding, SEC-014 analyses compiled outputs, file-load guard against reads outside the project.
- **CLI additions:** `doctor --write-baseline`, `--azd-version`, `summary.hidden` and `summary.skippedByReason` in the JSON summary.
- **Workflows:** CI for Linux, macOS and Windows (tests, smoke samples, cross-platform build), `verify-phase.sh --strict` in the quality job, pinned Bicep 0.48.1 in the smoke job. The broken in-repo starter workflow was removed; the copyable sample is `samples/github/foundry-doctor-starter.yml`.
- **Rules-only PSRule version:** a separate folder `foundry-rules/` at the repository root (not part of the Go module): 108 rule IDs, 28 evaluated in part, 80 documented as not evaluated. It has not been run by PSRule (no PowerShell in the build environment).

## Changed contracts

- `ARMResource` gained `Conditional`, `Condition`, `Looped` and the earlier `Region`, `Kind`, `SKUName`, `Identity`, `Scope`.
- `sdk.Finding`: `Key` (hashed into the fingerprint), `Severity` and `Confidence` as rule-set lower-only overrides (ADR-008 Amendment 3). The engine clamps them to the catalogue value; platform-basis rules ignore a severity override.
- `sdk.Result`: `Skips` next to `Findings` (per-resource uncertain skips). The earlier evidence-note workaround is gone.
- `sdk.Skip`: `MissingCapability`; skip reasons `azd-version-unknown`, `synthetic-infrastructure`, `not-applicable`.
- **Fingerprint v3** (ADR-008 Amendments 1 to 3): `FingerprintVersion = "v3"`, 32 hex characters. A rule that sets no `Key` gets an engine-derived `auto:<line>:<col>:<hash of evidence>` key, so its fingerprint changes when the line or evidence wording changes. **Every fingerprint changed once** between the archived implementation and this one; baselines and suppressions that name a fingerprint must be regenerated.
- Engine rule IDs `bicep/<code>` (compiler diagnostics) and `FND-SYS-SUPPRESSION-EXPIRED` (ADR-009).
- Exit-code order is 4, 2, 3, 1, 0 (PRD section 7); ADR-010 decision 2 was amended to match the code. The default profile is `foundry-prod` (PRD section 6 states no default); ADR-010 decision 6 was amended.
- Go directive is 1.26.9 for the core and the extension (ADR-005 Amendment 1).

## Rule catalogue changes

- Rule versions were bumped wherever behaviour changed (CFG-001, 003 to 006, 009 to 012; ENV-001 to 004; NET-001 to 011 in part; SEC-001 to 006, 008, 009, 010, 012 to 014; IDN-001, 003, 004, 005; OPS-001, 003 to 010; COST-001 to 004; REL-004).
- All 376 `verifiedVia` values are URLs; none cites a local `refs/` path. Most are branch URLs and say "not commit-pinned"; 16 are commit-pinned. Where the source file is in `MicrosoftDocs/azure-ai-docs` (unreachable from the build environment) the entry names the Learn URL and the source file path as text.
- Compatibility windows were widened to azd 1.36.0 and `azure.ai.projects` beta.14 / agents beta.20 / connections beta.10 / toolboxes beta.11 for 44 rules where the diffs showed no change to the rule's facts. Rules left narrower (CFG-001/003/011 agents beta.18, CFG-006, CFG-009 routines beta.8) are listed with reasons in `docs/tool-compatibility.md` section 6. ENV-001 to 004 were widened to azd 1.36.0 after a tag diff of the `.env` grammar, layout, key names and environment-name regexp (unchanged; 1.36.0 only tightened validation).
- `docs/rule-catalog.md` and `docs/overlap-analysis.md` are regenerated from the catalogue and pass the generation check.

## Commands implemented

`doctor` (with `--local`, `--profile`, `--format console|json|markdown|sarif`, `--out`, `--baseline`, `--write-baseline [--force]`, `--min-severity`, `--fail-on`, `--strict`, `--rules`, `--azd-version`), `explain`, `compare`. The tree also contains the later-phase commands from `main`; they are outside this hand-off. Exit codes 0 to 4 per PRD section 7.

## Tests executed and results

`scripts/claude/verify-phase.sh --strict`, run locally on Linux with Go 1.26.9 (2026-10-16, head 83e055e):

| Result | Gate | Detail |
|---|---|---|
| PASS | gofmt -l | |
| PASS | git diff --check HEAD | |
| PASS | go vet ./... | |
| PASS | go test ./... | |
| PASS | go test -race ./... | |
| PASS | staticcheck ./... | |
| SKIPPED | govulncheck ./... | vulnerability database unreachable (vuln.go.dev) in the build environment |
| PASS | go build ./cmd/foundry-doctor | |
| PASS | check-rule-catalog.sh | 108 rules |
| PASS | check-no-secrets.sh | |
| PASS | check-import-boundary.sh | |
| PASS | bash -n scripts/ci/*.sh | |
| PASS | harness unit tests | |

`failed=0 skipped=1`. Under PRD section 22 a strict run with a skipped gate is not a pass; the skipped gate is `govulncheck`, which runs in GitHub CI (`quality` job, now `--strict`) where the database is reachable. `GOOS=windows go vet ./...` also passes locally.

GitHub CI on PR #29 (workflow `foundry-doctor-ci`): the run on head 83e055e (run 53) completed successfully, including `lint, vet, vulncheck, catalogue` in `verify-phase.sh --strict` mode (so `govulncheck` passed on Go 1.26.9), `go test` on ubuntu, macOS and Windows, smoke samples on three operating systems, the cross-platform build, the ADR-005 import boundary and the ADR-003 namespace check; CodeQL and dependency review were also green on the PR. Runs that failed earlier on this branch and were fixed: `staticcheck` (unused function, fixed in d557157), Windows `TestBuildParams` and `TestFileURIPathAndSafePath` (tests assumed POSIX path spelling, fixed in 9214baf and 06ab397), the secret scan (fake secret-shaped literal in a test, fixed in af260e4). The run on the docs-only commit a02fc50 (this hand-off) was still in progress when this file was last edited; confirm it on the PR. Coverage of the engine packages: all at or above 80% (rules 97%, findings 87%, baseline 97%, report 94%, config 99%).

Not run: the Bicep-backed samples and the `spike`-tagged real-CLI tests in the audit environment (no Bicep CLI); they run in CI smoke (Bicep 0.48.1 installed in the job). `foundry-doctor-release.yml` (signing, SBOM, provenance) has never been run.

## Security and privacy review

Deferred to Phase 9 by the user (ADR-019, which also defers the Go principal review). A canary secret in an `azure.yaml` literal appeared in none of the four output formats in the DoD audit. A hostile `.bicepparam` that read a file outside the project through `loadTextContent` was found by the Azure black belt and fixed (file-load guard, ADR-018 Amendment 2). The global item "no unresolved critical/high security findings" cannot be shown until Phase 9.

## Foundry lead review

Reports: `reviews/phase-1-foundry-lead.md` (archived code), `-rerun.md`, `-verify.md`, `-final.md`. Last verdict: **REJECT** at 9214baf on two blockers: uncertain notes dropped when a CFG or ENV rule had findings (N3-1), and CFG-009 reporting not-applicable for routine triggers written in the current extension spelling (N3-2). Both, and the minor findings N3-3 to N3-6, N3-8 to N3-12, were fixed afterwards (commits 324e65c, a8f4e5d, 83e055e). **No re-review of the fixed code has been recorded.** N3-7 (ENV-003 window) was resolved by widening the azd window. The reviewer confirmed the `requiredVersions` grammar by running the real blang and Masterminds libraries.

## Azure black belt review

Reports: `reviews/phase-1-azure-black-belt.md` (archived code), `-rerun.md`, `-verify.md`, `-round2.md`, `-final.md`. Last verdict: **ACCEPT with conditions** at 47d5aa7 (code identical to 36acd92). Conditions P1 (IDN-004 info scope), P2 (file loads outside the project) and P3 (variables in conditions) were addressed in 83e055e; P4 to P7 also fixed; P8 (API versions) was checked and needed no change. **No re-review of the fixed code has been recorded.**

## Known limitations

- Child-module variables are not folded into conditions; root variables are folded only when a parameter file exists. Microsoft sample 15 evaluates no additional rules from a parameter file for that reason (ADR-018).
- `--strict` can exit 3 on a clean Foundry project while required inputs are missing (no selected azd environment, no Bicep); skips that mean "nothing to check" are classified by design and do not count (ADR-010 Amendments 2 to 4).
- Extension versions (`azure.ai.*`) are never detected; their windows act only through `requiredVersions.extensions` notes (ADR-004). Extension constraints are never decided offline.
- CFG-009 reads only the routines beta.8 spelling; other spellings are uncertain.
- The IDN-004 role GUID table, `accounts/projects` log categories and several catalogue facts rest on documentation pages that were unreachable (Microsoft Learn, `MicrosoftDocs/azure-ai-docs`) in the build environment; their notes say so.
- `bicep/<code>` rule IDs are rewritten to `bicep_<code>` in SARIF (ADR-009 Amendment 1).
- The `--baseline` path resolves against the working directory, not `--dir`.
- `schemas/report.schema.json` describes a different shape than `internal/report/json.go` emits; nothing validates against it.
- `foundry-rules/` is untested against PSRule.
- Windows: environment names ending in a period are accepted although azd 1.36.0 rejects them on Windows.

## Deferred issues with rationale

- PSRule adapter in the Go tool (ADR-020); `advanced.psrule.enabled: true` prints a warning. Reason: PRD marks it optional; a separate rules-only PSRule folder exists.
- `rules.packs` is reserved and inert (a non-default pack prints a warning).
- `FND-IDN-002` catalogue-only (ADR-020).
- Go principal and security reviews until Phase 9 (ADR-019).
- ADR-011 decision 6 (supported OS list) is still "Proposed, owner to confirm"; CI covers ubuntu, macOS and Windows.
- The `unsupported-host` and `invalid-azure-yaml` findings of ADR-004 (documented as not implemented in ADR-004 Amendment 1).

## Compatibility matrix

Go 1.26.9 (core and extension, ADR-005 Amendment 1); Bicep CLI 0.48.1 (ADR-015); azd 1.34.2 to 1.36.0 (windows per rule in the catalogue; `docs/tool-compatibility.md` section 6); Foundry extensions as listed in section 6. Local and CI operating systems: ubuntu, macOS, Windows (amd64; the audit ran on Linux, an earlier session on Windows/arm64).

## Artefacts for next phase

`sdk.Rule`, `sdk.Result` (with `Skips`), `sdk.Finding` (`Key`, overrides), `internal/app` wiring (`builtinRules`), `armmodel` (flattened resources, `Variable`, `TagsAreExpression`, `FromDefault`, `FromDefaultPaths`, `Placements`), the rule test fixtures in `internal/rules/rulestest` (compiled with Bicep 0.48.1), `internal/rules/stability` (fingerprint and output stability tests), and the review reports under `docs/development/reviews/`.

## Exact next-phase prerequisites

1. Confirm the CI run on the final PR head (the run on the docs-only commit that adds this file) is green.
2. Obtain and record a re-review (or an explicit acceptance of the recorded verdicts) from the Foundry lead and the Azure black belt on the final head.
3. Resolve the two open decisions flagged in this hand-off: keep exit order 4, 2, 3, 1, 0 and default profile `foundry-prod`, or change them.
4. Run `foundry-rules/` once under PowerShell with PSRule and record the result.
5. Run `foundry-doctor-release.yml` once on a tag or branch to prove signing, SBOM and provenance.
6. ADR-011 decision 6 owner confirmation. Phase 2 needs the read-only Azure adapter design (ADR-006) and a test subscription for live tests.

## Definition of Done audit

Statuses follow the independent audit of 2026-10-16 (`implementation-coordinator`, audit mode, head 9214baf) and are updated for the changes made after it (head 83e055e).

| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| All selected 35-40 rules implemented, sourced, versioned | met: 37 of 38 allocated; FND-IDN-002 catalogue-only (ADR-020) | Go registry; catalogue has sources, `lastVerified`, versions, all `verifiedVia` values are URLs |
| Offline doctor completes without Azure credentials | met | `TestExitCodes` (`cmd/foundry-doctor/main_test.go`), CI smoke on three operating systems, canary run |
| Baseline hides only matching old fingerprints | met | `TestApplyHidesOnlyMatching`; baseline generation added (`doctor --write-baseline`) with round-trip test; fingerprint v3 (ADR-008) |
| Expired suppressions generate an error | met | `FND-SYS-SUPPRESSION-EXPIRED` (ADR-009 Amendment 1), `TestExpiredSuppressionFailsRun` |
| Console, JSON, Markdown and SARIF output is deterministic | met | goldens in `internal/report/testdata`, `TestOutputFormatsAreDeterministic`, `internal/rules/stability` |
| GitHub PR workflow uploads SARIF and enforces exit codes | unmet | the reusable workflow `foundry-doctor-offline.yml` enforces the exit code, but no successful run of it is evidenced; the broken in-repo starter was removed; sample starter checked with actionlint only |
| No secrets in findings, debug logs or snapshots | partly met | `check-no-secrets.sh`, redaction fuzz tests, canary run; independent security review deferred by ADR-019; no `--debug` flag yet |
| Cross-platform binaries produced | met for build; release signing unrun | CI job "cross-platform build and checksums" (six targets); `foundry-doctor-release.yml` never run |
| Exit codes 0 to 4 | met (order 4, 2, 3, 1, 0, ADR-010 Amendment 4) | `internal/report/exit.go` and tests |
| Skipped never equals passed; `--strict` contract | met with ADR-covered narrowing | ADR-010 decision 9 and Amendments 2 to 4; `skipclass.go`, `TestClassifySkip` |
| Profiles dev, test, prod; platform-basis rules error in every profile | met | generated from the catalogue; catalogue test (`catalog_test.go`); default profile `foundry-prod` (ADR-010 Amendment 4) |
| Bicep adapter compiles only, no custom parser | met | `internal/bicep` runs the CLI with `--no-restore --diagnostics-format sarif`; diagnostics become `bicep/<code>` findings; file-load guard |
| Read-only guarantee | met for Phase 1 | `os.Root` confinement; no Azure call in `--local`; ADR-006, ADR-018 |
| Fuzz tests (YAML, redaction) | met | `FuzzParse`, `FuzzRedact`, baseline, suppress, config, report and armmodel fuzzers |
| Independent Foundry lead and Azure black belt reviews | unmet | last verdicts REJECT (Foundry lead) and ACCEPT with conditions (Azure black belt); fixes landed after both; no re-review recorded |
| Security and Go principal reviews | requirement changed by ADR-019 | deferred to Phase 9 |
| Hand-off written, DoD audited | met | this file and the audit above |
