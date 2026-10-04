"""Pydantic v2 models for the x-foundry configuration.

The models mirror ``x-foundry.schema.json``: JSON Schema validates the document as
authored, and these models apply defaults and typed access. Cross-object rules live
in :mod:`xfoundry.validators`.
"""

from __future__ import annotations

import ipaddress
import re
import uuid
from typing import Annotated, Any, Literal
from urllib.parse import urlparse

from pydantic import (
    AfterValidator,
    BaseModel,
    ConfigDict,
    Field,
    StringConstraints,
    model_validator,
)
from pydantic.alias_generators import to_camel

# --------------------------------------------------------------------------- types


def _uri(value: str) -> str:
    parsed = urlparse(value)
    if not parsed.scheme or not parsed.netloc:
        raise ValueError(f"{value!r} is not an absolute URI")
    return value


_HOSTNAME = re.compile(
    r"^(?=.{1,253}$)[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?"
    r"(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$"
)


def _hostname(value: str) -> str:
    if not _HOSTNAME.match(value):
        raise ValueError(f"{value!r} is not a valid hostname")
    return value


def _uuid(value: str) -> str:
    try:
        uuid.UUID(value)
    except ValueError as exc:
        raise ValueError(f"{value!r} is not a UUID") from exc
    return value


def _email(value: str) -> str:
    if not re.match(r"^[^@\s]+@[^@\s]+\.[^@\s]+$", value):
        raise ValueError(f"{value!r} is not an email address")
    return value


def _ip_range(value: str) -> str:
    try:
        ipaddress.ip_network(value, strict=False)
    except ValueError as exc:
        raise ValueError(f"{value!r} is not an IP address or CIDR range") from exc
    return value


Uri = Annotated[str, AfterValidator(_uri)]
Hostname = Annotated[str, AfterValidator(_hostname)]
Uuid = Annotated[str, AfterValidator(_uuid)]
Email = Annotated[str, AfterValidator(_email)]
IpRange = Annotated[str, AfterValidator(_ip_range)]
ResourceName = Annotated[
    str,
    StringConstraints(
        min_length=2, max_length=64, pattern=r"^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,62}[a-zA-Z0-9])?$"
    ),
]
Tags = dict[str, Annotated[str, StringConstraints(max_length=256)]]
Dimension = Literal[
    "tenant",
    "user",
    "group",
    "department",
    "project",
    "agent",
    "model",
    "knowledgeBase",
    "endpoint",
    "subscription",
]
EnvValue = str | int | float | bool  # JSON Schema: string, number or boolean


class Model(BaseModel):
    """Base class: camelCase aliases, unknown keys rejected."""

    model_config = ConfigDict(
        extra="forbid",
        alias_generator=to_camel,
        populate_by_name=True,
        protected_namespaces=(),
    )


class _NameOrExisting(Model):
    """Mixin enforcing that a component is either created or an existing resource."""

    @model_validator(mode="after")
    def _name_xor_existing(self) -> _NameOrExisting:
        if getattr(self, "name", None) and getattr(self, "existing_resource_id", None):
            raise ValueError("'name' and 'existingResourceId' are mutually exclusive")
        return self


# ------------------------------------------------------------------- core / security


class Topology(Model):
    mode: Literal["standalone", "hub-spoke"] = "standalone"


class Defaults(Model):
    location: str | None = None
    resource_group: str | None = None
    naming_prefix: Annotated[str, StringConstraints(pattern=r"^[a-zA-Z0-9-]{1,20}$")] | None = None
    environment: Annotated[str, StringConstraints(pattern=r"^[a-z0-9-]{1,12}$")] = "dev"
    tags: Tags = Field(default_factory=dict)


class PrincipalObject(Model):
    type: Literal["user", "group", "servicePrincipal", "managedIdentity"]
    id: str | None = None
    name: str | None = None

    @model_validator(mode="after")
    def _id_or_name(self) -> PrincipalObject:
        if not (self.id or self.name):
            raise ValueError("a principal needs 'id' or 'name'")
        return self


Principal = Annotated[str, StringConstraints(min_length=1)] | PrincipalObject


class SecurityRoles(Model):
    admins: list[Principal]
    developers: list[Principal] = Field(default_factory=list)
    consumers: list[Principal] = Field(default_factory=list)
    operators: list[Principal] = Field(default_factory=list)


