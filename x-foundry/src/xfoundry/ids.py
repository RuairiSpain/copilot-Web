"""Stable identifiers for scopes and deployment-graph nodes.

A *scope* is where a configuration item is declared: ``root`` (top-level shared
items), ``hub`` or ``project:<name>``. Node ids are shared by the normaliser (private
endpoint targets) and the dependency graph.
"""

from __future__ import annotations

import re

ROOT_SCOPE = "root"
HUB_SCOPE = "hub"

RESOURCE_GROUP = "resource-group"
IDENTITY = "identity"
NETWORK = "network"
PRIVATE_DNS = "private-dns"
WORKSPACE = "observability"
ALERTS = "alerts"
STORAGE = "storage"
KEY_VAULT = "key-vault"
REDIS = "redis"
EVENTS = "events"
REGISTRY = "registry"
FOUNDRY = "foundry"
GATEWAY = "gateway"
GOVERNANCE = "governance"


def project_scope(name: str) -> str:
    return f"project:{name}"


def search_node(scope: str) -> str:
    return f"search:{scope}"


def project_node(scope: str) -> str:
    """Node of the Foundry project that backs ``scope`` (``hub`` or ``project:<name>``)."""
    return f"foundry-project:{scope}"


def item_node(kind: str, scope: str, name: str) -> str:
    return f"{kind}:{scope}:{name}"


def private_endpoint_node(component: str, group: str) -> str:
    return f"private-endpoint:{component}:{group}"


def slug(value: str) -> str:
    """Make ``value`` usable as a resource name (letters, digits and hyphens)."""
    cleaned = re.sub(r"[^A-Za-z0-9-]+", "-", value).strip("-")
    return cleaned or "item"
