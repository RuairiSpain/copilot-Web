from __future__ import annotations

import pytest

from xfoundry.normalise import normalise
from xfoundry.normalise.inheritance import merge_models, merge_named
from xfoundry.parser import parse_mapping
from xfoundry.schema.models import Agent, ModelConfiguration, ModelDeployment

from .conftest import analyse, gateway, hub_doc, iq, kb, mk


def norm(doc):
    result = normalise(parse_mapping(doc).config)
    return result.config


def plan(doc):
    analysis = analyse(doc)
    assert analysis.ok, [str(d) for d in analysis.diagnostics]
    return analysis.plan.config


# Defaults, tags, principals ---------------------------------------------------------------------


def test_defaults_tags_and_environment():
    cfg = plan(
        mk(
            defaults={"environment": "prod", "location": "westeurope", "tags": {"team": "x"}},
            tags={"owner": "me"},
        )
    )
    assert cfg.environment == "prod"
    assert cfg.tags == {
        "environment": "prod",
        "managed-by": "x-foundry",
        "team": "x",
        "owner": "me",
    }
    project = cfg.project("finance")
    assert project.tags["project"] == "finance"
    assert project.location == "westeurope"
    assert project.display_name == "finance"


def test_principals_are_resolved_deduplicated_and_merged():
    guid = "11111111-2222-3333-4444-555555555555"
    doc = mk(
        security={
            "roles": {
                "admins": ["Admins", "admins", guid, {"type": "user", "name": "ann"}],
                "developers": ["Devs"],
            }
        },
        projects=[
            {
                "name": "fin",
                "roles": {
                    "developers": ["Devs", "FinDevs"],
                    "consumers": [{"type": "servicePrincipal", "id": guid}],
                },
            }
        ],
    )
    cfg = plan(doc)
    admins = cfg.roles.admins
    assert [a.name or a.id for a in admins] == ["Admins", guid, "ann"]
    assert admins[1].id == guid and admins[1].name is None
    assert admins[2].type == "user"
    project = cfg.project("fin")
    assert [d.name for d in project.roles.developers] == ["Devs", "FinDevs"]
    assert project.roles.consumers[0].type == "servicePrincipal"
    assert project.roles.admins == admins


# Implicit resources ----------------------------------------------------------------------------


def kinds(cfg):
    return {(i.kind, i.name) for i in cfg.implicit}


def test_minimal_deploys_only_identity_and_private_network():
    cfg = plan(mk())
    assert kinds(cfg) == {("managed-identity", "identity"), ("network", "vnet")}
    assert cfg.storage is None and cfg.redis is None and cfg.observability is None
    assert cfg.key_vault is None and cfg.events is None and cfg.gateway is None
    assert cfg.managed_identity.enabled


def test_rule20_iq_creates_search_and_shows_the_resolved_dependency():
    cfg = plan(mk(models={"default": "gpt-5", "allowed": ["gpt-5"]}, iq=iq(kb())))
    assert ("search", "search") in kinds(cfg)
    assert cfg.scopes["root"].search is not None and cfg.scopes["root"].search.enabled
    assert cfg.project("finance").search_scope == "root"


def test_iq_uses_an_explicit_search_instead_of_creating_one():
    cfg = plan(mk(search={"sku": "basic"}, iq=iq(kb())))
    assert ("search", "search") not in kinds(cfg)
    assert cfg.scopes["root"].search.sku == "basic"


def test_search_without_iq_is_resolved_when_declared():
    cfg = plan(mk(search={}))
    assert cfg.project("finance").search_scope == "root"
    assert plan(mk()).project("finance").search_scope is None


def test_blob_sources_create_storage_with_containers():
    cfg = plan(mk(iq=iq(kb())))
    assert cfg.storage.purposes == ["knowledge"]
    assert {c.name for c in cfg.storage.containers} == {"knowledge", "policies"}
    assert ("storage", "storage") in kinds(cfg)


