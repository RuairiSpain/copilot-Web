import assert from "node:assert/strict";
import { test } from "node:test";

import { PoolClient, PoolError, PoolStreamError, parseSse, type SseEvent } from "../src/index.js";

type Scripted = Response | Error;

function problem(status: number, code: string, safe: boolean, retryAfter?: number): Response {
  const body: Record<string, unknown> = {
    title: code,
    status,
    detail: "d",
    error_code: code,
    phase: "queue",
    retry_safe: safe,
  };
  const headers: Record<string, string> = { "content-type": "application/problem+json" };
  if (retryAfter !== undefined) {
    body.retry_after_seconds = retryAfter;
    headers["retry-after"] = String(retryAfter);
  }
  return new Response(JSON.stringify(body), { status, headers });
}

const ok = () =>
  new Response(JSON.stringify({ request_id: "req_1", result: { a: 1 } }), {
    status: 200,
    headers: { "content-type": "application/json", "x-correlation-id": "corr" },
  });

function scripted(responses: Scripted[], extra: Record<string, unknown> = {}) {
  const seen: { url: string; init: RequestInit }[] = [];
  const sleeps: number[] = [];
  const fetchImpl = (async (url: string | URL | Request, init?: RequestInit) => {
    seen.push({ url: String(url), init: init ?? {} });
    const next = responses.shift();
    if (next === undefined) throw new Error("no more scripted responses");
    if (next instanceof Error) throw next;
    return next;
  }) as typeof fetch;
  const client = new PoolClient({
    baseUrl: "http://svc/",
    fetch: fetchImpl,
    sleep: async (seconds) => {
      sleeps.push(seconds);
    },
    ...extra,
  });
  return { client, seen, sleeps };
}

const header = (init: RequestInit, name: string) => (init.headers as Record<string, string>)[name];

test("respond sends the options as headers and returns a typed result", async () => {
  const { client, seen } = scripted([ok()], { token: "tok" });
  const result = await client.respond(
    "agent-a",
    { input: "x" },
    { conversationKey: "c1", subject: "alice", idempotencyKey: "k", timeoutSeconds: 9 },
  );
  assert.equal(result.requestId, "req_1");
  assert.deepEqual(result.result, { a: 1 });
  assert.equal(result.correlationId, "corr");
  assert.equal(result.replayed, false);
  const call = seen[0]!;
  assert.equal(call.url, "http://svc/v1/agents/agent-a/responses");
  assert.equal(header(call.init, "X-Conversation-Key"), "c1");
  assert.equal(header(call.init, "X-Pool-Subject"), "alice");
  assert.equal(header(call.init, "Idempotency-Key"), "k");
  assert.equal(header(call.init, "Authorization"), "Bearer tok");
  assert.deepEqual(JSON.parse(call.init.body as string), { input: { input: "x" }, timeout_seconds: 9 });
});

test("a replayed response is flagged", async () => {
  const response = new Response(JSON.stringify({ request_id: "r", result: {} }), {
    headers: { "idempotent-replayed": "true" },
  });
  const { client } = scripted([response]);
  assert.equal((await client.respond("a", {})).replayed, true);
});

test("chat sends the thread references", async () => {
  const { client, seen } = scripted([ok()]);
  await client.chat("a", "hi", { previousResponseId: "r1" });
  assert.deepEqual(JSON.parse(seen[0]!.init.body as string), { message: "hi", previous_response_id: "r1" });
});

test("a safe error with a delay is retried after at least that delay, with one correlation id", async () => {
  const { client, seen, sleeps } = scripted([problem(429, "QUEUE_FULL", true, 3), ok()]);
  const result = await client.respond("a", {});
  assert.equal(result.requestId, "req_1");
  assert.equal(seen.length, 2);
  assert.ok(sleeps[0]! >= 3);
  assert.equal(header(seen[0]!.init, "X-Correlation-ID"), header(seen[1]!.init, "X-Correlation-ID"));
});

test("an error that is not safe to repeat is not retried", async () => {
  const { client, seen } = scripted([problem(504, "FOUNDRY_TIMEOUT", false), ok()]);
  await assert.rejects(client.respond("a", {}), (error: unknown) => {
    assert.ok(error instanceof PoolError);
    assert.equal(error.retrySafe, false);
    return true;
  });
  assert.equal(seen.length, 1);
});

