"""Write your own scheduling rules.

A *filter* says whether a session may serve a request. A *score* ranks the sessions that pass.
Register them in a ``PluginRegistry``, pass the registry to ``Hack(..., scheduler_plugins=...)``
and name them in an agent's ``scheduler_profile`` in the YAML.
"""

from __future__ import annotations

from hosted_agent_kit.domain.models import SessionRecord
from hosted_agent_kit.services.scheduling import (
    CycleState,
    FilterPlugin,
    PluginRegistry,
    SchedulingRequest,
    ScorePlugin,
    default_plugins,
)

__all__ = [
    "CycleState",
    "FilterPlugin",
    "PluginRegistry",
    "SchedulingRequest",
    "ScorePlugin",
    "SessionRecord",
    "default_plugins",
]
