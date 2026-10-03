"""Every sample app runs, every sample YAML is valid, every sample agent answers."""

from __future__ import annotations

import importlib
import importlib.util
import json
import sys
from collections.abc import Callable
from pathlib import Path
from typing import Any

import pytest
import yaml
from fastapi.testclient import TestClient

from hosted_agent_kit import Hack
from hosted_agent_kit.cli import main as cli_main
from hosted_agent_kit.config.loader import build_config, build_settings_overrides, parse_yaml
from hosted_agent_kit.config.settings import KitSettings
from hosted_agent_kit.samples import _common
from hosted_agent_kit.testing import DemoFoundry

SAMPLES = Path(_common.__file__).parent
USER = {"X-User-Id": "alice"}
ADMIN = {"X-Admin-Key": "dev-admin-key"}

Check = Callable[[Any], None]


@pytest.fixture(autouse=True)
def demo_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("FOUNDRY_PROJECT_ENDPOINT", raising=False)
    monkeypatch.delenv("POOL_SESSION_ID_KEY", raising=False)


def run(module: str, steps: list[tuple[str, str, dict[str, Any], int]]) -> list[Any]:
    app = importlib.import_module(f"hosted_agent_kit.samples.{module}").app
    results = []
    with TestClient(app) as client:
        for method, path, kwargs, expected in steps:
            response = client.request(method, path, **kwargs)
            assert response.status_code == expected, (module, path, response.text)
            results.append(response)
    return results


def json_of(response: Any) -> Any:
    return response.json()


# --------------------------------------------------------------------------- response


def test_basic_ask() -> None:
    ask, agents = run(
        "response.basic_ask",
        [
            ("POST", "/ask", {"json": {"message": "hi"}, "headers": USER}, 200),
            ("GET", "/agents", {}, 200),
        ],
    )
    assert json_of(ask)["answer"]["output_text"] == "Echo from support-bot: hi"
    assert json_of(agents)[0]["name"] == "support-bot"
    assert json_of(agents)[0]["stateful"] is False


def test_user_header_is_required() -> None:
    (response,) = run("response.basic_ask", [("POST", "/ask", {"json": {"message": "hi"}}, 401)])
    assert json_of(response)["error_code"] == "SAMPLE_USER_REQUIRED"


def test_streaming() -> None:
    stream, collected = run(
        "response.streaming",
        [
            ("POST", "/chat/stream", {"json": {"message": "hello there"}, "headers": USER}, 200),
            ("POST", "/chat/collect", {"json": {"message": "hello there"}, "headers": USER}, 200),
        ],
    )
    assert stream.headers["content-type"].startswith("text/event-stream")
    assert "response.output_text.delta" in stream.text
    assert "response.completed" in stream.text
    assert "Echo" in json_of(collected)["raw_events"]


def test_conversations_keep_separate_sessions() -> None:
    first, again, other, default = run(
        "response.conversations",
        [
            (
                "POST",
                "/conversations/trip/messages",
                {"json": {"message": "a"}, "headers": USER},
                200,
            ),
            (
                "POST",
                "/conversations/trip/messages",
                {"json": {"message": "b"}, "headers": USER},
                200,
            ),
            (
                "POST",
                "/conversations/work/messages",
                {"json": {"message": "c"}, "headers": USER},
                200,
            ),
            ("POST", "/messages", {"json": {"message": "d"}, "headers": USER}, 200),
        ],
    )

    def turn(response: Any) -> int:
        return int(json_of(response)["reply"]["turn_in_session"])

    assert (turn(first), turn(again)) == (1, 2)  # the same conversation reuses its session
    assert turn(other) == 1  # another conversation starts a new one
    assert turn(default) == 1


def test_idempotent_requests_replay() -> None:
    headers = {**USER, "Idempotency-Key": "order-42"}
    body = {"message": "summarise"}
    first, second, different = run(
        "response.idempotent_requests",
        [
            ("POST", "/orders/summary", {"json": body, "headers": headers}, 200),
            ("POST", "/orders/summary", {"json": body, "headers": headers}, 200),
            ("POST", "/orders/summary", {"json": {"message": "other"}, "headers": headers}, 422),
        ],
    )
    assert json_of(first)["replayed"] is False
    assert json_of(second)["replayed"] is True
    assert json_of(second)["answer"] == json_of(first)["answer"]
    assert json_of(different)["error_code"] == "IDEMPOTENCY_KEY_REUSED"


