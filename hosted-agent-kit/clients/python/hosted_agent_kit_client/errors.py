"""Errors raised by the client. Each carries the service's problem details."""

from __future__ import annotations

from typing import Any


class PoolError(Exception):
    """The service returned an error (RFC 7807 problem details)."""

    def __init__(self, status: int, problem: dict[str, Any]) -> None:
        self.status = status
        self.problem = problem
        self.error_code: str = str(problem.get("error_code", "HTTP_ERROR"))
        self.title: str = str(problem.get("title", "Request failed"))
        self.detail: str = str(problem.get("detail", self.title))
        # Where the request failed: request, auth, queue, create, invoke or stream.
        self.phase: str = str(problem.get("phase", "request"))
        # True when repeating the same request cannot repeat work the agent already did.
        self.retry_safe: bool = bool(problem.get("retry_safe", False))
        retry_after = problem.get("retry_after_seconds")
        self.retry_after_seconds: float | None = (
            float(retry_after) if isinstance(retry_after, int | float) else None
        )
        self.request_id: str | None = problem.get("request_id")
        self.correlation_id: str | None = problem.get("correlation_id")
        super().__init__(f"{self.error_code} ({status}): {self.detail}")


class PoolStreamError(PoolError):
    """A stream failed after it had started: the service sent a final ``error`` event."""

    def __init__(self, problem: dict[str, Any]) -> None:
        super().__init__(200, problem)
        self.phase = str(problem.get("phase", "stream"))