def test_declared_storage_gains_required_purposes():
    cfg = plan(
        mk(
            storage={"purposes": ["documents"]},
            iq=iq(kb()),
            evaluation={"enabled": True, "datasets": [{"name": "golden", "path": "g.jsonl"}]},
        )
    )
    assert cfg.storage.purposes == ["documents", "knowledge", "evaluations"]
    assert ("storage-purpose", "knowledge") in kinds(cfg)
    assert ("storage", "storage") not in kinds(cfg)
    assert {c.name for c in cfg.storage.containers} >= {
        "documents",
        "knowledge",
        "evaluations",
        "policies",
    }


def test_connector_backed_sources_do_not_need_storage():
    doc = mk(
        connectors=[{"name": "sp", "type": "sharepoint"}],
        iq=iq(
            kb(sources=[{"name": "site", "type": "sharepoint", "site": "hr", "connection": "sp"}])
        ),
    )
    assert plan(doc).storage is None


def test_runtime_implies_observability_and_registry_for_sources():
    cfg = plan(mk(runtime={"enabled": True, "source": "./app"}))
    assert ("observability", "observability") in kinds(cfg)
    assert ("container-registry", "registry") in kinds(cfg)
    assert any(pe.component == "registry" for pe in cfg.network.private_endpoints)
    image = plan(mk(runtime={"enabled": True, "image": "ghcr.io/x/y"}))
    assert ("container-registry", "registry") not in kinds(image)


def test_secret_references_imply_key_vault():
    doc = mk(
        mcps=[
            {
                "name": "graph",
                "endpoint": "https://a.example",
                "authentication": {"mode": "apiKey", "secretRef": "graph-key"},
            }
        ]
    )
    assert ("key-vault", "key-vault") in kinds(plan(doc))
    doc = mk(runtime={"enabled": True, "image": "i", "secrets": {"db": "db-password"}})
    assert ("key-vault", "key-vault") in kinds(plan(doc))
    doc = mk(
        connectors=[
            {
                "name": "svc",
                "type": "api",
                "authentication": {"mode": "apiKey", "secretRef": "svc-key"},
            }
        ]
    )
    assert plan(doc).key_vault is not None


def test_explicit_key_vault_is_not_duplicated():
    doc = mk(
        keyVault={"sku": "premium"},
        runtime={"enabled": True, "image": "i", "secrets": {"db": "db-password"}},
    )
    cfg = plan(doc)
    assert cfg.key_vault.sku == "premium"
    assert ("key-vault", "key-vault") not in kinds(cfg)


def test_implicit_model_deployments():
    cfg = plan(mk(models={"default": "gpt-5", "allowed": ["gpt-5", "gpt-4.1"]}))
    names = [d.name for d in cfg.scopes["root"].models.deployments]
    assert names == ["gpt-5", "gpt-4-1"]
    assert cfg.project("finance").models.default == "gpt-5"
    assert ("model-deployment", "gpt-5") in kinds(cfg)


def test_embedding_deployment_is_reused_or_created():
    reuse = plan(
        mk(
            models={"deployments": [{"name": "emb", "model": "text-embedding-3-large"}]},
            iq=iq(kb()),
        )
    )
    assert reuse.scopes["root"].knowledge_bases[0].index.vector.deployment == "emb"
    assert len(reuse.scopes["root"].models.deployments) == 1
    created = plan(mk(iq=iq(kb())))
    assert [d.name for d in created.scopes["root"].models.deployments] == ["text-embedding-3-large"]
    novector = plan(
        mk(iq=iq(kb(index={"vector": {"enabled": False}}, retrieval={"mode": "keyword"})))
    )
    assert novector.scopes["root"].models.deployments == []


def test_index_is_materialised():
    cfg = plan(mk(iq=iq(kb())))
    index = cfg.scopes["root"].knowledge_bases[0].index
    assert index.name == "policies"
    fields = {f.name: f for f in index.fields}
    assert set(fields) == {"id", "content", "title", "contentVector"}
    assert fields["id"].key and fields["content"].searchable
    vector = fields["contentVector"]
    assert (vector.dimensions, vector.vector_profile) == (3072, "default-vector-profile")
    assert index.semantic.title_field == "title" and index.semantic.content_fields == ["content"]


