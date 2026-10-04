from __future__ import annotations

import pytest

from xfoundry.azure_names import NAME_RULES, name_problems
from xfoundry.ids import slug
from xfoundry.validators.locations import canonical, is_known_region
from xfoundry.validators.secrets import (
    is_key_vault_reference,
    is_secret_name_or_reference,
    raw_secret_kind,
)


@pytest.mark.parametrize(
    "kind, good, bad",
    [
        ("storage", ["stfin01", "abc"], ["ab", "St-Fin", "x" * 25, "st_fin"]),
        ("key-vault", ["kv-fin", "kvfin1"], ["1kv", "kv--x", "kv-", "ab"]),
        ("search", ["srch-1", "ab"], ["-ab", "Ab", "ab-", "a--b"]),
        ("redis", ["r", "Redis-1"], ["-r", "r--1", "x" * 64]),
        ("apim", ["apim1", "a"], ["1apim", "apim-"]),
        ("container-app", ["app-1", "ab"], ["App", "1app", "a--b", "x" * 33]),
        ("storage-container", ["abc", "a-b-c"], ["ab", "A-b", "a--b", "-ab"]),
        ("registry", ["acr12345"], ["acr-1", "abcd"]),
        ("service-bus", ["bus-name"], ["1bus-name", "bus"]),
        ("managed-identity", ["id-fin_1"], ["ab", "-id"]),
        ("resource-group", ["rg-fin", "rg.(1)"], ["rg.", ""]),
    ],
)
def test_name_rules(kind, good, bad):
    for name in good:
        assert name_problems(kind, name) == [], (kind, name)
    for name in bad:
        assert name_problems(kind, name), (kind, name)


def test_rules_are_keyed_by_kind():
    assert all(rule.kind == kind for kind, rule in NAME_RULES.items())


def test_locations():
    assert is_known_region("westeurope") and is_known_region("West Europe")
    assert not is_known_region("mars")
    assert canonical(" East US 2 ") == "eastus2"


@pytest.mark.parametrize(
    "value, expected",
    [
        ("@Microsoft.KeyVault(SecretUri=https://v.vault.azure.net/secrets/x/)", True),
        ("keyvault:my-secret", True),
        ("https://my-vault.vault.azure.net/secrets/key", True),
        ("my-secret", False),
        ("hunter2 with spaces", False),
    ],
)
def test_key_vault_references(value, expected):
    assert is_key_vault_reference(value) is expected


def test_secret_names_or_references():
    assert is_secret_name_or_reference("db-password")
    assert is_secret_name_or_reference("keyvault:db-password")
    assert not is_secret_name_or_reference("has spaces!")
    assert not is_secret_name_or_reference("x" * 200)


def test_raw_secret_kind():
    assert raw_secret_kind("hello world") is None
    assert raw_secret_kind("https://example.com/path") is None
    assert (
        raw_secret_kind("DefaultEndpointsProtocol=https;AccountKey=abc") == "a storage or bus key"
    )


@pytest.mark.parametrize(
    "value, expected",
    [("gpt-4.1", "gpt-4-1"), ("a b", "a-b"), ("--", "item"), ("ok-name", "ok-name")],
)
def test_slug(value, expected):
    assert slug(value) == expected
