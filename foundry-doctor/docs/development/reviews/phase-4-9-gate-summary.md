# Foundry Doctor – Phase 4–9 decision-gate summary (2026-10-18)

Reviewers: `foundry-lead` (Foundry AI specialist) and `azure-black-belt`. Rule: up to 3 review/fix loops per phase; a phase passes only when both approve.

| Phase | Scope | Round 1 | Round 2 | Round 3 | Gate |
|---|---|---|---|---|---|
| 4 | WAF assessment & evidence reporting | REJECT / REJECT | REJECT / REJECT | APPROVE / APPROVE | **PASSED** |
| 5 | Dependency & deployment graphs | REJECT / REJECT | REJECT / REJECT | APPROVE / APPROVE | **PASSED** |
| 6 | Annotated review copies | REJECT / REJECT | APPROVE / APPROVE | – | **PASSED** |
| 7 | Cost intelligence | REJECT / REJECT | REJECT / REJECT | APPROVE / APPROVE | **PASSED** |
| 8 | Foundry IQ, Search, APIM gateway | REJECT / REJECT | APPROVE / APPROVE | – | **PASSED** |
| 9 | Optional LLM narrative | REJECT / REJECT | APPROVE / APPROVE | – | **PASSED** |

(Verdicts listed as Foundry lead / Azure black belt.)

New decision records: ADR-024 (Phase 6), ADR-025 (Phase 7), ADR-026 (Phase 4), ADR-027 (Phase 5), ADR-028 (Phase 8).
Every review, plus a fix log per phase, is in `foundry-doctor/docs/development/reviews/`.

## Conditions attached to every approval

No Go toolchain was available in the review environment. Nothing was compiled or tested; reviewers checked compile-correctness by reading code. Before merging, the maintainer must:

1. Run `gofmt -w` on changed Go files, then `scripts/claude/verify-phase.sh --strict` (gofmt, vet, `go test ./...`, `-race`, staticcheck, govulncheck, build, catalogue check, secret scan). A failing gate re-opens the matching finding.
2. Regenerate hand-computed goldens and samples and review the diffs:
   - Phase 4: `go test ./cmd/foundry-doctor -run TestAssessSampleReports -update-assess-samples` (the test fails by design until regenerated).
   - Phase 5 and 7: golden/sample tests (`-update` where offered).
3. Run the Phase 6 real-Bicep test: `go test -tags spike` with `BICEP_PATH` set.
4. Regenerate `docs/rule-catalog.md` / `docs/overlap-analysis.md` with `cmd/rulecatalog` (hand-edited in Phase 7).
5. Record the real gate output in each hand-off.

## Release blockers still open (v1)

- ADR-019: go-principal-engineer and security-reviewer reviews not done.
- ADR-028: FND-IDN-002 live, FND-NET-002 live and FND-SEC-005 not delivered (pre-v1 work packet). ADR-028 needs confirmation by the project-lead delegate and a named owner.

## Behaviour changes to be aware of

- `doctor` resolves `infra.path` relative to the folder holding `azure.yaml`; annotate review copies use `azure.review.yaml` (ADR-024).
- Search data-plane reads are opt-in: `doctor --iq-metadata` (ADR-028). Without it, IQ rules skip, so `--strict` can exit 3.
- All 18 Phase 8 rules and several cost rules had their versions raised, so baselines for those rules reset.
- `--llm-explain` no longer uses `AZURE_ACCESS_TOKEN`; CI or managed-identity runs without azd/az sign-in get "LLM advisory unavailable".
- New graph mode `--diff-base/--diff-target`; new `unobserved` origin.
- The project uses `go 1.26.9`. The earlier hand-off claims of runs on Go 1.25.13 were withdrawn.
