"""Validation of the declared configuration (before normalisation).

These rules need to know what the author wrote, so they run on the parsed model with
``model_fields_set`` available.
"""

from __future__ import annotations

import re
from collections.abc import Iterable
from typing import Any

from pydantic import BaseModel

from xfoundry.azure_names import name_problems
from xfoundry.diagnostics import Diagnostic, error, warning
from xfoundry.schema.models import (
    Agent,
    Connector,
    KnowledgeBase,
    Mcp,
    Runtime,
    XFoundry,
)
from xfoundry.validators.common import ROOT, dedupe, duplicates
from xfoundry.validators.locations import canonical, is_known_region
from xfoundry.validators.secrets import (
    find_raw_secrets,
    is_secret_name_or_reference,
)

_ARM = r"^/subscriptions/[^/]+/resourceGroups/[^/]+/providers/{type}/[^/]+$"
_EXISTING_TYPES = {
    "search": ("Microsoft.Search/searchServices", "search"),
    "storage": ("Microsoft.Storage/storageAccounts", "storage"),
    "redis": ("Microsoft.Cache/(?:redis|redisEnterprise)", "redis"),
    "keyVault": ("Microsoft.KeyVault/vaults", "key-vault"),
    "managedIdentity": ("Microsoft.ManagedIdentity/userAssignedIdentities", "managed-identity"),
}
# Settings only meaningful when the extension creates the resource.
_CREATE_ONLY = {
    "search": (
        "sku",
        "replicas",
        "partitions",
        "semantic_ranking",
        "local_authentication",
        "public_network_access",
        "managed_identity",
        "tags",
    ),
    "storage": (
        "sku",
        "hierarchical_namespace",
        "public_network_access",
        "local_authentication",
        "retention_days",
        "tags",
    ),
    "redis": (
        "service",
        "sku",
        "capacity",
        "tls_only",
        "public_network_access",
        "persistence",
        "tags",
    ),
    "keyVault": (
        "sku",
        "rbac_authorisation",
        "soft_delete_days",
        "purge_protection",
        "public_network_access",
        "tags",
    ),
    "managedIdentity": ("tags",),
}
_REDIS_SKUS = {
    "azure-managed-redis": {"memory-optimised", "balanced", "compute-optimised"},
    "azure-cache-for-redis": {"basic", "standard", "premium"},
}
_CRON = re.compile(r"^\S+(?:\s+\S+){4}$")


def _camel(snake: str) -> str:
    head, *rest = snake.split("_")
    return head + "".join(p.title() for p in rest)


def _p(*parts: str | int) -> str:
    out = ROOT
    for part in parts:
        out += f"[{part}]" if isinstance(part, int) else f".{part}"
    return out


# ------------------------------------------------------------------ iteration helpers


def _scopes(cfg: XFoundry) -> Iterable[tuple[str, str, Any]]:
    """Yield ``(scope_id, path_prefix, holder)`` for root, hub and each project."""
    yield "root", ROOT, cfg
    if cfg.hub is not None:
        yield "hub", f"{ROOT}.hub", cfg.hub
    for p in cfg.projects:
        yield f"project:{p.name}", f"{ROOT}.projects[{p.name}]", p


def _kbs(holder: Any) -> list[KnowledgeBase]:
    iq = getattr(holder, "iq", None)
    return list(iq.knowledge_bases) if iq else []


def _runtimes(cfg: XFoundry) -> list[tuple[str, Runtime]]:
    found: list[tuple[str, Runtime]] = []
    for _, prefix, holder in _scopes(cfg):
        runtime = getattr(holder, "runtime", None)
        if runtime is not None:
            found.append((f"{prefix}.runtime", runtime))
        for agent in getattr(holder, "agents", []):
            if agent.runtime is not None:
                found.append((f"{prefix}.agents[{agent.name}].runtime", agent.runtime))
    return found


# --------------------------------------------------------------------- rules 1-5, 16