def test_request_options() -> None:
    (response,) = run(
        "response.request_options",
        [
            (
                "POST",
                "/respond",
                {
                    "json": {
                        "messages": [{"role": "user", "content": "hi"}],
                        "metadata": {"k": "v"},
                        "timeout_seconds": 30,
                    },
                    "headers": USER,
                },
                200,
            )
        ],
    )
    assert json_of(response)["status"] == "completed"


# ---------------------------------------------------------------------------- invoke


def test_json_invocation_round_trips() -> None:
    (response,) = run(
        "invoke.json_invocation",
        [("POST", "/summarise", {"json": {"title": "Q3"}, "headers": USER}, 200)],
    )
    assert json_of(response) == {"title": "Q3"}  # the demo agent echoes the body


def test_file_upload_relays_content_type_and_bytes() -> None:
    (response,) = run(
        "invoke.file_upload",
        [
            (
                "POST",
                "/process",
                {"content": b"\x00\x01binary", "headers": {**USER, "content-type": "image/png"}},
                200,
            )
        ],
    )
    assert response.headers["content-type"] == "image/png"
    assert response.content == b"\x00\x01binary"


def test_passthrough_rejects_responses_agents_and_unknown_agents() -> None:
    ok, unknown = run(
        "invoke.passthrough",
        [
            ("POST", "/raw/doc-processor", {"content": b"hello", "headers": USER}, 200),
            ("POST", "/raw/nobody", {"content": b"hello", "headers": USER}, 404),
        ],
    )
    assert ok.content == b"hello"
    assert json_of(unknown)["error_code"] == "AGENT_NOT_CONFIGURED"


# ------------------------------------------------------------------------- reporting


def test_pool_status() -> None:
    status, router, events, pool = run(
        "reporting.pool_status",
        [
            ("GET", "/status", {}, 200),
            ("GET", "/reporting/agents", {}, 200),
            ("GET", "/reporting/events", {}, 200),
            ("GET", "/reporting/agents/support-bot", {}, 200),
        ],
    )
    assert {row["agent"] for row in json_of(status)} == {"support-bot", "memory-bot"}
    assert {row["agent_name"] for row in json_of(router)} == {"support-bot", "memory-bot"}
    assert isinstance(json_of(events), list)
    assert json_of(pool)["spec"]["max_sessions"] == 10


def test_events_and_metrics() -> None:
    _, events, metrics, text = run(
        "reporting.events_and_metrics",
        [
            ("POST", "/ping", {"headers": USER}, 200),
            ("GET", "/events", {"params": {"agent": "support-bot", "limit": 5}}, 200),
            ("GET", "/metrics", {}, 200),
            ("GET", "/metrics.txt", {}, 200),
        ],
    )
    assert any(e["reason"] == "Created" for e in json_of(events))
    assert isinstance(json_of(metrics), dict) and json_of(metrics)
    assert 'pool_requests_total{agent="support-bot",outcome="ok"} 1' in text.text


def test_session_inspector_redacts_identities_by_default() -> None:
    _, hidden, revealed = run(
        "reporting.session_inspector",
        [
            ("POST", "/seed", {"headers": USER}, 200),
            ("GET", "/sessions", {}, 200),
            ("GET", "/sessions/revealed", {}, 200),
        ],
    )
    assert json_of(hidden)[0]["user"].startswith("u_")
    assert json_of(revealed)[0]["user"] == "alice"


# --------------------------------------------------------------------- administrative


def test_admin_api_needs_the_key() -> None:
    denied, listing, sessions = run(
        "administrative.admin_api",
        [
            ("GET", "/ops/agents", {}, 403),
            ("GET", "/ops/agents", {"headers": ADMIN}, 200),
            ("GET", "/ops/agents/memory-bot/sessions", {"headers": ADMIN}, 200),
        ],
    )
    assert json_of(denied)["error_code"] == "SAMPLE_ADMIN_KEY_REQUIRED"
    assert len(json_of(listing)) == 2
    assert json_of(sessions) == []


def test_capacity_control() -> None:
    warm, sync, reload = run(
        "administrative.capacity_control",
        [
            ("POST", "/capacity/support-bot/warm", {"headers": ADMIN}, 200),
            ("POST", "/capacity/support-bot/sync", {"headers": ADMIN}, 200),
            ("POST", "/config/reload", {"headers": ADMIN}, 200),
        ],
    )
    assert json_of(warm) == {"created": True}
    assert json_of(sync)["agent_name"] == "support-bot"
    assert json_of(reload) == {"changed": {}}


