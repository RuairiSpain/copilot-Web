"""Pool service: affinity, leases, per-agent FIFO queues, creation and failure recovery.

Concurrency model
-----------------
Every state change for one agent happens inside that agent's ``asyncio.Lock`` (the
"critical section"). The lock is never held across a Foundry call. Requests that cannot
be served immediately are queued; whenever state changes the *dispatcher* re-evaluates
waiters in FIFO order and resolves each future with a grant. There is no polling.

A grant is either a ``SessionGrant`` (an existing session, already leased) or a
``CreateGrant`` (a reserved capacity slot, plus a pending affinity claim for stateful
agents). The grant holder creates the session outside the lock, then registers it.
"""

from __future__ import annotations

import asyncio
import logging
import random
import uuid
from collections.abc import AsyncGenerator, AsyncIterator
from dataclasses import dataclass
from typing import Any

import anyio

from hosted_agent_kit.adapters.foundry_sdk import sse_frame
from hosted_agent_kit.config.holder import ConfigHolder, ConfigSource
from hosted_agent_kit.config.models import AgentConfig, AgentPoolConfig
from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.controllers.plane import ControlPlane
from hosted_agent_kit.controllers.session import SessionController
from hosted_agent_kit.domain.enums import (
    LocalSessionState,
    UserIsolation,
)
from hosted_agent_kit.domain.errors import (
    QUOTA_REGIONAL,
    AgentNotConfiguredError,
    AppError,
    FoundryError,
    FoundryQuotaExceeded,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeoutAppError,
    PoolCapacityExceededError,
    QueueFullError,
    QueueWaitTimeoutError,
    RequestTimeoutError,
    ServiceDrainingError,
    SessionNotFoundAdminError,
    StickySessionTimeoutError,
    StreamTimeoutError,
    UpstreamError,
)
from hosted_agent_kit.domain.models import (
    AgentSnapshot,
    FoundrySession,
    InvokeContext,
    SessionAffinityKey,
    SessionRecord,
)
from hosted_agent_kit.domain.resources import session_key
from hosted_agent_kit.logging_config import hash_identifier, log_event
from hosted_agent_kit.ports.affinity import AffinityStore
from hosted_agent_kit.ports.foundry import FoundryAdapter, UpstreamResponse
from hosted_agent_kit.ports.metrics import MetricsRecorder
from hosted_agent_kit.ports.queue import (
    CreateGrant,
    Grant,
    QueueManager,
    QueueTicket,
    SessionGrant,
)
from hosted_agent_kit.ports.registry import SessionRegistry
from hosted_agent_kit.services.clock import Clock
from hosted_agent_kit.services.errors_map import to_app_error
from hosted_agent_kit.services.events import EventType
from hosted_agent_kit.services.isolation import UserIsolationKeys
from hosted_agent_kit.services.quota import QuotaGate
from hosted_agent_kit.services.remote import RemoteSessions
from hosted_agent_kit.services.scheduler import Scheduler
from hosted_agent_kit.services.scheduling import (
    PluginRegistry,
    SchedulerFramework,
    SchedulingRequest,
    default_plugins,
)
from hosted_agent_kit.services.scheduling.framework import Decision, Profile, Wait
from hosted_agent_kit.services.scheduling.profiles import build_profiles
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from hosted_agent_kit.services.store import mutate_session

logger = logging.getLogger(__name__)

_UNLEASED = frozenset({LocalSessionState.AVAILABLE, LocalSessionState.UNAVAILABLE})


@dataclass(frozen=True)
class PoolRequest:
    agent_name: str
    user_id: str
    payload: Any
    timeout_seconds: float
    request_id: str
    correlation_id: str
    idempotency_key: str | None = None
    stream: bool = False
    conversation_key: str | None = None
    raw_body: bytes | None = None
    content_type: str | None = None


@dataclass
class Lease:
    agent_name: str
    session_id: str
    request_id: str
    affinity_key: SessionAffinityKey | None
    released: bool = False


@dataclass
class PoolResult:
    request_id: str
    status_code: int
    body: dict[str, Any] | None
    stream: AsyncGenerator[bytes] | None
    media_type: str
    session_id: str
    raw: bytes | None = None

    async def close(self) -> None:
        """Release a streaming lease even if the response never finished. Idempotent."""
        if self.stream is not None:
            await self.stream.aclose()


@dataclass
class _Run:
    cfg: AgentConfig
    req: PoolRequest
    lease: Lease


def _outcome(exc: BaseException) -> str:
    if isinstance(exc, AppError):
        return exc.code
    if isinstance(exc, asyncio.CancelledError):
        return "CLIENT_CANCELLED"
    return "INTERNAL_ERROR"


