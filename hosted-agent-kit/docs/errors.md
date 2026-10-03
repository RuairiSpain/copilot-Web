# Error reference

Every error response is `application/problem+json` and carries the `X-Correlation-ID` header.

```json
{
  "type": "urn:hosted-agent-kit:error:QUEUE_FULL",
  "title": "Agent queue is full",
  "status": 429,
  "detail": "The queue for this agent is full.",
  "error_code": "QUEUE_FULL",
  "correlation_id": "6f1c0a52d3d44a0e8f4b1c3b7a9d2e10",
  "retry_after_seconds": 10
}
```

Every error also says where the request failed and whether repeating it is safe:

| Field | Meaning |
|---|---|
| `phase` | `request`, `auth`, `queue`, `create`, `invoke` or `stream`. |
| `retry_safe` | True when repeating the same request cannot repeat work the agent may already have done. False for a timeout or failure after the agent started. |
| `request_id`, `correlation_id` | Quote them when reporting a problem. Both are also response headers. |

`retry_after_seconds` and the `Retry-After` header appear together. A 422 response also has an
`errors` list. Each item holds only `loc`, `msg` and `type`. Submitted values are never echoed.

## Codes

| Code | HTTP | Retry-After | Meaning | What the caller should do |
|---|---|---|---|---|
| `AUTHENTICATION_REQUIRED` | 401 | no | Missing, invalid or expired token, or a token from another tenant. | Get a fresh token. The `WWW-Authenticate` header says why. |
| `INSUFFICIENT_SCOPE` | 403 | no | The token lacks the required role. | Ask for the role. Do not retry. |
| `AGENT_NOT_CONFIGURED` | 404 | no | The agent is not in the pool configuration. | Fix the agent name. No Foundry call was made. |
| `SESSION_NOT_FOUND` | 404 | no | Admin API: the session is not in the pool. | Check the id. |
| `VALIDATION_ERROR` | 422 | no | Invalid body, parameter or header, Foundry rejected the payload, or `chat` was called on an Invocations agent. | Fix the request. |
| `PAYLOAD_TOO_LARGE` | 413 | no | The body exceeds `POOL_MAX_BODY_BYTES`. | Send less. |
| `POOL_CAPACITY_EXCEEDED` | 429 | yes | All sessions are busy and the queue is disabled. | Retry after the delay. |
| `QUEUE_FULL` | 429 | yes | The agent's queue is at `max_depth`. | Retry after the delay. |
| `UPSTREAM_THROTTLED` | 429 | yes | Foundry kept throttling after the retries. | Retry after the delay. |
| `REGIONAL_SESSION_QUOTA_EXCEEDED` | 429 | yes | Foundry answered `regional_session_quota_exceeded`: the subscription's concurrent session limit for the region is full. Retried with backoff (`create_retries`, `upstream_throttle_retries`) before it is returned. | Retry after the delay, or use another region. |
| `SESSION_QUOTA_EXCEEDED` | 429 | yes | Foundry answered `session_quota_exceeded`. Not retried: it does not clear until sessions are stopped or deleted. | Free sessions, or request a higher quota. |
| `QUEUE_WAIT_TIMEOUT` | 504 | yes | Waited `max_wait_seconds` in the queue. | Retry later, or raise the limit. |
| `STICKY_SESSION_TIMEOUT` | 504 | yes | A stateful user's own session stayed busy for `max_wait_seconds`. | Retry. The user is never moved to another session. |
| `FOUNDRY_TIMEOUT` | 504 | no | Foundry did not answer within the request timeout, or a stream sent nothing before `POOL_MAX_STREAM_SECONDS`. | Retry if the call is safe to repeat. |
| `REQUEST_TIMEOUT` | 504 | no | The whole request (queue, session creation, backoff and every attempt) passed `POOL_MAX_REQUEST_SECONDS`. | Retry later, or raise the limit. |
| `STREAM_TIMEOUT` | 504 | no | Only as a final stream event: the stream passed `POOL_MAX_STREAM_SECONDS`. | Start a new request. |
| `RESPONSE_TOO_LARGE` | 502 | no | A non-streaming Invocations response is larger than `POOL_MAX_RESPONSE_BYTES`. | Ask the agent for less, or stream. |
| `SESSION_NOT_FOUND` | 502 | no | The session vanished twice during one request. | Retry. |
| `SESSION_FAILED` | 502 | no | The session failed. It was deleted. | Retry. Send `Idempotency-Key` to let the service retry once on a new session. The key does not deduplicate requests. |
| `UPSTREAM_ERROR` | 502 | no | Foundry refused the call (for example 403 on a role problem), or an admin delete failed. | Alert an operator. |
| `FOUNDRY_CIRCUIT_OPEN` | 503 | yes | The agent's circuit breaker is open after repeated Foundry failures. Nothing was sent. | Retry after the delay. |
| `FOUNDRY_UNAVAILABLE` | 503 | yes | Foundry returned a server error or could not be reached. | Retry with backoff. |
| `END_USER_REQUIRED` | 403 | no | An app-only token called a stateful agent without `X-Pool-Subject`. | Send the end user in `X-Pool-Subject` (needs the delegate role). |
| `IDEMPOTENCY_KEY_REUSED` | 422 | no | The `Idempotency-Key` was already used with a different request. | Use a new key for a new request. |
| `IDEMPOTENCY_IN_PROGRESS` | 409 | yes | A request with this key is still running. | Retry after the delay to get its response. |
| `SERVICE_SHUTTING_DOWN` | 503 | yes | The service is shutting down and no longer admits requests. | Retry after the delay, which reaches another instance or the restarted one. |
| `CONFIG_INVALID` | 422 | no | Admin: a configuration reload was rejected. The old settings still apply. | Fix the document and reload again. |
| `RELOAD_UNAVAILABLE` | 409 | no | Admin: the service was not started from a configuration file. | Restart with a configuration file. |
| `SYNC_IN_PROGRESS` | 409 | no | Admin: a reconciliation is already running for the agent. | Wait, then retry. |
| `INTERNAL_ERROR` | 500 | no | Unexpected error. No detail is exposed. | Report the correlation id. |
| `NOT_FOUND`, `METHOD_NOT_ALLOWED` | 404, 405 | no | Unknown route or method. | Fix the URL. |

