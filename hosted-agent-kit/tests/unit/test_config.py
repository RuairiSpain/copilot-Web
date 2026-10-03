"""Configuration: merge, validation, precedence and settings."""

from __future__ import annotations

from pathlib import Path
from textwrap import dedent
from typing import Any

import pytest

from hosted_agent_kit.config.loader import (
    build_config,
    load_config,
    load_config_file,
    parse_yaml,
)
from hosted_agent_kit.config.models import ConfigError, deep_merge
from hosted_agent_kit.config.settings import Settings
from hosted_agent_kit.domain.enums import AffinityMode, AgentMode, SchedulerStrategy


def cfg(section: dict[str, Any]) -> Any:
    return build_config({"agentPool": section})


def test_defaults_merge_and_overrides_win() -> None:
    config = cfg(
        {
            "defaults": {
                "mode": "stateless",
                "scheduler": "round_robin",
                "max_sessions": 8,
                "queue": {"max_depth": 50, "max_wait_seconds": 30},
                "telemetry": {"enabled": False},
            },
            "agents": {
                "a": {},
                "b": {
                    "mode": "stateful",
                    "queue": {"max_wait_seconds": 5},
                    "telemetry": {"enabled": True},
                },
            },
        }
    )
    a, b = config.agents["a"], config.agents["b"]
    assert (a.mode, a.affinity, a.scheduler, a.max_sessions) == (
        AgentMode.STATELESS,
        AffinityMode.NONE,
        SchedulerStrategy.ROUND_ROBIN,
        8,
    )
    assert a.queue.max_depth == 50 and a.queue.max_wait_seconds == 30
    assert a.telemetry.enabled is False
    assert b.affinity is AffinityMode.USER
    assert b.queue.max_depth == 50 and b.queue.max_wait_seconds == 5  # nested merge
    assert b.telemetry.enabled is True
    assert config.names == ["a", "b"] and config.get("zzz") is None


def test_built_in_defaults_apply() -> None:
    a = cfg({"agents": {"a": {"mode": "stateless"}}}).agents["a"]
    assert (a.max_sessions, a.min_warm_sessions, a.sync_interval_seconds, a.create_retries) == (
        10,
        0,
        60,
        3,
    )
    assert a.queue.enabled and a.queue.max_depth == 500 and a.queue.max_wait_seconds == 120
    assert a.telemetry.enabled and a.scheduler is SchedulerStrategy.FIRST_AVAILABLE


def test_none_values_are_treated_as_unset() -> None:
    a = cfg(
        {
            "defaults": {"mode": "stateless", "max_sessions": 4},
            "agents": {"a": {"max_sessions": None}},
        }
    ).agents["a"]
    assert a.max_sessions == 4


def test_explicit_matching_affinity_is_accepted() -> None:
    a = cfg({"agents": {"a": {"mode": "stateful", "affinity": "user"}}}).agents["a"]
    assert a.stateful


@pytest.mark.parametrize(
    ("section", "needle"),
    [
        ({"agents": {"a": {"mode": "stateful", "affinity": "none"}}}, "requires affinity"),
        ({"agents": {"a": {"mode": "stateless", "affinity": "user"}}}, "requires affinity"),
        (
            {"defaults": {"affinity": "none"}, "agents": {"a": {"mode": "stateful"}}},
            "requires affinity",
        ),
        (
            {"agents": {"a": {"mode": "stateless", "min_warm_sessions": 5, "max_sessions": 2}}},
            "min_warm_sessions",
        ),
        (
            {"agents": {"a": {"mode": "stateless", "queue": {"enabled": True, "max_depth": 0}}}},
            "max_depth",
        ),
        ({"agents": {"a": {}}}, "mode is required"),
        ({"agents": {"a": {"mode": "weird"}}}, "agents.a.mode"),
        ({"agents": {"a": {"mode": "stateless", "unknown_key": 1}}}, "unknown_key"),
        ({"agents": {"a": {"mode": "stateless", "queue": {"nope": 1}}}}, "nope"),
        ({"defaults": {"mode": "stateless", "bogus": 1}, "agents": {"a": {}}}, "defaults.bogus"),
        ({"agents": {"a": {"mode": "stateless", "max_sessions": 0}}}, "max_sessions"),
        (
            {"agents": {"a": {"mode": "stateless", "sync_interval_seconds": 1}}},
            "sync_interval_seconds",
        ),
        ({"agents": {"a": {"mode": "stateless", "create_retries": 11}}}, "create_retries"),
        ({"agents": {"a": {"mode": "stateless", "scheduler": "random"}}}, "scheduler"),
        ({"agents": {"../evil": {"mode": "stateless"}}}, "invalid agent name"),
        ({"agents": {"has space": {"mode": "stateless"}}}, "invalid agent name"),
        ({"agents": {1: {"mode": "stateless"}}}, "invalid agent name"),
        ({"agents": {}}, "at least one agent"),
        ({"agents": None}, "at least one agent"),
        ({}, "at least one agent"),
        ({"agents": {"a": {"mode": "stateless"}}, "extra": 1}, "unknown agentPool keys"),
        ({"defaults": [], "agents": {"a": {"mode": "stateless"}}}, "defaults: must be a mapping"),
        ({"agents": {"a": "oops"}}, "agents.a: must be a mapping"),
    ],
)
def test_invalid_configuration_is_rejected(section: dict[str, Any], needle: str) -> None:
    with pytest.raises(ConfigError, match=needle):
        cfg(section)


