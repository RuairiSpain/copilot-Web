"""Semantic rules that run on the configuration as authored."""

from __future__ import annotations

import pytest

from xfoundry.diagnostics import Severity
from xfoundry.schema.models import Hub, XFoundry
from xfoundry.validators import validate_declared

from .conftest import analyse, codes, gateway, hub_doc, iq, kb, mk

PUBLIC = {"network": {"mode": "public"}, "roles": {"admins": ["a"]}}
ARM = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers"


def test_minimal_is_valid_and_has_no_diagnostics():
    assert analyse(mk()).ok
    assert codes(mk()) == set()


# Rule 1 ----------------------------------------------------------------------------


def test_rule1_duplicate_project_names(expect):
    expect(mk(projects=[{"name": "a1"}, {"name": "a1"}]), "XF001", "projects")


def test_rule1_hub_name_collides_with_project(expect):
    expect(hub_doc(projects=[{"name": "shared"}]), "XF001", "hub.name")


# Rule 2 ----------------------------------------------------------------------------


@pytest.mark.parametrize(
    "doc",
    [
        mk(projects=[{"name": "fin", "agents": [{"name": "bot"}, {"name": "bot"}]}]),
        mk(agents=[{"name": "bot"}, {"name": "bot"}]),
        mk(
            agents=[{"name": "bot", "project": "fin"}],
            projects=[{"name": "fin", "agents": [{"name": "bot"}]}],
        ),
        mk(
            models={
                "deployments": [{"name": "m1", "model": "gpt-5"}, {"name": "m1", "model": "gpt-5"}]
            }
        ),
        mk(
            mcps=[
                {"name": "graph", "endpoint": "https://a.example"},
                {"name": "graph", "endpoint": "https://b.example"},
            ]
        ),
        mk(connectors=[{"name": "sp", "type": "sharepoint"}, {"name": "sp", "type": "graph"}]),
        mk(
            toolboxes=[{"name": "tb", "tools": [{"name": "t1", "type": "mcp", "reference": "x"}]}]
            * 2
        ),
        mk(iq=iq(kb("one"), kb("one"))),
        mk(projects=[{"name": "fin", "iq": iq(kb("one"), kb("one"))}]),
        mk(
            iq=iq(
                kb("one", sources=[{"name": "s1", "type": "web", "url": "https://x.example"}] * 2)
            )
        ),
        mk(
            gateway=gateway(
                endpoints=[
                    {"name": "ep1", "path": "/a", "target": "x", "targetType": "agent"},
                    {"name": "ep1", "path": "/b", "target": "x", "targetType": "agent"},
                ]
            )
        ),
        mk(events={"enabled": True, "entities": [{"name": "q1", "type": "queue"}] * 2}),
        mk(storage={"containers": [{"name": "abc"}, {"name": "abc"}]}),
        mk(
            runtime={
                "enabled": True,
                "image": "i",
                "scale": {"rules": [{"name": "r1", "type": "http"}] * 2},
            }
        ),
        mk(evaluation={"datasets": [{"name": "d1", "path": "p"}] * 2}),
    ],
)
def test_rule2_duplicate_names(doc, expect):
    expect(doc, "XF002")


def test_rule2_hub_scope_duplicates(expect):
    doc = hub_doc()
    doc["x-foundry"]["hub"]["mcps"] = [{"name": "graph", "endpoint": "https://a.example"}] * 2
    expect(doc, "XF002", "hub.mcps")


def test_rule2_same_name_in_different_scopes_is_an_override_not_a_duplicate():
    doc = hub_doc(
        projects=[
            {
                "name": "fin",
                "toolboxes": [
                    {"name": "tb", "tools": [{"name": "t1", "type": "function", "reference": "f"}]}
                ],
            }
        ]
    )
    doc["x-foundry"]["hub"]["toolboxes"] = [
        {"name": "tb", "tools": [{"name": "t1", "type": "function", "reference": "g"}]}
    ]
    assert analyse(doc).ok


# Rules 3 and 4 ----------------------------------------------------------------------