class ProjectRoles(Model):
    admins: list[Principal] = Field(default_factory=list)
    developers: list[Principal] = Field(default_factory=list)
    consumers: list[Principal] = Field(default_factory=list)
    operators: list[Principal] = Field(default_factory=list)


class NetworkSecurity(Model):
    mode: Literal["public", "restricted", "private"] = "private"
    allowed_domains: list[Hostname] = Field(default_factory=list)
    allowed_ips: list[IpRange] = Field(default_factory=list)
    private_dns: bool = True
    existing_vnet_resource_id: str | None = Field(
        default=None,
        pattern=r"^/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Network/virtualNetworks/[^/]+$",
    )
    egress: Literal["azure-default", "restricted", "private-only"] = "restricted"


class Security(Model):
    network: NetworkSecurity = Field(default_factory=NetworkSecurity)
    roles: SecurityRoles
    local_authentication: bool = False
    public_network_access: bool = False
    purge_protection: bool = True


# ----------------------------------------------------------------------- models/agents


class ModelDeployment(Model):
    name: ResourceName
    model: str = Field(min_length=1)
    version: str | None = None
    format: str = "OpenAI"
    sku: Literal[
        "GlobalStandard",
        "GlobalProvisionedManaged",
        "DataZoneStandard",
        "DataZoneProvisionedManaged",
        "Standard",
        "ProvisionedManaged",
    ] = "GlobalStandard"
    capacity: int = Field(default=10, ge=1)
    location: str | None = None
    rai_policy: str | None = None
    version_upgrade_option: Literal[
        "OnceNewDefaultVersionAvailable", "OnceCurrentVersionExpired", "NoAutoUpgrade"
    ] = "OnceNewDefaultVersionAvailable"


class ModelConfiguration(Model):
    default: str | None = None
    deployments: list[ModelDeployment] = Field(default_factory=list)
    allowed: list[str] = Field(default_factory=list)
    denied: list[str] = Field(default_factory=list)


class Tool(Model):
    name: ResourceName
    type: Literal["mcp", "knowledgeBase", "openapi", "function", "codeInterpreter", "fileSearch"]
    reference: str
    configuration: dict[str, Any] = Field(default_factory=dict)


class Toolbox(Model):
    name: ResourceName
    project: str | None = None
    description: str | None = None
    tools: list[Tool] = Field(min_length=1)


class ConnectionAuthentication(Model):
    mode: Literal["managedIdentity", "entra", "apiKey", "oauth2", "none"] = "managedIdentity"
    client_id: Uuid | None = None
    tenant_id: Uuid | None = None
    scopes: list[str] = Field(default_factory=list)
    secret_ref: str | None = None


class Mcp(Model):
    name: ResourceName
    project: str | None = None
    endpoint: Uri
    transport: Literal["streamable-http", "sse"] = "streamable-http"
    authentication: ConnectionAuthentication | None = None
    allowed_tools: list[str] = Field(default_factory=list)
    headers: dict[str, str] = Field(default_factory=dict)


class Connector(Model):
    name: ResourceName
    type: Literal[
        "azureOpenAI",
        "azureAISearch",
        "storage",
        "sharepoint",
        "graph",
        "serviceNow",
        "salesforce",
        "sap",
        "sql",
        "api",
        "custom",
    ]
    endpoint: Uri | None = None
    authentication: ConnectionAuthentication | None = None
    project: str | None = None
    shared: bool = False
    configuration: dict[str, Any] = Field(default_factory=dict)


# ---------------------------------------------------------------------------- runtime


class ContainerRegistry(Model):
    mode: Literal["managed", "existing", "external"] = "managed"
    name: ResourceName | None = None
    resource_id: str | None = None
    server: Hostname | None = None
    repository: str | None = None
    tag: str = "latest"
    authentication: Literal["managedIdentity", "credentials"] = "managedIdentity"


class RuntimeIngress(Model):
    external: bool = False
    target_port: int = Field(default=8000, ge=1, le=65535)
    transport: Literal["auto", "http", "http2", "tcp"] = "auto"
    allow_insecure: bool = False
    allowed_ips: list[IpRange] = Field(default_factory=list)


class ScaleRule(Model):
    name: ResourceName
    type: Literal["http", "cpu", "memory", "azure-servicebus", "custom"]
    metadata: dict[str, str] = Field(default_factory=dict)


