"""Semantic rules that need inheritance and implicit resources resolved."""

from __future__ import annotations

from xfoundry.diagnostics import Severity

from .conftest import analyse, codes, gateway, hub_doc, iq, kb, mk

MODELS = {"default": "gpt-5", "allowed": ["gpt-5"]}
EMBED = {"name": "emb", "model": "text-embedding-3-large"}


def with_kb(**kb_over):
    return mk(iq=iq(kb(**kb_over)), models={**MODELS})


# Rule 5 / 6 --------------------------------------------------------------------------


def test_rule6_default_model_must_be_known(expect):
    expect(mk(models={"default": "gpt-9"}), "XF006", "models.default")
    assert analyse(
        mk(models={"default": "gpt-5", "deployments": [{"name": "gpt-5", "model": "gpt-5"}]})
    ).ok
    assert analyse(mk(models={"default": "gpt-5", "allowed": ["gpt-5"]})).ok
    assert analyse(
        mk(models={"default": "chat", "deployments": [{"name": "chat", "model": "gpt-5"}]})
    ).ok


def test_rule6_default_cannot_be_denied_or_outside_allowed(expect):
    expect(
        mk(
            models={
                "default": "gpt-5",
                "deployments": [{"name": "gpt-5", "model": "gpt-5"}],
                "denied": ["gpt-5"],
            }
        ),
        "XF006",
        message="denied",
    )
    expect(
        mk(
            models={
                "default": "chat",
                "deployments": [{"name": "chat", "model": "gpt-5"}],
                "denied": ["gpt-5"],
            }
        ),
        "XF006",
        message="denied",
    )
    doc = mk(
        models={
            "default": "mini",
            "allowed": ["gpt-5"],
            "deployments": [{"name": "mini", "model": "gpt-5-mini"}],
        }
    )
    expect(doc, "XF006", message="not in models.allowed")


def test_rule5_agent_references(expect):
    agents = [
        {
            "name": "bot",
            "model": "gpt-5",
            "toolboxes": ["nope"],
            "mcps": ["nope"],
            "knowledgeBases": ["nope"],
        }
    ]
    doc = mk(models=MODELS, projects=[{"name": "fin", "agents": agents}])
    found = [d for d in analyse(doc).diagnostics if d.code == "XF005"]
    assert {
        m
        for d in found
        for m in ("toolbox", "MCP", "knowledge base")
        if f"unknown {m}" in d.message
    } == {"toolbox", "MCP", "knowledge base"}
    expect(
        mk(models=MODELS, agents=[{"name": "bot", "model": "gpt-9"}]),
        "XF005",
        message="unknown model",
    )


def test_prompt_agent_needs_a_model(expect):
    expect(mk(agents=[{"name": "bot"}]), "XF112", message="no model")
    assert analyse(mk(models=MODELS, agents=[{"name": "bot"}])).ok


def test_agent_cannot_use_a_denied_model(expect):
    doc = mk(
        models={"denied": ["gpt-4"], "deployments": [{"name": "old", "model": "gpt-4"}]},
        agents=[{"name": "bot", "model": "old"}],
    )
    expect(doc, "XF016", message="agent 'bot' uses denied model")


def test_shared_agent_reference_must_resolve_for_every_project(expect):
    doc = mk(
        models=MODELS,
        agents=[{"name": "bot", "toolboxes": ["tb"]}],
        projects=[
            {
                "name": "aa",
                "toolboxes": [
                    {"name": "tb", "tools": [{"name": "t1", "type": "function", "reference": "f"}]}
                ],
            },
            {"name": "bb"},
        ],
    )
    found = [d for d in analyse(doc).diagnostics if d.code == "XF005"]
    assert len(found) == 1 and "project 'bb'" in found[0].message


