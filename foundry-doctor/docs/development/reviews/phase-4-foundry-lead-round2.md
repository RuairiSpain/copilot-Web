# Phase 4 review: Foundry lead, round 2

Reviewer: `foundry-lead` (independent; did not write the code). Date: 2026-10-18. Round 2.

Scope: Phase 4 (PRD §12) after fix loop 1. I read:

- `REVIEW_BRIEF.md`;
- `docs/development/reviews/phase-4-fixlog-loop1.md`;
- ADR-026;
- the rewritten `docs/development/hand-offs/phase-4.md`;
- `internal/assess/**`, `internal/report/{owner,developer,evidence,html,common}/**`, `internal/app/assess.go`,
  `cmd/foundry-doctor/assess*.go`, `docs/assess.md` and `samples/assess-*/reports/`;
- every caller of the changed APIs, including `internal/llm/redact` and `internal/app/llm*.go`.

Environment constraints (not defects of the code):

- The sandbox has no Go toolchain and no package registry. `go build` and `go test` were not run, by me or by the
  fixer. Compile safety was judged by reading every caller of a changed symbol.
- I verified platform claims against Microsoft Learn and the APRL repository. The URLs are cited inline.

Round-1 note: in round 1 I counted 131 rule packets in a different working copy. The canonical tree has 108 rule
files (`rules/catalog/*/*.yaml`). The hand-off's corrected count of 108 is right for this tree.

## 1. Round-1 findings: verification

