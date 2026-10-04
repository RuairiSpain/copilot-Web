from __future__ import annotations

from pathlib import Path

import pytest
from agent_framework import Agent
from agent_framework.foundry import FoundryChatClient
from agent_framework_foundry_hosting import FoundryToolbox

import agent as agent_module
from agent import AGENT_NAME, build_agent, build_client, build_toolbox, load_instructions
from settings import ConfigError, Settings

from .conftest import StubCredential


@pytest.fixture
def settings(env: dict[str, str]) -> Settings:
    return Settings.from_env(env)


class TestInstructions:
    def test_the_shipped_prompt_loads(self) -> None:
        assert "Azure AI Search" in load_instructions()

    def test_the_prompt_covers_both_tools(self) -> None:
        text = load_instructions().lower()
        assert "search" in text and "code interpreter" in text

    def test_the_prompt_resists_prompt_injection(self) -> None:
        text = load_instructions().lower()
        assert "data, not instructions" in text
        assert "never reveal" in text

    def test_empty_file_is_rejected(self, tmp_path: Path) -> None:
        empty = tmp_path / "instructions.md"
        empty.write_text("  \n")
        with pytest.raises(ConfigError, match="empty"):
            load_instructions(empty)


class TestBuilders:
    def test_client_is_bound_to_the_deployment(
        self, settings: Settings, credential: StubCredential
    ) -> None:
        client = build_client(settings, credential)
        assert isinstance(client, FoundryChatClient)

    def test_toolbox_is_a_foundry_toolbox(
        self, settings: Settings, credential: StubCredential
    ) -> None:
        assert isinstance(build_toolbox(settings, credential), FoundryToolbox)


class TestBuildAgent:
    def test_agent_identity(self, settings: Settings, credential: StubCredential) -> None:
        agent = build_agent(settings, credential)
        assert isinstance(agent, Agent)
        assert agent.name == AGENT_NAME
        assert agent.description

    def test_instructions_are_applied(self, settings: Settings, credential: StubCredential) -> None:
        agent = build_agent(settings, credential)
        assert agent.default_options["instructions"] == load_instructions()

    def test_instructions_can_be_overridden(
        self, settings: Settings, credential: StubCredential
    ) -> None:
        agent = build_agent(settings, credential, instructions="Be brief.")
        assert agent.default_options["instructions"] == "Be brief."

    def test_service_side_history_is_off(
        self, settings: Settings, credential: StubCredential
    ) -> None:
        # The Invocations host owns history, so storing it twice would waste money.
        assert build_agent(settings, credential).default_options["store"] is False

    def test_the_toolbox_is_attached_as_an_mcp_tool(
        self, settings: Settings, credential: StubCredential
    ) -> None:
        agent = build_agent(settings, credential)
        assert [type(t).__name__ for t in agent.mcp_tools] == ["FoundryToolbox"]

    def test_injected_dependencies_are_used(
        self, settings: Settings, credential: StubCredential
    ) -> None:
        client = build_client(settings, credential)
        toolbox = build_toolbox(settings, credential)
        agent = build_agent(settings, credential, client=client, tools=toolbox)
        assert agent.client is client
        assert agent.mcp_tools == [toolbox]

    def test_an_empty_tool_list_gives_a_tool_less_agent(
        self, settings: Settings, credential: StubCredential
    ) -> None:
        assert build_agent(settings, credential, tools=[]).mcp_tools == []

    def test_importing_the_module_does_no_io(self) -> None:
        # No module-level clients or credentials: importing must be side-effect free.
        assert not any(isinstance(v, FoundryChatClient) for v in vars(agent_module).values())
