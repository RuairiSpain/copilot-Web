"""Test doubles for code that uses the kit: an in-memory Foundry and a controllable clock.

Use them to test your own FastAPI endpoints without Azure::

    kit = Hack.from_yaml("scheduler.yaml", adapter=FakeFoundry())

``FakeFoundry`` records every call and can be scripted to fail or stall. ``DemoFoundry`` answers
like a small agent, which is what the bundled samples use in demo mode.
"""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime, timedelta

from hosted_agent_kit.domain.enums import AgentProtocol, FoundrySessionStatus
from hosted_agent_kit.domain.errors import (
    FoundryConflict,
    FoundryThrottled,
    FoundryTimeout,
    FoundryUnavailable,
)
from hosted_agent_kit.domain.models import AgentSummary, FoundrySession, InvokeContext
from hosted_agent_kit.ports.foundry import UpstreamResponse

__all__ = [
    "DemoFoundry",
    "FakeClock",
    "FakeFoundry",
    "FoundryThrottled",
    "FoundryTimeout",
    "FoundryUnavailable",
    "UpstreamResponse",
]


class FakeClock:
    def __init__(self) -> None:
        self._now = datetime(2026, 10, 2, 12, 0, 0, tzinfo=UTC)
        self._mono = 1000.0
        self.sleeps: list[float] = []

    def now(self) -> datetime:
        return self._now

    def monotonic(self) -> float:
        return self._mono

    def advance(self, seconds: float) -> None:
        self._now += timedelta(seconds=seconds)
        self._mono += seconds

    async def sleep(self, seconds: float) -> None:
        self.sleeps.append(seconds)
        self.advance(seconds)


