"""Application factory, lifespan wiring and process entrypoint."""

from __future__ import annotations

import logging
import sys
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI

from hosted_agent_kit import __version__
from hosted_agent_kit.adapters.otel import configure_azure_monitor
from hosted_agent_kit.config.loader import load_config
from hosted_agent_kit.config.models import AgentPoolConfig, ConfigError
from hosted_agent_kit.config.settings import Settings
from hosted_agent_kit.logging_config import configure_logging, log_event
from hosted_agent_kit.ports.foundry import FoundryAdapter
from hosted_agent_kit.runtime import build_adapter, build_runtime, session_deriver
from hosted_agent_kit.service.api import admin, client, health
from hosted_agent_kit.service.api.errors import install_error_handlers
from hosted_agent_kit.service.api.middleware import BodyLimitMiddleware, CorrelationMiddleware
from hosted_agent_kit.service.api.openapi import install_openapi
from hosted_agent_kit.service.container import Container
from hosted_agent_kit.service.security.auth import Authenticator, JwksCache, TokenValidator
from hosted_agent_kit.services.clock import Clock, SystemClock
from hosted_agent_kit.services.idempotency import IdempotencyStore
from hosted_agent_kit.services.scheduling import PluginRegistry

logger = logging.getLogger(__name__)


def _check_consistent(settings: Settings, config: AgentPoolConfig) -> None:
    session_deriver(settings, config)  # raises ConfigError for an inconsistent combination


def build_container(
    settings: Settings,
    pool_config: AgentPoolConfig,
    adapter: FoundryAdapter,
    clock: Clock,
    validator: TokenValidator | None,
    config_loader: Callable[[], AgentPoolConfig] | None = None,
    scheduler_plugins: PluginRegistry | None = None,
) -> Container:
    runtime = build_runtime(settings, pool_config, adapter, clock, config_loader, scheduler_plugins)
    return Container(
        settings=settings,
        runtime=runtime,
        config=runtime.config,
        reloader=runtime.reloader,
        adapter=runtime.adapter,
        pool=runtime.pool,
        reconciler=runtime.reconciler,
        metrics_store=runtime.metrics_store,
        authenticator=Authenticator(settings, validator),
        circuits=runtime.circuits,
        idempotency=IdempotencyStore(
            clock,
            ttl_seconds=settings.idempotency_ttl_seconds,
            max_entries=settings.idempotency_max_entries,
            max_body_bytes=settings.idempotency_max_body_bytes,
        ),
        session_ids=runtime.session_ids,
    )


def create_app(
    settings: Settings | None = None,
    *,
    config: AgentPoolConfig | None = None,
    adapter: FoundryAdapter | None = None,
    clock: Clock | None = None,
    validator: TokenValidator | None = None,
    config_loader: Callable[[], AgentPoolConfig] | None = None,
    scheduler_plugins: PluginRegistry | None = None,
) -> FastAPI:
    """Build the application. Invalid configuration fails here, before serving traffic."""
    resolved = settings or Settings()
    configure_logging(resolved.log_level)
    pool_config = config or load_config(resolved.agent_pool_config)
    if config_loader is None and config is None:
        path = resolved.agent_pool_config
        config_loader = lambda: load_config(path)  # noqa: E731 - re-read the file on reload
    session_deriver(resolved, pool_config)  # fail fast on an inconsistent configuration

    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        foundry = adapter or build_adapter(resolved)
        token_validator = validator
        jwks: JwksCache | None = None
        if resolved.auth_mode == "entra" and token_validator is None:
            host = resolved.entra_authority_host.rstrip("/")
            url = f"{host}/{resolved.entra_tenant_id}/discovery/v2.0/keys"
            jwks = JwksCache(url, resolved.jwks_cache_seconds)
            token_validator = TokenValidator(resolved, jwks)
        if resolved.auth_mode == "development":
            log_event(
                logger,
                "development_auth_enabled",
                level=logging.WARNING,
                hint="never use POOL_AUTH_MODE=development outside local development",
            )
        configure_azure_monitor(resolved.applicationinsights_connection_string)
        container: Container | None = None
        try:
            container = build_container(
                resolved,
                pool_config,
                foundry,
                clock or SystemClock(),
                token_validator,
                config_loader,
                scheduler_plugins,
            )
            app.state.container = container
            await container.runtime.start()
            container.ready = True
            log_event(logger, "service_started", agents=pool_config.names, version=__version__)
            yield
        finally:
            # Runs on a failed start-up too, so a half-built service never leaks its clients.
            if container is not None:
                container.ready = False
                await container.runtime.stop()
            else:
                await foundry.close()
            if jwks is not None:
                await jwks.close()

    app = FastAPI(
        title="Foundry Hosted Agent Pool",
        version=__version__,
        description="BFF and worker-pool controller for Microsoft Foundry hosted agents.",
        lifespan=lifespan,
    )
    install_error_handlers(app)
    install_openapi(app)
    app.include_router(client.router)
    app.include_router(admin.router)
    app.include_router(health.router)
    if resolved.metrics_endpoint_enabled:
        app.include_router(admin.metrics_router)
    app.add_middleware(BodyLimitMiddleware, max_bytes=resolved.max_body_bytes)
    app.add_middleware(CorrelationMiddleware)
    return app


def run() -> None:
    """Console entrypoint: validate configuration, then serve."""
    try:
        settings = Settings()
        app = create_app(settings)
    except ConfigError as exc:
        sys.exit(f"configuration error: {exc}")
    uvicorn.run(
        app,
        host=settings.host,
        port=settings.port,
        log_config=None,
        # On SIGTERM uvicorn stops accepting connections and waits this long for open ones.
        timeout_graceful_shutdown=max(1, int(settings.shutdown_grace_seconds)),
    )
