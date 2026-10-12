# ADR-014: Docs, samples, end-to-end tests and the PSRule adapter are deferred to the last phase

Status: Accepted (project owner decision, 2026-10-12)
Date: 2026-10-12

## Context

PRD section 9 lists these Phase 1 deliverables: user documentation and a security threat model, good/bad/azure.yaml-only/Bicep-backed
sample repositories with a GitHub Actions smoke test, the optional PSRule adapter, and (testing strategy) end-to-end and CLI contract tests.
During Phase 1 the owner asked to stop the docs, samples, end-to-end and PSRule work and do it in the final project phase.

## Decision

1. The following move to the last project phase: user docs (`README.md`, `docs/user/*`), the threat model, the licence-inventory transitive audit,
   the sample projects, end-to-end tests under `test/e2e`, the smoke test of the samples in CI, and wiring of the PSRule adapter.
2. Already delivered and kept as is: `docs/user/cli.md`, `docs/development/*`, ADRs, the CLI contract tests in `test/integration/cli` and `internal/app`,
   the PSRule adapter package `internal/adapters` (built and tested against fakes and recorded fixtures only; not wired into `internal/app`, never run
   against a real PSRule, SARIF shape unverified; see `docs/development/adapters.md`), and the CI smoke script, which reports SKIPPED while no samples exist.
3. Partial user docs written before the stop are parked, unreviewed, on the branch `claude/foundry-doctor-docs-wip`.
4. Every phase hand-off lists these items as deferred by this ADR, not as met.

## Consequences

- Phase 1 DoD items that depend on samples cannot be shown: "GitHub pull request workflow ... enforces exit codes" is covered by CLI contract tests,
  but no workflow run against samples exists. The PRD deliverables "sample repositories" and "security threat model and dependency inventory" are unmet.
- Without end-to-end tests over real projects, rule behaviour on realistic projects is checked only by the rule unit tests and the independent reviews.
- `auto` PSRule mode has no effect yet: PSRule-dependent rule halves stay skipped with `tool-missing:psrule`.
- The `scripts/ci/smoke-samples.sh` gate reports SKIPPED (exit 5); in CI that is a warning, not a pass.
