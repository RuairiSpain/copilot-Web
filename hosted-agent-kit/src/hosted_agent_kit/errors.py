"""Errors raised by the kit. Catch ``HackError`` for any of them.

Four groups say what to do. ``CapacityError``: retry after ``retry_after_seconds`` (the request did
not reach the agent). ``UpstreamFailureError``: Foundry or the agent failed; retry only when
``retry_safe``. ``CallerError``: fix the request first. ``NotServingError``: this process is
shutting down or a standby; retry against another kit.

Every error has ``status`` (the HTTP status it maps to), ``code`` (a stable string),
``phase`` (where the call was: request, auth, queue, create, invoke, stream),
``retry_safe`` (True when retrying cannot repeat work the agent already did) and, for
capacity and throttling errors, ``retry_after_seconds``.
"""

from __future__ import annotations

from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.domain.errors import (
    AgentNotConfiguredError,
    AppError,
    CallerError,
    CapacityError,
    ClientDisconnectedError,
    ConfigInvalidError,
    IdempotencyInProgressError,
    IdempotencyKeyReusedError,
    NotActiveError,
    NotServingError,
    OwnershipConflictError,
    PoolCapacityExceededError,
    QueueFullError,
    QueueWaitTimeoutError,
    RegionalCapacityError,
    ReloadUnavailableError,
    RequestTimeoutError,
    RequestTooLargeError,
    ResponseTooLargeError,
    ServiceDrainingError,
    SessionFailedError,
    SessionNotFoundAdminError,
    SessionNotFoundUpstreamError,
    SessionQuotaError,
    StickySessionTimeoutError,
    StreamTimeoutError,
    SyncInProgressError,
    UpstreamError,
    UpstreamFailureError,
    UpstreamThrottledError,
    ValidationFailedError,
    WrongShardError,
)

HackError = AppError

__all__ = [
    "AgentNotConfiguredError",
    "CallerError",
    "CapacityError",
    "ClientDisconnectedError",
    "ConfigError",
    "ConfigInvalidError",
    "HackError",
    "IdempotencyInProgressError",
    "IdempotencyKeyReusedError",
    "NotActiveError",
    "NotServingError",
    "OwnershipConflictError",
    "PoolCapacityExceededError",
    "QueueFullError",
    "QueueWaitTimeoutError",
    "RegionalCapacityError",
    "ReloadUnavailableError",
    "RequestTimeoutError",
    "RequestTooLargeError",
    "ResponseTooLargeError",
    "ServiceDrainingError",
    "SessionFailedError",
    "SessionNotFoundAdminError",
    "SessionNotFoundUpstreamError",
    "SessionQuotaError",
    "StickySessionTimeoutError",
    "StreamTimeoutError",
    "SyncInProgressError",
    "UpstreamError",
    "UpstreamFailureError",
    "UpstreamThrottledError",
    "ValidationFailedError",
    "WrongShardError",
]
