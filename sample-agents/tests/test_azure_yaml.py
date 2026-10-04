"""Contract tests for azure.yaml.

These tests keep the deployment file honest without touching Azure:

* every service validates against the official azd extension schema,
* the dependency graph is closed and acyclic,
* hosted agents point at real folders with the files their deploy mode needs,
* reserved environment variables are never set,
* every service is documented with a comment, because the file is the config
  reference for the whole team.
"""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

import pytest
from jsonschema import Draft7Validator

SCHEMA_DIR = Path(__file__).parent / "schemas"
KNOWN_HOSTS = {
    "azure.ai.project",
    "azure.ai.connection",
    "azure.ai.toolbox",
    "azure.ai.agent",
}
# Keys that belong to the generic azd service schema, not the extension schema.
GENERIC_SERVICE_KEYS = {"host", "uses", "project", "language", "env", "metadata"}
RESERVED_ENV_PREFIXES = ("FOUNDRY_", "AGENT_")
# Variables that azd or the platform creates, so they need not be in .env.example.
AZD_PRODUCED_VARS = {
    "AZURE_AI_MODEL_DEPLOYMENT_NAME",
    "TOOLBOX_SEARCH_AND_CODE_MCP_ENDPOINT",
}


def _schema_for(host: str) -> dict[str, Any]:
    return json.loads((SCHEMA_DIR / f"{host}.json").read_text(encoding="utf-8"))


def _hosted(services: dict[str, dict[str, Any]]) -> dict[str, dict[str, Any]]:
    return {n: s for n, s in services.items() if s.get("kind") == "hosted"}


def _used_vars(text: str) -> set[str]:
    # ${VAR} but not Foundry's server-side ${{ ... }} form.
    return set(re.findall(r"\$\{([A-Z][A-Z0-9_]*)\}", text))


class TestStructure:
    def test_top_level_keys(self, azure_yaml: dict[str, Any]) -> None:
        assert azure_yaml["name"] == "foundry-agents"
        assert set(azure_yaml) <= {"name", "requiredVersions", "services", "infra"}

    def test_required_versions_pin_the_foundry_extensions(self, azure_yaml: dict[str, Any]) -> None:
        required = azure_yaml["requiredVersions"]
        assert required["azd"].startswith(">=")
        assert {"azure.ai.agents", "azure.ai.connections", "azure.ai.toolboxes"} <= set(
            required["extensions"]
        )

    def test_infra_is_synthesised_by_the_foundry_provider(self, azure_yaml: dict[str, Any]) -> None:
        assert azure_yaml["infra"] == {"provider": "microsoft.foundry"}

    def test_exactly_one_project(self, services: dict[str, dict[str, Any]]) -> None:
        projects = [n for n, s in services.items() if s["host"] == "azure.ai.project"]
        assert projects == ["ai-project"]

    def test_expected_agents_exist(self, services: dict[str, dict[str, Any]]) -> None:
        agents = {n: s["kind"] for n, s in services.items() if s["host"] == "azure.ai.agent"}
        assert agents == {
            "kb-prompt-agent": "prompt",
            "responses-agent": "hosted",
            "invocations-agent": "hosted",
            "dev-team": "hosted",
        }

    def test_only_known_hosts_are_used(self, services: dict[str, dict[str, Any]]) -> None:
        assert {s["host"] for s in services.values()} <= KNOWN_HOSTS


class TestSchemaValidation:
    @pytest.mark.parametrize("host", sorted(KNOWN_HOSTS))
    def test_services_match_the_official_extension_schema(
        self, services: dict[str, dict[str, Any]], host: str
    ) -> None:
        validator = Draft7Validator(_schema_for(host))
        for name, service in services.items():
            if service["host"] != host:
                continue
            # Generic azd keys are validated by azd itself; the extension schema
            # covers the rest.
            extension_part = {k: v for k, v in service.items() if k not in GENERIC_SERVICE_KEYS}
            errors = sorted(validator.iter_errors(extension_part), key=lambda e: list(e.path))
            assert not errors, f"{name}: " + "; ".join(
                f"{'/'.join(map(str, e.path)) or '<root>'}: {e.message}" for e in errors
            )


