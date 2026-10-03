"""Stores the AgentPool resources: one per agent, with spec, status and conditions."""

from __future__ import annotations

from collections.abc import Callable
from datetime import datetime

from hosted_agent_kit.config.holder import ConfigHolder
from hosted_agent_kit.domain.pools import AgentPoolResource, AgentPoolStatus
from hosted_agent_kit.domain.resources import (
    POOL_CIRCUIT_OPEN,
    POOL_CONFIGURATION_VALID,
    POOL_FOUNDRY_REACHABLE,
    POOL_INITIAL_SYNC_COMPLETE,
    POOL_READY,
    POOL_RECOVERY_BLOCKED,
    Condition,
    ConditionStatus,
    get_condition,
    set_condition,
)
from hosted_agent_kit.services.events import EventRecorder, EventType


class PoolStore:
    def __init__(
        self, holder: ConfigHolder, now: Callable[[], datetime], events: EventRecorder
    ) -> None:
        self._holder = holder
        self._now = now
        self._events = events
        self._pools: dict[str, AgentPoolResource] = {}
        for name, cfg in holder.agents.items():
            conditions: list[Condition] = []
            stamp = now()
            for type_, status, reason in (
                (POOL_READY, ConditionStatus.UNKNOWN, "Starting"),
                (POOL_INITIAL_SYNC_COMPLETE, ConditionStatus.FALSE, "AwaitingSync"),
                (POOL_CONFIGURATION_VALID, ConditionStatus.TRUE, "Valid"),
                (POOL_FOUNDRY_REACHABLE, ConditionStatus.UNKNOWN, "NotObserved"),
                (POOL_CIRCUIT_OPEN, ConditionStatus.FALSE, "Closed"),
                (POOL_RECOVERY_BLOCKED, ConditionStatus.FALSE, "NotBlocked"),
            ):
                conditions = set_condition(conditions, type_, status, reason, now=stamp)
            self._pools[name] = AgentPoolResource(
                agent_name=name,
                generation=holder.generation(name),
                resource_version=1,
                spec=cfg,
                status=AgentPoolStatus(conditions=conditions),
            )

    def get(self, agent_name: str) -> AgentPoolResource:
        return self._pools[agent_name].model_copy(deep=True)

    def list(self) -> list[AgentPoolResource]:
        return [p.model_copy(deep=True) for p in self._pools.values()]

    def sync_spec(self, agent_name: str) -> bool:
        """Bring the stored spec in line with the live configuration. True if it changed."""
        pool = self._pools[agent_name]
        cfg = self._holder.get(agent_name)
        generation = self._holder.generation(agent_name)
        if cfg is None or generation == pool.generation:
            return False
        pool.spec = cfg
        pool.generation = generation
        pool.resource_version += 1
        self._events.pool(
            agent_name,
            EventType.NORMAL,
            "SpecChanged",
            f"The configuration changed (generation {generation}).",
        )
        return True

    def update_status(self, agent_name: str, change: Callable[[AgentPoolStatus], None]) -> None:
        pool = self._pools[agent_name]
        before = pool.status.model_copy(deep=True)
        change(pool.status)
        if pool.status != before:
            pool.resource_version += 1

    def condition(self, agent_name: str, type_: str) -> Condition | None:
        return get_condition(self._pools[agent_name].status.conditions, type_)

    def set_condition(
        self,
        agent_name: str,
        type_: str,
        status: ConditionStatus,
        reason: str,
        message: str = "",
    ) -> bool:
        """Set a condition. Records an event when its status changes. True if anything changed."""
        pool = self._pools[agent_name]
        previous = get_condition(pool.status.conditions, type_)
        updated = set_condition(
            pool.status.conditions, type_, status, reason, message, now=self._now()
        )
        if updated is pool.status.conditions:
            return False
        pool.status.conditions = updated
        pool.resource_version += 1
        if previous is None or previous.status is not status:
            warning = (type_ == POOL_READY and status is not ConditionStatus.TRUE) or (
                type_ in (POOL_CIRCUIT_OPEN, POOL_RECOVERY_BLOCKED)
                and status is ConditionStatus.TRUE
            )
            self._events.pool(
                agent_name,
                EventType.WARNING if warning else EventType.NORMAL,
                "ConditionChanged",
                f"{type_} is now {status.value} ({reason}).",
            )
        return True
