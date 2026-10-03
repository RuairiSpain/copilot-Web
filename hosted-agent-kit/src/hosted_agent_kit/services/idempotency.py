"""Request deduplication for ``Idempotency-Key``.

A key is scoped to the caller and the agent. The first request with a key runs. A repeat with the
same request returns the stored response. A repeat while the first is still running is refused, and
a repeat with a different request is an error, so one key can never stand for two different calls.
Only successful, non-streaming responses are stored: a failure is not remembered, so a retry runs
again. State is in memory, like the rest of the service, so it does not survive a restart.
"""

from __future__ import annotations

import hashlib
from dataclasses import dataclass, field
from enum import StrEnum

from hosted_agent_kit.services.clock import Clock


class Outcome(StrEnum):
    NEW = "new"
    REPLAY = "replay"
    IN_PROGRESS = "in_progress"
    MISMATCH = "mismatch"


@dataclass(frozen=True)
class StoredResponse:
    status_code: int
    media_type: str
    body: bytes


@dataclass
class _Entry:
    fingerprint: str
    expires_at: float
    response: StoredResponse | None = None
    created_at: float = field(default=0.0)


def fingerprint(*parts: bytes | str) -> str:
    """A stable digest of the parts that make two requests the same request."""
    digest = hashlib.sha256()
    for part in parts:
        data = part.encode() if isinstance(part, str) else part
        digest.update(len(data).to_bytes(8, "big"))
        digest.update(data)
    return digest.hexdigest()


class IdempotencyStore:
    def __init__(
        self, clock: Clock, *, ttl_seconds: float, max_entries: int, max_body_bytes: int
    ) -> None:
        self._clock = clock
        self._ttl = ttl_seconds
        self._max_entries = max_entries
        self._max_body_bytes = max_body_bytes
        self._entries: dict[str, _Entry] = {}

    @property
    def enabled(self) -> bool:
        return self._ttl > 0

    @property
    def ttl_seconds(self) -> float:
        return self._ttl

    def begin(self, scope: str, request_fingerprint: str) -> tuple[Outcome, StoredResponse | None]:
        """Claim a key. The caller must then call ``complete`` or ``abandon``."""
        now = self._clock.monotonic()
        self._expire(now)
        entry = self._entries.get(scope)
        if entry is not None:
            if entry.fingerprint != request_fingerprint:
                return Outcome.MISMATCH, None
            if entry.response is None:
                return Outcome.IN_PROGRESS, None
            return Outcome.REPLAY, entry.response
        self._make_room()
        self._entries[scope] = _Entry(request_fingerprint, now + self._ttl, created_at=now)
        return Outcome.NEW, None

    def complete(self, scope: str, response: StoredResponse) -> None:
        """Remember a successful response. One that is too large is not kept."""
        entry = self._entries.get(scope)
        if entry is None:
            return
        if len(response.body) > self._max_body_bytes:
            del self._entries[scope]
            return
        entry.response = response
        entry.expires_at = self._clock.monotonic() + self._ttl

    def abandon(self, scope: str) -> None:
        """Forget a claim whose request did not produce a storable response."""
        entry = self._entries.get(scope)
        if entry is not None and entry.response is None:
            del self._entries[scope]

    def _expire(self, now: float) -> None:
        for scope in [s for s, e in self._entries.items() if e.expires_at <= now and e.response]:
            del self._entries[scope]

    def _make_room(self) -> None:
        overflow = len(self._entries) - self._max_entries + 1
        if overflow > 0:
            finished = sorted(
                (e.created_at, s) for s, e in self._entries.items() if e.response is not None
            )
            for _, scope in finished[:overflow]:
                del self._entries[scope]
