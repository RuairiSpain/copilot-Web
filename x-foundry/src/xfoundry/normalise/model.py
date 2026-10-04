"""Output types of the normalisation engine."""

from __future__ import annotations

import re
from typing import Literal

from pydantic import Field

from xfoundry.schema.models import (
    Agent,
    Connector,
    Evaluation,
    Events,
    Gateway,
    Governance,
    Inheritance,
    KeyVault,
    KnowledgeBase,
    ManagedIdentity,
    Mcp,
    Model,
    ModelConfiguration,
    Observability,
    ProjectGateway,
    Redis,
    Runtime,
    Search,
    Storage,
    Tags,
    Toolbox,
)

_GUID = re.compile(r"^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$")


def is_guid(value: str) -> bool:
    return bool(_GUID.match(value))


class ResolvedPrincipal(Model):
    type: Literal["user", "group", "servicePrincipal", "managedIdentity"]
    id: str | None = None
    name: str | None = None

    @property
    def key(self) -> str:
        return (self.id or self.name or "").lower()


class ResolvedRoles(Model):
    admins: list[ResolvedPrincipal] = Field(default_factory=list)
    developers: list[ResolvedPrincipal] = Field(default_factory=list)
    consumers: list[ResolvedPrincipal] = Field(default_factory=list)
    operators: list[ResolvedPrincipal] = Field(default_factory=list)


class ScopeResources(Model):
    """Resources declared in one scope (``root``, ``hub`` or ``project:<name>``)."""

    scope: str
    models: ModelConfiguration = Field(default_factory=ModelConfiguration)
    agents: list[Agent] = Field(default_factory=list)
    toolboxes: list[Toolbox] = Field(default_factory=list)
    mcps: list[Mcp] = Field(default_factory=list)
    connectors: list[Connector] = Field(default_factory=list)
    knowledge_bases: list[KnowledgeBase] = Field(default_factory=list)
    search: Search | None = None
    runtime: Runtime | None = None
    evaluation: Evaluation | None = None


class ImplicitResource(Model):
    """A resource the normaliser derived because another setting requires it."""

    kind: str
    name: str
    scope: str
    reason: str


class PrivateEndpoint(Model):
    component: str
    group: str


class ResolvedNetwork(Model):
    mode: Literal["public", "restricted", "private"]
    vnet: Literal["create", "existing"] | None = None
    existing_vnet_resource_id: str | None = None
    private_dns: bool = False
    allowed_ips: list[str] = Field(default_factory=list)
    private_endpoints: list[PrivateEndpoint] = Field(default_factory=list)
    private_dns_zones: list[str] = Field(default_factory=list)


class EffectiveProject(Model):
    """A project's resources after inheritance (root < hub < project)."""

    name: str
    display_name: str
    description: str | None = None
    location: str | None = None
    resource_group: str | None = None
    inherits_hub: bool
    roles: ResolvedRoles
    tags: Tags = Field(default_factory=dict)
    models: ModelConfiguration = Field(default_factory=ModelConfiguration)
    agents: list[Agent] = Field(default_factory=list)
    toolboxes: list[Toolbox] = Field(default_factory=list)
    mcps: list[Mcp] = Field(default_factory=list)
    connectors: list[Connector] = Field(default_factory=list)
    knowledge_bases: list[KnowledgeBase] = Field(default_factory=list)
    search_scope: str | None = None
    runtime: Runtime | None = None
    runtime_scope: str | None = None
    evaluation: Evaluation | None = None
    gateway: ProjectGateway | None = None
    origins: dict[str, str] = Field(default_factory=dict)


class HubView(Model):
    name: str
    location: str | None = None
    resource_group: str | None = None
    inheritance: Inheritance
    search_scope: str | None = None
    tags: Tags = Field(default_factory=dict)


class NormalisedConfig(Model):
    """The fully resolved configuration used for planning."""

    schema_version: str
    topology_mode: Literal["standalone", "hub-spoke"]
    naming_prefix: str | None = None
    environment: str
    location: str | None = None
    resource_group: str | None = None
    tags: Tags = Field(default_factory=dict)
    roles: ResolvedRoles
    network: ResolvedNetwork
    local_authentication: bool = False
    purge_protection: bool = True
    hub: HubView | None = None
    scopes: dict[str, ScopeResources] = Field(default_factory=dict)
    projects: list[EffectiveProject] = Field(default_factory=list)
    gateway: Gateway | None = None
    storage: Storage | None = None
    redis: Redis | None = None
    key_vault: KeyVault | None = None
    managed_identity: ManagedIdentity | None = None
    observability: Observability | None = None
    events: Events | None = None
    governance: Governance | None = None
    implicit: list[ImplicitResource] = Field(default_factory=list)

    def project(self, name: str) -> EffectiveProject:
        for project in self.projects:
            if project.name == name:
                return project
        raise KeyError(name)
