# Phase 4 hand-off: WAF assessment and evidence reporting

Status: **gate PASSED in loop 2 (Go 1.26.9). Loop 1: both reviewers REJECT on one shared blocker
(RunOutput.Evaluated dropped by the engine adapter), fixed. Loop 2: foundry-lead APPROVE, azure-black-belt APPROVE, no blocking findings.**
Fix log: `docs/development/reviews/phase-4-fixlog-loop1.md`. Decisions and deferrals: ADR-026.

## Delivered
- `internal/assess`: aggregation into WAF-facing controls. States PASS, FAIL, WARNING, UNKNOWN, QUESTION, SKIPPED plus
  (loop 1) INFO, ATTESTED and NOT_APPLICABLE; every control has an evidence `basis` and a `stateReason`. A partly
  skipped evaluation is UNKNOWN, never PASS. Info-only findings are INFO. Question answers are evaluated (true with a
  reference is ATTESTED, false is WARNING, anything else stays QUESTION). Costly optional controls are kept out of the
  top actions and listed separately.
- `internal/assess/waf`: versioned mapping file `mapping.yaml` (every catalogue rule mapped or excluded with a reason,
  question entries, sourced cost flags) and `Validate`, with a completeness test that fails on gaps.
- `internal/assess/wara`: reliability-only WARA references with full APRL GUIDs, labels, `pgVerified` and source URL,
  cross-checked against the catalogue source notes.
- Renderers in `internal/report/{owner,developer,evidence,html,common}`: owner and developer HTML are separate pages;
  every view shows sources with verification dates or a "project opinion" label, and limitations.
- `foundry-doctor assess waf` in `internal/app/assess.go` and `cmd/foundry-doctor/assess.go`.
- Samples `samples/assess-{dev,test,prod}/` with `reports/` in every audience and format. **The reports are
  engine output**, regenerated on Go 1.26.9 in gate loop 1.

## Changes in fix loop 2
- `internal/assess` has its own skip classification (`classifySkip`) and no longer imports `internal/rules`;
  `internal/rules/skipclass.go` (the `--strict` classifier) is unchanged. `policy-managed` / `central-dns` →
  ATTESTED (basis attested, "Delegated to Azure Policy by configuration; assignment and compliance not verified");
  `no-*-declared` / `no-*-requirement` → QUESTION; only "no such resource or scope" skips → NOT_APPLICABLE. A mandatory
  ATTESTED control keeps the decision at "Proceed with conditions" and is listed (marked mandatory) under
  "Team-attested answers". INFO and PASS reasons reworded. ADR-026 item 3 amended.
- `mapping.yaml`: question prompts for FND-OPS-002 and FND-REL-009; FND-NET-010 cost flag (sourced); FND-REL-003 cost
  basis narrowed.
- Cross-phase: `internal/llm/redact/project.go` projects only FAIL/WARNING/UNKNOWN/QUESTION/SKIPPED controls (recorded
  in the Phase 9 hand-off).
- Samples and the golden were regenerated with `-update-assess-samples` in gate loop 1; the earlier hand-derived banners are gone.

## Changed contracts
- `waf.Definitions` now returns `([]assess.Definition, error)`; `waf.Applicable` was removed (the mapping file replaces it).
- `assess.Input.WARALookups` is `map[string][]assess.WARARef`; `assess.Control.WARAReferences` is `[]WARARef`.
- `assess.Assessment.Costly` (`costlyRecommendations`) is `[]assess.Action` with cost basis and source.
- New `assess.Control` fields: `ruleVersion`, `basis`, `stateReason`, `costBasis`, `costSourceUrl`, `costLastVerified`,
  `externalOverlap`. New `Assessment.generatedAt`. Evidence JSON adds `toolVersion`, `generatedAt`, `scopeNote`,
  `summary`, `unassessed`, `costlyRecommendations`.
- `htmlreport.Render` replaced by `RenderOwner` and `RenderDeveloper`.
- `assess-test` sample no longer has a duplicate `name:` key, so it renders a report; invalid YAML is now covered by a
  temp-dir test.
- `internal/llm/redact.Assessment` projects only outstanding or unassessed controls (loop 2; see the Phase 9 hand-off).

## Rule catalogue changes
- `rules/catalog/rel/FND-REL-007.yaml`: the APRL source note now carries the full GUID
  `dff62efe-c3a3-4621-98b3-c877c65cb195` (was `dff62efe`). Note text only; behaviour unchanged, so `version` is not raised.
  The note is not rendered into `docs/rule-catalog.md` or `docs/overlap-analysis.md`, so the generated docs are unchanged;
  re-run `cmd/rulecatalog` with Go to confirm.

## Commands implemented
- `foundry-doctor assess waf`: owner Markdown/HTML, developer Markdown/HTML, evidence Markdown/JSON.

## Tests executed and results
Run on Go 1.26.9 after Phase 4 gate loop 1:
- `go test ./...`: pass. `go vet ./...` and `gofmt -l .`: clean. `scripts/claude/check-rule-catalog.sh`: ok (108 rules).
- `-race`, staticcheck, govulncheck and the `-tags spike` Bicep test are run by CI (`foundry-doctor-ci.yml`), not locally.
- Sample reports `samples/assess-{dev,test,prod}/reports` are engine output (regenerated with `-update-assess-samples`);
  no `HAND-DERIVED` marker remains.
- Earlier statements that tests were "not run" and that `TestAssessSampleReports` "fails by design" are withdrawn.

Tests added in gate loop 1: `TestAssessSampleReports` now fails when the real engine yields no PASS control (verified
by reverting the fix); `TestUnassessedActionHasNoFixText`; `TestDecisionStatesUnassessedCount`.

