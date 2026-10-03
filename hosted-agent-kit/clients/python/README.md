# hosted-agent-kit-client (Python)

An async client for the HACK standalone service (hosted-agent-kit). It depends only on `httpx`.

```python
import asyncio
from hosted_agent_kit_client import PoolClient, PoolError


async def main() -> None:
    async with PoolClient("https://pool.example.com", token=get_token) as pool:
        for agent in await pool.agents():
            print(agent.name, agent.protocol, agent.stateful)

        reply = await pool.respond(
            "coding-agent",
            {"input": "Write a haiku"},
            conversation_key="project-1",
            idempotency_key="job-42",
        )
        print(reply.result, reply.replayed)

        async for event in pool.stream_responses("coding-agent", {"input": "Tell a story"}):
            print(event.event, event.data)

        out = await pool.invoke("pipeline-agent", [{"task": "summarise"}])
        print(out.status_code, out.json())


asyncio.run(main())
```

`token` is a bearer token or a function (sync or async) that returns a fresh one for each attempt.

- **Errors** are `PoolError` with `status`, `error_code`, `detail`, `phase`, `retry_safe`,
  `retry_after_seconds`, `request_id` and `correlation_id`. A stream that fails after it started raises
  `PoolStreamError`.
- **Retries.** An error that is safe to repeat (`retry_safe`) and carries a delay is retried up to
  `max_retries` times, waiting at least the delay the service gave. A delay longer than
  `max_retry_after` is not waited for: the error is raised with the delay on it. A refused connection is
  retried because nothing was sent. Timeouts and failures after the agent started are never retried by
  the client. Use `idempotency_key` to make repeating such a call safe for a successful first attempt.
- **Correlation.** One `X-Correlation-ID` is sent for every attempt of a call and is on the result or
  error.
- **Service callers.** An app-only token calling a stateful agent passes `subject="<end user id>"`.

Run the tests from the service repository: `uv run pytest tests/clients`.
