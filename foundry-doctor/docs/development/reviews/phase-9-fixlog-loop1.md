# Phase 9 fix log, loop 1 (2026-10-18)

Reviews addressed: `phase-9-foundry-lead-round1.md` (FL) and
`phase-9-azure-black-belt-round1.md` (BB). No Go toolchain was available: none of
the changes or new tests have been compiled or run. The maintainer must run
`go build ./...`, `go vet ./...`, `go test -race ./...` and
`scripts/claude/verify-phase.sh --strict`.

## Blocking findings

| Finding | Status | Files |
|---|---|---|
| FL-B1 / BB-B1 `AZURE_ACCESS_TOKEN` forwarded to inference host | FIXED: LLM provider uses new `azure.NewCLICredentialForTenant` (azd/az only, env source removed); default provider credential and narrator wiring changed; docs corrected; tests | `internal/azure/auth.go` (additive constructor only), `internal/azure/auth_cli_test.go`, `internal/llm/provider_azure_openai.go`, `internal/llm/provider_azure_openai_test.go`, `internal/app/llm_narrator.go`, `docs/llm.md` |
| BB-B2 (FL-N1) scope accepts any audience | FIXED: allow-list `https://ai.azure.com/.default`, `https://cognitiveservices.azure.com/.default` in `ResolveConfig` and in the provider; tests | `internal/llm/config.go`, `internal/llm/config_test.go`, `internal/llm/provider_azure_openai.go`, `internal/app/llm_narrator_test.go`, `docs/llm.md` |
| BB-B3 redirects bypass allow-list | FIXED: `CheckRedirect` returns `http.ErrUseLastResponse` (forced even on caller clients); 3xx -> unavailable; test asserts one transport call | `internal/llm/provider_azure_openai.go`, `internal/llm/provider_azure_openai_test.go` |
| FL-B4 / BB-B4 unknown rule IDs dropped, free text unchecked | FIXED: unknown `ruleIds` reject the response; `FND-*-*` tokens in summary/heading/section summary/actions must be in input; tests (unit + app end to end) | `internal/llm/validate/validate.go`, `internal/llm/validate/validate_test.go`, `internal/app/llm_narrator_test.go` |
| BB-B5 skip reasons carry ARM IDs / GUIDs / hosts | FIXED: ARM IDs and GUIDs redacted from all projected text; host names also from skip reasons; test | `internal/llm/redact/project.go`, `internal/llm/redact/project_test.go` |
| FL-B2 / BB-B6 `samples/llm/` | FIXED: samples now match the real projection and validator (previous "good" sample would have been rejected); added rejected-unknown-rule sample, request-shape sample and README | `samples/llm/*` |
| FL-B3 ADR-019 obligation | FIXED (disclosed): hand-off has Go principal engineer and Security review sections, both NOT DONE and required before release; PRD 22 rows added to DoD table as unmet | `docs/development/hand-offs/phase-9.md` |
| BB-B6 hand-off accuracy | FIXED: removed unsupported "black belt approved" and passing-gates claims; reviews marked round 1 REJECT, pending re-review | `docs/development/hand-offs/phase-9.md` |

## Non-blocking findings

| Finding | Status | Notes |
|---|---|---|
| FL-N1 | FIXED | see BB-B2; both audiences documented, cognitiveservices acceptance marked UNVERIFIED |
| FL-N2, FL-N3 (schema) | no change needed | confirmed correct by reviewer |
| FL-N3 (refusal / finish_reason) | FIXED | refusal, `length`, `content_filter` -> specific rejected note; tests |
| FL-N4 / BB-N2 temperature | FIXED | `temperature` no longer sent; documented. Default 10 s timeout unchanged (still configurable to 30 s) |
| BB-N1 output cap | FIXED | `max_completion_tokens: 4096`; golden request-keys test; documented |
| FL-N5 / BB-N9 prohibited-claim breadth | PARTIAL | removed `upgrade`; bare `priority`/`severity`/`urgent` kept (fail-closed); false-rejection rate unmeasured, listed in known limitations |
| FL-N6 CLI test env isolation | FIXED | `exec` clears `FOUNDRY_DOCTOR_LLM_*`, `AZURE_OPENAI_*`, `AZURE_ACCESS_TOKEN` (`cmd/foundry-doctor/main_test.go`) |
| FL-N7 ambient `AZURE_OPENAI_ENDPOINT` | FIXED (docs) | warning in `docs/llm.md` |
| FL-N8 / BB-N10 run metadata claim | FIXED (docs) | `docs/llm.md` now lists only audience |
| FL-N9 | FIXED | hand-off links both review files |
| BB-N3 data residency | FIXED (docs) | |
| BB-N4 roles | FIXED (docs) | Foundry User (formerly Azure AI User) verified on Learn 2026-10-18; Cognitive Services OpenAI User taken from reviewer-cited page, not re-fetched |
| BB-N5 network docs | FIXED (docs) | proxy, private endpoint / 403, sovereign clouds rejected, redirects refused |
| BB-N6 tenant selection | NOT-FIXED | no tenant flag on `explain`; documented as known limitation (uses CLI default tenant) |
| BB-N7 explicit ports | FIXED | rejected in `providerURL`; test |
| BB-N8 byte-wise truncation | FIXED | rune-safe in `redact.truncate` and `validate.sanitize`; tests |
| FL missing test 3 (end-to-end narrator) | FIXED | `internal/app/llm_narrator_test.go`; fake narrator "Priority" heading replaced in `internal/app/llm_test.go` |
| FL missing test 6 (HTML insertion / evidence-pack identity) | NOT-FIXED | renderers are concurrently changed by the phase 4 fixer; deferred to re-review, recorded in hand-off |

## Compile-risk notes
- `provider_azure_openai.go` copies `*p.Client` into a local `http.Client` to force `CheckRedirect`; `http.Client` has no lock fields, so `go vet` copylocks should not fire.
- gofmt alignment of the new `const`/`var` blocks was kept safe with blank-line separation, but has not been machine-checked.