def test_semantic_fields_follow_custom_index_fields():
    fields = [
        {"name": "docId", "type": "Edm.String", "key": True},
        {"name": "body", "type": "Edm.String", "searchable": True},
    ]
    index = {
        "keyField": "docId",
        "contentField": "body",
        "fields": fields,
        "vector": {"enabled": False},
    }
    cfg = plan(mk(iq=iq(kb(index=index, retrieval={"mode": "keyword"}))))
    built = cfg.scopes["root"].knowledge_bases[0].index
    assert built.semantic.content_fields == ["body"]
    assert "id" not in {f.name for f in built.fields}


def test_redis_default_sku_follows_the_service():
    cfg = plan(mk(redis={"enabled": True, "service": "azure-cache-for-redis"}))
    assert cfg.redis.sku == "standard"
    assert plan(mk(redis={"enabled": True})).redis.sku == "balanced"


def test_container_memory_follows_cpu_unless_set():
    cfg = plan(mk(runtime={"enabled": True, "image": "i", "resources": {"cpu": 2}}))
    assert cfg.scopes["root"].runtime.resources.memory == "4Gi"
    agents = [
        {
            "name": "bot",
            "kind": "hosted",
            "runtime": {"enabled": True, "image": "i", "resources": {"cpu": 0.5}},
        }
    ]
    cfg = plan(mk(models={"default": "gpt-5", "allowed": ["gpt-5"]}, agents=agents))
    assert cfg.scopes["root"].agents[0].runtime.resources.memory == "1Gi"


# Network ------------------------------------------------------------------------------------------


def test_rule21_private_mode_derives_private_connectivity():
    doc = mk(
        storage={"hierarchicalNamespace": True},
        keyVault={},
        redis={"enabled": True},
        events={"enabled": True},
        search={},
        observability={},
    )
    net = plan(doc).network
    assert net.mode == "private" and net.vnet == "create" and net.private_dns
    assert {(pe.component, pe.group) for pe in net.private_endpoints} == {
        ("foundry", "account"),
        ("search:root", "searchService"),
        ("storage", "blob"),
        ("storage", "dfs"),
        ("key-vault", "vault"),
        ("redis", "redisCache"),
        ("events", "namespace"),
    }
    assert "privatelink.search.windows.net" in net.private_dns_zones
    assert "privatelink.dfs.core.windows.net" in net.private_dns_zones


def test_private_mode_with_existing_vnet_or_no_dns():
    vnet = "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/vnet1"
    security = {
        "roles": {"admins": ["a"]},
        "network": {"mode": "private", "existingVnetResourceId": vnet},
    }
    net = plan(mk(security=security)).network
    assert net.vnet == "existing" and net.existing_vnet_resource_id == vnet
    nodns = {"roles": {"admins": ["a"]}, "network": {"mode": "private", "privateDns": False}}
    net = plan(mk(security=nodns)).network
    assert net.private_dns_zones == [] and net.private_endpoints


def test_non_private_modes_have_no_private_endpoints():
    public = {"roles": {"admins": ["a"]}, "network": {"mode": "public"}}
    net = plan(mk(security=public)).network
    assert net.vnet is None and net.private_endpoints == []
    restricted = {
        "roles": {"admins": ["a"]},
        "network": {"mode": "restricted", "allowedIps": ["10.0.0.0/8"]},
    }
    net = plan(mk(security=restricted)).network
    assert net.allowed_ips == ["10.0.0.0/8"] and net.private_endpoints == []


# Hub and inheritance ---------------------------------------------------------------------------------


def hub_with(hub_extra=None, **top):
    doc = hub_doc(**top)
    doc["x-foundry"]["hub"].update(hub_extra or {})
    return doc


