"""The scheduling cycle. The caller holds the agent's lock, so a cycle never interleaves."""

from __future__ import annotations

import uuid
from dataclasses import dataclass, field
from datetime import datetime
from typing import Any, Protocol

from hosted_agent_kit.config.models import AgentConfig
from hosted_agent_kit.domain.models import SessionAffinityKey, SessionRecord
from hosted_agent_kit.ports.affinity import AffinityStore
from hosted_agent_kit.ports.metrics import MetricsRecorder
from hosted_agent_kit.ports.queue import CreateGrant, Grant, SessionGrant
from hosted_agent_kit.ports.registry import SessionRegistry
from hosted_agent_kit.services.scheduler import order_key


@dataclass(frozen=True)
class SchedulingRequest:
    """Everything a plugin may look at. Built once per cycle and never changed."""

    agent: AgentConfig
    request_id: str
    affinity_key: SessionAffinityKey | None  # set for stateful agents
    now: datetime
    derived_ids: bool  # sessions are created under ids derived from the user


@dataclass
class CycleState:
    """Scratch space shared by the plugins of one cycle."""

    data: dict[str, Any] = field(default_factory=dict)
    target: str | None = None  # the one session this request must use, if it has one
    restoring: bool = False  # the target is a session found again after a restart
    derived_id: str | None = None


@dataclass(frozen=True)
class PreFilterResult:
    """Normally proceed. ``wait`` stops the cycle and the request waits for a session."""

    wait: bool = False
    pinned: bool = False


PROCEED = PreFilterResult()


@dataclass(frozen=True)
class Wait:
    pinned: bool


Decision = SessionGrant | CreateGrant | Wait


class PreFilterPlugin(Protocol):
    name: str

    async def pre_filter(
        self, request: SchedulingRequest, state: CycleState
    ) -> PreFilterResult: ...


class FilterPlugin(Protocol):
    name: str

    def filter(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> bool:
        """True if the session can serve the request."""
        ...


class PreScorePlugin(Protocol):
    def prepare(
        self, request: SchedulingRequest, candidates: list[SessionRecord], state: CycleState
    ) -> None: ...


class ScorePlugin(Protocol):
    name: str

    def score(
        self, request: SchedulingRequest, session: SessionRecord, state: CycleState
    ) -> int: ...


class SelectionObserver(Protocol):
    """A score plugin that needs to know which session was finally chosen."""

    def selected(self, request: SchedulingRequest, session: SessionRecord) -> None: ...


@dataclass(frozen=True)
class Profile:
    """The plugins for one agent. Core filters always run, then the configured ones."""

    name: str
    pre_filters: tuple[PreFilterPlugin, ...]
    filters: tuple[FilterPlugin, ...]
    scores: tuple[tuple[ScorePlugin, int], ...]


def _new_token() -> str:
    return f"slot_{uuid.uuid4().hex}"


class SchedulerFramework:
    def __init__(
        self,
        *,
        registry: SessionRegistry,
        affinity: AffinityStore,
        metrics: MetricsRecorder,
        profiles: dict[str, Profile],
    ) -> None:
        self._registry = registry
        self._affinity = affinity
        self._metrics = metrics
        self._profiles = profiles

    def replace_profiles(self, profiles: dict[str, Profile]) -> None:
        self._profiles = profiles

    def profile(self, agent_name: str) -> Profile:
        return self._profiles[agent_name]

    async def schedule(self, request: SchedulingRequest) -> Decision:
        """Decide what the request can have: a leased session, a slot to create one, or a wait."""
        cfg = request.agent
        profile = self._profiles[cfg.name]
        state = CycleState()

        # PreFilter
        for pre in profile.pre_filters:
            result = await pre.pre_filter(request, state)
            if result.wait:
                return Wait(pinned=result.pinned)

        # Filter
        records = await self._registry.list(cfg.name)
        feasible = [r for r in records if all(f.filter(request, r, state) for f in profile.filters)]
        if state.target is not None:
            feasible = [r for r in feasible if r.session_id == state.target]
            if not feasible:
                return Wait(pinned=True)  # its own session is busy: it waits for that one

        # Score
        for plugin, _ in profile.scores:
            prepare = getattr(plugin, "prepare", None)
            if prepare is not None:
                prepare(request, feasible, state)
        ranked = sorted(
            feasible,
            key=lambda r: (
                -sum(weight * plugin.score(request, r, state) for plugin, weight in profile.scores),
                order_key(r),
            ),
        )

        # Reserve, then Bind
        for session in ranked:
            leased = await self._registry.try_lease(
                cfg.name, session.session_id, request.request_id, request.now
            )
            if leased is None:
                if state.target is not None:
                    return Wait(pinned=True)
                continue
            for plugin, _ in profile.scores:
                selected = getattr(plugin, "selected", None)
                if selected is not None:
                    selected(request, session)
            await self._bind(request, session, state)
            return SessionGrant(session.session_id)

        # Nothing to lease: reserve capacity to create a session, or Permit a wait.
        token = _new_token()
        if await self._registry.reserve_slot(cfg.name, token, cfg.max_sessions):
            if request.affinity_key is not None:
                await self._affinity.reserve(request.affinity_key, request.request_id)
            return CreateGrant(token=token, affinity_key=request.affinity_key)
        return Wait(pinned=False)

    async def _bind(
        self, request: SchedulingRequest, session: SessionRecord, state: CycleState
    ) -> None:
        key = request.affinity_key
        if key is None:
            return
        if session.affinity_key != key:
            await self._registry.bind(request.agent.name, session.session_id, key)
            await self._affinity.bind(key, session.session_id)
        if state.restoring:
            self._metrics.session_restored(request.agent.name)

    async def unreserve(
        self, cfg: AgentConfig, grant: Grant, request_id: str, now: datetime
    ) -> None:
        """Give back what a cycle reserved. Safe to call more than once."""
        if isinstance(grant, SessionGrant):
            await self._registry.release(cfg.name, grant.session_id, request_id, now)
            return
        if grant.affinity_key is not None:
            await self._affinity.cancel_reservation(grant.affinity_key, request_id)
        await self._registry.release_slot(cfg.name, grant.token)