test("a delay longer than the limit is returned, not waited for", async () => {
  const { client, seen, sleeps } = scripted([problem(429, "UPSTREAM_THROTTLED", true, 120), ok()], {
    maxRetryAfter: 30,
  });
  await assert.rejects(client.respond("a", {}), (error: unknown) => {
    assert.ok(error instanceof PoolError);
    assert.equal(error.retryAfterSeconds, 120);
    return true;
  });
  assert.equal(seen.length, 1);
  assert.equal(sleeps.length, 0);
});

test("retries stop at the limit", async () => {
  const { client, seen } = scripted(Array.from({ length: 5 }, () => problem(503, "FOUNDRY_CIRCUIT_OPEN", true, 1)), {
    maxRetries: 2,
  });
  await assert.rejects(client.respond("a", {}), PoolError);
  assert.equal(seen.length, 3);
});

test("client errors are never retried", async () => {
  const { client, seen } = scripted([problem(422, "VALIDATION_ERROR", true), ok()]);
  await assert.rejects(client.respond("a", {}), PoolError);
  assert.equal(seen.length, 1);
});

test("a refused connection is retried because nothing was sent", async () => {
  const refused = Object.assign(new TypeError("fetch failed"), { cause: { code: "ECONNREFUSED" } });
  const { client, seen, sleeps } = scripted([refused, ok()]);
  await client.respond("a", {});
  assert.equal(seen.length, 2);
  assert.equal(sleeps.length, 1);
  const other = new TypeError("fetch failed");
  const second = scripted([other, ok()]);
  await assert.rejects(second.client.respond("a", {}), TypeError);
  assert.equal(second.seen.length, 1);
});

test("the token is fetched for every attempt and may be async", async () => {
  const tokens = ["t1", "t2"];
  const { client, seen } = scripted([problem(429, "QUEUE_FULL", true, 1), ok()], {
    token: async () => tokens.shift()!,
  });
  await client.respond("a", {});
  assert.deepEqual(
    seen.map((c) => header(c.init, "Authorization")),
    ["Bearer t1", "Bearer t2"],
  );
});

test("an error that is not problem json still raises a PoolError", async () => {
  const { client } = scripted([new Response("bad gateway from a proxy", { status: 502, statusText: "Bad Gateway" })]);
  await assert.rejects(client.respond("a", {}), (error: unknown) => {
    assert.ok(error instanceof PoolError);
    assert.equal(error.status, 502);
    assert.equal(error.errorCode, "HTTP_ERROR");
    assert.match(error.detail, /bad gateway/);
    assert.equal(error.retrySafe, false);
    return true;
  });
});

test("problem fields are exposed on the error", async () => {
  const body = {
    title: "t",
    detail: "d",
    error_code: "AGENT_NOT_CONFIGURED",
    phase: "request",
    retry_safe: true,
    request_id: "req_9",
    correlation_id: "c9",
  };
  const { client } = scripted([new Response(JSON.stringify(body), { status: 404 })]);
  await assert.rejects(client.agent("ghost"), (error: unknown) => {
    assert.ok(error instanceof PoolError);
    assert.equal(error.errorCode, "AGENT_NOT_CONFIGURED");
    assert.equal(error.requestId, "req_9");
    assert.equal(error.correlationId, "c9");
    assert.match(error.message, /AGENT_NOT_CONFIGURED \(404\)/);
    return true;
  });
});

test("invoke sends any body type with the right content type and returns the raw response", async () => {
  const reply = () =>
    new Response('{"ok":true}', {
      status: 201,
      headers: { "content-type": "application/json", "x-request-id": "req_2" },
    });
  const { client, seen } = scripted([reply(), reply(), reply(), reply(), reply()]);
  const result = await client.invoke("inv", [1, 2, { a: 3 }]);
  assert.equal(result.status, 201);
  assert.deepEqual(result.json(), { ok: true });
  assert.equal(result.text(), '{"ok":true}');
  assert.equal(result.requestId, "req_2");
  assert.equal(seen[0]!.init.body, '[1,2,{"a":3}]');
  assert.equal(header(seen[0]!.init, "Content-Type"), "application/json");
  await client.invoke("inv", new Uint8Array([0, 255]));
  assert.equal(header(seen[1]!.init, "Content-Type"), "application/octet-stream");
  await client.invoke("inv", "plain", { contentType: "text/markdown", timeoutSeconds: 5 });
  assert.equal(header(seen[2]!.init, "Content-Type"), "text/markdown");
  assert.match(seen[2]!.url, /\/v1\/agents\/inv\/invocations\?timeout_seconds=5$/);
  await client.invoke("inv");
  assert.equal(seen[3]!.init.body, undefined);
  assert.equal(header(seen[3]!.init, "Content-Type"), undefined);
});