class RuntimeScale(Model):
    min_replicas: int = Field(default=1, ge=0)
    max_replicas: int = Field(default=3, ge=1)
    http_concurrency: int = Field(default=50, ge=1)
    rules: list[ScaleRule] = Field(default_factory=list)


_CPU = Literal[0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 2.25, 2.5, 2.75, 3, 3.25, 3.5, 3.75, 4]


class ContainerResources(Model):
    cpu: _CPU = 1
    memory: str = Field(default="2Gi", pattern=r"^[0-9]+(?:\.[0-9]+)?Gi$")


class HealthConfiguration(Model):
    liveness_path: str = "/health/live"
    readiness_path: str = "/health/ready"
    startup_path: str = "/health/startup"
    port: int = Field(default=8000, ge=1, le=65535)


class Runtime(Model):
    enabled: bool = False
    type: Literal["containerApps"] = "containerApps"
    name: ResourceName | None = None
    project: str | None = None
    image: str | None = None
    source: str | None = None
    registry: ContainerRegistry = Field(default_factory=ContainerRegistry)
    ingress: RuntimeIngress = Field(default_factory=RuntimeIngress)
    scale: RuntimeScale = Field(default_factory=RuntimeScale)
    resources: ContainerResources = Field(default_factory=ContainerResources)
    environment: dict[str, EnvValue] = Field(default_factory=dict)
    secrets: dict[str, str] = Field(default_factory=dict)
    health: HealthConfiguration = Field(default_factory=HealthConfiguration)
    managed_identity: bool = True
    tags: Tags = Field(default_factory=dict)

    @model_validator(mode="after")
    def _image_or_source(self) -> Runtime:
        if self.enabled and not (self.image or self.source):
            raise ValueError("an enabled runtime needs 'image' or 'source'")
        return self


class Agent(Model):
    name: ResourceName
    project: str | None = None
    kind: Literal["prompt", "hosted"] = "prompt"
    model: str | None = None
    instructions: str | None = None
    source: str | None = None
    protocols: list[Literal["responses", "a2a", "invocations"]] = Field(
        default_factory=lambda: ["responses"]
    )
    toolboxes: list[str] = Field(default_factory=list)
    mcps: list[str] = Field(default_factory=list)
    knowledge_bases: list[str] = Field(default_factory=list)
    runtime: Runtime | None = None
    environment: dict[str, EnvValue] = Field(default_factory=dict)
    tags: Tags = Field(default_factory=dict)


# ------------------------------------------------------------------- search and IQ


class Search(_NameOrExisting):
    enabled: bool = True
    name: ResourceName | None = None
    existing_resource_id: str | None = None
    sku: Literal[
        "free",
        "basic",
        "standard",
        "standard2",
        "standard3",
        "storage_optimized_l1",
        "storage_optimized_l2",
    ] = "standard"
    semantic_ranking: bool = True
    local_authentication: bool = False
    public_network_access: bool = False
    managed_identity: bool = True
    replicas: int = Field(default=1, ge=1, le=12)
    partitions: int = Field(default=1, ge=1, le=12)
    tags: Tags = Field(default_factory=dict)


class KnowledgeSource(Model):
    name: ResourceName
    type: Literal["sharepoint", "blob", "adls", "search-index", "web", "sql", "custom"]
    connection: str | None = None
    site: str | None = None
    library: str | None = None
    container: str | None = None
    path: str | None = None
    index: str | None = None
    url: Uri | None = None
    database: str | None = None
    table: str | None = None
    query: str | None = None
    include: list[str] = Field(default_factory=list)
    exclude: list[str] = Field(default_factory=list)
    metadata: dict[str, str | int | float | bool] = Field(default_factory=dict)

    @model_validator(mode="after")
    def _required_by_type(self) -> KnowledgeSource:
        required = {
            "sharepoint": ("site",),
            "blob": ("container",),
            "adls": ("container",),
            "search-index": ("index",),
            "web": ("url",),
            "sql": ("database",),
        }.get(self.type, ())
        missing = [f for f in required if not getattr(self, f)]
        if missing:
            raise ValueError(f"source type '{self.type}' requires {', '.join(missing)}")
        if self.type == "sql" and not (self.table or self.query):
            raise ValueError("source type 'sql' requires 'table' or 'query'")
        return self


