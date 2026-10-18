# Phase 9 hand-off: optional LLM explanations and narrative

Status: **open**. Round 1 reviews (foundry-lead, azure-black-belt) REJECTED;
fixes applied in fix loop 1 (2026-10-18), pending round 2 re-review. Fix log:
[phase-9-fixlog-loop1.md](../reviews/phase-9-fixlog-loop1.md).

## Delivered
- `internal/llm/**`: provider-neutral contracts, allow-list projection
  (`redact`), versioned prompt templates (`prompts`), Azure OpenAI provider,
  and response validation (`validate`).
- Optional `--llm-explain` for `foundry-doctor explain` and
  `foundry-doctor assess waf` (owner/developer audiences only).
- Advisory appendix rendering with provenance labels for console, markdown and
  HTML outputs.
- `docs/llm.md` (data flow, threat boundaries, auth, roles, network, limits).
- `samples/llm/`: projected request, provider request shape, one accepted and
  two rejected responses, with a README. These are hand-written illustrations,
  not captures from a live deployment.

## Changes in fix loop 1
- LLM credential: the provider now uses `azure.NewCLICredentialForTenant`
  (azd/az only). `AZURE_ACCESS_TOKEN` is never sent to the inference host.
  `internal/azure/auth.go` gained only this constructor; the default chain used
  by other commands is unchanged.
- `FOUNDRY_DOCTOR_LLM_SCOPE` is allow-listed to `https://ai.azure.com/.default`
  and `https://cognitiveservices.azure.com/.default` (checked in
  `ResolveConfig` and again in the provider).
- HTTP: dedicated client; redirects are refused (`http.ErrUseLastResponse`)
  even for caller-supplied clients, and 3xx makes the advisory unavailable.
  Explicit ports in the endpoint are rejected.
- Request: `temperature` removed; `max_completion_tokens: 4096` added.
  Responses with `refusal`, `finish_reason: "length"` or
  `"content_filter"` are rejected with a specific note.
- Validation: unknown IDs in `ruleIds` reject the whole response (no longer
  dropped); rule-ID-shaped tokens in summary, headings, section summaries and
  actions must be in the projected input. `upgrade` removed from the
  prohibited-claim list (false rejections on azd version-floor rules).
- Redaction: ARM resource IDs and GUIDs are removed from all projected text;
  DNS host names are also removed from free-text skip reasons. Truncation is
  rune-safe.
- CLI tests clear `FOUNDRY_DOCTOR_LLM_*`, `AZURE_OPENAI_*` and
  `AZURE_ACCESS_TOKEN`, so no test can reach a real provider.

## Cross-phase change from Phase 4 fix loop 2 (2026-10-18)

- `internal/llm/redact/project.go` (`Assessment`): the projection now sends only outstanding or unassessed controls
  (`assess.Outstanding(state) || state == SKIPPED`: FAIL, WARNING, UNKNOWN, QUESTION, SKIPPED). PASS, INFO, ATTESTED and
  NOT_APPLICABLE are no longer projected, because `llm.Finding` has no state field and the model would narrate them as
  problems (Phase 4 round 2, foundry-lead NB2 / azure-black-belt N2). A payload with nothing outstanding returns
  `llm.ErrUnavailable` (error text now "no outstanding or unassessed controls available for advisory narrative").
- Redaction, the `llm.Finding` DTO, prompts, schema and validator are unchanged; no prompt version bump.
- Test: `TestAssessmentProjectionSkipsSettledStates` in `internal/llm/redact/project_test.go`. Not run (no Go toolchain).

## Changed contracts
- Added optional `Services.Narrator` app dependency.
- Added `ExplainRequest` and LLM-specific assess options without changing
  deterministic finding or report schemas.
- `llm.Config` gained `MaxOutputTokens`; `llm` exports `LegacyScope` and
  `DefaultMaxCompletionTokens`.
- `azure.NewCLICredentialForTenant` (new, additive).

## Rule catalogue changes
- None.

## Commands implemented
- `foundry-doctor explain <rule-id> --llm-explain`
- `foundry-doctor assess waf --llm-explain`

## Tests executed and results
**Not run in fix loop 1.** No Go toolchain is available in this environment,
so `go build ./...`, `go vet ./...`, `go test ./...` and
`scripts/claude/verify-phase.sh --strict` have NOT been run against the current tree.
The maintainer must run them before re-review. Earlier claims of passing gates
are withdrawn for the current tree.

Tests added or changed in loop 1 (unexecuted):
- `internal/azure/auth_cli_test.go`: CLI chain ignores `AZURE_ACCESS_TOKEN`.
- `internal/llm/provider_azure_openai_test.go`: ARM-audience env token never
  forwarded; redirect refused (one transport call); exact request keys;
  refusal and `length` rejection; port and scope rejection; default client
  refuses redirects.
- `internal/llm/config_test.go`: scope allow-list.
- `internal/llm/validate/validate_test.go`: unknown IDs in `ruleIds` and in
  free text rejected; known IDs in text accepted; rune-safe truncation.
