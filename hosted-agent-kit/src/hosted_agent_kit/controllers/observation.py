"""The observation controller: the informer and the recovery logic.

Foundry offers list and get but no watch, so the pool observes it by listing. Each observation
is compared with the last one to produce events, the store is brought in line with what Foundry
reports, and anything that has to go is marked for deletion for the session controller to remove.

Foundry is authoritative for whether a session exists and for its platform status. A session
missing from a listing is treated as gone only after a *complete* listing and a confirming
``get_session``. A partial listing never implies a deletion. Anything uncertain leaves the record
UNAVAILABLE and is checked again at the next observation.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.config.models import AgentConfig
from hosted_agent_kit.config.settings import ShardSettings
from hosted_agent_kit.controllers.plane import ControlPlane
from hosted_agent_kit.controllers.reports import ReconcileReport
from hosted_agent_kit.controllers.runtime import ReconcileResult
from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState
from hosted_agent_kit.domain.errors import FoundryError, FoundrySessionNotFound
from hosted_agent_kit.domain.models import FoundrySession, SessionRecord
from hosted_agent_kit.domain.resources import (
    FINALIZER_PLATFORM_DELETION,
    ResourceKey,
    ResourceKind,
    session_key,
)
from hosted_agent_kit.logging_config import hash_identifier, log_event
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.ports.metrics import MetricsRecorder
from hosted_agent_kit.ports.registry import ChangeEvent, SessionRegistry
from hosted_agent_kit.services.clock import Clock
from hosted_agent_kit.services.events import EventType
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from hosted_agent_kit.services.sharding import shard_of

logger = logging.getLogger(__name__)

_GONE = frozenset({FoundrySessionStatus.DELETED, FoundrySessionStatus.EXPIRED})
_PROVISIONING = frozenset(
    {FoundrySessionStatus.CREATING, FoundrySessionStatus.UPDATING, FoundrySessionStatus.UNKNOWN}
)


@dataclass
class AgentObservation:
    """What the last observation of one agent found."""

    attempted_at: float | None = None
    completed_at: float | None = None  # monotonic time of the last complete listing
    complete: bool = False  # whether the most recent attempt listed everything
    consecutive_failures: int = 0
    statuses: dict[str, FoundrySessionStatus] = field(default_factory=dict)


class ObservationCache:
    def __init__(self) -> None:
        self._agents: dict[str, AgentObservation] = {}

    def get(self, agent_name: str) -> AgentObservation | None:
        return self._agents.get(agent_name)

    def entry(self, agent_name: str) -> AgentObservation:
        return self._agents.setdefault(agent_name, AgentObservation())


class ObservationController:
    name = "observation"
    workers = 1

    def __init__(
        self,
        *,
        config: ConfigSource,
        adapter: FoundryAdapter,
        registry: SessionRegistry,
        plane: ControlPlane,
        metrics: MetricsRecorder,
        clock: Clock,
        cache: ObservationCache,
        session_ids: SessionIdDeriver | None = None,
        shard: ShardSettings | None = None,
    ) -> None:
        self._shard = shard
        self._config = config
        self._adapter = adapter
        self._registry = registry
        self._plane = plane
        self._metrics = metrics
        self._clock = clock
        self._cache = cache
        self._session_ids = session_ids

    def map_event(self, event: ChangeEvent) -> list[ResourceKey]:
        return []  # observation is driven by the resync loop, not by store changes

    async def reconcile(self, key: ResourceKey) -> ReconcileResult:
        cfg = self._config.get(key.agent_name)
        if key.kind is not ResourceKind.AGENT_POOL or cfg is None:
            return ReconcileResult.done()
        report = self._plane.report(cfg.name) or ReconcileReport(agent_name=cfg.name)
        await self.observe(cfg, report)
        return ReconcileResult.done()

    # ----------------------------------------------------------------- algorithm

    async def observe(self, cfg: AgentConfig, report: ReconcileReport) -> None:
        observation = self._cache.entry(cfg.name)
        observation.attempted_at = self._clock.monotonic()
        seen: dict[str, FoundrySession] = {}
        complete = True
        try:
            async for session in self._adapter.list_sessions(cfg.name):
                seen[session.session_id] = session
        except FoundryError as exc:
            complete = False
            report.outcome = "incomplete"
            report.errors.append(type(exc).__name__)
        observation.complete = complete
        if complete:
            observation.completed_at = self._clock.monotonic()
            observation.consecutive_failures = 0
        else:
            observation.consecutive_failures += 1
            self._plane.events.pool(
                cfg.name,
                EventType.WARNING,
                "ObservationIncomplete",
                f"The session listing failed ({report.errors[-1]}). Nothing will be deleted "
                "on the strength of it.",
            )

        local = {r.session_id: r for r in await self._registry.list(cfg.name)}
        for session in seen.values():
            report.discovered += 1
            self._note_status(cfg, observation, session)
            await self._apply(cfg, session, local.get(session.session_id), report)

        for session_id, record in local.items():
            if session_id in seen:
                continue
            if complete:
                # A leased session is verified too. If it is gone it is marked for deletion now,
                # and its removal waits for the lease to end, so it is never offered again.
                await self._verify_missing(cfg, record, report)
            elif record.lease_request_id is None:
                await self._mark_uncertain(cfg, session_id, report)
        if complete:
            observation.statuses = {sid: s.status for sid, s in seen.items()}

    def _note_status(
        self, cfg: AgentConfig, observation: AgentObservation, session: FoundrySession
    ) -> None:
        previous = observation.statuses.get(session.session_id)
        if previous is not None and previous is not session.status:
            self._plane.events.session(
                cfg.name,
                session.session_id,
                EventType.NORMAL,
                "StatusChanged",
                f"Foundry status {previous.value} to {session.status.value}.",
            )

    async def _apply(
        self,
        cfg: AgentConfig,
        session: FoundrySession,
        record: SessionRecord | None,
        report: ReconcileReport,
    ) -> None:
        status = session.status
        if status in _GONE:
            if record is not None:
                await self._registry.refresh(
                    cfg.name,
                    session.session_id,
                    platform_status=status,
                    agent_version=session.agent_version,
                    last_accessed_at=session.last_accessed_at,
                    expires_at=session.expires_at,
                    last_seen_at=self._clock.now(),
                )
                await self._retire(cfg, session.session_id, "gone", cleanup=False, report=report)
            return
        if record is None:
            if not self._belongs_to_this_shard(session):
                return  # another kit owns it: not ours to adopt, count or delete
            if not (cfg.adopt_unbound_sessions or self._owned_by_derivation(cfg, session)):
                log_event(
                    logger,
                    "session_ignored",
                    agent_name=cfg.name,
                    session_id_hash=hash_identifier(session.session_id),
                    reason="adopt_unbound_sessions is false",
                )
                self._plane.events.session(
                    cfg.name,
                    session.session_id,
                    EventType.NORMAL,
                    "Ignored",
                    "Not adopted: adopt_unbound_sessions is false and the id is not derived.",
                )
                return
            await self._adopt(cfg, session)
        else:
            await self._registry.refresh(
                cfg.name,
                session.session_id,
                platform_status=status,
                agent_version=session.agent_version,
                last_accessed_at=session.last_accessed_at,
                expires_at=session.expires_at,
                last_seen_at=self._clock.now(),
            )
        if status is FoundrySessionStatus.FAILED:
            report.failed_sessions += 1
            self._metrics.session_failed(cfg.name)
            log_event(
                logger,
                "session_failed",
                level=logging.WARNING,
                agent_name=cfg.name,
                session_id_hash=hash_identifier(session.session_id),
                platform_status=status.value,
                action="delete",
            )
            self._plane.events.session(
                cfg.name,
                session.session_id,
                EventType.WARNING,
                "SessionFailed",
                "Foundry reports the session failed. Deleting it.",
            )
            await self._retire(cfg, session.session_id, "failed", cleanup=True, report=report)
        elif status is FoundrySessionStatus.DELETING:
            # Foundry is deleting it. Keep the record until the session is gone, so it is not
            # adopted again in between.
            await self._registry.mark_retiring(
                cfg.name,
                session.session_id,
                cleanup=False,
                reason="foundry_deleting",
                finalizers=(FINALIZER_PLATFORM_DELETION,),
            )
        elif status in _PROVISIONING:
            await self._registry.set_local_state(
                cfg.name,
                session.session_id,
                LocalSessionState.UNAVAILABLE,
                only_from=frozenset({LocalSessionState.AVAILABLE}),
            )
        else:
            await self._registry.set_local_state(
                cfg.name,
                session.session_id,
                LocalSessionState.AVAILABLE,
                only_from=frozenset({LocalSessionState.UNAVAILABLE}),
            )

    def _belongs_to_this_shard(self, session: FoundrySession) -> bool:
        """With sharding, a session is ours if its id carries our shard. Ids without a shard
        prefix (made before sharding, or by someone else) belong to shard 0, so exactly one kit
        decides whether to adopt them."""
        if self._shard is None:
            return True
        owner = shard_of(session.session_id)
        return owner == self._shard.index if owner is not None else self._shard.index == 0

    def _owned_by_derivation(self, cfg: AgentConfig, session: FoundrySession) -> bool:
        """A derived id was created by this service for one user, who can reclaim it."""
        return (
            cfg.stateful
            and self._session_ids is not None
            and self._session_ids.is_derived(session.session_id)
        )

    async def _adopt(self, cfg: AgentConfig, session: FoundrySession) -> None:
        """Register a session Foundry knows about but this process does not."""
        now = self._clock.now()
        record = SessionRecord(
            session_id=session.session_id,
            agent_name=cfg.name,
            agent_version=session.agent_version,
            platform_status=session.status,
            local_state=LocalSessionState.UNAVAILABLE,
            created_at=session.created_at,
            last_seen_at=now,
            last_accessed_at=session.last_accessed_at,
            expires_at=session.expires_at,
        )
        await self._registry.add(record)
        self._plane.events.session(
            cfg.name,
            session.session_id,
            EventType.NORMAL,
            "Adopted",
            "Found in Foundry and registered.",
        )

    async def _retire(
        self,
        cfg: AgentConfig,
        session_id: str,
        reason: str,
        *,
        cleanup: bool,
        report: ReconcileReport,
    ) -> None:
        """Mark a session for deletion. The session controller does the rest."""
        await self._registry.mark_retiring(cfg.name, session_id, cleanup=cleanup, reason=reason)
        if not cleanup:
            # Nothing remains to wait for on Foundry's side.
            await self._registry.remove_finalizer(cfg.name, session_id, FINALIZER_PLATFORM_DELETION)
        # Queue it even if it was already marked: a delete that failed earlier is retried by
        # every observation.
        self._plane.manager.enqueue("session", session_key(cfg.name, session_id))

    async def _verify_missing(
        self, cfg: AgentConfig, record: SessionRecord, report: ReconcileReport
    ) -> None:
        """A session absent from a complete listing is checked once before removal."""
        try:
            session = await self._adapter.get_session(cfg.name, record.session_id)
        except FoundrySessionNotFound:
            self._plane.events.session(
                cfg.name,
                record.session_id,
                EventType.WARNING,
                "Missing",
                "Not in Foundry any more. Removing the record.",
            )
            await self._retire(cfg, record.session_id, "missing", cleanup=False, report=report)
            return
        except FoundryError:
            await self._mark_uncertain(cfg, record.session_id, report)
            return
        await self._apply(cfg, session, record, report)

    async def _mark_uncertain(
        self, cfg: AgentConfig, session_id: str, report: ReconcileReport
    ) -> None:
        report.uncertain += 1
        await self._registry.set_local_state(
            cfg.name,
            session_id,
            LocalSessionState.UNAVAILABLE,
            only_from=frozenset({LocalSessionState.AVAILABLE}),
        )
