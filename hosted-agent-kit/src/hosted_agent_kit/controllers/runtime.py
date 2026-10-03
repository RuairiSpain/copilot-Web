"""The controller runtime: registers controllers, feeds their queues and runs their workers.

A controller reconciles one resource key at a time and must be idempotent: it looks at the current
state, does what moves it toward the desired state, and returns. The runtime owns retries, backoff,
deduplication, per-key mutual exclusion and shutdown, so a controller contains only its own logic.
"""

from __future__ import annotations

import asyncio
import logging
import weakref
from collections.abc import Callable
from dataclasses import dataclass
from types import TracebackType
from typing import Protocol

from hosted_agent_kit.controllers.workqueue import WorkQueue
from hosted_agent_kit.domain.resources import ResourceKey
from hosted_agent_kit.logging_config import log_event
from hosted_agent_kit.ports.registry import ChangeEvent, SessionRegistry
from hosted_agent_kit.services.clock import Clock

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class ReconcileResult:
    """What to do after a reconcile. By default nothing: the key is done until something changes."""

    requeue: bool = False  # try again with backoff
    requeue_after: float | None = None  # try again after this many seconds

    @classmethod
    def done(cls) -> ReconcileResult:
        return cls()

    @classmethod
    def retry(cls) -> ReconcileResult:
        return cls(requeue=True)

    @classmethod
    def after(cls, seconds: float) -> ReconcileResult:
        return cls(requeue_after=seconds)


class Controller(Protocol):
    name: str
    workers: int

    async def reconcile(self, key: ResourceKey) -> ReconcileResult: ...

    def map_event(self, event: ChangeEvent) -> list[ResourceKey]:
        """The keys a store change concerns. An empty list means this controller ignores it."""
        ...


class ControllerManager:
    """Runs controllers in this process, sharing one clock, store watch and shutdown."""

    def __init__(
        self,
        clock: Clock,
        registry: SessionRegistry,
        *,
        max_retries: int = 5,
        base_delay: float = 0.5,
        max_delay: float = 60.0,
    ) -> None:
        self._clock = clock
        self._max_retries = max_retries
        self._base_delay = base_delay
        self._max_delay = max_delay
        self._controllers: dict[str, Controller] = {}
        self._queues: dict[str, WorkQueue[ResourceKey]] = {}
        self._workers: list[asyncio.Task[None]] = []
        self._locks: dict[str, weakref.WeakValueDictionary[ResourceKey, asyncio.Lock]] = {}
        self._running = False
        self._error_hooks: list[Callable[[str, ResourceKey, Exception], None]] = []
        registry.subscribe(self._on_change)

    # --------------------------------------------------------------- registration

    def register(self, controller: Controller) -> None:
        self._controllers[controller.name] = controller
        self._queues[controller.name] = WorkQueue(
            self._clock,
            base_delay=self._base_delay,
            max_delay=self._max_delay,
            max_retries=self._max_retries,
        )
        self._locks[controller.name] = weakref.WeakValueDictionary()

    def on_error(self, hook: Callable[[str, ResourceKey, Exception], None]) -> None:
        self._error_hooks.append(hook)

    def queue(self, controller: str) -> WorkQueue[ResourceKey]:
        return self._queues[controller]

    # --------------------------------------------------------------------- input

    def enqueue(self, controller: str, key: ResourceKey) -> None:
        self._queues[controller].add(key)

    def enqueue_after(self, controller: str, key: ResourceKey, delay: float) -> None:
        self._queues[controller].add_after(key, delay)

    def _on_change(self, event: ChangeEvent) -> None:
        for name, controller in self._controllers.items():
            for key in controller.map_event(event):
                self._queues[name].add(key)

    # ------------------------------------------------------------------ reconcile

    def _lock(self, controller: str, key: ResourceKey) -> asyncio.Lock:
        locks = self._locks[controller]
        lock = locks.get(key)
        if lock is None:
            lock = asyncio.Lock()
            locks[key] = lock
        return lock

    async def reconcile_now(self, controller: str, key: ResourceKey) -> ReconcileResult:
        """Reconcile a key in the caller's task, excluding any worker that holds the same key.

        Used where a caller needs the outcome, for example an admin delete. Follow-up work the
        result asks for is queued as usual.
        """
        async with self._lock(controller, key):
            result = await self._controllers[controller].reconcile(key)
        self._apply(controller, key, result)
        return result

    def _apply(self, controller: str, key: ResourceKey, result: ReconcileResult) -> None:
        queue = self._queues[controller]
        if result.requeue_after is not None:
            queue.forget(key)
            queue.add_after(key, result.requeue_after)
        elif result.requeue:
            if queue.add_rate_limited(key) is None:
                log_event(
                    logger,
                    "controller_gave_up",
                    level=logging.WARNING,
                    controller=controller,
                    key=str(key),
                )
        else:
            queue.forget(key)

    async def _process(self, controller: str, key: ResourceKey) -> None:
        try:
            async with self._lock(controller, key):
                result = await self._controllers[controller].reconcile(key)
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            log_event(
                logger,
                "controller_error",
                level=logging.ERROR,
                controller=controller,
                key=str(key),
                error_type=type(exc).__name__,
            )
            for hook in self._error_hooks:
                hook(controller, key, exc)
            self._apply(controller, key, ReconcileResult.retry())
        else:
            self._apply(controller, key, result)

    # -------------------------------------------------------------------- running

    async def _worker(self, controller: str) -> None:
        queue = self._queues[controller]
        while (key := await queue.get()) is not None:
            try:
                await self._process(controller, key)
            finally:
                queue.done(key)

    async def start(self) -> None:
        if self._running:
            return
        self._running = True
        for name, controller in self._controllers.items():
            queue = self._queues[name]
            queue.reopen()
            queue.enable_timers()
            for _ in range(controller.workers):
                self._workers.append(asyncio.create_task(self._worker(name)))

    async def stop(self, grace_seconds: float = 5.0) -> None:
        """Stop taking work, let running reconciles finish, cancel any that overrun."""
        for queue in self._queues.values():
            queue.shutdown()
        if self._workers:
            _, pending = await asyncio.wait(self._workers, timeout=grace_seconds)
            for task in pending:
                task.cancel()
            await asyncio.gather(*self._workers, return_exceptions=True)
        self._workers = []
        self._running = False

    @property
    def running(self) -> bool:
        return self._running

    # ----------------------------------------------------------------- draining

    async def drain(self) -> None:
        """Process queued keys until every queue is idle.

        With workers running this waits for them. Otherwise it reconciles inline, one key at a
        time, which is what a one-shot sync and the tests use. Keys delayed to a later time are
        released only if their time has come; they are not waited for.
        """
        for queue in self._queues.values():
            queue.release_due()
        if self._running:
            for queue in self._queues.values():
                await queue.wait_idle()
            return
        progressed = True
        while progressed:
            progressed = False
            for name, queue in self._queues.items():
                while (key := queue.get_nowait()) is not None:
                    progressed = True
                    try:
                        await self._process(name, key)
                    finally:
                        queue.done(key)

    async def __aenter__(self) -> ControllerManager:
        await self.start()
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.stop()
