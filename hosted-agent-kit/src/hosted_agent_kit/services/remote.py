"""Remote session operations against Foundry: create and wait, poll, delete, backoff.

This holds no pool state. Callers pass ``CreationHooks`` so the bookkeeping (recording the session,
cleaning up after a failure) happens at the moments that matter: the instant Foundry has created a
session it is recorded, so from then on something owns it even if the caller is cancelled.
"""

from __future__ import annotations

import logging
import random
from collections.abc import Callable
from typing import Protocol

from hosted_agent_kit.config.models import AgentConfig
from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.domain.enums import FoundrySessionStatus
from hosted_agent_kit.domain.errors import (
    QUOTA_REGIONAL,
    FoundryConflict,
    FoundryError,
    FoundryQuotaExceeded,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryUnavailable,
)
from hosted_agent_kit.domain.models import FoundrySession, SessionAffinityKey
from hosted_agent_kit.logging_config import hash_identifier, log_event
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.ports.metrics import MetricsRecorder
from hosted_agent_kit.services.clock import Clock
from hosted_agent_kit.services.errors_map import to_app_error
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from hosted_agent_kit.services.sharding import fresh_session_id

logger = logging.getLogger(__name__)

TRANSIENT = (FoundryUnavailable, FoundryThrottled, FoundryTimeout)
USABLE = (
    FoundrySessionStatus.ACTIVE,
    FoundrySessionStatus.IDLE,
    FoundrySessionStatus.CREATING,
    FoundrySessionStatus.UPDATING,
)
_READY = (FoundrySessionStatus.ACTIVE, FoundrySessionStatus.IDLE)
_STARTING = (FoundrySessionStatus.CREATING, FoundrySessionStatus.UPDATING)


def retry_after_of(exc: Exception) -> float | None:
    if isinstance(exc, FoundryThrottled | FoundryQuotaExceeded):
        return exc.retry_after_seconds
    return None


class CreationHooks(Protocol):
    """Bookkeeping at the points of a creation."""

    async def created(self, session: FoundrySession, *, owned: bool, restored: bool) -> None:
        """Foundry has the session. Record it now. ``owned`` is False for one found, not created."""
        ...

    async def ready(self, session: FoundrySession) -> None:
        """The session is usable."""
        ...

    async def failed(self, session: FoundrySession, *, delete: bool) -> None:
        """The session did not become usable. ``delete`` says whether it should be removed."""
        ...


