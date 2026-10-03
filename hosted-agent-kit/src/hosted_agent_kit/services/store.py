"""Helpers for read-modify-write updates with optimistic concurrency."""

from __future__ import annotations

from collections.abc import Callable

from hosted_agent_kit.domain.models import SessionRecord
from hosted_agent_kit.ports.registry import ResourceConflictError, SessionRegistry


async def mutate_session(
    registry: SessionRegistry,
    agent_name: str,
    session_id: str,
    change: Callable[[SessionRecord], None],
    *,
    attempts: int = 5,
) -> SessionRecord | None:
    """Apply ``change`` to a stored session. Retries when another writer got there first.

    Returns the stored record, or None if the session no longer exists.
    """
    for _ in range(attempts):
        record = await registry.get(agent_name, session_id)
        if record is None:
            return None
        version = record.resource_version
        change(record)
        try:
            return await registry.update(record, expected_version=version)
        except ResourceConflictError:
            continue
    raise ResourceConflictError(f"{agent_name}/{session_id} kept changing while it was updated")
