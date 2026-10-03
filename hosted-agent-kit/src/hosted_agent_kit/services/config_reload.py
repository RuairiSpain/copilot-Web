"""Reloading the agent pool configuration while the service runs."""

from __future__ import annotations

from collections.abc import Callable

from hosted_agent_kit.config.holder import ConfigHolder
from hosted_agent_kit.config.models import AgentPoolConfig, ConfigError
from hosted_agent_kit.controllers.plane import ControlPlane
from hosted_agent_kit.domain.errors import ConfigInvalidError
from hosted_agent_kit.domain.resources import (
    POOL_CONFIGURATION_VALID,
    ConditionStatus,
    pool_key,
)
from hosted_agent_kit.services.events import EventType
from hosted_agent_kit.services.pool import PoolService


class ConfigReloader:
    """Loads, validates and applies a new configuration. A rejected one changes nothing."""

    def __init__(
        self,
        *,
        holder: ConfigHolder,
        pool: PoolService,
        plane: ControlPlane,
        load: Callable[[], AgentPoolConfig],
        validate: Callable[[AgentPoolConfig], None],
    ) -> None:
        self._holder = holder
        self._pool = pool
        self._plane = plane
        self._load = load
        self._validate = validate

    def reload(self) -> dict[str, int]:
        """Apply the configuration on disk. Returns the generation of each agent that changed."""
        try:
            new = self._load()
            self._validate(new)
            self._pool.check_config(new)  # an unknown plugin must not leave a half-applied reload
            changed = self._holder.replace(new)
        except ConfigError as exc:
            for name in self._holder.names:
                self._plane.pools.set_condition(
                    name,
                    POOL_CONFIGURATION_VALID,
                    ConditionStatus.FALSE,
                    "ReloadRejected",
                    str(exc),
                )
                self._plane.manager.enqueue("status", pool_key(name))
            self._plane.events.pool(
                self._holder.names[0],
                EventType.WARNING,
                "ReloadRejected",
                f"The new configuration was rejected: {exc}",
            )
            raise ConfigInvalidError(str(exc)) from exc
        self._pool.refresh_profiles()
        for name in self._holder.names:
            self._plane.pools.set_condition(
                name, POOL_CONFIGURATION_VALID, ConditionStatus.TRUE, "Valid"
            )
            self._plane.manager.enqueue("status", pool_key(name))
        for name in changed:
            self._plane.manager.enqueue("pool", pool_key(name))
        return {name: self._holder.generation(name) for name in changed}
