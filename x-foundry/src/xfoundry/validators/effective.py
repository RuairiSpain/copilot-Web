"""Validation of the normalised (effective) configuration."""

from __future__ import annotations

from xfoundry.diagnostics import Diagnostic, error
from xfoundry.normalise.inheritance import merge_models, merge_named
from xfoundry.normalise.model import EffectiveProject, NormalisedConfig
from xfoundry.schema.models import ModelConfiguration, ModelDeployment, XFoundry
from xfoundry.validators.common import ROOT, item_path, scope_path
from xfoundry.validators.gateway import validate_gateway
from xfoundry.validators.knowledge import validate_knowledge_base


def _chain(norm: NormalisedConfig, scope: str) -> list[str]:
    """Scopes (broadest first) whose models apply to ``scope``."""
    if scope == "root":
        return ["root"]
    if scope == "hub":
        return ["root", "hub"]
    project = norm.project(scope.split(":", 1)[1])
    chain = ["root"]
    if project.inherits_hub and norm.hub is not None and norm.hub.inheritance.models:
        chain.append("hub")
    return [*chain, scope]


def scope_models(norm: NormalisedConfig, scope: str) -> ModelConfiguration:
    return merge_models((s, norm.scopes[s].models) for s in _chain(norm, scope))


def scope_deployments(norm: NormalisedConfig, scope: str) -> list[ModelDeployment]:
    merged, _ = merge_named((s, norm.scopes[s].models.deployments) for s in _chain(norm, scope))
    return merged


def _model_known(models: ModelConfiguration, name: str) -> bool:
    return name in models.allowed or any(name in {d.name, d.model} for d in models.deployments)


def _project_rules(norm: NormalisedConfig, project: EffectiveProject) -> list[Diagnostic]:
    out: list[Diagnostic] = []
    base = f"{ROOT}.projects[{project.name}]"
    models = project.models
    toolboxes = {t.name for t in project.toolboxes}
    mcps = {m.name for m in project.mcps}
    kbs = {k.name for k in project.knowledge_bases}
    connectors = {c.name for c in project.connectors}
    agents = {a.name for a in project.agents}

    # Rule 6
    if models.default is not None:
        default = models.default
        if not _model_known(models, default):
            out.append(
                error(
                    "XF006",
                    f"models.default '{default}' is neither a model deployment nor in models.allowed",
                    f"{base}.models.default",
                )
            )
        else:
            served = next((d.model for d in models.deployments if d.name == default), default)
            if default in models.denied or served in models.denied:
                out.append(
                    error(
                        "XF006", f"models.default '{default}' is denied", f"{base}.models.default"
                    )
                )
            elif models.allowed and default not in models.allowed and served not in models.allowed:
                out.append(
                    error(
                        "XF006",
                        f"models.default '{default}' is not in models.allowed",
                        f"{base}.models.default",
                    )
                )

    for agent in project.agents:
        scope = project.origins.get(f"agent:{agent.name}", f"project:{project.name}")
        path = item_path(scope, "agents", agent.name)
        label = f" (project '{project.name}')" if scope == "root" else ""
        model = agent.model or models.default
        if agent.kind == "prompt" and model is None:
            out.append(
                error(
                    "XF112",
                    f"prompt agent '{agent.name}' has no model and there is no models.default{label}",
                    path,
                )
            )
        elif model is not None and not _model_known(models, model):
            out.append(
                error(
                    "XF005",
                    f"agent '{agent.name}' references unknown model '{model}'{label}",
                    f"{path}.model",
                )
            )
        elif model is not None and (
            model in models.denied
            or next((d.model for d in models.deployments if d.name == model), model)
            in models.denied
        ):
            out.append(
                error(
                    "XF016",
                    f"agent '{agent.name}' uses denied model '{model}'{label}",
                    f"{path}.model",
                )
            )
        for ref, known, kind in (
            (agent.toolboxes, toolboxes, "toolbox"),
            (agent.mcps, mcps, "MCP"),
            (agent.knowledge_bases, kbs, "knowledge base"),
        ):
            for name in ref:
                if name not in known:
                    out.append(
                        error(
                            "XF005",
                            f"agent '{agent.name}' references unknown {kind} '{name}'{label}",
                            path,
                        )
                    )

    for toolbox in project.toolboxes:
        scope = project.origins.get(f"toolbox:{toolbox.name}", f"project:{project.name}")
        for tool in toolbox.tools:
            known = {"mcp": mcps, "knowledgeBase": kbs}.get(tool.type)
            if known is not None and tool.reference not in known:
                kind = "MCP" if tool.type == "mcp" else "knowledge base"
                out.append(
                    error(
                        "XF005",
                        f"tool '{tool.name}' references unknown {kind} '{tool.reference}' (project '{project.name}')",
                        f"{item_path(scope, 'toolboxes', toolbox.name)}.tools[{tool.name}]",
                    )
                )

    for kb in project.knowledge_bases:
        scope = project.origins.get(f"knowledgeBase:{kb.name}", f"project:{project.name}")
        for source in kb.sources:
            if source.connection and source.connection not in connectors:
                out.append(
                    error(
                        "XF005",
                        f"source '{source.name}' references unknown connector '{source.connection}' (project '{project.name}')",
                        item_path(
                            scope, "knowledgeBases", kb.name, f"sources[{source.name}].connection"
                        ),
                    )
                )
        for route in kb.routing.routes:
            path = item_path(scope, "knowledgeBases", kb.name, f"routing.routes[{route.name}]")
            if route.knowledge_base not in kbs:
                out.append(
                    error(
                        "XF005",
                        f"route '{route.name}' targets unknown knowledge base '{route.knowledge_base}' (project '{project.name}')",
                        f"{path}.knowledgeBase",
                    )
                )
            if route.when.agent and route.when.agent not in agents:
                out.append(
                    error(
                        "XF005",
                        f"route '{route.name}' matches unknown agent '{route.when.agent}' (project '{project.name}')",
                        f"{path}.when.agent",
                    )
                )
        if kb.retrieval.semantic_ranking and project.search_scope is not None:
            search = norm.scopes[project.search_scope].search
            if search is not None and not search.semantic_ranking:
                out.append(
                    error(
                        "XF117",
                        f"retrieval.semanticRanking needs semanticRanking on the Search service in scope '{project.search_scope}'",
                        item_path(scope, "knowledgeBases", kb.name, "retrieval.semanticRanking"),
                    )
                )
    return out