def _projects_and_hub(cfg: XFoundry) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    for name in duplicates(cfg.projects, lambda p: p.name):
        out.append(error("XF001", f"project name '{name}' is used more than once", _p("projects")))
    if cfg.hub is not None and cfg.hub.name in {p.name for p in cfg.projects}:
        out.append(
            error(
                "XF001",
                f"hub name '{cfg.hub.name}' collides with a project name",
                _p("hub", "name"),
            )
        )
    # Rule 3 (also enforced by JSON Schema and the model).
    if cfg.hub is not None and cfg.topology.mode != "hub-spoke":
        out.append(
            error("XF003", "'hub' is only valid when topology.mode is 'hub-spoke'", _p("hub"))
        )
    if cfg.hub is None and cfg.topology.mode == "hub-spoke":
        out.append(error("XF003", "topology.mode 'hub-spoke' requires a 'hub' section", _p("hub")))
    # Rule 4: only an explicit inheritHub: true is a contradiction; the default resolves to false.
    if cfg.hub is None:
        for p in cfg.projects:
            if "inherit_hub" in p.model_fields_set and p.inherit_hub:
                out.append(
                    error(
                        "XF004",
                        f"project '{p.name}' sets inheritHub: true but there is no hub",
                        _p(f"projects[{p.name}]", "inheritHub"),
                    )
                )
    return out


def _unique_names(cfg: XFoundry) -> list[Diagnostic]:
    """Rule 2: names are unique within their effective scope."""
    out: list[Diagnostic] = []
    project_names = {p.name for p in cfg.projects}

    def check(label: str, scope_path: str, items: Iterable[Any]) -> None:
        for dup in duplicates(items, lambda i: i.name):
            out.append(error("XF002", f"duplicate {label} name '{dup}'", scope_path))

    def assigned(items: Iterable[Any], project: str) -> list[Any]:
        return [i for i in items if getattr(i, "project", None) == project]

    def unassigned(items: Iterable[Any]) -> list[Any]:
        return [i for i in items if getattr(i, "project", None) not in project_names]

    root_kbs: list[tuple[KnowledgeBase, str | None]] = []
    if cfg.iq:
        root_kbs = [(kb, kb.project or cfg.iq.project) for kb in cfg.iq.knowledge_bases]
    check("agent", _p("agents"), unassigned(cfg.agents))
    check("toolbox", _p("toolboxes"), unassigned(cfg.toolboxes))
    check("MCP", _p("mcps"), unassigned(cfg.mcps))
    check("connector", _p("connectors"), unassigned(cfg.connectors))
    check("model deployment", _p("models", "deployments"), cfg.models.deployments)
    check(
        "knowledge base",
        _p("iq", "knowledgeBases"),
        [kb for kb, pr in root_kbs if pr not in project_names],
    )
    if cfg.hub is not None:
        hp = f"{ROOT}.hub"
        check("toolbox", f"{hp}.toolboxes", cfg.hub.toolboxes)
        check("MCP", f"{hp}.mcps", cfg.hub.mcps)
        check("model deployment", f"{hp}.models.deployments", cfg.hub.models.deployments)
        check("knowledge base", f"{hp}.iq.knowledgeBases", _kbs(cfg.hub))
    for p in cfg.projects:
        pp = f"{ROOT}.projects[{p.name}]"
        check("agent", f"{pp}.agents", [*p.agents, *assigned(cfg.agents, p.name)])
        check("toolbox", f"{pp}.toolboxes", [*p.toolboxes, *assigned(cfg.toolboxes, p.name)])
        check("MCP", f"{pp}.mcps", [*p.mcps, *assigned(cfg.mcps, p.name)])
        check("connector", f"{pp}.connectors", [*p.connectors, *assigned(cfg.connectors, p.name)])
        check("model deployment", f"{pp}.models.deployments", p.models.deployments)
        own = _kbs(p) + [kb for kb, pr in root_kbs if pr == p.name]
        check("knowledge base", f"{pp}.iq.knowledgeBases", own)
    # Nested lists.
    for _, prefix, holder in _scopes(cfg):
        for toolbox in getattr(holder, "toolboxes", []):
            check("tool", f"{prefix}.toolboxes[{toolbox.name}].tools", toolbox.tools)
        for kb in _kbs(holder):
            kp = f"{prefix}.iq.knowledgeBases[{kb.name}]"
            check("knowledge source", f"{kp}.sources", kb.sources)
            check("index field", f"{kp}.index.fields", kb.index.fields)
            check("route", f"{kp}.routing.routes", kb.routing.routes)
            check("scoring profile", f"{kp}.index.scoringProfiles", kb.index.scoring_profiles)
    for path, runtime in _runtimes(cfg):
        check("scale rule", f"{path}.scale.rules", runtime.scale.rules)
    if cfg.gateway is not None:
        gp = _p("gateway")
        check("gateway endpoint", f"{gp}.endpoints", cfg.gateway.endpoints)
        for dup in duplicates(cfg.gateway.endpoints, lambda e: e.path):
            out.append(
                error(
                    "XF013",
                    f"gateway endpoint path '{dup}' is used more than once",
                    f"{gp}.endpoints",
                )
            )
        check("quota profile", f"{gp}.quotas.profiles", cfg.gateway.quotas.profiles)
        check("limit profile", f"{gp}.limits.profiles", cfg.gateway.limits.profiles)
        check("backend", f"{gp}.routing.backends", cfg.gateway.routing.backends)
    if cfg.storage is not None:
        check("storage container", _p("storage", "containers"), cfg.storage.containers)
    if cfg.events is not None:
        check("event entity", _p("events", "entities"), cfg.events.entities)
    for _, prefix, holder in _scopes(cfg):
        ev = getattr(holder, "evaluation", None)
        if ev is not None:
            check("dataset", f"{prefix}.evaluation.datasets", ev.datasets)
            check("evaluator", f"{prefix}.evaluation.evaluators", ev.evaluators)
    if cfg.managed_identity is not None:
        check(
            "federated credential",
            _p("managedIdentity", "federatedCredentials"),
            cfg.managed_identity.federated_credentials,
        )
    return out


