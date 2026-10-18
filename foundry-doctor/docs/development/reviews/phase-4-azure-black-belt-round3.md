# Phase 4 review: Azure black belt, round 3 (final loop)

Reviewer: `azure-black-belt` (independent, read-only, did not author the code). Date: 2026-10-18. Round 3.

Inputs:
- `REVIEW_BRIEF.md`
- the "Loop 2" section of `phase-4-fixlog-loop1.md`
- ADR-026 item 3 (amended)
- `internal/assess/assess.go` and `assess_test.go`
- `internal/llm/redact/project.go` and `project_test.go`
- `internal/app/{assess.go,llm_narrator.go,llm_render.go,llm_test.go}`
- `internal/report/owner/report.go`
- `internal/assess/waf/mapping.yaml`

Environment: this sandbox has no Go toolchain and no access to a package registry, so nothing was compiled or run. I
reviewed by reading the code. Per the gate policy, the hand-off honestly states that the gates were not run, and that
is accepted.

## Round-2 findings

| Id | Status | Evidence |
|---|---|---|
| NB1 delegated skips reported as deterministic NOT_APPLICABLE | **CLOSED** | See the notes below this table. |
| N1 INFO reason overclaims | **CLOSED** | The reason now reads "Evaluated; informational at this profile's severity, see evidence." |
| N2 LLM projection sends settled states | **CLOSED** | `project.go` projects only `Outstanding(state) \|\| SKIPPED`. A payload that contains only settled states returns a wrapped `llm.ErrUnavailable`. Test: `TestAssessmentProjectionSkipsSettledStates` checks PASS, INFO, ATTESTED and NOT_APPLICABLE are dropped, only QUESTION is sent, and settled-only input gives `ErrUnavailable`. `errors` is imported in the test. |
| N3 no banner in hand-derived files | **CLOSED** for Markdown and HTML | JSON cannot carry a banner; the README says so. Acceptable. |
| N4 REL-003 cost basis | **CLOSED** | The basis is narrowed to continuous backup, which I verified in round 2. |
| N5 central-dns consistency | **CLOSED** | `delegatedHeads = {"policy-managed","central-dns"}`. |

Notes on NB1:
- `assess.classifySkip` now has its own lists and no longer imports `internal/rules`. The only remaining match for
  `rules.` in `internal/assess` is a comment at `assess.go:412`.
- `policy-managed` and `central-dns` give `ATTESTED` with basis `attested` and the reason "Delegated to Azure Policy by
  configuration; assignment and compliance not verified (…)".
- `decisionFor` now counts `c.Mandatory && c.State == StateAttested` as "Proceed with conditions", so FND-OPS-001 and
  FND-NET-003 can no longer reach "Proceed within assessed scope" on a declaration alone.
- The owner view lists these controls under "Team-attested answers" and marks them mandatory (`owner/report.go:61-66`).
- `no-*-declared` and `no-*-requirement` skips give `QUESTION`, which counts as unassessed and outstanding.
- Only "no such resource or scope" heads give `NOT_APPLICABLE`.
- Tests: `TestPolicyManagedIsAttestedNotApplicable`, `TestUndeclaredRequirementIsQuestion`, `TestClassifySkip`
  (`assess_test.go:112-175`).
- `internal/rules/skipclass.go` is unchanged, so `--strict` behaviour is unchanged.

## Compile check

- `assess.go` imports `fmt`, `slices`, `strings`, `catalog` and `sdk`, and all five are used. `internal/rules` was
  removed and nothing still references it.
- The new unexported identifiers (`skipKind`, `skipHidden`, `skipNotApplicable`, `skipDelegated`, `skipUndeclared`,
  `classifySkip`, `notApplicableHeads`, `notApplicableInputDetails`, `delegatedHeads`) are defined once. In
  `assess_test.go` they are used with matching types (`map[string]skipKind`).
- `strings.Cut` is used correctly (`head, detail, _`).
- No exported signature changed in loop 2. `redact.Assessment`, `assess.Outstanding` and `assess.StateSkipped` all
  exist. `lazyNarrator.ExplainAssessment` calls `redact.Assessment(rules, a, audience)` with an unchanged signature.
- `mapping.yaml`: the new costly entry FND-NET-010 is `nice-to-have`, so `Validate`'s must-have guard passes. It has a
  basis, an https source and a date.

I found no compile breaks. gofmt alignment of the new map literal in `assess_test.go` was checked by hand only, and the
hand-off says so.

## ErrUnavailable from the projection, phase-9 path

The error passes through these steps:
1. `redact.Assessment` returns `fmt.Errorf("%w: …", llm.ErrUnavailable)`.
2. `lazyNarrator.ExplainAssessment` returns the error unchanged (`llm_narrator.go:83-86`).
3. `app.assessmentNarrative` (`assess.go:~240-256`) returns it only if it is `context.Canceled` or
   `DeadlineExceeded`. Any other error becomes the note "LLM advisory unavailable (…). Deterministic findings remain
   authoritative." with a nil error.
4. `assessWAF` computes the exit code from the deterministic findings before the narrative step. `appendRenderedNarrative`
   (`llm_render.go:16-41`) appends only the note, and a nil narrative is handled.

The result is a graceful advisory-unavailable note. The deterministic report and the exit code are unchanged, and no
provider call is made for a settled-only assessment. `llm_test.go:98` covers this path with a wrapped `ErrUnavailable`
from the narrator.

## Regression check of earlier findings

- R1 B1 (INFO): unchanged and intact. INFO stays out of top actions, the costly list and the decision.
- R1 B2 (costly): intact. The must-have guard still holds, and costly items are still kept out of the top actions.
- R1 B3 (mapping completeness): intact.
- R1 B4 (partial skip): intact and stronger. A hidden skip on an evaluated rule gives `UNKNOWN`. Evaluated plus only
  not-applicable skips gives `PASS`, with the not-applicable checks named.
- R1 B5 (DR answer): `answerState` is unchanged.
- R1 B6 (HTML): unchanged. The HTML now also includes the ATTESTED wording.
- R1 B7 (disclosure): intact, and banners were added.
- R1 N1–N10: no regressions found.

## Non-blocking

- **N1** `input-unavailable: no dependency is proven private-endpoint backed` is now `skipHidden`. Under the old
  `--strict` list it was by design, so FND-RUN-002 shows as `SKIPPED` rather than `NOT_APPLICABLE`. This is conservative
  and acceptable offline. Revisit it when a deployed mode exists (ADR-026 item 7).
- **N2** The `no-*-declared` and `no-*-requirement` heuristic is pattern-based. A future rule that emits such a reason
  for a resource that does not exist would become `QUESTION` instead of `NOT_APPLICABLE`. That fails safe. Keep
  `TestClassifySkip` in step with new reasons.
- **N3** The FND-NET-010 cost basis matches the source: the Azure Monitor private link article points to Azure Private
  Link pricing for cost (https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-security).

## Verdict

All round-2 blockers are closed with code and tests. I found no compile breaks and no regressions. The LLM
`ErrUnavailable` path degrades gracefully without changing the deterministic output or the exit code. The remaining
DoD evidence (a Go gate run and regenerated sample reports) is honestly disclosed as pending under the gate policy for
this loop.

VERDICT: APPROVE
