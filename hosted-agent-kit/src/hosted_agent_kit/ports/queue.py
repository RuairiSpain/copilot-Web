"""Port for per-agent bounded FIFO queues and the grants handed to waiters."""

from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
from datetime import datetime
from typing import Protocol

from hosted_agent_kit.domain.models import SessionAffinityKey


@dataclass(frozen=True)
class SessionGrant:
    """An existing session, already leased to the waiting request."""

    session_id: str


@dataclass(frozen=True)
class CreateGrant:
    """A reserved capacity slot. The holder must create a session or give the slot back."""

    token: str
    affinity_key: SessionAffinityKey | None


Grant = SessionGrant | CreateGrant


@dataclass
class QueueTicket:
    request_id: str
    agent_name: str
    user_id: str | None
    enqueued_at: datetime
    pinned: bool
    future: asyncio.Future[Grant] = field(repr=False)
    conversation_key: str | None = None


class QueueManager(Protocol):
    async def enqueue(self, ticket: QueueTicket, max_depth: int) -> None:
        """Append a ticket. Raises ``QueueFullError`` when the queue is at ``max_depth``."""
        ...

    async def pending(self, agent_name: str) -> list[QueueTicket]:
        """Return waiting tickets in FIFO order."""
        ...

    async def remove(self, agent_name: str, request_id: str) -> QueueTicket | None: ...

    async def depth(self, agent_name: str) -> int: ...
