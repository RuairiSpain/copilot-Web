"""Helpers shared by the validators."""

from __future__ import annotations

from collections import Counter
from collections.abc import Callable, Iterable
from typing import TypeVar

from xfoundry.diagnostics import Diagnostic, Severity

T = TypeVar("T")

ROOT = "x-foundry"


def dedupe(diagnostics: Iterable[Diagnostic]) -> list[Diagnostic]:
    """Drop repeated diagnostics (shared items are checked once per project)."""
    seen: set[Diagnostic] = set()
    out: list[Diagnostic] = []
    for d in diagnostics:
        if d not in seen:
            seen.add(d)
            out.append(d)
    return out


def duplicates(items: Iterable[T], key: Callable[[T], str]) -> list[str]:
    counts = Counter(key(i) for i in items)
    return sorted(k for k, n in counts.items() if n > 1)


def scope_path(scope: str) -> str:
    """Config path prefix for a scope id."""
    if scope == "root":
        return ROOT
    if scope == "hub":
        return f"{ROOT}.hub"
    return f"{ROOT}.projects[{scope.split(':', 1)[1]}]"


def item_path(scope: str, collection: str, name: str, tail: str = "") -> str:
    base = scope_path(scope)
    if collection == "knowledgeBases":
        collection = "iq.knowledgeBases"
    path = f"{base}.{collection}[{name}]"
    return f"{path}.{tail}" if tail else path


def has_errors(diagnostics: Iterable[Diagnostic]) -> bool:
    return any(d.severity is Severity.ERROR for d in diagnostics)
