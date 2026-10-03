"""Errors raised by the kit. Catch ``HackError`` for any of them.

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
    ClientDisconnectedError,
    ConfigInvalidError,
    IdempotencyInProgressError,
    IdempotencyKeyReusedError,
    PoolCapacityExceededError,
    QueueFullError,
    QueueWaitTimeoutError,
    ReloadUnavailableError,
    RequestTimeoutError,
    ResponseTooLargeError,
    ServiceDrainingError,
    SessionFailedError,
    SessionNotFoundAdminError,
    SessionNotFoundUpstreamError,
    StickySessionTimeoutError,
    StreamTimeoutError,
    SyncInProgressError,
    UpstreamError,
    UpstreamThrottledError,
    ValidationFailedError,
)

HackError = AppError

__all__ = [
    "AgentNotConfiguredError",
    "ClientDisconnectedError",
    "ConfigError",
    "ConfigInvalidError",
    "HackError",
    "IdempotencyInProgressError",
    "IdempotencyKeyReusedError",
    "PoolCapacityExceededError",
    "QueueFullError",
    "QueueWaitTimeoutError",
    "ReloadUnavailableError",
    "RequestTimeoutError",
    "ResponseTooLargeError",
    "ServiceDrainingError",
    "SessionFailedError",
    "SessionNotFoundAdminError",
    "SessionNotFoundUpstreamError",
    "StickySessionTimeoutError",
    "StreamTimeoutError",
    "SyncInProgressError",
    "UpstreamError",
    "UpstreamThrottledError",
    "ValidationFailedError",
]
