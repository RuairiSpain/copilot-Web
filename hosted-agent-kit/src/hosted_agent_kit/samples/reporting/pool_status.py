"""Reporting sample 1: show what the pool looks like right now.

Run:   uvicorn hosted_agent_kit.samples.reporting.pool_status:app --reload
Try:   curl -s localhost:8000/status | jq        curl -s localhost:8000/reporting/agents | jq

``reporting_router`` adds ready-made read-only endpoints. ``kit.reporting`` is the same data for
your own endpoints. Reporting never changes anything, but it shows pool internals, so the router
refuses to build unless you pass ``dependencies=`` or ``allow_unauthenticated=True``.
"""

from __future__ import annotations

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import KitDep, install, reporting_router
from hosted_agent_kit.samples._common import load_kit

kit = load_kit("config/stateful-and-stateless.yaml")
app = FastAPI(title="Reporting sample: pool status")
install(app, kit)
# This demo leaves the router open. In your application pass dependencies=[Depends(...)] that check
# the caller; without it (or allow_unauthenticated=True) the router refuses to build.
app.include_router(reporting_router(allow_unauthenticated=True), prefix="/reporting")


@app.get("/status")
async def status(kit: KitDep) -> list[dict[str, object]]:
    """One line per agent: how full it is, whether callers are waiting, whether it is healthy."""
    rows = []
    for agent in await kit.reporting.agents():
        rows.append(
            {
                "agent": agent.agent_name,
                "sessions": f"{agent.sessions_leased} busy / {agent.sessions_total} of "
                f"{agent.max_sessions}",
                "waiting": agent.queue_depth,
                "circuit": agent.circuit_state,
                "ready": any(c.type == "Ready" and c.status == "True" for c in agent.conditions),
            }
        )
    return rows
