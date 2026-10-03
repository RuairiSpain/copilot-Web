"""Quota control: which sessions hold compute, the kit's limit on them, and how it adapts.

Foundry limits concurrent hosted-agent sessions per subscription and region. Only sessions whose
compute is provisioning or running count. The kit therefore keeps two numbers apart:

- *persisted* sessions (``max_sessions``): sessions that exist, idle ones included;
- *active* sessions (``max_active_sessions``, the kit budget): sessions that may hold compute.

A session is **counted** (assumed to hold compute) while it is leased, provisioning, or inside the
idle window after its last use. ``QuotaGate`` decides, atomically, whether one more session may
become counted. ``KitGovernor`` is the kit-wide limit: a static budget, lowered when Foundry
refuses a session for quota and raised again after a quiet period, optionally extended by
permits borrowed from a shared spare pool (``QuotaLedger``).
"""

from __future__ import annotations

import asyncio
import contextlib
import heapq
import logging
import math
import uuid
from datetime import UTC, datetime, timedelta

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.config.models import AgentConfig
from hosted_agent_kit.config.settings import KitSettings, QuotaSettings
from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState
from hosted_agent_kit.domain.errors import FoundryError
from hosted_agent_kit.domain.models import SessionRecord
from hosted_agent_kit.logging_config import hash_identifier, log_event
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.ports.metrics import MetricsRecorder
from hosted_agent_kit.ports.quota import QuotaLedger
from hosted_agent_kit.ports.registry import ChangeEvent, ChangeType, SessionRegistry
from hosted_agent_kit.services.clock import Clock
from hosted_agent_kit.services.store import mutate_session

logger = logging.getLogger(__name__)

_NOT_HOLDING = (
    FoundrySessionStatus.FAILED,
    FoundrySessionStatus.DELETING,
    FoundrySessionStatus.DELETED,
    FoundrySessionStatus.EXPIRED,
)
_PROVISIONING = (FoundrySessionStatus.CREATING, FoundrySessionStatus.UPDATING)
# Foundry deprovisions a little after the timeout; count the session until then.
IDLE_MARGIN_SECONDS = 30


def last_activity(record: SessionRecord) -> datetime:
    candidates = [
        record.last_released_at,
        record.last_accessed_at,
        record.lease_acquired_at,
        record.created_at,
    ]
    return max(c for c in candidates if c is not None)


FOREVER = datetime.max.replace(tzinfo=UTC)


def counted_until(
    record: SessionRecord, cfg: AgentConfig, *, idle_status_deprovisions: bool
) -> datetime | None:
    """Until when the session is assumed to hold compute: ``FOREVER`` while it is leased or
    starting, the end of its idle window otherwise, or None when it holds none."""
    if record.lease_request_id is not None or record.platform_status in _PROVISIONING:
        return FOREVER
    if record.platform_status in _NOT_HOLDING:
        return None
    activity = last_activity(record)
    if record.compute_released_at is not None and record.compute_released_at >= activity:
        return None
    if idle_status_deprovisions and record.platform_status is FoundrySessionStatus.IDLE:
        return None
    return activity + timedelta(seconds=cfg.idle_timeout_seconds + IDLE_MARGIN_SECONDS)


def is_counted(
    record: SessionRecord, cfg: AgentConfig, now: datetime, *, idle_status_deprovisions: bool
) -> bool:
    """True when the session is assumed to hold compute, and so to count toward the quota."""
    until = counted_until(record, cfg, idle_status_deprovisions=idle_status_deprovisions)
    return until is not None and now < until