def _project_references(cfg: XFoundry) -> list[Diagnostic]:
    """Rule 5 (declared part): every ``project`` field names an existing project."""
    names = {p.name for p in cfg.projects}
    out: list[Diagnostic] = []

    def check(ref: str | None, path: str, owner: str | None = None) -> None:
        if ref is None:
            return
        if ref not in names:
            out.append(error("XF005", f"unknown project '{ref}'", path))
        elif owner is not None and ref != owner:
            out.append(
                error(
                    "XF005",
                    f"declared inside project '{owner}' but assigned to project '{ref}'",
                    path,
                )
            )

    def items(holder_path: str, owner: str | None, holder: Any) -> None:
        for collection in ("agents", "toolboxes", "mcps", "connectors"):
            for item in getattr(holder, collection, []):
                check(item.project, f"{holder_path}.{collection}[{item.name}].project", owner)
        iq = getattr(holder, "iq", None)
        if iq is not None:
            check(iq.project, f"{holder_path}.iq.project", owner)
            for kb in iq.knowledge_bases:
                check(kb.project, f"{holder_path}.iq.knowledgeBases[{kb.name}].project", owner)
                for route in kb.routing.routes:
                    check(
                        route.when.project,
                        f"{holder_path}.iq.knowledgeBases[{kb.name}].routing.routes[{route.name}].when.project",
                    )
        for single in ("runtime", "evaluation"):
            value = getattr(holder, single, None)
            if value is not None:
                check(value.project, f"{holder_path}.{single}.project", owner)

    items(ROOT, None, cfg)
    if cfg.hub is not None:
        items(f"{ROOT}.hub", None, cfg.hub)
    for p in cfg.projects:
        items(f"{ROOT}.projects[{p.name}]", p.name, p)
    if cfg.gateway is not None:
        for e in cfg.gateway.endpoints:
            check(e.project, _p("gateway", f"endpoints[{e.name}]") + ".project")
    return out


def _overlaps(label: str, allowed: list[str], denied: list[str], path: str) -> list[Diagnostic]:
    both = sorted(set(allowed) & set(denied))
    if not both:
        return []
    return [error("XF016", f"{label} lists {', '.join(both)} as both allowed and denied", path)]


def _model_sets(cfg: XFoundry) -> list[Diagnostic]:
    out = _overlaps("models", cfg.models.allowed, cfg.models.denied, _p("models"))
    if cfg.hub is not None:
        out += _overlaps(
            "hub models", cfg.hub.models.allowed, cfg.hub.models.denied, _p("hub", "models")
        )
    for p in cfg.projects:
        out += _overlaps(
            f"project '{p.name}' models",
            p.models.allowed,
            p.models.denied,
            _p(f"projects[{p.name}]", "models"),
        )
    if cfg.gateway is not None:
        m = cfg.gateway.models
        out += _overlaps("gateway models", m.allowed, m.denied, _p("gateway", "models"))
    if cfg.governance is not None:
        mp = cfg.governance.model_policy
        out += _overlaps(
            "modelPolicy", mp.allowed_models, mp.denied_models, _p("governance", "modelPolicy")
        )
    return out


