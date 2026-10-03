"""The live pool configuration, which can be replaced while the service runs.

Every component reads agent settings through a ``ConfigSource``. ``ConfigHolder`` is the one that
can change: a reload validates the new document, keeps the set of agents fixed (adding or removing
an agent needs a restart, because locks, queues and workers are made per agent) and swaps in the
new settings. Each change of an agent's settings bumps its ``generation``, and the controllers
report the generation they have applied as ``observed_generation``.
"""

from __future__ import annotations

from typing import Protocol

from hosted_agent_kit.config.models import AgentConfig, AgentPoolConfig, ConfigError


class ConfigSource(Protocol):
    @property
    def agents(self) -> dict[str, AgentConfig]: ...

    @property
    def names(self) -> list[str]: ...

    def get(self, agent_name: str) -> AgentConfig | None: ...


class ConfigHolder:
    def __init__(self, config: AgentPoolConfig) -> None:
        self._config = config
        self._generations = dict.fromkeys(config.names, 1)

    @property
    def current(self) -> AgentPoolConfig:
        return self._config

    @property
    def agents(self) -> dict[str, AgentConfig]:
        return self._config.agents

    @property
    def names(self) -> list[str]:
        return self._config.names

    def get(self, agent_name: str) -> AgentConfig | None:
        return self._config.get(agent_name)

    def generation(self, agent_name: str) -> int:
        return self._generations.get(agent_name, 0)

    def replace(self, new: AgentPoolConfig) -> list[str]:
        """Swap in a new configuration. Returns the agents whose settings changed.

        Raises ``ConfigError`` if the set of agents differs. Nothing changes in that case.
        """
        if set(new.names) != set(self._config.names):
            added = sorted(set(new.names) - set(self._config.names))
            removed = sorted(set(self._config.names) - set(new.names))
            raise ConfigError(
                "a reload cannot add or remove agents; restart the service instead "
                f"(added: {added}, removed: {removed})"
            )
        changed = [name for name in new.names if new.agents[name] != self._config.agents[name]]
        self._config = new
        for name in changed:
            self._generations[name] += 1
        return changed