class FakeFoundry:
    def __init__(self, clock: FakeClock | None = None) -> None:
        self.clock = clock or FakeClock()
        self.sessions: dict[str, FoundrySession] = {}
        self._counter = 0
        self.started = False
        self.closed = False
        # recorded calls
        self.created: list[str] = []
        self.create_versions: list[str | None] = []
        self.latest_version: str | None = "1"
        self.latest_version_error: Exception | None = None
        self.create_requests: list[str | None] = []
        self.deleted: list[str] = []
        self.stopped: list[str] = []
        self.invocations: list[InvokeContext] = []
        self.get_calls: list[str] = []
        # scripted behaviour
        self.create_errors: list[Exception] = []
        self.create_status = FoundrySessionStatus.ACTIVE
        self.create_progress: list[FoundrySessionStatus] = []
        self.get_errors: dict[str, Exception] = {}
        self.delete_errors: list[Exception] = []
        self.invoke_errors: list[Exception] = []
        self.invoke_gate: asyncio.Event | None = None
        self.invoke_started = asyncio.Event()
        self.invoke_handler: Callable[[InvokeContext], UpstreamResponse] | None = None
        self.stream_frames: list[bytes] = [b"event: a\ndata: 1\n\n", b"event: b\ndata: 2\n\n"]
        self.stream_error: Exception | None = None
        self.stream_stall = False
        self.list_fail_after: int | None = None
        self.list_error: Exception | None = None
        self.agents: list[AgentSummary] = []
        self.list_agents_error: Exception | None = None

    async def start(self) -> None:
        self.started = True

    async def close(self) -> None:
        self.closed = True

    def add_session(
        self,
        agent: str,
        status: FoundrySessionStatus = FoundrySessionStatus.ACTIVE,
        session_id: str | None = None,
    ) -> FoundrySession:
        self._counter += 1
        sid = session_id or f"sess-{self._counter:04d}"
        session = FoundrySession(
            session_id=sid,
            agent_name=agent,
            agent_version="1",
            status=status,
            created_at=self.clock.now() + timedelta(seconds=self._counter),
        )
        self.sessions[sid] = session
        return session

    def set_status(self, session_id: str, status: FoundrySessionStatus) -> None:
        self.sessions[session_id] = self.sessions[session_id].model_copy(update={"status": status})

    async def list_agents(self) -> AsyncIterator[AgentSummary]:
        if self.list_agents_error is not None:
            raise self.list_agents_error
        for agent in self.agents:
            yield agent

    async def list_sessions(self, agent_name: str) -> AsyncIterator[FoundrySession]:
        items = [s for s in self.sessions.values() if s.agent_name == agent_name]
        for index, session in enumerate(items):
            if self.list_fail_after is not None and index >= self.list_fail_after:
                raise self.list_error or RuntimeError("list failed")
            yield session
        if self.list_fail_after is not None and self.list_fail_after >= len(items):
            raise self.list_error or RuntimeError("list failed")

    async def get_session(self, agent_name: str, session_id: str) -> FoundrySession:
        self.get_calls.append(session_id)
        if session_id in self.get_errors:
            raise self.get_errors[session_id]
        session = self.sessions.get(session_id)
        if session is None or session.agent_name != agent_name:
            from hosted_agent_kit.domain.errors import FoundrySessionNotFound

            raise FoundrySessionNotFound()
        if self.create_progress and session.status is FoundrySessionStatus.CREATING:
            session = session.model_copy(update={"status": self.create_progress.pop(0)})
            self.sessions[session_id] = session
        return session

    async def latest_agent_version(self, agent_name: str) -> str | None:
        if self.latest_version_error is not None:
            raise self.latest_version_error
        return self.latest_version

    async def create_session(
        self, agent_name: str, session_id: str | None = None, agent_version: str | None = None
    ) -> FoundrySession:
        self.create_requests.append(session_id)
        self.create_versions.append(agent_version)
        if self.create_errors:
            raise self.create_errors.pop(0)
        if session_id is not None and session_id in self.sessions:
            raise FoundryConflict()
        session = self.add_session(agent_name, self.create_status, session_id)
        session = session.model_copy(update={"agent_version": agent_version or self.latest_version})
        self.sessions[session.session_id] = session
        self.created.append(session.session_id)
        return session

    async def stop_session(self, agent_name: str, session_id: str) -> None:
        self.stopped.append(session_id)

    async def delete_session(self, agent_name: str, session_id: str) -> None:
        if self.delete_errors:
            raise self.delete_errors.pop(0)
        self.deleted.append(session_id)
        self.sessions.pop(session_id, None)

    async def invoke(self, context: InvokeContext) -> UpstreamResponse:
        self.invocations.append(context)
        self.invoke_started.set()
        if self.invoke_gate is not None:
            await self.invoke_gate.wait()
        if self.invoke_errors:
            raise self.invoke_errors.pop(0)
        if self.invoke_handler is not None:
            return self.invoke_handler(context)
        if context.stream:
            return UpstreamResponse(stream=self._stream(), media_type="text/event-stream")
        return UpstreamResponse(body={"status": "completed", "echo": context.payload})

    async def _stream(self) -> AsyncIterator[bytes]:
        for frame in self.stream_frames:
            yield frame
        if self.stream_error is not None:
            raise self.stream_error
        if self.stream_stall:
            await asyncio.sleep(3600)


class DemoFoundry(FakeFoundry):
    """A fake that behaves like a small agent, so samples run without Azure.

    A Responses agent answers ``Echo from <agent>: <text>`` and counts the messages each
    session has seen (so a stateful agent visibly remembers). An Invocations agent returns the
    request body with the same content type. Streaming sends the answer word by word.
    """

    def __init__(self, clock: FakeClock | None = None) -> None:
        super().__init__(clock)
        self.turns: dict[str, int] = {}
        self.invoke_handler = self._answer

    def _answer(self, context: InvokeContext) -> UpstreamResponse:
        turn = self.turns[context.session_id] = self.turns.get(context.session_id, 0) + 1
        if context.protocol is AgentProtocol.INVOCATIONS:
            body = context.raw_body or b""
            return UpstreamResponse(
                raw=body, media_type=context.content_type or "application/octet-stream"
            )
        payload = context.payload if isinstance(context.payload, dict) else {}
        text = f"Echo from {context.agent_name}: {payload.get('input', '')}"
        if context.stream:
            return UpstreamResponse(stream=self._words(text), media_type="text/event-stream")
        return UpstreamResponse(
            body={
                "id": f"resp_{context.request_id[:8]}",
                "status": "completed",
                "output_text": text,
                "turn_in_session": turn,
            }
        )

    async def _words(self, text: str) -> AsyncIterator[bytes]:
        for word in text.split():
            data = json.dumps({"delta": word + " "})
            yield f"event: response.output_text.delta\ndata: {data}\n\n".encode()
        yield b'event: response.completed\ndata: {"status": "completed"}\n\n'