def test_rule3_hub_in_standalone_is_rejected_by_the_model_layer():
    cfg = XFoundry.model_validate(mk()["x-foundry"])
    forced = cfg.model_copy(update={"hub": Hub(name="shared")})
    assert {d.code for d in validate_declared(forced)} >= {"XF003"}
    forced = cfg.model_copy(
        update={"topology": cfg.topology.model_copy(update={"mode": "hub-spoke"})}
    )
    assert "XF003" in {d.code for d in validate_declared(forced)}


def test_rule3_schema_and_model_reject_hub_mismatch():
    assert "XF102" in codes(mk(hub={"name": "shared"}))
    assert "XF102" in codes(mk(topology={"mode": "hub-spoke"}))


def test_rule4_explicit_inherit_hub_without_hub(expect):
    expect(mk(projects=[{"name": "fin", "inheritHub": True}]), "XF004", "inheritHub")


def test_rule4_inherit_hub_default_and_false_are_fine_without_hub():
    assert analyse(mk(projects=[{"name": "fin", "inheritHub": False}])).ok


# Rule 5 (declared part) --------------------------------------------------------------


@pytest.mark.parametrize(
    "doc",
    [
        mk(agents=[{"name": "bot", "project": "nope"}]),
        mk(
            toolboxes=[
                {
                    "name": "tb",
                    "project": "nope",
                    "tools": [{"name": "t1", "type": "function", "reference": "f"}],
                }
            ]
        ),
        mk(iq={"project": "nope", "knowledgeBases": [kb()]}),
        mk(iq=iq(kb(project="nope"))),
        mk(runtime={"enabled": True, "image": "i", "project": "nope"}),
        mk(evaluation={"project": "nope"}),
        mk(
            gateway=gateway(
                endpoints=[
                    {
                        "name": "ep1",
                        "path": "/a",
                        "target": "x",
                        "targetType": "agent",
                        "project": "nope",
                    }
                ]
            )
        ),
        mk(
            iq=iq(
                kb(
                    routing={
                        "routes": [
                            {"name": "r1", "when": {"project": "nope"}, "knowledgeBase": "policies"}
                        ]
                    }
                )
            )
        ),
    ],
)
def test_rule5_unknown_project(doc, expect):
    expect(doc, "XF005", message="unknown project 'nope'")


def test_rule5_item_inside_a_project_cannot_target_another_project(expect):
    doc = mk(
        projects=[{"name": "aa"}, {"name": "bb", "agents": [{"name": "bot", "project": "aa"}]}]
    )
    expect(doc, "XF005", message="declared inside project 'bb'")


# Rule 16 -----------------------------------------------------------------------------


@pytest.mark.parametrize(
    "doc",
    [
        mk(models={"allowed": ["gpt-5"], "denied": ["gpt-5"]}),
        mk(projects=[{"name": "fin", "models": {"allowed": ["a1"], "denied": ["a1"]}}]),
        mk(gateway=gateway(models={"allowed": ["gpt-5"], "denied": ["gpt-5"]})),
        mk(governance={"modelPolicy": {"allowedModels": ["gpt-5"], "deniedModels": ["gpt-5"]}}),
    ],
)
def test_rule16_allowed_and_denied_overlap(doc, expect):
    expect(doc, "XF016", message="both allowed and denied")


def test_rule16_hub_overlap(expect):
    doc = hub_doc()
    doc["x-foundry"]["hub"]["models"] = {"allowed": ["gpt-5"], "denied": ["gpt-5"]}
    expect(doc, "XF016", "hub.models")


# Rule 17 -----------------------------------------------------------------------------


@pytest.mark.parametrize(
    "value",
    [
        "AccountKey=abc123==;EndpointSuffix=core.windows.net",
        "https://acct.blob.core.windows.net/c?sv=1&sig=AbCdEfGhIjKlMnOpQrStUvWxYz0123456789",
        "-----BEGIN RSA PRIVATE KEY-----",
        "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
        "sk-abcdefghijklmnopqrstuvwxyz123456",
        "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
        "Server=db;Password=hunter2",
        "https://user:pass@example.com/path",
    ],
)
def test_rule17_raw_secret_values_anywhere(value, expect):
    doc = mk(projects=[{"name": "fin", "description": value}])
    expect(doc, "XF017", "description", "appears to contain")


