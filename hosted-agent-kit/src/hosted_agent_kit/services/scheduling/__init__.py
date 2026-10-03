"""The scheduling framework: how a request is matched to a session.

The phases follow the Kubernetes scheduler. PreFilter resolves what the request needs. Filter
removes sessions that cannot serve it. Score ranks the rest. Reserve takes a lease on the best one,
or a capacity slot to create one. Permit decides whether the request must wait. Bind records the
affinity. Unreserve gives back whatever was reserved, and is safe to call more than once.
"""

from hosted_agent_kit.services.scheduling.framework import (
    CycleState,
    FilterPlugin,
    PreFilterPlugin,
    PreFilterResult,
    PreScorePlugin,
    Profile,
    SchedulerFramework,
    SchedulingRequest,
    ScorePlugin,
)
from hosted_agent_kit.services.scheduling.plugins import PluginRegistry, default_plugins

__all__ = [
    "CycleState",
    "FilterPlugin",
    "PluginRegistry",
    "PreFilterPlugin",
    "PreFilterResult",
    "PreScorePlugin",
    "Profile",
    "SchedulerFramework",
    "SchedulingRequest",
    "ScorePlugin",
    "default_plugins",
]