def test_session_cleanup_deletes_idle_sessions() -> None:
    _, deleted, after = run(
        "administrative.session_cleanup",
        [
            ("POST", "/seed", {"headers": USER}, 200),
            ("DELETE", "/sessions/support-bot/idle", {"headers": ADMIN}, 200),
            ("DELETE", "/sessions/support-bot/idle", {"headers": ADMIN}, 200),
        ],
    )
    removed = json_of(deleted)["removed"]
    assert len(removed) >= 1
    # The pool keeps warm sessions, so it replaces what was deleted with new ones.
    assert not set(removed) & set(json_of(after)["removed"])


def test_version_rollout() -> None:
    (response,) = run(
        "administrative.version_rollout", [("GET", "/rollout", {"headers": ADMIN}, 200)]
    )
    rows = {row["agent"]: row for row in json_of(response)}
    assert rows["support-bot"]["pinned_version"] == "7"
    assert rows["support-bot"]["drain"] == "unbound"
    assert rows["memory-bot"]["drain"] == "never"


# ------------------------------------------------------------------- miscellaneous


def test_error_handling_uses_the_applications_format() -> None:
    (response,) = run(
        "miscellaneous.error_handling",
        [("POST", "/ask/nobody", {"params": {"message": "hi"}, "headers": USER}, 404)],
    )
    body = json_of(response)
    assert body["problem"] == "AGENT_NOT_CONFIGURED" and body["safe_to_retry"] is True


def test_custom_scheduler_plugin_runs() -> None:
    first, second = run(
        "miscellaneous.custom_scheduler_plugin",
        [
            ("POST", "/ask", {"params": {"message": "a"}, "headers": USER}, 200),
            ("POST", "/ask", {"params": {"message": "b"}, "headers": USER}, 200),
        ],
    )
    assert json_of(first)["status"] == "completed" and json_of(second)["status"] == "completed"


def test_custom_lifespan_readiness() -> None:
    ready, ask = run(
        "miscellaneous.custom_lifespan",
        [
            ("GET", "/ready", {}, 200),
            ("POST", "/ask", {"params": {"message": "hi"}, "headers": USER}, 200),
        ],
    )
    assert json_of(ready)["status"] == "ready"
    assert json_of(ask)["output_text"] == "Echo from support-bot: hi"


def test_configuration_in_code() -> None:
    (response,) = run(
        "miscellaneous.configuration_in_code",
        [("POST", "/ask", {"params": {"message": "hi"}, "headers": USER}, 200)],
    )
    assert json_of(response)["status"] == "completed"


def test_multi_agent_gateway_picks_the_protocol() -> None:
    agents, chat, doc = run(
        "miscellaneous.multi_agent_gateway",
        [
            ("GET", "/agents", {}, 200),
            ("POST", "/call/support-bot", {"content": b"hello", "headers": USER}, 200),
            ("POST", "/call/doc-processor", {"content": b"raw bytes", "headers": USER}, 200),
        ],
    )
    assert {a["protocol"] for a in json_of(agents)} == {"responses", "invocations"}
    assert json_of(chat)["output_text"] == "Echo from support-bot: hello"
    assert doc.content == b"raw bytes"


def test_the_testing_sample_passes() -> None:
    module = importlib.import_module("hosted_agent_kit.samples.miscellaneous.testing_your_app")
    module.test_the_endpoint_returns_the_agents_answer()
    module.test_a_foundry_outage_is_reported_as_service_unavailable()


def test_every_python_sample_has_a_test_here() -> None:
    covered = Path(__file__).read_text()
    for path in sorted(SAMPLES.glob("*/*.py")):
        if path.name == "__init__.py":
            continue
        assert path.stem in covered, f"no test mentions {path.relative_to(SAMPLES)}"


# --------------------------------------------------------------------------- YAML

CONFIG_FILES = sorted((SAMPLES / "config").glob("*.yaml"))
SCHEDULER_FILES = sorted((SAMPLES / "agents").glob("*/scheduler.yaml"))
AZURE_FILES = sorted((SAMPLES / "agents").glob("*/azure.yaml"))


@pytest.mark.parametrize(
    "path", [*CONFIG_FILES, *SCHEDULER_FILES], ids=lambda p: p.name + p.parent.name
)
def test_scheduler_yaml_is_valid(path: Path) -> None:
    raw = parse_yaml(path.read_text(encoding="utf-8"), str(path))
    config = build_config(raw, str(path))
    overrides = build_settings_overrides(raw, str(path))
    if overrides:
        KitSettings(**overrides)
    assert config.agents


