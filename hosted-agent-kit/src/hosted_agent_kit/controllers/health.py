"""The dependency-health controller: records whether Foundry can be reached.

It turns what the circuit breaker and the observer know into pool conditions
(``FoundryReachable``, ``CircuitOpen``), so every other controller and every operator reads one
consistent answer. It runs when a circuit changes state and after each observation. The circuit
breaker itself still admits the half-open probes, so there is a single prober.
"""

from __future__ import annotations

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.controllers.observation import ObservationCache
from hosted_agent_kit.controllers.plane import ControlPlane
from hosted_agent_kit.controllers.runtime import ReconcileResult
from hosted_agent_kit.domain.enums import CircuitState
from hosted_agent_kit.domain.resources import (
    POOL_CIRCUIT_OPEN,
    POOL_FOUNDRY_REACHABLE,
    ConditionStatus,
    ResourceKey,
    ResourceKind,
    pool_key,
)
from hosted_agent_kit.ports.registry import ChangeEvent
from hosted_agent_kit.services.circuit_breaker import CircuitBreakers


class DependencyHealthController:
    name = "health"
    workers = 1

    def __init__(
        self,
        *,
        config: ConfigSource,
        plane: ControlPlane,
        cache: ObservationCache,
        circuits: CircuitBreakers | None = None,
    ) -> None:
        self._config = config
        self._plane = plane
        self._cache = cache
        self._circuits = circuits
        if circuits is not None:
            circuits.subscribe(self._on_circuit)

    def _on_circuit(self, agent: str, state: CircuitState) -> None:
        self._plane.manager.enqueue("health", pool_key(agent))

    def map_event(self, event: ChangeEvent) -> list[ResourceKey]:
        return []

    async def reconcile(self, key: ResourceKey) -> ReconcileResult:
        if key.kind is not ResourceKind.AGENT_POOL or self._config.get(key.agent_name) is None:
            return ReconcileResult.done()
        agent = key.agent_name
        pools = self._plane.pools
        state = self._circuits.state(agent) if self._circuits is not None else "disabled"
        circuit_open = state == CircuitState.OPEN.value
        pools.set_condition(
            agent,
            POOL_CIRCUIT_OPEN,
            ConditionStatus.TRUE if circuit_open else ConditionStatus.FALSE,
            "TooManyFailures" if circuit_open else "Closed",
            "Calls to Foundry are paused after repeated failures." if circuit_open else "",
        )
        observation = self._cache.get(agent)
        if circuit_open:
            reachable = (ConditionStatus.FALSE, "CircuitOpen", "The circuit breaker is open.")
        elif observation is None or observation.attempted_at is None:
            reachable = (ConditionStatus.UNKNOWN, "NotObserved", "Foundry has not been listed yet.")
        elif observation.complete:
            reachable = (ConditionStatus.TRUE, "Listed", "")
        else:
            reachable = (
                ConditionStatus.FALSE,
                "ListingFailed",
                f"The last {observation.consecutive_failures} listing(s) failed.",
            )
        pools.set_condition(agent, POOL_FOUNDRY_REACHABLE, *reachable)
        self._plane.manager.enqueue("status", key)  # Ready depends on what was just recorded
        return ReconcileResult.done()
