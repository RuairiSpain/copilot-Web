import { PoolError, PoolStreamError, type Problem } from "./errors.js";
import { parseSse, type SseEvent } from "./sse.js";

export interface Limits {
  max_body_bytes: number;
  default_timeout_seconds: number;
  max_timeout_seconds: number;
  max_stream_seconds: number;
  max_request_seconds: number | null;
  max_response_bytes: number | null;
}

export interface AgentInfo {
  name: string;
  protocol: "responses" | "invocations";
  stateful: boolean;
  /** `request`: the caller asks with `stream`. `agent`: the agent decides. */
  streaming: "request" | "agent";
  supports_conversation_key: boolean;
  endpoints: string[];
  request_content_types: string[];
  queue_enabled: boolean;
  queue_max_wait_seconds: number | null;
  idempotency_ttl_seconds: number;
  agent_version: string | null;
  limits: Limits;
}

export interface ResponsesResult {
  requestId: string;
  result: Record<string, unknown>;
  correlationId: string | undefined;
  /** True when the service replayed an earlier response for this idempotency key. */
  replayed: boolean;
}

export interface InvocationResult {
  status: number;
  contentType: string;
  body: Uint8Array;
  requestId: string | undefined;
  correlationId: string | undefined;
  replayed: boolean;
  headers: Headers;
  text(): string;
  json(): unknown;
}

export interface CallOptions {
  /** One of the caller's conversations with a stateful agent. Gets its own session. */
  conversationKey?: string;
  /** App-only callers: the end user the call is for. Needs the delegate role. */
  subject?: string;
  idempotencyKey?: string;
  timeoutSeconds?: number;
  signal?: AbortSignal;
}

export type InvokeBody = Uint8Array | string | Record<string, unknown> | unknown[] | number | boolean | null | undefined;
export type TokenProvider = string | (() => string | Promise<string>);

export interface PoolClientOptions {
  baseUrl: string;
  /** A bearer token, or a function that returns a fresh one for each attempt. */
  token?: TokenProvider;
  fetch?: typeof fetch;
  /** Retries for errors that are safe to repeat and carry a delay. Default 2. */
  maxRetries?: number;
  /** A longer delay than this (seconds) is returned on the error, not waited for. Default 30. */
  maxRetryAfter?: number;
  sleep?: (seconds: number) => Promise<void>;
}

const RETRYABLE_STATUS = new Set([429, 503, 504]);
const CONNECT_ERRORS = new Set(["ECONNREFUSED", "ENOTFOUND", "EAI_AGAIN"]);

function defaultSleep(seconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, seconds * 1000));
}

function encodeBody(body: InvokeBody, contentType?: string): { content?: BodyInit; type?: string } {
  if (body === undefined || body === null) return { type: contentType };
  if (body instanceof Uint8Array) {
    return { content: body as BodyInit, type: contentType ?? "application/octet-stream" };
  }
  if (typeof body === "string") {
    return { content: body, type: contentType ?? "text/plain; charset=utf-8" };
  }
  return { content: JSON.stringify(body), type: contentType ?? "application/json" };
}

async function errorFrom(response: Response): Promise<PoolError> {
  const text = await response.text();
  let problem: Problem = {};
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
      problem = parsed as Problem;
    }
  } catch {
    // Not JSON, for example a proxy's error page.
  }
  problem.title ??= response.statusText || "Request failed";
  problem.detail ??= text.slice(0, 200);
  problem.correlation_id ??= response.headers.get("x-correlation-id");
  problem.request_id ??= response.headers.get("x-request-id");
  return new PoolError(response.status, problem);
}

interface Call {
  method: "GET" | "POST";
  path: string;
  json?: unknown;
  content?: BodyInit;
  contentType?: string;
  headers?: Record<string, string>;
  query?: Record<string, string>;
  signal?: AbortSignal;
}

export class PoolClient {
  private readonly base: string;
  private readonly token: TokenProvider | undefined;
  private readonly fetchImpl: typeof fetch;
  private readonly maxRetries: number;
  private readonly maxRetryAfter: number;
  private readonly sleep: (seconds: number) => Promise<void>;