def test_rule17_sensitive_environment_and_header_names(expect):
    doc = mk(
        runtime={
            "enabled": True,
            "image": "i",
            "environment": {"DB_PASSWORD": "hunter2", "MAX_TOKENS": 4096, "LOG_LEVEL": "info"},
        }
    )
    found = [d for d in analyse(doc).diagnostics if d.code == "XF017"]
    assert [d.path for d in found] == ["x-foundry.runtime.environment.DB_PASSWORD"]
    doc = mk(
        mcps=[
            {
                "name": "graph",
                "endpoint": "https://a.example",
                "headers": {"x-api-key": "abc", "accept": "json"},
            }
        ]
    )
    expect(doc, "XF017", "headers.x-api-key")


@pytest.mark.parametrize(
    "reference",
    [
        "@Microsoft.KeyVault(SecretUri=https://kv.vault.azure.net/secrets/key/)",
        "keyvault:search-key",
        "https://my-vault.vault.azure.net/secrets/search-key",
        "https://my-vault.vault.azure.net/secrets/search-key/0123456789abcdef0123456789abcdef",
    ],
)
def test_rule17_key_vault_references_are_accepted(reference):
    doc = mk(runtime={"enabled": True, "image": "i", "environment": {"SEARCH_API_KEY": reference}})
    assert "XF017" not in codes(doc)


def test_rule17_runtime_secrets_and_secret_refs(expect):
    expect(
        mk(runtime={"enabled": True, "image": "i", "secrets": {"db": "not a name!"}}),
        "XF017",
        "secrets.db",
    )
    expect(
        mk(
            mcps=[
                {
                    "name": "graph",
                    "endpoint": "https://a.example",
                    "authentication": {"mode": "apiKey", "secretRef": "bad value"},
                }
            ]
        ),
        "XF017",
        "secretRef",
    )
    ok = mk(runtime={"enabled": True, "image": "i", "secrets": {"db": "db-password"}})
    assert "XF017" not in codes(ok)


def test_rule17_api_key_requires_secret_ref(expect):
    expect(
        mk(connectors=[{"name": "svc", "type": "api", "authentication": {"mode": "apiKey"}}]),
        "XF119",
    )


# Rule 22 -----------------------------------------------------------------------------


@pytest.mark.parametrize(
    "component, existing, extra",
    [
        ("search", f"{ARM}/Microsoft.Search/searchServices/s1", {"sku": "basic"}),
        ("search", f"{ARM}/Microsoft.Search/searchServices/s1", {"replicas": 2}),
        ("storage", f"{ARM}/Microsoft.Storage/storageAccounts/st1", {"sku": "Standard_LRS"}),
        ("redis", f"{ARM}/Microsoft.Cache/redis/r1", {"capacity": 2}),
        ("keyVault", f"{ARM}/Microsoft.KeyVault/vaults/kv1", {"sku": "premium"}),
        (
            "managedIdentity",
            f"{ARM}/Microsoft.ManagedIdentity/userAssignedIdentities/id1",
            {"tags": {"a": "b"}},
        ),
    ],
)
def test_rule22_existing_resource_cannot_carry_creation_settings(
    component, existing, extra, expect
):
    expect(mk(**{component: {"existingResourceId": existing, **extra}}), "XF022", next(iter(extra)))


@pytest.mark.parametrize(
    "component, existing",
    [
        ("search", f"{ARM}/Microsoft.Search/searchServices/s1"),
        ("storage", f"{ARM}/Microsoft.Storage/storageAccounts/st1"),
        ("redis", f"{ARM}/Microsoft.Cache/redisEnterprise/r1"),
        ("keyVault", f"{ARM}/Microsoft.KeyVault/vaults/kv1"),
        ("managedIdentity", f"{ARM}/Microsoft.ManagedIdentity/userAssignedIdentities/id1"),
    ],
)
def test_rule22_valid_existing_resource(component, existing):
    assert analyse(mk(**{component: {"existingResourceId": existing}})).ok


