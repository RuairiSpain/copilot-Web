"""Application container: the wired components shared by the API handlers."""

from __future__ import annotations

from dataclasses import dataclass, field

from hosted_agent_kit.config.holder import ConfigHolder
from hosted_agent_kit.config.settings import Settings
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.runtime import Runtime
from hosted_agent_kit.service.security.auth import Authenticator
from hosted_agent_kit.services.circuit_breaker import CircuitBreakers
from hosted_agent_kit.services.config_reload import ConfigReloader
from hosted_agent_kit.services.idempotency import IdempotencyStore
from hosted_agent_kit.services.metrics import InMemoryMetrics
from hosted_agent_kit.services.pool import PoolService
from hosted_agent_kit.services.reconciler import Reconciler
from hosted_agent_kit.services.session_ids import SessionIdDeriver


@dataclass
class Container:
    settings: Settings
    config: ConfigHolder
    adapter: FoundryAdapter
    pool: PoolService
    reconciler: Reconciler
    metrics_store: InMemoryMetrics
    authenticator: Authenticator
    circuits: CircuitBreakers
    runtime: Runtime
    idempotency: IdempotencyStore | None = None
    reloader: ConfigReloader | None = None
    session_ids: SessionIdDeriver | None = None
    ready: bool = field(default=False)
