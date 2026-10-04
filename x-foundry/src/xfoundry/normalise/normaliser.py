"""Normalisation: defaults, implicit resources and inheritance.

The normaliser assumes the declared configuration already passed
:func:`xfoundry.validators.validate_declared`, so references resolve and names are
unique within their lists. It never raises; problems that only surface while resolving
(for example Foundry IQ with Search disabled) are returned as diagnostics.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from xfoundry import ids
from xfoundry.diagnostics import Diagnostic, error
from xfoundry.normalise.inheritance import merge_models, merge_named
from xfoundry.normalise.model import (
    EffectiveProject,
    HubView,
    ImplicitResource,
    NormalisedConfig,
    PrivateEndpoint,
    ResolvedNetwork,
    ResolvedPrincipal,
    ResolvedRoles,
    ScopeResources,
    is_guid,
)
from xfoundry.schema.models import (
    Chargeback,
    Gateway,
    GatewayAuthentication,
    GatewayModels,
    IndexField,
    KeyVault,
    KnowledgeBase,
    ManagedIdentity,
    ModelDeployment,
    Observability,
    Runtime,
    Search,
    Storage,
    StorageContainer,
    XFoundry,
)

PRIVATE_LINK_ZONES = {
    "foundry": (
        "account",
        (
            "privatelink.cognitiveservices.azure.com",
            "privatelink.openai.azure.com",
            "privatelink.services.ai.azure.com",
        ),
    ),
    "search": ("searchService", ("privatelink.search.windows.net",)),
    "storage": ("blob", ("privatelink.blob.core.windows.net",)),
    "storage-dfs": ("dfs", ("privatelink.dfs.core.windows.net",)),
    "key-vault": ("vault", ("privatelink.vaultcore.azure.net",)),
    "redis": ("redisCache", ("privatelink.redis.cache.windows.net",)),
    "events": ("namespace", ("privatelink.servicebus.windows.net",)),
    "registry": ("registry", ("privatelink.azurecr.io",)),
}


@dataclass(slots=True)
class NormalisedResult:
    config: NormalisedConfig
    diagnostics: list[Diagnostic] = field(default_factory=list)


def normalise(config: XFoundry) -> NormalisedResult:
    """Return the normalised configuration for a declared (validated) ``config``."""
    return _Normaliser(config.model_copy(deep=True)).run()


class _Normaliser:
    def __init__(self, cfg: XFoundry):
        self.cfg = cfg
        self.diagnostics: list[Diagnostic] = []
        self.implicit: list[ImplicitResource] = []
        self.scopes: dict[str, ScopeResources] = {}
        self.project_names = [p.name for p in cfg.projects]
        self.hub = cfg.hub

    # ------------------------------------------------------------------ helpers

    def _add_implicit(self, kind: str, name: str, scope: str, reason: str) -> None:
        self.implicit.append(ImplicitResource(kind=kind, name=name, scope=scope, reason=reason))

    def _inherits(self, project_name: str) -> bool:
        project = next(p for p in self.cfg.projects if p.name == project_name)
        return self.hub is not None and project.inherit_hub

    def _ancestors(self, scope: str) -> list[str]:
        """Scopes whose model deployments are visible from ``scope`` (broadest first)."""
        if scope == ids.ROOT_SCOPE:
            return [ids.ROOT_SCOPE]
        if scope == ids.HUB_SCOPE:
            return [ids.ROOT_SCOPE, ids.HUB_SCOPE]
        name = scope.split(":", 1)[1]
        chain = [ids.ROOT_SCOPE]
        if self._inherits(name) and self.hub.inheritance.models:  # type: ignore[union-attr]
            chain.append(ids.HUB_SCOPE)
        return [*chain, scope]

    def _deployments_visible(self, scope: str) -> list[ModelDeployment]:
        merged, _ = merge_named(
            (s, self.scopes[s].models.deployments) for s in self._ancestors(scope)
        )
        return merged

    # ----------------------------------------------------------------- assembly

    def _assemble(self) -> None:
        cfg = self.cfg
        self.scopes[ids.ROOT_SCOPE] = ScopeResources(
            scope=ids.ROOT_SCOPE, models=cfg.models, search=cfg.search
        )
        if self.hub is not None:
            hub = self.hub
            self.scopes[ids.HUB_SCOPE] = ScopeResources(
                scope=ids.HUB_SCOPE,
                models=hub.models,
                toolboxes=list(hub.toolboxes),
                mcps=list(hub.mcps),
                knowledge_bases=list(hub.iq.knowledge_bases) if hub.iq and hub.iq.enabled else [],
                search=hub.search,
            )
        for p in cfg.projects:
            self.scopes[ids.project_scope(p.name)] = ScopeResources(
                scope=ids.project_scope(p.name),
                models=p.models,
                agents=list(p.agents),
                toolboxes=list(p.toolboxes),
                mcps=list(p.mcps),
                connectors=list(p.connectors),
                knowledge_bases=list(p.iq.knowledge_bases) if p.iq and p.iq.enabled else [],
                search=p.search,
                runtime=p.runtime,
                evaluation=p.evaluation,
            )

        def target(project: str | None) -> ScopeResources:
            if project in self.project_names:
                return self.scopes[ids.project_scope(project)]  # type: ignore[arg-type]
            return self.scopes[ids.ROOT_SCOPE]

        for agent in cfg.agents:
            target(agent.project).agents.append(agent)
        for toolbox in cfg.toolboxes:
            target(toolbox.project).toolboxes.append(toolbox)
        for mcp in cfg.mcps:
            target(mcp.project).mcps.append(mcp)
        for connector in cfg.connectors:
            target(connector.project).connectors.append(connector)
        if cfg.iq and cfg.iq.enabled:
            for kb in cfg.iq.knowledge_bases:
                target(kb.project or cfg.iq.project).knowledge_bases.append(kb)
        if cfg.runtime is not None:
            scope = target(cfg.runtime.project)
            if scope.runtime is None:
                scope.runtime = cfg.runtime
        if cfg.evaluation is not None:
            scope = target(cfg.evaluation.project)
            if scope.evaluation is None:
                scope.evaluation = cfg.evaluation

    # ------------------------------------------------------------------- models

    def _implicit_deployments(self) -> None:
        """Deploy models named in ``allowed`` or used for embeddings when nothing else does."""
        for scope_id, scope in self.scopes.items():
            visible = {d.name for d in self._deployments_visible(scope_id)} | {
                d.model for d in self._deployments_visible(scope_id)
            }
            for model in scope.models.allowed:
                if model in visible:
                    continue
                scope.models.deployments.append(ModelDeployment(name=ids.slug(model), model=model))
                visible |= {ids.slug(model), model}
                self._add_implicit(
                    "model-deployment", ids.slug(model), scope_id, f"'{model}' is in models.allowed"
                )
            for kb in scope.knowledge_bases:
                vector = kb.index.vector
                if not vector.enabled or vector.deployment:
                    continue
                match = next(
                    (d for d in self._deployments_visible(scope_id) if d.model == vector.model),
                    None,
                )
                if match is None:
                    match = ModelDeployment(name=ids.slug(vector.model), model=vector.model)
                    scope.models.deployments.append(match)
                    self._add_implicit(
                        "model-deployment",
                        match.name,
                        scope_id,
                        f"embedding model for knowledge base '{kb.name}'",
                    )
                vector.deployment = match.name

    # -------------------------------------------------------------------- index

    @staticmethod
    def _materialise_index(kb: KnowledgeBase) -> None:
        index = kb.index
        if index.name is None:
            index.name = kb.name
        have = {f.name for f in index.fields}

        def add(field_: IndexField) -> None:
            if field_.name not in have:
                index.fields.append(field_)
                have.add(field_.name)

        if index.key_field == "id":
            add(IndexField(name="id", type="Edm.String", key=True))
        if index.content_field == "content":
            add(IndexField(name="content", type="Edm.String", searchable=True))
        if index.title_field == "title":
            add(IndexField(name="title", type="Edm.String", searchable=True))
        if index.vector.enabled and index.vector_field == "contentVector":
            add(
                IndexField(
                    name="contentVector",
                    type="Collection(Edm.Single)",
                    searchable=True,
                    retrievable=False,
                    dimensions=index.vector.dimensions,
                    vector_profile=index.vector.profile,
                )
            )
        if "title_field" not in index.semantic.model_fields_set:
            index.semantic.title_field = index.title_field
        if "content_fields" not in index.semantic.model_fields_set:
            index.semantic.content_fields = [index.content_field]

    # ------------------------------------------------------------------ search

    def _chain_for_search(self, scope_id: str) -> list[str]:
        if scope_id == ids.ROOT_SCOPE:
            return [ids.ROOT_SCOPE]
        if scope_id == ids.HUB_SCOPE:
            return [ids.HUB_SCOPE, ids.ROOT_SCOPE]
        name = scope_id.split(":", 1)[1]
        chain = [scope_id]
        if self._inherits(name) and self.hub.inheritance.search:  # type: ignore[union-attr]
            chain.append(ids.HUB_SCOPE)
        return [*chain, ids.ROOT_SCOPE]

    def _find_search(self, scope_id: str) -> ScopeResources | None:
        for candidate in self._chain_for_search(scope_id):
            if self.scopes[candidate].search is not None:
                return self.scopes[candidate]
        return None

    def _needs_search(self, project: EffectiveProject | None, scope_id: str) -> bool:
        if project is not None:
            return bool(project.knowledge_bases)
        return bool(self.scopes[scope_id].knowledge_bases)

    def _resolve_search(self, effective: dict[str, EffectiveProject]) -> None:
        consumers: list[tuple[str, EffectiveProject | None]] = [
            (ids.project_scope(n), p) for n, p in effective.items()
        ]
        if self.hub is not None:
            consumers.append((ids.HUB_SCOPE, None))
        # Pass 1: derive a root Search when IQ needs one and none is declared.
        for scope_id, project in consumers:
            if not self._needs_search(project, scope_id):
                continue
            found = self._find_search(scope_id)
            if found is None:
                root = self.scopes[ids.ROOT_SCOPE]
                root.search = Search()
                self._add_implicit(
                    "search", "search", ids.ROOT_SCOPE, "Foundry IQ requires Azure AI Search"
                )
            elif not found.search.enabled:  # type: ignore[union-attr]
                label = scope_id if project is None else f"project '{project.name}'"
                self.diagnostics.append(
                    error(
                        "XF020",
                        f"Foundry IQ for {label} requires Azure AI Search, but search is "
                        f"disabled in scope '{found.scope}'",
                        "x-foundry.search.enabled",
                    )
                )
        # Pass 2: record the resolved Search scope for every consumer.
        for scope_id, project in consumers:
            found = self._find_search(scope_id)
            resolved = found.scope if found and found.search and found.search.enabled else None
            if project is not None:
                project.search_scope = resolved
            elif self.hub is not None:
                self._hub_search_scope = resolved

    # ---------------------------------------------------------------- effective

    def _principals(self, *role_sets: object) -> ResolvedRoles:
        merged: dict[str, list[ResolvedPrincipal]] = {
            "admins": [],
            "developers": [],
            "consumers": [],
            "operators": [],
        }
        for roles in role_sets:
            for role in merged:
                for principal in getattr(roles, role, []):
                    resolved = self._principal(principal)
                    if all(resolved.key != existing.key for existing in merged[role]):
                        merged[role].append(resolved)
        return ResolvedRoles(**merged)

    @staticmethod
    def _principal(value: object) -> ResolvedPrincipal:
        if isinstance(value, str):
            if is_guid(value):
                return ResolvedPrincipal(type="group", id=value)
            return ResolvedPrincipal(type="group", name=value)
        return ResolvedPrincipal(type=value.type, id=value.id, name=value.name)  # type: ignore[attr-defined]

    def _effective_project(self, name: str) -> EffectiveProject:
        cfg = self.cfg
        p = next(x for x in cfg.projects if x.name == name)
        scope_id = ids.project_scope(name)
        own = self.scopes[scope_id]
        root = self.scopes[ids.ROOT_SCOPE]
        inherits = self._inherits(name)
        hub = self.scopes.get(ids.HUB_SCOPE)
        inheritance = self.hub.inheritance if self.hub else None

        def layers(attr: str, hub_flag: str | None):
            out = [(ids.ROOT_SCOPE, getattr(root, attr))]
            if (
                inherits
                and hub is not None
                and (hub_flag is None or getattr(inheritance, hub_flag))
            ):
                out.append((ids.HUB_SCOPE, getattr(hub, attr)))
            out.append((scope_id, getattr(own, attr)))
            return out

        model_layers = [(ids.ROOT_SCOPE, root.models)]
        if inherits and hub is not None and inheritance.models:  # type: ignore[union-attr]
            model_layers.append((ids.HUB_SCOPE, hub.models))
        model_layers.append((scope_id, own.models))

        origins: dict[str, str] = {}
        collected = {}
        for attr, flag, kind in (
            ("agents", None, "agent"),
            ("toolboxes", "toolboxes", "toolbox"),
            ("mcps", "mcps", "mcp"),
            ("connectors", None, "connector"),
            ("knowledge_bases", "iq", "knowledgeBase"),
        ):
            items, item_origins = merge_named(layers(attr, flag))
            collected[attr] = items
            origins.update({f"{kind}:{k}": v for k, v in item_origins.items()})
        runtime = own.runtime or root.runtime
        evaluation = own.evaluation or root.evaluation
        gateway = p.gateway.model_copy(deep=True) if p.gateway else None
        if gateway is not None and gateway.path is None:
            gateway.path = f"/{name}"
        tags = {**self.base_tags, **p.tags, "project": name}
        return EffectiveProject(
            name=name,
            display_name=p.display_name or name,
            description=p.description,
            location=p.location or cfg.defaults.location,
            resource_group=p.resource_group or cfg.defaults.resource_group,
            inherits_hub=inherits,
            roles=self._principals(cfg.security.roles, p.roles),
            tags=tags,
            models=merge_models(model_layers),
            runtime=runtime,
            runtime_scope=(scope_id if own.runtime else ids.ROOT_SCOPE if root.runtime else None),
            evaluation=evaluation,
            gateway=gateway,
            origins=origins,
            **collected,
        )

    # --------------------------------------------------------------- components

    def _storage(self) -> Storage | None:
        storage = self.cfg.storage
        purposes: dict[str, str] = {}
        containers: dict[str, str] = {}
        for scope in self.scopes.values():
            for kb in scope.knowledge_bases:
                for source in kb.sources:
                    if source.type in {"blob", "adls"} and not source.connection:
                        purposes.setdefault(
                            "knowledge", f"knowledge base '{kb.name}' has a {source.type} source"
                        )
                        containers.setdefault(source.container or "", f"knowledge base '{kb.name}'")
            ev = scope.evaluation
            if ev is not None and ev.enabled and ev.datasets:
                purposes.setdefault("evaluations", "evaluation datasets are enabled")
        if not purposes:
            return storage
        if storage is None:
            storage = Storage(purposes=[])
            self._add_implicit("storage", "storage", ids.ROOT_SCOPE, "; ".join(purposes.values()))
        elif not storage.enabled:
            self.diagnostics.append(
                error(
                    "XF107",
                    f"storage is disabled but required: {'; '.join(purposes.values())}",
                    "x-foundry.storage.enabled",
                )
            )
            return storage
        for purpose, reason in purposes.items():
            if purpose not in storage.purposes:
                storage.purposes.append(purpose)  # type: ignore[arg-type]
                self._add_implicit("storage-purpose", purpose, ids.ROOT_SCOPE, reason)
        adls = any(
            s.type == "adls"
            for scope in self.scopes.values()
            for kb in scope.knowledge_bases
            for s in kb.sources
        )
        if (
            adls
            and "hierarchical_namespace" not in storage.model_fields_set
            and not storage.existing_resource_id
        ):
            storage.hierarchical_namespace = True
        have = {c.name for c in storage.containers}
        for purpose in storage.purposes:
            if not any(c.purpose == purpose for c in storage.containers):
                storage.containers.append(StorageContainer(name=purpose, purpose=purpose))
                have.add(purpose)
        for container in containers:
            if container and container not in have:
                storage.containers.append(StorageContainer(name=container, purpose="knowledge"))
                have.add(container)
        return storage

    def _gateway(self) -> Gateway | None:
        gateway = self.cfg.gateway
        if gateway is None or not gateway.enabled:
            return gateway
        if gateway.authentication is None:
            prefix = self.cfg.defaults.naming_prefix or "x-foundry"
            gateway.authentication = GatewayAuthentication(audiences=[f"api://{prefix}-gateway"])
            self._add_implicit(
                "gateway-authentication",
                "entra",
                ids.ROOT_SCOPE,
                "gateway requires Entra authentication",
            )
        tracking = gateway.token_tracking
        charge: Chargeback = gateway.chargeback
        if charge.enabled:
            for dim in charge.dimensions:
                if dim not in tracking.dimensions:
                    tracking.dimensions.append(dim)
        if "department" in tracking.dimensions and tracking.department_claim is None:
            tracking.department_claim = "department"
        if not gateway.models.model_fields_set - {"require_deployment_alias"}:
            root_models = self.cfg.models
            gateway.models = GatewayModels(
                default=root_models.default,
                allowed=list(root_models.allowed),
                denied=list(root_models.denied),
                require_deployment_alias=gateway.models.require_deployment_alias,
            )
        return gateway

    def _runtimes(self) -> list[Runtime]:
        found: list[Runtime] = [s.runtime for s in self.scopes.values() if s.runtime is not None]
        found += [
            a.runtime for s in self.scopes.values() for a in s.agents if a.runtime is not None
        ]
        for runtime in found:
            if "memory" not in runtime.resources.model_fields_set:
                runtime.resources.memory = f"{runtime.resources.cpu * 2:g}Gi"
        return found

    def _network(self, comps: dict[str, bool], search_scopes: list[str]) -> ResolvedNetwork:
        net = self.cfg.security.network
        resolved = ResolvedNetwork(
            mode=net.mode,
            allowed_ips=list(net.allowed_ips),
            existing_vnet_resource_id=net.existing_vnet_resource_id,
        )
        if net.existing_vnet_resource_id:
            resolved.vnet = "existing"
        elif net.mode == "private":
            resolved.vnet = "create"
            self._add_implicit("network", "vnet", ids.ROOT_SCOPE, "private network mode")
        if net.mode != "private":
            return resolved
        resolved.private_dns = net.private_dns
        components: list[tuple[str, str]] = [(ids.FOUNDRY, "foundry")]
        components += [(ids.search_node(s), "search") for s in search_scopes]
        if comps.get("storage"):
            components.append((ids.STORAGE, "storage"))
            if self.storage and self.storage.hierarchical_namespace:
                components.append((ids.STORAGE, "storage-dfs"))
        for key, node, zone_key in (
            ("key_vault", ids.KEY_VAULT, "key-vault"),
            ("redis", ids.REDIS, "redis"),
            ("events", ids.EVENTS, "events"),
            ("registry", ids.REGISTRY, "registry"),
        ):
            if comps.get(key):
                components.append((node, zone_key))
        zones: list[str] = []
        for component, zone_key in components:
            group, names = PRIVATE_LINK_ZONES[zone_key]
            resolved.private_endpoints.append(PrivateEndpoint(component=component, group=group))
            zones.extend(z for z in names if z not in zones)
        if net.private_dns:
            resolved.private_dns_zones = zones
        return resolved

    # ---------------------------------------------------------------------- run

    def run(self) -> NormalisedResult:
        cfg = self.cfg
        self.base_tags = {
            "environment": cfg.defaults.environment,
            "managed-by": "x-foundry",
            **cfg.defaults.tags,
            **cfg.tags,
        }
        self._hub_search_scope: str | None = None
        self._assemble()
        self._implicit_deployments()
        for scope in self.scopes.values():
            for kb in scope.knowledge_bases:
                self._materialise_index(kb)

        effective = {name: self._effective_project(name) for name in self.project_names}
        self._resolve_search(effective)
        runtimes = self._runtimes()
        self.storage = self._storage()
        gateway = self._gateway()
        redis = cfg.redis
        if (
            redis is not None
            and redis.service == "azure-cache-for-redis"
            and "sku" not in redis.model_fields_set
        ):
            redis.sku = "standard"

        identity = cfg.managed_identity
        if identity is None:
            identity = ManagedIdentity()
            self._add_implicit(
                "managed-identity", "identity", ids.ROOT_SCOPE, "keyless access and RBAC"
            )
        uses_secrets = (
            any(
                (m.authentication and m.authentication.secret_ref)
                for s in self.scopes.values()
                for m in s.mcps
            )
            or any(
                (c.authentication and c.authentication.secret_ref)
                for s in self.scopes.values()
                for c in s.connectors
            )
            or any(r.secrets for r in runtimes)
        )
        key_vault = cfg.key_vault
        if key_vault is None and uses_secrets:
            key_vault = KeyVault()
            self._add_implicit(
                "key-vault", "key-vault", ids.ROOT_SCOPE, "secret references are used"
            )
        observability = cfg.observability
        if observability is None and (
            (gateway is not None and gateway.enabled) or any(r.enabled for r in runtimes)
        ):
            observability = Observability()
            self._add_implicit(
                "observability", "observability", ids.ROOT_SCOPE, "gateway or runtime telemetry"
            )

        search_scopes = sorted(
            {p.search_scope for p in effective.values() if p.search_scope}
            | ({self._hub_search_scope} if self._hub_search_scope else set())
        )
        registry_needed = any(
            r.enabled and r.source and r.registry.mode == "managed" for r in runtimes
        )
        if registry_needed:
            self._add_implicit(
                "container-registry",
                "registry",
                ids.ROOT_SCOPE,
                "runtime image is built from source",
            )
        comps = {
            "storage": bool(self.storage and self.storage.enabled),
            "key_vault": bool(key_vault and key_vault.enabled),
            "redis": bool(cfg.redis and cfg.redis.enabled),
            "events": bool(cfg.events and cfg.events.enabled),
            "registry": registry_needed,
        }
        network = self._network(comps, search_scopes)

        hub_view = None
        if self.hub is not None:
            hub_view = HubView(
                name=self.hub.name,
                location=self.hub.location or cfg.defaults.location,
                resource_group=self.hub.resource_group or cfg.defaults.resource_group,
                inheritance=self.hub.inheritance,
                search_scope=self._hub_search_scope,
                tags={**self.base_tags, **self.hub.tags},
            )
        config = NormalisedConfig(
            schema_version=cfg.schema_version,
            topology_mode=cfg.topology.mode,
            naming_prefix=cfg.defaults.naming_prefix,
            environment=cfg.defaults.environment,
            location=cfg.defaults.location,
            resource_group=cfg.defaults.resource_group,
            tags=self.base_tags,
            roles=self._principals(cfg.security.roles),
            network=network,
            local_authentication=cfg.security.local_authentication,
            purge_protection=cfg.security.purge_protection,
            hub=hub_view,
            scopes=self.scopes,
            projects=list(effective.values()),
            gateway=gateway,
            storage=self.storage,
            redis=redis,
            key_vault=key_vault,
            managed_identity=identity,
            observability=observability,
            events=cfg.events,
            governance=cfg.governance,
            implicit=self.implicit,
        )
        return NormalisedResult(config, self.diagnostics)
