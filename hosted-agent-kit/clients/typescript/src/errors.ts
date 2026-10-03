/** Problem details as the service returns them (RFC 7807 plus pool fields). */
export interface Problem {
  type?: string;
  title?: string;
  status?: number;
  detail?: string;
  error_code?: string;
  /** Where the request failed: request, auth, queue, create, invoke or stream. */
  phase?: string;
  /** True when repeating the same request cannot repeat work the agent already did. */
  retry_safe?: boolean;
  retry_after_seconds?: number;
  request_id?: string | null;
  correlation_id?: string | null;
  [key: string]: unknown;
}

export class PoolError extends Error {
  readonly status: number;
  readonly problem: Problem;
  readonly errorCode: string;
  readonly title: string;
  readonly detail: string;
  readonly phase: string;
  readonly retrySafe: boolean;
  readonly retryAfterSeconds: number | undefined;
  readonly requestId: string | undefined;
  readonly correlationId: string | undefined;

  constructor(status: number, problem: Problem) {
    const code = problem.error_code ?? "HTTP_ERROR";
    const detail = problem.detail ?? problem.title ?? "Request failed";
    super(`${code} (${status}): ${detail}`);
    this.name = "PoolError";
    this.status = status;
    this.problem = problem;
    this.errorCode = code;
    this.title = problem.title ?? "Request failed";
    this.detail = detail;
    this.phase = problem.phase ?? "request";
    this.retrySafe = problem.retry_safe ?? false;
    this.retryAfterSeconds =
      typeof problem.retry_after_seconds === "number" ? problem.retry_after_seconds : undefined;
    this.requestId = problem.request_id ?? undefined;
    this.correlationId = problem.correlation_id ?? undefined;
  }
}

/** A stream failed after it had started: the service sent a final `error` event. */
export class PoolStreamError extends PoolError {
  constructor(problem: Problem) {
    super(200, { phase: "stream", ...problem });
    this.name = "PoolStreamError";
  }
}
