"""Runtime configuration for the dev-team agent, read once from environment variables."""

from __future__ import annotations

import logging
import os
import tempfile
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path

MIN_TIMEOUT_SECONDS = 10
MAX_TIMEOUT_SECONDS = 1800


class ConfigError(ValueError):
    """The environment is missing or has invalid configuration."""


@dataclass(frozen=True, slots=True)
class Settings:
    """Configuration for the dev-team agent.

    Attributes:
        project_endpoint: Foundry project endpoint. Injected by the platform when hosted.
        model_deployment: Name of the model deployment.
        workspace_root: Folder that holds the project's git repository.
        command_timeout_seconds: Seconds before an agent shell command is killed.
        max_output_chars: Longest command output returned to a model.
        max_round_count: Upper bound on Magentic rounds in one build.
        max_stall_count: Rounds without progress before the manager re-plans.
        max_reset_count: How many times the manager may re-plan from scratch.
        log_level: Standard logging level name.
    """

    project_endpoint: str
    model_deployment: str
    workspace_root: Path
    command_timeout_seconds: int = 180
    max_output_chars: int = 20_000
    max_round_count: int = 12
    max_stall_count: int = 3
    max_reset_count: int = 1
    log_level: str = "INFO"

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> Settings:
        """Build settings from ``env`` (defaults to ``os.environ``).

        Raises:
            ConfigError: listing every problem at once.
        """
        source = os.environ if env is None else env

        def get(name: str) -> str | None:
            value = source.get(name, "").strip()
            return value or None

        problems: list[str] = []

        endpoint = get("FOUNDRY_PROJECT_ENDPOINT")
        if endpoint is None:
            problems.append("FOUNDRY_PROJECT_ENDPOINT is not set")
        elif not endpoint.startswith("https://"):
            problems.append("FOUNDRY_PROJECT_ENDPOINT must start with https://")

        model = get("AZURE_AI_MODEL_DEPLOYMENT_NAME") or get("FOUNDRY_MODEL_NAME")
        if model is None:
            problems.append("AZURE_AI_MODEL_DEPLOYMENT_NAME is not set")

        def integer(name: str, default: int, low: int, high: int) -> int:
            raw = get(name)
            if raw is None:
                return default
            try:
                value = int(raw)
            except ValueError:
                problems.append(f"{name} must be a whole number, got {raw!r}")
                return default
            if not low <= value <= high:
                problems.append(f"{name} must be between {low} and {high}, got {value}")
                return default
            return value

        timeout = integer(
            "DEV_TEAM_COMMAND_TIMEOUT_SECONDS", 180, MIN_TIMEOUT_SECONDS, MAX_TIMEOUT_SECONDS
        )
        rounds = integer("DEV_TEAM_MAX_ROUNDS", 12, 3, 60)

        level = (get("LOG_LEVEL") or "INFO").upper()
        if level not in logging.getLevelNamesMapping():
            problems.append(f"LOG_LEVEL {level!r} is not a valid logging level")

        if problems:
            raise ConfigError("Invalid configuration:\n- " + "\n- ".join(problems))

        assert endpoint is not None and model is not None
        workspace = get("WORKSPACE_ROOT") or str(
            Path(source.get("HOME") or tempfile.gettempdir()) / "workspace"
        )
        return cls(
            project_endpoint=endpoint,
            model_deployment=model,
            workspace_root=Path(workspace).expanduser().resolve(),
            command_timeout_seconds=timeout,
            max_round_count=rounds,
            log_level=level,
        )
