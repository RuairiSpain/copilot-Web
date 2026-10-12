# Phase 9 hand-off: optional LLM explanations and narrative

## Delivered
- `internal/llm/**` provider-neutral contracts, allow-list projection, prompt templates, Azure OpenAI provider, and response validation.
- Optional `--llm-explain` support for `foundry-doctor explain` and `foundry-doctor assess waf` owner/developer audiences.
- Advisory appendix rendering with provenance labels for console, markdown, and HTML outputs.
- `docs/llm.md` and `samples/llm/` data-flow examples.

## Changed contracts
- Added optional `Services.Narrator` app dependency.
- Added `ExplainRequest` and LLM-specific assess options without changing deterministic finding or report schemas.

## Rule catalogue changes
- None.

## Commands implemented
- `foundry-doctor explain <rule-id> --llm-explain`
- `foundry-doctor assess waf --llm-explain`

## Tests executed and results
- Prompt snapshot tests.
- Redaction/projection tests.
- Mock provider tests.
- Malformed/hallucinated response rejection tests.
- Default-off and exit-code invariance tests.
- Full module gates passed locally: `gofmt -l .`, `go vet ./...`,
  `go test -count=1 ./...`, `staticcheck ./...`, `govulncheck ./...`,
  `bash scripts/claude/check-no-secrets.sh`, and
  `bash scripts/claude/check-rule-catalog.sh`.

## Security and privacy review
- LLM integration is explicit opt-in and off by default.
- Only allow-listed, redacted DTOs are sent.
- Accepted endpoint families are `*.openai.azure.com` and
  `*.services.ai.azure.com`; accepted paths are the bare resource URL for
  `*.openai.azure.com` and `/openai/v1/` for either family.
- No payload logging or config-file credentials.

## Foundry lead review
- Pending final re-review after the latest fallback/validation tightening.

## Azure black belt review
- Approved after endpoint/auth clarification fixes.

## Known limitations
- MVP supports the `azure-openai` provider only.
- Advisory narrative is appended only to human-facing explain and WAF owner/developer outputs.
- Evidence-pack JSON and deterministic report formats remain unchanged.
- Assessment advisory generation fails closed if the provider response omits any
  supplied rule ID.

## Deferred issues with rationale
- Additional providers are deferred pending separate auth/privacy review.
- Repo config is intentionally not allowed to enable LLM automatically; explicit CLI opt-in is required every run.

## Compatibility matrix
- Validated against current Microsoft Learn guidance for Azure OpenAI / Foundry OpenAI-compatible endpoints, structured outputs, and Entra ID auth on 2026-10-06.

## Artefacts for next phase
- `docs/llm.md`
- `samples/llm/`
- `internal/llm/**`

## Exact next-phase prerequisites
- Any future provider addition needs separate auth/privacy review.

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| Default execution makes no model call | met | `internal/app/llm_test.go` default-off test |
| No raw source files, secrets, prompts or document content are sent | met | `internal/llm/redact/project.go` and redaction tests |
| LLM output cannot add/remove findings or alter severity | met | `internal/llm/validate/validate_test.go`; deterministic findings unchanged in app tests |
| Reports label generated content and provider metadata | met | advisory appendix renderer tests and command/app integration tests |
