from __future__ import annotations

import pytest

from settings import ConfigError, Settings

from .conftest import PROJECT, TOOLBOX


def test_reads_a_complete_environment(env: dict[str, str]) -> None:
    settings = Settings.from_env(env)
    assert settings.project_endpoint == PROJECT
    assert settings.model_deployment == "gpt-5.4-mini"
    assert settings.toolbox_url == TOOLBOX
    assert settings.log_level == "INFO"


def test_reads_os_environ_by_default(monkeypatch: pytest.MonkeyPatch, env: dict[str, str]) -> None:
    for key, value in env.items():
        monkeypatch.setenv(key, value)
    assert Settings.from_env().model_deployment == "gpt-5.4-mini"


def test_falls_back_to_the_alternative_model_variable(env: dict[str, str]) -> None:
    del env["AZURE_AI_MODEL_DEPLOYMENT_NAME"]
    env["FOUNDRY_MODEL_NAME"] = "my-deployment"
    assert Settings.from_env(env).model_deployment == "my-deployment"


def test_builds_the_consumer_endpoint_from_the_toolbox_name(env: dict[str, str]) -> None:
    del env["TOOLBOX_ENDPOINT"]
    env["TOOLBOX_NAME"] = "search-and-code"
    env["FOUNDRY_PROJECT_ENDPOINT"] = PROJECT + "/"  # trailing slash is tolerated
    assert Settings.from_env(env).toolbox_url == TOOLBOX


def test_explicit_endpoint_wins_over_the_name(env: dict[str, str]) -> None:
    env["TOOLBOX_NAME"] = "other"
    assert Settings.from_env(env).toolbox_url == TOOLBOX


def test_blank_values_count_as_missing(env: dict[str, str]) -> None:
    env["AZURE_AI_MODEL_DEPLOYMENT_NAME"] = "   "
    with pytest.raises(ConfigError, match="AZURE_AI_MODEL_DEPLOYMENT_NAME"):
        Settings.from_env(env)


def test_reports_every_problem_at_once() -> None:
    with pytest.raises(ConfigError) as info:
        Settings.from_env({})
    message = str(info.value)
    assert "FOUNDRY_PROJECT_ENDPOINT" in message
    assert "AZURE_AI_MODEL_DEPLOYMENT_NAME" in message
    assert "TOOLBOX_ENDPOINT or TOOLBOX_NAME" in message


@pytest.mark.parametrize("key", ["FOUNDRY_PROJECT_ENDPOINT", "TOOLBOX_ENDPOINT"])
def test_endpoints_must_be_https(env: dict[str, str], key: str) -> None:
    env[key] = "http://insecure.example"
    with pytest.raises(ConfigError, match="https"):
        Settings.from_env(env)


def test_rejects_an_unknown_log_level(env: dict[str, str]) -> None:
    env["LOG_LEVEL"] = "loud"
    with pytest.raises(ConfigError, match="LOG_LEVEL"):
        Settings.from_env(env)


def test_log_level_is_normalised(env: dict[str, str]) -> None:
    env["LOG_LEVEL"] = "debug"
    assert Settings.from_env(env).log_level == "DEBUG"


def test_settings_are_immutable(env: dict[str, str]) -> None:
    settings = Settings.from_env(env)
    with pytest.raises(AttributeError):
        settings.model_deployment = "other"  # type: ignore[misc]