  constructor(options: PoolClientOptions) {
    this.base = options.baseUrl.replace(/\/+$/, "");
    this.token = options.token;
    this.fetchImpl = options.fetch ?? ((...args) => fetch(...args));
    this.maxRetries = options.maxRetries ?? 2;
    this.maxRetryAfter = options.maxRetryAfter ?? 30;
    this.sleep = options.sleep ?? defaultSleep;
  }

  // ------------------------------------------------------------------ discovery

  async agents(): Promise<AgentInfo[]> {
    const response = await this.send({ method: "GET", path: "/v1/agents" });
    return (await response.json()) as AgentInfo[];
  }

  async agent(name: string): Promise<AgentInfo> {
    const response = await this.send({ method: "GET", path: `/v1/agents/${encodeURIComponent(name)}` });
    return (await response.json()) as AgentInfo;
  }

  // ------------------------------------------------------------------ responses

  /** Call a Responses agent and return the full result. */
  async respond(agent: string, input: Record<string, unknown>, options: CallOptions = {}): Promise<ResponsesResult> {
    const body: Record<string, unknown> = { input };
    if (options.timeoutSeconds !== undefined) body.timeout_seconds = options.timeoutSeconds;
    const response = await this.send({
      method: "POST",
      path: `/v1/agents/${encodeURIComponent(agent)}/responses`,
      json: body,
      headers: this.callHeaders(options),
      signal: options.signal,
    });
    return this.responsesResult(response);
  }

  async chat(
    agent: string,
    message: string,
    options: CallOptions & { previousResponseId?: string; conversationId?: string } = {},
  ): Promise<ResponsesResult> {
    const body: Record<string, unknown> = { message };
    if (options.previousResponseId) body.previous_response_id = options.previousResponseId;
    if (options.conversationId) body.conversation_id = options.conversationId;
    const response = await this.send({
      method: "POST",
      path: `/v1/agents/${encodeURIComponent(agent)}/chat`,
      json: body,
      headers: this.callHeaders(options),
      signal: options.signal,
    });
    return this.responsesResult(response);
  }

  /** Yield the agent's events. A final error event throws a `PoolStreamError`. */
  streamResponses(agent: string, input: Record<string, unknown>, options: CallOptions = {}): AsyncGenerator<SseEvent> {
    const body: Record<string, unknown> = { input, stream: true };
    if (options.timeoutSeconds !== undefined) body.timeout_seconds = options.timeoutSeconds;
    return this.stream({
      method: "POST",
      path: `/v1/agents/${encodeURIComponent(agent)}/responses`,
      json: body,
      headers: this.callHeaders(options),
      signal: options.signal,
    });
  }

  // ---------------------------------------------------------------- invocations

  /** Call an Invocations agent. `body` may be bytes, text or any JSON value. */
  async invoke(
    agent: string,
    body?: InvokeBody,
    options: CallOptions & { contentType?: string } = {},
  ): Promise<InvocationResult> {
    const { content, type } = encodeBody(body, options.contentType);
    const query: Record<string, string> = {};
    if (options.timeoutSeconds !== undefined) query.timeout_seconds = String(options.timeoutSeconds);
    const response = await this.send({
      method: "POST",
      path: `/v1/agents/${encodeURIComponent(agent)}/invocations`,
      content,
      contentType: type,
      headers: this.callHeaders(options),
      query,
      signal: options.signal,
    });
    const bytes = new Uint8Array(await response.arrayBuffer());
    return {
      status: response.status,
      contentType: response.headers.get("content-type") ?? "",
      body: bytes,
      requestId: response.headers.get("x-request-id") ?? undefined,
      correlationId: response.headers.get("x-correlation-id") ?? undefined,
      replayed: response.headers.get("idempotent-replayed") === "true",
      headers: response.headers,
      text: () => new TextDecoder().decode(bytes),
      json: () => JSON.parse(new TextDecoder().decode(bytes)) as unknown,
    };
  }