def test_rule22_existing_id_must_match_the_component_type(expect):
    expect(
        mk(storage={"existingResourceId": f"{ARM}/Microsoft.KeyVault/vaults/kv1"}),
        "XF022",
        "existingResourceId",
    )


def test_rule22_name_and_existing_are_mutually_exclusive():
    assert "XF102" in codes(
        mk(
            storage={
                "name": "stacct",
                "existingResourceId": f"{ARM}/Microsoft.Storage/storageAccounts/st1",
            }
        )
    )


def test_rule22_project_level_search_is_checked_too(expect):
    doc = mk(
        projects=[
            {
                "name": "fin",
                "search": {"existingResourceId": f"{ARM}/Microsoft.Storage/storageAccounts/x"},
            }
        ]
    )
    expect(doc, "XF022", "projects[fin].search")


@pytest.mark.parametrize(
    "registry, message",
    [
        ({"mode": "managed", "server": "x.azurecr.io"}, "managed registry cannot set"),
        ({"mode": "existing"}, "requires resourceId"),
        (
            {"mode": "existing", "resourceId": f"{ARM}/Microsoft.Storage/storageAccounts/x"},
            "not a container registry",
        ),
        ({"mode": "external"}, "requires server"),
        (
            {"mode": "external", "server": "ghcr.io", "name": "acr12345"},
            "applies only to a managed registry",
        ),
    ],
)
def test_rule22_registry_modes(registry, message, expect):
    expect(
        mk(runtime={"enabled": True, "image": "i", "registry": registry}), "XF022", message=message
    )


def test_rule22_valid_existing_registry():
    reg = {"mode": "existing", "resourceId": f"{ARM}/Microsoft.ContainerRegistry/registries/acr1"}
    assert analyse(mk(runtime={"enabled": True, "image": "i", "registry": reg})).ok


# Rule 23 -----------------------------------------------------------------------------


def test_rule23_unknown_regions(expect):
    expect(mk(defaults={"location": "atlantis"}), "XF023", "defaults.location")
    expect(mk(projects=[{"name": "fin", "location": "moon"}]), "XF023", "projects[fin].location")
    expect(
        mk(models={"deployments": [{"name": "m1", "model": "gpt-5", "location": "nowhere"}]}),
        "XF023",
        "deployments[m1]",
    )


def test_rule23_region_display_names_are_accepted():
    assert analyse(mk(defaults={"location": "West Europe"})).ok


def test_rule23_data_residency(expect):
    doc = mk(
        defaults={"location": "eastus"}, governance={"dataResidency": ["westeurope", "northeurope"]}
    )
    expect(doc, "XF023", message="outside governance.dataResidency")
    expect(mk(governance={"dataResidency": ["atlantis"]}), "XF023", "dataResidency")
    assert analyse(
        mk(defaults={"location": "westeurope"}, governance={"dataResidency": ["westeurope"]})
    ).ok


def test_rule23_cross_region_inheritance_warns_not_errors():
    doc = hub_doc(
        defaults={"location": "westeurope"},
        projects=[{"name": "fin", "location": "eastus"}],
        security=PUBLIC,
    )
    result = analyse(doc)
    assert result.ok
    assert {d.code for d in result.diagnostics if d.severity is Severity.WARNING} == {"XF120"}
    assert result.plan.warnings[0].code == "XF120"


def test_rule23_project_location_differing_from_the_account_warns():
    doc = mk(
        defaults={"location": "westeurope"},
        projects=[{"name": "fin", "location": "eastus"}],
        security=PUBLIC,
    )
    assert codes(doc, Severity.WARNING) == {"XF120"}


def test_rule23_region_mismatch_is_an_error_in_private_mode(expect):
    doc = mk(defaults={"location": "westeurope"}, projects=[{"name": "fin", "location": "eastus"}])
    expect(doc, "XF120", "projects[fin].location")
    hub = hub_doc(
        defaults={"location": "westeurope"}, projects=[{"name": "fin", "location": "eastus"}]
    )
    assert codes(hub, Severity.ERROR) == {"XF120"}