| ID | Status | Evidence |
|---|---|---|
| B1 partial evidence becomes PASS | **CLOSED** | `assess.stateFor`: `case c.Evaluated && len(hidden) > 0` returns `UNKNOWN` ("Partial evaluation") before the PASS case. The control is listed in `Unassessed`. Test: `TestPartialSkipIsNeverPass`, `TestInfoWithPartialSkipIsUnknown`. |
| B2 any answer becomes PASS | **CLOSED** for the question path | `answerState`: only boolean `true` plus a non-empty `disasterRecovery.reference` gives `ATTESTED` (basis `attested`); `false` gives `WARNING` (attested); anything else stays `QUESTION`. Attested answers have their own section in owner Markdown and HTML. Test: `TestQuestionAnswers`. **But** the same class of defect returns through `NOT_APPLICABLE`; see NB1. |
| B3 allow-list, tautological test, no mapping file | **CLOSED** | `internal/assess/waf/mapping.yaml` (version 1, `pillarSource` cited) has 78 mapped and 30 excluded entries, each excluded rule with a reason. `waf.Validate` rejects a missing or duplicate rule, a pillar mismatch, an excluded rule scheduled in phases 1-4, a dropped or deprecated rule that is mapped, and a costly entry that is must-have or unsourced. `TestMappingCoversEveryCatalogueRule` checks against the catalogue independently of `Definitions`. `TestPhaseOneToThreeRulesAreMapped` passes. WAF checklist codes are deferred, with disclosure (ADR-026 §2 and a limitation line in every report). |
| B4 HTML identical, no sources or labels | **CLOSED** | `htmlreport.RenderOwner` and `RenderDeveloper` render separate templates. The developer page has provenance (`prov`), `lastVerified`, the opinion label, question, WARA, overlap IDs, evidence with location and suppression metadata, fix, and limitations (`foot`). The owner page has top five actions, costly items, attested answers, unassessed scope and limitations. Test: `TestAssessWAFHTMLAudiencesDiffer`, plus the sample invariant. |
| B5 opinion unlabelled; costly not separated or sourced | **CLOSED** | `Action.ProjectOpinion` is rendered through `common.Provenance` and HTML `prov`. A "Costly recommendations (optional)" section appears in the owner, HTML and evidence views. Cost flags in `mapping.yaml` have a basis, source and date, and `Validate` forbids must-have entries. I spot-checked two cost sources and both hold: [search-sku-manage-costs](https://learn.microsoft.com/azure/search/search-sku-manage-costs) says "you pay for the minimum required replica and partition combination (R × P)" and "Customer-managed keys … require a billable Azure Key Vault". [managed-virtual-network](https://learn.microsoft.com/azure/foundry/how-to/managed-virtual-network) says FQDN rules in Allow Only Approved Outbound mode create "a managed Azure Firewall … with associated Firewall costs". |
| B6 malformed WARA ID, not validated | **CLOSED** | `wara/mapping.go` uses the full `dff62efe-c3a3-4621-98b3-c877c65cb195`, which matches the APRL Search file ([recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Search/searchServices/recommendations.yaml)). It carries labels, `pgVerified` and the source URL. `TestReferencesAreWellFormedAndSourced` checks GUID format and cross-checks each ID against the source note. |
| B7 hand-off not reproducible; no sample reports | **CLOSED as a disclosure** (accepted under this loop's gate policy) | The toolchain and gate claims are withdrawn, every DoD row is "unmet (pending)", and reviews are marked pending. Sample reports exist for each audience and format, marked hand-derived (see §3). |
| N1 FAIL wording | CLOSED | `docs/assess.md` state table |
| N2 LLM flags; "verified" wording | CLOSED | `docs/assess.md` lines 34-42 and 96 |
| N3 NoEvidenceState over skips | CLOSED | Skips are handled before `NoEvidenceState` in `stateFor`. `stateReason` is rendered. |
| N4 `--strict` help | CLOSED | `cmd/foundry-doctor/assess.go:53` |
| N5 evidence-pack audit metadata | CLOSED (spot-checked) | `evidence/report.go` renders the fingerprint, generated time and suppression metadata. The JSON adds `toolVersion`, `generatedAt`, `scopeNote`, `unassessed` and `costlyRecommendations`. |
| N6 deployed evidence not named | CLOSED | `app.assessLimitations` lists the phase 2/3 controls that were not evaluated. Deployed mode is deferred (ADR-026 §7). |
| N7 PSRule/Advisor/Defender not recorded | CLOSED | Limitation line in every report; ADR-026 §6 |
| N8 pillar names | CLOSED | `assess.PillarName` matches the names on the [WAF pillars page](https://learn.microsoft.com/azure/well-architected/pillars). |
| N9 HTML a11y and dark mode | CLOSED | `caption`, `scope`, `prefers-color-scheme` palette |
| N10 status filter | CLOSED | `Validate` rejects mapped dropped or deprecated rules. `scheduledForAssessment` ignores proposed rules. |

## 2. Compile-safety review of changed contracts

I searched the whole module for every changed symbol. Results:

- `waf.Definitions` now returns `([]assess.Definition, error)`. Its only callers are `internal/app/assess.go:116`,
  which handles the error, and `waf/mapping_test.go`, which does too.
- `waf.Applicable` has no remaining references.
- `htmlreport.Render` has no remaining references. `internal/app/assess.go:262,267` call `RenderOwner` and
  `RenderDeveloper`.
- `WARALookups`, `WARAReferences` and `Assessment.Costly` changed type. The only users are `internal/assess`, the
  renderers, their tests and `app/assess.go`. All use the new types (`[]assess.WARARef`, `[]assess.Action`).
- `internal/llm/redact/project.go`, `internal/app/llm.go`, `llm_narrator.go` and `llm_test.go` use only
  `assess.Assessment`, `Control.{RuleID,State,Recommendation,Findings,Skipped}` and the constants `StatePass`,
  `StateFail`, `StateWarning`, `StateQuestion`, `StateUnknown` and `StateSkipped`. All of these still exist with
  unchanged types.
- `internal/assess` now imports `internal/rules`. `internal/rules` imports only `armexpr`, `catalog`, `findings` and
  `sdk`, so there is no import cycle.
- `rules.ReasonHead`, `rules.ClassifySkip`, `rules.SkipByDesign`, `sdk.SkipSyntheticInfrastructure` and
  `sdk.SkipProfileKeyPrefix` all exist.
- The HTML templates reference fields that exist: `sdk.Suppression` has `Reason`, `Owner` and `Expires`; `Action`
  and `Control` both have `ProjectOpinion`, `SourceURL`, `LastVerified`, `CostBasis`, `CostSourceURL` and
  `CostVerified`.
- `go.yaml.in/yaml/v3` is already in `go.mod`.
- The cmd test helpers `sample`, `noBicep` and `run`, and `app.Services.Now`, all exist.

I found no code that would not compile. As the fix log says, `gofmt` and `vet` remain unverified.

## 3. Sample-report disclosure

The disclosure is adequate for this gate:

- Each `samples/assess-*/reports/` directory has a `HAND-DERIVED` marker and a `README.md` stating "hand-derived,
  not engine output", with the regeneration command.
- `docs/assess.md` §"Sample fixtures and reports" says the same.
- `TestAssessSampleReports` fails while the marker exists and, after regeneration, works as a golden test with
  invariant checks: no PASS without deterministic evaluation, ATTESTED only with an attested basis, limitations
  present, owner and developer views differ.
- The hand-off keeps the deliverable and DoD rows unmet.

The hand-derived content is already known to differ from what the engine will produce. For example,
`FND-OPS-002` is shown as SKIPPED with an "(estimated reason)", but `ops.eval002` always returns a by-design skip
(see NB1). The failing test guarantees that this is corrected at regeneration. See N1 for the remaining risk.

## 4. New blocking findings

### NB1 `NOT_APPLICABLE` turns team declarations and missing requirements into deterministic not-applicable, including for mandatory controls

- **Where:** `internal/assess/assess.go`, `notApplicable` and the `StateNotApplicable` branch of `stateFor`. They
  reuse `rules.ClassifySkip == SkipByDesign` (`internal/rules/skipclass.go`, `byDesignHeads`). That list was
  written to decide whether `--strict` exits 3 (ADR-010 decision 9), not to decide whether a WAF control applies.
- **Why it is wrong:** besides genuine "nothing to check" heads (`not-applicable: …`, `project-has-no-agent-services`),
  `byDesignHeads` contains:
  - `policy-managed`, emitted by **FND-OPS-001** (must-have, diagnostic settings; `rules/ops/ops.go:454`) and
    **FND-NET-003** (must-have, private DNS zone group; `rules/net/rules.go:319`). These fire when the team writes
    `managedByAzurePolicy: [...]` or `network.dnsManagedByPolicy: true` in configuration. The control then becomes
    `NOT_APPLICABLE` with basis **`deterministic`**. It drops out of the unassessed list and is ignored by
    `decisionFor`. A self-declared, unverified statement therefore clears a **mandatory** control and reads as
    tool-verified. This is the defect closed for FND-REL-007 in B2, reached by another path, and it contradicts
    ADR-026 §3 ("ATTESTED comes only from a recorded team answer"; "PASS, INFO and FAIL/WARNING come only from
    deterministic rule evidence").
  - `no-explicit-observability-or-server-tracing-requirement`: `ops.eval002` (**FND-OPS-002**) returns this
    unconditionally (`rules/ops/ops.go:556-557`). The control is always `NOT_APPLICABLE`, never in the unassessed
    list, and never in the costly section, even though `mapping.yaml` flags it as costly.
  - `no-resilience-objective-declared`: `rel.eval009` (**FND-REL-009**, `rules/rel/rel.go:597`) emits this whenever
    the team has not declared an APIM SLA, zone or region objective, before checking whether APIM exists. "No
    objective recorded" is the WAF question "define reliability and recovery targets" (RE:04,
    https://learn.microsoft.com/azure/well-architected/reliability/checklist). It is not non-applicability.
  - The result breaks the DoD item "Unknown/non-machine-verifiable controls are visible" and the PRD §12 rule that
    controls needing business or operational context "remain UNKNOWN or QUESTION rather than falsely passing".
    `NOT_APPLICABLE` is excluded from every outstanding or unassessed list and from the decision, so it behaves
    like a pass.
- **Fix:** do not derive WAF applicability from the `--strict` classifier. In `assess`, keep a separate,
  explicit classification:
  - `policy-managed` becomes `ATTESTED` (basis `attested`, with the policy key shown in "Team-attested answers").
    For a must-have control, a reasonable choice is that it stays outstanding as `QUESTION` until deployed policy
    evidence exists. Decide which and record it in ADR-026.
  - "No requirement or objective declared" heads (`no-*-declared`, `no-explicit-*-requirement`) become `QUESTION`,
    with a prompt naming the missing policy key.
  - Only `not-applicable:` and the "project has no such resource" heads and details become `NOT_APPLICABLE`.
  - Add tests for FND-OPS-001 or NET-003 with `policy-managed` (not NOT_APPLICABLE; not deterministic; mandatory
    control stays visible in the decision) and for OPS-002 and REL-009 (QUESTION, listed in the unassessed
    scope).

### NB2 The `--llm-explain` projection sends INFO, ATTESTED and NOT_APPLICABLE controls as findings to narrate

- **Where:** `internal/llm/redact/project.go:70`, which skips only `StatePass`, and `rank()`, where the new states
  fall into `default: 5`. The caller is `internal/app/assess.go`, `assessmentNarrative`. The hand-off records this
  but does not fix it.
- **Privacy:** **no new exposure.** The projection still sends only the catalogue description and recommendation,
  plus `controlEvidence`, which carries finding locations and skip reasons that pass through `findings.Redact`,
  ARM-ID, GUID and host redaction. `QuestionAnswer`, including the DR plan reference URL, is never projected.
  NOT_APPLICABLE skip reasons go through the same redaction as SKIPPED reasons already did.
- **Correctness: defect.** `llm.Finding` has no state field (`internal/llm/types.go:36-41`), and the projection
  error text calls the payload "non-pass findings". Once the higher-ranked states leave free slots (at most 10),
  the model is handed ATTESTED, NOT_APPLICABLE or INFO controls as problems to explain. The narrative appended to
  the owner or developer report then contradicts the deterministic state shown above it. PRD §17 lists "Narrative
  contradicts findings" as a key risk, with "status immutability" as the mitigation. This change of meaning was
  introduced by the Phase 4 state change and reaches users through the Phase 4 command, so Phase 4 must not ship
  it.
- **Fix:** in `redact.Assessment`, project only outstanding or unassessed states, using
  `assess.Outstanding(c.State) || c.State == assess.StateSkipped` (or exclude PASS, INFO, ATTESTED and
  NOT_APPLICABLE explicitly). Add a test in `project_test.go` showing that ATTESTED, NOT_APPLICABLE and INFO
  controls are not projected and that a payload containing only them returns `llm.ErrUnavailable`. If the Phase 9
  owner prefers to project INFO, add the state to `llm.Finding` instead and version the prompt.

## 5. Non-blocking findings

- **N1** The hand-derived sample files carry no in-file banner. Someone opening `owner.md` or `owner.html`
  directly, for example from a link in `docs/assess.md`, sees no "hand-derived" warning. Add a one-line banner at
  the top of each file until regeneration. The failing test makes this low-risk.
- **N2** By design, `TestAssessSampleReports` keeps `go test ./...` red until a maintainer regenerates the samples.
  Record this in the CI/release checklist (`docs/development/ci.md`), so that a red run is not mistaken for a
  regression and a regeneration without review is not merged silently.
- **N3** Cost candidates marked UNVERIFIED in ADR-026 §5:
  - FND-NET-010 (Private Link and AMPLS). The [managed-virtual-network](https://learn.microsoft.com/azure/foundry/how-to/managed-virtual-network)
    page states "you're charged for … Azure Private Link", which is primary-source support for flagging it.
  - FND-REL-009 (APIM Premium for zones and multi-region) is neither flagged nor listed as a candidate.
  - Either flag both with sources or list them as UNVERIFIED.
- **N4** `TestEvaluatedWithNotApplicableSkipPasses` shows that an evaluated rule with a not-applicable skip is
  PASS. That is correct, but `stateReason` still says "no skipped check". Mention the not-applicable skip in the
  reason text.

## 6. Unsupported assumptions

- Pillar assignments still come only from the catalogue `pillar` field. The per-pillar WAF service-guide citations
  are deferred by ADR-026 §2 and disclosed in every report. This is accepted for this phase.
- The question prompts, decision wording and rank weights are labelled project opinion in the reports and docs.
  This is accepted.
- The equivalence between the `--strict` skip classification and WAF applicability is assumed without a source or
  ADR rationale (NB1).

## 7. Missing tests

- `policy-managed`, `no-resilience-objective-declared` and `no-explicit-observability-or-server-tracing-requirement`
  skips through `assess.Build` (NB1).
- `redact.Assessment` with ATTESTED, NOT_APPLICABLE and INFO controls (NB2).
- All other round-1 missing tests are now present as listed in the hand-off. They have not been executed (no
  toolchain).

## 8. Verdict

Phase 4 DoD (PRD §12), as of this tree:

| DoD item | Status |
|---|---|
| No report claims complete WAF compliance | met in code (base limitations in every view) |
| Unknown/non-machine-verifiable controls are visible | **unmet**: NB1 (OPS-001, NET-003 via `policy-managed`; OPS-002, REL-009 via undeclared requirements) |
| Every recommendation links to a verified source or is labelled project opinion | met in code |
| All three audiences receive materially different, useful views | met in code; the `--llm-explain` appendix can contradict the state shown (NB2) |
| Suppressed and baselined controls remain visible in the evidence pack | met in code |

Gates were not run and the sample reports are hand-derived. Both are disclosed and accepted under this loop's
gate policy, and both must be resolved before release. The rejection rests on NB1 and NB2 only. Each is a small,
local change with tests.

VERDICT: REJECT