def test_tool_references(expect):
    tools = [
        {"name": "t1", "type": "mcp", "reference": "ghost"},
        {"name": "t2", "type": "knowledgeBase", "reference": "ghost"},
    ]
    doc = mk(toolboxes=[{"name": "tb", "tools": tools}])
    found = [d for d in analyse(doc).diagnostics if d.code == "XF005"]
    assert len(found) == 2


def test_connector_and_route_references(expect):
    doc = mk(
        models=MODELS,
        iq=iq(
            kb(
                sources=[{"name": "sp", "type": "sharepoint", "site": "hr", "connection": "ghost"}],
                routing={
                    "routes": [{"name": "r1", "when": {"agent": "ghost"}, "knowledgeBase": "ghost"}]
                },
            )
        ),
    )
    found = [d.message for d in analyse(doc).diagnostics if d.code == "XF005"]
    assert any("connector 'ghost'" in m for m in found)
    assert any("unknown knowledge base 'ghost'" in m for m in found)
    assert any("unknown agent 'ghost'" in m for m in found)


# Rules 7-12 ----------------------------------------------------------------------------


def test_rule7_dimensions_must_fit_the_embedding_model(expect):
    small = mk(
        models={"deployments": [{"name": "emb", "model": "text-embedding-3-small"}]},
        iq=iq(
            kb(
                index={
                    "vector": {
                        "model": "text-embedding-3-small",
                        "deployment": "emb",
                        "dimensions": 3072,
                    }
                }
            )
        ),
    )
    expect(small, "XF007", message="at most 1536")
    ada = mk(
        models={"deployments": [{"name": "emb", "model": "text-embedding-ada-002"}]},
        iq=iq(
            kb(
                index={
                    "vector": {
                        "model": "text-embedding-ada-002",
                        "deployment": "emb",
                        "dimensions": 1024,
                    }
                }
            )
        ),
    )
    expect(ada, "XF007", message="exactly 1536")
    ok = mk(
        models={"deployments": [{"name": "emb", "model": "text-embedding-3-small"}]},
        iq=iq(
            kb(
                index={
                    "vector": {
                        "model": "text-embedding-3-small",
                        "deployment": "emb",
                        "dimensions": 1024,
                    }
                }
            )
        ),
    )
    assert analyse(ok).ok


def test_rule7_vector_model_must_match_the_deployment(expect):
    doc = mk(
        models={"deployments": [{"name": "emb", "model": "text-embedding-3-small"}]},
        iq=iq(kb(index={"vector": {"deployment": "emb"}})),
    )
    expect(doc, "XF007", message="serves 'text-embedding-3-small'")


def test_rule7_unknown_embedding_model_warns_and_missing_deployment_errors(expect):
    doc = mk(
        models={"deployments": [{"name": "emb", "model": "custom-embed"}]},
        iq=iq(
            kb(index={"vector": {"model": "custom-embed", "deployment": "emb", "dimensions": 100}})
        ),
    )
    assert analyse(doc).ok
    assert codes(doc, Severity.WARNING) == {"XF007"}
    expect(
        mk(iq=iq(kb(index={"vector": {"deployment": "ghost"}}))),
        "XF005",
        message="embedding deployment 'ghost'",
    )


def test_rule7_vector_field_dimensions_must_match(expect):
    fields = [
        {
            "name": "contentVector",
            "type": "Collection(Edm.Single)",
            "dimensions": 1536,
            "vectorProfile": "default-vector-profile",
            "searchable": True,
        }
    ]
    expect(with_kb(index={"fields": fields}), "XF007", message="1536 dimensions")


def test_rule8_filter_fields(expect):
    expect(with_kb(retrieval={"filterFields": ["department"]}), "XF008", message="undefined field")
    fields = [{"name": "department", "type": "Edm.String"}]
    expect(
        with_kb(index={"fields": fields}, retrieval={"filterFields": ["department"]}),
        "XF008",
        message="not filterable",
    )
    fields = [{"name": "department", "type": "Edm.String", "filterable": True}]
    assert analyse(with_kb(index={"fields": fields}, retrieval={"filterFields": ["department"]})).ok
    expect(
        with_kb(access={"filterClaims": {"groups": "groupIds"}}), "XF008", message="filterClaims"
    )