class Chunking(Model):
    strategy: Literal["fixed", "semantic", "document-layout", "custom"] = "fixed"
    size: int = Field(default=1024, ge=128, le=8192)
    overlap: int = Field(default=128, ge=0, le=2048)
    unit: Literal["tokens", "characters"] = "tokens"


class VectorConfiguration(Model):
    enabled: bool = True
    model: str = "text-embedding-3-large"
    deployment: str | None = None
    dimensions: int = Field(default=3072, ge=1, le=4096)
    algorithm: Literal["hnsw", "exhaustiveKnn"] = "hnsw"
    metric: Literal["cosine", "dotProduct", "euclidean"] = "cosine"
    profile: str = "default-vector-profile"
    compression: Literal["none", "scalar", "binary"] = "none"


class SemanticConfiguration(Model):
    enabled: bool = True
    name: str = "default-semantic-config"
    title_field: str = "title"
    content_fields: list[str] = Field(default_factory=lambda: ["content"])
    keyword_fields: list[str] = Field(default_factory=list)


EdmType = Literal[
    "Edm.String",
    "Edm.Boolean",
    "Edm.Int32",
    "Edm.Int64",
    "Edm.Double",
    "Edm.DateTimeOffset",
    "Edm.GeographyPoint",
    "Collection(Edm.String)",
    "Collection(Edm.Single)",
]


class IndexField(Model):
    name: str = Field(pattern=r"^[A-Za-z][A-Za-z0-9_]*$")
    source: str | None = None
    type: EdmType
    key: bool = False
    searchable: bool = False
    filterable: bool = False
    sortable: bool = False
    facetable: bool = False
    retrievable: bool = True
    hidden: bool = False
    analyser: str | None = None
    synonym_maps: list[str] = Field(default_factory=list)
    dimensions: int | None = Field(default=None, ge=1, le=4096)
    vector_profile: str | None = None

    @model_validator(mode="after")
    def _vector_needs_dims(self) -> IndexField:
        if self.type == "Collection(Edm.Single)" and not (self.dimensions and self.vector_profile):
            raise ValueError("vector fields need 'dimensions' and 'vectorProfile'")
        return self


class ScoringProfile(Model):
    name: ResourceName
    text_weights: dict[str, float] = Field(default_factory=dict)


class IndexConfiguration(Model):
    name: ResourceName | None = None
    key_field: str = "id"
    content_field: str = "content"
    title_field: str = "title"
    vector_field: str = "contentVector"
    chunking: Chunking = Field(default_factory=Chunking)
    vector: VectorConfiguration = Field(default_factory=VectorConfiguration)
    semantic: SemanticConfiguration = Field(default_factory=SemanticConfiguration)
    fields: list[IndexField] = Field(default_factory=list)
    scoring_profiles: list[ScoringProfile] = Field(default_factory=list)


class Retrieval(Model):
    mode: Literal["keyword", "vector", "hybrid"] = "hybrid"
    semantic_ranking: bool = True
    top_k: int = Field(default=10, ge=1, le=100)
    vector_weight: float = Field(default=0.7, ge=0, le=1)
    keyword_weight: float = Field(default=0.3, ge=0, le=1)
    minimum_score: float | None = Field(default=None, ge=0, le=1)
    filter_fields: list[str] = Field(default_factory=list)
    default_filter: str | None = None
    query_language: str = "en-us"


class RouteCondition(Model):
    intent: str | None = None
    project: str | None = None
    agent: str | None = None
    groups: list[str] = Field(default_factory=list)


class KnowledgeRoute(Model):
    name: ResourceName
    when: RouteCondition
    knowledge_base: str
    priority: int = Field(default=100, ge=1)


class Routing(Model):
    strategy: Literal["semantic", "keyword", "priority", "explicit", "model"] = "semantic"
    fallback: Literal["keyword", "vector", "none"] = "keyword"
    threshold: float = Field(default=0.75, ge=0, le=1)
    routes: list[KnowledgeRoute] = Field(default_factory=list)


class KnowledgeAccess(Model):
    allowed_groups: list[str] = Field(default_factory=list)
    filter_claims: dict[str, str] = Field(default_factory=dict)


class RefreshSchedule(Model):
    enabled: bool = True
    schedule: str = "0 0 * * *"
    incremental: bool = True


