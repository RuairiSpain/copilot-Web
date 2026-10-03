"""Middleware, disconnect helper, config model guards and the process entrypoint."""

from __future__ import annotations

import asyncio
import runpy
import sys
from typing import Any

import pytest
import uvicorn
from starlette.requests import Request
from starlette.types import Message, Receive, Scope, Send

from hosted_agent_kit.config.models import AgentPoolConfig, ConfigError
from hosted_agent_kit.domain.errors import ClientDisconnectedError
from hosted_agent_kit.service import main
from hosted_agent_kit.service.api.disconnect import run_until_disconnect
from hosted_agent_kit.service.api.middleware import CorrelationMiddleware
from tests.conftest import make_config, make_settings


def test_empty_agent_pool_config_is_rejected_at_model_level() -> None:
    with pytest.raises(ValueError, match="at least one agent"):
        AgentPoolConfig(agents={})


async def test_disconnect_helper_ignores_non_disconnect_messages() -> None:
    messages = iter(
        [{"type": "http.request"}, {"type": "http.request"}, {"type": "http.disconnect"}]
    )

    async def receive() -> Message:
        await asyncio.sleep(0)
        return next(messages)

    request = Request({"type": "http", "headers": []}, receive)
    gate = asyncio.Event()

    async def work() -> str:
        await gate.wait()
        return "done"

    with pytest.raises(ClientDisconnectedError):
        await run_until_disconnect(request, work())


async def test_disconnect_helper_cancels_the_work_when_the_caller_is_cancelled() -> None:
    cancelled = asyncio.Event()

    async def receive() -> Message:
        await asyncio.sleep(3600)
        return {"type": "http.disconnect"}

    async def work() -> None:
        try:
            await asyncio.sleep(3600)
        except asyncio.CancelledError:
            cancelled.set()
            raise

    request = Request({"type": "http", "headers": []}, receive)
    outer = asyncio.create_task(run_until_disconnect(request, work()))
    await asyncio.sleep(0.01)
    outer.cancel()
    with pytest.raises(asyncio.CancelledError):
        await outer
    assert cancelled.is_set()


async def test_crash_after_response_start_is_reraised_not_masked() -> None:
    async def app(scope: Scope, receive: Receive, send: Send) -> None:
        await send({"type": "http.response.start", "status": 200, "headers": []})
        raise RuntimeError("mid-stream failure")

    sent: list[Message] = []

    async def send(message: Message) -> None:
        sent.append(message)

    async def receive() -> Message:
        return {"type": "http.request"}

    with pytest.raises(RuntimeError, match="mid-stream"):
        await CorrelationMiddleware(app)({"type": "http", "headers": []}, receive, send)
    assert dict(sent[0]["headers"])[b"x-correlation-id"]


async def test_non_http_scopes_pass_through_untouched() -> None:
    seen: list[str] = []

    async def app(scope: Scope, receive: Receive, send: Send) -> None:
        seen.append(scope["type"])

    from hosted_agent_kit.service.api.middleware import BodyLimitMiddleware

    async def receive() -> Message:
        return {"type": "lifespan.startup"}

    async def send(message: Message) -> None:
        return None

    await CorrelationMiddleware(app)({"type": "lifespan"}, receive, send)
    await BodyLimitMiddleware(app, 10)({"type": "websocket"}, receive, send)
    assert seen == ["lifespan", "websocket"]


# ------------------------------------------------------------- entrypoints


def test_run_serves_the_app_with_configured_host_and_port(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Any
) -> None:
    config = tmp_path / "pool.yaml"
    config.write_text("agentPool:\n  agents:\n    a:\n      mode: stateless\n")
    monkeypatch.setenv("AGENT_POOL_CONFIG", str(config))
    monkeypatch.setenv("POOL_AUTH_MODE", "development")
    monkeypatch.setenv("POOL_PORT", "9999")
    served: dict[str, Any] = {}
    monkeypatch.setattr(uvicorn, "run", lambda app, **kw: served.update(app=app, **kw))
    main.run()
    assert served["port"] == 9999 and served["log_config"] is None
    assert served["host"] == "127.0.0.1"  # loopback unless POOL_HOST says otherwise
    monkeypatch.setenv("POOL_HOST", "0.0.0.0")
    main.run()
    assert served["host"] == "0.0.0.0"


def test_run_exits_with_a_clear_message_on_invalid_configuration(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Any
) -> None:
    bad = tmp_path / "pool.yaml"
    bad.write_text("agentPool:\n  agents:\n    a:\n      mode: stateful\n      affinity: none\n")
    monkeypatch.setenv("AGENT_POOL_CONFIG", str(bad))
    monkeypatch.setenv("POOL_AUTH_MODE", "development")
    with pytest.raises(SystemExit) as info:
        main.run()
    assert "configuration error" in str(info.value) and "requires affinity" in str(info.value)


