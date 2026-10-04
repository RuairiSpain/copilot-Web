from __future__ import annotations

import pytest

from xfoundry.graph import STAGES, CycleError, DeploymentGraph, Node, build_graph
from xfoundry.plan import analyse_file

from .conftest import EXAMPLES, analyse, gateway, iq, kb, mk


def node(graph, node_id, kind="resource-group", stage=0):
    return graph.add_node(Node(node_id, kind, stage))


def test_topological_order_puts_dependencies_first_and_ties_by_stage_then_id():
    g = DeploymentGraph()
    for node_id, stage in (("c", 2), ("a", 1), ("b", 1), ("d", 0)):
        node(g, node_id, stage=stage)
    g.add_dependency("c", "a")
    g.add_dependency("a", "d")
    g.add_dependency("b", "d")
    assert g.topological_order() == ["d", "a", "b", "c"]
    assert g.layers() == [["d"], ["a", "b"], ["c"]]
    assert g.dependencies_of("c") == ["a"] and g.dependents_of("d") == ["a", "b"]


def test_add_node_is_idempotent():
    g = DeploymentGraph()
    first = node(g, "a")
    assert g.add_node(Node("a", "other", 9)) is first
    assert g.has("a") and not g.has("b")


def test_cycles_are_detected():
    g = DeploymentGraph()
    for node_id in "abc":
        node(g, node_id)
    g.add_dependency("a", "b")
    g.add_dependency("b", "c")
    g.add_dependency("c", "a")
    with pytest.raises(CycleError) as info:
        g.topological_order()
    assert info.value.nodes == ["a", "b", "c"]
    assert "a, b, c" in str(info.value)


def test_self_dependency_and_unknown_nodes_are_rejected():
    g = DeploymentGraph()
    node(g, "a")
    with pytest.raises(CycleError):
        g.add_dependency("a", "a")
    with pytest.raises(KeyError):
        g.add_dependency("a", "ghost")
    with pytest.raises(KeyError):
        g.add_dependency("ghost", "a")


def test_empty_graph():
    g = DeploymentGraph()
    assert g.topological_order() == [] and g.layers() == []


def deps(plan, node_id):
    return set(plan.node(node_id).depends_on)


def test_every_node_kind_has_a_stage():
    plan = analyse_file(EXAMPLES / "enterprise.yaml").plan
    assert {n.kind for n in plan.nodes} <= set(STAGES)


def test_stage_order_follows_the_specification_for_foundations():
    plan = analyse_file(EXAMPLES / "enterprise.yaml").plan
    order = plan.order
    for before, after in (
        ("resource-group", "identity"),
        ("identity", "network"),
        ("network", "storage"),
        ("storage", "redis"),
        ("redis", "search:hub"),
        ("search:hub", "foundry"),
        ("foundry", "foundry-project:hub"),
        ("foundry-project:hub", "model-deployment:hub:gpt-5"),
        ("gateway", "alerts"),
        ("alerts", "governance"),
    ):
        assert order.index(before) < order.index(after), (before, after)


def test_dependencies_of_a_knowledge_base_and_agent():
    plan = analyse(
        mk(
            models={"default": "gpt-5", "allowed": ["gpt-5"]},
            connectors=[
                {
                    "name": "sp",
                    "type": "sharepoint",
                    "authentication": {"mode": "apiKey", "secretRef": "sp-key"},
                }
            ],
            iq=iq(
                kb(
                    sources=[
                        {"name": "site", "type": "sharepoint", "site": "hr", "connection": "sp"},
                        {"name": "files", "type": "blob", "container": "policies"},
                    ]
                )
            ),
            projects=[{"name": "fin", "agents": [{"name": "bot", "knowledgeBases": ["policies"]}]}],
        )
    ).plan
    kb_node = "knowledge-base:project:fin:policies"
    assert deps(plan, kb_node) == {
        "foundry-project:project:fin",
        "search:root",
        "storage",
        "connector:project:fin:sp",
        "model-deployment:root:text-embedding-3-large",
    }
    assert deps(plan, "connector:project:fin:sp") == {"foundry-project:project:fin", "key-vault"}
    agent = deps(plan, "agent:project:fin:bot")
    assert {kb_node, "model-deployment:root:gpt-5", "foundry-project:project:fin"} == agent


def test_hub_items_are_shared_and_root_items_are_instantiated_per_project():
    doc = mk(
        topology={"mode": "hub-spoke"},
        agents=[{"name": "shared"}],
        models={"default": "gpt-5", "allowed": ["gpt-5"]},
        projects=[{"name": "aa"}, {"name": "bb"}],
        hub={
            "name": "hub1",
            "mcps": [{"name": "graph", "endpoint": "https://a.example"}],
            "toolboxes": [
                {"name": "tb", "tools": [{"name": "t1", "type": "mcp", "reference": "graph"}]}
            ],
        },
    )
    plan = analyse(doc).plan
    ids = set(plan.order)
    assert {
        "agent:project:aa:shared",
        "agent:project:bb:shared",
        "mcp:hub:graph",
        "toolbox:hub:tb",
    } <= ids
    assert "mcp:project:aa:graph" not in ids
    assert deps(plan, "toolbox:hub:tb") == {"foundry-project:hub", "mcp:hub:graph"}


