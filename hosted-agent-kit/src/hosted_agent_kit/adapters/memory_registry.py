"""In-memory session store (Version 1).

All methods are free of ``await`` points, so each call is atomic on the event loop. Every change
to a record bumps its ``resource_version``, refreshes its derived conditions and is announced to
subscribers, which is what lets controllers react to state instead of polling for it.
"""

from __future__ import annotations

from collections.abc import Sequence
from datetime import datetime

from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState
from hosted_agent_kit.domain.models import SessionAffinityKey, SessionRecord
from hosted_agent_kit.domain.resources import (
    FINALIZER_SESSION_CLEANUP,
    ConditionStatus,
    derive_session_conditions,
    set_condition,
)
from hosted_agent_kit.ports.registry import (
    ChangeEvent,
    ChangeListener,
    ChangeType,
    ResourceConflictError,
)
from hosted_agent_kit.services.clock import Clock, SystemClock


def _order_key(record: SessionRecord) -> tuple[datetime, str]:
    return (record.created_at, record.session_id)


# A session id is unique only within one agent endpoint, so every record is keyed by both.
_Key = tuple[str, str]


class MemorySessionRegistry:
    def __init__(self, clock: Clock | None = None) -> None:
        self._clock = clock or SystemClock()
        self._records: dict[_Key, SessionRecord] = {}
        self._by_agent: dict[str, dict[str, SessionRecord]] = {}  # the same records, by agent
        self._slots: dict[str, dict[str, float]] = {}
        self._listeners: list[ChangeListener] = []

    # ------------------------------------------------------------------ plumbing

    def subscribe(self, listener: ChangeListener) -> None:
        self._listeners.append(listener)

    def _emit(
        self, change: ChangeType, record: SessionRecord, *, status_only: bool = False
    ) -> None:
        if not self._listeners:
            return
        event = ChangeEvent(change, record.key, record.model_copy(deep=True), status_only)
        for listener in self._listeners:
            listener(event)

    def _commit(self, record: SessionRecord, *, desired: bool = False) -> None:
        """Record a change: bump the version, refresh conditions and announce it."""
        record.resource_version += 1
        if desired:
            record.generation += 1
        record.conditions = derive_session_conditions(record, self._clock.now())
        self._emit(ChangeType.MODIFIED, record)

    # --------------------------------------------------------------------- reads

    async def get(self, agent_name: str, session_id: str) -> SessionRecord | None:
        record = self._records.get((agent_name, session_id))
        return record.model_copy(deep=True) if record else None

    async def list(self, agent_name: str) -> list[SessionRecord]:
        return [r.model_copy(deep=True) for r in await self.view(agent_name)]

    async def view(self, agent_name: str) -> Sequence[SessionRecord]:
        return sorted(self._by_agent.get(agent_name, {}).values(), key=_order_key)

    async def count(self, agent_name: str) -> int:
        return len(self._by_agent.get(agent_name, {}))

    def _store(self, record: SessionRecord) -> None:
        self._records[(record.agent_name, record.session_id)] = record
        self._by_agent.setdefault(record.agent_name, {})[record.session_id] = record

    def _discard(self, agent_name: str, session_id: str) -> SessionRecord | None:
        record = self._records.pop((agent_name, session_id), None)
        self._by_agent.get(agent_name, {}).pop(session_id, None)
        return record

    # -------------------------------------------------------------------- writes

    async def add(self, record: SessionRecord) -> None:
        key = (record.agent_name, record.session_id)
        if key in self._records:
            raise ValueError("session is already registered")
        stored = record.model_copy(deep=True)
        stored.resource_version = 1
        stored.conditions = derive_session_conditions(stored, self._clock.now())
        self._store(stored)
        self._emit(ChangeType.ADDED, stored)

    async def update(self, record: SessionRecord, *, expected_version: int) -> SessionRecord:
        key = (record.agent_name, record.session_id)
        current = self._records.get(key)
        if current is None or current.resource_version != expected_version:
            raise ResourceConflictError(f"{record.key} changed since version {expected_version}")
        desired = (
            record.affinity_key != current.affinity_key
            or record.deletion_timestamp != current.deletion_timestamp
        )
        stored = record.model_copy(deep=True)
        stored.resource_version = current.resource_version
        stored.generation = current.generation
        self._store(stored)
        self._commit(stored, desired=desired)
        return stored.model_copy(deep=True)

    async def try_lease(
        self, agent_name: str, session_id: str, request_id: str, now: datetime
    ) -> SessionRecord | None:
        record = self._records.get((agent_name, session_id))
        if record is None or record.local_state is not LocalSessionState.AVAILABLE:
            return None
        record.local_state = LocalSessionState.LEASED
        record.lease_request_id = request_id
        record.lease_acquired_at = now
        self._commit(record)
        return record.model_copy(deep=True)

    async def release(
        self, agent_name: str, session_id: str, request_id: str, now: datetime
    ) -> SessionRecord | None:
        record = self._records.get((agent_name, session_id))
        if record is None or record.lease_request_id != request_id:
            return None
        record.lease_request_id = None
        record.lease_acquired_at = None
        record.last_released_at = now
        if record.local_state is LocalSessionState.LEASED:
            record.local_state = LocalSessionState.AVAILABLE
        self._commit(record)
        return record.model_copy(deep=True)

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
        record = self._records.get((agent_name, session_id))
        if record is None:
            return None
        changed = (
            record.platform_status != platform_status
            or record.agent_version != agent_version
            or record.expires_at != expires_at
        )
        touched = record.last_accessed_at != last_accessed_at
        record.platform_status = platform_status
        record.agent_version = agent_version
        record.last_accessed_at = last_accessed_at
        record.expires_at = expires_at
        record.last_seen_at = last_seen_at  # a heartbeat: it does not count as a change
        if changed:
            self._commit(record)
        elif touched:
            # Foundry saw the session used. Nothing for a controller to do, but the quota count
            # extends the session's idle window from it.
            self._emit(ChangeType.MODIFIED, record, status_only=True)
        return record.model_copy(deep=True)

    async def set_local_state(
        self,
        agent_name: str,
        session_id: str,
        state: LocalSessionState,
        *,
        only_from: frozenset[LocalSessionState],
    ) -> bool:
        record = self._records.get((agent_name, session_id))
        if record is None or record.local_state not in only_from:
            return False
        if record.local_state is not state:
            record.local_state = state
            self._commit(record)
        return True

    async def mark_retiring(
        self,
        agent_name: str,
        session_id: str,
        *,
        cleanup: bool = True,
        reason: str | None = None,
        finalizers: tuple[str, ...] = (),
    ) -> SessionRecord | None:
        record = self._records.get((agent_name, session_id))
        if record is None:
            return None
        before = (record.local_state, record.deletion_timestamp, tuple(record.finalizers))
        record.local_state = LocalSessionState.RETIRING
        if record.deletion_timestamp is None:
            record.deletion_timestamp = self._clock.now()
            record.deletion_reason = reason
        wanted = (FINALIZER_SESSION_CLEANUP,) if cleanup else ()
        for finalizer in (*wanted, *finalizers):
            if finalizer not in record.finalizers:
                record.finalizers = [*record.finalizers, finalizer]
        after = (record.local_state, record.deletion_timestamp, tuple(record.finalizers))
        if after != before:
            self._commit(record, desired=before[1] is None)
        return record.model_copy(deep=True)

    async def remove_finalizer(
        self, agent_name: str, session_id: str, finalizer: str
    ) -> SessionRecord | None:
        record = self._records.get((agent_name, session_id))
        if record is None:
            return None
        if finalizer in record.finalizers:
            record.finalizers = [f for f in record.finalizers if f != finalizer]
            self._commit(record)
        return record.model_copy(deep=True)

    async def set_condition(
        self,
        agent_name: str,
        session_id: str,
        type_: str,
        status: ConditionStatus,
        reason: str,
        message: str = "",
    ) -> SessionRecord | None:
        record = self._records.get((agent_name, session_id))
        if record is None:
            return None
        updated = set_condition(
            record.conditions, type_, status, reason, message, now=self._clock.now()
        )
        if updated is not record.conditions:
            record.conditions = updated
            record.resource_version += 1
            self._emit(ChangeType.MODIFIED, record, status_only=True)
        return record.model_copy(deep=True)

    async def bind(self, agent_name: str, session_id: str, key: SessionAffinityKey) -> bool:
        record = self._records.get((agent_name, session_id))
        if record is None:
            return False
        if record.affinity_key != key:
            record.affinity_key = key
            self._commit(record, desired=True)
        return True

    async def remove(self, agent_name: str, session_id: str) -> SessionRecord | None:
        record = self._discard(agent_name, session_id)
        if record is None:
            return None
        record.resource_version += 1
        self._emit(ChangeType.DELETED, record)
        return record.model_copy(deep=True)

    # ------------------------------------------------------------------ capacity

    async def reserve_slot(
        self, agent_name: str, token: str, limit: int, *, force: bool = False
    ) -> bool:
        slots = self._slots.setdefault(agent_name, {})
        in_use = len(self._by_agent.get(agent_name, {}))
        if not force and in_use + len(slots) >= limit:
            return False
        slots[token] = self._clock.monotonic()
        return True

    async def release_slot(self, agent_name: str, token: str) -> None:
        self._slots.get(agent_name, {}).pop(token, None)

    async def release_expired_slots(self, agent_name: str, older_than_seconds: float) -> int:
        slots = self._slots.get(agent_name, {})
        cutoff = self._clock.monotonic() - older_than_seconds
        expired = [token for token, at in slots.items() if at < cutoff]
        for token in expired:
            del slots[token]
        return len(expired)

    async def reserved(self, agent_name: str) -> int:
        return len(self._slots.get(agent_name, {}))
