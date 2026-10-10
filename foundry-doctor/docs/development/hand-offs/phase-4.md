# Phase 4 hand-off: WAF assessment and evidence reporting

## Delivered
- `internal/assess` aggregation model for WAF-oriented control views and action prioritisation.
- `internal/assess/waf` rule-to-control mapping with completeness coverage test.
- `internal/assess/wara` reliability-only WARA reference mapping.
- Audience renderers in `internal/report/{owner,developer,evidence,html}` plus shared escaping helpers in `internal/report/common`.
- `foundry-doctor assess waf` wiring in `internal/app/assess.go` and `cmd/foundry-doctor/assess.go`.
- Dev/test/prod assessment fixtures and docs in `docs/assess.md`.

## Changed contracts
- Added CLI entrypoint `foundry-doctor assess waf`.
- Added `app.AssessWAF` request/response flow and audience/format validation.
- Added assessment states `PASS`, `FAIL`, `WARNING`, `UNKNOWN`, `QUESTION`, `SKIPPED` for WAF-facing reports.

## Rule catalogue changes
- No rule packet schema changes.
- Phase 4 consumes existing catalogue metadata (`pillar`, `category`, `recommendation`, `fix`, `sources`, `basis`) and fails tests if an applicable rule is unmapped.

## Commands implemented
- `foundry-doctor assess waf`
  - owner Markdown
  - developer Markdown / HTML
  - evidence Markdown / JSON

## Tests executed and results
- `gofmt -l .` → pass (empty)
- `go vet ./...` → pass
- `go test -count=1 ./...` → pass
- `staticcheck ./...` → pass
- `govulncheck ./...` → pass (`No vulnerabilities found.`)
- `bash scripts/claude/check-no-secrets.sh` → pass
- `bash scripts/claude/check-rule-catalog.sh` → pass (`catalogue valid: 108 rules`)
- `-race` not run on windows/arm64 per project guidance.

## Security and privacy review
- Report text uses existing redaction helpers and Markdown escaping for untrusted strings.
- HTML output is static, script-free, template-escaped, and covered by an escaping/accessibility test.
- Evidence pack keeps suppressed and baselined findings visible rather than silently dropping them.

## Foundry lead review
- Not yet run in-session.

## Azure black belt review
- Not yet run in-session.

## Known limitations
- Assessment scope is intentionally limited to locally available project evidence and compiled infrastructure when available.
- Missing business or operational context remains `QUESTION` or `UNKNOWN`; the command does not claim full WAF compliance.
- Current WARA support is reliability-specific only, per PRD.

## Deferred issues with rationale
- Independent reviewer passes remain deferred to a follow-up review turn; implementation gates are green.

## Compatibility matrix
- Validated on Windows ARM64 with Go `1.25.13` toolchain and local `GOTOOLCHAIN=local`.
- No new external runtime dependencies introduced.

## Artefacts for next phase
- `docs/assess.md`
- `samples/assess-{dev,test,prod}/`
- Golden fixtures under `internal/report/{owner,developer,evidence}/testdata/`

## Exact next-phase prerequisites
- Run independent reviews against Phase 4 mappings and output wording.
- Reconcile any future catalogue additions with `internal/assess/waf` completeness coverage.

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| No report claims complete WAF compliance | met | Scope/limitations text in owner, developer, evidence, HTML, and `docs/assess.md` |
| Unknown/non-machine-verifiable controls are visible | met | Aggregation states plus QUESTION/UNKNOWN/SKIPPED coverage tests and fixtures |
| Every recommendation links to a verified source or is labelled project opinion | met | Control renderers surface first verified catalogue source and project-opinion label |
| All three audiences receive materially different, useful views | met | Separate owner/developer/evidence renderers and persona-difference tests |
| Suppressed and baselined controls remain visible in evidence pack | met | Evidence renderer tests and golden outputs |
