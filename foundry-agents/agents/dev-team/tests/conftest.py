from __future__ import annotations

from copy import deepcopy
from pathlib import Path
from typing import Any

import pytest
from agent_framework import (
    BaseChatClient,
    ChatResponse,
    ChatResponseUpdate,
    Content,
    Message,
    ResponseStream,
)

from settings import Settings
from tools.agent_tools import TeamContext
from tools.worktrees import WorktreeManager

PROJECT = "https://acct.services.ai.azure.com/api/projects/proj"

COMPLETE_BRIEF: dict[str, Any] = {
    "project_name": "slugify_lib",
    "summary": "A small library that turns article titles into URL slugs.",
    "target_audience": "Python developers building blogs who need stable URL slugs.",
    "functional_requirements": [
        "slugify lowercases the title and joins words with single hyphens.",
        "slugify removes every character that is not a letter, digit or hyphen.",
        "slugify raises ValueError when the title is empty.",
    ],
    "dependencies": [
        {"name": "pydantic", "kind": "library", "purpose": "Validate the input model."}
    ],
    "no_dependencies_confirmed": False,
    "io_samples": [{"name": "basic", "input": "Hello, World!", "expected_output": "hello-world"}],
    "out_of_scope": ["Transliteration of non-Latin scripts."],
    "definition_of_done": ["All unit tests pass.", "Every requirement has at least one test."],
}


class FakeChatClient(BaseChatClient):
    """Replies with canned text and records every call. No network."""

    def __init__(self, reply: str = "ok") -> None:
        super().__init__()
        self.reply = reply
        self.calls: list[dict[str, Any]] = []

    def _inner_get_response(self, *, messages, stream, options, **kwargs):  # type: ignore[no-untyped-def]
        self.calls.append({"messages": [m.text for m in messages]})
        if stream:

            async def updates():  # type: ignore[no-untyped-def]
                yield ChatResponseUpdate(contents=[Content.from_text(self.reply)], role="assistant")

            return ResponseStream(updates(), finalizer=ChatResponse.from_updates)

        async def one():  # type: ignore[no-untyped-def]
            return ChatResponse(messages=[Message(role="assistant", contents=[self.reply])])

        return one()


async def call_tool(tool: Any, **arguments: Any) -> str:
    """Invoke an Agent Framework tool and return its text."""
    contents = await tool.invoke(arguments=arguments)
    return "".join(getattr(c, "text", "") or "" for c in contents)


@pytest.fixture
def brief_dict() -> dict[str, Any]:
    return deepcopy(COMPLETE_BRIEF)


@pytest.fixture
def env(tmp_path: Path) -> dict[str, str]:
    return {
        "FOUNDRY_PROJECT_ENDPOINT": PROJECT,
        "AZURE_AI_MODEL_DEPLOYMENT_NAME": "gpt-5.4-mini",
        "WORKSPACE_ROOT": str(tmp_path / "ws"),
    }


@pytest.fixture
def settings(tmp_path: Path) -> Settings:
    return Settings(
        project_endpoint=PROJECT,
        model_deployment="gpt-5.4-mini",
        workspace_root=tmp_path / "ws",
        command_timeout_seconds=30,
    )


@pytest.fixture
def manager(tmp_path: Path) -> WorktreeManager:
    return WorktreeManager(tmp_path / "repo")


@pytest.fixture
def ctx(settings: Settings, manager: WorktreeManager) -> TeamContext:
    return TeamContext(settings, manager)


@pytest.fixture
def fake_client() -> FakeChatClient:
    return FakeChatClient()


class StubCredential:
    """A credential that never touches the network."""

    def get_token(self, *scopes: str, **kwargs: object):  # type: ignore[no-untyped-def]
        from azure.core.credentials import AccessToken

        return AccessToken("stub-token", 9_999_999_999)


@pytest.fixture
def credential() -> StubCredential:
    return StubCredential()
