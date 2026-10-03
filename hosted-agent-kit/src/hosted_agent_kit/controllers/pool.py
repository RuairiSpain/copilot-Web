"""The pool controller: keeps each agent's pool at its desired size and version.

It creates warm sessions up to ``min_warm_sessions`` (never beyond ``max_sessions``) and, when the
agent asks for it, retires idle sessions that run another agent version. It acts only on a complete
observation, so an unreachable or partly listed Foundry never makes it create or delete anything.
"""

from __future__ import annotations

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.config.models import AgentConfig
from hosted_agent_kit.controllers.observation import ObservationCache
from hosted_agent_kit.controllers.plane import ControlPlane
from hosted_agent_kit.controllers.reports import ReconcileReport
from hosted_agent_kit.controllers.runtime import ReconcileResult
from hosted_agent_kit.domain.enums import LocalSessionState, VersionDrain
from hosted_agent_kit.domain.errors import AppError, FoundryError
from hosted_agent_kit.domain.resources import ResourceKey, ResourceKind, pool_key, session_key
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.ports.registry import ChangeEvent, ChangeType, SessionRegistry
from hosted_agent_kit.services.events import EventType
from hosted_agent_kit.services.pool import PoolService
from hosted_agent_kit.services.session_ids import SessionIdDeriver


class PoolController:
    name = "pool"
    workers = 2

    def __init__(
        self,
        *,
        config: ConfigSource,
        adapter: FoundryAdapter,
        registry: SessionRegistry,
        pool: PoolService,
        plane: ControlPlane,
        cache: ObservationCache,
        session_ids: SessionIdDeriver | None = None,
    ) -> None:
        self._config = config
        self._adapter = adapter
        self._registry = registry
        self._pool = pool
        self._plane = plane
        self._cache = cache
        self._session_ids = session_ids

    def map_event(self, event: ChangeEvent) -> list[ResourceKey]:
        # A session going away frees capacity, which may call for a replacement warm session.
        return [pool_key(event.key.agent_name)] if event.type is ChangeType.DELETED else []

    async def reconcile(self, key: ResourceKey) -> ReconcileResult:
        cfg = self._config.get(key.agent_name)
        if key.kind is not ResourceKind.AGENT_POOL or cfg is None:
            return ReconcileResult.done()
        observation = self._cache.get(cfg.name)
        if observation is None or not observation.complete:
            return ReconcileResult.done()  # never act on a missing or partial view of Foundry
        report = self._plane.report(cfg.name) or ReconcileReport(agent_name=cfg.name)
        await self._drain_old_versions(cfg, report)
        await self._maintain_warm(cfg, report)
        return ReconcileResult.done()

    async def _drain_old_versions(self, cfg: AgentConfig, report: ReconcileReport) -> None:
        """Retire idle sessions that run a version other than the target version.

        The target is the pinned ``agent_version``, or the latest version. Leased sessions are
        never touched, so a request in flight finishes on the version it started with.
        """
        if cfg.version_drain is VersionDrain.NEVER:
            return
        try:
            target = cfg.agent_version or await self._adapter.latest_agent_version(cfg.name)
        except FoundryError as exc:
            report.errors.append(type(exc).__name__)
            return
        if target is None:
            return
        for record in await self._registry.list(cfg.name):
            stale = (
                record.agent_version is not None
                and record.agent_version != target
                and record.local_state is LocalSessionState.AVAILABLE
                and record.lease_request_id is None
            )
            if not stale:
                continue
            owned = record.affinity_key is not None or self._owned_by_id(cfg, record.session_id)
            if owned and cfg.version_drain is not VersionDrain.IDLE:
                continue  # a user's session holds their conversation, so only "idle" ends it
            await self._registry.mark_retiring(
                cfg.name, record.session_id, cleanup=True, reason="version_drain"
            )
            self._plane.manager.enqueue("session", session_key(cfg.name, record.session_id))
            self._plane.events.session(
                cfg.name,
                record.session_id,
                EventType.NORMAL,
                "VersionDrain",
                f"Running {record.agent_version}, the target is {target}. Draining.",
            )

    def _owned_by_id(self, cfg: AgentConfig, session_id: str) -> bool:
        return (
            cfg.stateful
            and self._session_ids is not None
            and self._session_ids.is_derived(session_id)
        )

    async def _maintain_warm(self, cfg: AgentConfig, report: ReconcileReport) -> None:
        """Keep ``min_warm_sessions`` AVAILABLE, unbound sessions, within ``max_sessions``."""
        records = await self._registry.list(cfg.name)
        warm = sum(
            1
            for r in records
            if r.local_state is LocalSessionState.AVAILABLE and r.affinity_key is None
        )
        for _ in range(max(0, cfg.min_warm_sessions - warm)):
            try:
                created = await self._pool.provision_warm(cfg.name)
            except AppError as exc:
                report.errors.append(exc.code)
                self._plane.events.pool(
                    cfg.name,
                    EventType.WARNING,
                    "WarmProvisioningFailed",
                    f"Could not create a warm session ({exc.code}).",
                )
                break
            if not created:
                break
            report.warm_created += 1
