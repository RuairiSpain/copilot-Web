# @hosted-agent-kit/client (TypeScript)

A client for the HACK standalone service (hosted-agent-kit) with no runtime dependencies. It uses `fetch`, so it runs
on Node 20 or later and in browsers.

```ts
import { PoolClient, PoolError } from "@hosted-agent-kit/client";

const pool = new PoolClient({ baseUrl: "https://pool.example.com", token: () => getToken() });

for (const agent of await pool.agents()) console.log(agent.name, agent.protocol, agent.stateful);

const reply = await pool.respond("coding-agent", { input: "Write a haiku" }, {
  conversationKey: "project-1",
  idempotencyKey: "job-42",
});
console.log(reply.result, reply.replayed);

for await (const event of pool.streamResponses("coding-agent", { input: "Tell a story" })) {
  console.log(event.event, event.data);
}

const out = await pool.invoke("pipeline-agent", [{ task: "summarise" }]);
console.log(out.status, out.json());
```

- **Errors** are `PoolError` with `status`, `errorCode`, `detail`, `phase`, `retrySafe`,
  `retryAfterSeconds`, `requestId` and `correlationId`. A stream that fails after it started throws
  `PoolStreamError`.
- **Retries.** An error that is safe to repeat and carries a delay is retried up to `maxRetries` times,
  waiting at least the delay the service gave. A delay longer than `maxRetryAfter` is not waited for: the
  error is thrown with the delay on it. A refused connection is retried because nothing was sent.
- **Correlation.** One `X-Correlation-ID` is sent for every attempt of a call.
- **Service callers.** An app-only token calling a stateful agent passes `subject: "<end user id>"`.

```bash
npm ci
npm test
```