# Rule 24 -----------------------------------------------------------------------------


@pytest.mark.parametrize(
    "doc, path",
    [
        (mk(storage={"name": "My-Storage"}), "storage.name"),
        (mk(storage={"name": "ab"}), "storage.name"),
        (mk(keyVault={"name": "kv--double"}), "keyVault.name"),
        (mk(search={"name": "Search1"}), "search.name"),
        (mk(redis={"name": "r--x"}), "redis.name"),
        (mk(gateway=gateway(name="1apim")), "gateway.name"),
        (mk(events={"enabled": True, "namespace": "1bus-name"}), "events.namespace"),
        (mk(managedIdentity={"name": "ab"}), "managedIdentity.name"),
        (mk(runtime={"enabled": True, "image": "i", "name": "Upper"}), "runtime.name"),
        (
            mk(
                runtime={
                    "enabled": True,
                    "image": "i",
                    "registry": {"mode": "managed", "name": "reg-1"},
                }
            ),
            "registry.name",
        ),
        (mk(defaults={"resourceGroup": "rg."}), "defaults.resourceGroup"),
        (mk(projects=[{"name": "fin", "resourceGroup": "bad!"}]), "projects[fin].resourceGroup"),
    ],
)
def test_rule24_explicit_names_follow_provider_constraints(doc, path, expect):
    expect(doc, "XF024", path)


def test_rule24_valid_explicit_names():
    doc = mk(
        storage={"name": "stfinance01"},
        keyVault={"name": "kv-finance"},
        search={"name": "srch-finance"},
        redis={"name": "redis-finance"},
        managedIdentity={"name": "id-finance"},
        defaults={"resourceGroup": "rg-finance"},
    )
    assert analyse(doc).ok


# Other declared-phase rules ------------------------------------------------------------


def test_admins_empty_is_a_warning():
    doc = mk(security={"roles": {"admins": []}})
    result = analyse(doc)
    assert result.ok
    assert codes(doc, Severity.WARNING) == {"XF114"}


def test_restricted_network_needs_allowed_ips(expect):
    expect(mk(security={"network": {"mode": "restricted"}, "roles": {"admins": ["a"]}}), "XF105")
    ok = mk(
        security={
            "network": {"mode": "restricted", "allowedIps": ["203.0.113.0/24"]},
            "roles": {"admins": ["a"]},
        }
    )
    assert analyse(ok).ok


def test_public_network_conflicts(expect):
    private = {
        "network": {"mode": "private"},
        "roles": {"admins": ["a"]},
        "publicNetworkAccess": True,
    }
    expect(mk(security=private), "XF021", "security.publicNetworkAccess")
    expect(mk(storage={"publicNetworkAccess": True}), "XF021", "storage.publicNetworkAccess")
    public = {"network": {"mode": "public"}, "roles": {"admins": ["a"]}}
    expect(
        mk(security=public, redis={"enabled": True, "publicNetworkAccess": True}),
        "XF106",
        "redis.publicNetworkAccess",
    )
    ok = {**public, "publicNetworkAccess": True}
    assert analyse(mk(security=ok, redis={"enabled": True, "publicNetworkAccess": True})).ok


def test_local_authentication_conflict(expect):
    expect(mk(search={"localAuthentication": True}), "XF106", "search.localAuthentication")
    ok = {"roles": {"admins": ["a"]}, "localAuthentication": True}
    assert analyse(mk(security=ok, search={"localAuthentication": True})).ok


def test_private_mode_requirements(expect):
    expect(mk(managedIdentity={"enabled": False}), "XF021", "managedIdentity.enabled")
    expect(
        mk(runtime={"enabled": True, "image": "i", "ingress": {"external": True}}),
        "XF021",
        "ingress.external",
    )
    no_dns = {"network": {"mode": "private", "privateDns": False}, "roles": {"admins": ["a"]}}
    assert codes(mk(security=no_dns), Severity.WARNING) == {"XF021"}


