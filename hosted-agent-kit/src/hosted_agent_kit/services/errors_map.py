"""Translation of Foundry adapter errors into the API's errors."""

from __future__ import annotations

import math

from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.domain.errors import (
    AppError,
    FoundryCircuitOpen,
    FoundryCircuitOpenError,
    FoundryConflict,
    FoundryError,
    FoundryRejected,
    FoundryResponseTooLarge,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryTimeoutAppError,
    FoundryUnavailableError,
    ResponseTooLargeError,
    SessionFailedError,
    SessionNotFoundUpstreamError,
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
