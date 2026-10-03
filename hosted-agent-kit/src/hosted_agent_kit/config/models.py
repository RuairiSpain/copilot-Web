"""Validated pool configuration models (PRD section 7)."""

from __future__ import annotations

import re
from typing import Any, Self

from pydantic import BaseModel, ConfigDict, Field, model_validator

from hosted_agent_kit.domain.enums import (
    AffinityMode,
    AgentMode,
    AgentProtocol,
    SchedulerStrategy,
    VersionDrain,
)

AGENT_NAME_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$")


class ConfigError(Exception):
    """Raised when configuration cannot be loaded or fails validation."""


class _Strict(BaseModel):
    model_config = ConfigDict(extra="forbid")


class PartialQueue(_Strict):
    enabled: bool | None = None
    max_depth: int | None = Field(default=None, ge=0)
    max_wait_seconds: float | None = Field(default=None, gt=0)


class PartialTelemetry(_Strict):
    enabled: bool | None = None


class PartialCircuitBreaker(_Strict):
    enabled: bool | None = None
    failure_threshold: int | None = Field(default=None, ge=1, le=1000)
    open_seconds: float | None = Field(default=None, gt=0)
    half_open_max_calls: int | None = Field(default=None, ge=1, le=100)


class SchedulerProfileConfig(_Strict):
    """Extra scheduling plugins for one agent. Core filters always run."""

    filters: list[str] = Field(default_factory=list)
    scores: dict[str, int] = Field(default_factory=dict)

    @model_validator(mode="after")
    def _weights(self) -> Self:
        for name, weight in self.scores.items():
            if not 1 <= weight <= 1000:
                raise ValueError(f"score weight for '{name}' must be 1 to 1000")
        return self


class PartialAgentSettings(_Strict):
    """Properties allowed at both the ``defaults`` and per-agent override levels."""

    mode: AgentMode | None = None
    affinity: AffinityMode | None = None
    scheduler: SchedulerStrategy | None = None
    min_warm_sessions: int | None = Field(default=None, ge=0)
    max_sessions: int | None = Field(default=None, ge=1)
    queue: PartialQueue | None = None
    sync_interval_seconds: int | None = Field(default=None, ge=10)
    create_retries: int | None = Field(default=None, ge=0, le=10)
    telemetry: PartialTelemetry | None = None
    adopt_unbound_sessions: bool | None = None
    protocol: AgentProtocol | None = None
    circuit_breaker: PartialCircuitBreaker | None = None
    agent_version: str | None = Field(default=None, min_length=1, max_length=64)
    version_drain: VersionDrain | None = None
    scheduler_profile: SchedulerProfileConfig | None = None


class QueueConfig(_Strict):
    enabled: bool = True
    max_depth: int = Field(default=500, ge=0)
    max_wait_seconds: float = Field(default=120, gt=0)


class TelemetryConfig(_Strict):
    enabled: bool = True


class CircuitBreakerConfig(_Strict):
    """Per-agent circuit breaker. Only unavailable and timeout errors count as failures."""

    enabled: bool = True
    failure_threshold: int = Field(default=5, ge=1, le=1000)
    open_seconds: float = Field(default=30, gt=0)
    half_open_max_calls: int = Field(default=1, ge=1, le=100)


class AgentConfig(_Strict):
    """Fully merged and validated settings for one agent."""

    model_config = ConfigDict(extra="forbid", frozen=True)

    name: str
    mode: AgentMode
    affinity: AffinityMode
    scheduler: SchedulerStrategy = SchedulerStrategy.FIRST_AVAILABLE
    min_warm_sessions: int = Field(default=0, ge=0)
    max_sessions: int = Field(default=10, ge=1)
    queue: QueueConfig = Field(default_factory=QueueConfig)
    sync_interval_seconds: int = Field(default=60, ge=10)
    create_retries: int = Field(default=3, ge=0, le=10)
    telemetry: TelemetryConfig = Field(default_factory=TelemetryConfig)
    adopt_unbound_sessions: bool = True
    protocol: AgentProtocol = AgentProtocol.RESPONSES
    circuit_breaker: CircuitBreakerConfig = Field(default_factory=CircuitBreakerConfig)
    # Pin new sessions to this agent version. Unset means the latest version at creation time.
    agent_version: str | None = Field(default=None, min_length=1, max_length=64)
    version_drain: VersionDrain = VersionDrain.NEVER
    # Filters to add to the core ones, and score weights that replace the strategy score.
    scheduler_profile: SchedulerProfileConfig = Field(default_factory=SchedulerProfileConfig)

    @model_validator(mode="after")
    def _check_consistency(self) -> Self:
        expected = AffinityMode.USER if self.mode is AgentMode.STATEFUL else AffinityMode.NONE
        if self.affinity is not expected:
            raise ValueError(
                f"mode '{self.mode}' requires affinity '{expected}', got '{self.affinity}'"
            )
        if self.min_warm_sessions > self.max_sessions:
            raise ValueError("min_warm_sessions cannot exceed max_sessions")
        if self.queue.enabled and self.queue.max_depth == 0:
            raise ValueError("queue.enabled requires queue.max_depth greater than zero")
        return self

    @property
    def stateful(self) -> bool:
        return self.mode is AgentMode.STATEFUL


class AgentPoolConfig(BaseModel):
    """The complete, validated agent pool configuration."""

    model_config = ConfigDict(frozen=True)

    agents: dict[str, AgentConfig]

    @model_validator(mode="after")
    def _check_agents(self) -> Self:
        if not self.agents:
            raise ValueError("at least one agent must be configured")
        return self

    def get(self, agent_name: str) -> AgentConfig | None:
        return self.agents.get(agent_name)

    @property
    def names(self) -> list[str]:
        return list(self.agents)


def deep_merge(base: dict[str, Any], override: dict[str, Any]) -> dict[str, Any]:
    """Merge ``override`` over ``base``; nested dicts merge key by key."""
    merged = dict(base)
    for key, value in override.items():
        existing = merged.get(key)
        if isinstance(existing, dict) and isinstance(value, dict):
            merged[key] = deep_merge(existing, value)
        else:
            merged[key] = value
    return merged
