"""A keyed work queue in the style of a Kubernetes controller queue.

- A key is queued at most once, however often it is added (deduplication).
- A key added while it is being processed is queued again once, when processing ends (dirty bit).
- Failures are retried with exponential backoff, and ``forget`` resets the count.
- ``add_after`` schedules a key for later. The earliest requested time wins.

Delayed keys become ready in two ways. When timers are enabled (a running controller manager) each
delay is a timer task on the injected clock. Otherwise ``release_due`` moves due keys into the
queue, which keeps tests and one-shot syncs deterministic.
"""

from __future__ import annotations

import asyncio
from collections import deque
from collections.abc import Hashable

from hosted_agent_kit.services.clock import Clock


class WorkQueue[K: Hashable]:
    def __init__(
        self,
        clock: Clock,
        *,
        base_delay: float = 0.5,
        max_delay: float = 60.0,
        max_retries: int | None = None,
    ) -> None:
        self._clock = clock
        self._base = base_delay
        self._max_delay = max_delay
        self._max_retries = max_retries
        self._queue: deque[K] = deque()
        self._queued: set[K] = set()
        self._processing: set[K] = set()
        self._dirty: set[K] = set()
        self._failures: dict[K, int] = {}
        self._delayed: dict[K, float] = {}
        self._timers: dict[K, asyncio.Task[None]] = {}
        self._timers_enabled = False
        self._shutdown = False
        self._available = asyncio.Event()
        self._idle = asyncio.Event()
        self._idle.set()

    # -------------------------------------------------------------------- adding

    def add(self, key: K) -> None:
        if self._shutdown:
            return
        if key in self._processing:
            self._dirty.add(key)  # queued again when the current run ends
            return
        if key in self._queued:
            return
        self._queued.add(key)
        self._queue.append(key)
        self._available.set()
        self._idle.clear()

    def add_after(self, key: K, delay: float) -> None:
        if delay <= 0:
            self.add(key)
            return
        ready_at = self._clock.monotonic() + delay
        current = self._delayed.get(key)
        if current is not None and current <= ready_at:
            return  # an earlier request wins
        self._delayed[key] = ready_at
        if self._timers_enabled and not self._shutdown:
            previous = self._timers.pop(key, None)
            if previous is not None:
                previous.cancel()
            self._timers[key] = asyncio.get_running_loop().create_task(self._timer(key, delay))

    def add_rate_limited(self, key: K) -> float | None:
        """Retry with exponential backoff. Returns the delay, or None when retries are used up."""
        failures = self._failures.get(key, 0) + 1
        if self._max_retries is not None and failures > self._max_retries:
            self._failures.pop(key, None)
            return None
        self._failures[key] = failures
        delay: float = min(self._max_delay, self._base * 2 ** (failures - 1))
        self.add_after(key, delay)
        return delay

    def forget(self, key: K) -> None:
        self._failures.pop(key, None)

    def num_requeues(self, key: K) -> int:
        return self._failures.get(key, 0)

    # ------------------------------------------------------------------ delayed

    async def _timer(self, key: K, delay: float) -> None:
        await self._clock.sleep(delay)
        self._timers.pop(key, None)
        self._delayed.pop(key, None)
        self.add(key)

    def enable_timers(self) -> None:
        """Start a timer for every delayed key. Used while a controller manager is running."""
        self._timers_enabled = True
        now = self._clock.monotonic()
        for key, ready_at in list(self._delayed.items()):
            if key not in self._timers:
                delay = max(0.0, ready_at - now)
                self._timers[key] = asyncio.get_running_loop().create_task(self._timer(key, delay))

    def release_due(self) -> int:
        """Queue every delayed key whose time has come. Returns how many were queued."""
        now = self._clock.monotonic()
        due = [k for k, at in self._delayed.items() if at <= now]
        for key in due:
            del self._delayed[key]
            timer = self._timers.pop(key, None)
            if timer is not None:
                timer.cancel()
            self.add(key)
        return len(due)

    def delayed_keys(self) -> list[K]:
        return list(self._delayed)

    # ---------------------------------------------------------------- consuming

    async def get(self) -> K | None:
        """Wait for the next key. Returns None once the queue is shut down and empty."""
        while not self._queue:
            if self._shutdown:
                return None
            self._available.clear()
            await self._available.wait()
        key = self._queue.popleft()
        self._queued.discard(key)
        self._processing.add(key)
        return key

    def get_nowait(self) -> K | None:
        if not self._queue:
            return None
        key = self._queue.popleft()
        self._queued.discard(key)
        self._processing.add(key)
        return key

    def done(self, key: K) -> None:
        self._processing.discard(key)
        if key in self._dirty:
            self._dirty.discard(key)
            self.add(key)
        if not self._queue and not self._processing:
            self._idle.set()

    # ------------------------------------------------------------------- status

    def __len__(self) -> int:
        return len(self._queue)

    @property
    def idle(self) -> bool:
        """Nothing is queued or being processed. Delayed keys do not count."""
        return not self._queue and not self._processing

    async def wait_idle(self) -> None:
        await self._idle.wait()

    def reopen(self) -> None:
        """Accept work again after a shutdown, so a stopped manager can be started again."""
        self._shutdown = False
        self._timers_enabled = False

    def shutdown(self) -> None:
        self._shutdown = True
        for timer in self._timers.values():
            timer.cancel()
        self._timers.clear()
        self._available.set()
