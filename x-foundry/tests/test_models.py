"""The Pydantic layer enforces the JSON Schema's constraints when used on its own."""

from __future__ import annotations

import copy

import pytest
from pydantic import ValidationError

from xfoundry.schema.models import (
    GatewayAuthentication,
    IndexField,
    KnowledgeSource,
    PrincipalObject,
    QuotaProfile,
    Runtime,
    Search,
    Storage,
    XFoundry,
)

from .conftest import BASE


def test_root_model_applies_defaults_and_accepts_camel_and_snake_case():
    cfg = XFoundry.model_validate(copy.deepcopy(BASE))
    assert cfg.security.network.mode == "private"
    again = XFoundry.model_validate({**BASE, "keyVault": {"purgeProtection": False}})
    assert again.key_vault.purge_protection is False
    by_name = XFoundry.model_validate({**BASE, "key_vault": {"purge_protection": False}})
    assert by_name.key_vault.purge_protection is False


def test_unknown_keys_are_rejected():
    with pytest.raises(ValidationError):
        XFoundry.model_validate({**BASE, "bogus": 1})


def test_hub_must_match_topology():
    with pytest.raises(ValidationError, match="not allowed"):
        XFoundry.model_validate({**BASE, "hub": {"name": "shared"}})
    with pytest.raises(ValidationError, match="required"):
        XFoundry.model_validate({**BASE, "topology": {"mode": "hub-spoke"}})


@pytest.mark.parametrize(
    "build",
    [
        lambda: PrincipalObject(type="user"),
        lambda: Search(name="srch-1", existing_resource_id="/x"),
        lambda: Storage(name="stacct", existing_resource_id="/x"),
        lambda: Runtime(enabled=True),
        lambda: KnowledgeSource(name="site", type="sharepoint"),
        lambda: KnowledgeSource(name="db", type="sql", database="d"),
        lambda: IndexField(name="v", type="Collection(Edm.Single)"),
        lambda: QuotaProfile(name="qq", scope="user"),
        lambda: GatewayAuthentication(audiences=[]),
    ],
)
def test_model_level_constraints(build):
    with pytest.raises(ValidationError):
        build()


def test_model_level_constraints_accept_valid_values():
    assert PrincipalObject(type="user", name="ann").name == "ann"
    assert KnowledgeSource(name="db", type="sql", database="d", query="select 1").query
    assert KnowledgeSource(name="cust", type="custom").type == "custom"
    assert QuotaProfile(name="qq", scope="user", daily_requests=5).daily_requests == 5
    assert Runtime(enabled=False).image is None


@pytest.mark.parametrize(
    "path, value",
    [
        ("mcps", [{"name": "graph", "endpoint": "not a uri"}]),
        ("security", {"roles": {"admins": ["a"]}, "network": {"allowedDomains": ["bad host!"]}}),
        ("security", {"roles": {"admins": ["a"]}, "network": {"allowedIps": ["999.1.1.1"]}}),
        ("gateway", {"authentication": {"audiences": ["a"], "tenantId": "not-a-uuid"}}),
        ("governance", {"budgets": {"contacts": ["not-an-email"]}}),
    ],
)
def test_format_types_are_checked(path, value):
    with pytest.raises(ValidationError):
        XFoundry.model_validate({**BASE, path: value})


def test_format_types_accept_valid_values():
    cfg = XFoundry.model_validate(
        {
            **BASE,
            "mcps": [{"name": "graph", "endpoint": "https://graph.contoso.com/mcp"}],
            "security": {
                "roles": {"admins": ["a"]},
                "network": {"allowedDomains": ["contoso.com"], "allowedIps": ["10.0.0.0/8", "::1"]},
            },
            "gateway": {
                "authentication": {
                    "audiences": ["a"],
                    "tenantId": "11111111-2222-3333-4444-555555555555",
                }
            },
            "governance": {"budgets": {"contacts": ["ops@contoso.com"]}},
        }
    )
    assert cfg.mcps[0].transport == "streamable-http"