`SESSION_NOT_FOUND` has two meanings, distinguished by status: 404 for the admin API and 502 for a
session lost during an invocation.

`PAYLOAD_TOO_LARGE`, `UPSTREAM_ERROR` and `FOUNDRY_CIRCUIT_OPEN` are additions to the PRD error list.

For Invocations agents, a 400 or 422 from the agent is `VALIDATION_ERROR`. Other 4xx responses are
`UPSTREAM_ERROR`. The agent's own error body is not forwarded.

## Client disconnects

If the caller disconnects, the service cancels the work, releases the lease and removes any queue
entry. The (unseen) response status is 499 with an empty body. It appears in logs and metrics as the
outcome `CLIENT_CANCELLED`.

## Streaming

Once a stream has started, the HTTP status is already 200. A later failure is sent as a final
server-sent event:

```
event: error
data: {"error_code":"FOUNDRY_UNAVAILABLE","title":"Foundry is unavailable","detail":"...","correlation_id":"6f1c...","request_id":"req_...","phase":"stream","retry_safe":false,"retry_after_seconds":10}
```

`error_code`, `title`, `detail`, `correlation_id`, `request_id`, `phase` and `retry_safe` are always present. `retry_after_seconds` is
present when a retry delay applies. Codes in this event are the ones above, plus `STREAM_TIMEOUT`
when a stream passes `POOL_MAX_STREAM_SECONDS`. The service reads the first upstream frame before it
answers, so a failure before that frame is a normal problem response with the matching HTTP status.

## Timeouts

`timeout_seconds` limits one upstream call. It does not limit the queue wait
(`queue.max_wait_seconds`), session creation (`POOL_CREATE_READY_TIMEOUT_SECONDS`), backoff, or
the number of attempts. Set `POOL_MAX_REQUEST_SECONDS` for one ceiling over all of them. After a
stream starts, `POOL_MAX_STREAM_SECONDS` applies, including to a client that stops reading.

## Retry-After from Foundry

A `Retry-After` from Foundry (seconds or an HTTP date) is the least the service waits before it
retries. When it is longer than `POOL_BACKOFF_MAX_SECONDS` the service does not retry early: the
caller gets `UPSTREAM_THROTTLED` with Foundry's own delay.

## Session quota errors

Foundry limits concurrent hosted-agent sessions per subscription and region. A 429 that carries one
of the two quota codes is mapped to its own error instead of `UPSTREAM_THROTTLED`, because the
right response differs. These errors do not count as failures for the circuit breaker, and the
quota governor (see `quota.md`) lowers the kit's own limit when it sees one.
