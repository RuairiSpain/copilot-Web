"""Builds each agent's scheduling profile from its configuration."""

from __future__ import annotations

from hosted_agent_kit.config.holder import ConfigSource
from hosted_agent_kit.ports.affinity import AffinityStore
from hosted_agent_kit.ports.registry import SessionRegistry
from hosted_agent_kit.services.scheduling.framework import Profile
from hosted_agent_kit.services.scheduling.plugins import (
    STRATEGY_PLUGIN,
    AffinityResolution,
    PluginRegistry,
)
from hosted_agent_kit.services.session_ids import SessionIdDeriver

CORE_FILTERS = ("Ready", "AffinityCompatible", "RestoreHeld")


def build_profiles(
    *,
    config: ConfigSource,
    plugins: PluginRegistry,
    registry: SessionRegistry,
    affinity: AffinityStore,
    session_ids: SessionIdDeriver | None,
) -> dict[str, Profile]:
    """One profile per agent. An unknown plugin name fails here, before the service serves."""
    profiles: dict[str, Profile] = {}
    for name, cfg in config.agents.items():
        extra = [f for f in cfg.scheduler_profile.filters if f not in CORE_FILTERS]
        filters = tuple(plugins.filter(f) for f in (*CORE_FILTERS, *extra))
        weights = cfg.scheduler_profile.scores or {STRATEGY_PLUGIN[cfg.scheduler]: 1}
        scores = tuple((plugins.score(n), w) for n, w in weights.items())
        profiles[name] = Profile(
            name=f"{cfg.mode.value}-{cfg.scheduler.value}",
            pre_filters=(AffinityResolution(registry, affinity, session_ids),),
            filters=filters,
            scores=scores,
        )
    return profiles