def test_runtime_and_agent_runtime_dependencies():
    agents = [
        {
            "name": "bot",
            "kind": "hosted",
            "runtime": {"enabled": True, "source": "./bot", "secrets": {"db": "db-pass"}},
        }
    ]
    plan = analyse(
        mk(
            models={"default": "gpt-5", "allowed": ["gpt-5"]},
            redis={"enabled": True},
            projects=[
                {"name": "fin", "agents": agents, "runtime": {"enabled": True, "image": "i"}}
            ],
        )
    ).plan
    runtime = deps(plan, "runtime:project:fin")
    assert {
        "resource-group",
        "identity",
        "observability",
        "foundry",
        "foundry-project:project:fin",
        "redis",
    } <= runtime
    agent_runtime = deps(plan, "runtime:agent:project:fin:bot")
    assert {"registry", "key-vault"} <= agent_runtime
    assert "runtime:agent:project:fin:bot" in deps(plan, "agent:project:fin:bot")


def test_gateway_depends_on_every_endpoint_target():
    doc = mk(
        models={"default": "gpt-5", "allowed": ["gpt-5"]},
        observability={},
        redis={"enabled": True},
        search={"name": "srch-main"},
        iq=iq(kb()),
        runtime={"enabled": True, "image": "i", "name": "rt-main"},
        projects=[{"name": "fin", "agents": [{"name": "bot"}]}],
        gateway=gateway(
            caching={"enabled": True},
            endpoints=[
                {"name": "ep1", "path": "/a", "target": "bot", "targetType": "agent"},
                {"name": "ep2", "path": "/m", "target": "gpt-5", "targetType": "model"},
                {"name": "ep3", "path": "/s", "target": "srch-main", "targetType": "search"},
                {"name": "ep4", "path": "/r", "target": "rt-main", "targetType": "runtime"},
                {"name": "ep5", "path": "/k", "target": "policies", "targetType": "knowledgeBase"},
            ],
        ),
    )
    plan = analyse(doc).plan
    assert {
        "agent:project:fin:bot",
        "model-deployment:root:gpt-5",
        "search:root",
        "runtime:root",
        "knowledge-base:project:fin:policies",
        "redis",
        "observability",
    } <= deps(plan, "gateway")


def test_alerts_and_governance():
    doc = mk(observability={"alerts": True}, search={}, governance={}, gateway=gateway())
    plan = analyse(doc).plan
    assert {"observability", "search:root", "gateway", "foundry"} <= deps(plan, "alerts")
    assert {"resource-group", "gateway", "alerts"} <= deps(plan, "governance")
    assert plan.order[-1] == "governance"
    quiet = analyse(mk(observability={"alerts": False})).plan
    assert "alerts" not in quiet.order and "governance" not in quiet.order


def test_existing_resources_are_marked_and_not_scheduled_before_their_dependencies():
    arm = "/subscriptions/s/resourceGroups/rg/providers"
    doc = mk(
        storage={"existingResourceId": f"{arm}/Microsoft.Storage/storageAccounts/st1"},
        search={"existingResourceId": f"{arm}/Microsoft.Search/searchServices/s1"},
        managedIdentity={
            "existingResourceId": f"{arm}/Microsoft.ManagedIdentity/userAssignedIdentities/id1"
        },
    )
    plan = analyse(doc).plan
    assert (
        plan.node("storage").existing
        and plan.node("search:root").existing
        and plan.node("identity").existing
    )
    assert not plan.node("foundry").existing


def test_existing_vnet_has_no_network_node_and_disabled_identity_is_skipped():
    vnet = "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v1"
    security = {
        "roles": {"admins": ["a"]},
        "network": {"mode": "private", "existingVnetResourceId": vnet},
    }
    plan = analyse(mk(security=security)).plan
    assert "network" not in plan.order and deps(plan, "private-dns") == {"resource-group"}
    public = {"roles": {"admins": ["a"]}, "network": {"mode": "public"}}
    plan = analyse(mk(security=public, managedIdentity={"enabled": False})).plan
    assert "identity" not in plan.order


def test_evaluation_depends_on_storage_and_agents():
    doc = mk(
        models={"default": "gpt-5", "allowed": ["gpt-5"]},
        projects=[
            {
                "name": "fin",
                "agents": [{"name": "bot"}],
                "evaluation": {
                    "enabled": True,
                    "datasets": [{"name": "golden", "path": "g.jsonl"}],
                },
            }
        ],
    )
    plan = analyse(doc).plan
    assert deps(plan, "evaluation:project:fin:evaluation") == {
        "foundry-project:project:fin",
        "storage",
        "agent:project:fin:bot",
    }


def test_build_graph_matches_plan_nodes():
    plan = analyse_file(EXAMPLES / "enterprise.yaml").plan
    graph = build_graph(plan.config)
    assert graph.topological_order() == plan.order
    assert graph.layers() == plan.layers
