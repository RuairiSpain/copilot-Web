"""Administrative sample 4: roll out a new agent version.

Run:   uvicorn hosted_agent_kit.samples.administrative.version_rollout:app --reload
Try:   curl -s localhost:8000/rollout -H 'X-Admin-Key: dev-admin-key' | jq

Roll-out is configuration, not code: set ``agent_version`` in the YAML to the version that new
sessions must use, set ``version_drain`` to retire idle sessions on other versions, then call
``reload_config``. ``never`` leaves old sessions alone, ``unbound`` retires idle pooled sessions
no user owns, and ``idle`` also retires idle user sessions (their state is lost). This endpoint
shows each agent's pinned version and drain mode, and whether the controllers have applied the
latest ``generation`` of the pool's configuration (``observed_generation == generation``).
"""

from __future__ import annotations

from fastapi import FastAPI

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import AdminDep, load_kit

kit = load_kit("config/version-pinning.yaml")
app = FastAPI(title="Administrative sample: version rollout")
install(app, kit)


@app.get("/rollout", dependencies=[AdminDep])
async def rollout(kit: KitDep) -> list[dict[str, object]]:
    return [
        {
            "agent": agent.agent_name,
            "pinned_version": agent.agent_version,
            "drain": agent.version_drain,
            "applied": agent.observed_generation == agent.generation,
        }
        for agent in await kit.reporting.agents()
    ]
