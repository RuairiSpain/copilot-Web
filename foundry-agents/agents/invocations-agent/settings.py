"""Runtime configuration, read once from environment variables.

Failing fast with one clear message beats a stack trace from deep inside an SDK.
"""

from __future__ import annotations

import logging
import os
from collections.abc import Mapping
from dataclasses import dataclass

TOOLBOX_API_VERSION = "v1"


class ConfigError(ValueError):
    """The environment is missing or has invalid configuration."""


@dataclass(frozen=True, slots=True)
class Settings:
    """Configuration for the hosted agent.

    Attributes:
        project_endpoint: Foundry project endpoint. Injected by the platform when hosted.
        model_deployment: Name of the model deployment (not the model name).
        toolbox_endpoint: Full toolbox MCP endpoint, if provided.
        toolbox_name: Toolbox name, used to build the consumer endpoint when no full
            endpoint is given.
        log_level: Standard logging level name.
    """

    project_endpoint: str
    model_deployment: str
    toolbox_endpoint: str | None = None
    toolbox_name: str | None = None
    log_level: str = "INFO"

    @property
    def toolbox_url(self) -> str:
        """The toolbox MCP URL. The consumer endpoint always serves the default version."""
        if self.toolbox_endpoint:
            return self.toolbox_endpoint
        assert self.toolbox_name  # guaranteed by from_env
        base = self.project_endpoint.rstrip("/")
        return f"{base}/toolboxes/{self.toolbox_name}/mcp?api-version={TOOLBOX_API_VERSION}"

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> Settings:
        """Build settings from ``env`` (defaults to ``os.environ``).

        Raises:
            ConfigError: listing every problem at once.
        """
        source = os.environ if env is None else env

        def first(*names: str) -> str | None:
            for name in names:
                value = source.get(name, "").strip()
                if value:
                    return value
            return None

        problems: list[str] = []

        endpoint = first("FOUNDRY_PROJECT_ENDPOINT")
        if endpoint is None:
            problems.append("FOUNDRY_PROJECT_ENDPOINT is not set")
        elif not endpoint.startswith("https://"):
            problems.append("FOUNDRY_PROJECT_ENDPOINT must start with https://")

        model = first("AZURE_AI_MODEL_DEPLOYMENT_NAME", "FOUNDRY_MODEL_NAME")
        if model is None:
            problems.append("AZURE_AI_MODEL_DEPLOYMENT_NAME is not set")

        toolbox_endpoint = first("TOOLBOX_ENDPOINT")
        toolbox_name = first("TOOLBOX_NAME")
        if toolbox_endpoint is None and toolbox_name is None:
            problems.append("set TOOLBOX_ENDPOINT or TOOLBOX_NAME")
        if toolbox_endpoint is not None and not toolbox_endpoint.startswith("https://"):
            problems.append("TOOLBOX_ENDPOINT must start with https://")

        level = (first("LOG_LEVEL") or "INFO").upper()
        if level not in logging.getLevelNamesMapping():
            problems.append(f"LOG_LEVEL {level!r} is not a valid logging level")

        if problems:
            raise ConfigError("Invalid configuration:\n- " + "\n- ".join(problems))

        assert endpoint is not None and model is not None
        return cls(
            project_endpoint=endpoint,
            model_deployment=model,
            toolbox_endpoint=toolbox_endpoint,
            toolbox_name=toolbox_name,
            log_level=level,
        )
