# Phase 4 fix log, loop 1

Date: 2026-10-18. Author: go-implementer (fix loop 1). Reviews addressed: `phase-4-foundry-lead-round1.md` (FL) and
`phase-4-azure-black-belt-round1.md` (BB). No Go toolchain was available: nothing was compiled or tested in this loop.

## Blocking findings

| Finding | Status | Files changed |
|---|---|---|
| FL-B1 partial evidence can PASS | FIXED: evaluated + non-applicability skip → UNKNOWN ("Partial evaluation"), listed in unassessed; tests | `internal/assess/assess.go`, `assess_test.go`, `internal/app/assess_test.go` |
| FL-B2 any answer becomes PASS | FIXED: value evaluated (true + non-empty reference → ATTESTED; false → WARNING; other → QUESTION); `basis: attested` shown in every view; tests for false, missing/blank reference, non-boolean | `internal/assess/assess.go`, `waf/mapping.yaml`, renderers, tests |
| FL-B3 allow-list, tautological completeness test, no mapping file | FIXED: `waf/mapping.yaml` (versioned) maps or excludes every catalogue rule; CFG/ENV/DEP/RUN mapped; `Validate` + test independent of the selection; negative tests. WAF checklist codes deferred (ADR-026) | `internal/assess/waf/*`, ADR-026 |
| FL-B4 HTML identical, no sources/opinion/limitations | FIXED: `RenderOwner` and `RenderDeveloper`; developer HTML has source, date, opinion label, question, WARA, evidence, location, fix, limitations; documented accessibility checklist test | `internal/report/html/*`, `internal/app/assess.go` |
| FL-B5 opinion unlabelled, costly not separated, unsourced costly list | FIXED: `Action.ProjectOpinion` rendered everywhere via `common.Provenance`; "Costly recommendations (optional)" in owner, HTML and evidence; cost flags in mapping file with basis/source/date; must-have never costly | `internal/assess/*`, renderers |
| FL-B6 malformed WARA ID, no validation | FIXED: full GUID; GUID-format, pgVerified, reliability/wara-basis and source-note cross-check tests; catalogue note corrected | `internal/assess/wara/*`, `rules/catalog/rel/FND-REL-007.yaml` |
| FL-B7 hand-off not reproducible, no sample reports | PARTLY FIXED / ADR-026: hand-off rewritten (no gate claims, DoD unmet, reviews pending, count corrected); sample reports added for all audiences/formats but **hand-derived**; `TestAssessSampleReports` fails until regenerated with Go. Strict gate NOT run (no toolchain) | `docs/development/hand-offs/phase-4.md`, `samples/assess-*/reports/*`, `cmd/foundry-doctor/assess_samples_test.go` |
| BB-B1 info findings become WARNING | FIXED: INFO state; excluded from top actions, costly list and decision; test with CMK posture | `internal/assess/assess.go`, `assess_test.go` |
| BB-B2 costly not separated; pillar==cost; OPS-001 | FIXED: see FL-B5; COST-001/002 and OPS-001 not costly (test) | `waf/mapping.yaml`, `waf/mapping_test.go` |
| BB-B3 self-defined applicability, circular test | FIXED: see FL-B3 | as FL-B3 |
| BB-B4 partial skip PASS; not-applicable forces conditions | FIXED: UNKNOWN for partial; NOT_APPLICABLE state not counted by the decision (test) | `internal/assess/assess.go`, tests |
| BB-B5 declared:false → PASS; IDN-005/OPS-008 unanswerable | FIXED for REL-007; ADR-026 for IDN-005/OPS-008 keys (documented in `docs/assess.md`) | `internal/assess/assess.go`, ADR-026, docs |
| BB-B6 HTML breaks two DoD items | FIXED: see FL-B4 | as FL-B4 |
| BB-B7 missing samples; PSRule/Advisor undisclosed | PARTLY FIXED / ADR-026: limitation line in every report, docs and hand-off; overlap IDs shown "not ingested"; samples hand-derived (see FL-B7) | `internal/app/assess.go`, docs, ADR-026 |

## Non-blocking findings

