"""The garbage-collection controller: finds what other controllers and requests left behind.

- Sessions marked for deletion that still exist are handed back to the session controller, so a
  delete that failed is retried at every sweep.
- A session that has been provisioning for too long with nobody waiting on it is given up.
- Affinity entries that point at a session that is gone or going are removed.
- Capacity reservations nobody released are expired.
"""

from __future__ import annotations

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.controllers.plane import ControlPlane
from hosted_agent_kit.controllers.reports import ReconcileReport
from hosted_agent_kit.controllers.runtime import ReconcileResult
from hosted_agent_kit.domain.resources import (
    ResourceKey,
    ResourceKind,
    session_key,
)
from hosted_agent_kit.ports.affinity import AffinityStore
from hosted_agent_kit.ports.registry import ChangeEvent, SessionRegistry
from hosted_agent_kit.services.clock import Clock
from hosted_agent_kit.services.events import EventType


class GarbageCollectionController:
    name = "gc"
    workers = 1

    def __init__(
        self,
        *,
        config: ConfigSource,
        registry: SessionRegistry,
        affinity: AffinityStore,
        plane: ControlPlane,
        clock: Clock,
        settings: KitSettings,
    ) -> None:
        self._config = config
        self._registry = registry
        self._affinity = affinity
        self._plane = plane
        self._clock = clock
        self._settings = settings

    def map_event(self, event: ChangeEvent) -> list[ResourceKey]:
        return []  # a sweep runs with each resync

    async def reconcile(self, key: ResourceKey) -> ReconcileResult:
        cfg = self._config.get(key.agent_name)
        if key.kind is not ResourceKind.AGENT_POOL or cfg is None:
            return ReconcileResult.done()
        report = self._plane.report(cfg.name) or ReconcileReport(agent_name=cfg.name)
        records = await self._registry.list(cfg.name)
        live = {r.session_id for r in records if r.deletion_timestamp is None}
        now = self._clock.now()
        give_up_after = self._settings.create_ready_timeout_seconds * 2
        for record in records:
            if record.deletion_timestamp is not None:
                if record.lease_request_id is None and record.session_id not in report.attempted:
                    self._plane.manager.enqueue("session", session_key(cfg.name, record.session_id))
                continue
            stuck = (
                record.provisioning_started_at is not None
                and record.lease_request_id is None
                and (now - record.provisioning_started_at).total_seconds() > give_up_after
            )
            if stuck:
                await self._registry.mark_retiring(
                    cfg.name, record.session_id, cleanup=True, reason="stuck_provisioning"
                )
                self._plane.events.session(
                    cfg.name,
                    record.session_id,
                    EventType.WARNING,
                    "ProvisioningStuck",
                    "Never became usable and nothing is waiting for it. Deleting it.",
                )
        for affinity_key, entry in await self._affinity.entries(cfg.name):
            if entry.session_id is not None and entry.session_id not in live:
                await self._affinity.remove(affinity_key)
        expired = await self._registry.release_expired_slots(
            cfg.name, self._settings.reservation_ttl_seconds
        )
        if expired:
            self._plane.events.pool(
                cfg.name,
                EventType.WARNING,
                "ReservationsExpired",
                f"Released {expired} capacity reservation(s) that were never used.",
            )
        return ReconcileResult.done()
