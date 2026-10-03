from __future__ import annotations

from typing import Any

import pytest
from azure.core.credentials import AccessToken

PROJECT = "https://acct.services.ai.azure.com/api/projects/proj"
TOOLBOX = f"{PROJECT}/toolboxes/search-and-code/mcp?api-version=v1"


class StubCredential:
    """A credential that never touches the network."""

    def get_token(self, *scopes: str, **kwargs: object) -> AccessToken:
        return AccessToken("stub-token", 9_999_999_999)


@pytest.fixture
def credential() -> StubCredential:
    return StubCredential()


@pytest.fixture
def env() -> dict[str, str]:
    return {
        "FOUNDRY_PROJECT_ENDPOINT": PROJECT,
        "AZURE_AI_MODEL_DEPLOYMENT_NAME": "gpt-5.4-mini",
        "TOOLBOX_ENDPOINT": TOOLBOX,
    }


# ---------------------------------------------------------------------------
# A fake chat client, so the agent and the HTTP host run end to end with no network.
# ---------------------------------------------------------------------------
from agent_framework import (  # noqa: E402
    BaseChatClient,
    ChatResponse,
    ChatResponseUpdate,
    Content,
    Message,
    ResponseStream,
)


class FakeChatClient(BaseChatClient):
    """Replies with canned text and records every call it receives."""

    def __init__(self, reply: str = "pong", chunks: list[str] | None = None) -> None:
        super().__init__()
        self.reply = reply
        self.chunks = chunks or [reply]
        self.calls: list[dict[str, Any]] = []

    def _inner_get_response(self, *, messages, stream, options, **kwargs):  # type: ignore[no-untyped-def]
        self.calls.append({"messages": [m.text for m in messages], "options": dict(options)})
        if stream:

            async def updates():  # type: ignore[no-untyped-def]
                for chunk in self.chunks:
                    yield ChatResponseUpdate(contents=[Content.from_text(chunk)], role="assistant")

            return ResponseStream(updates(), finalizer=ChatResponse.from_updates)

        async def one():  # type: ignore[no-untyped-def]
            return ChatResponse(messages=[Message(role="assistant", contents=[self.reply])])

        return one()


@pytest.fixture
def fake_client() -> FakeChatClient:
    return FakeChatClient()