class RemoteSessions:
    def __init__(
        self,
        *,
        adapter: FoundryAdapter,
        settings: KitSettings,
        clock: Clock,
        metrics: MetricsRecorder,
        session_ids: SessionIdDeriver | None = None,
        rng: random.Random | None = None,
        on_quota_exceeded: Callable[[str, str], None] | None = None,
    ) -> None:
        self._on_quota_exceeded = on_quota_exceeded
        self._adapter = adapter
        self._settings = settings
        self._clock = clock
        self._metrics = metrics
        self._session_ids = session_ids
        self._rng = rng or random.Random()  # noqa: S311  # nosec B311 - jitter, not security

    def note_quota(self, agent_name: str, exc: FoundryQuotaExceeded) -> None:
        """Tell the quota governor that Foundry refused a session because a quota is full."""
        if self._on_quota_exceeded is not None:
            self._on_quota_exceeded(agent_name, exc.scope)

    # ------------------------------------------------------------------ backoff

    async def sleep_backoff(
        self, attempt: int, retry_after: float | None, *, jitter: bool = True
    ) -> bool:
        """Wait before a retry. Returns False, without waiting, when the retry is not worth it.

        An upstream ``Retry-After`` is a minimum, never shortened. When it asks for longer than
        ``backoff_max_seconds`` the caller gets the upstream value instead of a retry that
        would arrive too early.
        """
        s = self._settings
        delay = min(s.backoff_max_seconds, s.backoff_base_seconds * 2 ** (attempt - 1))
        if jitter:
            delay *= self._rng.uniform(0.5, 1.0)
        if retry_after is not None:
            if retry_after > s.backoff_max_seconds:
                return False
            delay = max(delay, retry_after)
        await self._clock.sleep(delay)
        return True

    # ------------------------------------------------------------------ creation

    def derived_id(self, cfg: AgentConfig, key: SessionAffinityKey | None) -> str | None:
        if self._session_ids is None or key is None or not cfg.stateful:
            return None
        return self._session_ids.for_user(cfg.name, key.user_id, key.conversation_key)

    def fresh_id(self) -> str | None:
        """A caller-chosen id carrying the shard, for a kit that owns one shard of its agents."""
        shard = self._settings.shard
        return fresh_session_id(shard.index) if shard is not None else None

    async def lookup(self, cfg: AgentConfig, session_id: str) -> FoundrySession | None:
        try:
            return await self._adapter.get_session(cfg.name, session_id)
        except FoundrySessionNotFound:
            return None

    async def create_ready_session(
        self, cfg: AgentConfig, key: SessionAffinityKey | None, hooks: CreationHooks
    ) -> tuple[FoundrySession, bool]:
        """Create a session (or find the user's derived one) and wait until it is usable."""
        desired = self.derived_id(cfg, key)
        last: FoundryError = FoundryUnavailable("creation not attempted")
        for attempt in range(cfg.create_retries + 1):
            try:
                restored = False
                session: FoundrySession | None = None
                if desired is not None:
                    found = await self.lookup(cfg, desired)
                    if found is not None:
                        if found.status in USABLE:
                            self._metrics.session_restored(cfg.name)
                            session, restored = found, True
                        else:
                            if found.status is FoundrySessionStatus.FAILED:
                                await self.delete(cfg, desired, "restore_failed")
                            desired = None  # a dead session holds the id, which may not be reusable
                if session is None:
                    try:
                        session = await self._adapter.create_session(
                            cfg.name, desired or self.fresh_id(), cfg.agent_version
                        )
                    except FoundryConflict:
                        if desired is None:
                            raise
                        log_event(logger, "session_id_conflict", agent_name=cfg.name)
                        desired = None
                        session = await self._adapter.create_session(
                            cfg.name, self.fresh_id(), cfg.agent_version
                        )
                # From here the session is recorded, so it cannot be forgotten.
                await hooks.created(session, owned=not restored, restored=restored)
                ready = await self.await_ready(cfg, session, hooks, owned=not restored)
                await hooks.ready(ready)
                return ready, restored
            except (*TRANSIENT, FoundrySessionFailed) as exc:
                last = exc
                if attempt < cfg.create_retries and not await self.sleep_backoff(
                    attempt + 1, retry_after_of(exc)
                ):
                    raise to_app_error(self._settings, exc, invoking=False) from exc
            except FoundryQuotaExceeded as exc:
                self.note_quota(cfg.name, exc)
                last = exc
                regional_retry = exc.scope == QUOTA_REGIONAL and attempt < cfg.create_retries
                if not (
                    regional_retry and await self.sleep_backoff(attempt + 1, retry_after_of(exc))
                ):
                    raise to_app_error(self._settings, exc, invoking=False) from exc
            except FoundryError as exc:
                raise to_app_error(self._settings, exc, invoking=False) from exc
        raise to_app_error(self._settings, last, invoking=False) from last

    async def await_ready(
        self, cfg: AgentConfig, session: FoundrySession, hooks: CreationHooks, *, owned: bool
    ) -> FoundrySession:
        """Poll until the session is usable.

        On any failure, timeout or cancellation the ``failed`` hook runs. ``owned`` means this
        pool created the session, so it is deleted. A session that was only found belongs to its
        user and is deleted only when Foundry reports it failed.
        """
        deadline = self._clock.monotonic() + self._settings.create_ready_timeout_seconds
        polls = 0
        try:
            while session.status not in _READY:
                if session.status not in _STARTING:
                    raise FoundrySessionFailed()
                if self._clock.monotonic() >= deadline:
                    raise FoundryTimeout()
                polls += 1
                await self.sleep_backoff(polls, None, jitter=False)
                session = await self._adapter.get_session(cfg.name, session.session_id)
        except BaseException as exc:
            await hooks.failed(session, delete=owned or isinstance(exc, FoundrySessionFailed))
            raise
        return session

    # ------------------------------------------------------------------ deletion

    async def delete(self, cfg: AgentConfig, session_id: str, reason: str) -> bool:
        """Delete a remote session, retrying transient failures. True when it is gone."""
        retries = self._settings.delete_retries
        outcome = "failure"
        attempt = 0
        while True:
            try:
                await self._adapter.delete_session(cfg.name, session_id)
            except FoundrySessionNotFound:
                pass  # already gone: converged
            except TRANSIENT as exc:
                if attempt < retries:
                    attempt += 1
                    if await self.sleep_backoff(attempt, retry_after_of(exc)):
                        continue
                break
            except FoundryError:
                break
            outcome = "success"
            break
        self._metrics.session_deleted(cfg.name, reason, outcome)
        log_event(
            logger,
            "session_delete",
            level=logging.INFO if outcome == "success" else logging.WARNING,
            agent_name=cfg.name,
            session_id_hash=hash_identifier(session_id),
            action="delete",
            reason=reason,
            outcome=outcome,
        )
        return outcome == "success"
