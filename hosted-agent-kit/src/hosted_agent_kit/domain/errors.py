"""Domain and HTTP-mapped errors.

``AppError`` subclasses map one-to-one onto the error model in the PRD (section 14).
``FoundryError`` subclasses are raised by the Foundry adapter and translated by the
pool service, so the API layer never sees SDK types.
"""

from __future__ import annotations


class AppError(Exception):
    """An error with a stable code and HTTP mapping."""

    status: int = 500
    code: str = "INTERNAL_ERROR"
    title: str = "Internal error"
    # Where the request was when it failed: request, auth, queue, create, invoke or stream.
    phase: str = "request"
    # True when a retry of the same request cannot repeat work the agent may already have done.
    retry_safe: bool = False

    def __init__(
        self,
        detail: str | None = None,
        *,
        retry_after_seconds: int | None = None,
        headers: dict[str, str] | None = None,
    ) -> None:
        self.detail = detail or self.title
        self.retry_after_seconds = retry_after_seconds
        self.headers = headers or {}
        super().__init__(self.detail)


class CapacityError(AppError):
    """No session could be given right now. Retry after ``retry_after_seconds``, or use another
    region or kit. The request did not reach the agent, so retrying is safe."""


class UpstreamFailureError(AppError):
    """Foundry or the agent failed or timed out. Whether a retry is safe is in ``retry_safe``."""


class CallerError(AppError):
    """The request cannot succeed as sent: fix it (credentials, agent name, format) first."""


class NotServingError(AppError):
    """This process is shutting down or is a standby. Retry shortly, or against another kit."""


class AuthenticationRequiredError(CallerError):
    status = 401
    code = "AUTHENTICATION_REQUIRED"
    title = "Authentication required"
    phase = "auth"
    retry_safe = True


class InsufficientScopeError(CallerError):
    status = 403
    code = "INSUFFICIENT_SCOPE"
    title = "Insufficient scope"
    phase = "auth"
    retry_safe = True


class AgentNotConfiguredError(CallerError):
    status = 404
    code = "AGENT_NOT_CONFIGURED"
    title = "Agent is not configured"
    phase = "request"
    retry_safe = True


class SessionNotFoundAdminError(CallerError):
    status = 404
    code = "SESSION_NOT_FOUND"
    title = "Session not found"
    phase = "request"
    retry_safe = True


class ValidationFailedError(CallerError):
    status = 422
    code = "VALIDATION_ERROR"
    title = "Validation failed"
    phase = "request"
    retry_safe = True


class PoolCapacityExceededError(CapacityError):
    status = 429
    code = "POOL_CAPACITY_EXCEEDED"
    title = "Pool capacity exceeded"
    phase = "queue"
    retry_safe = True


class QueueFullError(CapacityError):
    status = 429
    code = "QUEUE_FULL"
    title = "Agent queue is full"
    phase = "queue"
    retry_safe = True


class RegionalCapacityError(CapacityError):
    """The subscription's regional session quota is full. Retrying later can succeed."""

    status = 429
    code = "REGIONAL_SESSION_QUOTA_EXCEEDED"
    title = "The regional session quota is full"
    phase = "invoke"
    retry_safe = True


class SessionQuotaError(CapacityError):
    """The session quota is full. Retrying does not help until sessions are stopped or deleted."""

    status = 429
    code = "SESSION_QUOTA_EXCEEDED"
    title = "The session quota is full"
    phase = "invoke"
    retry_safe = True


class UpstreamThrottledError(CapacityError):
    status = 429
    code = "UPSTREAM_THROTTLED"
    title = "Upstream is throttling requests"
    phase = "invoke"
    retry_safe = True


class QueueWaitTimeoutError(CapacityError):
    status = 504
    code = "QUEUE_WAIT_TIMEOUT"
    title = "Timed out waiting in the queue"
    phase = "queue"
    retry_safe = True


class StickySessionTimeoutError(CapacityError):
    status = 504
    code = "STICKY_SESSION_TIMEOUT"
    title = "Timed out waiting for the affinity session"
    phase = "queue"
    retry_safe = True


class RequestTimeoutError(UpstreamFailureError):
    status = 504
    code = "REQUEST_TIMEOUT"
    title = "The request ran out of time"
    phase = "request"
    retry_safe = False


class StreamTimeoutError(UpstreamFailureError):
    status = 504
    code = "STREAM_TIMEOUT"
    title = "Stream time limit reached"
    phase = "stream"
    retry_safe = False


class ResponseTooLargeError(UpstreamFailureError):
    status = 502
    code = "RESPONSE_TOO_LARGE"
    title = "The agent response is too large"
    phase = "invoke"
    retry_safe = False


class FoundryTimeoutAppError(UpstreamFailureError):
    status = 504
    code = "FOUNDRY_TIMEOUT"
    title = "Foundry did not respond in time"
    phase = "invoke"
    retry_safe = False


class SessionNotFoundUpstreamError(UpstreamFailureError):
    status = 502
    code = "SESSION_NOT_FOUND"
    title = "Session no longer exists"
    phase = "invoke"
    retry_safe = True


class SessionFailedError(UpstreamFailureError):
    status = 502
    code = "SESSION_FAILED"
    title = "Session failed"
    phase = "invoke"
    retry_safe = False


