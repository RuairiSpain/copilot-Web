"""APIM AI Gateway rules: 13, 14, 15 and consistency with tracking and observability."""

from __future__ import annotations

from xfoundry.diagnostics import Diagnostic, error, warning
from xfoundry.normalise.model import NormalisedConfig
from xfoundry.validators.common import ROOT

GW = f"{ROOT}.gateway"


def _runtime_names(norm: NormalisedConfig) -> set[str]:
    names: set[str] = set()
    for scope_id, scope in norm.scopes.items():
        if scope.runtime is not None and scope.runtime.enabled:
            default = "runtime" if scope_id == "root" else f"{scope_id.split(':', 1)[1]}-runtime"
            names.add(scope.runtime.name or default)
        for agent in scope.agents:
            if agent.runtime is not None and agent.runtime.enabled:
                names.add(agent.runtime.name or agent.name)
    return names


def _search_names(norm: NormalisedConfig) -> set[str]:
    names: set[str] = set()
    for scope in norm.scopes.values():
        search = scope.search
        if search is not None and search.enabled:
            names.add(search.name or "search")
            if search.existing_resource_id:
                names.add(search.existing_resource_id.rsplit("/", 1)[-1])
    return names


def _model_names(norm: NormalisedConfig) -> set[str]:
    names: set[str] = set()
    for scope in norm.scopes.values():
        for d in scope.models.deployments:
            names |= {d.name, d.model}
    if norm.gateway is not None:
        names |= set(norm.gateway.models.allowed)
    return names