def test_rule9_named_fields_must_exist_with_usable_types(expect):
    expect(with_kb(index={"keyField": "docId"}), "XF009", message="keyField 'docId'")
    expect(with_kb(index={"contentField": "body"}), "XF009", message="contentField 'body'")
    expect(with_kb(index={"titleField": "name"}), "XF009", message="titleField 'name'")
    expect(with_kb(index={"vectorField": "embedding"}), "XF009", message="vectorField 'embedding'")
    wrong = [{"name": "id", "type": "Edm.Int32", "key": True}]
    expect(with_kb(index={"fields": wrong}), "XF009", message="Edm.String with key: true")
    two = [
        {"name": "id", "type": "Edm.String", "key": True},
        {"name": "alt", "type": "Edm.String", "key": True},
    ]
    expect(with_kb(index={"fields": two}), "XF009", message="several key fields")
    content = [{"name": "content", "type": "Edm.Int32"}]
    expect(with_kb(index={"fields": content}), "XF009", message="searchable Edm.String")
    expect(
        with_kb(index={"semantic": {"keywordFields": ["tags"]}}),
        "XF009",
        message="undefined field 'tags'",
    )


def test_rule9_custom_fields_can_be_declared():
    fields = [
        {"name": "docId", "type": "Edm.String", "key": True},
        {"name": "body", "type": "Edm.String", "searchable": True},
        {"name": "name", "type": "Edm.String", "searchable": True},
        {
            "name": "embedding",
            "type": "Collection(Edm.Single)",
            "dimensions": 3072,
            "vectorProfile": "default-vector-profile",
            "searchable": True,
        },
    ]
    index = {
        "keyField": "docId",
        "contentField": "body",
        "titleField": "name",
        "vectorField": "embedding",
        "fields": fields,
    }
    assert analyse(with_kb(index=index)).ok


def test_rule10_vector_fields(expect):
    fields = [{"name": "contentVector", "type": "Edm.String"}]
    expect(with_kb(index={"fields": fields}), "XF010", message="must be Collection(Edm.Single)")
    fields = [
        {
            "name": "contentVector",
            "type": "Collection(Edm.Single)",
            "dimensions": 3072,
            "vectorProfile": "other",
            "searchable": True,
        }
    ]
    expect(with_kb(index={"fields": fields}), "XF010", message="uses profile 'other'")
    assert "XF102" in codes(
        with_kb(index={"fields": [{"name": "v", "type": "Collection(Edm.Single)"}]})
    )


def test_rule10_vector_retrieval_requires_vectors(expect):
    expect(
        with_kb(index={"vector": {"enabled": False}}),
        "XF117",
        message="requires index.vector.enabled",
    )
    keyword = with_kb(index={"vector": {"enabled": False}}, retrieval={"mode": "keyword"})
    assert analyse(keyword).ok
    expect(
        with_kb(
            index={"vector": {"enabled": False}},
            retrieval={"mode": "keyword"},
            routing={"fallback": "vector"},
        ),
        "XF117",
        message="fallback",
    )


def test_rule11_overlap_less_than_size(expect):
    expect(with_kb(index={"chunking": {"size": 256, "overlap": 256}}), "XF011")
    assert analyse(with_kb(index={"chunking": {"size": 256, "overlap": 255}})).ok


def test_rule12_hybrid_weights_sum_to_one(expect):
    expect(with_kb(retrieval={"vectorWeight": 0.8, "keywordWeight": 0.3}), "XF012")
    assert analyse(with_kb(retrieval={"vectorWeight": 0.6, "keywordWeight": 0.4})).ok
    assert analyse(
        with_kb(retrieval={"vectorWeight": 0.1, "keywordWeight": 0.2, "mode": "vector"})
    ).ok


