"""Miscellaneous sample 6: test your endpoints without Azure.

Run:   pytest src/hosted_agent_kit/samples/miscellaneous/testing_your_app.py

``FakeFoundry`` is an in-memory Foundry. It records every call and can be scripted: make the
next create fail, stall an invocation, return a chosen response. ``FakeClock`` removes real
waiting from backoff and timeouts. Build the kit with ``adapter=FakeFoundry()`` and use FastAPI's
``TestClient``: the app's lifespan starts and stops the kit as in production.
"""

from __future__ import annotations

from typing import Any

from fastapi import FastAPI
from fastapi.testclient import TestClient

from hosted_agent_kit import Hack
from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.testing import FakeFoundry, FoundryUnavailable, UpstreamResponse

CONFIG = {"agentPool": {"agents": {"support-bot": {"mode": "stateless", "max_sessions": 2}}}}


def create_app(foundry: FakeFoundry) -> FastAPI:
    """The code under test: normally this is your application's own factory."""
    app = FastAPI()
    install(app, Hack.from_dict(CONFIG, adapter=foundry))

    @app.post("/ask")
    async def ask(message: str, kit: KitDep) -> Any:
        result = await kit.ask("support-bot", message, user_id="tester")
        return result.json()

    return app


def test_the_endpoint_returns_the_agents_answer() -> None:
    foundry = FakeFoundry()
    foundry.invoke_handler = lambda ctx: UpstreamResponse(body={"output_text": "42"})
    with TestClient(create_app(foundry)) as client:
        response = client.post("/ask", params={"message": "meaning of life?"})
    assert response.json() == {"output_text": "42"}
    assert foundry.invocations[0].payload == {"input": "meaning of life?"}


def test_a_foundry_outage_is_reported_as_service_unavailable() -> None:
    foundry = FakeFoundry()
    foundry.create_errors = [FoundryUnavailable()] * 10
    with TestClient(create_app(foundry)) as client:
        response = client.post("/ask", params={"message": "hello"})
    assert response.status_code in (502, 503)
    assert response.headers["content-type"].startswith("application/problem+json")
