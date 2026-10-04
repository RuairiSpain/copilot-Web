"""Build the deployment graph from a normalised configuration.

Stage numbers follow the deployment order in the specification (resource group,
identity, networking, storage, Redis, Search, Foundry resource, projects, models, ...).
They break ties only; dependencies always win. Two deliberate deviations:

* MCPs, connectors and knowledge bases are ordered before toolboxes and agents because
  toolboxes and agents reference them;
* the Log Analytics / Application Insights workspace is created early (stage 10) because
  runtimes and the gateway log to it. Alerts are the late "Monitoring" step.
"""

from __future__ import annotations

from xfoundry import ids
from xfoundry.graph.graph import DeploymentGraph, Node
from xfoundry.normalise.model import EffectiveProject, NormalisedConfig
from xfoundry.schema.models import Runtime

STAGES: dict[str, int] = {
    "resource-group": 0,
    "identity": 10,
    "observability": 10,
    "network": 20,
    "private-dns": 22,
    "storage": 30,
    "key-vault": 32,
    "events": 35,
    "redis": 40,
    "registry": 45,
    "search": 50,
    "private-endpoint": 60,
    "foundry-account": 70,
    "foundry-project": 80,
    "runtime": 85,
    "model-deployment": 90,
    "connector": 95,
    "mcp": 100,
    "knowledge-base": 110,
    "toolbox": 120,
    "agent": 130,
    "evaluation": 150,
    "gateway": 160,
    "alerts": 170,
    "governance": 180,
}
# Items declared at root scope are instantiated in every project; hub items exist once.
_INSTANCED_KINDS = {"agent", "toolbox", "mcp", "connector", "knowledge-base"}