## Security and privacy review
- Markdown escapes untrusted text; HTML is `html/template`, script-free; unsafe URLs are neutralised (`#ZgotmplZ`).
- Findings are redacted before aggregation (unchanged). Sample reports contain only fake values.
- Evidence pack shows suppression reason, owner and expiry, and fingerprints.

## Foundry lead review
- Round 1 (2026-10-18): REJECT (B1-B7). Fixes applied in loop 1.
- Round 2 (2026-10-18): REJECT (NB1, NB2); round-1 items closed. Fixes applied in loop 2; pending re-review.

## Azure black belt review
- Round 1 (2026-10-18): REJECT (B1-B7). Fixes applied in loop 1.
- Round 2 (2026-10-18): REJECT (NB1); round-1 items closed. Fixes applied in loop 2; pending re-review.

## Known limitations
- Sample reports are generated without the Bicep CLI, so about 65 of 78 controls are SKIPPED and dev, test and prod look alike (gate loop 2 NB4).
- `assess waf` is offline: deployed state is not queried; the report names the deployment/runtime controls not evaluated.
- PSRule for Azure, Azure Advisor and Defender for Cloud evidence is not imported (ADR-020, ADR-026).
- WAF checklist codes and per-pillar WAF service-guide citations are not mapped (ADR-026).
- FND-IDN-005, FND-OPS-002, FND-OPS-008 and FND-REL-009 have no configuration answer key (ADR-007, ADR-026).
- Cost flags for FND-OPS-003, FND-REL-008 and FND-REL-009 are UNVERIFIED and not set.
- Delegations to Azure Policy are attested, not verified: deployed policy assignment and compliance are not read.
- WARA support is reliability-only, per PRD.

## Deferred issues with rationale
- See ADR-026 (checklist codes, IDN-005/OPS-008 keys, external evidence import, deployed-evidence mode, unverified cost
  candidates). PRD "Reviewer test with owner, developer and security personas" is covered only by the audience-difference
  and invariant tests; a persona review is part of the next review round.

Carried from the Phase 4 gate loop 1 reviews (non-blocking, not fixed):
- No per-pillar coverage statement; Performance Efficiency has no mapped rule (black belt N2).
- Pillar labels follow the catalogue and include preflight/deployment rules under Reliability and Security (N3); the
  limitations now call them project judgement.
- Cost flags absent on FND-REL-009, FND-OPS-003, FND-REL-008; FND-NET-011 and FND-SEC-010/011 mix free and paid
  advice (N4). Cost URLs and WARA `pgVerified` values need a Learn re-fetch before release (N5, N6); learn.microsoft.com
  was unreachable from the review sandbox, and the two reviewers disagreed on Cosmos continuous-backup tiers, so the
  mapping no longer names a tier beyond "beyond the 7-day tier".
- Gate loop 2 non-blocking items, open: `assess waf` drops engine notes and has no `--azd-version` flag, so a run
  without azd is almost all SKIPPED with no remedy shown; the owner view lists every skip and can read "Proceed with
  conditions" next to "No immediate actions"; SKIPPED controls appear under "Costly recommendations"; the developer view
  prints Fix blocks for PASS and SKIPPED controls; no sample shows FAIL, suppression or baseline, and UNKNOWN and INFO are
  unit-tested only; cost `lastVerified` stamps are not independently confirmed (learn.microsoft.com unreachable).
- Persona reviewer test and HTML accessibility audit remain deferred (ADR-026).

## Compatibility matrix
- Built and tested on Go 1.26.9 in gate loop 1 (`go.mod`/`go.work` declare `go 1.26.9`). No new module dependencies
  (`go.yaml.in/yaml/v3` was already required by `internal/catalog`).

## Artefacts for next phase
- `docs/assess.md`, `internal/assess/waf/mapping.yaml`, ADR-026, `samples/assess-{dev,test,prod}/reports/`.

## Exact next-phase prerequisites
- Run the full gate with Go 1.26.9, regenerate the sample reports, then request round 3 reviews.
- Keep `mapping.yaml` in step with catalogue additions (the test enforces it).

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| No report claims complete WAF compliance | met (code and tests pass on Go 1.26.9; re-reviewed in gate loop 2: foundry-lead APPROVE, azure-black-belt APPROVE) | `assess.BaseLimitations` in every view; owner/developer/evidence/HTML headers; `docs/assess.md` |
| Unknown/non-machine-verifiable controls are visible | met (code and tests pass on Go 1.26.9; re-reviewed in gate loop 2: foundry-lead APPROVE, azure-black-belt APPROVE) | Partial skip → UNKNOWN; delegations → ATTESTED (mandatory keeps conditions); undeclared requirements → QUESTION; own skip classification; `TestPolicyManagedIsAttestedNotApplicable`, `TestUndeclaredRequirementIsQuestion`, `TestClassifySkip` |
| Every recommendation links to a verified source or is labelled project opinion | met (code and tests pass on Go 1.26.9; re-reviewed in gate loop 2: foundry-lead APPROVE, azure-black-belt APPROVE) | `common.Provenance` in owner/developer/evidence Markdown and HTML `prov` template; tests in each renderer package |
| All three audiences receive materially different, useful views | met (code and tests pass on Go 1.26.9; re-reviewed in gate loop 2: foundry-lead APPROVE, azure-black-belt APPROVE) | Separate owner and developer HTML; costly section; `TestOwnerAndDeveloperDiffer`, `TestAssessWAFHTMLAudiencesDiffer` |
| Suppressed and baselined controls remain visible in evidence pack | met (code and tests pass on Go 1.26.9; re-reviewed in gate loop 2: foundry-lead APPROVE, azure-black-belt APPROVE) | Evidence Markdown/JSON with suppression reason/owner/expiry and fingerprints; `TestSuppressedAndBaselinedRemainVisible` |