def test_run_exits_on_invalid_settings(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("POOL_AUTH_MODE", raising=False)
    monkeypatch.delenv("POOL_ENTRA_TENANT_ID", raising=False)
    monkeypatch.delenv("POOL_ENTRA_AUDIENCE", raising=False)
    with pytest.raises(SystemExit, match="POOL_ENTRA_TENANT_ID"):
        main.run()


def test_module_entrypoint_runs_main(monkeypatch: pytest.MonkeyPatch) -> None:
    called: list[bool] = []
    monkeypatch.setattr(main, "run", lambda: called.append(True))
    sys.modules.pop("hosted_agent_kit.service.__main__", None)
    runpy.run_module("hosted_agent_kit.service", run_name="__main__")
    assert called == [True]


def test_create_app_fails_fast_on_missing_configuration(
    tmp_path: Any, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.chdir(tmp_path)
    with pytest.raises(ConfigError, match="no configuration found"):
        main.create_app(make_settings())


def test_build_adapter_requires_a_project_endpoint() -> None:
    from hosted_agent_kit.runtime import build_adapter

    with pytest.raises(ConfigError, match="FOUNDRY_PROJECT_ENDPOINT"):
        build_adapter(make_settings(foundry_project_endpoint=None))


def test_build_adapter_returns_the_sdk_adapter() -> None:
    from hosted_agent_kit.adapters.foundry_sdk import SdkFoundryAdapter
    from hosted_agent_kit.runtime import build_adapter

    adapter = build_adapter(
        make_settings(foundry_project_endpoint="https://x.services.ai.azure.com/api/projects/p")
    )
    assert isinstance(adapter, SdkFoundryAdapter)


async def test_lifespan_builds_the_sdk_adapter_and_jwks_for_entra_and_cleans_up(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from asgi_lifespan import LifespanManager

    from tests.fakes.foundry import FakeFoundry

    fake = FakeFoundry()
    monkeypatch.setattr(main, "build_adapter", lambda settings: fake)
    settings = make_settings(auth_mode="entra", entra_tenant_id="t", entra_audience=["api://x"])
    app = main.create_app(settings, config=make_config({"a": {"mode": "stateless"}}, {}))
    async with LifespanManager(app):
        container = app.state.container
        assert container.ready and fake.started
        assert container.authenticator._validator is not None
    assert fake.closed and container.ready is False
    assert not container.reconciler.workers_started


async def test_otel_sink_is_skipped_when_no_agent_has_telemetry() -> None:
    from tests.fakes.clock import FakeClock
    from tests.fakes.foundry import FakeFoundry

    config = make_config({"a": {"mode": "stateless", "telemetry": {"enabled": False}}}, {})
    container = main.build_container(make_settings(), config, FakeFoundry(), FakeClock(), None)
    container.pool
    assert container.metrics_store.snapshot() == {"agents": {}}


def test_importing_the_entrypoint_module_does_not_start_the_service(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    called: list[bool] = []
    monkeypatch.setattr(main, "run", lambda: called.append(True))
    sys.modules.pop("hosted_agent_kit.service.__main__", None)
    __import__("hosted_agent_kit.service.__main__")
    assert called == []


def test_openapi_rewrite_handles_responses_without_content_or_schema() -> None:
    from hosted_agent_kit.service.api.openapi import PROBLEM_JSON, _use_problem_media_type

    schema: dict[str, Any] = {
        "paths": {
            "/x": {
                "get": {
                    "responses": {
                        "200": {"content": {"application/json": {"schema": {"$ref": "ok"}}}},
                        "401": {"description": "no body documented"},
                        "403": {"content": {"text/plain": {}}},
                        "500": {"content": {"text/plain": {"schema": {"$ref": "p"}}}},
                    }
                }
            }
        }
    }
    _use_problem_media_type(schema)
    responses = schema["paths"]["/x"]["get"]["responses"]
    assert "content" not in responses["401"]
    assert responses["403"]["content"] == {"text/plain": {}}  # nothing to move
    assert responses["500"]["content"] == {PROBLEM_JSON: {"schema": {"$ref": "p"}}}
    assert "application/json" in responses["200"]["content"]  # success untouched


def test_openapi_document_is_generated_once_and_cached() -> None:
    app = main.create_app(make_settings(), config=make_config({"a": {"mode": "stateless"}}, {}))
    first = app.openapi()
    assert app.openapi() is first