@pytest.mark.parametrize(
    "path", CONFIG_FILES + SCHEDULER_FILES, ids=lambda p: p.name + p.parent.name
)
async def test_scheduler_yaml_starts_a_kit(path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("POOL_USER_ISOLATION_SECRET", "x" * 32)
    from hosted_agent_kit.plugins import default_plugins
    from hosted_agent_kit.samples.miscellaneous import custom_scheduler_plugin as custom

    plugins = custom.plugins if path.name == "custom-plugins.yaml" else default_plugins()
    kit = Hack.from_yaml(path, adapter=DemoFoundry(), scheduler_plugins=plugins)
    async with kit:
        assert kit.ready
        assert [info.name for info in kit.list_agents()] == kit.agent_names


def test_kitchen_sink_uses_every_agent_setting() -> None:
    from hosted_agent_kit.config.models import AgentConfig

    raw = yaml.safe_load((SAMPLES / "config" / "kitchen-sink.yaml").read_text())
    used = set(raw["agentPool"]["agents"]["kitchen-sink-agent"])
    expected = set(AgentConfig.model_fields) - {"name"}
    assert used == expected, f"missing from kitchen-sink.yaml: {sorted(expected - used)}"


def test_hack_settings_sample_names_every_file_setting() -> None:
    from hosted_agent_kit.config.loader import _ENV_ONLY_SETTINGS

    raw = yaml.safe_load((SAMPLES / "config" / "hack-settings.yaml").read_text())
    allowed = set(KitSettings.model_fields) - _ENV_ONLY_SETTINGS - {"agent_pool_config"}
    omitted = allowed - set(raw["hack"])
    # Connection and logging values are deployment details; everything else is shown.
    assert omitted <= {"foundry_project_endpoint", "log_level", "service_name"}, sorted(omitted)


@pytest.mark.parametrize("path", AZURE_FILES, ids=lambda p: p.parent.name)
def test_agent_azure_yaml_names_a_hosted_agent(path: Path) -> None:
    doc = yaml.safe_load(path.read_text())
    agent = doc["services"][path.parent.name]
    assert doc["name"] == path.parent.name
    assert agent["host"] == "azure.ai.agent" and agent["config"]["kind"] == "hosted"
    assert doc["services"]["ai-project"]["host"] == "azure.ai.project"
    scheduler = yaml.safe_load((path.parent / "scheduler.yaml").read_text())
    (scheduled,) = scheduler["agentPool"]["agents"]
    assert scheduled == doc["name"]  # the pool's agent name is the Foundry agent name
    protocol = scheduler["agentPool"]["agents"][scheduled]["protocol"]
    assert agent["config"]["protocols"] == [protocol]


def test_azure_embedded_sample_loads_as_an_application_azure_yaml(tmp_path: Path) -> None:
    kit = Hack.from_yaml(SAMPLES / "config" / "azure-embedded.yaml", adapter=DemoFoundry())
    assert kit.agent_names == ["support-bot"]
    assert kit.settings.default_timeout_seconds == 90


# ------------------------------------------------------------------------- agents


def load_agent(name: str) -> Any:
    path = SAMPLES / "agents" / name / "agent.py"
    spec = importlib.util.spec_from_file_location(f"sample_agent_{name.replace('-', '_')}", path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.mark.parametrize("name", ["support-bot", "memory-bot", "doc-processor"])
def test_agent_files_are_complete(name: str) -> None:
    folder = SAMPLES / "agents" / name
    for file in ("agent.py", "azure.yaml", "scheduler.yaml", "Dockerfile", "requirements.txt"):
        assert (folder / file).is_file(), f"{name}/{file}"
    assert "8088" in (folder / "Dockerfile").read_text()  # the hosted-agent contract port
    with TestClient(load_agent(name).app) as client:
        assert client.get("/readiness").status_code == 200


def test_support_bot_agent_answers_and_streams() -> None:
    client = TestClient(load_agent("support-bot").app)
    reply = client.post("/responses", json={"input": "What is your refund policy?"}).json()
    assert "Refunds" in reply["output_text"] and reply["status"] == "completed"
    streamed = client.post("/responses", json={"input": "shipping?", "stream": True})
    assert streamed.headers["content-type"].startswith("text/event-stream")
    assert "response.completed" in streamed.text


def test_memory_bot_agent_remembers(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MEMORY_PATH", str(tmp_path / "memory.txt"))
    client = TestClient(load_agent("memory-bot").app)
    assert (
        client.post("/responses", json={"input": "remember I like trains"}).json()["output_text"]
        == "Noted."
    )
    recalled = client.post("/responses", json={"input": "What do you remember?"}).json()
    assert "I like trains" in recalled["output_text"]


def test_doc_processor_agent_summarises_any_body() -> None:
    client = TestClient(load_agent("doc-processor").app)
    text = client.post(
        "/invocations", content=b"one two three", headers={"content-type": "text/plain"}
    )
    assert json.loads(text.text)["words"] == 3
    as_json = client.post("/invocations", json={"a": "b c"})
    assert json.loads(as_json.text)["content_type"].startswith("application/json")


# ----------------------------------------------------------------------------- CLI


def test_cli_validate_and_samples(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    good = str(SAMPLES / "config" / "minimal.yaml")
    assert cli_main(["validate", good]) == 0
    assert "1 agent(s): support-bot" in capsys.readouterr().out

    bad = tmp_path / "bad.yaml"
    bad.write_text("agentPool:\n  agents:\n    a:\n      mode: stateless\n      max_sessoins: 3\n")
    assert cli_main(["validate", str(bad)]) == 1
    assert "max_sessoins" in capsys.readouterr().err

    assert cli_main(["samples", "list"]) == 0
    assert "response/basic_ask.py" in capsys.readouterr().out
    target = tmp_path / "copy"
    assert cli_main(["samples", "copy", str(target)]) == 0
    assert (target / "agents" / "support-bot" / "azure.yaml").is_file()
    assert cli_main(["samples", "copy", str(target)]) == 1  # refuses a non-empty target
    assert cli_main(["samples", "copy", str(target), "--force"]) == 0


def test_cli_version(capsys: pytest.CaptureFixture[str]) -> None:
    with pytest.raises(SystemExit) as info:
        cli_main(["--version"])
    assert info.value.code == 0
    assert "hosted-agent-kit" in capsys.readouterr().out


def test_cli_serve_runs_the_service(monkeypatch: pytest.MonkeyPatch) -> None:
    called: list[bool] = []
    monkeypatch.setattr("hosted_agent_kit.service.main.run", lambda: called.append(True))
    assert cli_main(["serve"]) == 0
    assert called == [True]


def test_cli_samples_copy_needs_a_destination(capsys: pytest.CaptureFixture[str]) -> None:
    assert cli_main(["samples", "copy"]) == 2
    assert "destination" in capsys.readouterr().err


def test_cli_validate_reports_unreadable_and_invalid_settings(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    assert cli_main(["validate", str(tmp_path / "missing.yaml")]) == 1
    assert "cannot read" in capsys.readouterr().err
    bad = tmp_path / "bad.yaml"
    bad.write_text(
        "hack:\n  default_timeout_seconds: 500\n  max_timeout_seconds: 10\n"
        "agentPool:\n  agents:\n    a:\n      mode: stateless\n"
    )
    assert cli_main(["validate", str(bad)]) == 1
    assert "cannot exceed" in capsys.readouterr().err


# ------------------------------------------------------------------ 0.2.0 samples


def test_stream_events_sample_collects_text_and_event_names() -> None:
    (reply,) = run(
        "response.stream_events",
        [("POST", "/chat", {"json": {"message": "hi"}, "headers": USER}, 200)],
    )
    body = json_of(reply)
    assert body["complete"] is True and isinstance(body["events"], list)


def test_quota_status_sample_reports_the_budget() -> None:
    (reply,) = run("reporting.quota_status", [("GET", "/quota", {}, 200)])
    body = json_of(reply)
    assert body["kit_id"] == "team-a" and body["budget"] == 200 and body["counted"] >= 0


def test_user_isolation_sample_answers_for_both_modes() -> None:
    replies = run(
        "miscellaneous.user_isolation",
        [
            (
                "POST",
                "/ask/support-bot",
                {"json": {"message": "hi"}, "headers": USER},
                200,
            ),
            (
                "POST",
                "/ask/records-bot",
                {"json": {"message": "hi"}, "headers": USER},
                200,
            ),
        ],
    )
    assert len(replies) == 2


def test_sharded_gateway_sample_names_the_users_shard() -> None:
    (reply,) = run("miscellaneous.sharded_gateway", [("GET", "/route", {"headers": USER}, 200)])
    assert json_of(reply)["shard"] in (0, 1)


def test_entra_sample_refuses_a_call_without_a_token() -> None:
    (reply,) = run(
        "miscellaneous.entra_sign_in",
        [("POST", "/ask", {"params": {"message": "hi"}}, 401)],
    )
    assert json_of(reply)["error_code"] == "AUTHENTICATION_REQUIRED"
