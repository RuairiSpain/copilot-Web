"""Lifecycle events, kept briefly so an operator can see what the controllers did and why.

Events are not state: nothing reads them to decide anything. Repeated identical events are
collapsed into one with a count, as Kubernetes does.
"""

from __future__ import annotations

import logging
from collections import deque
from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum

from hosted_agent_kit.logging_config import hash_identifier, log_event
from hosted_agent_kit.services.clock import Clock

logger = logging.getLogger(__name__)


class EventType(StrEnum):
    NORMAL = "Normal"
    WARNING = "Warning"


@dataclass
class PoolEvent:
    agent_name: str
    object: str  # "pool" or "session/<hashed id>"
    type: EventType
    reason: str
    message: str
    first_seen: datetime
    last_seen: datetime
    count: int = 1


class EventRecorder:
    def __init__(self, clock: Clock, *, capacity: int = 500) -> None:
        self._clock = clock
        self._events: deque[PoolEvent] = deque(maxlen=capacity)

    def record(
        self,
        agent_name: str,
        obj: str,
        type_: EventType,
        reason: str,
        message: str,
    ) -> None:
        now = self._clock.now()
        for event in reversed(self._events):
            if (
                event.agent_name == agent_name
                and event.object == obj
                and event.reason == reason
                and event.message == message
                and event.type is type_
            ):
                event.count += 1
                event.last_seen = now
                return
        self._events.append(PoolEvent(agent_name, obj, type_, reason, message, now, now))
        log_event(
            logger,
            "pool_event",
            level=logging.WARNING if type_ is EventType.WARNING else logging.INFO,
            agent_name=agent_name,
            object=obj,
            reason=reason,
        )

    def pool(self, agent_name: str, type_: EventType, reason: str, message: str) -> None:
        self.record(agent_name, "pool", type_, reason, message)

    def session(
        self, agent_name: str, session_id: str, type_: EventType, reason: str, message: str
    ) -> None:
        self.record(agent_name, f"session/{hash_identifier(session_id)}", type_, reason, message)

    def list(self, agent_name: str | None = None, limit: int = 100) -> list[PoolEvent]:
        items = [e for e in self._events if agent_name is None or e.agent_name == agent_name]
        return list(reversed(items))[:limit]
