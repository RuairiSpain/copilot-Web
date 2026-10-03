"""The built-in scheduling plugins, and the registry that profiles look them up in."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass

from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.domain.enums import LocalSessionState, SchedulerStrategy
from hosted_agent_kit.domain.models import SessionRecord
from hosted_agent_kit.ports.affinity import AffinityStore
from hosted_agent_kit.ports.registry import SessionRegistry
from hosted_agent_kit.services.scheduler import Scheduler
from hosted_agent_kit.services.scheduling.framework import (
    PROCEED,
    CycleState,
    FilterPlugin,
    PreFilterPlugin,
    PreFilterResult,
    SchedulingRequest,
    ScorePlugin,
)
from hosted_agent_kit.services.session_ids import SessionIdDeriver

EXPIRY_MARGIN_SECONDS = 30.0


# ------------------------------------------------------------------------ pre-filter


class AffinityResolution:
    """Works out whether a stateful request already has a session of its own.

    - Another request is creating its session: wait for that.
    - It is bound to a healthy session: that session is the target.
    - The binding is stale (the session is gone or going): drop it.
    - After a restart its session may be waiting under the id derived from it: that is the target,
      if it is free, and the request waits if it is not.
    """

    name = "AffinityResolution"

    def __init__(
        self,
        registry: SessionRegistry,
        affinity: AffinityStore,
        session_ids: SessionIdDeriver | None,
    ) -> None:
        self._registry = registry
        self._affinity = affinity
        self._session_ids = session_ids

    async def pre_filter(self, request: SchedulingRequest, state: CycleState) -> PreFilterResult:
        key = request.affinity_key
        if key is None:
            return PROCEED
        cfg = request.agent
        entry = await self._affinity.get(key)
        if entry is not None and entry.session_id is None:
            return PreFilterResult(wait=True, pinned=True)  # being created by another request
        if entry is not None and entry.session_id is not None:
            record = await self._registry.get(cfg.name, entry.session_id)
            if record is None or record.deletion_timestamp is not None:
                await self._affinity.remove(key)  # stale mapping
            else:
                state.target = entry.session_id
                return PROCEED
        if request.derived_ids and self._session_ids is not None:
            derived = self._session_ids.for_user(cfg.name, key.user_id, key.conversation_key)
            state.derived_id = derived
            record = await self._registry.get(cfg.name, derived)
            if record is not None and record.affinity_key is None:
                if record.local_state in (LocalSessionState.UNAVAILABLE, LocalSessionState.LEASED):
                    return PreFilterResult(wait=True, pinned=True)  # still provisioning, or in use
                if record.local_state is LocalSessionState.AVAILABLE:
                    state.target = derived
                    state.restoring = True
        return PROCEED


# ---------------------------------------------------------------------------- filters


class Ready:
    """The session is idle and not being deleted."""

    name = "Ready"

    def filter(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> bool:
        return (
            session.local_state is LocalSessionState.AVAILABLE
            and session.deletion_timestamp is None
        )


class AffinityCompatible:
    """A stateful request may use its own session or a free one. A stateless one, a free one."""

    name = "AffinityCompatible"

    def filter(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> bool:
        if request.affinity_key is None:
            return session.affinity_key is None
        return session.affinity_key is None or session.affinity_key == request.affinity_key


class RestoreHeld:
    """With derived ids every user's session is created under their own id.

    A free session therefore cannot be handed to a user: it is either held for the user whose id
    it carries, or it is not theirs to use.
    """

    name = "RestoreHeld"

    def filter(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> bool:
        if not (request.derived_ids and request.affinity_key is not None):
            return True
        return (
            session.affinity_key == request.affinity_key
            or session.session_id == state.derived_id  # the user's own, found again after a restart
        )


class VersionCompatible:
    """When the agent pins a version, only sessions on it (or of unknown version) are used."""

    name = "VersionCompatible"

    def filter(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> bool:
        pinned = request.agent.agent_version
        return pinned is None or session.agent_version in (None, pinned)


class NotExpiring:
    """Skip sessions that will expire within a short margin, so a request is not cut off."""

    name = "NotExpiring"

    def filter(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> bool:
        if session.expires_at is None:
            return True
        return (session.expires_at - request.now).total_seconds() > EXPIRY_MARGIN_SECONDS


# ----------------------------------------------------------------------------- scores


@dataclass
class _StrategyScore:
    """Ranks by one of the classic strategies. The best candidate scores 1, the rest 0."""

    name: str
    strategy: SchedulerStrategy
    scheduler: Scheduler

    def prepare(
        self, request: SchedulingRequest, candidates: list[SessionRecord], state: CycleState
    ) -> None:
        # Round robin must advance its cursor only for the session that is actually used.
        chosen = self.scheduler.peek(self.strategy, request.agent.name, candidates)
        state.data[f"best:{self.name}"] = chosen.session_id if chosen else None

    def score(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> int:
        return 1 if state.data.get(f"best:{self.name}") == session.session_id else 0

    def selected(self, request: SchedulingRequest, session: SessionRecord) -> None:
        self.scheduler.advance(self.strategy, request.agent.name, session)


class AffinityPreference:
    """Prefer the session already bound to this request's user and conversation."""

    name = "AffinityPreference"

    def score(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> int:
        return 1 if request.affinity_key and session.affinity_key == request.affinity_key else 0


class VersionPreferred:
    """Prefer a session on the pinned version, if the agent pins one."""

    name = "VersionPreferred"

    def score(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> int:
        pinned = request.agent.agent_version
        return 1 if pinned is not None and session.agent_version == pinned else 0


class ExpiryRisk:
    """Prefer sessions with more time left, from 0 (about to expire) to 10 (an hour or more)."""

    name = "ExpiryRisk"

    def score(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> int:
        if session.expires_at is None:
            return 10
        remaining = (session.expires_at - request.now).total_seconds()
        return max(0, min(10, int(remaining // 360)))


# --------------------------------------------------------------------------- registry

FilterFactory = Callable[[], FilterPlugin]
ScoreFactory = Callable[[], ScorePlugin]


class PluginRegistry:
    """Looks plugins up by name. Applications can add their own before the service starts."""

    def __init__(self) -> None:
        self._filters: dict[str, FilterFactory] = {}
        self._scores: dict[str, ScoreFactory] = {}

    def register_filter(self, name: str, factory: FilterFactory) -> None:
        self._filters[name] = factory

    def register_score(self, name: str, factory: ScoreFactory) -> None:
        self._scores[name] = factory

    def filter(self, name: str) -> FilterPlugin:
        if name not in self._filters:
            raise ConfigError(f"unknown scheduler filter '{name}'; known: {sorted(self._filters)}")
        return self._filters[name]()

    def score(self, name: str) -> ScorePlugin:
        if name not in self._scores:
            raise ConfigError(f"unknown scheduler score '{name}'; known: {sorted(self._scores)}")
        return self._scores[name]()

    @property
    def filter_names(self) -> list[str]:
        return sorted(self._filters)

    @property
    def score_names(self) -> list[str]:
        return sorted(self._scores)


STRATEGY_PLUGIN = {
    SchedulerStrategy.FIRST_AVAILABLE: "FirstAvailable",
    SchedulerStrategy.ROUND_ROBIN: "RoundRobin",
    SchedulerStrategy.OLDEST_IDLE: "OldestIdle",
    SchedulerStrategy.NEWEST_IDLE: "NewestIdle",
}


def _strategy_factory(name: str, strategy: SchedulerStrategy, shared: Scheduler) -> ScoreFactory:
    def make() -> ScorePlugin:
        return _StrategyScore(name, strategy, shared)

    return make


def default_plugins(scheduler: Scheduler | None = None) -> PluginRegistry:
    """The built-in plugins. Strategy scores share one scheduler and its round-robin cursor."""
    shared = scheduler or Scheduler()
    registry = PluginRegistry()
    for filter_class in (Ready, AffinityCompatible, RestoreHeld, VersionCompatible, NotExpiring):
        registry.register_filter(filter_class.name, filter_class)
    for strategy, name in STRATEGY_PLUGIN.items():
        registry.register_score(name, _strategy_factory(name, strategy, shared))
    for score_class in (AffinityPreference, VersionPreferred, ExpiryRisk):
        registry.register_score(score_class.name, score_class)
    return registry


__all__ = [
    "AffinityCompatible",
    "AffinityPreference",
    "AffinityResolution",
    "ExpiryRisk",
    "NotExpiring",
    "PluginRegistry",
    "PreFilterPlugin",
    "Ready",
    "RestoreHeld",
    "VersionCompatible",
    "VersionPreferred",
    "default_plugins",
]