def test_spokes_inherit_hub_resources_and_override_by_name():
    tool = lambda ref: [{"name": "t1", "type": "function", "reference": ref}]  # noqa: E731
    doc = hub_with(
        {
            "toolboxes": [{"name": "tb", "tools": tool("hub")}],
            "mcps": [{"name": "graph", "endpoint": "https://hub.example"}],
        },
        projects=[
            {
                "name": "aa",
                "toolboxes": [
                    {"name": "tb", "tools": tool("spoke")},
                    {"name": "extra", "tools": tool("x")},
                ],
            },
            {"name": "bb"},
            {"name": "cc", "inheritHub": False},
        ],
    )
    cfg = plan(doc)
    aa, bb, cc = cfg.project("aa"), cfg.project("bb"), cfg.project("cc")
    assert [t.name for t in aa.toolboxes] == ["tb", "extra"]
    assert aa.toolboxes[0].tools[0].reference == "spoke"
    assert aa.origins["toolbox:tb"] == "project:aa" and bb.origins["toolbox:tb"] == "hub"
    assert bb.toolboxes[0].tools[0].reference == "hub"
    assert [m.name for m in bb.mcps] == ["graph"]
    assert cc.toolboxes == [] and cc.mcps == [] and not cc.inherits_hub


def test_inheritance_flags_switch_off_individual_resource_kinds():
    doc = hub_with(
        {
            "inheritance": {"toolboxes": False, "mcps": False, "models": False},
            "toolboxes": [
                {"name": "tb", "tools": [{"name": "t1", "type": "function", "reference": "f"}]}
            ],
            "mcps": [{"name": "graph", "endpoint": "https://hub.example"}],
            "models": {"default": "gpt-5", "allowed": ["gpt-5"]},
        },
    )
    project = plan(doc).project("finance")
    assert project.toolboxes == [] and project.mcps == [] and project.models.default is None


def test_hub_iq_is_not_inherited_by_default_but_can_be():
    doc = hub_with({"iq": iq(kb())})
    assert plan(doc).project("finance").knowledge_bases == []
    doc = hub_with({"iq": iq(kb()), "inheritance": {"iq": True}})
    cfg = plan(doc)
    assert [k.name for k in cfg.project("finance").knowledge_bases] == ["policies"]
    assert cfg.project("finance").origins["knowledgeBase:policies"] == "hub"
    assert cfg.project("finance").search_scope == "root"
    assert cfg.hub.search_scope == "root"


def test_search_resolution_precedence():
    doc = hub_with(
        {"search": {"sku": "standard2"}},
        search={"sku": "basic"},
        projects=[
            {"name": "aa"},
            {"name": "bb", "search": {"sku": "free"}},
            {"name": "cc", "inheritHub": False},
        ],
    )
    cfg = plan(doc)
    assert cfg.project("aa").search_scope == "hub"
    assert cfg.project("bb").search_scope == "project:bb"
    assert cfg.project("cc").search_scope == "root"
    assert cfg.hub.search_scope == "hub"
    no_inherit = hub_with({"search": {}, "inheritance": {"search": False}}, search={"sku": "basic"})
    assert plan(no_inherit).project("finance").search_scope == "root"


def test_models_merge_across_levels():
    doc = hub_with(
        {"models": {"default": "gpt-5", "allowed": ["gpt-5", "gpt-5-mini"], "denied": ["gpt-3"]}},
        models={"denied": ["gpt-2"]},
        projects=[
            {
                "name": "fin",
                "models": {"default": "gpt-5-mini", "allowed": ["gpt-5-mini"], "denied": ["gpt-1"]},
            }
        ],
    )
    models = plan(doc).project("fin").models
    assert models.default == "gpt-5-mini"
    assert models.allowed == ["gpt-5-mini"]
    assert models.denied == ["gpt-2", "gpt-3", "gpt-1"]
    assert {d.model for d in models.deployments} == {"gpt-5", "gpt-5-mini"}


def test_project_deployment_overrides_inherited_deployment():
    doc = hub_with(
        {"models": {"deployments": [{"name": "chat", "model": "gpt-5", "capacity": 50}]}},
        projects=[
            {
                "name": "fin",
                "models": {"deployments": [{"name": "chat", "model": "gpt-5", "capacity": 5}]},
            }
        ],
    )
    deployments = plan(doc).project("fin").models.deployments
    assert [(d.name, d.capacity) for d in deployments] == [("chat", 5)]


