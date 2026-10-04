from __future__ import annotations

import copy
from pathlib import Path
from typing import Any

import pytest

from xfoundry.diagnostics import Diagnostic, Severity
from xfoundry.plan import Analysis, analyse_mapping

EXAMPLES = Path(__file__).resolve().parent.parent / "examples"

BASE: dict[str, Any] = {
    "topology": {"mode": "standalone"},
    "security": {"roles": {"admins": ["Admins"]}},
    "projects": [{"name": "finance"}],
}


def mk(**top: Any) -> dict[str, Any]:
    """An azure.yaml mapping: the minimal valid x-foundry plus top-level overrides."""
    body = copy.deepcopy(BASE)
    for key, value in top.items():
        body[key] = copy.deepcopy(value)
    return {"name": "test", "x-foundry": body}


def hub_doc(**top: Any) -> dict[str, Any]:
    doc = mk(topology={"mode": "hub-spoke"}, hub={"name": "shared"}, **top)
    return doc


def kb(name: str = "policies", **over: Any) -> dict[str, Any]:
    base: dict[str, Any] = {
        "name": name,
        "sources": [{"name": "files", "type": "blob", "container": "policies"}],
    }
    base.update(copy.deepcopy(over))
    return base


def iq(*kbs: dict[str, Any]) -> dict[str, Any]:
    return {"knowledgeBases": list(kbs) or [kb()]}


def gateway(**over: Any) -> dict[str, Any]:
    base = {"enabled": True, "authentication": {"audiences": ["api://gw"]}}
    base.update(copy.deepcopy(over))
    return base


def analyse(doc: dict[str, Any]) -> Analysis:
    return analyse_mapping(doc)


def diagnostics(doc: dict[str, Any]) -> list[Diagnostic]:
    return analyse(doc).diagnostics


def codes(doc: dict[str, Any], severity: Severity | None = None) -> set[str]:
    return {d.code for d in diagnostics(doc) if severity is None or d.severity is severity}


@pytest.fixture
def expect():
    """``expect(doc, 'XF005', path='agents')`` asserts an error with that code (and path part)."""

    def check(doc: dict[str, Any], code: str, path: str | None = None, message: str | None = None):
        found = [d for d in diagnostics(doc) if d.code == code]
        assert found, f"expected {code}, got {[str(d) for d in diagnostics(doc)]}"
        if path is not None:
            assert any(path in d.path for d in found), (
                f"no {code} at '{path}': {[str(d) for d in found]}"
            )
        if message is not None:
            assert any(message in d.message for d in found), [str(d) for d in found]

    return check