class _Builder:
    def __init__(self, norm: NormalisedConfig):
        self.norm = norm
        self.graph = DeploymentGraph()
        self.runtime_names: dict[str, str] = {}

    # ------------------------------------------------------------------ helpers

    def node(
        self, node_id: str, kind: str, scope: str | None = None, existing: bool = False
    ) -> str:
        self.graph.add_node(Node(node_id, kind, STAGES[kind], scope, existing))
        return node_id

    def link(self, node_id: str, *deps: str | None) -> None:
        for dep in deps:
            if dep is not None and self.graph.has(dep):
                self.graph.add_dependency(node_id, dep)

    def host(self, project: EffectiveProject, origin: str | None) -> str:
        return ids.HUB_SCOPE if origin == ids.HUB_SCOPE else ids.project_scope(project.name)

    def deployment_node(self, project_scope: str, inherits_hub: bool, name: str) -> str | None:
        """Node of the deployment ``name`` as seen from a scope (project, then hub, then root)."""
        chain = [project_scope]
        if inherits_hub and ids.HUB_SCOPE in self.norm.scopes:
            chain.append(ids.HUB_SCOPE)
        chain.append(ids.ROOT_SCOPE)
        for scope in chain:
            for d in self.norm.scopes[scope].models.deployments:
                if name in {d.name, d.model}:
                    return ids.item_node("model-deployment", scope, d.name)
        return None

    # -------------------------------------------------------------- foundations

    def foundations(self) -> None:
        n = self.norm
        rg = self.node(ids.RESOURCE_GROUP, "resource-group")
        identity = None
        if n.managed_identity and n.managed_identity.enabled:
            identity = self.node(
                ids.IDENTITY, "identity", existing=bool(n.managed_identity.existing_resource_id)
            )
            self.link(identity, rg)
        workspace = None
        if n.observability and n.observability.enabled:
            workspace = self.node(ids.WORKSPACE, "observability")
            self.link(workspace, rg)
        network = dns = None
        if n.network.vnet == "create":
            network = self.node(ids.NETWORK, "network")
            self.link(network, rg)
        if n.network.private_dns:
            dns = self.node(ids.PRIVATE_DNS, "private-dns")
            self.link(dns, rg, network)
        self.rg, self.identity, self.workspace, self.network, self.dns = (
            rg,
            identity,
            workspace,
            network,
            dns,
        )

        components = (
            (ids.STORAGE, "storage", n.storage, bool(n.storage and n.storage.enabled)),
            (ids.KEY_VAULT, "key-vault", n.key_vault, bool(n.key_vault and n.key_vault.enabled)),
            (ids.EVENTS, "events", n.events, bool(n.events and n.events.enabled)),
            (ids.REDIS, "redis", n.redis, bool(n.redis and n.redis.enabled)),
        )
        for node_id, kind, config, enabled in components:
            if enabled:
                existing = bool(getattr(config, "existing_resource_id", None))
                self.node(node_id, kind, existing=existing)
                self.link(node_id, rg, identity if kind in {"storage", "key-vault"} else None)
        if any(i.kind == "container-registry" for i in n.implicit):
            self.node(ids.REGISTRY, "registry")
            self.link(ids.REGISTRY, rg, identity)
        for scope_id, scope in n.scopes.items():
            if scope.search is not None and scope.search.enabled:
                node_id = self.node(
                    ids.search_node(scope_id),
                    "search",
                    scope_id,
                    existing=bool(scope.search.existing_resource_id),
                )
                self.link(node_id, rg, identity)
        foundry = self.node(ids.FOUNDRY, "foundry-account")
        self.link(foundry, rg, identity, workspace, network)
        for pe in n.network.private_endpoints:
            node_id = self.node(
                ids.private_endpoint_node(pe.component, pe.group), "private-endpoint"
            )
            self.link(node_id, rg, network, dns, pe.component)
        self.foundry = foundry

    # ------------------------------------------------------------------ foundry

    def projects(self) -> None:
        n = self.norm
        if n.hub is not None:
            hub_scope = ids.HUB_SCOPE
            self.node(ids.project_node(hub_scope), "foundry-project", hub_scope)
            self.link(ids.project_node(hub_scope), self.foundry)
        for p in n.projects:
            scope = ids.project_scope(p.name)
            self.node(ids.project_node(scope), "foundry-project", scope)
            self.link(ids.project_node(scope), self.foundry)
        for scope_id, scope in n.scopes.items():
            for d in scope.models.deployments:
                node_id = self.node(
                    ids.item_node("model-deployment", scope_id, d.name),
                    "model-deployment",
                    scope_id,
                )
                self.link(
                    node_id,
                    self.foundry,
                    ids.project_node(scope_id) if scope_id != ids.ROOT_SCOPE else None,
                )
        for p in n.projects:
            self.project_items(p)

    def project_items(self, p: EffectiveProject) -> None:
        pscope = ids.project_scope(p.name)
        pnode = ids.project_node(pscope)
        secrets = ids.KEY_VAULT

        def origin(kind: str, name: str) -> str | None:
            return p.origins.get(f"{kind}:{name}")

        def target(kind: str, label: str, name: str) -> str:
            return ids.item_node(label, self.host(p, origin(kind, name)), name)

        for c in p.connectors:
            node_id = self.node(
                target("connector", "connector", c.name),
                "connector",
                self.host(p, origin("connector", c.name)),
            )
            self.link(
                node_id,
                ids.project_node(self.host(p, origin("connector", c.name))),
                secrets if c.authentication and c.authentication.secret_ref else None,
            )
        for m in p.mcps:
            host = self.host(p, origin("mcp", m.name))
            node_id = self.node(target("mcp", "mcp", m.name), "mcp", host)
            self.link(
                node_id,
                ids.project_node(host),
                secrets if m.authentication and m.authentication.secret_ref else None,
            )
        for kb in p.knowledge_bases:
            host = self.host(p, origin("knowledgeBase", kb.name))
            node_id = self.node(
                target("knowledgeBase", "knowledge-base", kb.name), "knowledge-base", host
            )
            search_scope = self.search_scope_for(p, host)
            self.link(
                node_id,
                ids.project_node(host),
                ids.search_node(search_scope) if search_scope else None,
            )
            if kb.index.vector.enabled and kb.index.vector.deployment:
                self.link(
                    node_id, self.deployment_node(host, p.inherits_hub, kb.index.vector.deployment)
                )
            if any(s.type in {"blob", "adls"} and not s.connection for s in kb.sources):
                self.link(node_id, ids.STORAGE)
            for s in kb.sources:
                if s.connection:
                    conn_origin = origin("connector", s.connection)
                    self.link(
                        node_id, ids.item_node("connector", self.host(p, conn_origin), s.connection)
                    )
        for t in p.toolboxes:
            host = self.host(p, origin("toolbox", t.name))
            node_id = self.node(target("toolbox", "toolbox", t.name), "toolbox", host)
            self.link(node_id, ids.project_node(host))
            for tool in t.tools:
                if tool.type == "mcp" and tool.reference in {m.name for m in p.mcps}:
                    self.link(node_id, target("mcp", "mcp", tool.reference))
                elif tool.type == "knowledgeBase" and tool.reference in {
                    k.name for k in p.knowledge_bases
                }:
                    self.link(node_id, target("knowledgeBase", "knowledge-base", tool.reference))
        if p.runtime is not None and p.runtime.enabled:
            rt_scope = p.runtime_scope or pscope
            default = "runtime" if rt_scope == ids.ROOT_SCOPE else f"{p.name}-runtime"
            self.runtime_node(
                rt_scope,
                p.runtime,
                pnode if rt_scope != ids.ROOT_SCOPE else None,
                p.runtime.name or default,
            )
        for a in p.agents:
            host = self.host(p, origin("agent", a.name))
            node_id = self.node(target("agent", "agent", a.name), "agent", host)
            self.link(node_id, ids.project_node(host))
            model = a.model or p.models.default
            if model:
                self.link(node_id, self.deployment_node(host, p.inherits_hub, model))
            for name in a.toolboxes:
                self.link(node_id, target("toolbox", "toolbox", name))
            for name in a.mcps:
                self.link(node_id, target("mcp", "mcp", name))
            for name in a.knowledge_bases:
                self.link(node_id, target("knowledgeBase", "knowledge-base", name))
            if a.runtime is not None and a.runtime.enabled:
                rt = self.runtime_node(
                    f"agent:{host}:{a.name}",
                    a.runtime,
                    ids.project_node(host),
                    a.runtime.name or a.name,
                )
                self.link(node_id, rt)
        ev = p.evaluation
        if ev is not None and ev.enabled:
            node_id = self.node(
                ids.item_node("evaluation", pscope, "evaluation"), "evaluation", pscope
            )
            self.link(node_id, pnode, ids.STORAGE if ev.datasets else None)
            for a in p.agents:
                self.link(node_id, target("agent", "agent", a.name))

    def runtime_node(
        self, key: str, runtime: Runtime, project_node: str | None, logical_name: str
    ) -> str:
        node_id = self.node(f"runtime:{key}", "runtime", key)
        self.runtime_names[node_id] = logical_name
        self.link(
            node_id,
            self.rg,
            self.identity if runtime.managed_identity else None,
            self.workspace,
            self.network,
            self.foundry,
            project_node,
        )
        if runtime.source and runtime.registry.mode == "managed":
            self.link(node_id, ids.REGISTRY)
        if runtime.secrets:
            self.link(node_id, ids.KEY_VAULT)
        if self.graph.has(ids.REDIS):
            self.link(node_id, ids.REDIS)
        return node_id

    def search_scope_for(self, p: EffectiveProject, host: str) -> str | None:
        if host == ids.HUB_SCOPE and self.norm.hub is not None:
            return self.norm.hub.search_scope
        return p.search_scope

    # ------------------------------------------------------------- cross-cutting

    def gateway(self) -> None:
        n = self.norm
        gateway = n.gateway
        if gateway is None or not gateway.enabled:
            return
        node_id = self.node(ids.GATEWAY, "gateway")
        self.link(node_id, self.rg, self.identity, self.workspace, self.network)
        if gateway.caching.enabled:
            self.link(node_id, ids.REDIS)
        for e in gateway.endpoints:
            if e.target_type == "agent":
                for p in n.projects:
                    if e.project in (None, p.name) and any(a.name == e.target for a in p.agents):
                        host = self.host(p, p.origins.get(f"agent:{e.target}"))
                        self.link(node_id, ids.item_node("agent", host, e.target))
            elif e.target_type == "knowledgeBase":
                for p in n.projects:
                    if e.project in (None, p.name) and any(
                        k.name == e.target for k in p.knowledge_bases
                    ):
                        host = self.host(p, p.origins.get(f"knowledgeBase:{e.target}"))
                        self.link(node_id, ids.item_node("knowledge-base", host, e.target))
            elif e.target_type == "model":
                for scope_id, scope in n.scopes.items():
                    for d in scope.models.deployments:
                        if e.target in {d.name, d.model}:
                            self.link(node_id, ids.item_node("model-deployment", scope_id, d.name))
            elif e.target_type == "search":
                for scope_id, scope in n.scopes.items():
                    s = scope.search
                    if (
                        s is not None
                        and s.enabled
                        and e.target
                        in {s.name or "search", (s.existing_resource_id or "").rsplit("/", 1)[-1]}
                    ):
                        self.link(node_id, ids.search_node(scope_id))
            elif e.target_type == "runtime":
                for graph_id, logical in self.runtime_names.items():
                    if logical == e.target:
                        self.link(node_id, graph_id)

    def monitoring_and_governance(self) -> None:
        n = self.norm
        if n.observability and n.observability.enabled and n.observability.alerts:
            alerts = self.node(ids.ALERTS, "alerts")
            watched = [
                node_id
                for node_id, node in self.graph.nodes.items()
                if node.kind in {"runtime", "gateway", "search", "foundry-account"}
            ]
            self.link(alerts, self.workspace, *watched)
        if n.governance and n.governance.enabled:
            governance = self.node(ids.GOVERNANCE, "governance")
            self.link(governance, self.rg, ids.GATEWAY, ids.ALERTS)


def build_graph(norm: NormalisedConfig) -> DeploymentGraph:
    builder = _Builder(norm)
    builder.foundations()
    builder.projects()
    builder.gateway()
    builder.monitoring_and_governance()
    return builder.graph
