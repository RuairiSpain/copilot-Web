"""Session selection strategies (PRD section 10.3)."""

from __future__ import annotations

from collections.abc import Sequence
from datetime import datetime

from hosted_agent_kit.domain.enums import SchedulerStrategy
from hosted_agent_kit.domain.models import SessionRecord


def order_key(record: SessionRecord) -> tuple[datetime, str]:
    """Stable registry order: creation time, then session id."""
    return (record.created_at, record.session_id)


def idle_since(record: SessionRecord) -> datetime:
    """Local last-release timestamp; a never-used session counts from its creation."""
    return record.last_released_at or record.created_at


class Scheduler:
    """Picks one session from a list of eligible AVAILABLE sessions.

    ``round_robin`` keeps a cursor per agent. Busy sessions are never passed in, so they
    cannot consume a turn.
    """

    def __init__(self) -> None:
        self._cursors: dict[str, tuple[datetime, str]] = {}

    def select(
        self, strategy: SchedulerStrategy, agent_name: str, candidates: Sequence[SessionRecord]
    ) -> SessionRecord | None:
        """Choose a session and, for round robin, move the cursor onto it."""
        chosen = self.peek(strategy, agent_name, candidates)
        if chosen is not None:
            self.advance(strategy, agent_name, chosen)
        return chosen

    def peek(
        self, strategy: SchedulerStrategy, agent_name: str, candidates: Sequence[SessionRecord]
    ) -> SessionRecord | None:
        """Choose a session without changing any state."""
        if not candidates:
            return None
        ordered = sorted(candidates, key=order_key)
        if strategy is SchedulerStrategy.FIRST_AVAILABLE:
            return ordered[0]
        if strategy is SchedulerStrategy.ROUND_ROBIN:
            cursor = self._cursors.get(agent_name)
            if cursor is None:
                return ordered[0]
            return next((r for r in ordered if order_key(r) > cursor), ordered[0])
        if strategy is SchedulerStrategy.OLDEST_IDLE:
            return min(ordered, key=lambda r: (idle_since(r), order_key(r)))
        newest = max(idle_since(r) for r in ordered)
        return next(r for r in ordered if idle_since(r) == newest)

    def advance(self, strategy: SchedulerStrategy, agent_name: str, used: SessionRecord) -> None:
        """Record that a session was used, so round robin moves on from it."""
        if strategy is SchedulerStrategy.ROUND_ROBIN:
            self._cursors[agent_name] = order_key(used)