def test_redis_service_and_sku_must_match(expect):
    expect(
        mk(redis={"enabled": True, "service": "azure-cache-for-redis", "sku": "balanced"}),
        "XF106",
        "redis.sku",
    )
    expect(mk(redis={"enabled": True, "sku": "premium"}), "XF106", "redis.sku")
    existing = {"existingResourceId": f"{ARM}/Microsoft.Cache/redis/r1"}
    assert analyse(mk(redis=existing)).ok


def test_runtime_rules(expect):
    expect(
        mk(runtime={"enabled": True, "image": "i", "scale": {"minReplicas": 5, "maxReplicas": 2}}),
        "XF108",
        "scale",
    )
    expect(
        mk(
            runtime={
                "enabled": True,
                "image": "i",
                "registry": {"mode": "managed", "authentication": "credentials"},
            }
        ),
        "XF108",
        "registry.authentication",
    )
    odd = mk(runtime={"enabled": True, "image": "i", "resources": {"cpu": 1, "memory": "3Gi"}})
    assert codes(odd, Severity.WARNING) == {"XF108"}
    assert analyse(odd).ok
    disabled = mk(runtime={"enabled": False, "scale": {"minReplicas": 5, "maxReplicas": 2}})
    assert "XF108" not in codes(disabled)


@pytest.mark.parametrize(
    "events",
    [
        {"enabled": True, "provider": "eventGrid", "entities": [{"name": "q1", "type": "queue"}]},
        {
            "enabled": True,
            "entities": [{"name": "es", "type": "eventSubscription", "parent": "t1"}],
        },
        {"enabled": True, "entities": [{"name": "sub1", "type": "subscription"}]},
        {
            "enabled": True,
            "entities": [
                {"name": "q1", "type": "queue"},
                {"name": "sub1", "type": "subscription", "parent": "q1"},
            ],
        },
        {"enabled": True, "entities": [{"name": "q1", "type": "queue", "parent": "x"}]},
        {"enabled": True, "provider": "eventHubs", "entities": [{"name": "t1", "type": "topic"}]},
    ],
)
def test_event_entity_rules(events, expect):
    expect(mk(events=events), "XF109")


def test_valid_events():
    events = {
        "enabled": True,
        "entities": [
            {"name": "t1", "type": "topic"},
            {"name": "sub1", "type": "subscription", "parent": "t1"},
        ],
    }
    assert analyse(mk(events=events)).ok


def test_cron_expressions(expect):
    expect(mk(iq=iq(kb(refresh={"schedule": "daily"}))), "XF118", "refresh.schedule")
    expect(
        mk(evaluation={"enabled": True, "schedule": "every day"}), "XF118", "evaluation.schedule"
    )


def test_hosted_agents_need_a_source_or_runtime(expect):
    expect(mk(agents=[{"name": "bot", "kind": "hosted"}]), "XF112")
    assert analyse(
        mk(
            agents=[{"name": "bot", "kind": "hosted", "source": "./bot"}],
            models={"default": "gpt-5", "allowed": ["gpt-5"]},
        )
    ).ok


# Hardening rules --------------------------------------------------------------------------


def restricted(*ips):
    return {"network": {"mode": "restricted", "allowedIps": list(ips)}, "roles": {"admins": ["a"]}}


@pytest.mark.parametrize(
    "value", ["10.0.0.0/8", "172.20.1.0/24", "192.168.1.5", "100.64.0.0/10", "fd00::/8"]
)
def test_xf121_private_ranges_are_rejected_in_firewall_rules(value, expect):
    expect(mk(security=restricted(value)), "XF121", "allowedIps", "private range")


@pytest.mark.parametrize("value", ["0.0.0.0/0", "::/0"])
def test_xf121_allow_all_is_rejected(value, expect):
    expect(mk(security=restricted(value)), "XF121", message="whole internet")
    expect(
        mk(gateway=gateway(security={"allowIps": [value]}), observability={}), "XF121", "allowIps"
    )