def test_semantic_ranking_requirements(expect):
    expect(
        with_kb(index={"semantic": {"enabled": False}}), "XF117", message="index.semantic.enabled"
    )
    doc = mk(models=MODELS, search={"semanticRanking": False}, iq=iq(kb()))
    expect(doc, "XF117", message="Search service")
    ok = mk(
        models=MODELS,
        search={"semanticRanking": False},
        iq=iq(kb(retrieval={"semanticRanking": False})),
    )
    assert analyse(ok).ok


def test_explicit_routing_needs_routes(expect):
    expect(with_kb(routing={"strategy": "explicit"}), "XF118", message="at least one route")


def test_source_container_must_be_lowercase(expect):
    expect(with_kb(sources=[{"name": "files", "type": "blob", "container": "Policies"}]), "XF024")


# Rule 20 --------------------------------------------------------------------------------


def test_rule20_iq_with_search_disabled(expect):
    expect(mk(models=MODELS, search={"enabled": False}, iq=iq(kb())), "XF020")
    expect(
        mk(models=MODELS, projects=[{"name": "fin", "search": {"enabled": False}, "iq": iq(kb())}]),
        "XF020",
    )


def test_rule20_search_opt_out_is_fine_without_iq():
    assert analyse(mk(search={"enabled": False})).ok


# Rule 16 / policy --------------------------------------------------------------------------


def test_rule16_deployments_respect_allowed_and_denied(expect):
    doc = mk(
        models={"allowed": ["gpt-5"], "deployments": [{"name": "mini", "model": "gpt-5-mini"}]}
    )
    expect(doc, "XF016", message="not in models.allowed")
    doc = mk(models={"denied": ["gpt-4"], "deployments": [{"name": "old", "model": "gpt-4"}]})
    expect(doc, "XF016", message="denied model")
    embedding = mk(models={"allowed": ["gpt-5"]}, iq=iq(kb()))
    assert analyse(
        embedding
    ).ok  # implicit embedding deployments are exempt from the chat allow-list


def test_denied_models_accumulate_across_hub_and_project(expect):
    doc = hub_doc(
        projects=[{"name": "fin", "models": {"deployments": [{"name": "old", "model": "gpt-4"}]}}]
    )
    doc["x-foundry"]["hub"]["models"] = {"denied": ["gpt-4"]}
    expect(doc, "XF016", "projects[fin]", "denied model")


def test_rule113_model_policy(expect):
    policy = {
        "allowedModels": ["gpt-5"],
        "deniedModels": ["gpt-4"],
        "allowedSkus": ["GlobalStandard"],
    }
    expect(
        mk(
            governance={"modelPolicy": policy},
            models={"deployments": [{"name": "mm", "model": "gpt-5-mini"}]},
        ),
        "XF113",
        message="allowedModels",
    )
    expect(
        mk(
            governance={"modelPolicy": {"deniedModels": ["gpt-4"]}},
            models={"deployments": [{"name": "mm", "model": "gpt-4"}]},
        ),
        "XF113",
        message="denies",
    )
    expect(
        mk(
            governance={"modelPolicy": policy},
            models={"deployments": [{"name": "mm", "model": "gpt-5", "sku": "Standard"}]},
        ),
        "XF113",
        message="allowedSkus",
    )
    expect(
        mk(
            governance={"modelPolicy": {"allowedModels": ["gpt-5"]}},
            models={"allowed": ["gpt-5"]},
            gateway=gateway(models={"allowed": ["gpt-5", "gpt-5-mini"]}),
            observability={},
        ),
        "XF113",
        message="gateway allows",
    )
    ok = mk(
        governance={"modelPolicy": policy},
        models={"deployments": [{"name": "mm", "model": "gpt-5"}]},
    )
    assert analyse(ok).ok


