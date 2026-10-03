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


class AuthenticationRequiredError(AppError):
    status = 401
    code = "AUTHENTICATION_REQUIRED"
    title = "Authentication required"
    phase = "auth"
    retry_safe = True


class InsufficientScopeError(AppError):
    status = 403
    code = "INSUFFICIENT_SCOPE"
    title = "Insufficient scope"
    phase = "auth"
    retry_safe = True


class AgentNotConfiguredError(AppError):
    status = 404
    code = "AGENT_NOT_CONFIGURED"
    title = "Agent is not configured"
    phase = "request"
    retry_safe = True


class SessionNotFoundAdminError(AppError):
    status = 404
    code = "SESSION_NOT_FOUND"
    title = "Session not found"
    phase = "request"
    retry_safe = True


class ValidationFailedError(AppError):
    status = 422
    code = "VALIDATION_ERROR"
    title = "Validation failed"
    phase = "request"
    retry_safe = True


class PoolCapacityExceededError(AppError):
    status = 429
    code = "POOL_CAPACITY_EXCEEDED"
    title = "Pool capacity exceeded"
    phase = "queue"
    retry_safe = True


class QueueFullError(AppError):
    status = 429
    code = "QUEUE_FULL"
    title = "Agent queue is full"
    phase = "queue"
    retry_safe = True


class UpstreamThrottledError(AppError):
    status = 429
    code = "UPSTREAM_THROTTLED"
    title = "Upstream is throttling requests"
    phase = "invoke"
    retry_safe = True


class QueueWaitTimeoutError(AppError):
    status = 504
    code = "QUEUE_WAIT_TIMEOUT"
    title = "Timed out waiting in the queue"
    phase = "queue"
    retry_safe = True


class StickySessionTimeoutError(AppError):
    status = 504
    code = "STICKY_SESSION_TIMEOUT"
    title = "Timed out waiting for the affinity session"
    phase = "queue"
    retry_safe = True


class RequestTimeoutError(AppError):
    status = 504
    code = "REQUEST_TIMEOUT"
    title = "The request ran out of time"
    phase = "request"
    retry_safe = False


class StreamTimeoutError(AppError):
    status = 504
    code = "STREAM_TIMEOUT"
    title = "Stream time limit reached"
    phase = "stream"
    retry_safe = False


class ResponseTooLargeError(AppError):
    status = 502
    code = "RESPONSE_TOO_LARGE"
    title = "The agent response is too large"
    phase = "invoke"
    retry_safe = False


class FoundryTimeoutAppError(AppError):
    status = 504
    code = "FOUNDRY_TIMEOUT"
    title = "Foundry did not respond in time"
    phase = "invoke"
    retry_safe = False


class SessionNotFoundUpstreamError(AppError):
    status = 502
    code = "SESSION_NOT_FOUND"
    title = "Session no longer exists"
    phase = "invoke"
    retry_safe = True


class SessionFailedError(AppError):
    status = 502
    code = "SESSION_FAILED"
    title = "Session failed"
    phase = "invoke"
    retry_safe = False


class UpstreamError(AppError):
    status = 502
    code = "UPSTREAM_ERROR"
    title = "Upstream request failed"
    phase = "invoke"
    retry_safe = False


class FoundryCircuitOpenError(AppError):
    status = 503
    code = "FOUNDRY_CIRCUIT_OPEN"
    title = "Foundry circuit is open"
    phase = "invoke"
    retry_safe = True


class FoundryUnavailableError(AppError):
    status = 503
    code = "FOUNDRY_UNAVAILABLE"
    title = "Foundry is unavailable"
    phase = "invoke"
    retry_safe = False


class IdempotencyKeyReusedError(AppError):
    status = 422
    code = "IDEMPOTENCY_KEY_REUSED"
    title = "Idempotency key reused with a different request"
    phase = "request"
    retry_safe = True


class IdempotencyInProgressError(AppError):
    status = 409
    code = "IDEMPOTENCY_IN_PROGRESS"
    title = "A request with this idempotency key is still running"
    phase = "request"
    retry_safe = True


class SubjectRequiredError(AppError):
    status = 403
    code = "END_USER_REQUIRED"
    title = "An end user identity is required"
    phase = "auth"
    retry_safe = True


class ConfigInvalidError(AppError):
    status = 422
    code = "CONFIG_INVALID"
    title = "The configuration was rejected"
    phase = "request"
    retry_safe = True


class ReloadUnavailableError(AppError):
    status = 409
    code = "RELOAD_UNAVAILABLE"
    title = "The configuration cannot be reloaded"
    phase = "request"
    retry_safe = True


class ServiceDrainingError(AppError):
    status = 503
    code = "SERVICE_SHUTTING_DOWN"
    title = "The service is shutting down"
    phase = "request"
    retry_safe = True


class SyncInProgressError(AppError):
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