def test_missing_root_section_is_rejected() -> None:
    with pytest.raises(ConfigError, match="missing 'agentPool'"):
        build_config({"other": 1})


def test_duplicate_agent_names_are_rejected() -> None:
    text = dedent(
        """
        agentPool:
          agents:
            a: {mode: stateless}
            a: {mode: stateful}
        """
    )
    with pytest.raises(ConfigError, match=r"duplicate keys.*a"):
        parse_yaml(text, "t")


def test_duplicate_keys_nested_are_rejected() -> None:
    with pytest.raises(ConfigError, match="duplicate"):
        parse_yaml(
            "agentPool:\n  agents:\n    a:\n      mode: stateless\n      mode: stateful\n", "t"
        )


def test_invalid_yaml_and_non_mapping_documents_are_rejected() -> None:
    with pytest.raises(ConfigError, match="invalid YAML"):
        parse_yaml("a: [unclosed", "t")
    with pytest.raises(ConfigError, match="top level must be a mapping"):
        parse_yaml("- a\n- b\n", "t")


def test_yaml_tags_cannot_execute_code() -> None:
    with pytest.raises(ConfigError, match="invalid YAML"):
        parse_yaml("a: !!python/object/apply:os.system ['echo hi']\n", "t")


def write(path: Path, body: str) -> Path:
    path.write_text(dedent(body), encoding="utf-8")
    return path


AZURE = """
name: x
agentPool:
  agents:
    from-azure:
      mode: stateless
"""
STANDALONE = """
agentPool:
  agents:
    from-standalone:
      mode: stateful
"""


def test_standalone_file_takes_precedence_over_azure_yaml(tmp_path: Path) -> None:
    write(tmp_path / "azure.yaml", AZURE)
    standalone = write(tmp_path / "pool.yaml", STANDALONE)
    config = load_config(standalone, cwd=tmp_path)
    assert config.names == ["from-standalone"]


def test_azure_yaml_is_used_when_no_standalone_file(tmp_path: Path) -> None:
    write(tmp_path / "azure.yaml", AZURE)
    assert load_config(None, cwd=tmp_path).names == ["from-azure"]


