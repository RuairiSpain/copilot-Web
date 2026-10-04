"""End-to-end tests: real Invocations host, real HTTP handling, fake model."""

from __future__ import annotations

import pytest
from starlette.testclient import TestClient

from agent import build_agent
from main import create_server
from settings import Settings

from .conftest import FakeChatClient


@pytest.fixture
def make_client(env: dict[str, str], credential):  # type: ignore[no-untyped-def]
    def _make(chat: FakeChatClient) -> TestClient:
        settings = Settings.from_env(env)
        server = create_server(
            settings,
            credential,
            agent_factory=lambda: build_agent(settings, credential, client=chat, tools=[]),
        )
        return TestClient(server)

    return _make


URL = "/invocations?agent_session_id=session-1"


def test_answers_a_question_as_json(make_client) -> None:  # type: ignore[no-untyped-def]
    chat = FakeChatClient(reply="Rotate keys in the portal.")
    response = make_client(chat).post(URL, json={"question": "How do I rotate a key?"})
    assert response.status_code == 200
    assert response.json() == {"response": "Rotate keys in the portal."}


def test_the_question_reaches_the_model(make_client) -> None:  # type: ignore[no-untyped-def]
    chat = FakeChatClient()
    make_client(chat).post(URL, json={"question": "How do I rotate a key?"})
    assert "How do I rotate a key?" in chat.calls[0]["messages"]


def test_token_cap_reaches_the_model(make_client) -> None:  # type: ignore[no-untyped-def]
    chat = FakeChatClient()
    make_client(chat).post(URL, json={"question": "hi", "max_output_tokens": 64})
    assert chat.calls[0]["options"]["max_tokens"] == 64


def test_streams_delta_frames_then_done(make_client) -> None:  # type: ignore[no-untyped-def]
    chat = FakeChatClient(chunks=["Hel", "lo"])
    response = make_client(chat).post(URL, json={"question": "hi", "stream": True})
    assert response.status_code == 200
    body = response.text
    assert body.count("event: delta") == 2
    assert body.rstrip().endswith('"session_id": "session-1"}')
    assert body.index("event: delta") < body.index("event: done")


@pytest.mark.parametrize(
    "payload",
    [
        {"question": ""},
        {"question": "hi", "extra": True},
        {"question": "hi", "max_output_tokens": 1},
    ],
)
def test_invalid_payloads_get_a_400(make_client, payload: dict) -> None:  # type: ignore[no-untyped-def,type-arg]
    chat = FakeChatClient()
    response = make_client(chat).post(URL, json=payload)
    assert response.status_code == 400
    assert "error" in response.json()
    assert chat.calls == []  # a rejected request never costs a model call


def test_malformed_json_gets_a_400(make_client) -> None:  # type: ignore[no-untyped-def]
    response = make_client(FakeChatClient()).post(
        URL, content="not json", headers={"content-type": "application/json"}
    )
    assert response.status_code == 400
