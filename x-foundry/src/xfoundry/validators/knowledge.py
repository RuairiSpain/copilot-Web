"""Foundry IQ / Search rules: 7-12, 117, 118 (per knowledge base, per declaring scope)."""

from __future__ import annotations

import math

from xfoundry.diagnostics import Diagnostic, error, warning
from xfoundry.schema.models import KnowledgeBase, ModelDeployment
from xfoundry.validators.common import item_path

# model -> (maximum dimensions, supports requesting fewer dimensions)
EMBEDDING_MODELS: dict[str, tuple[int, bool]] = {
    "text-embedding-3-large": (3072, True),
    "text-embedding-3-small": (1536, True),
    "text-embedding-ada-002": (1536, False),
}


def validate_knowledge_base(
    kb: KnowledgeBase,
    scope: str,
    deployments: list[ModelDeployment],
) -> list[Diagnostic]:
    out: list[Diagnostic] = []

    def at(tail: str = "") -> str:
        return item_path(scope, "knowledgeBases", kb.name, tail)

    index, vector, retrieval = kb.index, kb.index.vector, kb.retrieval
    fields = {f.name: f for f in index.fields}

    # Rule 11
    if index.chunking.overlap >= index.chunking.size:
        out.append(
            error(
                "XF011",
                f"chunking.overlap ({index.chunking.overlap}) must be less than chunking.size ({index.chunking.size})",
                at("index.chunking.overlap"),
            )
        )

    # Rule 12
    if retrieval.mode == "hybrid" and not math.isclose(
        retrieval.vector_weight + retrieval.keyword_weight, 1.0, abs_tol=1e-9
    ):
        out.append(
            error(
                "XF012",
                f"hybrid retrieval weights must sum to 1.0 (vectorWeight {retrieval.vector_weight} + keywordWeight {retrieval.keyword_weight})",
                at("retrieval"),
            )
        )

    # Rule 9: named fields exist with a usable type.
    def need(role: str, name: str, ok: bool, expectation: str) -> None:
        field = fields.get(name)
        if field is None:
            out.append(
                error(
                    "XF009", f"{role} '{name}' is not defined in index.fields", at(f"index.{role}")
                )
            )
        elif not ok:
            out.append(error("XF009", f"{role} '{name}' must {expectation}", at(f"index.{role}")))

    key = fields.get(index.key_field)
    need(
        "keyField",
        index.key_field,
        bool(key and key.key and key.type == "Edm.String"),
        "be an Edm.String with key: true",
    )
    content = fields.get(index.content_field)
    need(
        "contentField",
        index.content_field,
        bool(content and content.type == "Edm.String" and content.searchable),
        "be a searchable Edm.String",
    )
    title = fields.get(index.title_field)
    need(
        "titleField",
        index.title_field,
        bool(title and title.type == "Edm.String"),
        "be an Edm.String",
    )
    keys = [f.name for f in index.fields if f.key]
    if len(keys) > 1:
        out.append(
            error(
                "XF009",
                f"index declares several key fields ({', '.join(keys)}); exactly one is allowed",
                at("index.fields"),
            )
        )
    if index.semantic.enabled:
        for name in [
            index.semantic.title_field,
            *index.semantic.content_fields,
            *index.semantic.keyword_fields,
        ]:
            if name not in fields:
                out.append(
                    error(
                        "XF009",
                        f"semantic configuration references undefined field '{name}'",
                        at("index.semantic"),
                    )
                )

    # Rule 10 and 7: vector fields.
    for f in index.fields:
        if f.type == "Collection(Edm.Single)" and not (f.dimensions and f.vector_profile):
            out.append(
                error(
                    "XF010",
                    f"vector field '{f.name}' must declare dimensions and vectorProfile",
                    at(f"index.fields[{f.name}]"),
                )
            )
    if vector.enabled:
        vfield = fields.get(index.vector_field)
        if vfield is None:
            out.append(
                error(
                    "XF009",
                    f"vectorField '{index.vector_field}' is not defined in index.fields",
                    at("index.vectorField"),
                )
            )
        elif vfield.type != "Collection(Edm.Single)":
            out.append(
                error(
                    "XF010",
                    f"vectorField '{vfield.name}' must be Collection(Edm.Single), not {vfield.type}",
                    at("index.vectorField"),
                )
            )
        else:
            if vfield.dimensions != vector.dimensions:
                out.append(
                    error(
                        "XF007",
                        f"vector field '{vfield.name}' has {vfield.dimensions} dimensions but index.vector.dimensions is {vector.dimensions}",
                        at(f"index.fields[{vfield.name}].dimensions"),
                    )
                )
            if vfield.vector_profile != vector.profile:
                out.append(
                    error(
                        "XF010",
                        f"vector field '{vfield.name}' uses profile '{vfield.vector_profile}' but index.vector.profile is '{vector.profile}'",
                        at(f"index.fields[{vfield.name}].vectorProfile"),
                    )
                )
        out += _embedding(kb, scope, deployments)
    elif retrieval.mode in {"vector", "hybrid"}:
        out.append(
            error(
                "XF117",
                f"retrieval mode '{retrieval.mode}' requires index.vector.enabled",
                at("retrieval.mode"),
            )
        )
    if kb.routing.fallback == "vector" and not vector.enabled:
        out.append(
            error(
                "XF117",
                "routing.fallback 'vector' requires index.vector.enabled",
                at("routing.fallback"),
            )
        )

    # Rule 8
    for name in retrieval.filter_fields:
        f = fields.get(name)
        if f is None:
            out.append(
                error(
                    "XF008",
                    f"filterFields references undefined field '{name}'",
                    at("retrieval.filterFields"),
                )
            )
        elif not f.filterable:
            out.append(
                error(
                    "XF008",
                    f"filterFields references '{name}' which is not filterable: true",
                    at("retrieval.filterFields"),
                )
            )
    for claim, name in kb.access.filter_claims.items():
        f = fields.get(name)
        if f is None or not f.filterable:
            out.append(
                error(
                    "XF008",
                    f"filterClaims maps claim '{claim}' to '{name}', which must be a filterable index field",
                    at("access.filterClaims"),
                )
            )

    # Semantic ranking.
    if retrieval.semantic_ranking and not index.semantic.enabled:
        out.append(
            error(
                "XF117",
                "retrieval.semanticRanking requires index.semantic.enabled",
                at("retrieval.semanticRanking"),
            )
        )

    if kb.routing.strategy == "explicit" and not kb.routing.routes:
        out.append(
            error(
                "XF118",
                "routing strategy 'explicit' needs at least one route",
                at("routing.routes"),
            )
        )
    return out


