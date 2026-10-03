"""Port for the session store: records, leases, capacity reservations and change events."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum
from typing import Protocol

from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState
from hosted_agent_kit.domain.models import SessionAffinityKey, SessionRecord
from hosted_agent_kit.domain.resources import ConditionStatus, SessionKey


class ResourceConflictError(Exception):
    """An update used a ``resource_version`` that is no longer current."""


class ChangeType(StrEnum):
    ADDED = "Added"
    MODIFIED = "Modified"
    DELETED = "Deleted"


@dataclass(frozen=True)
class ChangeEvent:
    """A change to one session, delivered to subscribers as it happens (a watch)."""

    type: ChangeType
    key: SessionKey
    record: SessionRecord  # a snapshot taken after the change
    status_only: bool = False  # only conditions changed: nothing a controller must act on


ChangeListener = Callable[[ChangeEvent], None]


class SessionRegistry(Protocol):
    def subscribe(self, listener: ChangeListener) -> None:
        """Call ``listener`` after every change. It must not block or await."""
        ...

    async def add(self, record: SessionRecord) -> None: ...

    async def get(self, agent_name: str, session_id: str) -> SessionRecord | None: ...

    async def list(self, agent_name: str) -> list[SessionRecord]:
        """Return the agent's sessions in stable registry order."""
        ...

    async def count(self, agent_name: str) -> int: ...

    async def update(self, record: SessionRecord, *, expected_version: int) -> SessionRecord:
        """Replace a record if its ``resource_version`` is still ``expected_version``.

        Raises ``ResourceConflictError`` otherwise, so a stale read can never overwrite newer state.
        """
        ...

    async def try_lease(
        self, agent_name: str, session_id: str, request_id: str, now: datetime
    ) -> SessionRecord | None:
        """Lease an AVAILABLE session atomically; return None if it is not available."""
        ...

    async def release(
        self, agent_name: str, session_id: str, request_id: str, now: datetime
    ) -> SessionRecord | None:
        """Release a lease held by ``request_id``; return None if that request is not the holder."""
        ...

    async def refresh(
        self,
        agent_name: str,
        session_id: str,
        *,
        platform_status: FoundrySessionStatus,
        agent_version: str | None,
        last_accessed_at: datetime | None,
        expires_at: datetime | None,
        last_seen_at: datetime,
    ) -> SessionRecord | None:
        """Update platform metadata without touching local lease state."""
        ...

    async def set_local_state(
        self,
        agent_name: str,
        session_id: str,
        state: LocalSessionState,
        *,
        only_from: frozenset[LocalSessionState],
    ) -> bool:
        """Transition local state only when the current state is in ``only_from``."""
        ...

    async def mark_retiring(
        self,
        agent_name: str,
        session_id: str,
        *,
        cleanup: bool = True,
        reason: str | None = None,
        finalizers: tuple[str, ...] = (),
    ) -> SessionRecord | None:
        """Request deletion, even while leased. A lease holder is kept.

        Sets ``deletion_timestamp``. With ``cleanup`` the session also gets the cleanup finalizer,
        which stays until the remote session is confirmed gone.
        """
        ...

    async def remove_finalizer(
        self, agent_name: str, session_id: str, finalizer: str
    ) -> SessionRecord | None: ...

    async def set_condition(
        self,
        agent_name: str,
        session_id: str,
        type_: str,
        status: ConditionStatus,
        reason: str,
        message: str = "",
    ) -> SessionRecord | None: ...

    async def bind(self, agent_name: str, session_id: str, key: SessionAffinityKey) -> bool: ...

    async def remove(self, agent_name: str, session_id: str) -> SessionRecord | None: ...

    async def reserve_slot(
        self, agent_name: str, token: str, limit: int, *, force: bool = False
    ) -> bool:
        """Reserve capacity for an in-flight creation if sessions plus reservations < limit.

        ``force`` reserves regardless, for a replacement that takes the place of a session that
        was just given up.
        """
        ...

    async def release_slot(self, agent_name: str, token: str) -> None: ...

    async def release_expired_slots(self, agent_name: str, older_than_seconds: float) -> int:
        """Release reservations nobody used or released. Returns how many."""
        ...

    async def reserved(self, agent_name: str) -> int: ...
