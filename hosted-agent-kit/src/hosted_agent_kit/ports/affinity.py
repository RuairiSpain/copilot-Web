"""Port for the stateful affinity map: (user_id, agent_name) -> session."""

from __future__ import annotations

from typing import Protocol

from hosted_agent_kit.domain.models import AffinityEntry, SessionAffinityKey


class AffinityStore(Protocol):
    async def get(self, key: SessionAffinityKey) -> AffinityEntry | None: ...

    async def reserve(self, key: SessionAffinityKey, request_id: str) -> bool:
        """Claim a key for session creation. Fails when the key is bound or already claimed."""
        ...

    async def bind(self, key: SessionAffinityKey, session_id: str) -> None:
        """Bind a key to a session, replacing any pending claim."""
        ...

    async def cancel_reservation(self, key: SessionAffinityKey, request_id: str) -> bool: ...

    async def remove(self, key: SessionAffinityKey) -> None: ...

    async def remove_by_session(
        self, agent_name: str, session_id: str
    ) -> list[SessionAffinityKey]: ...

    async def count(self, agent_name: str) -> int: ...

    async def entries(self, agent_name: str) -> list[tuple[SessionAffinityKey, AffinityEntry]]:
        """Every entry for the agent, for garbage collection."""
        ...