def test_rule115_spokes_may_narrow_but_not_widen(expect):
    hub_models = {"allowed": ["gpt-5", "gpt-5-mini"]}
    wide = hub_doc(projects=[{"name": "fin", "models": {"allowed": ["gpt-5", "gpt-4"]}}])
    wide["x-foundry"]["hub"]["models"] = hub_models
    expect(wide, "XF115", "projects[fin].models.allowed", "gpt-4")
    narrow = hub_doc(projects=[{"name": "fin", "models": {"allowed": ["gpt-5"]}}])
    narrow["x-foundry"]["hub"]["models"] = hub_models
    assert analyse(narrow).ok
    root_wide = mk(
        models={"allowed": ["gpt-5"]}, projects=[{"name": "fin", "models": {"allowed": ["gpt-4"]}}]
    )
    expect(root_wide, "XF115")


def test_rule104_required_tags(expect):
    doc = mk(governance={"requiredTags": ["environment", "project", "managed-by", "cost-center"]})
    expect(doc, "XF104", "projects[finance].tags", "cost-center")
    ok = mk(defaults={"tags": {"cost-center": "1"}}, governance={"requiredTags": ["cost-center"]})
    assert analyse(ok).ok
    disabled = mk(governance={"enabled": False, "requiredTags": ["cost-center"]})
    assert analyse(disabled).ok


# Storage / adls --------------------------------------------------------------------------------


def test_storage_disabled_but_required(expect):
    expect(
        mk(models=MODELS, storage={"enabled": False}, iq=iq(kb())),
        "XF107",
        message="storage is disabled",
    )


def test_adls_sources_enable_hierarchical_namespace(expect):
    doc = mk(
        models=MODELS, iq=iq(kb(sources=[{"name": "lake", "type": "adls", "container": "lake"}]))
    )
    plan = analyse(doc).plan
    assert plan.config.storage.hierarchical_namespace is True
    assert {pe.group for pe in plan.config.network.private_endpoints} >= {"blob", "dfs"}
    explicit = mk(
        models=MODELS,
        storage={"hierarchicalNamespace": False},
        iq=iq(kb(sources=[{"name": "lake", "type": "adls", "container": "lake"}])),
    )
    expect(explicit, "XF107", message="hierarchicalNamespace")


# Gateway ----------------------------------------------------------------------------------------


def gw_doc(**gw):
    return mk(
        models={"default": "gpt-5", "allowed": ["gpt-5", "gpt-5-mini"]},
        projects=[{"name": "fin", "agents": [{"name": "bot", "instructions": "x"}]}],
        observability={},
        gateway=gateway(**gw),
    )


def endpoint(**over):
    base = {"name": "ep1", "path": "/a", "target": "bot", "targetType": "agent"}
    base.update(over)
    return base


def test_valid_gateway():
    assert analyse(gw_doc(endpoints=[endpoint()])).ok


def test_rule13_paths_are_unique(expect):
    doc = gw_doc(endpoints=[endpoint(), endpoint(name="ep2")])
    expect(doc, "XF013", message="used more than once")
    doc = mk(
        models=MODELS,
        observability={},
        gateway=gateway(endpoints=[endpoint(target="gpt-5", targetType="model", path="/fin")]),
        projects=[{"name": "fin", "gateway": {"path": "/fin"}}],
    )
    expect(doc, "XF013", "gateway.path", "already used")
    two = mk(
        observability={},
        gateway=gateway(),
        projects=[
            {"name": "aa", "gateway": {"path": "/x"}},
            {"name": "bb", "gateway": {"path": "/x"}},
        ],
    )
    expect(two, "XF013", "gateway.path")


