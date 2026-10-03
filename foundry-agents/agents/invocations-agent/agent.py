"""Builds the agent. No I/O happens at import time, so everything here is easy to test."""

from __future__ import annotations

from collections.abc import Sequence
from pathlib import Path

from agent_framework import Agent
from agent_framework._tools import ToolTypes
from agent_framework.foundry import FoundryChatClient
from agent_framework_foundry_hosting import FoundryToolbox
from azure.core.credentials import TokenCredential
from azure.core.credentials_async import AsyncTokenCredential

from settings import ConfigError, Settings

AGENT_NAME = "invocations-agent"
AGENT_DESCRIPTION = "Typed JSON question-answering API over AI Search and a code interpreter."
INSTRUCTIONS_FILE = Path(__file__).with_name("instructions.md")

Credential = TokenCredential | AsyncTokenCredential


def load_instructions(path: Path = INSTRUCTIONS_FILE) -> str:
    """Read the system prompt. Keeping it in a file makes prompt changes easy to review."""
    text = path.read_text(encoding="utf-8").strip()
    if not text:
        raise ConfigError(f"Instructions file is empty: {path}")
    return text


def build_client(settings: Settings, credential: Credential) -> FoundryChatClient:
    """Chat client bound to the project's model deployment."""
    return FoundryChatClient(
        project_endpoint=settings.project_endpoint,
        model=settings.model_deployment,
        credential=credential,
    )


def build_toolbox(settings: Settings, credential: Credential) -> FoundryToolbox:
    """MCP connection to the shared toolbox (AI Search + code interpreter).

    ``FoundryToolbox`` authenticates every request with ``credential`` and forwards the
    platform call context, so the toolbox sees the caller's identity.
    """
    return FoundryToolbox(credential, url=settings.toolbox_url)


def build_agent(
    settings: Settings,
    credential: Credential,
    *,
    client: FoundryChatClient | None = None,
    tools: ToolTypes | Sequence[ToolTypes] | None = None,
    instructions: str | None = None,
) -> Agent:
    """Create the agent.

    Args:
        settings: Validated configuration.
        credential: Azure credential shared by the client and the toolbox.
        client: Optional chat client, mainly for tests.
        tools: Optional replacement for the toolbox, mainly for tests. An empty list
            gives a tool-less agent.
        instructions: Optional system prompt override.
    """
    return Agent(
        client=client or build_client(settings, credential),
        name=AGENT_NAME,
        description=AGENT_DESCRIPTION,
        instructions=instructions or load_instructions(),
        tools=tools if tools is not None else build_toolbox(settings, credential),
        # The Invocations host persists each session itself. Turning service-side
        # storage off avoids keeping the same conversation twice.
        default_options={"store": False},
    )