- `internal/llm/redact/project_test.go`: ARM IDs, GUIDs and host names
  removed from skip reasons; rune-safe truncation.
- `internal/app/llm_narrator_test.go`: `lazyNarrator` end to end through the
  real redact -> prompts -> validate pipeline with a mock provider; scope
  rejection before any provider call.
- `cmd/foundry-doctor/main_test.go`: `exec` clears ambient LLM variables.

## Security and privacy self-assessment (not an independent review)
- LLM integration is explicit opt-in and off by default.
- Only allow-listed, redacted DTOs are sent.
- Accepted endpoint families are `*.openai.azure.com` and
  `*.services.ai.azure.com`, no port, no redirects.
- Token audience restricted to two allow-listed scopes, minted by azd/az.
- No payload logging or config-file credentials.

## Foundry lead review
- Round 1: REJECT ([phase-9-foundry-lead-round1.md](../reviews/phase-9-foundry-lead-round1.md)).
  Fixes applied in loop 1, pending re-review.

## Azure black belt review
- Round 1: REJECT ([phase-9-azure-black-belt-round1.md](../reviews/phase-9-azure-black-belt-round1.md)).
  Fixes applied in loop 1, pending re-review. (An earlier "Approved" line in
  this hand-off was unsupported: no Phase 9 black belt approval ever existed.)

## Go principal engineer review (ADR-019)
- Status: **NOT DONE.** ADR-019 moves this whole-code-base review to Phase 9.
  It has not run. It is required before any release tag is published.
- Review file: none yet. Open findings: unknown (review not run).

## Security review (ADR-019)
- Status: **NOT DONE.** ADR-019 moves this whole-code-base review to Phase 9.
  It has not run. It is required before any release tag is published.
- Review file: none yet. Open findings: unknown (review not run).
- Consequence: the PRD section 22 item "No unresolved critical/high security
  findings" cannot be shown (see DoD table).

## Known limitations
- MVP supports the `azure-openai` provider only.
- Advisory narrative is appended only to human-facing explain and WAF
  owner/developer outputs; evidence-pack JSON and deterministic report formats
  are unchanged.
- Assessment advisory fails closed if the response omits any supplied rule ID
  or names any rule ID not supplied.
- `AZURE_ACCESS_TOKEN`-only environments (managed identity, CI without
  azd/az login) cannot use `--llm-explain`; the advisory falls back.
- No tenant override for the LLM token (uses the CLI default tenant).
- `prohibitedClaim` still matches bare `priority`/`severity`/`urgent`; the
  false-rejection rate on real catalogue text has not been measured.
- No live call against a real deployment has been recorded; response shapes
  follow Learn samples.

## Deferred issues with rationale
- Additional providers are deferred pending separate auth/privacy review.
- Repo config cannot enable LLM; explicit CLI opt-in is required every run.
- HTML insertion-point and evidence-pack byte-identity tests (foundry-lead
  missing test 6) belong to the report renderers being changed by the phase 4
  fix loop; not added here.

## Compatibility matrix
- Scope, URL forms and `max_completion_tokens` checked against Microsoft Learn
  on 2026-10-18 (links in `docs/llm.md`). `cognitiveservices.azure.com`
  audience acceptance on `/openai/v1/` is UNVERIFIED.

## Artefacts for next phase
- `docs/llm.md`
- `samples/llm/`
- `internal/llm/**`

## Exact next-phase prerequisites
- Run `go build`, `go vet`, `go test -race ./...` and
  `scripts/claude/verify-phase.sh --strict` on the current tree.
- Round 2 foundry-lead and azure-black-belt reviews.
- ADR-019 go-principal-engineer and security-reviewer reviews before release.
- Any future provider addition needs separate auth/privacy review.

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| Default execution makes no model call | met (pending test run) | `internal/app/llm_test.go` default-off tests; flags default off |
| No raw source files, secrets, prompts or document content are sent | unmet until re-review and test run | loop 1 fixes: CLI-only credential, scope allow-list, redirect refusal, ARM-ID/GUID/host redaction; tests listed above not yet executed |
| LLM output cannot add/remove findings or alter severity | unmet until re-review and test run | loop 1: unknown rule IDs rejected in `ruleIds` and free text (`internal/llm/validate`); deterministic findings and exit codes unchanged |
| Reports label generated content and provider metadata | met (pending test run) | advisory appendix renderer tests |
| PRD 22: no unresolved critical/high security findings | unmet | ADR-019 security review not done |
| PRD 22: two independent expert reviews completed | unmet | round 1 REJECT from both; round 2 pending |

## Carried-forward release obligations from Phase 8 (added 2026-10-18)

- ADR-028 re-allocates the FND-IDN-002 live check, the FND-NET-002 live check and FND-SEC-005 to a pre-v1 work packet. They are **not delivered**; the v1 release gate in ADR-022/ADR-023 is kept. ADR-028 still needs confirmation by the project-lead delegate and a named owner.
- Together with the ADR-019 go-principal-engineer and security-reviewer reviews above, these block any v1 release tag.
