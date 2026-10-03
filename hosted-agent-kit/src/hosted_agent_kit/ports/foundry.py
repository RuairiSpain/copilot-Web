"""Port for the Foundry Hosted Agent platform. Only the adapter imports Azure SDK types."""

from __future__ import annotations

from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Any, Protocol

from hosted_agent_kit.domain.models import AgentSummary, FoundrySession, InvokeContext


@dataclass
class UpstreamResponse:
    """The result of an invocation.

    Exactly one of ``body``, ``raw`` and ``stream`` is set. ``body`` is a Responses result.
    ``raw`` is an Invocations response, returned as the agent sent it. ``stream`` yields bytes
    (server-sent-event frames for Responses). None of them carries the Foundry session id.
    """

    status_code: int = 200
    body: dict[str, Any] | None = None
    raw: bytes | None = None
    stream: AsyncIterator[bytes] | None = None
    media_type: str = "application/json"


class FoundryAdapter(Protocol):
    async def start(self) -> None: ...

    async def close(self) -> None: ...

    def list_agents(self) -> AsyncIterator[AgentSummary]: ...

    def list_sessions(self, agent_name: str) -> AsyncIterator[FoundrySession]: ...

    async def get_session(self, agent_name: str, session_id: str) -> FoundrySession: ...

    async def create_session(
        self, agent_name: str, session_id: str | None = None, agent_version: str | None = None
    ) -> FoundrySession:
        """Create a session. ``agent_version`` pins it; None uses the latest version."""
        ...

    async def latest_agent_version(self, agent_name: str) -> str | None: ...

    async def stop_session(self, agent_name: str, session_id: str) -> None: ...

    async def delete_session(self, agent_name: str, session_id: str) -> None: ...

    async def invoke(self, context: InvokeContext) -> UpstreamResponse: ...