def _embedding(
    kb: KnowledgeBase, scope: str, deployments: list[ModelDeployment]
) -> list[Diagnostic]:
    vector = kb.index.vector
    path = item_path(scope, "knowledgeBases", kb.name, "index.vector")
    name = vector.deployment
    deployment = next((d for d in deployments if d.name == name), None)
    if deployment is None:
        return [
            error("XF005", f"embedding deployment '{name}' does not exist", f"{path}.deployment")
        ]
    if deployment.model != vector.model:
        return [
            error(
                "XF007",
                f"deployment '{deployment.name}' serves '{deployment.model}' but index.vector.model is '{vector.model}'",
                f"{path}.model",
            )
        ]
    limits = EMBEDDING_MODELS.get(deployment.model)
    if limits is None:
        return [
            warning(
                "XF007",
                f"'{deployment.model}' is not a known embedding model; dimensions cannot be verified",
                f"{path}.dimensions",
            )
        ]
    maximum, flexible = limits
    if flexible and vector.dimensions > maximum:
        return [
            error(
                "XF007",
                f"{deployment.model} produces at most {maximum} dimensions, not {vector.dimensions}",
                f"{path}.dimensions",
            )
        ]
    if not flexible and vector.dimensions != maximum:
        return [
            error(
                "XF007",
                f"{deployment.model} produces exactly {maximum} dimensions, not {vector.dimensions}",
                f"{path}.dimensions",
            )
        ]
    return []
