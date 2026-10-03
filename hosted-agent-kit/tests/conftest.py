"""Shared fixtures and builders."""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Any

import pytest

from hosted_agent_kit.adapters.memory_affinity import MemoryAffinityStore
from hosted_agent_kit.adapters.memory_queue import MemoryQueueManager
from hosted_agent_kit.adapters.memory_registry import MemorySessionRegistry
from hosted_agent_kit.config.loader import build_config
from hosted_agent_kit.config.models import AgentPoolConfig
from hosted_agent_kit.config.settings import Settings
from hosted_agent_kit.runtime import isolation_keys
from hosted_agent_kit.services.metrics import GatedMetrics, InMemoryMetrics
from hosted_agent_kit.services.pool import PoolRequest, PoolService
from hosted_agent_kit.services.reconciler import Reconciler
from hosted_agent_kit.services.scheduler import Scheduler
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from tests.fakes.clock import FakeClock
from tests.fakes.foundry import FakeFoundry


def make_config(
    agents: dict[str, dict[str, Any]], defaults: dict[str, Any] | None = None
) -> AgentPoolConfig:
    section: dict[str, Any] = {"agents": agents}
    if defaults is not None:
        section["defaults"] = defaults
    return build_config({"agentPool": section})


def make_settings(**overrides: Any) -> Settings:
    values: dict[str, Any] = {
        "auth_mode": "development",
        "backoff_base_seconds": 0.01,
        "backoff_max_seconds": 0.05,
        "create_ready_timeout_seconds": 5,
        "retry_after_seconds": 7,
    }
    values.update(overrides)
    return Settings(**values)


@dataclass
class Harness:
    pool: PoolService
    reconciler: Reconciler
    fake: FoundryFake
    registry: MemorySessionRegistry
    affinity: MemoryAffinityStore
    queue: MemoryQueueManager
    metrics: InMemoryMetrics
    gated: GatedMetrics
    clock: FakeClock
    config: AgentPoolConfig
    settings: Settings
    plane: Any


FoundryFake = FakeFoundry


def make_harness(
    agents: dict[str, dict[str, Any]] | None = None,
    defaults: dict[str, Any] | None = None,
    *,
    fake: FakeFoundry | None = None,
    session_ids: SessionIdDeriver | None = None,
    plugins: Any = None,
    **settings_overrides: Any,
) -> Harness:
    config = make_config(
        agents or {"stateless-agent": {}},
        defaults if defaults is not None else {"mode": "stateless", "max_sessions": 2},
    )
    fake = fake or FakeFoundry(FakeClock())
    clock = fake.clock
    registry = MemorySessionRegistry(clock)
    affinity = MemoryAffinityStore()
    queue = MemoryQueueManager()
    memory = InMemoryMetrics()
    metrics = GatedMetrics([memory], lambda agent: config.agents[agent].telemetry.enabled)
    settings = make_settings(**settings_overrides)
    pool = PoolService(
        config=config,
        adapter=fake,
        registry=registry,
        affinity=affinity,
        queue=queue,
        metrics=metrics,
        scheduler=Scheduler(),
        clock=clock,
        settings=settings,
        session_ids=session_ids,
        plugins=plugins,
        isolation=isolation_keys(settings, config),
    )
    reconciler = Reconciler(
        config=config,
        adapter=fake,
        registry=registry,
        pool=pool,
        metrics=metrics,
        clock=clock,
        session_ids=session_ids,
    )
    return Harness(
        pool=pool,
        reconciler=reconciler,
        fake=fake,
        registry=registry,
        affinity=affinity,
        queue=queue,
        metrics=memory,
        gated=metrics,
        clock=clock,
        config=config,
        settings=settings,
        plane=pool.plane,
    )


_counter = 0


def make_request(agent: str = "stateless-agent", user: str = "u1", **kwargs: Any) -> PoolRequest:
    global _counter
    _counter += 1
    values: dict[str, Any] = {
        "agent_name": agent,
        "user_id": user,
        "payload": {"input": "hello"},
        "timeout_seconds": 30.0,
        "request_id": f"req-{_counter}",
        "correlation_id": f"corr-{_counter}",
    }
    values.update(kwargs)
    return PoolRequest(**values)


async def settle(times: int = 5) -> None:
    """Let ready tasks run so queued waiters reach their wait points."""
    for _ in range(times):
        await asyncio.sleep(0)


@pytest.fixture
def harness() -> Harness:
    return make_harness()


async def eventually(condition: Callable[[], Awaitable[bool]], within: float = 2.0) -> None:
    """Poll an async condition against real time (needed when thread hops are involved)."""
    deadline = asyncio.get_running_loop().time() + within
    while not await condition():
        if asyncio.get_running_loop().time() > deadline:
            raise AssertionError("condition not met in time")
        await asyncio.sleep(0.005)
