# Foundry Doctor

Foundry Doctor is a deterministic, read-only validation, deployment-preflight and
runtime-diagnosis platform for Microsoft Foundry projects, written in Go. The
rule catalogue is the product; the CLI and integrations execute, correlate and
report the rules.

Interfaces: `azd foundry doctor|explain|compare|assess waf|graph|annotate|cost|scaffold`
and the standalone `foundry-doctor` binary. Phase 0 decides the final public
namespace (ADR-003); do not hard-code `azd foundry` outside the command layer.

This directory lives inside the `copilot-Web` monorepo. Everything for this
project stays under `foundry-doctor/`. The Claude harness (agents, skills, rules,
hooks) is in the repo-root `.claude/`; its rules are scoped to `foundry-doctor/**`.

## Source of truth (read in this order)

1. `docs/requirements/Foundry_Doctor_Implementation_PRD.md`
2. `docs/rule-catalog.md` and `rules/catalog/` (after Phase 0)
3. `docs/overlap-analysis.md` (after Phase 0)
4. `docs/decisions/` (ADRs)
5. The newest file in `docs/development/hand-offs/`

If the code and the PRD disagree, stop and record the conflict as an ADR issue.
Do not reinterpret requirements silently. Known PRD inconsistencies are listed in
`docs/development/phase-execution.md`.

## Engineering principles

- Idiomatic Go. Small interfaces at integration boundaries, defined where consumed.
- Only deterministic engines create pass/fail findings. An LLM never does.
- Skipped is not passed. Report skipped checks, with the reason and the missing capability.
- The doctor is read-only. It never mutates Azure or azd state.
- Do not write a Bicep parser. Compile with the Bicep CLI and analyse the ARM JSON.
- Never emit secrets, tokens, connection strings, prompts or document content.
- Do not implement an Azure or Foundry property name, API version, SKU limit, role
  requirement or schema field that has no verified primary source.
- Every rule needs: stable ID, version, rationale, severity per profile, evidence
  definition, remediation, docs URL, `lastVerified` date, compatible azd/extension
  versions, and positive, negative and skipped tests.
- Platform-basis rules are errors in every profile.
- Existing projects stay adoptable: baselines hide only matching fingerprints,
  suppressions need a reason and an expiry.

## Layout

`cmd/foundry-doctor/`, `internal/`, `pkg/sdk/`, `rules/{catalog,packs,profiles,mappings}/`,
`schemas/`, `samples/`, `test/{fixtures,golden,integration,e2e,spikes}/`,
`docs/{decisions,development}/`. Business logic never lives in Cobra handlers.

## Verification commands

Run `scripts/claude/verify-phase.sh` (it skips, and says so, anything that does not
exist yet). It runs: `gofmt`, `go vet`, `go test ./...`, `go test -race ./...`,
`staticcheck`, `govulncheck`, build, then the rule-catalogue check and secret scan.
Rule changes also run `scripts/claude/check-rule-catalog.sh`.

## Agent workflow

Per phase, use the `implement-prd-phase` skill from the main session. Subagents
cannot spawn subagents, so the main session coordinates; the
`implementation-coordinator` agent only plans and audits.

1. Plan and map every Definition of Done (DoD) item to work and tests.
2. Run `go-implementer` agents in parallel, one package set each, in worktrees.
3. Integrate, then run `scripts/claude/verify-phase.sh`.
4. Independent reviews (read-only agents): `foundry-lead`, `azure-black-belt`,
   `go-principal-engineer`, plus `security-reviewer` where Azure or data is touched.
5. Fix findings or record an ADR with evidence.
6. Write the hand-off from `docs/development/hand-offs/TEMPLATE.md`.

A reviewer never approves what it authored. Do not mark a phase complete while a
DoD item is unmet unless an ADR waives it.

## Research

Use current primary sources: Context7 for library and SDK APIs, Microsoft Learn and
the Azure SDK references for Azure behaviour, go.dev for Go and gopls, Anthropic
documentation for Claude Code. Record URL and verification date in rule metadata.

## Git

Work on a feature branch or worktree. Keep commits focused. Never force-push shared
branches. Never commit `.env`, credentials, tokens or `.claude/settings.local.json`.
Do not bypass failing tests or hooks.