  /** Call an Invocations agent that answers with server-sent events. */
  streamInvocation(
    agent: string,
    body?: InvokeBody,
    options: CallOptions & { contentType?: string } = {},
  ): AsyncGenerator<SseEvent> {
    const { content, type } = encodeBody(body, options.contentType);
    return this.stream({
      method: "POST",
      path: `/v1/agents/${encodeURIComponent(agent)}/invocations`,
      content,
      contentType: type,
      headers: this.callHeaders(options),
      signal: options.signal,
    });
  }

  // ------------------------------------------------------------------- plumbing

  private callHeaders(options: CallOptions): Record<string, string> {
    const headers: Record<string, string> = {};
    if (options.conversationKey) headers["X-Conversation-Key"] = options.conversationKey;
    if (options.subject) headers["X-Pool-Subject"] = options.subject;
    if (options.idempotencyKey) headers["Idempotency-Key"] = options.idempotencyKey;
    return headers;
  }

  private async responsesResult(response: Response): Promise<ResponsesResult> {
    const data = (await response.json()) as { request_id: string; result: Record<string, unknown> };
    return {
      requestId: data.request_id,
      result: data.result,
      correlationId: response.headers.get("x-correlation-id") ?? undefined,
      replayed: response.headers.get("idempotent-replayed") === "true",
    };
  }

  private async request(call: Call, correlationId: string): Promise<Response> {
    const headers: Record<string, string> = { "X-Correlation-ID": correlationId, ...call.headers };
    const source = this.token;
    const token = typeof source === "function" ? await source() : source;
    if (token) headers.Authorization = `Bearer ${token}`;
    let body: BodyInit | undefined = call.content;
    if (call.json !== undefined) {
      body = JSON.stringify(call.json);
      headers["Content-Type"] = "application/json";
    } else if (call.contentType) {
      headers["Content-Type"] = call.contentType;
    }
    const query = call.query && Object.keys(call.query).length > 0 ? `?${new URLSearchParams(call.query)}` : "";
    return this.fetchImpl(this.base + call.path + query, {
      method: call.method,
      headers,
      body,
      signal: call.signal,
    });
  }

  private async send(call: Call): Promise<Response> {
    const correlationId = crypto.randomUUID().replaceAll("-", ""); // one id for every attempt
    for (let attempt = 0; ; attempt++) {
      let response: Response;
      try {
        response = await this.request(call, correlationId);
      } catch (error) {
        const code = (error as { cause?: { code?: string } }).cause?.code;
        // Nothing was sent when the connection could not be made, so repeating is safe.
        if (code !== undefined && CONNECT_ERRORS.has(code) && attempt < this.maxRetries) {
          await this.sleep(Math.min(0.25 * 2 ** (attempt + 1), this.maxRetryAfter));
          continue;
        }
        throw error;
      }
      if (response.status < 400) return response;
      const failure = await errorFrom(response);
      const delay = this.retryDelay(failure, response, attempt);
      if (delay === undefined) throw failure;
      await this.sleep(delay);
    }
  }

  private retryDelay(failure: PoolError, response: Response, attempt: number): number | undefined {
    if (attempt >= this.maxRetries || !failure.retrySafe || !RETRYABLE_STATUS.has(response.status)) {
      return undefined;
    }
    let asked = failure.retryAfterSeconds;
    const header = response.headers.get("retry-after");
    if (asked === undefined && header !== null && /^\d+$/.test(header)) asked = Number(header);
    if (asked !== undefined && asked > this.maxRetryAfter) return undefined; // the caller decides
    return Math.max(asked ?? 0, Math.min(0.25 * 2 ** (attempt + 1), this.maxRetryAfter));
  }

  private async *stream(call: Call): AsyncGenerator<SseEvent> {
    const response = await this.request(call, crypto.randomUUID().replaceAll("-", ""));
    if (response.status >= 400) throw await errorFrom(response);
    if (response.body === null) return;
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    async function* chunks(): AsyncGenerator<string> {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        yield decoder.decode(value, { stream: true });
      }
    }
    try {
      for await (const event of parseSse(chunks())) {
        if (event.event === "error" && event.data !== null && typeof event.data === "object") {
          throw new PoolStreamError(event.data as Problem);
        }
        yield event;
      }
    } finally {
      await reader.cancel().catch(() => undefined);
    }
  }
}
