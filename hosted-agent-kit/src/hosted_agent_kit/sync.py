"""A blocking wrapper for code that is not asynchronous (scripts, Flask, Django, notebooks).

``HackSync`` runs the kit on its own event loop in a background thread and blocks the calling
thread for each call::

    with HackSync(Hack.from_yaml("scheduler.yaml")) as kit:
        print(kit.ask("support-bot", "hello", user_id="u1").output_text)
        for event in kit.stream("support-bot", user_id="u1", input={"input": "hello"}):
            print(event.event, event.data)

Calls may come from several threads at once: they share the kit's pool like concurrent tasks.
"""

from __future__ import annotations

import asyncio
import threading
from collections.abc import Coroutine, Iterator, Mapping
from concurrent.futures import Future
from types import TracebackType
from typing import Any, TypeVar

from hosted_agent_kit.kit import Hack
from hosted_agent_kit.results import AgentResult
from hosted_agent_kit.sse import SseEvent

T = TypeVar("T")


class HackSync:
    def __init__(self, kit: Hack) -> None:
        self.kit = kit
        self._loop: asyncio.AbstractEventLoop | None = None
        self._thread: threading.Thread | None = None

    # ----------------------------------------------------------------- lifecycle

    def start(self) -> None:
        if self._loop is not None:
            return
        ready = threading.Event()
        loop = asyncio.new_event_loop()

        def run() -> None:
            asyncio.set_event_loop(loop)
            ready.set()
            loop.run_forever()
            loop.close()

        thread = threading.Thread(target=run, name="hosted-agent-kit", daemon=True)
        thread.start()
        ready.wait()
        self._loop, self._thread = loop, thread
        try:
            self.run(self.kit.start())
        except BaseException:
            self._shutdown_loop()
            raise

    def stop(self) -> None:
        if self._loop is None:
            return
        try:
            self.run(self.kit.stop())
        finally:
            self._shutdown_loop()

    def _shutdown_loop(self) -> None:
        loop, thread = self._loop, self._thread
        self._loop = self._thread = None
        if loop is not None:
            loop.call_soon_threadsafe(loop.stop)
        if thread is not None:
            thread.join(timeout=10)

    def __enter__(self) -> HackSync:
        self.start()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        self.stop()

    def run(self, coroutine: Coroutine[Any, Any, T]) -> T:
        """Run any kit coroutine (for example ``kit.reporting.agents()``) and wait for it."""
        loop = self._loop
        if loop is None:
            coroutine.close()
            raise RuntimeError("HackSync is not started: use `with HackSync(kit):` or start()")
        future: Future[T] = asyncio.run_coroutine_threadsafe(coroutine, loop)
        return future.result()

    # --------------------------------------------------------------------- calls

    def ask(self, agent: str, message: str, *, user_id: str, **options: Any) -> AgentResult:
        return self.run(self.kit.ask(agent, message, user_id=user_id, **options))

    def responses(
        self,
        agent: str,
        *,
        user_id: str,
        input: Mapping[str, Any],
        **options: Any,
    ) -> AgentResult:
        """A complete (not streamed) answer. For a stream use ``stream``."""
        return self.run(self.kit.responses(agent, user_id=user_id, input=input, **options))

    def invocations(self, agent: str, *, user_id: str, **options: Any) -> AgentResult:
        return self.run(self.kit.invocations(agent, user_id=user_id, **options))

    def stream(
        self,
        agent: str,
        *,
        user_id: str,
        input: Mapping[str, Any],
        **options: Any,
    ) -> Iterator[SseEvent]:
        """The events of a streamed Responses answer. Stop early and the session is released."""
        result = self.run(
            self.kit.responses(agent, user_id=user_id, input=input, stream=True, **options)
        )
        events = result.events()

        async def advance() -> SseEvent:
            return await events.__anext__()

        try:
            while True:
                try:
                    yield self.run(advance())
                except StopAsyncIteration:
                    return
        finally:
            self.run(result.aclose())
