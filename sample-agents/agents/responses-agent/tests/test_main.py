from __future__ import annotations

import pytest
from agent_framework import Agent
from agent_framework_foundry_hosting import ResponsesHostServer

import main as main_module
from main import create_server, make_agent_factory
from settings import ConfigError, Settings

from .conftest import StubCredential


@pytest.fixture
def settings(env: dict[str, str]) -> Settings:
    return Settings.from_env(env)


def test_factory_builds_a_fresh_agent_each_call(
    settings: Settings, credential: StubCredential
) -> None:
    factory = make_agent_factory(settings, credential)
    first, second = factory(), factory()
    assert isinstance(first, Agent) and isinstance(second, Agent)
    assert first is not second  # per-request agents keep caller identity isolated


def test_create_server_returns_a_responses_host(
    settings: Settings, credential: StubCredential
) -> None:
    assert isinstance(create_server(settings, credential), ResponsesHostServer)


def test_create_server_fails_fast_on_bad_config(monkeypatch: pytest.MonkeyPatch) -> None:
    for key in ("FOUNDRY_PROJECT_ENDPOINT", "AZURE_AI_MODEL_DEPLOYMENT_NAME", "TOOLBOX_ENDPOINT"):
        monkeypatch.delenv(key, raising=False)
    with pytest.raises(ConfigError):
        create_server()


def test_main_loads_dotenv_then_runs_the_server(
    monkeypatch: pytest.MonkeyPatch, env: dict[str, str]
) -> None:
    calls: list[str] = []

    class SpyServer:
        def run(self) -> None:
            calls.append("run")

    for key, value in env.items():
        monkeypatch.setenv(key, value)
    monkeypatch.setattr(main_module, "load_dotenv", lambda: calls.append("dotenv"))
    monkeypatch.setattr(main_module, "create_server", lambda settings: SpyServer())
    main_module.main()
    assert calls == ["dotenv", "run"]
