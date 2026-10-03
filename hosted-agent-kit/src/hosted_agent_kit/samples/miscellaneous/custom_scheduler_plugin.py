"""Miscellaneous sample 2: add your own scheduling rule.

Run:   uvicorn hosted_agent_kit.samples.miscellaneous.custom_scheduler_plugin:app --reload

The scheduler picks a session in phases: filters remove sessions that cannot serve the request,
scores rank the rest. This sample adds a filter that keeps a request off sessions that have been
open for too long, and a score that prefers the longest-open session still allowed. Both are named
in the YAML under ``scheduler_profile`` (see ``config/scheduler-profiles.yaml``).
"""

from __future__ import annotations

from typing import Any

from fastapi import FastAPI

from hosted_agent_kit import Hack
from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.plugins import (
    CycleState,
    SchedulingRequest,
    SessionRecord,
    default_plugins,
)
from hosted_agent_kit.samples._common import SAMPLES_DIR, UserDep, demo_mode
from hosted_agent_kit.testing import DemoFoundry

MAX_SESSION_AGE_SECONDS = 6 * 3600


class YoungEnough:
    """Filter: do not use a session older than six hours."""

    name = "YoungEnough"

    def filter(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> bool:
        return (request.now - session.created_at).total_seconds() < MAX_SESSION_AGE_SECONDS


class PreferOlder:
    """Score: prefer the oldest allowed session, so fewer sessions stay warm and idle."""

    name = "PreferOlder"

    def score(self, request: SchedulingRequest, session: SessionRecord, state: CycleState) -> int:
        hours = int((request.now - session.created_at).total_seconds() // 3600)
        return min(hours, 10)


plugins = default_plugins()
plugins.register_filter("YoungEnough", YoungEnough)
plugins.register_score("PreferOlder", PreferOlder)

kit = Hack.from_yaml(
    SAMPLES_DIR / "config" / "custom-plugins.yaml",
    adapter=DemoFoundry() if demo_mode() else None,
    scheduler_plugins=plugins,
)
app = FastAPI(title="Miscellaneous sample: custom scheduler plugin")
install(app, kit)


@app.post("/ask")
async def ask(message: str, user: UserDep, kit: KitDep) -> Any:
    return (await kit.ask("support-bot", message, user_id=user)).json()