class CountedIndex:
    """How many sessions of each agent are counted, kept up to date from registry changes.

    Counting by scanning every session on every request does not scale to thousands of sessions.
    The index keeps one expiry per session and a heap of the idle windows that will end, so a count
    is a few heap pops instead of a scan.
    """

    def __init__(self) -> None:
        self._until: dict[tuple[str, str], datetime] = {}
        self._finite: dict[str, int] = {}  # per agent: sessions with an idle window still open
        self._forever: dict[str, int] = {}  # per agent: leased or starting sessions
        self._heap: list[tuple[datetime, str, str]] = []

    def set(self, agent: str, session_id: str, until: datetime | None) -> None:
        key = (agent, session_id)
        previous = self._until.pop(key, None)
        if previous is not None:
            self._bump(agent, previous, -1)
        if until is not None:
            self._until[key] = until
            self._bump(agent, until, +1)
            if until != FOREVER:
                heapq.heappush(self._heap, (until, agent, session_id))

    def _bump(self, agent: str, until: datetime, step: int) -> None:
        table = self._forever if until == FOREVER else self._finite
        table[agent] = table.get(agent, 0) + step

    def count(self, agent: str, now: datetime) -> int:
        while self._heap and self._heap[0][0] <= now:
            until, name, session_id = heapq.heappop(self._heap)
            if self._until.get((name, session_id)) == until:  # not replaced since
                del self._until[(name, session_id)]
                self._bump(name, until, -1)
        return self._finite.get(agent, 0) + self._forever.get(agent, 0)

    def clear(self) -> None:
        self._until.clear()
        self._finite.clear()
        self._forever.clear()
        self._heap.clear()


class KitGovernor:
    """The kit-wide limit on counted sessions.

    ``base`` is the static budget, lowered by ``on_refused`` and raised by ``tick``. With no budget
    the limit is unbounded until Foundry first refuses a session, then it starts from what the kit
    was holding. Borrowed spare-pool permits add to the limit.
    """

    def __init__(
        self,
        settings: QuotaSettings,
        clock: Clock,
        *,
        kit_id: str,
        ledger: QuotaLedger | None = None,
    ) -> None:
        self._s = settings
        self._clock = clock
        self._kit_id = kit_id
        self._ledger = ledger
        self._ceiling: int | None = settings.budget
        self._base: int | None = settings.budget
        self._last_change = clock.monotonic()
        self._cooldown_until = 0.0
        self._permits: dict[str, float] = {}
        self.refusals = 0

    # ------------------------------------------------------------------ limit

    @property
    def base(self) -> int | None:
        return self._base

    @property
    def ceiling(self) -> int | None:
        return self._ceiling

    @property
    def borrowed(self) -> int:
        return len(self._permits)

    @property
    def tokens(self) -> list[str]:
        return list(self._permits)

    def limit(self) -> int | None:
        """The most sessions the kit may count now, or None when nothing limits it."""
        if self._base is None:
            return None
        return self._base + len(self._permits)

    # ------------------------------------------------------------- adaptation

    def on_refused(self, scope: str, active_now: int) -> None:
        """Foundry refused a session because a quota is full: lower the limit."""
        now = self._clock.monotonic()
        self.refusals += 1
        if now < self._cooldown_until:
            return
        s = self._s
        if self._base is None:
            self._ceiling = max(active_now, s.min_limit)
            held = active_now
        else:
            held = min(self._base, max(active_now, s.min_limit))
        lowered = max(s.min_limit, int(held * s.decrease_factor))
        if self._base is not None:
            lowered = min(lowered, self._base)
        self._base = lowered
        self._last_change = now
        self._cooldown_until = now + s.cooldown_seconds
        log_event(
            logger,
            "quota_limit_lowered",
            level=logging.WARNING,
            kit_id=self._kit_id,
            scope=scope,
            limit=lowered,
            ceiling=self._ceiling,
        )

    def tick(self) -> None:
        """Raise the limit one step when Foundry has not refused for ``probe_seconds``."""
        if self._base is None or self._ceiling is None or self._base >= self._ceiling:
            if self._base is not None and self._base == self._ceiling and self._s.budget is None:
                self._base = None  # back to the level that worked before the refusal
                self._ceiling = None
            return
        now = self._clock.monotonic()
        if now - self._last_change < self._s.probe_seconds or now < self._cooldown_until:
            return
        # At least ``increase_step``, and 5 percent of the ceiling, so a large kit recovers
        # in minutes rather than hours.
        step = max(self._s.increase_step, math.ceil(self._ceiling * 0.05))
        self._base = min(self._ceiling, self._base + step)
        self._last_change = now

    # -------------------------------------------------------------- borrowing

    async def try_borrow(self) -> bool:
        """Take one permit from the shared spare pool. False when there is none or no ledger."""
        if self._ledger is None or self._s.spare <= 0 or self._base is None:
            return False
        token = uuid.uuid4().hex
        try:
            granted = await self._ledger.acquire_spare(self._kit_id, token, self._s.spare)
        except Exception as exc:  # the ledger is optional: its outage must not stop the kit
            self._ledger_error("acquire", exc)
            return False
        if granted:
            self._permits[token] = self._clock.monotonic()
        return granted

    async def rebalance(self, counted: int) -> None:
        """Give back permits the kit no longer needs."""
        if self._ledger is None or self._base is None:
            return
        while self._permits and counted <= self._base + len(self._permits) - 1:
            token = next(iter(self._permits))
            try:
                await self._ledger.release_spare(self._kit_id, token)
            except Exception as exc:
                self._ledger_error("release", exc)
                return
            del self._permits[token]

    async def heartbeat(self, counted: int) -> None:
        """Publish the kit's count and keep its permits alive."""
        if self._ledger is None:
            return
        try:
            await self._ledger.heartbeat(self._kit_id, counted, self.tokens)
        except Exception as exc:
            self._ledger_error("heartbeat", exc)

    async def close(self) -> None:
        if self._ledger is None:
            return
        with contextlib.suppress(Exception):
            await self._ledger.forget(self._kit_id)
        self._permits.clear()

    def _ledger_error(self, action: str, exc: Exception) -> None:
        log_event(
            logger,
            "quota_ledger_error",
            level=logging.WARNING,
            action=action,
            error_type=type(exc).__name__,
        )


