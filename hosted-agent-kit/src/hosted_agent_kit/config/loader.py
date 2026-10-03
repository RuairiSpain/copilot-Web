"""Load, merge and validate the agent pool configuration."""

from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml
from pydantic import ValidationError

from hosted_agent_kit.config.models import (
    AGENT_NAME_PATTERN,
    AgentConfig,
    AgentPoolConfig,
    ConfigError,
    PartialAgentSettings,
    deep_merge,
)
from hosted_agent_kit.domain.enums import AffinityMode, AgentMode

ROOT_KEY = "agentPool"
SETTINGS_KEY = "hack"
# Secrets and the identity of the deployment come from the environment, never from a file.
_ENV_ONLY_SETTINGS = frozenset(
    {"session_id_key", "foundry_isolation_key", "applicationinsights_connection_string"}
)
DEFAULT_AZURE_YAML = Path("azure.yaml")


class _UniqueKeyLoader(yaml.SafeLoader):
    """Safe YAML loader that rejects duplicate mapping keys."""

    def construct_mapping(self, node: yaml.MappingNode, deep: bool = False) -> dict[Any, Any]:
        mapping = super().construct_mapping(node, deep=deep)
        keys = [self.construct_object(key_node, deep=deep) for key_node, _ in node.value]
        if len(keys) != len(set(keys)):
            duplicates = sorted({str(k) for k in keys if keys.count(k) > 1})
            raise ConfigError(f"duplicate keys are not allowed: {', '.join(duplicates)}")
        return mapping


def _format_errors(prefix: str, error: ValidationError) -> str:
    parts = []
    for item in error.errors(include_input=False, include_url=False):
        location = ".".join(str(part) for part in item["loc"])
        where = f"{prefix}.{location}" if location else prefix
        parts.append(f"{where}: {item['msg']}")
    return "; ".join(parts)


def parse_yaml(text: str, source: str) -> dict[str, Any]:
    try:
        data = yaml.load(text, Loader=_UniqueKeyLoader)  # noqa: S506  # nosec B506 - SafeLoader subclass
    except yaml.YAMLError as exc:
        raise ConfigError(f"{source}: invalid YAML: {exc}") from exc
    if not isinstance(data, dict):
        raise ConfigError(f"{source}: top level must be a mapping")
    return data


def build_config(raw: dict[str, Any], source: str = "configuration") -> AgentPoolConfig:
    """Validate a parsed document that contains an ``agentPool`` section."""
    section = raw.get(ROOT_KEY)
    if not isinstance(section, dict):
        raise ConfigError(f"{source}: missing '{ROOT_KEY}' section")
    unknown = set(section) - {"defaults", "agents"}
    if unknown:
        raise ConfigError(f"{source}: unknown {ROOT_KEY} keys: {', '.join(sorted(unknown))}")

    defaults = _validate_partial("defaults", _none_as_empty(section.get("defaults")))
    agents_raw = section.get("agents")
    if not isinstance(agents_raw, dict) or not agents_raw:
        raise ConfigError(f"{source}: at least one agent must be configured")

    agents: dict[str, AgentConfig] = {}
    for name, override_raw in agents_raw.items():
        if not isinstance(name, str) or not AGENT_NAME_PATTERN.match(name):
            raise ConfigError(f"{source}: invalid agent name '{name}'")
        override = _validate_partial(f"agents.{name}", _none_as_empty(override_raw))
        merged = deep_merge(defaults, override)
        agents[name] = _build_agent(name, merged)
    return AgentPoolConfig(agents=agents)


def _none_as_empty(raw: Any) -> Any:
    """A YAML key with no value (``a:``) means "no overrides"; other falsy values are errors."""
    return {} if raw is None else raw


def _validate_partial(where: str, raw: Any) -> dict[str, Any]:
    if not isinstance(raw, dict):
        raise ConfigError(f"{where}: must be a mapping")
    try:
        partial = PartialAgentSettings.model_validate(raw)
    except ValidationError as exc:
        raise ConfigError(_format_errors(where, exc)) from exc
    return partial.model_dump(exclude_none=True, mode="python")


def _build_agent(name: str, merged: dict[str, Any]) -> AgentConfig:
    if "mode" not in merged:
        raise ConfigError(f"agents.{name}: mode is required after merging defaults")
    stateful = merged["mode"] is AgentMode.STATEFUL
    if "affinity" not in merged:
        merged["affinity"] = AffinityMode.USER if stateful else AffinityMode.NONE
    if "adopt_unbound_sessions" not in merged:
        # A stateful session may hold one user's files. Never hand an unknown one to another
        # user unless the operator opts in.
        merged["adopt_unbound_sessions"] = not stateful
    try:
        return AgentConfig.model_validate({**merged, "name": name})
    except ValidationError as exc:
        raise ConfigError(_format_errors(f"agents.{name}", exc)) from exc


def load_config_file(path: Path) -> AgentPoolConfig:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ConfigError(f"{path}: cannot read configuration file: {exc.strerror}") from exc
    return build_config(parse_yaml(text, str(path)), source=str(path))


def load_config(explicit_path: Path | None = None, cwd: Path | None = None) -> AgentPoolConfig:
    """Load configuration.

    A standalone file (``AGENT_POOL_CONFIG``) takes precedence over the
    ``agentPool`` section of ``azure.yaml`` in the working directory.
    """
    if explicit_path is not None:
        return load_config_file(explicit_path)
    azure_yaml = (cwd / DEFAULT_AZURE_YAML) if cwd is not None else DEFAULT_AZURE_YAML
    if azure_yaml.is_file():
        return load_config_file(azure_yaml)
    raise ConfigError("no configuration found: set AGENT_POOL_CONFIG or provide azure.yaml")


def build_settings_overrides(raw: dict[str, Any], source: str = "configuration") -> dict[str, Any]:
    """The optional top-level ``hack`` section: runtime settings that may live in the file.

    Names are the ``KitSettings`` fields without the ``POOL_`` prefix. Secrets are refused here.
    """
    from hosted_agent_kit.config.settings import KitSettings

    section = raw.get(SETTINGS_KEY)
    if section is None:
        return {}
    if not isinstance(section, dict):
        raise ConfigError(f"{source}: '{SETTINGS_KEY}' must be a mapping")
    allowed = set(KitSettings.model_fields) - _ENV_ONLY_SETTINGS - {"agent_pool_config"}
    unknown = set(section) - allowed
    if unknown:
        env_only = unknown & _ENV_ONLY_SETTINGS
        if env_only:
            raise ConfigError(
                f"{source}: {SETTINGS_KEY}.{sorted(env_only)[0]} is a secret and can only be "
                "set in the environment"
            )
        raise ConfigError(
            f"{source}: unknown {SETTINGS_KEY} keys: {', '.join(sorted(map(str, unknown)))}"
        )
    return dict(section)


def load_document(path: Path) -> tuple[AgentPoolConfig, dict[str, Any]]:
    """Read one file: the pool configuration and any ``hack`` setting overrides."""
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ConfigError(f"{path}: cannot read configuration file: {exc.strerror}") from exc
    raw = parse_yaml(text, str(path))
    return build_config(raw, source=str(path)), build_settings_overrides(raw, str(path))
