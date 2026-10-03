from __future__ import annotations

from pathlib import Path

import pytest

from settings import ConfigError, Settings


def test_reads_a_minimal_environment(env: dict[str, str], tmp_path: Path) -> None:
    settings = Settings.from_env(env)
    assert settings.model_deployment == "gpt-5.4-mini"
    assert settings.workspace_root == (tmp_path / "ws").resolve()
    assert settings.command_timeout_seconds == 180
    assert settings.max_round_count == 12


def test_workspace_defaults_to_home(env: dict[str, str], tmp_path: Path) -> None:
    del env["WORKSPACE_ROOT"]
    env["HOME"] = str(tmp_path)
    assert Settings.from_env(env).workspace_root == (tmp_path / "workspace").resolve()


def test_numbers_are_parsed(env: dict[str, str]) -> None:
    env["DEV_TEAM_COMMAND_TIMEOUT_SECONDS"] = "60"
    env["DEV_TEAM_MAX_ROUNDS"] = "20"
    settings = Settings.from_env(env)
    assert (settings.command_timeout_seconds, settings.max_round_count) == (60, 20)


@pytest.mark.parametrize(
    ("name", "value"),
    [
        ("DEV_TEAM_COMMAND_TIMEOUT_SECONDS", "5"),
        ("DEV_TEAM_COMMAND_TIMEOUT_SECONDS", "99999"),
        ("DEV_TEAM_COMMAND_TIMEOUT_SECONDS", "soon"),
        ("DEV_TEAM_MAX_ROUNDS", "1"),
        ("LOG_LEVEL", "loud"),
        ("FOUNDRY_PROJECT_ENDPOINT", "http://insecure"),
    ],
)
def test_bad_values_are_reported(env: dict[str, str], name: str, value: str) -> None:
    env[name] = value
    with pytest.raises(ConfigError, match=name):
        Settings.from_env(env)


def test_every_problem_is_reported_at_once() -> None:
    with pytest.raises(ConfigError) as info:
        Settings.from_env({})
    assert "FOUNDRY_PROJECT_ENDPOINT" in str(info.value)
    assert "AZURE_AI_MODEL_DEPLOYMENT_NAME" in str(info.value)