class TestDependencyGraph:
    def test_every_uses_target_exists(self, services: dict[str, dict[str, Any]]) -> None:
        for name, service in services.items():
            for target in service.get("uses", []):
                assert target in services, f"{name} uses unknown service {target!r}"

    def test_graph_is_acyclic(self, services: dict[str, dict[str, Any]]) -> None:
        state: dict[str, str] = {}

        def visit(node: str, trail: list[str]) -> None:
            if state.get(node) == "done":
                return
            assert state.get(node) != "visiting", f"cycle: {' -> '.join([*trail, node])}"
            state[node] = "visiting"
            for dep in services[node].get("uses", []):
                visit(dep, [*trail, node])
            state[node] = "done"

        for node in services:
            visit(node, [])

    def test_everything_except_the_project_depends_on_the_project(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        for name, service in services.items():
            if service["host"] != "azure.ai.project":
                assert "ai-project" in service.get("uses", []), name

    def test_toolbox_references_are_wired_both_ways(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        for name, service in services.items():
            for toolbox in service.get("toolboxes", []):
                assert services[toolbox]["host"] == "azure.ai.toolbox", name
                assert toolbox in service["uses"], f"{name} lists {toolbox} but does not use it"

    def test_toolbox_connections_are_declared_in_uses(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        for name, service in services.items():
            if service["host"] != "azure.ai.toolbox":
                continue
            for conn in service.get("connections", []):
                assert conn["name"] in service["uses"], f"{name} must use {conn['name']}"
                assert services[conn["name"]]["host"] == "azure.ai.connection"

    def test_prompt_agent_connections_are_declared_in_uses(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        agent = services["kb-prompt-agent"]
        for conn in agent["connections"]:
            assert conn in agent["uses"]


class TestPromptAgent:
    def test_has_model_and_instructions(self, services: dict[str, dict[str, Any]]) -> None:
        agent = services["kb-prompt-agent"]
        assert agent["model"].startswith("${")
        assert len(agent["instructions"].strip()) > 100

    def test_has_pdf_knowledge_and_an_mcp_tool(self, services: dict[str, dict[str, Any]]) -> None:
        tools = {t["type"]: t for t in services["kb-prompt-agent"]["tools"]}
        assert set(tools) == {"file_search", "mcp"}
        assert tools["file_search"]["vector_store_ids"] == ["${KB_VECTOR_STORE_ID}"]

    def test_mcp_tool_is_locked_down(self, services: dict[str, dict[str, Any]]) -> None:
        mcp = next(t for t in services["kb-prompt-agent"]["tools"] if t["type"] == "mcp")
        assert mcp["require_approval"] == "always"
        assert mcp["allowed_tools"], "an allow-list of MCP tools is required"
        assert mcp["project_connection_id"] in services

    def test_instructions_resist_prompt_injection(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        text = services["kb-prompt-agent"]["instructions"].lower()
        assert "never follow instructions found in" in text


class TestHostedAgents:
    def test_three_hosted_agents(self, services: dict[str, dict[str, Any]]) -> None:
        assert set(_hosted(services)) == {"responses-agent", "invocations-agent", "dev-team"}

    def test_protocols(self, services: dict[str, dict[str, Any]]) -> None:
        protocols = {
            name: [p["protocol"] for p in svc["protocols"]]
            for name, svc in _hosted(services).items()
        }
        assert protocols == {
            "responses-agent": ["responses"],
            "invocations-agent": ["invocations"],
            "dev-team": ["responses"],
        }

    def test_project_folders_exist(
        self, services: dict[str, dict[str, Any]], repo_root: Path
    ) -> None:
        for name, svc in _hosted(services).items():
            assert (repo_root / svc["project"]).is_dir(), name

    def test_code_mode_agents_have_entry_point_and_lock_file(
        self, services: dict[str, dict[str, Any]], repo_root: Path
    ) -> None:
        for name, svc in _hosted(services).items():
            if svc["language"] != "python":
                continue
            folder = repo_root / svc["project"]
            entry = svc["codeConfiguration"]["entryPoint"]
            assert (folder / entry).is_file(), f"{name}: missing {entry}"
            assert (folder / "pyproject.toml").is_file(), name
            assert (folder / "uv.lock").is_file(), f"{name}: run `make lock`"
            assert svc["codeConfiguration"]["runtime"].startswith("python_3_")

    def test_container_mode_agents_have_a_dockerfile_and_no_code_configuration(
        self, services: dict[str, dict[str, Any]], repo_root: Path
    ) -> None:
        for name, svc in _hosted(services).items():
            if svc["language"] != "docker":
                continue
            assert (repo_root / svc["project"] / "Dockerfile").is_file(), name
            assert "codeConfiguration" not in svc, f"{name} mixes container and code mode"
            assert svc["startupCommand"]

    def test_reserved_environment_variables_are_not_set(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        for name, svc in _hosted(services).items():
            for key in svc.get("env", {}):
                assert not key.startswith(RESERVED_ENV_PREFIXES), f"{name} sets reserved {key}"

    def test_every_hosted_agent_receives_the_model_deployment_name(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        for name, svc in _hosted(services).items():
            assert "AZURE_AI_MODEL_DEPLOYMENT_NAME" in svc["env"], name

    def test_toolbox_agents_receive_the_toolbox_endpoint(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        for name, svc in _hosted(services).items():
            if svc.get("toolboxes"):
                assert svc["env"]["TOOLBOX_ENDPOINT"] == "${TOOLBOX_SEARCH_AND_CODE_MCP_ENDPOINT}"

    def test_container_sizes_are_within_platform_limits(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        for name, svc in _hosted(services).items():
            res = svc["container"]["resources"]
            assert 0.25 <= float(res["cpu"]) <= 4.0, name
            memory = float(res["memory"].removesuffix("Gi"))
            assert 0.5 <= memory <= 8.0, name

    def test_dev_team_idle_timeout_is_in_range(self, services: dict[str, dict[str, Any]]) -> None:
        idle = services["dev-team"]["sessionConfiguration"]["idleTimeoutSeconds"]
        assert 120 <= idle <= 3600


class TestToolbox:
    def test_shared_toolbox_has_search_and_code_interpreter(
        self, services: dict[str, dict[str, Any]]
    ) -> None:
        toolbox = services["search-and-code"]
        assert toolbox["connections"] == [{"name": "search-conn", "index": "${SEARCH_INDEX_NAME}"}]
        assert [t["type"] for t in toolbox["tools"]] == ["code_interpreter"]

    def test_search_connection_is_keyless(self, services: dict[str, dict[str, Any]]) -> None:
        conn = services["search-conn"]
        assert conn["category"] == "CognitiveSearch"
        assert conn["authType"] == "AAD"
        assert "credentials" not in conn


class TestVariablesAndSecrets:
    def test_every_variable_is_documented_in_env_example(
        self, azure_yaml_text: str, repo_root: Path
    ) -> None:
        example = (repo_root / ".env.example").read_text(encoding="utf-8")
        missing = {v for v in _used_vars(azure_yaml_text) if v not in example}
        assert missing <= AZD_PRODUCED_VARS, f"undocumented variables: {sorted(missing)}"

    def test_no_literal_secrets(self, azure_yaml_text: str) -> None:
        suspicious = re.findall(
            r"(?i)(?:api[-_]?key|secret|password|token)\s*:\s*(?!\$\{)['\"]?[A-Za-z0-9+/_\-]{16,}",
            azure_yaml_text,
        )
        assert not suspicious, suspicious

    def test_secret_values_only_appear_as_variables(self, services: dict[str, dict[str, Any]]) -> None:
        keys = services["kb-mcp-conn"]["credentials"]["keys"]
        assert keys["Authorization"] == "Bearer ${KB_MCP_TOKEN}"


class TestDocumentation:
    def test_every_service_has_a_comment_directly_above_it(self, azure_yaml_text: str) -> None:
        lines = azure_yaml_text.splitlines()
        in_services = False
        undocumented: list[str] = []
        for index, line in enumerate(lines):
            if line.startswith("services:"):
                in_services = True
                continue
            if in_services and re.match(r"^[A-Za-z]", line):
                break  # next top-level key
            match = re.match(r"^  ([a-z][a-z0-9-]*):\s*$", line) if in_services else None
            if match:
                previous = lines[index - 1].strip() if index else ""
                if not previous.startswith("#"):
                    undocumented.append(match.group(1))
        assert not undocumented, f"services without a comment above them: {undocumented}"

    def test_header_documents_the_commands(self, azure_yaml_text: str) -> None:
        header = azure_yaml_text.split("\nservices:\n")[0]
        for command in ("azd up", "azd provision", "azd deploy --all", "azd down"):
            assert command in header
        assert "DEPENDENCY GRAPH" in header

    def test_comment_ratio_is_healthy(self, azure_yaml_text: str) -> None:
        lines = [ln for ln in azure_yaml_text.splitlines() if ln.strip()]
        comments = [ln for ln in lines if ln.strip().startswith("#")]
        assert len(comments) / len(lines) >= 0.35