class PoolService:
    def __init__(
        self,
        *,
        config: ConfigSource,
        adapter: FoundryAdapter,
        registry: SessionRegistry,
        affinity: AffinityStore,
        queue: QueueManager,
        metrics: MetricsRecorder,
        scheduler: Scheduler | None = None,
        clock: Clock,
        settings: KitSettings,
        rng: random.Random | None = None,
        session_ids: SessionIdDeriver | None = None,
        plane: ControlPlane | None = None,
        plugins: PluginRegistry | None = None,
        quota: QuotaGate | None = None,
        isolation: UserIsolationKeys | None = None,
    ) -> None:
        self._isolation = isolation
        self._session_ids = session_ids
        self._holder = config if isinstance(config, ConfigHolder) else _holder_for(config)
        self._config: ConfigSource = self._holder
        self._adapter = adapter
        self._registry = registry
        self._affinity = affinity
        self._queue = queue
        self._metrics = metrics
        self._admitting = True
        self._ticker: asyncio.Task[None] | None = None
        self._inflight = 0
        self._idle = asyncio.Event()
        self._idle.set()
        self._plugins = plugins or default_plugins(scheduler)
        self._gate = quota or QuotaGate(
            registry=registry,
            config=self._holder,
            clock=clock,
            settings=settings,
            adapter=adapter,
            metrics=metrics,
        )
        self._framework = SchedulerFramework(
            registry=registry,
            affinity=affinity,
            metrics=metrics,
            profiles=self._build_profiles(),
            gate=self._gate,
            evict_for_quota=settings.evict_idle_for_quota,
        )
        self._clock = clock
        self._settings = settings
        self._rng = rng or random.Random()  # noqa: S311  # nosec B311 - jitter, not security
        self._locks = {name: asyncio.Lock() for name in config.names}
        self._remote = RemoteSessions(
            adapter=adapter,
            settings=settings,
            clock=clock,
            metrics=metrics,
            session_ids=session_ids,
            rng=rng,
            on_quota_exceeded=self._gate.on_refused,
        )
        self._plane = plane or ControlPlane(registry=registry, clock=clock, holder=self._holder)
        self._plane.manager.register(
            SessionController(
                config=config,
                registry=registry,
                affinity=affinity,
                remote=self._remote,
                plane=self._plane,
                pool=self,
            )
        )

    # ------------------------------------------------------------------ public

    def _build_profiles(self, config: ConfigSource | None = None) -> dict[str, Profile]:
        return build_profiles(
            config=config or self._config,
            plugins=self._plugins,
            registry=self._registry,
            affinity=self._affinity,
            session_ids=self._session_ids,
        )

    def check_config(self, config: AgentPoolConfig) -> None:
        """Raise ``ConfigError`` if the scheduling profiles for ``config`` cannot be built."""
        self._build_profiles(config)

    def refresh_profiles(self) -> None:
        """Rebuild the scheduling profiles after the configuration changed."""
        self._framework.replace_profiles(self._build_profiles())

    @property
    def queue(self) -> QueueManager:
        return self._queue

    @property
    def holder(self) -> ConfigHolder:
        return self._holder

    @property
    def affinity(self) -> AffinityStore:
        return self._affinity

    @property
    def clock(self) -> Clock:
        return self._clock

    @property
    def quota(self) -> QuotaGate:
        return self._gate

    @property
    def settings(self) -> KitSettings:
        return self._settings

    @property
    def plane(self) -> ControlPlane:
        return self._plane

    @property
    def remote(self) -> RemoteSessions:
        return self._remote

    async def start(self) -> None:
        """Start the quota housekeeping loop."""
        if self._ticker is None:
            self._ticker = asyncio.create_task(self._tick_loop(), name="pool:quota-ticker")

    async def stop(self) -> None:
        ticker, self._ticker = self._ticker, None
        if ticker is not None:
            ticker.cancel()
            await asyncio.gather(ticker, return_exceptions=True)
        await self._gate.governor.close()

    async def _tick_loop(self) -> None:
        while True:
            await asyncio.sleep(self._settings.quota_tick_seconds)
            try:
                await self._gate.tick()
                await self.dispatch_waiting()
            except Exception as exc:  # housekeeping must never end the loop
                log_event(
                    logger, "quota_tick_error", level=logging.ERROR, error_type=type(exc).__name__
                )

    async def dispatch_waiting(self) -> None:
        """Offer free capacity to queued callers. Needed when capacity frees as time passes."""
        for name in self.agent_names:
            if await self._queue.depth(name) > 0:
                async with self._locks[name]:
                    await self._dispatch_locked(self.agent_config(name))

    def agent_lock(self, agent_name: str) -> asyncio.Lock:
        return self._locks[agent_name]

    async def dispatch_locked(self, cfg: AgentConfig) -> None:
        await self._dispatch_locked(cfg)

    @property
    def agent_names(self) -> list[str]:
        return self._config.names

    def agent_config(self, agent_name: str) -> AgentConfig:
        cfg = self._config.get(agent_name)
        if cfg is None:
            raise AgentNotConfiguredError(f"Agent '{agent_name}' is not configured.")
        return cfg

    def begin_shutdown(self) -> None:
        """Stop admitting requests. Those already running finish."""
        self._admitting = False

    @property
    def draining(self) -> bool:
        return not self._admitting

    @property
    def in_flight(self) -> int:
        return self._inflight

    async def wait_idle(self, grace_seconds: float) -> bool:
        """Wait for running requests (streams included) to finish. False if time ran out."""
        try:
            await asyncio.wait_for(self._idle.wait(), timeout=grace_seconds)
        except TimeoutError:
            return False
        return True

    def _request_started(self) -> None:
        self._inflight += 1
        self._idle.clear()

    def _request_finished(self) -> None:
        self._inflight -= 1
        if self._inflight <= 0:
            self._inflight = 0
            self._idle.set()

    async def execute(self, req: PoolRequest) -> PoolResult:
        cfg = self.agent_config(req.agent_name)
        if not self._admitting:
            raise ServiceDrainingError(
                "The service is shutting down. Retry against another instance or shortly.",
                retry_after_seconds=self._settings.retry_after_seconds,
            )
        self._request_started()
        try:
            result = await self._execute(cfg, req)
        except BaseException:
            self._request_finished()
            raise
        if result.stream is None:
            self._request_finished()  # a stream is finished when its generator ends
        return result

    async def _execute(self, cfg: AgentConfig, req: PoolRequest) -> PoolResult:
        # One clock for every outcome: request duration is end to end, queue wait included.
        started = self._clock.monotonic()
        run: _Run | None = None
        handed_over = False
        try:
            try:
                async with asyncio.timeout(self._settings.max_request_seconds):
                    grant, _ = await self._acquire_grant(cfg, req)
                    lease = await self._materialise(cfg, req, grant)
                    run = _Run(cfg, req, lease)
                    upstream = await self._invoke_with_recovery(run)
            except TimeoutError as exc:
                raise RequestTimeoutError() from exc
            if upstream.stream is not None:
                stream = await self._open_stream(run, upstream.stream, started)
                handed_over = True
                return PoolResult(
                    request_id=req.request_id,
                    status_code=upstream.status_code,
                    body=None,
                    stream=stream,
                    media_type=upstream.media_type,
                    session_id=run.lease.session_id,
                )
            duration = self._clock.monotonic() - started
            self._metrics.request_completed(cfg.name, "ok", duration)
            return PoolResult(
                request_id=req.request_id,
                status_code=upstream.status_code,
                body=upstream.body,
                stream=None,
                media_type=upstream.media_type,
                session_id=run.lease.session_id,
                raw=upstream.raw,
            )
        except BaseException as exc:
            duration = self._clock.monotonic() - started
            self._metrics.request_completed(cfg.name, _outcome(exc), duration)
            raise
        finally:
            if run is not None and not handed_over:
                await self._release(cfg, run.lease)

    async def notify(self, agent_name: str) -> None:
        """Re-run the dispatcher after an external state change (for example a sync)."""
        cfg = self.agent_config(agent_name)
        async with self._locks[agent_name]:
            await self._dispatch_locked(cfg)

    async def snapshot(self, agent_name: str) -> AgentSnapshot:
        cfg = self.agent_config(agent_name)
        records = await self._registry.list(agent_name)
        states = [r.local_state for r in records]
        now = self._clock.now()
        counted = sum(1 for r in records if self._gate.counted(r, cfg, now))
        return AgentSnapshot(
            agent_name=agent_name,
            mode=cfg.mode.value,
            max_sessions=cfg.max_sessions,
            max_active_sessions=cfg.active_limit,
            sessions_counted=counted,
            sessions_total=len(records),
            sessions_available=states.count(LocalSessionState.AVAILABLE),
            sessions_leased=states.count(LocalSessionState.LEASED),
            sessions_unavailable=states.count(LocalSessionState.UNAVAILABLE),
            sessions_retiring=states.count(LocalSessionState.RETIRING),
            reserved_slots=await self._registry.reserved(agent_name),
            queue_depth=await self._queue.depth(agent_name),
            queue_max_depth=cfg.queue.max_depth if cfg.queue.enabled else 0,
            telemetry_enabled=cfg.telemetry.enabled,
        )

    async def list_sessions(self, agent_name: str) -> list[SessionRecord]:
        self.agent_config(agent_name)
        return await self._registry.list(agent_name)

    async def get_session_record(self, agent_name: str, session_id: str) -> SessionRecord:
        self.agent_config(agent_name)
        record = await self._registry.get(agent_name, session_id)
        if record is None:
            raise SessionNotFoundAdminError("No session with that identifier is registered.")
        return record

    async def admin_delete(self, agent_name: str, session_id: str) -> bool:
        """Delete a session. Returns False when deletion is deferred until the lease ends."""
        self.agent_config(agent_name)
        record = await self._registry.get(agent_name, session_id)
        if record is None:
            raise SessionNotFoundAdminError("No such session for this agent.")
        outcome = await self.retire_session(agent_name, session_id, "admin", delete_remote=True)
        if outcome is False:
            raise UpstreamError("Foundry could not delete the session; it will be retried.")
        return outcome is True

    async def retire_session(
        self, agent_name: str, session_id: str, reason: str, *, delete_remote: bool
    ) -> bool | None:
        """Take a session out of rotation.

        The session is marked for deletion and the session controller removes it, holding the
        cleanup finalizer until Foundry confirms the delete. Returns True when it is removed,
        False when the remote delete failed (the record stays and is retried), and None when a
        lease holder defers the removal until the lease ends.
        """
        cfg = self.agent_config(agent_name)
        record = await self._registry.mark_retiring(
            cfg.name, session_id, cleanup=delete_remote, reason=reason
        )
        if record is None:
            return True
        if record.lease_request_id is not None:
            return None
        await self._plane.manager.reconcile_now("session", session_key(cfg.name, session_id))
        return await self._registry.get(cfg.name, session_id) is None

    async def provision_warm(self, agent_name: str) -> bool:
        """Create one unbound AVAILABLE session if capacity allows."""
        cfg = self.agent_config(agent_name)
        token = _new_token()
        async with self._locks[agent_name]:
            if await self._gate.reserve_create(cfg, token, self._clock.now()) is not None:
                return False
        try:
            await self._remote.create_ready_session(cfg, None, _WarmHooks(self, cfg, token))
        except BaseException:
            with anyio.CancelScope(shield=True):
                async with self._locks[agent_name]:
                    await self._registry.release_slot(agent_name, token)
            raise
        with anyio.CancelScope(shield=True):
            async with self._locks[agent_name]:
                await self._dispatch_locked(cfg)
        return True

    # ------------------------------------------------------------- acquisition

    async def _schedule(
        self, cfg: AgentConfig, request_id: str, user_id: str, conversation_key: str | None = None
    ) -> Decision:
        """Run one scheduling cycle. The agent lock must be held."""
        key = (
            SessionAffinityKey(
                user_id=user_id, agent_name=cfg.name, conversation_key=conversation_key
            )
            if cfg.stateful
            else None
        )
        return await self._framework.schedule(
            SchedulingRequest(
                agent=cfg,
                request_id=request_id,
                affinity_key=key,
                now=self._clock.now(),
                derived_ids=self._session_ids is not None and cfg.stateful,
            )
        )

    async def _dispatch_locked(self, cfg: AgentConfig) -> None:
        for ticket in await self._queue.pending(cfg.name):
            if ticket.future.done():
                continue  # the waiter is abandoning; it removes itself
            decision = await self._schedule(
                cfg, ticket.request_id, ticket.user_id or "", ticket.conversation_key
            )
            if isinstance(decision, Wait):
                ticket.pinned = decision.pinned
                continue
            await self._queue.remove(cfg.name, ticket.request_id)
            ticket.future.set_result(decision)
        await self._refresh_gauges(cfg)

    async def _acquire_grant(self, cfg: AgentConfig, req: PoolRequest) -> tuple[Grant, float]:
        outcome = await self._decide_or_enqueue(cfg, req)
        if isinstance(outcome, QueueTicket):
            started = self._clock.monotonic()
            grant = await self._wait_for_grant(cfg, outcome)
            waited = self._clock.monotonic() - started
            self._metrics.queue_wait(cfg.name, waited)
            return grant, waited
        return outcome, 0.0

    async def _decide_or_enqueue(self, cfg: AgentConfig, req: PoolRequest) -> Grant | QueueTicket:
        async with self._locks[cfg.name]:
            decision = await self._schedule(cfg, req.request_id, req.user_id, req.conversation_key)
            result: Grant | QueueTicket
            if isinstance(decision, Wait):
                result = await self._enqueue(cfg, req, decision)
            else:
                result = decision
            await self._refresh_gauges(cfg)
        return result

    async def _enqueue(self, cfg: AgentConfig, req: PoolRequest, wait: Wait) -> QueueTicket:
        retry_after = self._settings.retry_after_seconds
        if not cfg.queue.enabled:
            raise PoolCapacityExceededError(
                "All sessions are busy and queueing is disabled.", retry_after_seconds=retry_after
            )
        ticket = QueueTicket(
            request_id=req.request_id,
            agent_name=cfg.name,
            user_id=req.user_id,
            conversation_key=req.conversation_key,
            enqueued_at=self._clock.now(),
            pinned=wait.pinned,
            future=asyncio.get_running_loop().create_future(),
        )
        try:
            await self._queue.enqueue(ticket, cfg.queue.max_depth)
        except QueueFullError as exc:
            raise QueueFullError(exc.detail, retry_after_seconds=retry_after) from exc
        return ticket

    async def _wait_for_grant(self, cfg: AgentConfig, ticket: QueueTicket) -> Grant:
        try:
            done, _ = await asyncio.wait({ticket.future}, timeout=cfg.queue.max_wait_seconds)
            if not done:
                if ticket.pinned:
                    raise StickySessionTimeoutError(
                        "The session for this user stayed busy for too long.",
                        retry_after_seconds=self._settings.retry_after_seconds,
                    )
                raise QueueWaitTimeoutError(
                    "No session became available in time.",
                    retry_after_seconds=self._settings.retry_after_seconds,
                )
            return ticket.future.result()
        except BaseException:
            with anyio.CancelScope(shield=True):
                await self._abandon(cfg, ticket)
            raise

    async def _abandon(self, cfg: AgentConfig, ticket: QueueTicket) -> None:
        """Remove a waiter that timed out or was cancelled, returning any grant it never used."""
        async with self._locks[cfg.name]:
            await self._queue.remove(cfg.name, ticket.request_id)
            if ticket.future.done():
                await self._return_grant_locked(cfg, ticket.future.result(), ticket.request_id)
            else:
                ticket.future.cancel()
            await self._dispatch_locked(cfg)

    async def _return_grant_locked(self, cfg: AgentConfig, grant: Grant, request_id: str) -> None:
        await self._framework.unreserve(cfg, grant, request_id, self._clock.now())

    async def _materialise(self, cfg: AgentConfig, req: PoolRequest, grant: Grant) -> Lease:
        if isinstance(grant, SessionGrant):
            self._metrics.session_reused(cfg.name)
            key = SessionAffinityKey(
                user_id=req.user_id, agent_name=cfg.name, conversation_key=req.conversation_key
            )
            return Lease(cfg.name, grant.session_id, req.request_id, key if cfg.stateful else None)
        return await self._create_for_grant(cfg, req, grant)

    async def _create_for_grant(
        self, cfg: AgentConfig, req: PoolRequest, grant: CreateGrant
    ) -> Lease:
        try:
            session, _ = await self._remote.create_ready_session(
                cfg, grant.affinity_key, _RequestHooks(self, cfg, req, grant)
            )
        except BaseException:
            with anyio.CancelScope(shield=True):
                async with self._locks[cfg.name]:
                    await self._return_grant_locked(cfg, grant, req.request_id)
                    await self._dispatch_locked(cfg)
            raise
        with anyio.CancelScope(shield=True):
            async with self._locks[cfg.name]:
                await self._refresh_gauges(cfg)
        return Lease(cfg.name, session.session_id, req.request_id, grant.affinity_key)

    async def _register(
        self,
        cfg: AgentConfig,
        session: FoundrySession,
        *,
        lease_to: str | None,
        key: SessionAffinityKey | None,
        local_state: LocalSessionState,
        provisioning: bool,
    ) -> None:
        """Record a session at the moment Foundry has it. Its lifecycle is owned from here on."""
        now = self._clock.now()
        leased = lease_to is not None
        record = SessionRecord(
            session_id=session.session_id,
            agent_name=cfg.name,
            agent_version=session.agent_version,
            platform_status=session.status,
            local_state=local_state,
            affinity_key=key,
            created_at=session.created_at,
            last_seen_at=now,
            last_accessed_at=session.last_accessed_at,
            expires_at=session.expires_at,
            lease_request_id=lease_to,
            lease_acquired_at=now if leased else None,
            provisioning_started_at=now if provisioning else None,
        )
        try:
            await self._registry.add(record)
        except ValueError:
            # The reconciler registered this session while we were claiming it.
            # This request holds the lease, so its record replaces the adopted one.
            await self._registry.remove(cfg.name, session.session_id)
            await self._registry.add(record)
        if key is not None:
            await self._affinity.bind(key, session.session_id)

    async def _mark_ready(self, cfg: AgentConfig, session: FoundrySession, *, free: bool) -> None:
        def apply(record: SessionRecord) -> None:
            record.platform_status = session.status
            record.agent_version = session.agent_version
            record.last_accessed_at = session.last_accessed_at
            record.expires_at = session.expires_at
            record.provisioning_started_at = None
            if free and record.local_state is LocalSessionState.UNAVAILABLE:
                record.local_state = LocalSessionState.AVAILABLE

        await mutate_session(self._registry, cfg.name, session.session_id, apply)

    # -------------------------------------------------------------- invocation

    async def _invoke_with_recovery(self, run: _Run) -> UpstreamResponse:
        cfg, req = run.cfg, run.req
        replaced = False
        throttled = 0
        while True:
            context = InvokeContext(
                agent_name=cfg.name,
                session_id=run.lease.session_id,
                payload=req.payload,
                raw_body=req.raw_body,
                content_type=req.content_type,
                timeout_seconds=req.timeout_seconds,
                request_id=req.request_id,
                correlation_id=req.correlation_id,
                stream=req.stream,
                protocol=cfg.protocol,
                **self._user_headers(cfg, req),
            )
            try:
                return await self._adapter.invoke(context)
            except FoundryThrottled as exc:
                if throttled >= self._settings.upstream_throttle_retries:
                    raise self._to_app_error(exc, invoking=True) from exc
                throttled += 1
                if not await self._remote.sleep_backoff(throttled, exc.retry_after_seconds):
                    raise self._to_app_error(exc, invoking=True) from exc
            except FoundryQuotaExceeded as exc:
                # Resuming an idle session needs quota. Nothing ran, so a regional retry is safe.
                self._remote.note_quota(cfg.name, exc)
                if (
                    exc.scope != QUOTA_REGIONAL
                    or throttled >= self._settings.upstream_throttle_retries
                ):
                    raise self._to_app_error(exc, invoking=True) from exc
                throttled += 1
                if not await self._remote.sleep_backoff(throttled, exc.retry_after_seconds):
                    raise self._to_app_error(exc, invoking=True) from exc
            except FoundrySessionNotFound as exc:
                # The session never ran the request, so a replacement retry is safe.
                grant = await self._discard(
                    run, "not_found", delete_remote=False, replace=not replaced
                )
                if grant is None:
                    raise self._to_app_error(exc, invoking=True) from exc
                replaced = True
                run.lease = await self._create_for_grant(cfg, req, grant)
            except FoundrySessionFailed as exc:
                # A failed run may have partly executed: retry only with an idempotency key.
                retry = not replaced and req.idempotency_key is not None
                grant = await self._discard(run, "failed", delete_remote=True, replace=retry)
                if grant is None:
                    raise self._to_app_error(exc, invoking=True) from exc
                replaced = True
                run.lease = await self._create_for_grant(cfg, req, grant)
            except FoundryError as exc:
                raise self._to_app_error(exc, invoking=True) from exc

    async def _discard(
        self, run: _Run, reason: str, *, delete_remote: bool, replace: bool
    ) -> CreateGrant | None:
        """Drop the leased session. Optionally reserve capacity for a replacement.

        The session is marked for deletion and its lease released, so the session controller can
        remove it. The remote delete runs now, and if it fails the record stays for a retry.
        """
        cfg, lease = run.cfg, run.lease
        with anyio.CancelScope(shield=True):
            if reason == "failed":
                self._metrics.session_failed(cfg.name)
                log_event(
                    logger,
                    "session_failed",
                    level=logging.WARNING,
                    agent_name=cfg.name,
                    session_id_hash=hash_identifier(lease.session_id),
                    platform_status="failed",
                    action="delete",
                )
            lease.released = True
            grant: CreateGrant | None = None
            async with self._locks[cfg.name]:
                await self._registry.mark_retiring(
                    cfg.name, lease.session_id, cleanup=delete_remote, reason=reason
                )
                await self._registry.release(
                    cfg.name, lease.session_id, lease.request_id, self._clock.now()
                )
                await self._affinity.remove_by_session(cfg.name, lease.session_id)
                if replace:
                    token = _new_token()
                    # A replacement takes the place of the session just given up.
                    await self._registry.reserve_slot(cfg.name, token, cfg.max_sessions, force=True)
                    if lease.affinity_key is not None:
                        await self._affinity.reserve(lease.affinity_key, run.req.request_id)
                    grant = CreateGrant(token=token, affinity_key=lease.affinity_key)
            # Removing the record wakes the waiters, so no separate dispatch is needed here.
            await self._plane.manager.reconcile_now(
                "session", session_key(cfg.name, lease.session_id)
            )
        return grant

    async def _open_stream(
        self, run: _Run, upstream: AsyncIterator[bytes], started: float
    ) -> AsyncGenerator[bytes]:
        """Read the first upstream frame, then hand back a generator that relays the rest.

        A failure before the first frame is raised here, so the caller gets a normal problem
        response instead of a 200 whose only content is an error event.
        """
        iterator = upstream.__aiter__()
        deadline = self._clock.monotonic() + self._settings.max_stream_seconds
        try:
            first = await self._next_frame(iterator, deadline)
        except BaseException as exc:
            with anyio.CancelScope(shield=True):
                await self._close_upstream(upstream)
                if isinstance(exc, FoundrySessionFailed):
                    await self._discard(run, "failed", delete_remote=True, replace=False)
            if isinstance(exc, TimeoutError):
                raise FoundryTimeoutAppError() from exc
            if isinstance(exc, FoundryError):
                raise self._to_app_error(exc, invoking=True) from exc
            raise
        stream = self._guard_stream(run, upstream, iterator, started, deadline, first)
        await anext(stream)  # start the generator so its cleanup always runs
        return stream

    async def _next_frame(self, iterator: AsyncIterator[bytes], deadline: float) -> bytes | None:
        remaining = deadline - self._clock.monotonic()
        if remaining <= 0:
            raise TimeoutError
        try:
            return await asyncio.wait_for(iterator.__anext__(), timeout=remaining)
        except StopAsyncIteration:
            return None

    async def _close_upstream(self, upstream: AsyncIterator[bytes]) -> None:
        """Close the upstream stream. A failing close is logged and never propagates."""
        aclose = getattr(upstream, "aclose", None)
        if aclose is None:
            return
        try:
            await aclose()
        except Exception:
            logger.warning("upstream_stream_close_failed", exc_info=True)

    def _error_frame(self, run: _Run, error: AppError, *, code: str | None = None) -> bytes:
        data: dict[str, Any] = {
            "error_code": code or error.code,
            "title": error.title,
            "detail": error.detail,
            "correlation_id": run.req.correlation_id,
            "request_id": run.req.request_id,
            "phase": "stream",
            "retry_safe": False,  # the agent had already started work
        }
        if error.retry_after_seconds is not None:
            data["retry_after_seconds"] = error.retry_after_seconds
        return sse_frame("error", data)

    async def _guard_stream(
        self,
        run: _Run,
        upstream: AsyncIterator[bytes],
        iterator: AsyncIterator[bytes],
        started: float,
        deadline: float,
        first: bytes | None,
    ) -> AsyncGenerator[bytes]:
        """Relay frames, enforce the stream time limit and always release the lease."""
        cfg = run.cfg
        outcome = "ok"
        failed = False
        try:
            yield b""  # started marker, inside the try so an early close still releases the lease
            frame = first
            while frame is not None:
                yield frame
                frame = await self._next_frame(iterator, deadline)
        except TimeoutError:
            outcome = "STREAM_TIMEOUT"
            yield self._error_frame(
                run, StreamTimeoutError("Stream time limit reached."), code=outcome
            )
        except FoundryError as exc:
            failed = isinstance(exc, FoundrySessionFailed)
            error = self._to_app_error(exc, invoking=True)
            outcome = error.code
            yield self._error_frame(run, error)
        except BaseException:
            outcome = "CLIENT_CANCELLED"
            raise
        finally:
            with anyio.CancelScope(shield=True):
                # Releasing the lease must happen whatever else fails during cleanup.
                try:
                    await self._close_upstream(upstream)
                    duration = self._clock.monotonic() - started
                    self._metrics.request_completed(cfg.name, outcome, duration)
                finally:
                    try:
                        if failed:
                            await self._discard(run, "failed", delete_remote=True, replace=False)
                        else:
                            await self._release(cfg, run.lease)
                    finally:
                        self._request_finished()

    # ----------------------------------------------------------------- release

    async def _release(self, cfg: AgentConfig, lease: Lease) -> None:
        if lease.released:
            return
        lease.released = True
        with anyio.CancelScope(shield=True):
            async with self._locks[cfg.name]:
                record = await self._registry.release(
                    cfg.name, lease.session_id, lease.request_id, self._clock.now()
                )
                deleting = record is not None and record.deletion_timestamp is not None
                await self._dispatch_locked(cfg)
            if deleting:
                # Deletion was requested while the session was in use. Now it can proceed.
                await self._plane.manager.reconcile_now(
                    "session", session_key(cfg.name, lease.session_id)
                )

    # --------------------------------------------------------------- utilities

    async def _refresh_gauges(self, cfg: AgentConfig) -> None:
        records = await self._registry.list(cfg.name)
        counts: dict[tuple[str, str], int] = {}
        for record in records:
            key = (record.platform_status.value, record.local_state.value)
            counts[key] = counts.get(key, 0) + 1
        self._metrics.sessions(cfg.name, counts)
        now = self._clock.now()
        self._metrics.sessions_counted(
            cfg.name, sum(1 for r in records if self._gate.counted(r, cfg, now))
        )
        self._metrics.queue_depth(cfg.name, await self._queue.depth(cfg.name))

    def _user_headers(self, cfg: AgentConfig, req: PoolRequest) -> dict[str, str]:
        """Per-user key (and acting user) for the agent's ``user_isolation`` mode."""
        if cfg.user_isolation is UserIsolation.OFF or self._isolation is None:
            return {}
        headers = {"isolation_key": self._isolation.key_for(req.user_id)}
        if cfg.user_isolation is UserIsolation.DELEGATED:
            headers["acting_user"] = req.user_id
        return headers

    def _to_app_error(self, exc: FoundryError, *, invoking: bool) -> AppError:
        return to_app_error(self._settings, exc, invoking=invoking)