def test_rule14_targets_resolve(expect):
    expect(
        gw_doc(endpoints=[endpoint(target="ghost")]),
        "XF014",
        message="agent 'ghost' does not exist",
    )
    expect(
        gw_doc(endpoints=[endpoint(project="fin", target="ghost")]),
        "XF014",
        message="in project 'fin'",
    )
    expect(
        gw_doc(endpoints=[endpoint(target="ghost", targetType="runtime")]),
        "XF014",
        message="runtime 'ghost'",
    )
    expect(
        gw_doc(endpoints=[endpoint(target="ghost", targetType="model")]),
        "XF014",
        message="model 'ghost'",
    )
    expect(
        gw_doc(endpoints=[endpoint(target="ghost", targetType="search")]),
        "XF014",
        message="search 'ghost'",
    )
    expect(
        gw_doc(endpoints=[endpoint(target="ghost", targetType="knowledgeBase")]),
        "XF014",
        message="knowledge base 'ghost'",
    )


def test_rule14_valid_targets_of_every_type():
    doc = mk(
        models={"default": "gpt-5", "allowed": ["gpt-5"]},
        observability={},
        search={"name": "srch-main"},
        runtime={"enabled": True, "image": "i", "name": "rt-main", "project": "fin"},
        iq=iq(kb()),
        projects=[{"name": "fin", "agents": [{"name": "bot", "instructions": "x"}]}],
        gateway=gateway(
            endpoints=[
                endpoint(),
                endpoint(name="ep2", path="/m", target="gpt-5", targetType="model"),
                endpoint(name="ep3", path="/s", target="srch-main", targetType="search"),
                endpoint(name="ep4", path="/r", target="rt-main", targetType="runtime"),
                endpoint(
                    name="ep5",
                    path="/k",
                    target="policies",
                    targetType="knowledgeBase",
                    project="fin",
                ),
            ]
        ),
    )
    assert analyse(doc).ok, [str(d) for d in analyse(doc).diagnostics]


def test_rule14_ambiguous_agent_target(expect):
    doc = mk(
        models=MODELS,
        observability={},
        projects=[
            {"name": "aa", "agents": [{"name": "bot"}]},
            {"name": "bb", "agents": [{"name": "bot"}]},
        ],
        gateway=gateway(endpoints=[endpoint()]),
    )
    expect(doc, "XF014", message="several projects")
    shared = mk(
        models=MODELS,
        observability={},
        agents=[{"name": "bot"}],
        projects=[{"name": "aa"}, {"name": "bb"}],
        gateway=gateway(endpoints=[endpoint()]),
    )
    assert analyse(shared).ok


def test_rule15_profile_references(expect):
    doc = gw_doc(endpoints=[endpoint(quotaProfile="qq", limitProfile="ll", routingProfile="rr")])
    found = [d.message for d in analyse(doc).diagnostics if d.code == "XF015"]
    assert len(found) == 3
    ok = gw_doc(
        endpoints=[endpoint(quotaProfile="qq", limitProfile="ll", routingProfile="b1")],
        quotas={"profiles": [{"name": "qq", "scope": "user", "dailyTokens": 1000}]},
        limits={"profiles": [{"name": "ll"}]},
        routing={"backends": [{"name": "b1", "target": "x"}]},
    )
    assert analyse(ok).ok
    reg = mk(
        models=MODELS,
        observability={},
        gateway=gateway(),
        projects=[{"name": "fin", "gateway": {"quotaProfile": "ghost"}}],
    )
    expect(reg, "XF015", "gateway.quotaProfile")


def test_project_registration_requires_an_enabled_gateway(expect):
    expect(mk(projects=[{"name": "fin", "gateway": {}}]), "XF111", message="gateway.enabled")
    disabled = mk(gateway={"enabled": False, "endpoints": [endpoint()]})
    assert analyse(disabled).ok
    assert codes(disabled, Severity.WARNING) == {"XF111"}