# ------------------------------------------------------------------------- rule 17


def _secrets(cfg: XFoundry) -> list[Diagnostic]:
    dumped = cfg.model_dump(by_alias=True, mode="json", exclude_unset=True)
    out = find_raw_secrets(dumped)
    for path, runtime in _runtimes(cfg):
        for key, value in runtime.secrets.items():
            if not is_secret_name_or_reference(value):
                out.append(
                    error(
                        "XF017",
                        f"secret '{key}' must be a Key Vault secret name or reference, not a value",
                        f"{path}.secrets.{key}",
                    )
                )
    for path, mcp_or_connector in _authenticated(cfg):
        auth = mcp_or_connector.authentication
        if auth is None:
            continue
        if auth.secret_ref and not is_secret_name_or_reference(auth.secret_ref):
            out.append(
                error(
                    "XF017",
                    "secretRef must be a Key Vault secret name or reference",
                    f"{path}.authentication.secretRef",
                )
            )
        if auth.mode == "apiKey" and not auth.secret_ref:
            out.append(
                error("XF119", "apiKey authentication requires secretRef", f"{path}.authentication")
            )
    return dedupe(out)


def _authenticated(cfg: XFoundry) -> list[tuple[str, Mcp | Connector]]:
    found: list[tuple[str, Mcp | Connector]] = []
    for _, prefix, holder in _scopes(cfg):
        found += [(f"{prefix}.mcps[{m.name}]", m) for m in getattr(holder, "mcps", [])]
        found += [(f"{prefix}.connectors[{c.name}]", c) for c in getattr(holder, "connectors", [])]
    return found


# ------------------------------------------------------------------------- rule 22


def _existing_vs_created(cfg: XFoundry) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    targets: list[tuple[str, str, BaseModel]] = []
    for _, prefix, holder in _scopes(cfg):
        if getattr(holder, "search", None) is not None:
            targets.append(("search", f"{prefix}.search", holder.search))
    for kind in ("storage", "redis", "key_vault", "managed_identity"):
        value = getattr(cfg, kind)
        if value is not None:
            targets.append((_camel(kind), _p(_camel(kind)), value))
    for kind, path, component in targets:
        existing = component.existing_resource_id  # type: ignore[attr-defined]
        if not existing:
            continue
        arm_type, _ = _EXISTING_TYPES[kind]
        if not re.match(_ARM.format(type=arm_type), existing, re.IGNORECASE):
            out.append(
                error(
                    "XF022",
                    f"existingResourceId is not a {arm_type.replace('(?:redis|redisEnterprise)', 'redis')} resource ID",
                    f"{path}.existingResourceId",
                )
            )
        conflicts = [f for f in _CREATE_ONLY[kind] if f in component.model_fields_set]
        for field_name in conflicts:
            out.append(
                error(
                    "XF022",
                    f"'{_camel(field_name)}' configures an extension-created resource and cannot be "
                    "combined with existingResourceId",
                    f"{path}.{_camel(field_name)}",
                )
            )
    for path, runtime in _runtimes(cfg):
        reg = runtime.registry
        if reg.mode == "managed" and (reg.resource_id or reg.server):
            out.append(
                error(
                    "XF022",
                    "a managed registry cannot set resourceId or server; use mode 'existing' or 'external'",
                    f"{path}.registry",
                )
            )
        if reg.mode == "existing" and not reg.resource_id:
            out.append(
                error(
                    "XF022",
                    "registry mode 'existing' requires resourceId",
                    f"{path}.registry.resourceId",
                )
            )
        if (
            reg.mode == "existing"
            and reg.resource_id
            and not re.match(
                _ARM.format(type="Microsoft.ContainerRegistry/registries"),
                reg.resource_id,
                re.IGNORECASE,
            )
        ):
            out.append(
                error(
                    "XF022",
                    "resourceId is not a container registry resource ID",
                    f"{path}.registry.resourceId",
                )
            )
        if reg.mode == "external" and not reg.server:
            out.append(
                error(
                    "XF022", "registry mode 'external' requires server", f"{path}.registry.server"
                )
            )
        if reg.mode != "managed" and reg.name:
            out.append(
                error(
                    "XF022",
                    "registry 'name' applies only to a managed registry",
                    f"{path}.registry.name",
                )
            )
    return out


