"""In-memory affinity store (Version 1)."""

from __future__ import annotations

from hosted_agent_kit.domain.models import AffinityEntry, SessionAffinityKey


class MemoryAffinityStore:
    def __init__(self) -> None:
        self._entries: dict[SessionAffinityKey, AffinityEntry] = {}

    async def get(self, key: SessionAffinityKey) -> AffinityEntry | None:
        return self._entries.get(key)

    async def reserve(self, key: SessionAffinityKey, request_id: str) -> bool:
        if key in self._entries:
            return False
        self._entries[key] = AffinityEntry(pending_request_id=request_id)
        return True

    async def bind(self, key: SessionAffinityKey, session_id: str) -> None:
        self._entries[key] = AffinityEntry(session_id=session_id)

    async def cancel_reservation(self, key: SessionAffinityKey, request_id: str) -> bool:
        entry = self._entries.get(key)
        if entry is None or entry.pending_request_id != request_id:
            return False
        del self._entries[key]
        return True

    async def remove(self, key: SessionAffinityKey) -> None:
        self._entries.pop(key, None)

    async def remove_by_session(self, agent_name: str, session_id: str) -> list[SessionAffinityKey]:
        keys = [
            k
            for k, e in self._entries.items()
            if k.agent_name == agent_name and e.session_id == session_id
        ]
        for key in keys:
            del self._entries[key]
        return keys

    async def entries(self, agent_name: str) -> list[tuple[SessionAffinityKey, AffinityEntry]]:
        return [(k, e) for k, e in self._entries.items() if k.agent_name == agent_name]

    async def count(self, agent_name: str) -> int:
        return sum(
            1
            for key, entry in self._entries.items()
            if key.agent_name == agent_name and entry.session_id
        )
