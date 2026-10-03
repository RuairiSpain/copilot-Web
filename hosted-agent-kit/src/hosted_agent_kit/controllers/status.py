"""The pool status controller: writes each pool's observed status and conditions.

It has no side effects on Foundry or on sessions. It counts what the store holds, works out the
capacity, recovery and readiness conditions, and records the generation of the spec it has seen,
so an operator can tell whether a configuration change has taken effect.
"""

from __future__ import annotations

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.controllers.observation import ObservationCache
from hosted_agent_kit.controllers.plane import ControlPlane
from hosted_agent_kit.controllers.runtime import ReconcileResult
from hosted_agent_kit.domain.enums import LocalSessionState
from hosted_agent_kit.domain.pools import AgentPoolStatus
from hosted_agent_kit.domain.resources import (
    POOL_CAPACITY_AVAILABLE,
    POOL_CONFIGURATION_VALID,
    POOL_FOUNDRY_REACHABLE,
    POOL_INITIAL_SYNC_COMPLETE,
    POOL_READY,
    POOL_RECOVERY_BLOCKED,
    ConditionStatus,
    ResourceKey,
    ResourceKind,
    pool_key,
)
from hosted_agent_kit.ports.queue import QueueManager
from hosted_agent_kit.ports.registry import ChangeEvent, SessionRegistry


class PoolStatusController:
    name = "status"
    workers = 1

    def __init__(
        self,
        *,
        config: ConfigSource,
        registry: SessionRegistry,
        queue: QueueManager,
        plane: ControlPlane,
        cache: ObservationCache,
        recovery_enabled: bool,
    ) -> None:
        self._config = config
        self._registry = registry
        self._queue = queue
        self._plane = plane
        self._cache = cache
        self._recovery_enabled = recovery_enabled

    def map_event(self, event: ChangeEvent) -> list[ResourceKey]:
        return [] if event.status_only else [pool_key(event.key.agent_name)]

    async def reconcile(self, key: ResourceKey) -> ReconcileResult:
        cfg = self._config.get(key.agent_name)
        if key.kind is not ResourceKind.AGENT_POOL or cfg is None:
            return ReconcileResult.done()
        agent = cfg.name
        pools = self._plane.pools
        pools.sync_spec(agent)
        records = await self._registry.list(agent)
        reserved = await self._registry.reserved(agent)
        queued = await self._queue.depth(agent)

        ready = sum(
            1
            for r in records
            if r.local_state is LocalSessionState.AVAILABLE and r.deletion_timestamp is None
        )
        leased = sum(1 for r in records if r.lease_request_id is not None)
        deleting = sum(1 for r in records if r.deletion_timestamp is not None)
        provisioning = sum(
            1
            for r in records
            if r.provisioning_started_at is not None
            or r.local_state is LocalSessionState.UNAVAILABLE
        )
        generation = pools.get(agent).generation

        def apply(status: AgentPoolStatus) -> None:
            status.observed_generation = generation
            status.ready_sessions = ready
            status.leased_sessions = leased
            status.provisioning_sessions = provisioning
            status.deleting_sessions = deleting
            status.reserved_slots = reserved
            status.queued_requests = queued

        pools.update_status(agent, apply)

        observation = self._cache.get(agent)
        synced = observation is not None and observation.completed_at is not None
        pools.set_condition(
            agent,
            POOL_INITIAL_SYNC_COMPLETE,
            ConditionStatus.TRUE if synced else ConditionStatus.FALSE,
            "Synced" if synced else "AwaitingSync",
            "" if synced else "No complete listing of Foundry yet.",
        )

        at_capacity = ready == 0 and len(records) + reserved >= cfg.max_sessions
        pools.set_condition(
            agent,
            POOL_CAPACITY_AVAILABLE,
            ConditionStatus.FALSE if at_capacity else ConditionStatus.TRUE,
            "AtCapacity" if at_capacity else "HasCapacity",
            f"All {cfg.max_sessions} sessions are in use or being created." if at_capacity else "",
        )

        blocked = self._recovery_enabled and cfg.stateful and not synced
        pools.set_condition(
            agent,
            POOL_RECOVERY_BLOCKED,
            ConditionStatus.TRUE if blocked else ConditionStatus.FALSE,
            "AwaitingObservation" if blocked else "NotBlocked",
            "Users' sessions cannot be found again until Foundry has been listed."
            if blocked
            else "",
        )

        self._set_ready(agent)
        return ReconcileResult.done()

    def _set_ready(self, agent: str) -> None:
        pools = self._plane.pools
        checks = (
            (POOL_CONFIGURATION_VALID, "ConfigurationInvalid"),
            (POOL_INITIAL_SYNC_COMPLETE, "AwaitingSync"),
            (POOL_FOUNDRY_REACHABLE, "FoundryUnreachable"),
        )
        for type_, reason in checks:
            condition = pools.condition(agent, type_)
            if condition is None or condition.status is not ConditionStatus.TRUE:
                detail = condition.message if condition is not None else ""
                pools.set_condition(agent, POOL_READY, ConditionStatus.FALSE, reason, detail)
                return
        pools.set_condition(agent, POOL_READY, ConditionStatus.TRUE, "AllConditionsMet")