| Finding | Status | Notes |
|---|---|---|
| FL-N1 / BB-N6 FAIL wording | FIXED | `docs/assess.md` says FAIL is any active error finding |
| FL-N2 LLM flags undocumented; "verified" source wording | FIXED | flags documented; wording now "first catalogue source and its lastVerified date" |
| FL-N3 NoEvidenceState over skips | FIXED | skips take precedence; reason shown in `stateReason` |
| FL-N4 --strict help | FIXED | help text matches `rules.AnyHiddenCheck` |
| FL-N5 / BB-N7 evidence pack audit metadata | FIXED | suppression reason/owner/expiry, fingerprint, rule version, source/date, tool version, generatedAt, scope, unassessed, costly, limitations (Markdown and JSON) |
| FL-N6 / BB-N10 deployed evidence not named | FIXED | limitation lists unevaluated phase 2/3 controls; deployed mode deferred (ADR-026) |
| FL-N7 PSRule/Advisor/Defender not recorded | FIXED (ADR-026) | |
| FL-N8 / BB-N4 pillar names | FIXED | `assess.PillarName` used in every view |
| FL-N9 HTML caption/scope/dark mode | FIXED | caption, `scope`, dark-mode palette |
| FL-N10 status filter | FIXED | `Validate` rejects mapped dropped/deprecated rules |
| BB-N1 truncated WARA ID | FIXED | see FL-B6 |
| BB-N2 WARA both directions | FIXED | test |
| BB-N3 WARA labels / pgVerified, e35cf148 | FIXED | labels from catalogue notes; e35cf148 labelled "platform migration, context only" |
| BB-N5 control IDs are rule IDs | FIXED (documented) / ADR-026 for checklist codes | |
| BB-N8 multi-line fix | FIXED | `common.Fenced` |
| BB-N9 profile next to decision in HTML | FIXED | |

## Unsupported assumptions raised by reviewers
- Costly classification: now sourced per rule (Microsoft Learn, 2026-10-18); three candidates left unflagged as UNVERIFIED.
- Question prompts, decision wording and ranking: labelled project opinion in reports and docs.
- Pillar assignments: taken from the catalogue; WAF service-guide citations deferred (ADR-026).

## Compile risks to check first
- gofmt alignment (hand-checked only); `go vet` on the new `html/template` definitions; `internal/assess` now imports
  `internal/rules` (no cycle found by inspection).

## Loop 2

Date: 2026-10-18. Reviews addressed: `phase-4-foundry-lead-round2.md` (FL2) and `phase-4-azure-black-belt-round2.md` (BB2).
No Go toolchain: nothing compiled or run.

| Finding | Status | Files changed |
|---|---|---|
| FL2-NB1 / BB2-NB1 NOT_APPLICABLE from the `--strict` classifier | FIXED: `assess.classifySkip` (own lists; `internal/rules/skipclass.go` untouched, `assess` no longer imports `internal/rules`). `policy-managed`/`central-dns` → ATTESTED, basis attested, reason "Delegated to Azure Policy by configuration; assignment and compliance not verified"; mandatory ATTESTED keeps "Proceed with conditions" and is listed (mandatory) in owner "Team-attested answers" (chosen treatment, ADR-026 item 3). `no-*-declared` / `no-*-requirement` → QUESTION (OPS-002, REL-009 listed as unassessed). Only "no such resource/scope" → NOT_APPLICABLE. Tests: `TestPolicyManagedIsAttestedNotApplicable` (OPS-001, NET-003 policy-managed and central-dns), `TestUndeclaredRequirementIsQuestion`, `TestClassifySkip` | `internal/assess/assess.go`, `assess_test.go`, `waf/mapping.yaml`, `internal/report/owner/report.go`, `internal/report/html/report.go`, ADR-026, `docs/assess.md` |
| FL2-NB2 / BB2-N2 LLM projection sends settled states | FIXED (filter only; `llm.Finding` unchanged to keep the Phase 9 schema): only FAIL/WARNING/UNKNOWN/QUESTION/SKIPPED are projected; settled-only payload → `ErrUnavailable`. Test `TestAssessmentProjectionSkipsSettledStates`. Noted in the Phase 9 hand-off | `internal/llm/redact/project.go`, `project_test.go`, `docs/development/hand-offs/phase-9.md` |
| FL2-N1 / BB2-N3 no in-file banner | FIXED for Markdown and HTML; JSON cannot carry one (README says so) | `samples/assess-*/reports/*` |
| FL2-N2 red test not in CI checklist | NOT-FIXED (file not owned by Phase 4); recorded in the hand-off for the maintainer | `docs/development/hand-offs/phase-4.md` |
| FL2-N3 cost candidates | FND-NET-010 FLAGGED (Azure Monitor private link article → Private Link pricing, 2026-10-18); FND-REL-009 left unflagged, listed UNVERIFIED in ADR-026 | `waf/mapping.yaml`, ADR-026 |
| FL2-N4 PASS reason with not-applicable skip | FIXED: reason names the not-applicable checks | `internal/assess/assess.go` |
| BB2-N1 INFO reason overclaims | FIXED: "informational at this profile's severity, see evidence" | `internal/assess/assess.go`, `docs/assess.md` |
| BB2-N4 REL-003 cost basis | FIXED: basis narrowed (zone-redundancy cost not cited) | `waf/mapping.yaml` |
| BB2-N5 central-dns consistency | FIXED: treated like policy-managed | `internal/assess/assess.go` |

Regenerated by hand: `internal/report/owner/testdata/owner.md.golden` (attested line and section text) and all
`samples/assess-*/reports/*` (state model and banners). Developer and evidence goldens are unchanged (fixtures set states directly).
Compile risks: gofmt alignment of the new map literal in `assess_test.go` (hand-aligned); nothing else new.