def test_azure_yaml_in_current_directory_by_default(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    write(tmp_path / "azure.yaml", AZURE)
    monkeypatch.chdir(tmp_path)
    assert load_config().names == ["from-azure"]


def test_no_configuration_found(tmp_path: Path) -> None:
    with pytest.raises(ConfigError, match="no configuration found"):
        load_config(None, cwd=tmp_path)


def test_unreadable_file(tmp_path: Path) -> None:
    with pytest.raises(ConfigError, match="cannot read"):
        load_config_file(tmp_path / "missing.yaml")


def test_environment_variable_selects_standalone_file(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    standalone = write(tmp_path / "pool.yaml", STANDALONE)
    monkeypatch.setenv("AGENT_POOL_CONFIG", str(standalone))
    monkeypatch.setenv("POOL_AUTH_MODE", "development")
    settings = Settings()
    assert settings.agent_pool_config == standalone
    assert load_config(settings.agent_pool_config, cwd=tmp_path).names == ["from-standalone"]


def test_deep_merge_does_not_mutate_inputs() -> None:
    base = {"queue": {"a": 1, "b": 2}, "x": 1}
    override = {"queue": {"b": 3}, "x": 2}
    merged = deep_merge(base, override)
    assert merged == {"queue": {"a": 1, "b": 3}, "x": 2}
    assert base == {"queue": {"a": 1, "b": 2}, "x": 1}


def test_example_configuration_file_is_valid() -> None:
    path = Path(__file__).resolve().parents[2] / "agent-pool.example.yaml"
    config = load_config_file(path)
    assert {"coding-agent", "research-agent"} <= set(config.names)


# ---------------------------------------------------------------- settings


def test_entra_mode_requires_tenant_and_audience(monkeypatch: pytest.MonkeyPatch) -> None:
    for name in ("POOL_ENTRA_TENANT_ID", "POOL_ENTRA_AUDIENCE", "POOL_AUTH_MODE"):
        monkeypatch.delenv(name, raising=False)
    with pytest.raises(ConfigError, match="POOL_ENTRA_TENANT_ID"):
        Settings()


def test_audience_is_split_on_commas(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("POOL_ENTRA_TENANT_ID", "tenant")
    monkeypatch.setenv("POOL_ENTRA_AUDIENCE", "api://one, api://two ,")
    assert Settings().entra_audience == ["api://one", "api://two"]


def test_audience_accepts_a_list_value() -> None:
    settings = Settings(entra_tenant_id="t", entra_audience=["a"])  # type: ignore[arg-type]
    assert settings.entra_audience == ["a"]


def test_default_timeout_cannot_exceed_maximum() -> None:
    with pytest.raises(ConfigError, match="DEFAULT_TIMEOUT"):
        Settings(auth_mode="development", default_timeout_seconds=100, max_timeout_seconds=50)


def test_environment_overrides_use_pool_prefix(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("POOL_AUTH_MODE", "development")
    monkeypatch.setenv("POOL_MAX_BODY_BYTES", "2048")
    monkeypatch.setenv("FOUNDRY_PROJECT_ENDPOINT", "https://x.services.ai.azure.com/api/projects/p")
    settings = Settings()
    assert settings.max_body_bytes == 2048
    assert settings.foundry_project_endpoint and settings.foundry_project_endpoint.endswith("/p")


def test_adoption_default_depends_on_mode_and_can_be_overridden() -> None:
    config = cfg(
        {
            "agents": {
                "stateless": {"mode": "stateless"},
                "stateful": {"mode": "stateful"},
                "stateful-adopting": {"mode": "stateful", "adopt_unbound_sessions": True},
                "stateless-ignoring": {"mode": "stateless", "adopt_unbound_sessions": False},
                "unset": {"mode": "stateful", "adopt_unbound_sessions": None},
            }
        }
    )
    got = {name: agent.adopt_unbound_sessions for name, agent in config.agents.items()}
    assert got == {
        "stateless": True,
        "stateful": False,
        "stateful-adopting": True,
        "stateless-ignoring": False,
        "unset": False,
    }


def test_adoption_can_be_set_in_defaults_and_overridden_per_agent() -> None:
    config = cfg(
        {
            "defaults": {"mode": "stateful", "adopt_unbound_sessions": True},
            "agents": {"a": {}, "b": {"adopt_unbound_sessions": False}},
        }
    )
    assert config.agents["a"].adopt_unbound_sessions is True
    assert config.agents["b"].adopt_unbound_sessions is False


def test_adoption_must_be_a_boolean() -> None:
    with pytest.raises(ConfigError, match="adopt_unbound_sessions"):
        cfg({"agents": {"a": {"mode": "stateless", "adopt_unbound_sessions": "maybe"}}})


def test_circuit_breaker_defaults_overrides_and_merging() -> None:
    config = cfg(
        {
            "defaults": {"mode": "stateless", "circuit_breaker": {"failure_threshold": 9}},
            "agents": {
                "a": {},
                "b": {"circuit_breaker": {"open_seconds": 5, "half_open_max_calls": 3}},
                "c": {"circuit_breaker": {"enabled": False}},
            },
        }
    )
    a, b, c = (config.agents[n].circuit_breaker for n in "abc")
    assert (a.enabled, a.failure_threshold, a.open_seconds, a.half_open_max_calls) == (
        True,
        9,
        30,
        1,
    )
    assert (b.failure_threshold, b.open_seconds, b.half_open_max_calls) == (9, 5, 3)
    assert c.enabled is False


@pytest.mark.parametrize(
    "bad",
    [{"failure_threshold": 0}, {"open_seconds": 0}, {"half_open_max_calls": 0}, {"nope": 1}],
)
def test_invalid_circuit_breaker_settings_are_rejected(bad: dict[str, Any]) -> None:
    with pytest.raises(ConfigError, match="circuit_breaker"):
        cfg({"agents": {"a": {"mode": "stateless", "circuit_breaker": bad}}})


def test_protocol_defaults_to_responses_and_can_be_set() -> None:
    config = cfg(
        {
            "agents": {
                "a": {"mode": "stateless"},
                "b": {"mode": "stateless", "protocol": "invocations"},
            }
        }
    )
    assert config.agents["a"].protocol.value == "responses"
    assert config.agents["b"].protocol.value == "invocations"
    with pytest.raises(ConfigError, match="protocol"):
        cfg({"agents": {"a": {"mode": "stateless", "protocol": "grpc"}}})