def _model_policy(norm: NormalisedConfig) -> list[Diagnostic]:
    """Rules 16 and 113: declared deployments respect allowed/denied sets and policy."""
    out: list[Diagnostic] = []
    policy = norm.governance.model_policy if norm.governance and norm.governance.enabled else None
    for scope_id, scope in norm.scopes.items():
        effective = scope_models(norm, scope_id)
        for d in scope.models.deployments:
            path = f"{scope_path(scope_id)}.models.deployments[{d.name}]"
            if d.model in effective.denied or d.name in effective.denied:
                out.append(
                    error("XF016", f"deployment '{d.name}' uses denied model '{d.model}'", path)
                )
            embedding = d.model.startswith("text-embedding")
            if (
                effective.allowed
                and not embedding
                and not ({d.model, d.name} & set(effective.allowed))
            ):
                out.append(
                    error(
                        "XF016",
                        f"deployment '{d.name}' uses model '{d.model}' which is not in models.allowed",
                        path,
                    )
                )
            if policy is None:
                continue
            if policy.denied_models and d.model in policy.denied_models:
                out.append(error("XF113", f"governance.modelPolicy denies model '{d.model}'", path))
            if policy.allowed_models and d.model not in policy.allowed_models:
                out.append(
                    error(
                        "XF113",
                        f"model '{d.model}' is not in governance.modelPolicy.allowedModels",
                        path,
                    )
                )
            if policy.allowed_skus and d.sku not in policy.allowed_skus:
                out.append(
                    error(
                        "XF113",
                        f"SKU '{d.sku}' is not in governance.modelPolicy.allowedSkus",
                        f"{path}.sku",
                    )
                )
    if policy and policy.allowed_models and norm.gateway and norm.gateway.enabled:
        for m in norm.gateway.models.allowed:
            if m not in policy.allowed_models:
                out.append(
                    error(
                        "XF113",
                        f"gateway allows model '{m}' which is not in governance.modelPolicy.allowedModels",
                        f"{ROOT}.gateway.models.allowed",
                    )
                )
    return out


def _widening(declared: XFoundry) -> list[Diagnostic]:
    """A spoke may narrow, but not widen, the allowed models it inherits."""
    out: list[Diagnostic] = []
    for p in declared.projects:
        if not p.models.allowed:
            continue
        inherits = declared.hub is not None and p.inherit_hub
        parent = (
            declared.hub.models.allowed
            if inherits and declared.hub.models.allowed
            else declared.models.allowed  # type: ignore[union-attr]
        )
        wider = sorted(set(p.models.allowed) - set(parent))
        if parent and wider:
            out.append(
                error(
                    "XF115",
                    f"project '{p.name}' allows {', '.join(wider)}, which its parent scope does not allow; projects may only narrow inherited allowed models",
                    f"{ROOT}.projects[{p.name}].models.allowed",
                )
            )
    return out


def _tags(norm: NormalisedConfig) -> list[Diagnostic]:
    if norm.governance is None or not norm.governance.enabled:
        return []
    out = []
    for p in norm.projects:
        missing = [t for t in norm.governance.required_tags if t not in p.tags]
        for tag in missing:
            out.append(
                error(
                    "XF104",
                    f"required tag '{tag}' is missing for project '{p.name}'; add it under tags or defaults.tags",
                    f"{ROOT}.projects[{p.name}].tags",
                )
            )
    return out


def validate_effective(norm: NormalisedConfig, declared: XFoundry) -> list[Diagnostic]:
    """Run every rule that needs inheritance and implicit resources resolved."""
    from xfoundry.validators.common import dedupe

    out: list[Diagnostic] = []
    for project in norm.projects:
        out += _project_rules(norm, project)
    for scope_id, scope in norm.scopes.items():
        deployments = scope_deployments(norm, scope_id)
        for kb in scope.knowledge_bases:
            out += validate_knowledge_base(kb, scope_id, deployments)
    if norm.storage is not None:
        adls = any(
            s.type == "adls"
            for sc in norm.scopes.values()
            for kb in sc.knowledge_bases
            for s in kb.sources
        )
        if (
            adls
            and not norm.storage.hierarchical_namespace
            and not norm.storage.existing_resource_id
        ):
            out.append(
                error(
                    "XF107",
                    "an adls knowledge source needs storage.hierarchicalNamespace",
                    f"{ROOT}.storage.hierarchicalNamespace",
                )
            )
    out += _model_policy(norm)
    out += _widening(declared)
    out += validate_gateway(norm)
    out += _tags(norm)
    return dedupe(out)
