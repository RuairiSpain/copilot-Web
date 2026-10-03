"""A per-agent circuit breaker for calls to Foundry.

Closed: calls pass and consecutive failures are counted. After ``failure_threshold`` in a row the
circuit opens. Open: calls fail at once with the time left. After ``open_seconds`` the circuit moves
to half open and lets ``half_open_max_calls`` probe calls through. A successful probe closes the
circuit and a failed probe opens it again.
"""

from __future__ import annotations

import logging
import math
from collections.abc import Callable

from hosted_agent_kit.config.models import AgentPoolConfig, CircuitBreakerConfig
from hosted_agent_kit.domain.enums import CircuitState
from hosted_agent_kit.domain.errors import FoundryCircuitOpen
from hosted_agent_kit.logging_config import log_event
from hosted_agent_kit.ports.metrics import MetricsRecorder
from hosted_agent_kit.services.clock import Clock

logger = logging.getLogger(__name__)
DISABLED = "disabled"


class CircuitBreaker:
    def __init__(
        self,
        agent: str,
        config: CircuitBreakerConfig,
        clock: Clock,
        metrics: MetricsRecorder,
        on_transition: Callable[[str, CircuitState], None] | None = None,
    ) -> None:
        self._on_transition = on_transition
        self._agent = agent
        self._config = config
        self._clock = clock
        self._metrics = metrics
        self._state = CircuitState.CLOSED
        self._failures = 0
        self._opened_at = 0.0
        self._probes = 0
        metrics.circuit_state(agent, self._state.value)

    @property
    def state(self) -> CircuitState:
        return self._state

    def before_call(self) -> bool:
        """Admit a call. True means a half-open probe. Raises while the circuit is open."""
        if self._state is CircuitState.OPEN:
            remaining = self._opened_at + self._config.open_seconds - self._clock.monotonic()
            if remaining > 0:
                raise FoundryCircuitOpen(remaining)
            self._to(CircuitState.HALF_OPEN)
        if self._state is CircuitState.HALF_OPEN:
            if self._probes >= self._config.half_open_max_calls:
                raise FoundryCircuitOpen(1.0)
            self._probes += 1
            return True
        return False

    def success(self, probe: bool) -> None:
        # The probe count needs no update here: closing or reopening resets it.
        if probe:
            if self._state is CircuitState.HALF_OPEN:
                self._failures = 0
                self._to(CircuitState.CLOSED)
            return
        self._failures = 0

    def failure(self, probe: bool) -> None:
        if probe:
            if self._state is CircuitState.HALF_OPEN:
                self._open()
            return
        if self._state is not CircuitState.CLOSED:
            return
        self._failures += 1
        if self._failures >= self._config.failure_threshold:
            self._open()

    def abandoned(self, probe: bool) -> None:
        """The call ended without an answer (for example the caller cancelled it)."""
        if probe:
            self._probes = max(0, self._probes - 1)

    def _open(self) -> None:
        self._opened_at = self._clock.monotonic()
        self._probes = 0
        self._to(CircuitState.OPEN)

    def _to(self, state: CircuitState) -> None:
        self._state = state
        self._metrics.circuit_state(self._agent, state.value)
        self._metrics.circuit_transition(self._agent, state.value)
        if self._on_transition is not None:
            self._on_transition(self._agent, state)
        log_event(
            logger,
            f"circuit_{state.value}",
            level=logging.WARNING if state is CircuitState.OPEN else logging.INFO,
            agent_name=self._agent,
            failure_threshold=self._config.failure_threshold,
            open_seconds=self._config.open_seconds,
        )


def retry_after_seconds(error: FoundryCircuitOpen) -> int:
    """Whole seconds for the Retry-After header, never less than one."""
    return max(1, math.ceil(error.retry_after_seconds))


class CircuitBreakers:
    """The breakers for every agent that has one enabled."""

    def __init__(self, config: AgentPoolConfig, clock: Clock, metrics: MetricsRecorder) -> None:
        self._listeners: list[Callable[[str, CircuitState], None]] = []
        self._breakers = {
            name: CircuitBreaker(name, agent.circuit_breaker, clock, metrics, self._announce)
            for name, agent in config.agents.items()
            if agent.circuit_breaker.enabled
        }

    def subscribe(self, listener: Callable[[str, CircuitState], None]) -> None:
        """Call ``listener`` with the agent and the new state whenever a circuit changes."""
        self._listeners.append(listener)

    def _announce(self, agent: str, state: CircuitState) -> None:
        for listener in self._listeners:
            listener(agent, state)

    def get(self, agent: str) -> CircuitBreaker | None:
        return self._breakers.get(agent)

    def state(self, agent: str) -> str:
        breaker = self._breakers.get(agent)
        return breaker.state.value if breaker is not None else DISABLED
