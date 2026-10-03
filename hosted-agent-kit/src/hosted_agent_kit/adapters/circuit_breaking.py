"""Decorates a Foundry adapter so every call goes through the agent's circuit breaker."""

from __future__ import annotations

import logging
from collections.abc import AsyncIterator, Awaitable, Callable
from typing import TypeVar

from hosted_agent_kit.domain.errors import FoundryError, FoundryTimeout, FoundryUnavailable
from hosted_agent_kit.domain.models import AgentSummary, FoundrySession, InvokeContext
from hosted_agent_kit.ports.foundry import FoundryAdapter, UpstreamResponse
from hosted_agent_kit.services.circuit_breaker import CircuitBreaker, CircuitBreakers

logger = logging.getLogger(__name__)
T = TypeVar("T")
# Only these say that Foundry itself is unhealthy. Any other answer, including a 404, a rejected
# request or a 429, proves that Foundry is responding.
_INFRASTRUCTURE_FAILURES = (FoundryUnavailable, FoundryTimeout)


class CircuitBreakingAdapter:
    def __init__(self, inner: FoundryAdapter, breakers: CircuitBreakers) -> None:
        self._inner = inner
        self._breakers = breakers

    async def start(self) -> None:
        await self._inner.start()

    async def close(self) -> None:
        await self._inner.close()

    def list_agents(self) -> AsyncIterator[AgentSummary]:
        return self._inner.list_agents()  # not tied to one agent, so not guarded

    async def _call(self, agent_name: str, operation: Callable[[], Awaitable[T]]) -> T:
        breaker = self._breakers.get(agent_name)
        if breaker is None:
            return await operation()
        probe = breaker.before_call()
        try:
            result = await operation()
        except _INFRASTRUCTURE_FAILURES:
            breaker.failure(probe)
            raise
        except FoundryError:
            breaker.success(probe)
            raise
        except BaseException:
            breaker.abandoned(probe)
            raise
        breaker.success(probe)
        return result

    async def list_sessions(self, agent_name: str) -> AsyncIterator[FoundrySession]:
        breaker = self._breakers.get(agent_name)
        probe = breaker.before_call() if breaker is not None else False
        settled = False
        try:
            async for session in self._inner.list_sessions(agent_name):
                yield session
        except _INFRASTRUCTURE_FAILURES:
            settled = True
            if breaker is not None:
                breaker.failure(probe)
            raise
        except FoundryError:
            settled = True
            if breaker is not None:
                breaker.success(probe)
            raise
        else:
            settled = True
            if breaker is not None:
                breaker.success(probe)
        finally:
            if not settled and breaker is not None:
                breaker.abandoned(probe)

    async def get_session(self, agent_name: str, session_id: str) -> FoundrySession:
        return await self._call(agent_name, lambda: self._inner.get_session(agent_name, session_id))

    async def create_session(
        self, agent_name: str, session_id: str | None = None, agent_version: str | None = None
    ) -> FoundrySession:
        return await self._call(
            agent_name, lambda: self._inner.create_session(agent_name, session_id, agent_version)
        )

    async def latest_agent_version(self, agent_name: str) -> str | None:
        return await self._call(agent_name, lambda: self._inner.latest_agent_version(agent_name))

    async def stop_session(self, agent_name: str, session_id: str) -> None:
        await self._call(agent_name, lambda: self._inner.stop_session(agent_name, session_id))

    async def delete_session(self, agent_name: str, session_id: str) -> None:
        await self._call(agent_name, lambda: self._inner.delete_session(agent_name, session_id))

    async def invoke(self, context: InvokeContext) -> UpstreamResponse:
        breaker = self._breakers.get(context.agent_name)
        if breaker is None:
            return await self._inner.invoke(context)
        probe = breaker.before_call()
        try:
            response = await self._inner.invoke(context)
        except _INFRASTRUCTURE_FAILURES:
            breaker.failure(probe)
            raise
        except FoundryError:
            breaker.success(probe)
            raise
        except BaseException:
            breaker.abandoned(probe)
            raise
        if response.stream is None:
            breaker.success(probe)
        else:
            # Opening the stream proves nothing yet. The probe is settled by how the stream ends.
            response.stream = _WatchedStream(breaker, probe, response.stream)
        return response


class _WatchedStream:
    """Settle the breaker, and a half-open probe, from the outcome of the whole stream."""

    def __init__(self, breaker: CircuitBreaker, probe: bool, stream: AsyncIterator[bytes]) -> None:
        self._breaker = breaker
        self._probe = probe
        self._stream = stream
        self._iterator = stream.__aiter__()
        self._settled = False

    def __aiter__(self) -> _WatchedStream:
        return self

    async def __anext__(self) -> bytes:
        try:
            return await self._iterator.__anext__()
        except StopAsyncIteration:
            self._settle(ok=True)
            raise
        except _INFRASTRUCTURE_FAILURES:
            self._settle(ok=False)
            raise
        except FoundryError:
            self._settle(ok=True)  # Foundry answered, so it is reachable
            raise

    async def aclose(self) -> None:
        # Closed before the stream finished, for example by a client that went away.
        if not self._settled:
            self._settled = True
            self._breaker.abandoned(self._probe)
        aclose = getattr(self._stream, "aclose", None)
        if aclose is not None:
            try:
                await aclose()
            except Exception:
                logger.warning("upstream_stream_close_failed", exc_info=True)

    def _settle(self, *, ok: bool) -> None:
        if self._settled:
            return
        self._settled = True
        if ok:
            self._breaker.success(self._probe)
        else:
            self._breaker.failure(self._probe)
