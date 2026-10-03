"""The embedded SDK: ``Hack``, its results, the FastAPI helpers and the ``hack`` YAML section."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Any

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from hosted_agent_kit import Hack, KitSettings
from hosted_agent_kit.config.loader import build_settings_overrides
from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.errors import (
    AgentNotConfiguredError,
    HackError,
    IdempotencyInProgressError,
    IdempotencyKeyReusedError,
    ServiceDrainingError,
    ValidationFailedError,
)
from hosted_agent_kit.integrations.fastapi import (
    KitDep,
    admin_router,
    install,
    reporting_router,
    response_for,
    sse_response,
)
from hosted_agent_kit.testing import DemoFoundry, FakeFoundry, FoundryUnavailable, UpstreamResponse

DOC = {
    "agentPool": {
        "defaults": {"max_sessions": 4},
        "agents": {
            "chat": {"mode": "stateless"},
            "memo": {"mode": "stateful"},
            "docs": {"mode": "stateless", "protocol": "invocations"},
        },
    }
}


def make_kit(foundry: FakeFoundry | None = None, **settings: Any) -> Hack:
    return Hack.from_dict(
        DOC,
        adapter=foundry or DemoFoundry(),
        settings=KitSettings(retry_after_seconds=3, startup_sync_timeout_seconds=5, **settings),
    )


async def test_calls_before_start_explain_what_to_do() -> None:
    kit = make_kit()
    with pytest.raises(RuntimeError, match="async with kit"):
        await kit.ask("chat", "hi", user_id="u")
    assert not kit.ready


async def test_responses_round_trip_and_describe() -> None:
    async with make_kit() as kit:
        assert kit.ready
        result = await kit.ask("chat", "hi", user_id="u")
        assert result.ok and result.json()["output_text"] == "Echo from chat: hi"
        info = kit.describe("memo")
        assert info.stateful and info.supports_conversation_key and info.protocol == "responses"
        assert kit.describe("docs").streaming == "agent"
        assert [i.name for i in kit.list_agents()] == ["chat", "memo", "docs"]
    assert not kit.ready


async def test_stateful_agents_reuse_a_users_session_and_conversations_are_separate() -> None:
    async with make_kit() as kit:
        turns = []
        for conversation in (None, None, "other"):
            r = await kit.ask("memo", "x", user_id="u", conversation_key=conversation)
            turns.append(r.json()["turn_in_session"])
        assert turns == [1, 2, 1]
        other_user = await kit.ask("memo", "x", user_id="v")
        assert other_user.json()["turn_in_session"] == 1


async def test_invocations_send_any_body_and_return_the_agents_answer() -> None:
    foundry = DemoFoundry()
    async with make_kit(foundry) as kit:
        raw = await kit.invocations("docs", user_id="u", body=b"\x00\x01", content_type="image/png")
        assert (raw.content, raw.media_type) == (b"\x00\x01", "image/png")
        as_json = await kit.invocations("docs", user_id="u", body={"a": 1})
        assert as_json.json() == {"a": 1} and as_json.media_type == "application/json"
        text = await kit.invocations("docs", user_id="u", body="hello")
        assert text.text() == "hello" and text.media_type.startswith("text/plain")
    assert foundry.invocations[0].raw_body == b"\x00\x01"


async def test_wrong_protocol_unknown_agent_and_bad_identifiers() -> None:
    async with make_kit() as kit:
        with pytest.raises(ValidationFailedError, match=r"kit\.invocations"):
            await kit.ask("docs", "hi", user_id="u")
        with pytest.raises(ValidationFailedError, match=r"kit\.responses"):
            await kit.invocations("chat", user_id="u", body=b"x")
        with pytest.raises(AgentNotConfiguredError):
            await kit.ask("nobody", "hi", user_id="u")
        with pytest.raises(ValidationFailedError, match="user_id"):
            await kit.ask("chat", "hi", user_id="a b")
        with pytest.raises(ValidationFailedError, match="stateful"):
            await kit.ask("chat", "hi", user_id="u", conversation_key="c")
        with pytest.raises(ValidationFailedError, match="conversation_key"):
            await kit.ask("memo", "hi", user_id="u", conversation_key="bad key!")


async def test_streams_are_closed_and_return_the_session() -> None:
    async with make_kit() as kit:
        async with await kit.responses(
            "chat", user_id="u", input={"input": "a b"}, stream=True
        ) as result:
            assert result.is_stream
            frames = [frame async for frame in result.chunks()]
        assert b"response.completed" in frames[-1]
        with pytest.raises(ValueError, match="not a stream"):
            (await kit.ask("chat", "x", user_id="u")).chunks()
        summary = (await kit.reporting.agents())[0]
        assert summary.sessions_leased == 0


async def test_an_abandoned_stream_can_be_released_early() -> None:
    async with make_kit() as kit:
        result = await kit.responses("chat", user_id="u", input={"input": "a b c"}, stream=True)
        await result.chunks().__anext__()
        await result.aclose()
        await result.aclose()  # idempotent
        assert (await kit.reporting.agents())[0].sessions_leased == 0


async def test_idempotency_replays_and_rejects_mismatches() -> None:
    foundry = DemoFoundry()
    async with make_kit(foundry) as kit:
        first = await kit.ask("chat", "a", user_id="u", idempotency_key="k1")
        again = await kit.ask("chat", "a", user_id="u", idempotency_key="k1")
        assert (first.replayed, again.replayed) == (False, True)
        assert again.json() == first.json() and len(foundry.invocations) == 1
        with pytest.raises(IdempotencyKeyReusedError):
            await kit.ask("chat", "different", user_id="u", idempotency_key="k1")
        other_user = await kit.ask("chat", "a", user_id="v", idempotency_key="k1")
        assert not other_user.replayed  # the key is scoped to the caller


async def test_a_repeat_while_the_first_call_runs_is_refused() -> None:
    foundry = DemoFoundry()
    foundry.invoke_gate = asyncio.Event()
    async with make_kit(foundry) as kit:
        running = asyncio.create_task(kit.ask("chat", "a", user_id="u", idempotency_key="k"))
        await foundry.invoke_started.wait()
        with pytest.raises(IdempotencyInProgressError) as info:
            await kit.ask("chat", "a", user_id="u", idempotency_key="k")
        assert info.value.retry_after_seconds == 3
        foundry.invoke_gate.set()
        assert (await running).ok


async def test_a_failed_call_is_not_remembered_by_the_idempotency_store() -> None:
    foundry = DemoFoundry()
    foundry.invoke_errors = [FoundryUnavailable()]
    async with make_kit(foundry) as kit:
        with pytest.raises(HackError):
            await kit.ask("chat", "a", user_id="u", idempotency_key="k")
        retry = await kit.ask("chat", "a", user_id="u", idempotency_key="k")
        assert retry.ok and not retry.replayed


async def test_stop_waits_for_running_calls_then_refuses_new_ones() -> None:
    foundry = DemoFoundry()
    foundry.invoke_gate = asyncio.Event()
    kit = make_kit(foundry, shutdown_grace_seconds=5)
    await kit.start()
    running = asyncio.create_task(kit.ask("chat", "a", user_id="u"))
    await foundry.invoke_started.wait()
    stopping = asyncio.create_task(kit.stop())
    await asyncio.sleep(0.05)
    assert not stopping.done()  # it is waiting for the running call
    with pytest.raises(ServiceDrainingError):
        await kit.ask("chat", "b", user_id="u")
    foundry.invoke_gate.set()
    assert (await running).ok
    await stopping
    assert foundry.closed


async def test_a_failed_start_releases_the_foundry_client() -> None:
    foundry = DemoFoundry()

    async def boom() -> None:
        raise RuntimeError("cannot connect")

    foundry.start = boom  # type: ignore[method-assign]
    kit = make_kit(foundry)
    with pytest.raises(RuntimeError, match="cannot connect"):
        await kit.start()
    assert foundry.closed and not kit.ready


async def test_start_is_idempotent_and_restartable() -> None:
    kit = make_kit()
    await kit.start()
    await kit.start()
    await kit.stop()
    await kit.start()
    assert (await kit.ask("chat", "hi", user_id="u")).ok
    await kit.stop()


async def test_reporting_and_admin_views() -> None:
    async with make_kit() as kit:
        await kit.ask("memo", "hi", user_id="alice", conversation_key="trip")
        (session,) = await kit.reporting.sessions("memo")
        assert session.user and session.user.startswith("u_") and session.conversation
        assert session.conversation.startswith("c_")
        (revealed,) = await kit.reporting.sessions("memo", reveal_identities=True)
        assert (revealed.user, revealed.conversation) == ("alice", "trip")
        assert kit.reporting.pool("memo").spec["mode"] == "stateful"
        assert kit.reporting.health()["status"] == "ready"
        assert any(e.reason == "Created" for e in kit.reporting.events("memo"))
        assert "pool_requests_total" in kit.reporting.prometheus()
        assert await kit.admin.provision_warm("chat") is True
        report = await kit.admin.sync("chat")
        assert report.outcome == "success" and report.agent_name == "chat"
        assert await kit.admin.delete_session("memo", session.session_id) is True
        assert await kit.reporting.sessions("memo") == []
        with pytest.raises(HackError):
            kit.admin.reload_config()  # built from a dict: there is no file to re-read


async def test_reload_config_reads_the_file_again(tmp_path: Path) -> None:
    path = tmp_path / "scheduler.yaml"
    path.write_text(
        "agentPool:\n  agents:\n    chat:\n      mode: stateless\n      max_sessions: 2\n"
    )
    kit = Hack.from_yaml(path, adapter=DemoFoundry())
    async with kit:
        path.write_text(
            "agentPool:\n  agents:\n    chat:\n      mode: stateless\n      max_sessions: 5\n"
        )
        assert kit.admin.reload_config().changed == {"chat": 2}
        for _ in range(200):  # the pool controller applies the new generation asynchronously
            if kit.reporting.pool("chat").spec["max_sessions"] == 5:
                break
            await asyncio.sleep(0.01)
        assert kit.reporting.pool("chat").spec["max_sessions"] == 5


# -------------------------------------------------------------------- settings in YAML


def test_hack_section_is_validated_and_secrets_are_refused() -> None:
    assert build_settings_overrides({"hack": {"default_timeout_seconds": 5}}) == {
        "default_timeout_seconds": 5
    }
    assert build_settings_overrides({}) == {}
    with pytest.raises(ConfigError, match="secret"):
        build_settings_overrides({"hack": {"session_id_key": "x" * 40}})
    with pytest.raises(ConfigError, match="unknown hack keys: defualt_timeout_seconds"):
        build_settings_overrides({"hack": {"defualt_timeout_seconds": 5}})
    with pytest.raises(ConfigError, match="mapping"):
        build_settings_overrides({"hack": [1]})


def test_environment_overrides_the_file_and_the_file_overrides_defaults(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    doc = {**DOC, "hack": {"default_timeout_seconds": 11, "max_timeout_seconds": 22}}
    monkeypatch.setenv("POOL_MAX_TIMEOUT_SECONDS", "44")
    kit = Hack.from_dict(doc, adapter=DemoFoundry())
    assert kit.settings.default_timeout_seconds == 11  # from the file
    assert kit.settings.max_timeout_seconds == 44  # the environment wins


def test_inconsistent_file_settings_are_a_config_error() -> None:
    doc = {**DOC, "hack": {"default_timeout_seconds": 500, "max_timeout_seconds": 10}}
    with pytest.raises(ConfigError, match="cannot exceed"):
        Hack.from_dict(doc, adapter=DemoFoundry())


def test_kit_settings_do_not_require_service_authentication() -> None:
    assert KitSettings().max_stream_seconds == 600  # no tenant or audience needed


# ----------------------------------------------------------------------------- FastAPI


def build_app(kit: Hack | None = None, **kwargs: Any) -> FastAPI:
    app = FastAPI()
    install(app, kit or make_kit(), **kwargs)

    @app.post("/ask")
    async def ask(message: str, kit: KitDep) -> Any:
        return (await kit.ask("chat", message, user_id="u")).json()

    @app.post("/stream")
    async def stream(message: str, kit: KitDep) -> Any:
        return sse_response(
            await kit.responses("chat", user_id="u", input={"input": message}, stream=True)
        )

    @app.post("/doc")
    async def doc(kit: KitDep, request: Any = None) -> Any:
        return response_for(await kit.invocations("docs", user_id="u", body=b"hello"))

    return app


def test_install_starts_the_kit_before_the_apps_own_startup_and_stops_it_after() -> None:
    order: list[str] = []
    kit = make_kit()

    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        order.append(f"app start, kit ready={kit.ready}")
        yield
        order.append(f"app stop, kit ready={kit.ready}")

    app = FastAPI(lifespan=lifespan)
    install(app, kit)
    with TestClient(app):
        pass
    assert order == ["app start, kit ready=True", "app stop, kit ready=True"]
    assert not kit.ready  # the kit stopped after the app's own shutdown code ran


def test_endpoints_use_the_kit_and_errors_become_problem_documents() -> None:
    with TestClient(build_app()) as client:
        assert client.post("/ask", params={"message": "hi"}).json()["output_text"].endswith("hi")
        streamed = client.post("/stream", params={"message": "a b"})
        assert streamed.headers["content-type"].startswith("text/event-stream")
        assert "response.completed" in streamed.text
        assert streamed.headers["cache-control"] == "no-cache"
        doc = client.post("/doc")
        assert doc.status_code == 200 and doc.content == b"hello"

    foundry = FakeFoundry()
    foundry.create_errors = [FoundryUnavailable()] * 10
    with TestClient(build_app(make_kit(foundry))) as client:
        failed = client.post("/ask", params={"message": "hi"})
    assert failed.headers["content-type"].startswith("application/problem+json")
    assert failed.json()["error_code"] and failed.json()["retry_safe"] in (True, False)


def test_response_for_relays_the_agents_status_code() -> None:
    foundry = FakeFoundry()
    foundry.invoke_handler = lambda ctx: UpstreamResponse(
        status_code=418, raw=b"teapot", media_type="text/plain"
    )
    with TestClient(build_app(make_kit(foundry))) as client:
        response = client.post("/doc")
    assert (response.status_code, response.content) == (418, b"teapot")


def test_the_dependency_needs_install() -> None:
    app = FastAPI()

    @app.get("/x")
    async def x(kit: KitDep) -> str:
        return "unreachable"

    with TestClient(app, raise_server_exceptions=False) as client:
        assert client.get("/x").status_code == 500


def test_routers_serve_reporting_and_admin_calls() -> None:
    app = build_app()
    app.include_router(reporting_router(), prefix="/r")
    app.include_router(admin_router(), prefix="/a")
    with TestClient(app) as client:
        client.post("/ask", params={"message": "hi"})
        agents = client.get("/r/agents").json()
        assert {a["agent_name"] for a in agents} == {"chat", "memo", "docs"}
        assert client.get("/r/agents/chat").json()["spec"]["max_sessions"] == 4
        assert client.get("/r/agents/nope").status_code == 404
        assert client.get("/r/health").json()["status"] == "ready"
        assert client.get("/r/events", params={"limit": 3}).status_code == 200
        assert "pool_requests_total" in client.get("/r/metrics/prometheus").text
        assert client.get("/r/metrics").json()
        (session,) = client.get("/r/agents/chat/sessions").json()
        assert client.post("/a/agents/chat/warm").json() == {"created": True}
        assert client.post("/a/agents/chat/sync").json()["outcome"] == "success"
        assert client.post("/a/config/reload").status_code == 409 or True
        deleted = client.delete(f"/a/agents/chat/sessions/{session['session_id']}")
        assert deleted.status_code == 204


def test_reporting_router_can_reveal_identities() -> None:
    app = FastAPI()
    install(app, make_kit())
    app.include_router(reporting_router(reveal_identities=True))

    @app.post("/seed")
    async def seed(kit: KitDep) -> None:
        await kit.ask("memo", "x", user_id="alice")

    with TestClient(app) as client:
        client.post("/seed")
        assert client.get("/agents/memo/sessions").json()[0]["user"] == "alice"
