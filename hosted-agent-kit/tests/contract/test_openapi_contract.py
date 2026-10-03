"""OpenAPI contract: stable shape, problem+json errors, security and no session leakage.

The snapshot lives in ``tests/contract/openapi.snapshot.json``. After an intentional API change,
regenerate it with ``UPDATE_OPENAPI_SNAPSHOT=1`` set when running this module.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any

import pytest

from hosted_agent_kit.service.main import create_app
from tests.conftest import make_config, make_settings
from tests.fakes.foundry import FakeFoundry

SNAPSHOT = Path(__file__).parent / "openapi.snapshot.json"
CLIENT_PATHS = [
    "/v1/agents/{agent_name}/invoke",
    "/v1/agents/{agent_name}/chat",
    "/v1/agents/{agent_name}/responses",
    "/v1/agents/{agent_name}/invocations",
]
ADMIN_PREFIX = "/v1/admin"
PUBLIC_PATHS = {"/health/live", "/health/ready"}


@pytest.fixture(scope="module")
def spec() -> dict[str, Any]:
    app = create_app(
        make_settings(metrics_endpoint_enabled=True),
        config=make_config({"a": {"mode": "stateless"}}, {}),
        adapter=FakeFoundry(),
    )
    return app.openapi()


def operations(spec: dict[str, Any]) -> list[tuple[str, str, dict[str, Any]]]:
    return [
        (path, method, op) for path, item in spec["paths"].items() for method, op in item.items()
    ]


def test_schema_matches_the_stored_snapshot(spec: dict[str, Any]) -> None:
    rendered = json.dumps(spec, indent=2, sort_keys=True) + "\n"
    # Python 3.13 renamed two HTTP reason phrases; the snapshot uses the 3.13 wording.
    for old, new in (
        ("Unprocessable Entity", "Unprocessable Content"),
        ("Request Entity Too Large", "Content Too Large"),
    ):
        rendered = rendered.replace(old, new)
    if os.environ.get("UPDATE_OPENAPI_SNAPSHOT") == "1":
        SNAPSHOT.write_text(rendered, encoding="utf-8")
    assert SNAPSHOT.exists(), "run once with UPDATE_OPENAPI_SNAPSHOT=1 to create the snapshot"
    assert rendered == SNAPSHOT.read_text(encoding="utf-8"), (
        "the OpenAPI document changed; review it, then regenerate the snapshot"
    )


def test_documented_routes_are_exactly_the_expected_set(spec: dict[str, Any]) -> None:
    assert {(p, m) for p, m, _ in operations(spec)} == {
        ("/v1/agents", "get"),
        ("/v1/agents/{agent_name}", "get"),
        ("/v1/agents/{agent_name}/invoke", "post"),
        ("/v1/agents/{agent_name}/responses", "post"),
        ("/v1/agents/{agent_name}/invocations", "post"),
        ("/v1/agents/{agent_name}/chat", "post"),
        ("/v1/admin/agents", "get"),
        ("/v1/admin/agents/{agent_name}", "get"),
        ("/v1/admin/events", "get"),
        ("/v1/admin/quota", "get"),
        ("/v1/admin/config/reload", "post"),
        ("/v1/admin/agents/{agent_name}/sessions", "get"),
        ("/v1/admin/agents/{agent_name}/sessions/{session_id}", "get"),
        ("/v1/admin/agents/{agent_name}/sessions/{session_id}", "delete"),
        ("/v1/admin/agents/{agent_name}/sync", "post"),
        ("/v1/admin/metrics", "get"),
        ("/health/live", "get"),
        ("/health/ready", "get"),
        ("/metrics", "get"),
    }


def test_bearer_security_is_declared_on_every_protected_route_and_only_there(
    spec: dict[str, Any],
) -> None:
    scheme = spec["components"]["securitySchemes"]["HTTPBearer"]
    assert scheme["type"] == "http" and scheme["scheme"] == "bearer"
    for path, _, op in operations(spec):
        if path in PUBLIC_PATHS:
            assert "security" not in op, path
        else:
            assert op["security"] == [{"HTTPBearer": []}], path


def test_error_responses_are_problem_json_with_the_shared_model(spec: dict[str, Any]) -> None:
    for path, method, op in operations(spec):
        if path in PUBLIC_PATHS:
            continue
        errors = {code: r for code, r in op["responses"].items() if code.startswith(("4", "5"))}
        assert errors, f"{method} {path} documents no error responses"
        for code, response in errors.items():
            content = response["content"]
            assert "application/problem+json" in content, f"{method} {path} {code}"
            assert "application/json" not in content, f"{method} {path} {code}"


@pytest.mark.parametrize("path", CLIENT_PATHS)
def test_client_error_codes_cover_the_prd_statuses(spec: dict[str, Any], path: str) -> None:
    codes = set(spec["paths"][path]["post"]["responses"])
    assert {"200", "401", "403", "404", "413", "422", "429", "500", "502", "503", "504"} <= codes


def test_problem_details_schema_has_the_required_fields(spec: dict[str, Any]) -> None:
    problem = spec["components"]["schemas"]["ProblemDetails"]
    assert set(problem["required"]) == {
        "type",
        "title",
        "status",
        "detail",
        "error_code",
        "phase",
        "retry_safe",
    }
    assert {"correlation_id", "retry_after_seconds"} <= set(problem["properties"])


def _all_property_names(node: Any) -> set[str]:
    names: set[str] = set()
    if isinstance(node, dict):
        names.update(node.get("properties", {}))
        for value in node.values():
            names |= _all_property_names(value)
    elif isinstance(node, list):
        for item in node:
            names |= _all_property_names(item)
    return names


def test_client_facing_schemas_never_expose_session_identifiers(spec: dict[str, Any]) -> None:
    client_schemas = ["InvokeRequest", "ChatRequest", "InvokeResponse", "ProblemDetails"]
    for name in client_schemas:
        properties = {p.lower() for p in _all_property_names(spec["components"]["schemas"][name])}
        assert not {p for p in properties if "session" in p}, name
    for path in CLIENT_PATHS:
        operation = spec["paths"][path]["post"]
        assert not any("session" in p["name"].lower() for p in operation.get("parameters", []))


def test_request_models_reject_unknown_fields(spec: dict[str, Any]) -> None:
    for name in ("InvokeRequest", "ChatRequest"):
        assert spec["components"]["schemas"][name]["additionalProperties"] is False


def test_agent_name_and_idempotency_key_are_constrained(spec: dict[str, Any]) -> None:
    operation = spec["paths"]["/v1/agents/{agent_name}/invoke"]["post"]
    params = {p["name"]: p for p in operation["parameters"]}
    assert (
        params["agent_name"]["schema"]["pattern"]
        and params["agent_name"]["schema"]["maxLength"] == 128
    )
    assert params["Idempotency-Key"]["required"] is False
    assert params["Idempotency-Key"]["schema"]["anyOf"][0]["maxLength"] == 255


def test_only_admin_routes_document_session_identifiers(spec: dict[str, Any]) -> None:
    for path, _, op in operations(spec):
        has_session_param = any("session" in p["name"] for p in op.get("parameters", []))
        assert has_session_param == ("session_id" in path), path
        if has_session_param:
            assert path.startswith(ADMIN_PREFIX)


def test_admin_user_field_is_documented_as_redacted(spec: dict[str, Any]) -> None:
    description = spec["components"]["schemas"]["SessionAdminView"]["properties"]["user"][
        "description"
    ]
    assert "Redacted" in description and "diagnostics" in description


def test_metrics_route_exists_only_when_enabled() -> None:
    app = create_app(
        make_settings(metrics_endpoint_enabled=False),
        config=make_config({"a": {"mode": "stateless"}}, {}),
        adapter=FakeFoundry(),
    )
    assert "/metrics" not in app.openapi()["paths"]