def test_xf121_public_ranges_and_documentation_ranges_are_fine():
    assert analyse(mk(security=restricted("203.0.113.0/24", "198.51.100.7"))).ok
    private_mode = {
        "network": {"mode": "private", "allowedIps": ["10.0.0.0/8"]},
        "roles": {"admins": ["a"]},
    }
    assert analyse(mk(security=private_mode)).ok  # not a firewall rule in private mode


@pytest.mark.parametrize(
    "doc",
    [
        mk(mcps=[{"name": "graph", "endpoint": "http://graph.example/mcp"}]),
        mk(mcps=[{"name": "graph", "endpoint": "https://169.254.169.254/metadata"}]),
        mk(mcps=[{"name": "graph", "endpoint": "https://localhost/mcp"}]),
        mk(mcps=[{"name": "graph", "endpoint": "https://[::1]/mcp"}]),
        mk(connectors=[{"name": "svc", "type": "api", "endpoint": "http://svc.example"}]),
        mk(iq=iq(kb(sources=[{"name": "web1", "type": "web", "url": "http://x.example"}]))),
        mk(
            managedIdentity={
                "federatedCredentials": [
                    {"name": "gh", "issuer": "http://issuer.example", "subject": "s"}
                ]
            }
        ),
    ],
)
def test_xf122_insecure_or_internal_endpoints(doc, expect):
    expect(doc, "XF122")


def test_xf122_https_endpoints_are_fine():
    assert analyse(mk(mcps=[{"name": "graph", "endpoint": "https://graph.contoso.com/mcp"}])).ok


@pytest.mark.parametrize(
    "search, message",
    [
        ({"sku": "free", "replicas": 2}, "at most 1 replica"),
        ({"sku": "basic", "partitions": 2}, "at most 1 partition"),
        ({"sku": "basic", "replicas": 4}, "at most 3 replica"),
        ({"sku": "standard", "replicas": 12, "partitions": 4}, "36 search units"),
        ({"sku": "free"}, "private endpoints"),
    ],
)
def test_xf123_search_sizing(search, message, expect):
    expect(mk(search=search), "XF123", message=message)


def test_xf123_valid_sizing_and_prod_replica_warning():
    assert analyse(mk(search={"sku": "basic", "replicas": 3})).ok
    assert analyse(mk(search={"sku": "storage_optimized_l1", "replicas": 3, "partitions": 12})).ok
    prod = mk(defaults={"environment": "prod"}, search={"sku": "standard"})
    assert analyse(prod).ok and codes(prod, Severity.WARNING) == {"XF123"}
    ha = mk(defaults={"environment": "prod"}, search={"sku": "standard", "replicas": 2})
    assert codes(ha) == set()
    assert codes(mk(search={"sku": "free"}, security=PUBLIC)) == set()
    existing = mk(
        defaults={"environment": "prod"},
        search={"existingResourceId": f"{ARM}/Microsoft.Search/searchServices/s1"},
    )
    assert codes(existing) == set()


def test_search_sku_uses_the_arm_spelling():
    assert "XF102" in codes(mk(search={"sku": "storage_optimised_l1"}))


def test_xf124_gateway_sku_in_private_mode(expect):
    expect(mk(gateway=gateway(sku="Consumption"), observability={}), "XF124", "gateway.sku")
    basic = mk(gateway=gateway(sku="BasicV2"), observability={})
    assert analyse(basic).ok and codes(basic, Severity.WARNING) == {"XF124"}
    assert codes(mk(gateway=gateway(sku="PremiumV2"), observability={})) == set()
    assert codes(mk(gateway=gateway(sku="Consumption"), observability={}, security=PUBLIC)) == set()


def test_xf125_azure_cache_for_redis_cannot_be_created(expect):
    expect(
        mk(redis={"enabled": True, "service": "azure-cache-for-redis", "sku": "standard"}),
        "XF125",
        "redis.service",
    )
    assert "XF125" not in codes(mk(redis={"enabled": False, "service": "azure-cache-for-redis"}))