class KnowledgeBase(Model):
    name: ResourceName
    description: str | None = None
    project: str | None = None
    sources: list[KnowledgeSource] = Field(min_length=1)
    index: IndexConfiguration = Field(default_factory=IndexConfiguration)
    retrieval: Retrieval = Field(default_factory=Retrieval)
    routing: Routing = Field(default_factory=Routing)
    access: KnowledgeAccess = Field(default_factory=KnowledgeAccess)
    refresh: RefreshSchedule = Field(default_factory=RefreshSchedule)


class FoundryIQ(Model):
    enabled: bool = True
    project: str | None = None
    knowledge_bases: list[KnowledgeBase] = Field(min_length=1)


# ---------------------------------------------------------------------------- gateway


class GatewayAuthentication(Model):
    mode: Literal["entra"] = "entra"
    tenant_id: Uuid | None = None
    tenant_restriction: bool = True
    audiences: list[str] = Field(min_length=1)
    issuers: list[Uri] = Field(default_factory=list)
    required_claims: dict[str, str | list[str]] = Field(default_factory=dict)
    allowed_domains: list[Hostname] = Field(default_factory=list)


class GatewayAuthorisation(Model):
    default_action: Literal["allow", "deny"] = "deny"
    admins: list[Principal] = Field(default_factory=list)
    developers: list[Principal] = Field(default_factory=list)
    consumers: list[Principal] = Field(default_factory=list)
    claim_source: Literal["groups", "roles", "scp"] = "groups"


