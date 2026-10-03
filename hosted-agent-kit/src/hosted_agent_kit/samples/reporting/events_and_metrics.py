"""Reporting sample 2: events, metrics and a Prometheus endpoint.

Run:   uvicorn hosted_agent_kit.samples.reporting.events_and_metrics:app --reload
Try:   curl -s 'localhost:8000/events?limit=5' | jq     curl -s localhost:8000/metrics.txt

Events say what the controllers did and why (a session was created, a circuit opened, a sync
failed). They are a log for people: nothing reads them back. Metrics are counters, gauges and
histograms; ``prometheus()`` renders them for a scraper. To send metrics to Azure Monitor set
``APPLICATIONINSIGHTS_CONNECTION_STRING`` and install the ``azure-monitor`` extra.
"""

from __future__ import annotations

from typing import Annotated, Any

from fastapi import FastAPI, Query
from fastapi.responses import PlainTextResponse

from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit
from hosted_agent_kit.views import EventView

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Reporting sample: events and metrics")
install(app, kit)


@app.get("/events", response_model=list[EventView])
async def events(
    kit: KitDep,
    agent: str | None = None,
    limit: Annotated[int, Query(ge=1, le=500)] = 50,
) -> list[EventView]:
    return kit.reporting.events(agent, limit=limit)


@app.get("/metrics")
async def metrics(kit: KitDep) -> dict[str, Any]:
    return kit.reporting.metrics()


@app.get("/metrics.txt", response_class=PlainTextResponse)
async def prometheus(kit: KitDep) -> str:
    return kit.reporting.prometheus()


@app.post("/ping")
async def ping(user: UserDep, kit: KitDep) -> Any:
    """Make a call so there is something to report."""
    result = await kit.ask("support-bot", "hello", user_id=user)
    return result.json()