# ------------------------------------------------------------------------- rule 24


def _explicit_names(cfg: XFoundry) -> list[Diagnostic]:
    out: list[Diagnostic] = []

    def check(kind: str, name: str | None, path: str) -> None:
        if not name:
            return
        for problem in name_problems(kind, name):
            out.append(error("XF024", f"'{name}' is not a valid {kind} name: {problem}", path))

    for _, prefix, holder in _scopes(cfg):
        if getattr(holder, "search", None) is not None:
            check("search", holder.search.name, f"{prefix}.search.name")
    check("storage", cfg.storage.name if cfg.storage else None, _p("storage", "name"))
    check("redis", cfg.redis.name if cfg.redis else None, _p("redis", "name"))
    check("key-vault", cfg.key_vault.name if cfg.key_vault else None, _p("keyVault", "name"))
    check(
        "managed-identity",
        cfg.managed_identity.name if cfg.managed_identity else None,
        _p("managedIdentity", "name"),
    )
    check("apim", cfg.gateway.name if cfg.gateway else None, _p("gateway", "name"))
    check("service-bus", cfg.events.namespace if cfg.events else None, _p("events", "namespace"))
    check("resource-group", cfg.defaults.resource_group, _p("defaults", "resourceGroup"))
    check("resource-group", cfg.hub.resource_group if cfg.hub else None, _p("hub", "resourceGroup"))
    for p in cfg.projects:
        check("resource-group", p.resource_group, _p(f"projects[{p.name}]", "resourceGroup"))
    for _, prefix, holder in _scopes(cfg):
        for kb in _kbs(holder):
            for source in kb.sources:
                if source.type in {"blob", "adls"}:
                    check(
                        "storage-container",
                        source.container,
                        f"{prefix}.iq.knowledgeBases[{kb.name}].sources[{source.name}].container",
                    )
    if cfg.storage is not None:
        for container in cfg.storage.containers:
            check(
                "storage-container",
                container.name,
                _p("storage", f"containers[{container.name}]", "name"),
            )
    for path, runtime in _runtimes(cfg):
        check("container-app", runtime.name, f"{path}.name")
        check("registry", runtime.registry.name, f"{path}.registry.name")
    return out


# ------------------------------------------------------------------------- rule 23


def _locations(cfg: XFoundry) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    located: list[tuple[str, str]] = []

    def add(location: str | None, path: str) -> None:
        if location:
            located.append((location, path))

    add(cfg.defaults.location, _p("defaults", "location"))
    if cfg.hub is not None:
        add(cfg.hub.location, _p("hub", "location"))
    for p in cfg.projects:
        add(p.location, _p(f"projects[{p.name}]", "location"))
    for _, prefix, holder in _scopes(cfg):
        models = holder.models if hasattr(holder, "models") else None
        for d in models.deployments if models is not None else []:
            add(d.location, f"{prefix}.models.deployments[{d.name}].location")
    residency = cfg.governance.data_residency if cfg.governance else []
    for index, region in enumerate(residency):
        if not is_known_region(region):
            out.append(
                error(
                    "XF023",
                    f"'{region}' is not a known Azure region",
                    _p("governance", "dataResidency", index),
                )
            )
    allowed = {canonical(r) for r in residency}
    for location, path in located:
        if not is_known_region(location):
            out.append(error("XF023", f"'{location}' is not a known Azure region", path))
        elif allowed and canonical(location) not in allowed:
            out.append(
                error(
                    "XF023",
                    f"'{location}' is outside governance.dataResidency ({', '.join(sorted(allowed))})",
                    path,
                )
            )
    account_location = (
        (cfg.hub.location if cfg.hub else None)
        or cfg.defaults.location
        or next((p.location for p in cfg.projects if p.location), None)
    )
    if account_location:
        for p in cfg.projects:
            if p.location and canonical(p.location) != canonical(account_location):
                out.append(
                    warning(
                        "XF120",
                        f"project '{p.name}' asks for {p.location}, but the Foundry resource is created "
                        f"in {account_location}; one Foundry resource is deployed per configuration",
                        _p(f"projects[{p.name}]", "location"),
                    )
                )
    if cfg.hub is not None:
        hub_location = cfg.hub.location or cfg.defaults.location
        for p in cfg.projects:
            location = p.location or cfg.defaults.location
            if (
                p.inherit_hub
                and hub_location
                and location
                and canonical(hub_location) != canonical(location)
            ):
                out.append(
                    warning(
                        "XF120",
                        f"project '{p.name}' ({location}) inherits shared resources from hub "
                        f"'{cfg.hub.name}' in {hub_location}; expect cross-region latency and data movement",
                        _p(f"projects[{p.name}]", "location"),
                    )
                )
    return out