class _RequestHooks:
    """Creation bookkeeping for a request that waits for its own new session."""

    def __init__(
        self, pool: PoolService, cfg: AgentConfig, req: PoolRequest, grant: CreateGrant
    ) -> None:
        self._pool = pool
        self._cfg = cfg
        self._req = req
        self._grant = grant

    async def created(self, session: FoundrySession, *, owned: bool, restored: bool) -> None:
        pool, cfg = self._pool, self._cfg
        with anyio.CancelScope(shield=True):
            async with pool._locks[cfg.name]:
                await pool._register(
                    cfg,
                    session,
                    lease_to=self._req.request_id,
                    key=self._grant.affinity_key,
                    local_state=LocalSessionState.LEASED,
                    provisioning=owned,
                )
                # The record now counts toward capacity, so the reservation is no longer needed.
                await pool._registry.release_slot(cfg.name, self._grant.token)
                if not restored:
                    pool._metrics.session_created(cfg.name)
        log_event(
            logger,
            "session_restored" if restored else "session_created",
            agent_name=cfg.name,
            session_id_hash=hash_identifier(session.session_id),
            platform_status=session.status.value,
        )
        pool._plane.events.session(
            cfg.name,
            session.session_id,
            EventType.NORMAL,
            "Restored" if restored else "Created",
            f"Foundry reports {session.status.value}.",
        )

    async def ready(self, session: FoundrySession) -> None:
        with anyio.CancelScope(shield=True):
            await self._pool._mark_ready(self._cfg, session, free=False)

    async def failed(self, session: FoundrySession, *, delete: bool) -> None:
        pool, cfg, grant = self._pool, self._cfg, self._grant
        with anyio.CancelScope(shield=True):
            async with pool._locks[cfg.name]:
                if delete:
                    await pool._registry.mark_retiring(
                        cfg.name, session.session_id, cleanup=True, reason="create_failed"
                    )
                await pool._registry.release(
                    cfg.name, session.session_id, self._req.request_id, pool._clock.now()
                )
                await pool._affinity.remove_by_session(cfg.name, session.session_id)
                # The request may try again, so it keeps its place until it gives up for good.
                await pool._registry.reserve_slot(
                    cfg.name, grant.token, cfg.max_sessions, force=True
                )
                if grant.affinity_key is not None:
                    await pool._affinity.reserve(grant.affinity_key, self._req.request_id)
            pool._plane.events.session(
                cfg.name,
                session.session_id,
                EventType.WARNING,
                "ProvisioningFailed",
                "The session did not become usable." + (" Deleting it." if delete else ""),
            )
            if delete:
                await pool._plane.manager.reconcile_now(
                    "session", session_key(cfg.name, session.session_id)
                )


