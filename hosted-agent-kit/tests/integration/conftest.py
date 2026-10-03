"""API integration fixtures: the real ASGI app with fakes behind the ports."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Any

import httpx
import httpx as _httpx
import pytest
from asgi_lifespan import LifespanManager
from fastapi import FastAPI

from hosted_agent_kit.config.settings import Settings
from hosted_agent_kit.service.container import Container
from hosted_agent_kit.service.main import create_app
from hosted_agent_kit.service.security.auth import JwksCache, TokenValidator
from tests.conftest import make_config, make_settings
from tests.fakes.clock import FakeClock
from tests.fakes.entra import AUDIENCE, OID, TENANT, JwksServer, token
from tests.fakes.foundry import FakeFoundry

AGENTS: dict[str, dict[str, Any]] = {
    "coding-agent": {"mode": "stateful", "max_sessions": 3},
    "research-agent": {"mode": "stateless", "max_sessions": 2},
}


@dataclass
class Api:
    app: FastAPI
    client: httpx.AsyncClient
    fake: FakeFoundry
    container: Container
    settings: Settings

    def headers(self, user: str = "u1", **extra: str) -> dict[str, str]:
        return {"X-Dev-User-Id": user, **extra}


async def start_api(
    *,
    agents: dict[str, dict[str, Any]] | None = None,
    defaults: dict[str, Any] | None = None,
    entra: bool = False,
    clock: FakeClock | None = None,
    config_loader: Any = None,
    **settings_overrides: Any,
) -> tuple[Api, LifespanManager]:
    config = make_config(agents or AGENTS, defaults if defaults is not None else {})
    if entra:
        settings_overrides.update(
            auth_mode="entra", entra_tenant_id=TENANT, entra_audience=[AUDIENCE]
        )
    settings = make_settings(**settings_overrides)
    fake = FakeFoundry(clock or FakeClock())
    validator = None
    if entra:
        server = JwksServer()
        jwks = JwksCache(
            "https://keys.test/keys",
            settings.jwks_cache_seconds,
            _httpx.AsyncClient(transport=_httpx.MockTransport(server.handler)),
        )
        validator = TokenValidator(settings, jwks)
    app = create_app(
        settings,
        config=config,
        adapter=fake,
        validator=validator,
        clock=clock,
        config_loader=config_loader,
    )
    manager = LifespanManager(app)
    await manager.__aenter__()
    client = httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://test")
    container: Container = app.state.container
    # The first sync starts with the app. Wait for it, so tests do not race it.
    for _ in range(400):
        if container.reconciler.initial_sync_done:
            break
        await asyncio.sleep(0.005)
    return Api(app, client, fake, container, settings), manager


@pytest.fixture
async def api() -> AsyncIterator[Api]:
    instance, manager = await start_api()
    try:
        yield instance
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


@pytest.fixture
async def entra_api() -> AsyncIterator[Api]:
    instance, manager = await start_api(entra=True)
    try:
        yield instance
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


def bearer(**claims: Any) -> dict[str, str]:
    return {"Authorization": f"Bearer {token(**claims)}"}


__all__ = ["OID", "Api", "bearer", "start_api"]
