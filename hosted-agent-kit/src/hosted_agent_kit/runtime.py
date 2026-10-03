"""The wired scheduling runtime: pool service, controllers, stores and metrics.

Both the embedded SDK (``Hack``) and the standalone HTTP service build one ``Runtime`` and
start and stop it the same way.
"""

from __future__ import annotations

import contextlib
import logging
import uuid
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlparse

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
from hosted_agent_kit.ports.ownership import OwnershipStore
from hosted_agent_kit.ports.quota import QuotaLedger
from hosted_agent_kit.services.circuit_breaker import CircuitBreakers
from hosted_agent_kit.services.clock import Clock, SystemClock
from hosted_agent_kit.services.config_reload import ConfigReloader
from hosted_agent_kit.services.isolation import UserIsolationKeys
from hosted_agent_kit.services.metrics import GatedMetrics, InMemoryMetrics
from hosted_agent_kit.services.pool import PoolService
from hosted_agent_kit.services.quota import KitGovernor, QuotaGate
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
    shard = settings.shard.index if settings.shard is not None else None
    return SessionIdDeriver(key.get_secret_value().encode(), shard=shard)


def owned_config(settings: KitSettings, config: AgentPoolConfig) -> AgentPoolConfig:
    """The agents this kit schedules: ``owns`` when set, else every configured agent."""
    owns = settings.owns
    if owns is None:
        return config
    unknown = [name for name in owns if name not in config.agents]
    if unknown:
        raise ConfigError(f"owns names agents that are not configured: {', '.join(unknown)}")
    if not owns:
        raise ConfigError("owns is empty: a kit must own at least one agent")
    return AgentPoolConfig(agents={n: config.agents[n] for n in config.names if n in owns})


def ledger_scope(settings: KitSettings) -> str:
    q = settings.quota
    return f"{q.subscription_id or 'subscription'}/{q.region or 'region'}"


def build_ledger(
    settings: KitSettings, clock: Clock, *, client: Any | None = None
) -> QuotaLedger | None:
    """The shared regional ledger, when ``quota.ledger`` is configured."""
    section = settings.quota.ledger
    if section is None:
        return None
    if section.backend == "memory":
        from hosted_agent_kit.adapters.memory_ledger import MemoryLedger

        return MemoryLedger(clock, ttl_seconds=section.ttl_seconds)
    from hosted_agent_kit.adapters.redis_ledger import RedisLedger
    from hosted_agent_kit.adapters.redis_support import connect

    redis = connect(
        section.url,
        entra_auth=section.entra_auth,
        timeout_seconds=section.timeout_seconds,
        client=client,
    )
    return RedisLedger(redis, scope=ledger_scope(settings), ttl_seconds=section.ttl_seconds)


def project_id(settings: KitSettings) -> str:
    """A stable name for the Foundry project, used in ownership keys."""
    endpoint = settings.foundry_project_endpoint
    if not endpoint:
        return "local"
    parsed = urlparse(endpoint)
    return f"{parsed.netloc}{parsed.path}".strip("/").lower() or "local"


def build_ownership_store(
    settings: KitSettings, clock: Clock, *, client: Any | None = None
) -> OwnershipStore:
    """The lease store for ``ownership.backend``."""
    section = settings.ownership
    if section.backend == "memory":
        from hosted_agent_kit.adapters.memory_ownership import MemoryOwnershipStore

        return MemoryOwnershipStore(clock)
    if section.backend == "redis":
        from hosted_agent_kit.adapters.redis_ownership import RedisOwnershipStore
        from hosted_agent_kit.adapters.redis_support import connect

        redis = connect(
            section.url,
            entra_auth=section.entra_auth,
            timeout_seconds=section.timeout_seconds,
            client=client,
        )
        return RedisOwnershipStore(redis)
    raise ConfigError("ownership.backend is none: there is no lease store to build")


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
    ledger: QuotaLedger | None = None
    kit_id: str = "kit"
    ready: bool = False
    started: bool = False

    async def start(self) -> None:
        """Start the Foundry client and the controllers, then accept requests."""
        self.started = True
        if self.ledger is not None:
            try:
                await self.ledger.start()
            except Exception as exc:  # the ledger is optional: the kit runs on its own budget
                log_event(
                    logger,
                    "quota_ledger_unavailable",
                    level=logging.WARNING,
                    error_type=type(exc).__name__,
                )
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
        if self.ledger is not None:
            with contextlib.suppress(Exception):
                await self.ledger.close()
        await self.raw_adapter.close()
        self.started = False


def build_runtime(
    settings: KitSettings,
    pool_config: AgentPoolConfig,
    adapter: FoundryAdapter,
    clock: Clock | None = None,
    config_loader: Callable[[], AgentPoolConfig] | None = None,
    scheduler_plugins: PluginRegistry | None = None,
    ledger: QuotaLedger | None = None,
) -> Runtime:
    from hosted_agent_kit.adapters.otel import OtelMetrics  # optional dependency, imported late

    clock = clock or SystemClock()
    pool_config = owned_config(settings, pool_config)
    if config_loader is not None:
        unfiltered = config_loader

        def config_loader() -> AgentPoolConfig:
            return owned_config(settings, unfiltered())

    kit_id = settings.kit_id or f"kit-{uuid.uuid4().hex[:8]}"
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
    ledger = ledger if ledger is not None else build_ledger(settings, clock)
    gate = QuotaGate(
        registry=registry,
        config=config,
        clock=clock,
        settings=settings,
        adapter=guarded,
        governor=KitGovernor(settings.quota, clock, kit_id=kit_id, ledger=ledger),
        metrics=metrics,
    )
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
        quota=gate,
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
        ledger=ledger,
        kit_id=kit_id,
    )
