# Phase 4 review: Foundry lead, round 3

Reviewer: `foundry-lead` (independent; did not write the code). Date: 2026-10-18. Round 3 (final loop).

## Scope

- The "Loop 2" section of `docs/development/reviews/phase-4-fixlog-loop1.md`.
- ADR-026 item 3 as amended.
- `internal/assess/assess.go` and `assess_test.go`.
- `internal/report/owner` and `internal/report/html`.
- `internal/assess/waf/mapping.yaml`.
- `internal/llm/redact/project.go` and `project_test.go`.
- `internal/app/assess.go`, `llm_narrator.go` and `llm_test.go`.
- `docs/assess.md`, `samples/assess-*/reports/` and the Phase 9 hand-off note.

## Environment constraints

- No Go toolchain and no registry access in this sandbox. Nothing was compiled or run, by me or by the fixer.
- I judged compile safety by reading every changed declaration and every caller.

## 1. Round-2 blockers

### NB1: CLOSED

`NOT_APPLICABLE` was derived from the `--strict` classifier, so team declarations and missing requirements showed as not applicable.

- **Own classifier.** `assess.classifySkip` (`internal/assess/assess.go`) now has its own explicit lists. `assess` no longer imports `internal/rules`; its imports are `fmt`, `slices`, `strings`, `catalog` and `sdk`. `rules.ClassifySkip` and `--strict` are unchanged.
- **Delegated settings become ATTESTED.** `policy-managed` and `central-dns` give `skipDelegated`, which maps to `ATTESTED` with basis `attested`. The reason reads "Delegated to Azure Policy by configuration; assignment and compliance not verified". This covers both emitters: `rules/ops/ops.go:454` (FND-OPS-001) and `rules/net/rules.go:319,322` (FND-NET-003, both heads).
- **Mandatory delegated controls stay conditional.** `decisionFor` adds `c.State == StateAttested` to the mandatory "Proceed with conditions" case. The owner Markdown (`owner/report.go:55-66`) and owner HTML (`html/report.go:89-93`) list every attested control, marked mandatory or optional, with the reason.
  - A delegated must-have control is no longer reported as deterministic, and it no longer lets the decision move off "conditions".
  - This is the treatment chosen in ADR-026 item 3. I accept it: the state is visible and labelled unverified, and it never clears the decision.
- **Undeclared requirements become QUESTION.** Heads matching `no-*-declared` or `no-*-requirement` give `QUESTION` with basis `none`. They are listed as unassessed and kept in the costly section when flagged.
  - FND-OPS-002 (`ops.eval002`, unconditional skip) and FND-REL-009 (`no-resilience-objective-declared`) now become QUESTION.
- **NOT_APPLICABLE is narrow.** Only `not-applicable:`, the listed "no such resource or scope" heads, and the four input-unavailable "project has no …" details give `NOT_APPLICABLE`. Every unknown head falls back to `skipHidden`, which is SKIPPED or UNKNOWN. That fails safe.
- **Tests:** `TestPolicyManagedIsAttestedNotApplicable`, `TestUndeclaredRequirementIsQuestion` and `TestClassifySkip`.
- **Docs:** `docs/assess.md` (state table and decision rule, lines 58-70) and ADR-026 item 3 match the code.

### NB2: CLOSED

The LLM projection used to send settled states as findings.

- **Filter.** `redact.Assessment` projects only `assess.Outstanding(state) || state == StateSkipped`, that is FAIL, WARNING, UNKNOWN, QUESTION and SKIPPED. A payload with nothing that qualifies returns a wrapped `llm.ErrUnavailable`. Test: `TestAssessmentProjectionSkipsSettledStates`, which also checks the settled-only `ErrUnavailable` path.
- **Privacy is unchanged.** No new fields are projected. `QuestionAnswer` and the DR reference are never sent.
- **The projection runs before any provider call.** `lazyNarrator.ExplainAssessment` calls `ResolveConfig`, then `redact.Assessment`, then the prompt, then `provider.Complete`. A settled-only assessment therefore sends nothing to the model.
- **The app path handles `ErrUnavailable` gracefully.** In `app.assessmentNarrative`, any error that is not `context.Canceled` or `DeadlineExceeded` becomes the note "LLM advisory unavailable (…). Deterministic findings remain authoritative.", with a nil error.
  - `assessWAF` appends that note through `appendRenderedNarrative`.
  - It returns the `code` computed earlier from the deterministic findings and skips (`ExitCode(Outcome{…})`), so the exit code is unchanged.
  - The deterministic report bytes are emitted unchanged, with the note appended.
  - The existing `internal/app/llm_test.go` assess test (narrator error, so `ExitFindings` is kept and the note is present) exercises the same branch.
