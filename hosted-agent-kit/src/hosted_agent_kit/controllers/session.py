"""The session controller: guarantees that a session's remote resource is cleaned up.

A session marked for deletion keeps the cleanup finalizer until Foundry confirms the remote session
is gone (or reports it not found). Only then is the local record removed. A failed delete leaves
the record, with a ``DeletionFailed`` condition, and is retried with backoff and by every sync, so
a remote session is never forgotten.
"""

from __future__ import annotations

import asyncio
from typing import Protocol

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.config.models import AgentConfig
from hosted_agent_kit.controllers.plane import ControlPlane
from hosted_agent_kit.controllers.runtime import ReconcileResult
from hosted_agent_kit.domain.resources import (
    FINALIZER_SESSION_CLEANUP,
    SESSION_DELETION_FAILED,
    ConditionStatus,
    ResourceKey,
    ResourceKind,
    session_key,
)
from hosted_agent_kit.ports.affinity import AffinityStore
from hosted_agent_kit.ports.registry import ChangeEvent, ChangeType, SessionRegistry
from hosted_agent_kit.services.events import EventType
from hosted_agent_kit.services.remote import RemoteSessions


class PoolAccess(Protocol):
    """What the controllers need from the request path."""

    def agent_lock(self, agent_name: str) -> asyncio.Lock: ...

    async def dispatch_locked(self, cfg: AgentConfig) -> None: ...


class SessionController:
    name = "session"
    workers = 4

    def __init__(
        self,
        *,
        config: ConfigSource,
        registry: SessionRegistry,
        affinity: AffinityStore,
        remote: RemoteSessions,
        plane: ControlPlane,
        pool: PoolAccess,
    ) -> None:
        self._config = config
        self._registry = registry
        self._affinity = affinity
        self._remote = remote
        self._plane = plane
        self._pool = pool

    def map_event(self, event: ChangeEvent) -> list[ResourceKey]:
        if (
            event.type is ChangeType.DELETED
            or event.status_only
            or event.record.deletion_timestamp is None
        ):
            return []
        return [session_key(event.key.agent_name, event.key.session_id)]

    async def reconcile(self, key: ResourceKey) -> ReconcileResult:
        if key.kind is not ResourceKind.AGENT_SESSION:
            return ReconcileResult.done()
        cfg = self._config.get(key.agent_name)
        record = await self._registry.get(key.agent_name, key.name)
        if cfg is None or record is None or record.deletion_timestamp is None:
            return ReconcileResult.done()
        if record.lease_request_id is not None:
            # The request using it finishes first. Releasing the lease is a store change, which
            # queues this key again.
            return ReconcileResult.done()
        report = self._plane.report(key.agent_name)
        reason = record.deletion_reason or "retired"
        if FINALIZER_SESSION_CLEANUP in record.finalizers:
            if report is not None:
                report.attempted.add(key.name)
            deleted = await self._remote.delete(cfg, key.name, reason)
            if not deleted:
                await self._registry.set_condition(
                    key.agent_name,
                    key.name,
                    SESSION_DELETION_FAILED,
                    ConditionStatus.TRUE,
                    "RemoteDeleteFailed",
                    "Foundry did not confirm the delete. It will be retried.",
                )
                self._plane.events.session(
                    key.agent_name,
                    key.name,
                    EventType.WARNING,
                    "DeleteFailed",
                    "The remote session could not be deleted. Retrying.",
                )
                return ReconcileResult.retry()
            record = await self._registry.remove_finalizer(
                key.agent_name, key.name, FINALIZER_SESSION_CLEANUP
            )
        if record is not None and record.finalizers:
            # Another party still has a say, for example Foundry is still deleting the session.
            return ReconcileResult.done()
        async with self._pool.agent_lock(cfg.name):
            await self._registry.remove(cfg.name, key.name)
            await self._affinity.remove_by_session(cfg.name, key.name)
            await self._pool.dispatch_locked(cfg)
        if report is not None:
            report.removed += 1
            if reason == "version_drain":
                report.drained += 1
        self._plane.events.session(
            cfg.name, key.name, EventType.NORMAL, "Deleted", f"Removed ({reason})."
        )
        return ReconcileResult.done()
