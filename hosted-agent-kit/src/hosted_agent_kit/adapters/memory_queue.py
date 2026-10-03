"""In-memory bounded FIFO queues, one per agent (Version 1)."""

from __future__ import annotations

from hosted_agent_kit.domain.errors import QueueFullError
from hosted_agent_kit.ports.queue import QueueTicket


class MemoryQueueManager:
    def __init__(self) -> None:
        self._queues: dict[str, list[QueueTicket]] = {}

    async def enqueue(self, ticket: QueueTicket, max_depth: int) -> None:
        queue = self._queues.setdefault(ticket.agent_name, [])
        if len(queue) >= max_depth:
            raise QueueFullError("The configured queue capacity has been reached.")
        queue.append(ticket)

    async def pending(self, agent_name: str) -> list[QueueTicket]:
        return list(self._queues.get(agent_name, []))

    async def remove(self, agent_name: str, request_id: str) -> QueueTicket | None:
        queue = self._queues.get(agent_name, [])
        for index, ticket in enumerate(queue):
            if ticket.request_id == request_id:
                return queue.pop(index)
        return None

    async def depth(self, agent_name: str) -> int:
        return len(self._queues.get(agent_name, []))