def test_root_items_assigned_to_a_project_move_into_it():
    doc = mk(
        projects=[{"name": "aa"}, {"name": "bb"}],
        models={"default": "gpt-5", "allowed": ["gpt-5"]},
        agents=[{"name": "shared"}, {"name": "only-bb", "project": "bb"}],
        iq={"project": "aa", "knowledgeBases": [kb("kb-aa"), kb("kb-bb", project="bb")]},
        runtime={"enabled": True, "image": "i", "project": "bb"},
        evaluation={"enabled": True, "project": "aa"},
    )
    cfg = plan(doc)
    aa, bb = cfg.project("aa"), cfg.project("bb")
    assert [a.name for a in aa.agents] == ["shared"]
    assert [a.name for a in bb.agents] == ["shared", "only-bb"]
    assert [k.name for k in aa.knowledge_bases] == ["kb-aa"]
    assert [k.name for k in bb.knowledge_bases] == ["kb-bb"]
    assert aa.runtime is None and bb.runtime_scope == "project:bb"
    assert aa.evaluation.enabled and bb.evaluation is None


def test_root_runtime_applies_to_every_project_and_project_runtime_wins():
    doc = mk(
        projects=[{"name": "aa"}, {"name": "bb", "runtime": {"enabled": True, "image": "mine"}}],
        runtime={"enabled": True, "image": "shared"},
    )
    cfg = plan(doc)
    assert cfg.project("aa").runtime.image == "shared" and cfg.project("aa").runtime_scope == "root"
    assert (
        cfg.project("bb").runtime.image == "mine"
        and cfg.project("bb").runtime_scope == "project:bb"
    )


def test_disabled_iq_contributes_nothing():
    cfg = plan(mk(iq={"enabled": False, "knowledgeBases": [kb()]}))
    assert cfg.project("finance").knowledge_bases == [] and cfg.scopes["root"].search is None


def test_project_gateway_path_defaults_to_the_project_name():
    cfg = plan(mk(gateway=gateway(), observability={}, projects=[{"name": "fin", "gateway": {}}]))
    assert cfg.project("fin").gateway.path == "/fin"


def test_normalise_does_not_mutate_the_parsed_config():
    parsed = parse_mapping(mk(iq=iq(kb()), models={"allowed": ["gpt-5"]}))
    before = parsed.config.model_dump()
    normalise(parsed.config)
    assert parsed.config.model_dump() == before


# Inheritance engine units ---------------------------------------------------------------------------


def test_merge_named_overrides_in_place_and_tracks_origin():
    a1, a2 = Agent(name="aa"), Agent(name="bb")
    override = Agent(name="aa", model="x")
    merged, origins = merge_named([("root", [a1, a2]), ("project", [override, Agent(name="cc")])])
    assert [a.name for a in merged] == ["aa", "bb", "cc"]
    assert merged[0].model == "x"
    assert origins == {"aa": "project", "bb": "root", "cc": "project"}


def test_merge_models_unit():
    parent = ModelConfiguration(
        default="a1",
        allowed=["a1", "b1"],
        denied=["z1"],
        deployments=[ModelDeployment(name="d1", model="m1")],
    )
    child = ModelConfiguration(
        allowed=[], denied=["y1", "z1"], deployments=[ModelDeployment(name="d2", model="m2")]
    )
    merged = merge_models([("root", parent), ("project", child)])
    assert merged.default == "a1" and merged.allowed == ["a1", "b1"]
    assert merged.denied == ["z1", "y1"]
    assert [d.name for d in merged.deployments] == ["d1", "d2"]
    assert merged.deployments[0] is not parent.deployments[0]


@pytest.mark.parametrize(
    "value, expected", [("11111111-2222-3333-4444-555555555555", True), ("not-a-guid", False)]
)
def test_is_guid(value, expected):
    from xfoundry.normalise.model import is_guid

    assert is_guid(value) is expected


def test_effective_project_lookup():
    cfg = plan(mk())
    with pytest.raises(KeyError):
        cfg.project("missing")