def validate_gateway(norm: NormalisedConfig) -> list[Diagnostic]:
    gateway = norm.gateway
    out: list[Diagnostic] = []
    registered = [p for p in norm.projects if p.gateway is not None and p.gateway.enabled]
    if gateway is None or not gateway.enabled:
        for p in registered:
            out.append(
                error(
                    "XF111",
                    f"project '{p.name}' registers with the gateway, but gateway.enabled is not true",
                    f"{ROOT}.projects[{p.name}].gateway",
                )
            )
        if gateway is not None and gateway.endpoints:
            out.append(
                warning(
                    "XF111",
                    "gateway endpoints are ignored because gateway.enabled is false",
                    f"{GW}.endpoints",
                )
            )
        return out

    # Rule 13: endpoint paths (project registrations share the namespace).
    claimed: dict[str, str] = {e.path: f"endpoint '{e.name}'" for e in gateway.endpoints}
    for p in registered:
        path = p.gateway.path  # type: ignore[union-attr]
        if path in claimed:
            out.append(
                error(
                    "XF013",
                    f"project '{p.name}' gateway path '{path}' is already used by {claimed[path]}",
                    f"{ROOT}.projects[{p.name}].gateway.path",
                )
            )
        claimed.setdefault(path, f"project '{p.name}'")  # type: ignore[arg-type]

    # Rule 14: targets resolve.
    agents: dict[str, set[str]] = {}
    for p in norm.projects:
        for a in p.agents:
            agents.setdefault(a.name, set()).add(
                p.name if p.origins.get(f"agent:{a.name}") != "root" else ""
            )
    runtimes, searches, models = _runtime_names(norm), _search_names(norm), _model_names(norm)
    kbs = {p.name: {kb.name for kb in p.knowledge_bases} for p in norm.projects}
    for e in gateway.endpoints:
        path = f"{GW}.endpoints[{e.name}]"
        if e.target_type == "agent":
            owners = agents.get(e.target, set())
            if e.project:
                p = next((x for x in norm.projects if x.name == e.project), None)
                if p is None or e.target not in {a.name for a in p.agents}:
                    out.append(
                        error(
                            "XF014",
                            f"agent '{e.target}' does not exist in project '{e.project}'",
                            f"{path}.target",
                        )
                    )
            elif not owners:
                out.append(
                    error(
                        "XF014",
                        f"endpoint target agent '{e.target}' does not exist",
                        f"{path}.target",
                    )
                )
            elif len(owners - {""}) > 1 or ("" in owners and len(owners) > 1):
                out.append(
                    error(
                        "XF014",
                        f"agent '{e.target}' exists in several projects; set endpoint.project",
                        f"{path}.target",
                    )
                )
        elif e.target_type == "runtime" and e.target not in runtimes:
            out.append(
                error(
                    "XF014",
                    f"endpoint target runtime '{e.target}' does not exist or is not enabled",
                    f"{path}.target",
                )
            )
        elif e.target_type == "model" and e.target not in models:
            out.append(
                error(
                    "XF014",
                    f"endpoint target model '{e.target}' has no deployment",
                    f"{path}.target",
                )
            )
        elif e.target_type == "search" and e.target not in searches:
            out.append(
                error(
                    "XF014",
                    f"endpoint target search '{e.target}' does not exist or is not enabled",
                    f"{path}.target",
                )
            )
        elif e.target_type == "knowledgeBase":
            scope = [kbs[e.project]] if e.project in kbs else list(kbs.values())
            if not any(e.target in names for names in scope):
                out.append(
                    error(
                        "XF014",
                        f"endpoint target knowledge base '{e.target}' does not exist",
                        f"{path}.target",
                    )
                )

    # Rule 15: profile references.
    quotas = {q.name for q in gateway.quotas.profiles}
    limits = {x.name for x in gateway.limits.profiles}
    backends = {b.name for b in gateway.routing.backends}
    for e in gateway.endpoints:
        path = f"{GW}.endpoints[{e.name}]"
        for ref, known, label, field in (
            (e.quota_profile, quotas, "quota", "quotaProfile"),
            (e.limit_profile, limits, "limit", "limitProfile"),
            (e.routing_profile, backends, "routing (backend)", "routingProfile"),
        ):
            if ref is not None and ref not in known:
                out.append(
                    error(
                        "XF015",
                        f"{label} profile '{ref}' is not defined in the gateway",
                        f"{path}.{field}",
                    )
                )
    for p in registered:
        ref = p.gateway.quota_profile  # type: ignore[union-attr]
        if ref is not None and ref not in quotas:
            out.append(
                error(
                    "XF015",
                    f"quota profile '{ref}' is not defined in the gateway",
                    f"{ROOT}.projects[{p.name}].gateway.quotaProfile",
                )
            )

    # Tracking, chargeback and the telemetry sink.
    tracking = gateway.token_tracking
    if gateway.chargeback.enabled and not tracking.enabled:
        out.append(error("XF111", "chargeback requires tokenTracking.enabled", f"{GW}.chargeback"))
    for e in gateway.endpoints:
        if e.token_tracking and not tracking.enabled and "token_tracking" in e.model_fields_set:
            out.append(
                error(
                    "XF111",
                    "endpoint enables token tracking but gateway tokenTracking is disabled",
                    f"{GW}.endpoints[{e.name}].tokenTracking",
                )
            )
    obs = norm.observability
    if tracking.enabled and not (obs and obs.enabled and obs.log_analytics):
        out.append(
            error(
                "XF111",
                "token tracking needs observability with logAnalytics enabled",
                f"{ROOT}.observability",
            )
        )
    models = gateway.models
    if models.allowed and models.default and models.default not in models.allowed:
        out.append(
            error(
                "XF006",
                f"gateway default model '{models.default}' is not in gateway.models.allowed",
                f"{GW}.models.default",
            )
        )
    if models.default and models.default in models.denied:
        out.append(
            error(
                "XF006",
                f"gateway default model '{models.default}' is denied",
                f"{GW}.models.default",
            )
        )
    missing = sorted(m for m in models.allowed if m not in _model_names_without_gateway(norm))
    for m in missing:
        out.append(
            error(
                "XF005",
                f"gateway allows model '{m}' but no deployment serves it",
                f"{GW}.models.allowed",
            )
        )
    if norm.network.mode == "private" and not gateway.security.internal_only:
        out.append(
            error(
                "XF021",
                "gateway.security.internalOnly cannot be false when network mode is 'private'",
                f"{GW}.security.internalOnly",
            )
        )
    return out


def _model_names_without_gateway(norm: NormalisedConfig) -> set[str]:
    names: set[str] = set()
    for scope in norm.scopes.values():
        for d in scope.models.deployments:
            names |= {d.name, d.model}
    return names
