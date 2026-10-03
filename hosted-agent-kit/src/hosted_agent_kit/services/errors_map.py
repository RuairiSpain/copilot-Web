"""Translation of Foundry adapter errors into the API's errors."""

from __future__ import annotations

import math

from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.domain.errors import (
    QUOTA_REGIONAL,
    AppError,
    FoundryCircuitOpen,
    FoundryCircuitOpenError,
    FoundryConflict,
    FoundryError,
    FoundryQuotaExceeded,
    FoundryRejected,
    FoundryResponseTooLarge,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryTimeoutAppError,
    FoundryUnavailableError,
    RegionalCapacityError,
    ResponseTooLargeError,
    SessionFailedError,
    SessionNotFoundUpstreamError,
    SessionQuotaError,
    UpstreamError,
    UpstreamThrottledError,
    ValidationFailedError,
)
from hosted_agent_kit.services.circuit_breaker import retry_after_seconds


def to_app_error(settings: KitSettings, exc: FoundryError, *, invoking: bool) -> AppError:
    error = _map(settings, exc, invoking=invoking)
    if not invoking:
        # Nothing reached the agent, so the same request can be retried as it is.
        error.phase = "create"
        error.retry_safe = True
    return error


def _map(settings: KitSettings, exc: FoundryError, *, invoking: bool) -> AppError:
    retry_after = settings.retry_after_seconds
    if isinstance(exc, FoundryThrottled):
        hint = exc.retry_after_seconds
        return UpstreamThrottledError(
            "Foundry is throttling requests.",
            retry_after_seconds=math.ceil(hint) if hint is not None else retry_after,
        )
    if isinstance(exc, FoundryQuotaExceeded):
        hint = exc.retry_after_seconds
        seconds = math.ceil(hint) if hint is not None else retry_after
        if exc.scope == QUOTA_REGIONAL:
            return RegionalCapacityError(
                "The regional session quota is full. Retry later or use another region.",
                retry_after_seconds=seconds,
            )
        return SessionQuotaError(
            "The session quota is full. Stop or delete sessions that are not needed, "
            "or request a higher limit.",
            retry_after_seconds=seconds,
        )
    if isinstance(exc, FoundryCircuitOpen):
        return FoundryCircuitOpenError(
            "Foundry is failing, so calls are paused. Try again shortly.",
            retry_after_seconds=retry_after_seconds(exc),
        )
    if isinstance(exc, FoundryConflict):
        return UpstreamError("Foundry reported a conflict.")
    if isinstance(exc, FoundryResponseTooLarge):
        return ResponseTooLargeError()
    if isinstance(exc, FoundryTimeout):
        return FoundryTimeoutAppError()
    if isinstance(exc, FoundrySessionFailed):
        return SessionFailedError()
    if isinstance(exc, FoundrySessionNotFound):
        if invoking:
            return SessionNotFoundUpstreamError()
        return UpstreamError("The agent was not found in Foundry.")
    if isinstance(exc, FoundryRejected):
        if invoking and exc.status_code in (400, 422):
            return ValidationFailedError("The agent rejected the request payload.")
        return UpstreamError()
    return FoundryUnavailableError(retry_after_seconds=retry_after)
