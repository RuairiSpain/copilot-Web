# ADR 0008: A stable API contract, conversations, service callers and idempotency

Status: accepted

## Context

A review of the first releases found that agent developers could not build a reliable client:

- `/invoke` returned an envelope, arbitrary bytes, any status code or a stream, depending on server
  configuration, so a generated client could not model it.
- A stateful user had exactly one session per agent.
- A service calling for its users shared one session between all of them.
- `Idempotency-Key` only allowed one retry after a session failure. It did not deduplicate.
- Invocations bodies had to be JSON objects.
- Errors did not say where the request failed or whether repeating it was safe.
- There was no way to roll agents onto a new version in a controlled way.

## Decision

- **Separate endpoints per protocol.** `responses` takes a JSON body and returns an envelope or SSE.
  `invocations` takes any body, passes it to the agent as received and returns the agent's response,
  or an envelope with `?envelope=true`. Each refuses the other protocol with a message naming the right
  endpoint. `invoke` stays for existing callers and is marked deprecated, with a `Link` to its
  successor. `GET /v1/agents[/{agent}]` describes protocol, streaming, endpoints and limits for any
  caller with the invoke role.
- **Conversation key.** An opaque key, scoped under the authenticated user, selects one of the
  user's sessions. It extends the affinity key and the derived session id. Without a key the
  affinity and the derived id are the same as before, so existing sessions are still found.
- **App-only callers.** A token without `scp` (or with `idtyp=app`) is app-only. For stateful agents
  it needs the end user in `X-Pool-Subject` and the delegate role, and the session is scoped to
  caller and subject. The subject is trusted because the caller authenticated and holds a role an
  administrator granted, not because the header is signed. `POOL_APP_ONLY_POLICY=allow` restores the
  earlier behaviour. This is a breaking change for app-only callers of stateful agents.
- **Idempotency.** `Idempotency-Key` deduplicates non-streaming requests within a time and size bound.
  The key is scoped to the caller and agent and bound to a digest of the request. Only successes are
  stored, so a retry after a failure runs. Streams are not replayed. State is in memory (ADR 0003).
- **Error detail.** Every error carries `phase`, `retry_safe`, `request_id` and `correlation_id`, in
  problem responses and in the final stream event.
- **Agent versions.** `agent_version` pins new sessions. `version_drain` retires idle sessions on any
  other version, never a leased one, and only user-owned sessions under the `idle` policy.
- **Clients.** Python and TypeScript clients wrap the contract: authentication, problem details,
  retry on safe errors with the service's delay, one correlation id per call, and event streams.

## Consequences

- Generated clients can model `responses` and `invocations`. `invoke` can be removed in a later
  major version.
- A service caller must be granted the delegate role and send `X-Pool-Subject`.
- Each conversation uses a session, so `max_sessions` bounds users times conversations.
- Idempotency does not survive a restart or cover streams. A caller that needs that must handle it
  in its own system.
