# Phase 4 review: Azure black belt, round 2

Reviewer: `azure-black-belt` (independent, read-only, did not author the code). Date: 2026-10-18. Round 2.
Inputs: `REVIEW_BRIEF.md`, `phase-4-fixlog-loop1.md`, ADR-026, round-1 review, PRD section 12, and the code under
`internal/assess/**`, `internal/report/{owner,developer,evidence,html,common}/**`, `internal/app/assess.go`,
`internal/llm/redact/project.go`, `internal/rules/skipclass.go`, `cmd/foundry-doctor/assess_samples_test.go`,
`samples/assess-*/reports/`, `rules/catalog/**`.

Environment: this sandbox has no Go toolchain and no access to a package registry, so nothing was compiled or run. The
review is by reading code. Gate policy for this loop: an honest hand-off that says the gates were not run is accepted.
The phase-4 hand-off does say this (lines 45–51) and marks every DoD row `unmet (pending …)`.

## Round-1 findings

| Id | Status | Evidence |
|---|---|---|
| B1 info → WARNING | **CLOSED** | `assess.stateFor` gives `info` findings their own branch and the `INFO` state (`assess.go`, `stateFor`). `INFO` is not `Outstanding` or `Unassessed`, so it is kept out of the top actions, the costly list and the decision. Test: `assess_test.go:112-118` (SEC-010 CMK posture). Wording issue under N1. |
| B2 costly not separated / wrong flags | **CLOSED** | The costly flags are now in `waf/mapping.yaml` with a basis, an https source and a date, and `Validate` rejects a must-have rule marked costly. `pillar == cost` no longer means costly. A "Costly recommendations (optional)" section is rendered in the owner Markdown, the HTML and the evidence Markdown and JSON (`owner/report.go`, `html/report.go:82-85`, `evidence/report.go:23,92`). Costly items are kept out of `topActions`. I spot-checked two cost bases: AI Search replicas × partitions billing, and CMK needing a billable Key Vault (https://learn.microsoft.com/azure/search/search-sku-manage-costs), plus Cosmos DB continuous backup, where the 30- and 35-day tiers are charged, the 7-day tier is not, and restores are always charged (https://learn.microsoft.com/azure/cosmos-db/continuous-backup-restore-introduction). Both are correct. Budgets are free (https://azure.microsoft.com/pricing/details/cost-management/), which matches COST-001 being unflagged. |
| B3 self-defined applicability / circular test | **CLOSED** | `mapping.yaml` lists every catalogue rule as mapped or excluded. `Validate` fails on a missing or duplicate rule, a pillar mismatch, an excluded rule from phases 1–4, or a dropped or deprecated rule that is mapped. CFG, ENV, DEP and RUN rules are now mapped. The check does not depend on the selection logic. The ADR-026 deferral of WAF checklist codes is acceptable and is disclosed in `BaseLimitations`. |
| B4 partial skip PASS; N/A forces conditions | **CLOSED, with a new defect in the N/A classification** (see NB1) | An evaluated rule with a hidden skip now gives `UNKNOWN` ("Partial evaluation"). A rule whose skips are all by-design gives `NOT_APPLICABLE`, which the decision does not count. |
| B5 declared:false → PASS | **CLOSED** | `answerState`: only `true` plus a non-empty `disasterRecovery.reference` gives `ATTESTED` with basis `attested`. `false` gives `WARNING`, and a non-boolean value or a missing reference stays `QUESTION`. ADR-026 §4 defers the IDN-005 and OPS-008 keys, and the deferral is disclosed. |
| B6 HTML | **CLOSED** | `RenderOwner` and `RenderDeveloper` are separate. Both carry the `prov` template (source, date, project-opinion label), limitations, the costly section and the profile. The table has a caption and `scope` attributes. `html/template` sanitises URL attributes in `href`. |
| B7 samples / PSRule / Advisor disclosure | **PARTIALLY, accepted for this loop** | Every report gets the PSRule/Advisor/Defender "not imported" limitation (`app/assess.go`, `assessLimitations`). The developer view lists the overlapping IDs as "not ingested". The sample reports exist but are hand-derived; see "Sample disclosure" below. |
| N1 truncated WARA ID | **CLOSED** | `wara/mapping.go` and the `FND-REL-007` catalogue note (line 67) now use the full GUID. I confirmed it upstream: `dff62efe-c3a3-4621-98b3-c877c65cb195` is "Reliability in Azure AI Search", multi-region (https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Search/searchServices/recommendations.yaml). |
| N2 WARA reliability-only | **CLOSED** | Tested in both directions (fix log; `wara/mapping_test.go`). |
| N3 WARA labels / pgVerified / e35cf148 | **CLOSED** | Each reference has a label and pgVerified. e35cf148 is labelled "platform migration, context only". |
| N4 pillar names | **CLOSED** | `assess.PillarName` maps to the five official pillar names (https://learn.microsoft.com/azure/well-architected/pillars). |
| N5 control IDs | **CLOSED (disclosed)** | Covered by a `BaseLimitations` entry and ADR-026 §2. |
| N6 FAIL wording | **CLOSED** | `docs/assess.md`. |
| N7 evidence auditability | **CLOSED** | The evidence pack now has suppression reason, owner and expiry, fingerprint, source and date, tool version, generatedAt, unassessed and costly. |
| N8 multi-line fix | **CLOSED** | `common.Fenced`. |
| N9 profile in HTML | **CLOSED** | |
| N10 deployed evidence not named | **CLOSED** | `assessLimitations` names each phase 2 and phase 3 control that was not evaluated. |

## Compile check (callers of changed signatures)

- `waf.Definitions` now returns `([]assess.Definition, error)`. All callers handle the error: `internal/app/assess.go:116`
  and `waf/mapping_test.go:46,63,120,144`. No other caller exists (I grepped `internal`, `cmd` and `pkg`).
- `waf.Applicable`: no remaining references.
- `htmlreport.Render` was removed. The only caller (`internal/app/assess.go:262,267`) now uses `RenderOwner` and
  `RenderDeveloper`. `html/report_test.go` uses the new names. The match in `cmd/foundry-doctor/e2e_test.go:242` is an
  unrelated `failingReporter.Render` method.
- `Control.WARAReferences` changed from `[]string` to `[]assess.WARARef`, and `Assessment.Costly` changed from
  `[]ControlRef` to `[]Action`. Callers and tests are updated (`developer/report_test.go:48`, `assess_test.go:117,180`,
  `evidence/report.go:23`).
- LLM code (`internal/app/llm.go`, `llm_narrator.go`, `llm_test.go`, `internal/llm/redact/project.go` and its test) uses
  only `assess.Assessment`, `assess.Control{RuleID, State, Findings, Skipped, Recommendation}` and existing state
  constants. All of these still exist.
- New import `internal/assess` → `internal/rules`: `internal/rules` imports only `armexpr`, `catalog`, `findings` and
  `pkg/sdk`, so there is no cycle. `wara` imports `assess`, but `assess` does not import `wara`.
- In `html/report.go`, `Funcs` is applied before `Parse`, and both templates are built from `layout` and then the page.
  The fields that the `prov` and `cost` templates read exist on both `Action` and `Control`.

I found no code that would fail to compile. gofmt and vet were not checked.

## Sample-report disclosure

The disclosure is adequate for this loop:
- `reports/HAND-DERIVED` and `reports/README.md` say "hand-derived, not engine output".
- `TestAssessSampleReports` fails while the marker exists, and `-update-assess-samples` deletes the marker.
- The hand-off and ADR-026 §8 both disclose it.
- The DoD rows stay `unmet`.

Non-blocking gap: the report files themselves (for example `samples/assess-prod/reports/owner.md`) carry no banner, so a
reader who opens one file directly will not see the caveat. See N3.

## LLM projection with the new states

There is no privacy defect. `redact.Assessment` projects only the rule description, the redacted finding locations, the
redacted and host-stripped skip reasons, and the recommendation. It does **not** project `QuestionAnswer`, which can hold
the DR plan reference URL, `StateReason`, `CostBasis`, or the WARA and external IDs.

There is a correctness issue, which is non-blocking because the narrative is advisory and deterministic output stays
authoritative (N2). The projection excludes only `PASS`. `INFO`, `ATTESTED` and `NOT_APPLICABLE` controls (rank 5) are
sent as `llm.Finding` entries with no state. The model therefore cannot tell an attested DR answer or a not-applicable
Cosmos check apart from a defect, and it may narrate them as problems. These controls can also fill the
`maxAssessmentFindings` budget when only a few outstanding items exist. An assessment with nothing outstanding still
reaches the provider instead of returning `ErrUnavailable`.

## New blocking finding

### NB1. Configuration-delegated skips are reported as deterministic NOT_APPLICABLE
`assess.notApplicable` (`assess.go`) relies on `rules.ClassifySkip == SkipByDesign`. `skipclass.go:31-51` treats several
reason heads as by design that are really team **declarations**, not facts that Foundry Doctor checked:
- `policy-managed`. This is emitted by **must-have** `FND-OPS-001` (diagnostic settings,
  `internal/rules/ops/ops.go:453-455`) and by **must-have** `FND-NET-003` (private-endpoint DNS zone groups,
  `internal/rules/net/rules.go:318-320`) when configuration says Azure Policy manages the setting.
- `no-resilience-objective-declared` (`FND-REL-009`, `rel.go:597`) and `no-access-requirement-declared`.

These controls become `NOT_APPLICABLE` with `basis: deterministic` and the reason "Not applicable: policy-managed". They
are left out of the unassessed list and the decision, and the owner view does not list them. An offline run cannot see
whether a DeployIfNotExists or Modify assignment exists or is compliant. That is deployed Azure Policy state, and
remediation of existing resources needs a remediation task
(https://learn.microsoft.com/azure/governance/policy/how-to/remediate-resources). So a prod project can reach "Proceed
within assessed scope" while diagnostic settings and private DNS are unverified. That is a false "deterministic" claim
on mandatory security and operations controls, which the brief classes as blocking.

Fix (small, local to `internal/assess`): before applying `ClassifySkip`, treat `policy-managed` as `ATTESTED` with
basis `attested` and the reason "Delegated to Azure Policy by configuration; assignment and compliance not verified".
Do the same, or use `QUESTION`, for `no-*-declared`. These controls then appear in the owner's "Team-attested answers"
section. Keep them out of `NOT_APPLICABLE`. Add a test with OPS-001 skipped as `policy-managed` that asserts the state is
not `NOT_APPLICABLE` and the basis is not `deterministic`. Leave `skipclass.go` unchanged for `--strict`.

## Non-blocking findings

- **N1 INFO reason overclaims.** The `INFO` reason says "no defect reported". `INFO` also covers must-have rules at
  `dev` profile severity (`FND-NET-001`, `FND-OPS-001`, `FND-CFG-007`, `FND-ENV-001`, `FND-ENV-003` have `dev: info`). It
  also covers rule-lowered findings such as `FND-IDN-004` Contributor at Foundry scope, where the rule's own evidence says
  "not proof of safety" (`idn004.go:337-339`). Change the wording to "informational at this profile's severity; see
  evidence". Consider listing `INFO` controls in the owner view when `Mandatory` is true.
- **N2 LLM projection.** Project only controls where `Outstanding || Unassessed` is true, and include the state token in
  `llm.Finding`. Return `ErrUnavailable` when nothing qualifies. Add a test with ATTESTED, INFO and NOT_APPLICABLE
  controls.
- **N3 Banner in hand-derived files.** Add a one-line "hand-derived, not engine output (ADR-026)" banner to each
  committed sample report. The `-update-assess-samples` run overwrites it.
- **N4 REL-003 cost basis.** The basis cites only continuous backup. The rule also recommends Cosmos DB zone redundancy,
  which has its own pricing impact. Cite it or narrow the basis. Unverified here.
- **N5 `central-dns` reason.** FND-NET-003's `central-dns:` skip is (correctly) not by design, so it gives `SKIPPED`.
  Its sibling `policy-managed` should be handled the same way as NB1, for consistency.

## Verdict

All seven round-1 blocking items are closed; B7 is partially closed and accepted under the gate policy, with an adequate
hand-derived disclosure. I found no compile breaks. One new blocking defect (NB1) presents team-declared delegation of
two mandatory controls as a deterministic not-applicable outcome that bypasses the decision. The fix is small and local.

VERDICT: REJECT