- The Phase 9 hand-off records the change (`hand-offs/phase-9.md:46-50`).

## 2. Compile safety (loop 2 changes)

- **`internal/assess/assess.go`.** The new unexported `skipKind`, `classifySkip`, `notApplicableHeads`, `notApplicableInputDetails` and `delegatedHeads` reference only `sdk.SkipNotApplicable`, `sdk.SkipInputUnavailable`, `slices` and `strings`, which all exist. The removed import of `internal/rules` leaves no dangling use: `ReasonHead` and `ClassifySkip` are no longer referenced in the package.
- **`assess_test.go`, `TestClassifySkip`.** The map literal has 13 distinct keys, so there is no duplicate-constant-key compile error. It uses `sdk.SkipMissingPolicyKey`, `SkipNotImplemented`, `SkipSyntheticInfrastructure` and `SkipInputUnavailable`, which all exist. As the fix log says, gofmt alignment is hand-done.
- **`redact/project.go`.** `assess.Outstanding` and `assess.StateSkipped` are exported. `project_test.go` imports `errors`.
- **Signatures.** No exported signature changed in loop 2. The round-2 checks of `waf.Definitions`, `RenderOwner`/`RenderDeveloper` and the WARA and Costly types still hold. The HTML template change uses `.Mandatory`, `.QuestionAnswer` and `.StateReason`, which all exist on `assess.Control`.

## 3. Regression check of earlier findings

| Round 1 | Status | Evidence |
|---|---|---|
| B1 | still CLOSED | `len(hidden) > 0 && c.Evaluated` returns UNKNOWN as the first non-finding case. |
| B2 | still CLOSED | The `answerState` question path is unchanged, and delegations are now also attested, not deterministic. |
| B3 | still CLOSED | The mapping file and `Validate` are unchanged. NET-010 is now a sourced costly entry: an optional rule, with a basis and a date. |
| B4 | still CLOSED | Owner and developer HTML are still separate. The owner HTML adds mandatory/optional to the attested list. |
| B5 | still CLOSED | No change. |
| B6 | still CLOSED | No change. |
| B7 | still CLOSED as a disclosure | No change. |
| N1–N10 | still CLOSED | No change. |

Round-2 non-blocking findings:

| Round 2 | Status | Evidence |
|---|---|---|
| N1 (in-file banner) | CLOSED for Markdown; see N1 below for HTML | Banner present in the Markdown and HTML files. |
| N2 (CI checklist) | OPEN, disclosed | Recorded in the hand-off for the maintainer. |
| N3 | PARTIALLY | NET-010 is flagged with a source; REL-009 is listed as UNVERIFIED in ADR-026. |
| N4 | CLOSED | The PASS reason now names the not-applicable checks. |

## 4. New findings

There are no blocking findings.

- **N1 (non-blocking).** The HTML sample banner is an HTML comment (`<!-- HAND-DERIVED … -->`), so it does not show in a browser. Use a visible `<p>` until regeneration. Regeneration removes it in any case.
- **N2 (non-blocking).** No app-level test sends an `ErrUnavailable` from the projection itself through `AssessWAF`. The branch is the same one the existing narrator-error test covers, so this is a coverage gap, not a defect.

## 5. Verdict

Every round-1 and round-2 blocking finding is closed. I found no code that would not compile and no regression.

Before release, the maintainer must still:

- run the strict gate with Go 1.26.9;
- regenerate and review the sample reports, which removes `HAND-DERIVED` and lets `TestAssessSampleReports` pass;
- update the hand-off DoD rows.

All of these are disclosed in the hand-off and ADR-026, and they are accepted under this loop's gate policy.

VERDICT: APPROVE