class _WarmHooks:
    """Creation bookkeeping for a warm session that no request is waiting for."""

    def __init__(self, pool: PoolService, cfg: AgentConfig, token: str) -> None:
        self._pool = pool
        self._cfg = cfg
        self._token = token

    async def created(self, session: FoundrySession, *, owned: bool, restored: bool) -> None:
        pool, cfg = self._pool, self._cfg
        with anyio.CancelScope(shield=True):
            async with pool._locks[cfg.name]:
                await pool._register(
                    cfg,
                    session,
                    lease_to=None,
                    key=None,
                    local_state=LocalSessionState.UNAVAILABLE,  # not schedulable until ready
                    provisioning=True,
                )
                await pool._registry.release_slot(cfg.name, self._token)
                pool._metrics.session_created(cfg.name)
        log_event(
            logger,
            "session_created",
            agent_name=cfg.name,
            session_id_hash=hash_identifier(session.session_id),
            platform_status=session.status.value,
        )
        pool._plane.events.session(
            cfg.name, session.session_id, EventType.NORMAL, "Created", "Warm session created."
        )

    async def ready(self, session: FoundrySession) -> None:
        with anyio.CancelScope(shield=True):
            await self._pool._mark_ready(self._cfg, session, free=True)

    async def failed(self, session: FoundrySession, *, delete: bool) -> None:
        pool, cfg = self._pool, self._cfg
        with anyio.CancelScope(shield=True):
            async with pool._locks[cfg.name]:
                if delete:
                    await pool._registry.mark_retiring(
                        cfg.name, session.session_id, cleanup=True, reason="create_failed"
                    )
                await pool._registry.reserve_slot(
                    cfg.name, self._token, cfg.max_sessions, force=True
                )
            pool._plane.events.session(
                cfg.name,
                session.session_id,
                EventType.WARNING,
                "ProvisioningFailed",
                "The warm session did not become usable. Deleting it.",
            )
            if delete:
                await pool._plane.manager.reconcile_now(
                    "session", session_key(cfg.name, session.session_id)
                )


def _holder_for(config: ConfigSource) -> ConfigHolder:
    if isinstance(config, AgentPoolConfig):
        return ConfigHolder(config)
    raise TypeError("config must be an AgentPoolConfig or a ConfigHolder")


def _new_token() -> str:
    return f"slot_{uuid.uuid4().hex}"