function sse(parts: string[]): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const part of parts) controller.enqueue(encoder.encode(part));
      controller.close();
    },
  });
  return new Response(stream, { headers: { "content-type": "text/event-stream" } });
}

async function collect(source: AsyncGenerator<SseEvent>): Promise<SseEvent[]> {
  const events: SseEvent[] = [];
  for await (const event of source) events.push(event);
  return events;
}

test("streaming yields events in order, even when the network splits them", async () => {
  const { client } = scripted([sse(['event: a\ndata: {"n"', ":1}\n\nevent: b\ndata: 2\n", "\n"])]);
  const events = await collect(client.streamResponses("a", { input: "x" }));
  assert.deepEqual(
    events.map((e) => [e.event, e.data]),
    [
      ["a", { n: 1 }],
      ["b", 2],
    ],
  );
});

test("a final error event throws a PoolStreamError after the earlier events", async () => {
  const error = {
    error_code: "FOUNDRY_UNAVAILABLE",
    title: "t",
    detail: "d",
    correlation_id: "c",
    request_id: "r",
    phase: "stream",
    retry_safe: false,
    retry_after_seconds: 10,
  };
  const { client } = scripted([sse(["event: a\ndata: 1\n\n", `event: error\ndata: ${JSON.stringify(error)}\n\n`])]);
  const seen: SseEvent[] = [];
  await assert.rejects(
    (async () => {
      for await (const event of client.streamResponses("a", {})) seen.push(event);
    })(),
    (thrown: unknown) => {
      assert.ok(thrown instanceof PoolStreamError);
      assert.equal(thrown.errorCode, "FOUNDRY_UNAVAILABLE");
      assert.equal(thrown.phase, "stream");
      assert.equal(thrown.retryAfterSeconds, 10);
      return true;
    },
  );
  assert.equal(seen.length, 1);
});

test("a failure before the stream starts is a normal PoolError", async () => {
  const { client } = scripted([problem(404, "AGENT_NOT_CONFIGURED", true)]);
  await assert.rejects(collect(client.streamResponses("ghost", {})), (error: unknown) => {
    assert.ok(error instanceof PoolError);
    assert.ok(!(error instanceof PoolStreamError));
    assert.equal(error.status, 404);
    return true;
  });
});

test("an invocations agent can stream events too", async () => {
  const { client, seen } = scripted([sse(["data: hello\n\n"])]);
  const events = await collect(client.streamInvocation("inv", { go: 1 }));
  assert.deepEqual(events.map((e) => e.data), ["hello"]);
  assert.match(seen[0]!.url, /\/invocations$/);
});

test("agents can be listed and described", async () => {
  const info = {
    name: "a",
    protocol: "responses",
    stateful: false,
    streaming: "request",
    supports_conversation_key: false,
    endpoints: ["/v1/agents/a/responses"],
    request_content_types: ["application/json"],
    queue_enabled: true,
    queue_max_wait_seconds: 120,
    idempotency_ttl_seconds: 600,
    agent_version: null,
    limits: {},
  };
  const { client, seen } = scripted([
    new Response(JSON.stringify([info])),
    new Response(JSON.stringify(info)),
  ]);
  assert.equal((await client.agents())[0]!.name, "a");
  assert.equal((await client.agent("a")).protocol, "responses");
  assert.equal(seen[0]!.url, "http://svc/v1/agents");
});

async function* text(...parts: string[]): AsyncGenerator<string> {
  for (const part of parts) yield part;
}

test("the parser handles comments, CRLF, multi-line data and a missing final blank line", async () => {
  const events = [];
  for await (const event of parseSse(text(': c\r\nevent: a\r\ndata: {"n":', "1}\r\n\r\ndata: l1\n", "data: l2\n\nevent: z\ndata: tail"))) {
    events.push([event.event, event.data]);
  }
  assert.deepEqual(events, [
    ["a", { n: 1 }],
    ["message", "l1\nl2"],
    ["z", "tail"],
  ]);
  const empty = [];
  for await (const event of parseSse(text("\n\nevent: x\n\n"))) empty.push(event);
  assert.deepEqual(empty, []);
});