# ------------------------------------------------------------- security and others


def _security(cfg: XFoundry) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    net = cfg.security.network
    if not cfg.security.roles.admins:
        out.append(
            warning(
                "XF114",
                "security.roles.admins is empty; nobody will administer the deployment",
                _p("security", "roles", "admins"),
            )
        )
    if net.mode == "restricted" and not (net.allowed_ips or net.existing_vnet_resource_id):
        out.append(
            error(
                "XF105",
                "network mode 'restricted' requires allowedIps or existingVnetResourceId",
                _p("security", "network"),
            )
        )
    if net.mode == "private" and cfg.security.public_network_access:
        out.append(
            error(
                "XF021",
                "security.publicNetworkAccess cannot be true when network mode is 'private'",
                _p("security", "publicNetworkAccess"),
            )
        )
    components: list[tuple[str, Any]] = []
    for _, prefix, holder in _scopes(cfg):
        if getattr(holder, "search", None) is not None:
            components.append((f"{prefix}.search", holder.search))
    for kind in ("storage", "redis", "key_vault", "events"):
        value = getattr(cfg, kind)
        if value is not None:
            components.append((_p(_camel(kind)), value))
    for path, component in components:
        if (
            "public_network_access" in component.model_fields_set
            and component.public_network_access
        ):
            if net.mode == "private":
                out.append(
                    error(
                        "XF021",
                        "publicNetworkAccess cannot be true when network mode is 'private'",
                        f"{path}.publicNetworkAccess",
                    )
                )
            elif not cfg.security.public_network_access:
                out.append(
                    error(
                        "XF106",
                        "publicNetworkAccess is true but security.publicNetworkAccess is false; set the global intent first",
                        f"{path}.publicNetworkAccess",
                    )
                )
        if (
            "local_authentication" in component.model_fields_set
            and component.local_authentication
            and not cfg.security.local_authentication
        ):
            out.append(
                error(
                    "XF106",
                    "localAuthentication is true but security.localAuthentication is false; set the global intent first",
                    f"{path}.localAuthentication",
                )
            )
    if (
        cfg.managed_identity is not None
        and not cfg.managed_identity.enabled
        and net.mode == "private"
    ):
        out.append(
            error(
                "XF021",
                "private network mode requires a managed identity; do not disable managedIdentity",
                _p("managedIdentity", "enabled"),
            )
        )
    if net.mode == "private" and not net.private_dns and not net.existing_vnet_resource_id:
        out.append(
            warning(
                "XF021",
                "privateDns is false: private endpoints will not resolve unless you manage DNS yourself",
                _p("security", "network", "privateDns"),
            )
        )
    redis = cfg.redis
    if (
        redis is not None
        and "sku" in redis.model_fields_set
        and redis.sku not in _REDIS_SKUS[redis.service]
    ):
        out.append(
            error(
                "XF106",
                f"redis sku '{redis.sku}' is not available for service '{redis.service}' (use {', '.join(sorted(_REDIS_SKUS[redis.service]))})",
                _p("redis", "sku"),
            )
        )
    return out


