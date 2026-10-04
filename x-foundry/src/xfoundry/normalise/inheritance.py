"""Inheritance engine: root < hub < project, with overrides by name.

Rules (specification section 8):

* a project's own item replaces an inherited item with the same name;
* project additions are merged with inherited items;
* model ``denied`` sets accumulate; a project ``allowed`` set replaces the inherited one
  (validation separately rejects a project that widens its hub's allowed set).
"""

from __future__ import annotations

from collections.abc import Iterable
from typing import Protocol, TypeVar

from xfoundry.schema.models import ModelConfiguration


class _Named(Protocol):
    name: str


T = TypeVar("T", bound=_Named)


def merge_named(layers: Iterable[tuple[str, Iterable[T]]]) -> tuple[list[T], dict[str, str]]:
    """Merge ``(scope, items)`` layers; later layers override earlier ones by name.

    Returns the merged list (order of first appearance) and ``name -> winning scope``.
    """
    merged: dict[str, T] = {}
    origins: dict[str, str] = {}
    for scope, items in layers:
        for item in items:
            merged[item.name] = item
            origins[item.name] = scope
    return list(merged.values()), origins


def merge_models(layers: Iterable[tuple[str, ModelConfiguration]]) -> ModelConfiguration:
    """Combine model configurations from broadest to narrowest scope."""
    layers = list(layers)
    default: str | None = None
    allowed: list[str] = []
    denied: list[str] = []
    for _, models in layers:
        if models.default is not None:
            default = models.default
        if models.allowed:
            allowed = list(models.allowed)
        denied.extend(d for d in models.denied if d not in denied)
    deployments, _ = merge_named((scope, m.deployments) for scope, m in layers)
    return ModelConfiguration(
        default=default,
        deployments=[d.model_copy(deep=True) for d in deployments],
        allowed=allowed,
        denied=denied,
    )
