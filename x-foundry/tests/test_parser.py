from __future__ import annotations

import pytest

from xfoundry.diagnostics import ValidationFailed
from xfoundry.parser import parse_file, parse_mapping, parse_text, parse_x_foundry

from .conftest import BASE, EXAMPLES, mk

MINIMAL = """
x-foundry:
  topology: {mode: standalone}
  security: {roles: {admins: [Admins]}}
  projects: [{name: finance}]
"""


def failure(text: str | None = None, **kwargs) -> ValidationFailed:
    with pytest.raises(ValidationFailed) as info:
        parse_text(text if text is not None else MINIMAL, **kwargs)
    return info.value


def test_minimal_parses_with_defaults():
    parsed = parse_text(MINIMAL)
    cfg = parsed.config
    assert cfg.schema_version == "1.0"
    assert cfg.security.network.mode == "private"
    assert cfg.projects[0].inherit_hub is True
    assert cfg.gateway is None


def test_parse_file(tmp_path):
    parsed = parse_file(EXAMPLES / "standalone-minimal.yaml")
    assert parsed.source.endswith("standalone-minimal.yaml")
    assert parse_file(EXAMPLES / "hub-spoke.yaml").config.hub.name == "shared-ai"


def test_missing_file_is_a_diagnostic():
    err = None
    with pytest.raises(ValidationFailed) as info:
        parse_file("/nonexistent/azure.yaml")
    err = info.value
    assert err.diagnostics[0].code == "XF100"


def test_invalid_yaml():
    exc = failure("x-foundry: [unclosed")
    assert exc.diagnostics[0].code == "XF100"
    assert "line" in exc.diagnostics[0].message


def test_duplicate_keys_are_rejected():
    exc = failure(MINIMAL + "  projects: [{name: other}]\n")
    assert exc.diagnostics[0].code == "XF100"
    assert "duplicate key" in exc.diagnostics[0].message


@pytest.mark.parametrize("document", [[], "text", None])
def test_document_must_be_a_mapping(document):
    with pytest.raises(ValidationFailed) as info:
        parse_mapping(document)
    assert info.value.diagnostics[0].code == "XF101"


def test_x_foundry_section_is_required_and_must_be_a_mapping():
    with pytest.raises(ValidationFailed) as info:
        parse_mapping({"name": "app"})
    assert "no 'x-foundry'" in info.value.diagnostics[0].message
    with pytest.raises(ValidationFailed) as info:
        parse_mapping({"x-foundry": ["a"]})
    assert info.value.diagnostics[0].code == "XF101"


def test_other_azd_keys_are_ignored():
    doc = mk()
    doc["services"] = {"api": {"project": "./api"}}
    assert parse_mapping(doc).config.projects[0].name == "finance"


@pytest.mark.parametrize("version", ["2.0", "0.9"])
def test_incompatible_major_version_is_rejected(version):
    raw = {**BASE, "schemaVersion": version}
    with pytest.raises(ValidationFailed) as info:
        parse_x_foundry(raw)
    assert info.value.diagnostics[0].code == "XF103"
    assert "1.x" in info.value.diagnostics[0].message


def test_compatible_minor_version_is_accepted():
    assert parse_x_foundry({**BASE, "schemaVersion": "1.7"}).config.schema_version == "1.7"


def test_malformed_version_is_a_schema_error():
    with pytest.raises(ValidationFailed) as info:
        parse_x_foundry({**BASE, "schemaVersion": "one"})
    assert info.value.diagnostics[0].code == "XF102"


def test_schema_errors_have_paths_and_are_sorted():
    raw = {
        "topology": {"mode": "mesh"},
        "security": {"roles": {}},
        "projects": [{"name": "x"}],
        "bogus": 1,
    }
    with pytest.raises(ValidationFailed) as info:
        parse_x_foundry(raw)
    paths = [d.path for d in info.value.diagnostics]
    assert paths == sorted(paths)
    assert "x-foundry.topology.mode" in paths
    assert "x-foundry.security.roles" in paths
    assert "x-foundry.projects[0].name" in paths
    assert "x-foundry" in paths  # additional property


def test_required_sections():
    with pytest.raises(ValidationFailed) as info:
        parse_x_foundry({"topology": {"mode": "standalone"}})
    messages = " ".join(d.message for d in info.value.diagnostics)
    assert "'security' is a required property" in messages
    assert "'projects' is a required property" in messages


def test_hub_forbidden_in_standalone_and_required_in_hub_spoke():
    with pytest.raises(ValidationFailed):
        parse_x_foundry({**BASE, "hub": {"name": "shared"}})
    with pytest.raises(ValidationFailed):
        parse_x_foundry({**BASE, "topology": {"mode": "hub-spoke"}})


def test_oneof_errors_carry_detail():
    raw = {**BASE, "security": {"roles": {"admins": [{"type": "robot", "name": "x"}]}}}
    with pytest.raises(ValidationFailed) as info:
        parse_x_foundry(raw)
    assert any(
        "not valid under any of the given schemas" in d.message for d in info.value.diagnostics
    )


def test_ip_range_format_is_enforced():
    good = {
        **BASE,
        "security": {
            "roles": {"admins": ["a"]},
            "network": {"allowedIps": ["10.0.0.0/8", "1.2.3.4", "2001:db8::/32"]},
        },
    }
    parse_x_foundry(good)
    bad = {
        **BASE,
        "security": {"roles": {"admins": ["a"]}, "network": {"allowedIps": ["not-an-ip"]}},
    }
    with pytest.raises(ValidationFailed):
        parse_x_foundry(bad)


@pytest.mark.parametrize(
    "mutation, code, key",
    [
        ({"projects": [{"name": "ok", "sessionPool": {"size": 5}}]}, "XF018", "sessionPool"),
        (
            {"projects": [{"name": "ok", "agents": [{"name": "bot", "sessionPooling": True}]}]},
            "XF018",
            "sessionPooling",
        ),
        ({"runtime": {"enabled": True, "image": "x", "sessionPools": []}}, "XF018", "sessionPools"),
        ({"projects": [{"name": "ok", "agentPool": {}}]}, "XF018", "agentPool"),
        ({"redis": {"enabled": True, "sessionStore": True}}, "XF019", "sessionStore"),
    ],
)
def test_session_pool_and_redis_session_settings_have_specific_errors(mutation, code, key):
    with pytest.raises(ValidationFailed) as info:
        parse_x_foundry({**BASE, **mutation})
    found = [d for d in info.value.diagnostics if d.code == code]
    assert found and key in found[0].message
    # The generic additionalProperties error for the same key is suppressed.
    assert not [d for d in info.value.diagnostics if d.code == "XF102" and f"'{key}'" in d.message]


def test_pydantic_backstop_converts_to_diagnostics():
    # An unparseable URI passes JSON Schema when no format library is installed,
    # and is caught by the Pydantic layer; either way it is reported against the field.
    raw = {**BASE, "mcps": [{"name": "graph", "endpoint": "not a uri"}]}
    with pytest.raises(ValidationFailed) as info:
        parse_x_foundry(raw)
    assert any(d.path.endswith("mcps[0].endpoint") for d in info.value.diagnostics)


def test_validation_failed_message_lists_diagnostics():
    exc = failure("x-foundry: {}")
    assert "validation failed with" in str(exc)
    assert "XF102" in str(exc)