class QuotaGate:
    """Admission of sessions that would hold compute: new ones and resumed ones.

    Callers take ``lock`` around "check, then lease or reserve", so two agents cannot both take the
    last unit of the kit's budget.
    """

    def __init__(
        self,
        *,
        registry: SessionRegistry,
        config: ConfigSource,
        clock: Clock,
        settings: KitSettings,
        adapter: FoundryAdapter,
        governor: KitGovernor | None = None,
        metrics: MetricsRecorder | None = None,
    ) -> None:
        self._registry = registry
        self._config = config
        self._clock = clock
        self._settings = settings
        self._adapter = adapter
        self.governor = governor or KitGovernor(
            settings.quota, clock, kit_id=settings.kit_id or "kit"
        )
        self.lock = asyncio.Lock()
        self._index = CountedIndex()
        registry.subscribe(self._on_change)
        self.evictions = 0
        self.last_counted = 0
        self._metrics = metrics

    # ----------------------------------------------------------------- counts

    def counted(self, record: SessionRecord, cfg: AgentConfig, now: datetime) -> bool:
        return is_counted(
            record, cfg, now, idle_status_deprovisions=self._settings.idle_status_deprovisions
        )

    def _on_change(self, event: ChangeEvent) -> None:
        key = event.key
        cfg = self._config.get(key.agent_name)
        if event.type is ChangeType.DELETED or cfg is None:
            self._index.set(key.agent_name, key.session_id, None)
            return
        until = counted_until(
            event.record, cfg, idle_status_deprovisions=self._settings.idle_status_deprovisions
        )
        self._index.set(key.agent_name, key.session_id, until)

    async def rebuild(self) -> None:
        """Recompute every session's window from the registry: after a configuration reload, and
        now and then to correct any drift."""
        self._index.clear()
        for name in self._config.names:
            cfg = self._config.get(name)
            if cfg is None:
                continue
            for record in await self._registry.view(name):
                until = counted_until(
                    record, cfg, idle_status_deprovisions=self._settings.idle_status_deprovisions
                )
                self._index.set(name, record.session_id, until)

    async def agent_counted(self, cfg: AgentConfig, now: datetime) -> int:
        """Counted sessions plus the slots reserved for sessions being created."""
        return self._index.count(cfg.name, now) + await self._registry.reserved(cfg.name)

    async def kit_counted(self, now: datetime) -> int:
        total = 0
        for name in self._config.names:
            cfg = self._config.get(name)
            if cfg is not None:
                total += await self.agent_counted(cfg, now)
        self.last_counted = total
        return total

    # -------------------------------------------------------------- admission

    async def has_room(self, cfg: AgentConfig, now: datetime) -> bool:
        """May one more session of ``cfg`` hold compute? Call with ``lock`` held."""
        if await self.agent_counted(cfg, now) >= cfg.active_limit:
            return False
        limit = self.governor.limit()
        if limit is None:
            return True
        counted = await self.kit_counted(now)
        if counted < limit:
            return True
        if await self.governor.try_borrow():
            return counted < (self.governor.limit() or 0)
        return False

    async def reserve_create(
        self, cfg: AgentConfig, token: str, now: datetime, *, force: bool = False
    ) -> str | None:
        """Reserve a slot for a new session. None when reserved, else why not.

        ``"active"``: the active limits (agent, kit) are reached; stopping an idle session can
        help. ``"persisted"``: ``max_sessions`` sessions already exist; only a session being
        released or removed can help.
        """
        async with self.lock:
            if not force:
                persisted = await self._registry.count(cfg.name)
                if persisted + await self._registry.reserved(cfg.name) >= cfg.max_sessions:
                    return "persisted"  # stopping a session would not make room for a new one
                if not await self.has_room(cfg, now):
                    return "active"
            reserved = await self._registry.reserve_slot(
                cfg.name, token, cfg.max_sessions, force=force
            )
            return None if reserved else "persisted"

    # --------------------------------------------------------------- eviction

    async def evict_one(self, cfg: AgentConfig, now: datetime) -> bool:
        """Stop the least recently used idle session to free compute. Its state is kept.

        Stopping keeps the session's persisted state, so a user's conversation survives and the
        session resumes on its next call. Sessions that are leased, retiring or still starting are
        never chosen. Prefers a session of ``cfg`` when the agent's own limit is the one reached.
        """
        agent_full = await self.agent_counted(cfg, now) >= cfg.active_limit
        candidates: list[tuple[AgentConfig, SessionRecord]] = []
        names = [cfg.name] if agent_full else list(self._config.names)
        for name in names:
            owner = self._config.get(name)
            if owner is None:
                continue
            for record in await self._registry.view(name):
                if (
                    record.local_state is LocalSessionState.AVAILABLE
                    and record.lease_request_id is None
                    and record.deletion_timestamp is None
                    and record.platform_status not in _PROVISIONING
                    and self.counted(record, owner, now)
                ):
                    candidates.append((owner, record))
        candidates.sort(key=lambda pair: last_activity(pair[1]))
        for owner, record in candidates[:3]:
            if await self._stop(owner, record, now):
                return True
        return False

    async def _stop(self, cfg: AgentConfig, record: SessionRecord, now: datetime) -> bool:
        # Hold the session so no scheduler leases it while Foundry stops it.
        held = await self._registry.set_local_state(
            cfg.name,
            record.session_id,
            LocalSessionState.UNAVAILABLE,
            only_from=frozenset({LocalSessionState.AVAILABLE}),
        )
        if not held:
            return False
        stopped = False
        try:
            await self._adapter.stop_session(cfg.name, record.session_id)
            stopped = True
        except FoundryError as exc:
            log_event(
                logger,
                "session_stop_failed",
                level=logging.WARNING,
                agent_name=cfg.name,
                session_id_hash=hash_identifier(record.session_id),
                error_type=type(exc).__name__,
            )

        def apply(r: SessionRecord) -> None:
            if stopped:
                # Never earlier than the last use, so the stop is not read as older than it.
                r.compute_released_at = max(now, last_activity(r))
            if r.local_state is LocalSessionState.UNAVAILABLE and r.lease_request_id is None:
                r.local_state = LocalSessionState.AVAILABLE

        await mutate_session(self._registry, cfg.name, record.session_id, apply)
        if stopped:
            self.evictions += 1
            if self._metrics is not None:
                self._metrics.session_evicted(cfg.name)
            log_event(
                logger,
                "session_evicted",
                agent_name=cfg.name,
                session_id_hash=hash_identifier(record.session_id),
                reason="quota",
            )
        return stopped

    # ---------------------------------------------------------------- signals

    def on_refused(self, agent: str, scope: str) -> None:
        """Foundry refused a session for quota. Lower the kit's limit."""
        if self._metrics is not None:
            self._metrics.quota_refused(agent, scope)
        self.governor.on_refused(scope, self.last_counted)

    async def tick(self) -> int:
        """One housekeeping step: raise the limit, give back permits, publish the count."""
        now = self._clock.now()
        await self.rebuild()  # correct any drift in the incremental count
        counted = await self.kit_counted(now)
        self._last_counted = counted
        self.governor.tick()
        await self.governor.rebalance(counted)
        await self.governor.heartbeat(counted)
        return counted