def test_gateway_tracking_chargeback_and_telemetry(expect):
    expect(gw_doc(tokenTracking={"enabled": False}), "XF111", message="chargeback requires")
    expect(
        gw_doc(
            tokenTracking={"enabled": False},
            chargeback={"enabled": False},
            endpoints=[endpoint(tokenTracking=True)],
        ),
        "XF111",
        message="endpoint enables",
    )
    off = gw_doc(tokenTracking={"enabled": False}, chargeback={"enabled": False})
    assert analyse(off).ok
    no_obs = gw_doc()
    del no_obs["x-foundry"]["observability"]
    assert analyse(no_obs).ok  # implicit observability is added
    disabled = gw_doc()
    disabled["x-foundry"]["observability"] = {"enabled": False}
    expect(disabled, "XF111", message="logAnalytics")


def test_gateway_models(expect):
    expect(
        gw_doc(models={"default": "gpt-5", "allowed": ["gpt-5-mini"]}),
        "XF006",
        message="not in gateway.models.allowed",
    )
    expect(gw_doc(models={"default": "gpt-5", "denied": ["gpt-5"]}), "XF006", message="denied")
    expect(gw_doc(models={"allowed": ["gpt-9"]}), "XF005", message="no deployment serves it")


def test_gateway_private_mode_requires_internal_only(expect):
    expect(gw_doc(security={"internalOnly": False}), "XF021", message="internalOnly")


def test_gateway_defaults_are_derived():
    plan = analyse(mk(models=MODELS, gateway={"enabled": True}, projects=[{"name": "fin"}])).plan
    gw = plan.config.gateway
    assert gw.authentication.audiences == ["api://x-foundry-gateway"]
    assert gw.models.default == "gpt-5" and gw.models.allowed == ["gpt-5"]
    assert {i.kind for i in plan.config.implicit} >= {"gateway-authentication", "observability"}
    prefixed = analyse(mk(defaults={"namingPrefix": "ent"}, gateway={"enabled": True})).plan
    assert prefixed.config.gateway.authentication.audiences == ["api://ent-gateway"]


def test_gateway_chargeback_dimensions_are_tracked_and_department_has_a_claim():
    plan = analyse(gw_doc(chargeback={"dimensions": ["project", "department"]})).plan
    tracking = plan.config.gateway.token_tracking
    assert "department" in tracking.dimensions
    assert tracking.department_claim == "department"
    custom = analyse(
        gw_doc(tokenTracking={"departmentClaim": "dept"}, chargeback={"dimensions": ["department"]})
    ).plan
    assert custom.config.gateway.token_tracking.department_claim == "dept"


def test_gateway_explicit_models_are_kept():
    plan = analyse(gw_doc(models={"default": "gpt-5", "allowed": ["gpt-5"]})).plan
    assert plan.config.gateway.models.allowed == ["gpt-5"]


def test_xf126_data_residency_rules_out_global_deployments(expect):
    residency = {"dataResidency": ["westeurope"]}
    doc = mk(
        defaults={"location": "westeurope"},
        governance=residency,
        models={"deployments": [{"name": "chat", "model": "gpt-5", "sku": "GlobalStandard"}]},
    )
    expect(doc, "XF126", "deployments[chat].sku")
    ok = mk(
        defaults={"location": "westeurope"},
        governance=residency,
        models={"deployments": [{"name": "chat", "model": "gpt-5", "sku": "DataZoneStandard"}]},
    )
    assert analyse(ok).ok


def test_implicit_deployments_follow_data_residency():
    doc = mk(
        defaults={"location": "westeurope"},
        governance={"dataResidency": ["westeurope"]},
        models={"allowed": ["gpt-5"]},
        iq=iq(kb()),
    )
    plan = analyse(doc).plan
    skus = {d.name: d.sku for d in plan.config.scopes["root"].models.deployments}
    assert skus == {"gpt-5": "DataZoneStandard", "text-embedding-3-large": "DataZoneStandard"}
    plain = analyse(mk(models={"allowed": ["gpt-5"]})).plan
    assert plain.config.scopes["root"].models.deployments[0].sku == "GlobalStandard"