def _runtime_rules(cfg: XFoundry) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    private = cfg.security.network.mode == "private"
    for path, runtime in _runtimes(cfg):
        if not runtime.enabled:
            continue
        if runtime.scale.min_replicas > runtime.scale.max_replicas:
            out.append(
                error("XF108", "scale.minReplicas cannot exceed scale.maxReplicas", f"{path}.scale")
            )
        if "memory" in runtime.resources.model_fields_set:
            memory = float(runtime.resources.memory.removesuffix("Gi"))
            if memory != runtime.resources.cpu * 2:
                out.append(
                    warning(
                        "XF108",
                        f"{runtime.resources.cpu} CPU with {runtime.resources.memory} memory is not a Consumption profile combination (memory is 2 x CPU)",
                        f"{path}.resources",
                    )
                )
        if private and runtime.ingress.external:
            out.append(
                error(
                    "XF021",
                    "ingress.external cannot be true when network mode is 'private'",
                    f"{path}.ingress.external",
                )
            )
        if runtime.registry.authentication == "credentials" and runtime.registry.mode == "managed":
            out.append(
                error(
                    "XF108",
                    "a managed registry uses managed identity authentication",
                    f"{path}.registry.authentication",
                )
            )
    return out


def _events(cfg: XFoundry) -> list[Diagnostic]:
    ev = cfg.events
    if ev is None:
        return []
    out: list[Diagnostic] = []
    by_name = {e.name: e for e in ev.entities}
    for e in ev.entities:
        path = _p("events", f"entities[{e.name}]")
        if e.type in {"queue", "topic", "subscription"} and ev.provider != "serviceBus":
            out.append(
                error(
                    "XF109",
                    f"entity type '{e.type}' requires provider 'serviceBus'",
                    f"{path}.type",
                )
            )
        if e.type == "eventSubscription" and ev.provider != "eventGrid":
            out.append(
                error(
                    "XF109",
                    "entity type 'eventSubscription' requires provider 'eventGrid'",
                    f"{path}.type",
                )
            )
        if e.type in {"subscription", "eventSubscription"}:
            parent = by_name.get(e.parent or "")
            if parent is None or (e.type == "subscription" and parent.type != "topic"):
                out.append(
                    error(
                        "XF109",
                        f"{e.type} '{e.name}' needs 'parent' naming a topic declared in events.entities",
                        f"{path}.parent",
                    )
                )
        elif e.parent:
            out.append(
                error("XF109", f"{e.type} '{e.name}' cannot have a parent", f"{path}.parent")
            )
    if ev.provider == "eventHubs" and ev.entities:
        out.append(
            error(
                "XF109",
                "event entities are not supported for provider 'eventHubs'",
                _p("events", "entities"),
            )
        )
    return out


def _cron(cfg: XFoundry) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    for _, prefix, holder in _scopes(cfg):
        for kb in _kbs(holder):
            if not _CRON.match(kb.refresh.schedule):
                out.append(
                    error(
                        "XF118",
                        f"'{kb.refresh.schedule}' is not a 5-field cron expression",
                        f"{prefix}.iq.knowledgeBases[{kb.name}].refresh.schedule",
                    )
                )
        ev = getattr(holder, "evaluation", None)
        if ev is not None and ev.schedule and not _CRON.match(ev.schedule):
            out.append(
                error(
                    "XF118",
                    f"'{ev.schedule}' is not a 5-field cron expression",
                    f"{prefix}.evaluation.schedule",
                )
            )
    return out


def _agents(cfg: XFoundry) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    for _, prefix, holder in _scopes(cfg):
        for agent in getattr(holder, "agents", []):
            out += _agent(agent, f"{prefix}.agents[{agent.name}]")
    return out


def _agent(agent: Agent, path: str) -> list[Diagnostic]:
    if agent.kind == "hosted" and not (agent.source or agent.runtime):
        return [error("XF112", f"hosted agent '{agent.name}' needs 'source' or 'runtime'", path)]
    return []


def validate_declared(cfg: XFoundry) -> list[Diagnostic]:
    """Run every rule that needs the configuration exactly as authored."""
    rules = (
        _projects_and_hub,
        _unique_names,
        _project_references,
        _model_sets,
        _secrets,
        _existing_vs_created,
        _explicit_names,
        _locations,
        _security,
        _runtime_rules,
        _events,
        _cron,
        _agents,
    )
    diagnostics: list[Diagnostic] = []
    for rule in rules:
        diagnostics += rule(cfg)
    return dedupe(diagnostics)