class GatewayEndpoint(Model):
    name: ResourceName
    path: str = Field(pattern=r"^/")
    methods: list[Literal["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]] = Field(
        default_factory=lambda: ["POST"]
    )
    target: str
    target_type: Literal["agent", "runtime", "model", "search", "knowledgeBase"]
    project: str | None = None
    allowed_groups: list[str] = Field(default_factory=list)
    allowed_roles: list[str] = Field(default_factory=list)
    required_scopes: list[str] = Field(default_factory=list)
    quota_profile: str | None = None
    limit_profile: str | None = None
    routing_profile: str | None = None
    token_tracking: bool = True


class GatewayModels(Model):
    default: str | None = None
    allowed: list[str] = Field(default_factory=list)
    denied: list[str] = Field(default_factory=list)
    require_deployment_alias: bool = True


class CircuitBreaker(Model):
    enabled: bool = True
    failure_threshold: int = Field(default=5, ge=1)
    recovery_seconds: int = Field(default=30, ge=1)


class GatewayBackend(Model):
    name: ResourceName
    target: str
    priority: int = Field(default=1, ge=1)
    weight: int = Field(default=100, ge=0, le=100)
    enabled: bool = True
    circuit_breaker: CircuitBreaker = Field(default_factory=CircuitBreaker)


class GatewayRouting(Model):
    strategy: Literal["priority", "round-robin", "weighted", "latency", "cost"] = "priority"
    failover: bool = True
    retry_count: int = Field(default=2, ge=0, le=10)
    retry_status_codes: list[Annotated[int, Field(ge=400, le=599)]] = Field(
        default_factory=lambda: [429, 500, 502, 503, 504]
    )
    backends: list[GatewayBackend] = Field(default_factory=list)


class TokenTracking(Model):
    enabled: bool = True
    dimensions: list[Dimension] = Field(
        default_factory=lambda: ["user", "project", "agent", "model"]
    )
    user_claim: str = "oid"
    group_claim: str = "groups"
    department_claim: str | None = None
    capture_prompt_tokens: bool = True
    capture_completion_tokens: bool = True
    capture_cached_tokens: bool = True
    retention_days: int = Field(default=365, ge=30, le=730)


class Chargeback(Model):
    enabled: bool = True
    dimensions: list[Dimension] = Field(default_factory=lambda: ["project", "user"])
    currency: str = Field(default="USD", pattern=r"^[A-Z]{3}$")
    export: list[Literal["applicationInsights", "logAnalytics", "storage", "eventHub"]] = Field(
        default_factory=lambda: ["logAnalytics"]
    )


class QuotaProfile(Model):
    name: ResourceName
    scope: Literal["user", "group", "project", "agent", "endpoint", "subscription"]
    daily_tokens: int | None = Field(default=None, ge=1)
    monthly_tokens: int | None = Field(default=None, ge=1)
    daily_requests: int | None = Field(default=None, ge=1)
    monthly_requests: int | None = Field(default=None, ge=1)
    key_claim: str | None = None
    targets: list[str] = Field(default_factory=list)

    @model_validator(mode="after")
    def _some_limit(self) -> QuotaProfile:
        if not any(
            v is not None
            for v in (
                self.daily_tokens,
                self.monthly_tokens,
                self.daily_requests,
                self.monthly_requests,
            )
        ):
            raise ValueError("a quota profile needs at least one token or request limit")
        return self


class GatewayQuotas(Model):
    profiles: list[QuotaProfile] = Field(default_factory=list)


class LimitProfile(Model):
    name: ResourceName
    scope: Literal["global", "user", "group", "project", "agent", "endpoint", "subscription"] = (
        "global"
    )
    requests: int = Field(default=100, ge=1)
    renewal_period_seconds: int = Field(default=60, ge=1)
    concurrent_requests: int = Field(default=20, ge=1)
    key_claim: str | None = None
    targets: list[str] = Field(default_factory=list)


class GatewayLimits(Model):
    profiles: list[LimitProfile] = Field(default_factory=list)


class GatewayCaching(Model):
    enabled: bool = False
    semantic: bool = False
    ttl_seconds: int = Field(default=300, ge=1)
    vary_by: list[Literal["tenant", "user", "group", "project", "agent", "model", "endpoint"]] = (
        Field(default_factory=lambda: ["model", "endpoint"])
    )


class GatewayLogging(Model):
    metadata: bool = True
    prompts: bool = False
    completions: bool = False
    headers: bool = False
    redact: list[str] = Field(default_factory=lambda: ["authorization", "api-key", "set-cookie"])
    retention_days: int = Field(default=90, ge=30, le=730)


class GatewayAlerts(Model):
    error_rate_percent: float = Field(default=5, ge=0, le=100)
    latency_ms: int = Field(default=5000, ge=1)
    quota_usage_percent: float = Field(default=80, ge=0, le=100)
    token_usage_percent: float = Field(default=80, ge=0, le=100)


class GatewayMonitoring(Model):
    track_latency: bool = True
    track_failures: bool = True
    track_token_usage: bool = True
    track_backend: bool = True
    sample_rate: float = Field(default=1, ge=0, le=1)
    alerts: GatewayAlerts = Field(default_factory=GatewayAlerts)


class Cors(Model):
    enabled: bool = False
    allowed_origins: list[Uri] = Field(default_factory=list)
    allowed_methods: list[str] = Field(default_factory=lambda: ["GET", "POST", "OPTIONS"])
    allowed_headers: list[str] = Field(default_factory=lambda: ["authorization", "content-type"])


class GatewaySecurity(Model):
    internal_only: bool = True
    block_anonymous: bool = True
    allow_ips: list[IpRange] = Field(default_factory=list)
    deny_ips: list[IpRange] = Field(default_factory=list)
    require_https: bool = True
    validate_content_type: bool = True
    max_request_bytes: int = Field(default=10485760, ge=1)
    cors: Cors = Field(default_factory=Cors)


class Gateway(Model):
    enabled: bool = False
    name: ResourceName | None = None
    mode: Literal["ai-gateway"] = "ai-gateway"
    sku: Literal["Consumption", "Developer", "BasicV2", "StandardV2", "Premium", "PremiumV2"] = (
        "StandardV2"
    )
    endpoint: Hostname | None = None
    authentication: GatewayAuthentication | None = None
    authorisation: GatewayAuthorisation = Field(default_factory=GatewayAuthorisation)
    endpoints: list[GatewayEndpoint] = Field(default_factory=list)
    models: GatewayModels = Field(default_factory=GatewayModels)
    routing: GatewayRouting = Field(default_factory=GatewayRouting)
    token_tracking: TokenTracking = Field(default_factory=TokenTracking)
    chargeback: Chargeback = Field(default_factory=Chargeback)
    quotas: GatewayQuotas = Field(default_factory=GatewayQuotas)
    limits: GatewayLimits = Field(default_factory=GatewayLimits)
    caching: GatewayCaching = Field(default_factory=GatewayCaching)
    logging: GatewayLogging = Field(default_factory=GatewayLogging)
    monitoring: GatewayMonitoring = Field(default_factory=GatewayMonitoring)
    security: GatewaySecurity = Field(default_factory=GatewaySecurity)
    tags: Tags = Field(default_factory=dict)


class ProjectGateway(Model):
    enabled: bool = True
    path: str | None = None
    allowed_groups: list[str] = Field(default_factory=list)
    quota_profile: str | None = None


# ------------------------------------------------------- platform components


StoragePurpose = Literal["knowledge", "documents", "evaluations", "telemetry", "runtime"]


class StorageContainer(Model):
    name: str = Field(pattern=r"^[a-z0-9](?:[a-z0-9-]{1,61}[a-z0-9])$")
    purpose: (
        Literal["knowledge", "documents", "evaluations", "telemetry", "runtime", "custom"] | None
    ) = None
    public_access: Literal["none", "blob", "container"] = "none"


class Storage(_NameOrExisting):
    enabled: bool = True
    name: ResourceName | None = None
    existing_resource_id: str | None = None
    sku: Literal["Standard_LRS", "Standard_ZRS", "Standard_GRS", "Standard_GZRS", "Premium_LRS"] = (
        "Standard_ZRS"
    )
    purposes: list[StoragePurpose] = Field(default_factory=lambda: ["knowledge"])
    containers: list[StorageContainer] = Field(default_factory=list)
    hierarchical_namespace: bool = False
    public_network_access: bool = False
    local_authentication: bool = False
    retention_days: int = Field(default=30, ge=1)
    tags: Tags = Field(default_factory=dict)


class Redis(_NameOrExisting):
    enabled: bool = False
    name: ResourceName | None = None
    existing_resource_id: str | None = None
    service: Literal["azure-managed-redis", "azure-cache-for-redis"] = "azure-managed-redis"
    sku: Literal[
        "memory-optimised", "balanced", "compute-optimised", "basic", "standard", "premium"
    ] = "balanced"
    capacity: int = Field(default=1, ge=1)
    tls_only: bool = True
    public_network_access: bool = False
    persistence: bool = False
    tags: Tags = Field(default_factory=dict)


class KeyVault(_NameOrExisting):
    enabled: bool = True
    name: ResourceName | None = None
    existing_resource_id: str | None = None
    sku: Literal["standard", "premium"] = "standard"
    rbac_authorisation: bool = True
    soft_delete_days: int = Field(default=90, ge=7, le=90)
    purge_protection: bool = True
    public_network_access: bool = False
    tags: Tags = Field(default_factory=dict)


class FederatedCredential(Model):
    name: ResourceName
    issuer: Uri
    subject: str
    audiences: list[str] = Field(default_factory=lambda: ["api://AzureADTokenExchange"])


class ManagedIdentity(_NameOrExisting):
    enabled: bool = True
    type: Literal["userAssigned"] = "userAssigned"
    name: ResourceName | None = None
    existing_resource_id: str | None = None
    federated_credentials: list[FederatedCredential] = Field(default_factory=list)
    tags: Tags = Field(default_factory=dict)


class Observability(Model):
    enabled: bool = True
    application_insights: bool = True
    log_analytics: bool = True
    retention_days: int = Field(default=90, ge=30, le=730)
    sampling_percentage: float = Field(default=100, ge=0, le=100)
    capture_prompts: bool = False
    capture_completions: bool = False
    dashboards: bool = True
    alerts: bool = True
    export: list[Literal["storage", "eventHub"]] = Field(default_factory=list)
    tags: Tags = Field(default_factory=dict)


class EventEntity(Model):
    name: ResourceName
    type: Literal["queue", "topic", "subscription", "eventSubscription"]
    parent: str | None = None
    max_delivery_count: int = Field(default=10, ge=1)
    dead_lettering: bool = True


class Events(Model):
    enabled: bool = False
    provider: Literal["serviceBus", "eventGrid", "eventHubs"] = "serviceBus"
    namespace: ResourceName | None = None
    entities: list[EventEntity] = Field(default_factory=list)
    public_network_access: bool = False
    tags: Tags = Field(default_factory=dict)


class EvaluationDataset(Model):
    name: ResourceName
    path: str
    format: Literal["jsonl", "json", "csv", "parquet"] = "jsonl"


class Evaluator(Model):
    name: ResourceName
    type: Literal[
        "groundedness", "relevance", "coherence", "fluency", "similarity", "safety", "custom"
    ]
    threshold: float | None = Field(default=None, ge=0, le=1)
    configuration: dict[str, Any] = Field(default_factory=dict)


class Evaluation(Model):
    enabled: bool = False
    project: str | None = None
    datasets: list[EvaluationDataset] = Field(default_factory=list)
    evaluators: list[Evaluator] = Field(default_factory=list)
    schedule: str | None = None
    retention_days: int = Field(default=90, ge=30, le=730)
    fail_threshold: float | None = Field(default=None, ge=0, le=1)


class Budgets(Model):
    monthly_amount: float | None = Field(default=None, gt=0)
    currency: str = Field(default="USD", pattern=r"^[A-Z]{3}$")
    monthly_tokens: int | None = Field(default=None, ge=1)
    thresholds: list[Annotated[float, Field(ge=0, le=100)]] = Field(
        default_factory=lambda: [50, 80, 100]
    )
    contacts: list[Email] = Field(default_factory=list)


class ModelPolicy(Model):
    allowed_models: list[str] = Field(default_factory=list)
    denied_models: list[str] = Field(default_factory=list)
    allowed_skus: list[str] = Field(default_factory=list)
    require_approved_version: bool = True


class Governance(Model):
    enabled: bool = True
    budgets: Budgets = Field(default_factory=Budgets)
    model_policy: ModelPolicy = Field(default_factory=ModelPolicy)
    resource_locks: bool = True
    required_tags: list[str] = Field(
        default_factory=lambda: ["environment", "project", "managed-by"]
    )
    policy_assignments: list[str] = Field(default_factory=list)
    data_residency: list[str] = Field(default_factory=list)


# ---------------------------------------------------------------- hub / project / root


class Inheritance(Model):
    models: bool = True
    toolboxes: bool = True
    mcps: bool = True
    iq: bool = False
    search: bool = True


class Hub(Model):
    name: ResourceName
    location: str | None = None
    resource_group: str | None = None
    models: ModelConfiguration = Field(default_factory=ModelConfiguration)
    toolboxes: list[Toolbox] = Field(default_factory=list)
    mcps: list[Mcp] = Field(default_factory=list)
    iq: FoundryIQ | None = None
    search: Search | None = None
    inheritance: Inheritance = Field(default_factory=Inheritance)
    tags: Tags = Field(default_factory=dict)


class Project(Model):
    name: ResourceName
    display_name: str | None = Field(default=None, min_length=1, max_length=128)
    description: str | None = Field(default=None, max_length=1024)
    location: str | None = None
    resource_group: str | None = None
    inherit_hub: bool = True
    roles: ProjectRoles = Field(default_factory=ProjectRoles)
    models: ModelConfiguration = Field(default_factory=ModelConfiguration)
    agents: list[Agent] = Field(default_factory=list)
    toolboxes: list[Toolbox] = Field(default_factory=list)
    mcps: list[Mcp] = Field(default_factory=list)
    iq: FoundryIQ | None = None
    search: Search | None = None
    runtime: Runtime | None = None
    gateway: ProjectGateway | None = None
    connectors: list[Connector] = Field(default_factory=list)
    evaluation: Evaluation | None = None
    tags: Tags = Field(default_factory=dict)


class XFoundry(Model):
    """Root of the ``x-foundry`` subtree."""

    schema_version: str = Field(default="1.0", pattern=r"^[0-9]+\.[0-9]+$")
    topology: Topology
    defaults: Defaults = Field(default_factory=Defaults)
    security: Security
    hub: Hub | None = None
    projects: list[Project] = Field(min_length=1)
    models: ModelConfiguration = Field(default_factory=ModelConfiguration)
    agents: list[Agent] = Field(default_factory=list)
    toolboxes: list[Toolbox] = Field(default_factory=list)
    mcps: list[Mcp] = Field(default_factory=list)
    search: Search | None = None
    iq: FoundryIQ | None = None
    runtime: Runtime | None = None
    gateway: Gateway | None = None
    storage: Storage | None = None
    redis: Redis | None = None
    key_vault: KeyVault | None = None
    managed_identity: ManagedIdentity | None = None
    observability: Observability | None = None
    connectors: list[Connector] = Field(default_factory=list)
    events: Events | None = None
    evaluation: Evaluation | None = None
    governance: Governance | None = None
    tags: Tags = Field(default_factory=dict)

    @model_validator(mode="after")
    def _hub_matches_topology(self) -> XFoundry:
        if self.topology.mode == "standalone" and self.hub is not None:
            raise ValueError("'hub' is not allowed when topology.mode is 'standalone'")
        if self.topology.mode == "hub-spoke" and self.hub is None:
            raise ValueError("'hub' is required when topology.mode is 'hub-spoke'")
        return self
