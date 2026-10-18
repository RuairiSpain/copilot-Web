# LLM advisory data-flow samples (Phase 9)

Illustrative, hand-written examples of what crosses the provider boundary when
`--llm-explain` is passed. They are not captured from a live deployment.

| File | What it shows |
|---|---|
| `projected-request.json` | The allow-listed, redacted `AssessmentInput` DTO (`internal/llm/redact`). Raw finding evidence is replaced by a location summary; skip reasons have ARM resource IDs, GUIDs and host names replaced. |
| `provider-request-shape.json` | Keys of the single `POST <endpoint>/openai/v1/chat/completions` body. No `temperature`, no `api-version`, no API key. |
| `owner-response-good.json` | A response that passes `internal/llm/validate` for the projected request: every input rule covered, actions copied verbatim from deterministic recommendations, no prohibited claims. |
| `owner-response-bad.json` | Rejected: prohibited claims ("all controls pass", "severity was downgraded") and an unknown rule ID. |
| `owner-response-rejected-unknown-rule.json` | Rejected: the summary names `FND-NET-009` and `ruleIds` mixes a known ID with `FND-NOPE-999`. Unknown IDs reject the whole response; they are never silently dropped. |

A rejected response never reaches the report: the deterministic output is kept
and the advisory appendix says "LLM advisory unavailable (...)".
