"""Synchronisation of the pools with Foundry, run by the controllers.

``Reconciler`` is the entry point that the service, the admin API and the tests use. A sync of one
agent runs the controllers in a fixed order, each as "queue the pool key, then let the controller
manager work through the queues":

1. observation: list Foundry, bring the store in line, mark what must go for deletion;
2. the session controller deletes what is marked (queued by the store changes);
3. garbage collection: retries deletions that failed, expires stuck work;
4. the pool controller: drains old versions and tops up warm sessions.

The same controllers also react to store changes between syncs when the manager is running.
"""

from __future__ import annotations

import asyncio
import logging

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.controllers.gc import GarbageCollectionController
from hosted_agent_kit.controllers.health import DependencyHealthController
from hosted_agent_kit.controllers.observation import ObservationCache, ObservationController
from hosted_agent_kit.controllers.pool import PoolController
from hosted_agent_kit.controllers.reports import ReconcileReport
from hosted_agent_kit.controllers.status import PoolStatusController
from hosted_agent_kit.domain.errors import SyncInProgressError
from hosted_agent_kit.domain.resources import ResourceKey, pool_key
from hosted_agent_kit.logging_config import log_event
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.ports.metrics import MetricsRecorder
from hosted_agent_kit.ports.registry import SessionRegistry
from hosted_agent_kit.services.circuit_breaker import CircuitBreakers
from hosted_agent_kit.services.clock import Clock
from hosted_agent_kit.services.pool import PoolService
from hosted_agent_kit.services.session_ids import SessionIdDeriver

logger = logging.getLogger(__name__)

__all__ = ["ReconcileReport", "Reconciler"]


class Reconciler:
    def __init__(
        self,
        *,
        config: ConfigSource,
        adapter: FoundryAdapter,
        registry: SessionRegistry,
        pool: PoolService,
        metrics: MetricsRecorder,
        clock: Clock,
        session_ids: SessionIdDeriver | None = None,
        circuits: CircuitBreakers | None = None,
    ) -> None:
        self._session_ids = session_ids
        self._synced: set[str] = set()
        self._config = config
        self._adapter = adapter
        self._registry = registry
        self._pool = pool
        self._metrics = metrics
        self._clock = clock
        self._mutexes = {name: asyncio.Lock() for name in config.names}
        self._stop = asyncio.Event()
        self._tasks: list[asyncio.Task[None]] = []

        self.plane = pool.plane
        self.manager = self.plane.manager
        self.cache = ObservationCache()
        self.observation = ObservationController(
            config=config,
            adapter=adapter,
            registry=registry,
            plane=self.plane,
            metrics=metrics,
            clock=clock,
            cache=self.cache,
            session_ids=session_ids,
        )
        self.pool_controller = PoolController(
            config=config,
            adapter=adapter,
            registry=registry,
            pool=pool,
            plane=self.plane,
            cache=self.cache,
            session_ids=session_ids,
        )
        self.gc = GarbageCollectionController(
            config=config,
            registry=registry,
            affinity=pool.affinity,
            plane=self.plane,
            clock=clock,
            settings=pool.settings,
        )
        self.health = DependencyHealthController(
            config=config, plane=self.plane, cache=self.cache, circuits=circuits
        )
        self.status = PoolStatusController(
            config=config,
            registry=registry,
            queue=pool.queue,
            plane=self.plane,
            cache=self.cache,
            recovery_enabled=session_ids is not None,
        )
        for controller in (
            self.observation,
            self.health,
            self.gc,
            self.pool_controller,
            self.status,
        ):
            self.manager.register(controller)
        self.manager.on_error(self._on_controller_error)

    def _on_controller_error(self, controller: str, key: ResourceKey, exc: Exception) -> None:
        """An unexpected error in a controller during a sync is reported on that sync."""
        report = self.plane.report(key.agent_name)
        if report is not None:
            report.outcome = "error"
            report.errors.append(type(exc).__name__)

    @property
    def initial_sync_done(self) -> bool:
        """True once every agent has had one reconciliation attempt, whatever its outcome."""
        return self._synced >= set(self._config.names)

    @property
    def workers_started(self) -> bool:
        return bool(self._tasks)

    async def start(self) -> None:
        """Start the controller workers and one resync loop per agent (each syncs at once)."""
        self._stop.clear()
        await self.manager.start()
        self._tasks = [
            asyncio.create_task(self._loop(name), name=f"reconciler:{name}")
            for name in self._config.names
        ]

    async def stop(self, grace_seconds: float = 5.0) -> None:
        """Signal the loops to finish and stop the controllers; cancel any that overrun."""
        self._stop.set()
        if self._tasks:
            _, pending = await asyncio.wait(self._tasks, timeout=grace_seconds)
            for task in pending:
                task.cancel()
            await asyncio.gather(*self._tasks, return_exceptions=True)
        self._tasks = []
        await self.manager.stop(grace_seconds)

    async def _loop(self, agent_name: str) -> None:
        while not self._stop.is_set():
            try:
                await self.run_agent(agent_name)
            except Exception as exc:  # keep the loop alive whatever happens in one pass
                log_event(
                    logger,
                    "reconcile_worker_error",
                    level=logging.ERROR,
                    agent_name=agent_name,
                    error_type=type(exc).__name__,
                )
            current = self._config.get(agent_name)
            interval = current.sync_interval_seconds if current else 60
            try:
                # Event-driven sleep: wakes immediately on shutdown, otherwise after the interval.
                await asyncio.wait_for(self._stop.wait(), timeout=interval)
            except TimeoutError:
                continue

    async def run_agent(self, agent_name: str, *, manual: bool = False) -> ReconcileReport:
        self._pool.agent_config(agent_name)
        mutex = self._mutexes[agent_name]
        if mutex.locked():
            if manual:
                raise SyncInProgressError(f"A reconciliation for '{agent_name}' is running.")
            return ReconcileReport(agent_name=agent_name, outcome="skipped", skipped=True)
        async with mutex:
            started = self._clock.monotonic()
            report = ReconcileReport(agent_name=agent_name)
            self.plane.begin_sync(report)
            try:
                await self._sync(agent_name)
            except Exception as exc:  # a loop must survive any single failed pass
                report.outcome = "error"
                report.errors.append(type(exc).__name__)
                log_event(
                    logger,
                    "reconcile_error",
                    level=logging.ERROR,
                    agent_name=agent_name,
                    error_type=type(exc).__name__,
                )
            finally:
                self.plane.end_sync(agent_name)
            report.duration_seconds = self._clock.monotonic() - started
            self._synced.add(agent_name)
            self._metrics.reconcile(agent_name, report.outcome, report.duration_seconds)
            log_event(
                logger,
                "reconcile_completed",
                agent_name=agent_name,
                outcome=report.outcome,
                discovered=report.discovered,
                removed=report.removed,
                failed_sessions=report.failed_sessions,
                uncertain=report.uncertain,
                warm_created=report.warm_created,
                drained=report.drained,
                duration_seconds=round(report.duration_seconds, 3),
            )
        await self._pool.notify(agent_name)
        return report

    async def _sync(self, agent_name: str) -> None:
        key = pool_key(agent_name)
        for controller in ("observation", "health", "gc", "pool", "status"):
            self.manager.enqueue(controller, key)
            await self.manager.drain()

    async def run_all(self) -> list[ReconcileReport]:
        return [await self.run_agent(name) for name in self._config.names]
