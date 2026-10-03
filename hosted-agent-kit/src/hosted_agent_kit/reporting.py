"""Read-only reporting: pools, sessions, events, metrics and health.

``Reporting`` never changes anything. User ids and conversation keys are hashed unless the
caller asks for them with ``reveal_identities=True``.
"""

from __future__ import annotations

from typing import Any

from hosted_agent_kit.domain.models import SessionRecord
from hosted_agent_kit.domain.resources import (
    SESSION_RECOVERABLE,
    Condition,
    ConditionStatus,
)
from hosted_agent_kit.logging_config import hash_identifier
from hosted_agent_kit.runtime import Runtime
from hosted_agent_kit.views import (
    AgentAdminView,
    ConditionView,
    EventView,
    PoolResourceView,
    SessionAdminView,
)


def condition_views(conditions: list[Condition]) -> list[ConditionView]:
    return [
        ConditionView(
            type=c.type,
            status=c.status.value,
            reason=c.reason,
            message=c.message,
            last_transition_time=c.last_transition_time,
        )
        for c in conditions
    ]


class Reporting:
    def __init__(self, runtime: Runtime) -> None:
        self._rt = runtime

    async def agents(self) -> list[AgentAdminView]:
        """One row per configured agent: capacity, queue, circuit and pool conditions."""
        return [await self.agent_summary(name) for name in self._rt.pool.agent_names]

    async def agent_summary(self, agent_name: str) -> AgentAdminView:
        rt = self._rt
        snapshot = await rt.pool.snapshot(agent_name)
        pool = rt.pool.plane.pools.get(agent_name)
        cfg = rt.config.agents[agent_name]
        return AgentAdminView.model_validate(
            {
                **snapshot.model_dump(),
                "generation": pool.generation,
                "observed_generation": pool.status.observed_generation,
                "conditions": condition_views(pool.status.conditions),
                "circuit_state": rt.circuits.state(agent_name),
                "agent_version": cfg.agent_version,
                "version_drain": cfg.version_drain.value,
            }
        )

    def pool(self, agent_name: str) -> PoolResourceView:
        """The pool resource: spec, status, conditions and its latest events."""
        rt = self._rt
        rt.pool.agent_config(agent_name)
        pool = rt.pool.plane.pools.get(agent_name)
        status = pool.status
        return PoolResourceView(
            agent_name=agent_name,
            generation=pool.generation,
            observed_generation=status.observed_generation,
            resource_version=pool.resource_version,
            spec=pool.spec.model_dump(mode="json"),
            ready_sessions=status.ready_sessions,
            leased_sessions=status.leased_sessions,
            provisioning_sessions=status.provisioning_sessions,
            deleting_sessions=status.deleting_sessions,
            reserved_slots=status.reserved_slots,
            queued_requests=status.queued_requests,
            conditions=condition_views(status.conditions),
            events=self.events(agent_name, limit=20),
        )

    def events(self, agent_name: str | None = None, *, limit: int = 100) -> list[EventView]:
        """What the controllers did and why, newest first. Not state: nothing reads it."""
        if agent_name is not None:
            self._rt.pool.agent_config(agent_name)
        return [
            EventView(
                agent_name=e.agent_name,
                object=e.object,
                type=e.type.value,
                reason=e.reason,
                message=e.message,
                count=e.count,
                first_seen=e.first_seen,
                last_seen=e.last_seen,
            )
            for e in self._rt.pool.plane.events.list(agent_name, limit)
        ]

    async def sessions(
        self, agent_name: str, *, reveal_identities: bool = False
    ) -> list[SessionAdminView]:
        records = await self._rt.pool.list_sessions(agent_name)
        return [self.session_view(r, reveal_identities) for r in records]

    async def session(
        self, agent_name: str, session_id: str, *, reveal_identities: bool = False
    ) -> SessionAdminView:
        record = await self._rt.pool.get_session_record(agent_name, session_id)
        return self.session_view(record, reveal_identities)

    def session_view(self, record: SessionRecord, reveal_identities: bool) -> SessionAdminView:
        user: str | None = None
        conversation: str | None = None
        if record.affinity_key is not None:
            raw = record.affinity_key.user_id
            user = raw if reveal_identities else f"u_{hash_identifier(raw)}"
            key = record.affinity_key.conversation_key
            if key is not None:
                conversation = key if reveal_identities else f"c_{hash_identifier(key)}"
        ids = self._rt.session_ids
        restorable = (
            record.affinity_key is None and ids is not None and ids.is_derived(record.session_id)
        )
        conditions = list(record.conditions)
        if restorable:
            # Computed, not stored: it depends on the service's key, not on the session.
            conditions.append(
                Condition(
                    type=SESSION_RECOVERABLE,
                    status=ConditionStatus.TRUE,
                    reason="HeldForOwner",
                    message="Found after a restart and held for the user it belongs to.",
                    last_transition_time=record.last_seen_at,
                )
            )
        return SessionAdminView(
            session_id=record.session_id,
            agent_name=record.agent_name,
            agent_version=record.agent_version,
            platform_status=record.platform_status.value,
            local_state=record.local_state.value,
            leased=record.lease_request_id is not None,
            bound_to_user=record.affinity_key is not None,
            restorable=restorable,
            user=user,
            conversation=conversation,
            generation=record.generation,
            resource_version=record.resource_version,
            finalizers=list(record.finalizers),
            deletion_timestamp=record.deletion_timestamp,
            deletion_reason=record.deletion_reason,
            conditions=condition_views(conditions),
            created_at=record.created_at,
            last_accessed_at=record.last_accessed_at,
            expires_at=record.expires_at,
            last_released_at=record.last_released_at,
        )

    def metrics(self) -> dict[str, Any]:
        """Counters, gauges and histograms as a JSON-friendly dict."""
        return self._rt.metrics_store.snapshot()

    def prometheus(self) -> str:
        """The same metrics in the Prometheus text exposition format."""
        return self._rt.metrics_store.prometheus_text()

    def health(self) -> dict[str, Any]:
        """Readiness of this process, without calling Foundry."""
        rt = self._rt
        checks = {
            "started": rt.started,
            "ready": rt.ready,
            "workers": rt.reconciler.workers_started,
            "initial_sync": rt.reconciler.initial_sync_done,
        }
        return {"status": "ready" if all(checks.values()) else "not_ready", "checks": checks}
