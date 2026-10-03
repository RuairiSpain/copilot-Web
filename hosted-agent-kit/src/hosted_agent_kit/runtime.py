"""The wired scheduling runtime: pool service, controllers, stores and metrics.

Both the embedded SDK (``Hack``) and the standalone HTTP service build one ``Runtime`` and
start and stop it the same way.
"""

from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass

from hosted_agent_kit.adapters.circuit_breaking import CircuitBreakingAdapter
from hosted_agent_kit.adapters.memory_affinity import MemoryAffinityStore
from hosted_agent_kit.adapters.memory_queue import MemoryQueueManager
from hosted_agent_kit.adapters.memory_registry import MemorySessionRegistry
from hosted_agent_kit.config.holder import ConfigHolder
from hosted_agent_kit.config.models import AgentPoolConfig, ConfigError
from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.domain.enums import UserIsolation
from hosted_agent_kit.logging_config import configure_identifier_hashing, log_event
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.ports.metrics import MetricsRecorder
from hosted_agent_kit.services.circuit_breaker import CircuitBreakers
from hosted_agent_kit.services.clock import Clock, SystemClock
from hosted_agent_kit.services.config_reload import ConfigReloader
from hosted_agent_kit.services.isolation import UserIsolationKeys
from hosted_agent_kit.services.metrics import GatedMetrics, InMemoryMetrics
from hosted_agent_kit.services.pool import PoolService
from hosted_agent_kit.services.reconciler import Reconciler
from hosted_agent_kit.services.scheduling import PluginRegistry
from hosted_agent_kit.services.session_ids import SessionIdDeriver

logger = logging.getLogger(__name__)


def build_adapter(settings: KitSettings) -> FoundryAdapter:
    if not settings.foundry_project_endpoint:
        raise ConfigError("FOUNDRY_PROJECT_ENDPOINT is required")
    from hosted_agent_kit.adapters.foundry_sdk import SdkFoundryAdapter  # imports the Azure SDK

    return SdkFoundryAdapter(
        settings.foundry_project_endpoint,
        isolation_key=settings.foundry_isolation_key,
        max_response_bytes=settings.max_response_bytes,
    )


def session_deriver(settings: KitSettings, config: AgentPoolConfig) -> SessionIdDeriver | None:
    """The derived-id helper, or None when POOL_SESSION_ID_KEY is not set."""
    key = settings.session_id_key
    if key is None:
        return None
    warm = [n for n, a in config.agents.items() if a.stateful and a.min_warm_sessions > 0]
    if warm:
        raise ConfigError(
            "stateful agents cannot keep warm sessions when POOL_SESSION_ID_KEY is set, because "
            f"each user's session id is derived from the user: {', '.join(warm)}"
        )
    return SessionIdDeriver(key.get_secret_value().encode())


def isolation_keys(settings: KitSettings, config: AgentPoolConfig) -> UserIsolationKeys | None:
    """Per-user key derivation, or None when no agent sends per-user headers."""
    needed = [n for n, a in config.agents.items() if a.user_isolation is not UserIsolation.OFF]
    secret = settings.user_isolation_secret or settings.session_id_key
    if secret is None:
        if needed:
            raise ConfigError(
                "agents that set user_isolation need POOL_USER_ISOLATION_SECRET (or "
                f"POOL_SESSION_ID_KEY): {', '.join(needed)}"
            )
        return None
    return UserIsolationKeys(secret.get_secret_value().encode())


def check_consistent(settings: KitSettings, config: AgentPoolConfig) -> None:
    session_deriver(settings, config)  # raises ConfigError for an inconsistent combination
    isolation_keys(settings, config)


@dataclass(kw_only=True)
class Runtime:
    settings: KitSettings
    config: ConfigHolder
    raw_adapter: FoundryAdapter
    adapter: FoundryAdapter  # wrapped in the circuit breaker
    pool: PoolService
    reconciler: Reconciler
    metrics_store: InMemoryMetrics
    circuits: CircuitBreakers
    session_ids: SessionIdDeriver | None = None
    reloader: ConfigReloader | None = None
    ready: bool = False
    started: bool = False

    async def start(self) -> None:
        """Start the Foundry client and the controllers, then accept requests."""
        self.started = True
        await self.raw_adapter.start()
        await self.reconciler.start()
        await self.pool.start()
        self.ready = True

    async def stop(self) -> None:
        """Stop admitting requests, wait for running ones, then stop the controllers.

        Safe after a failed ``start``: a half-started runtime still releases its clients.
        """
        self.ready = False  # readiness fails first, so traffic is routed away
        if self.started:
            self.pool.begin_shutdown()  # then no new request is admitted
            if not await self.pool.wait_idle(self.settings.shutdown_grace_seconds):
                log_event(
                    logger,
                    "shutdown_with_requests_in_flight",
                    level=logging.WARNING,
                    in_flight=self.pool.in_flight,
                )
            await self.reconciler.stop()
            await self.pool.stop()
        await self.raw_adapter.close()
        self.started = False


def build_runtime(
    settings: KitSettings,
    pool_config: AgentPoolConfig,
    adapter: FoundryAdapter,
    clock: Clock | None = None,
    config_loader: Callable[[], AgentPoolConfig] | None = None,
    scheduler_plugins: PluginRegistry | None = None,
) -> Runtime:
    from hosted_agent_kit.adapters.otel import OtelMetrics  # optional dependency, imported late

    clock = clock or SystemClock()
    secret = settings.session_id_key or settings.user_isolation_secret
    configure_identifier_hashing(secret.get_secret_value().encode() if secret else None)
    config = ConfigHolder(pool_config)
    store = InMemoryMetrics()
    sinks: list[MetricsRecorder] = [store]
    if any(agent.telemetry.enabled for agent in config.agents.values()):
        sinks.append(OtelMetrics())
    metrics = GatedMetrics(sinks, lambda agent: config.agents[agent].telemetry.enabled)
    registry = MemorySessionRegistry(clock)
    deriver = session_deriver(settings, pool_config)
    circuits = CircuitBreakers(pool_config, clock, metrics)
    guarded = CircuitBreakingAdapter(adapter, circuits)
    pool = PoolService(
        config=config,
        adapter=guarded,
        registry=registry,
        affinity=MemoryAffinityStore(),
        queue=MemoryQueueManager(),
        metrics=metrics,
        clock=clock,
        settings=settings,
        session_ids=deriver,
        plugins=scheduler_plugins,
        isolation=isolation_keys(settings, pool_config),
    )
    reconciler = Reconciler(
        config=config,
        adapter=guarded,
        registry=registry,
        pool=pool,
        metrics=metrics,
        clock=clock,
        session_ids=deriver,
        circuits=circuits,
    )
    reloader = (
        ConfigReloader(
            holder=config,
            pool=pool,
            plane=pool.plane,
            load=config_loader,
            validate=lambda new: check_consistent(settings, new),
        )
        if config_loader is not None
        else None
    )
    return Runtime(
        settings=settings,
        config=config,
        raw_adapter=adapter,
        adapter=guarded,
        pool=pool,
        reconciler=reconciler,
        metrics_store=store,
        circuits=circuits,
        session_ids=deriver,
        reloader=reloader,
    )