class UpstreamError(UpstreamFailureError):
    status = 502
    code = "UPSTREAM_ERROR"
    title = "Upstream request failed"
    phase = "invoke"
    retry_safe = False


class FoundryCircuitOpenError(UpstreamFailureError):
    status = 503
    code = "FOUNDRY_CIRCUIT_OPEN"
    title = "Foundry circuit is open"
    phase = "invoke"
    retry_safe = True


class FoundryUnavailableError(UpstreamFailureError):
    status = 503
    code = "FOUNDRY_UNAVAILABLE"
    title = "Foundry is unavailable"
    phase = "invoke"
    retry_safe = False


class IdempotencyKeyReusedError(CallerError):
    status = 422
    code = "IDEMPOTENCY_KEY_REUSED"
    title = "Idempotency key reused with a different request"
    phase = "request"
    retry_safe = True


class IdempotencyInProgressError(CallerError):
    status = 409
    code = "IDEMPOTENCY_IN_PROGRESS"
    title = "A request with this idempotency key is still running"
    phase = "request"
    retry_safe = True


class SubjectRequiredError(CallerError):
    status = 403
    code = "END_USER_REQUIRED"
    title = "An end user identity is required"
    phase = "auth"
    retry_safe = True


class ConfigInvalidError(CallerError):
    status = 422
    code = "CONFIG_INVALID"
    title = "The configuration was rejected"
    phase = "request"
    retry_safe = True


class ReloadUnavailableError(CallerError):
    status = 409
    code = "RELOAD_UNAVAILABLE"
    title = "The configuration cannot be reloaded"
    phase = "request"
    retry_safe = True


class RequestTooLargeError(CallerError):
    status = 413
    code = "REQUEST_TOO_LARGE"
    title = "The request body is too large"
    phase = "request"
    retry_safe = True


class WrongShardError(CallerError):
    """This kit serves another shard of the agent. Send the call to the shard named in the error."""

    status = 421
    code = "WRONG_SHARD"
    title = "This user is served by another shard"
    phase = "request"
    retry_safe = True

    def __init__(self, detail: str | None = None, *, shard: int, count: int) -> None:
        super().__init__(detail)
        self.shard = shard
        self.count = count

    @property
    def problem_extra(self) -> dict[str, int]:
        return {"shard": self.shard, "shard_count": self.count}


class NotActiveError(NotServingError):
    """This kit is a standby or has lost ownership of its agents. Another kit is serving them."""

    status = 503
    code = "KIT_NOT_ACTIVE"
    title = "This kit is not the active owner of its agents"
    phase = "request"
    retry_safe = True


class OwnershipConflictError(Exception):
    """Another live kit owns an agent this kit was asked to schedule."""


class ServiceDrainingError(NotServingError):
    status = 503
    code = "SERVICE_SHUTTING_DOWN"
    title = "The service is shutting down"
    phase = "request"
    retry_safe = True


class SyncInProgressError(CallerError):
    status = 409
    code = "SYNC_IN_PROGRESS"
    title = "Reconciliation already running"
    phase = "request"
    retry_safe = True


class ClientDisconnectedError(Exception):
    """Raised internally when the caller went away; never mapped to a response."""


class FoundryError(Exception):
    """Base class for errors raised by the Foundry adapter."""


class FoundrySessionNotFound(FoundryError):
    """The session (or its route) does not exist."""


class FoundrySessionFailed(FoundryError):
    """The session is in a failed state or failed while executing."""


class FoundryThrottled(FoundryError):
    def __init__(self, retry_after_seconds: float | None = None) -> None:
        super().__init__("throttled")
        self.retry_after_seconds = retry_after_seconds


QUOTA_REGIONAL = "regional"
QUOTA_SESSION = "session"


class FoundryQuotaExceeded(FoundryError):
    """HTTP 429 that names a session quota (not request throttling).

    ``scope`` is ``regional`` (``regional_session_quota_exceeded``: retry with backoff, or use
    another region) or ``session`` (``session_quota_exceeded``: stop or delete sessions first).
    """

    def __init__(self, scope: str, retry_after_seconds: float | None = None) -> None:
        super().__init__(f"{scope} session quota exceeded")
        self.scope = scope
        self.retry_after_seconds = retry_after_seconds


class FoundryTimeout(FoundryError):
    """The upstream call timed out."""


class FoundryUnavailable(FoundryError):
    """Transient connectivity or 5xx failure."""


class FoundryConflict(FoundryError):
    """The resource already exists (HTTP 409), for example a caller-chosen session id."""


class FoundryResponseTooLarge(FoundryError):
    """A non-streaming agent response is larger than the pool is willing to buffer."""


class FoundryCircuitOpen(FoundryError):
    """The circuit breaker is open: the call was not sent to Foundry."""

    def __init__(self, retry_after_seconds: float) -> None:
        super().__init__("circuit open")
        self.retry_after_seconds = retry_after_seconds


class FoundryRejected(FoundryError):
    """Non-retryable upstream rejection (4xx other than 404 and 429)."""

    def __init__(self, status_code: int) -> None:
        super().__init__(f"upstream rejected request with status {status_code}")
        self.status_code = status_code
