# ADR 0002: Foundry SDK use and invocation protocol

Status: accepted

## Context

The service manages hosted agent sessions and sends requests to them. The pinned SDK is
`azure-ai-projects==2.7.0`, the latest stable release when this was built.

## Decision

- **Two protocols, chosen per agent** (`protocol: responses | invocations`).
  - Responses: the session is bound with the `agent_session_id` field in the request body, sent
    through `extra_body`.
  - Invocations: the session is the `agent_session_id` query parameter of
    `POST .../agents/{name}/endpoint/protocols/invocations`. The SDK has no typed client for it, so
    the adapter calls `AIProjectClient.send_request`, which reuses the SDK's authentication and
    retry policy. The body is the caller's `input`, unchanged. The response body, status and content type are returned as the agent
    sent them (headers are dropped, and the session id is removed from JSON), because the protocol exists for agents whose format is their own. A
    `text/event-stream` response is relayed as a stream.
- **`AIProjectClient(allow_preview=True)`.** It is required for the agent-scoped OpenAI client
  (`get_openai_client(agent_name=...)`).
- **Session creation pins the latest agent version** with `VersionRefIndicator`.
- **The pool owns retries.** The SDK pipeline runs with `retry_status=0` and the OpenAI client with
  `max_retries=0`, so retries happen in one place, with bounded and logged attempts. The number depends on the call: `upstream_throttle_retries` for a 429 on invoke, `create_retries` for creation and `delete_retries` for deletion. A `Retry-After` is a minimum delay and is never shortened.
- **Session ids are scrubbed** from every Responses body and stream event, and from JSON
  Invocations bodies. Upstream headers are never forwarded. Other Invocations content is returned
  unchanged, so an agent that echoes its session id in plain text would expose it.
- **Isolation key.** Foundry scopes sessions by an isolation key. In the default Entra scheme it is
  derived from the caller's token, so the pool's one identity gives one key. For agents on the
  Header scheme the platform requires `x-ms-user-isolation-key` on every call, so the adapter can
  send one constant value (`POOL_FOUNDRY_ISOLATION_KEY`). Per-user keys are not used. They would
  make pool-wide listing impossible.
- **Caller-chosen session ids.** `create_session` accepts an `agent_session_id` that must be unique
  within the agent endpoint. See ADR 0007.
- **Errors** are translated at the adapter boundary into domain errors. Unknown exceptions are not
  disguised.
- **Sessions found in Foundry:** `list_sessions` is agent scoped and paged. An incomplete listing
  never implies deletion.

## Consequences

- `scripts/verify_versions.py` and the contract tests fail loudly if an SDK upgrade changes the
  surface or wire shapes the adapter uses.
- The request shapes are taken from the SDK and Microsoft Learn and are tested against the real SDK
  clients over a faked HTTP layer. They have not been exercised against a live project.
- The Invocations endpoint may need preview headers. The adapter assumes the SDK pipeline adds them.
- Which status Foundry returns for a duplicate session id (409 is assumed) is unverified. The pool
  falls back to a random id if the call fails with a conflict.
- `allow_preview=True` means a preview surface can change. Pin and test before upgrading.
