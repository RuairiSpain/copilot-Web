// Package config holds the typed x-foundry configuration. The structs mirror
// schemas/x-foundry.schema.json: JSON Schema validates the document as authored, then
// Decode fills defaults (from `default` struct tags) and records which keys the author set.
package config

// Tracked records which JSON keys were present in the source document. Embed it in a
// struct to be able to tell an explicit false/0/"" from an absent value.
type Tracked struct {
	Set map[string]bool `json:"-"`
}

// Has reports whether the author wrote the given JSON key.
func (t Tracked) Has(key string) bool { return t.Set[key] }

// Tags are Azure resource tags.
type Tags map[string]string

// Principal is an Entra user, group, service principal or managed identity. A plain
// string in the document is a group display name or object ID.
type Principal struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// ---------------------------------------------------------------- root and security

// XFoundry is the root of the x-foundry subtree.
type XFoundry struct {
	Tracked
	SchemaVersion   string             `json:"schemaVersion" default:"1.0"`
	Topology        Topology           `json:"topology"`
	Defaults        Defaults           `json:"defaults"`
	Security        Security           `json:"security"`
	Hub             *Hub               `json:"hub,omitempty"`
	Projects        []Project          `json:"projects"`
	Models          ModelConfiguration `json:"models"`
	Agents          []Agent            `json:"agents,omitempty"`
	Toolboxes       []Toolbox          `json:"toolboxes,omitempty"`
	Mcps            []Mcp              `json:"mcps,omitempty"`
	Search          *Search            `json:"search,omitempty"`
	IQ              *FoundryIQ         `json:"iq,omitempty"`
	Gateway         *Gateway           `json:"gateway,omitempty"`
	Storage         *Storage           `json:"storage,omitempty"`
	KeyVault        *KeyVault          `json:"keyVault,omitempty"`
	Cosmos          *Cosmos            `json:"cosmos,omitempty"`
	AgentService    AgentService       `json:"agentService"`
	ManagedIdentity *ManagedIdentity   `json:"managedIdentity,omitempty"`
	Observability   *Observability     `json:"observability,omitempty"`
	Connectors      []Connector        `json:"connectors,omitempty"`
	Evaluation      *Evaluation        `json:"evaluation,omitempty"`
	Governance      *Governance        `json:"governance,omitempty"`
	Tags            Tags               `json:"tags,omitempty"`
}

// Topology selects standalone or hub-spoke.
type Topology struct {
	Mode string `json:"mode" default:"standalone"`
}

// Defaults are values applied unless a project or component overrides them.
type Defaults struct {
	Location      string `json:"location,omitempty"`
	ResourceGroup string `json:"resourceGroup,omitempty"`
	NamingPrefix  string `json:"namingPrefix,omitempty"`
	Environment   string `json:"environment" default:"dev"` // dev | test | prod
	Tags          Tags   `json:"tags,omitempty"`
}

// Security is the intent-based security policy.
type Security struct {
	Network             NetworkSecurity `json:"network"`
	Roles               SecurityRoles   `json:"roles"`
	LocalAuthentication bool            `json:"localAuthentication"`
	PublicNetworkAccess bool            `json:"publicNetworkAccess"`
	PurgeProtection     bool            `json:"purgeProtection" default:"true"`
}

// NetworkSecurity is high-level network intent.
type NetworkSecurity struct {
	Tracked
	Mode                                    string   `json:"mode"`
	ModeSource                              string   `json:"-"` // explicit | default | vnet-settings
	AllowedDomains                          []string `json:"allowedDomains,omitempty"`
	AllowedIPs                              []string `json:"allowedIps,omitempty"`
	PrivateDNS                              bool     `json:"privateDns" default:"true"`
	ExistingVnetResourceID                  string   `json:"existingVnetResourceId,omitempty"`
	ExistingAgentSubnetResourceID           string   `json:"existingAgentSubnetResourceId,omitempty"`
	ExistingPrivateEndpointSubnetResourceID string   `json:"existingPrivateEndpointSubnetResourceId,omitempty"`
	AddressSpace                            string   `json:"addressSpace,omitempty"`
	AgentSubnetPrefixLength                 int      `json:"agentSubnetPrefixLength" default:"24"`
	Egress                                  string   `json:"egress" default:"restricted"`
}

// SecurityRoles are logical principal assignments (admins are required by the schema).
type SecurityRoles struct {
	Admins     []Principal `json:"admins"`
	Developers []Principal `json:"developers,omitempty"`
	Consumers  []Principal `json:"consumers,omitempty"`
	Operators  []Principal `json:"operators,omitempty"`
}

// ------------------------------------------------------------------ models, agents

// ModelDeployment is one Foundry model deployment.
type ModelDeployment struct {
	Name                 string `json:"name"`
	Model                string `json:"model"`
	Version              string `json:"version,omitempty"`
	Format               string `json:"format" default:"OpenAI"`
	SKU                  string `json:"sku" default:"GlobalStandard"`
	Capacity             int    `json:"capacity" default:"10"`
	Location             string `json:"location,omitempty"`
	RaiPolicy            string `json:"raiPolicy,omitempty"`
	VersionUpgradeOption string `json:"versionUpgradeOption" default:"NoAutoUpgrade"`
}

// GetName returns the deployment name.
func (d ModelDeployment) GetName() string { return d.Name }

// ModelConfiguration is deployment and model-governance configuration.
type ModelConfiguration struct {
	Default     string            `json:"default,omitempty"`
	Deployments []ModelDeployment `json:"deployments,omitempty"`
	Allowed     []string          `json:"allowed,omitempty"`
	Denied      []string          `json:"denied,omitempty"`
}

// Tool is one tool in a toolbox.
type Tool struct {
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Reference     string         `json:"reference"`
	Configuration map[string]any `json:"configuration,omitempty"`
}

// GetName returns the tool name.
func (t Tool) GetName() string { return t.Name }

// Toolbox groups tools.
type Toolbox struct {
	Name        string `json:"name"`
	Project     string `json:"project,omitempty"`
	Description string `json:"description,omitempty"`
	Tools       []Tool `json:"tools"`
}

// GetName returns the toolbox name.
func (t Toolbox) GetName() string { return t.Name }

// ConnectionAuthentication describes how a connection authenticates.
type ConnectionAuthentication struct {
	Mode      string   `json:"mode" default:"managedIdentity"`
	ClientID  string   `json:"clientId,omitempty"`
	TenantID  string   `json:"tenantId,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	SecretRef string   `json:"secretRef,omitempty"`
}

// Mcp is a Model Context Protocol server.
type Mcp struct {
	Name           string                    `json:"name"`
	Project        string                    `json:"project,omitempty"`
	Endpoint       string                    `json:"endpoint"`
	Transport      string                    `json:"transport" default:"streamable-http"`
	Authentication *ConnectionAuthentication `json:"authentication,omitempty"`
	AllowedTools   []string                  `json:"allowedTools,omitempty"`
	Headers        map[string]string         `json:"headers,omitempty"`
}

// GetName returns the MCP name.
func (m Mcp) GetName() string { return m.Name }

// Connector is an external connection.
type Connector struct {
	Name           string                    `json:"name"`
	Type           string                    `json:"type"`
	Endpoint       string                    `json:"endpoint,omitempty"`
	Authentication *ConnectionAuthentication `json:"authentication,omitempty"`
	Project        string                    `json:"project,omitempty"`
	Shared         bool                      `json:"shared"`
	Configuration  map[string]any            `json:"configuration,omitempty"`
}

// GetName returns the connector name.
func (c Connector) GetName() string { return c.Name }

// Agent is a Foundry agent.
type Agent struct {
	Name           string         `json:"name"`
	Project        string         `json:"project,omitempty"`
	Kind           string         `json:"kind" default:"prompt"`
	Model          string         `json:"model,omitempty"`
	Instructions   string         `json:"instructions,omitempty"`
	Source         string         `json:"source,omitempty"`
	Protocols      []string       `json:"protocols" default:"responses"`
	Toolboxes      []string       `json:"toolboxes,omitempty"`
	Mcps           []string       `json:"mcps,omitempty"`
	KnowledgeBases []string       `json:"knowledgeBases,omitempty"`
	Environment    map[string]any `json:"environment,omitempty"`
	Tags           Tags           `json:"tags,omitempty"`
}

// GetName returns the agent name.
func (a Agent) GetName() string { return a.Name }

// ------------------------------------------------------------------ search and IQ

// Search is the Azure AI Search service intent.
type Search struct {
	Tracked
	Enabled             bool   `json:"enabled" default:"true"`
	Name                string `json:"name,omitempty"`
	ExistingResourceID  string `json:"existingResourceId,omitempty"`
	SKU                 string `json:"sku" default:"standard"`
	SemanticRanking     bool   `json:"semanticRanking" default:"true"`
	LocalAuthentication bool   `json:"localAuthentication"`
	PublicNetworkAccess bool   `json:"publicNetworkAccess"`
	ManagedIdentity     bool   `json:"managedIdentity" default:"true"`
	Replicas            int    `json:"replicas" default:"1"`
	Partitions          int    `json:"partitions" default:"1"`
	Tags                Tags   `json:"tags,omitempty"`
}

// KnowledgeSource is one source of a knowledge base.
type KnowledgeSource struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Connection string         `json:"connection,omitempty"`
	Site       string         `json:"site,omitempty"`
	Library    string         `json:"library,omitempty"`
	Container  string         `json:"container,omitempty"`
	Path       string         `json:"path,omitempty"`
	Index      string         `json:"index,omitempty"`
	URL        string         `json:"url,omitempty"`
	Database   string         `json:"database,omitempty"`
	Table      string         `json:"table,omitempty"`
	Query      string         `json:"query,omitempty"`
	Include    []string       `json:"include,omitempty"`
	Exclude    []string       `json:"exclude,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// GetName returns the source name.
func (s KnowledgeSource) GetName() string { return s.Name }

// Chunking configures document chunking.
type Chunking struct {
	Strategy string `json:"strategy" default:"fixed"`
	Size     int    `json:"size" default:"1024"`
	Overlap  int    `json:"overlap" default:"128"`
	Unit     string `json:"unit" default:"tokens"`
}

// VectorConfiguration configures vector search.
type VectorConfiguration struct {
	Enabled     bool   `json:"enabled" default:"true"`
	Model       string `json:"model" default:"text-embedding-3-large"`
	Deployment  string `json:"deployment,omitempty"`
	Dimensions  int    `json:"dimensions" default:"3072"`
	Algorithm   string `json:"algorithm" default:"hnsw"`
	Metric      string `json:"metric" default:"cosine"`
	Profile     string `json:"profile" default:"default-vector-profile"`
	Compression string `json:"compression" default:"none"`
}

// SemanticConfiguration configures semantic ranking.
type SemanticConfiguration struct {
	Tracked
	Enabled       bool     `json:"enabled" default:"true"`
	Name          string   `json:"name" default:"default-semantic-config"`
	TitleField    string   `json:"titleField" default:"title"`
	ContentFields []string `json:"contentFields" default:"content"`
	KeywordFields []string `json:"keywordFields,omitempty"`
}

// IndexField is a search index field.
type IndexField struct {
	Name          string   `json:"name"`
	Source        string   `json:"source,omitempty"`
	Type          string   `json:"type"`
	Key           bool     `json:"key"`
	Searchable    bool     `json:"searchable"`
	Filterable    bool     `json:"filterable"`
	Sortable      bool     `json:"sortable"`
	Facetable     bool     `json:"facetable"`
	Retrievable   bool     `json:"retrievable" default:"true"`
	Hidden        bool     `json:"hidden"`
	Analyser      string   `json:"analyser,omitempty"`
	SynonymMaps   []string `json:"synonymMaps,omitempty"`
	Dimensions    int      `json:"dimensions,omitempty"`
	VectorProfile string   `json:"vectorProfile,omitempty"`
}

// GetName returns the field name.
func (f IndexField) GetName() string { return f.Name }

// ScoringProfile is a scoring profile.
type ScoringProfile struct {
	Name        string             `json:"name"`
	TextWeights map[string]float64 `json:"textWeights,omitempty"`
}

// GetName returns the profile name.
func (s ScoringProfile) GetName() string { return s.Name }

// IndexConfiguration configures the index.
type IndexConfiguration struct {
	Name            string                `json:"name,omitempty"`
	KeyField        string                `json:"keyField" default:"id"`
	ContentField    string                `json:"contentField" default:"content"`
	TitleField      string                `json:"titleField" default:"title"`
	VectorField     string                `json:"vectorField" default:"contentVector"`
	Chunking        Chunking              `json:"chunking"`
	Vector          VectorConfiguration   `json:"vector"`
	Semantic        SemanticConfiguration `json:"semantic"`
	Fields          []IndexField          `json:"fields,omitempty"`
	ScoringProfiles []ScoringProfile      `json:"scoringProfiles,omitempty"`
}

// Retrieval configures retrieval.
type Retrieval struct {
	Mode            string   `json:"mode" default:"hybrid"`
	SemanticRanking bool     `json:"semanticRanking" default:"true"`
	TopK            int      `json:"topK" default:"10"`
	VectorWeight    float64  `json:"vectorWeight" default:"0.7"`
	KeywordWeight   float64  `json:"keywordWeight" default:"0.3"`
	MinimumScore    float64  `json:"minimumScore,omitempty"`
	FilterFields    []string `json:"filterFields,omitempty"`
	DefaultFilter   string   `json:"defaultFilter,omitempty"`
	QueryLanguage   string   `json:"queryLanguage" default:"en-us"`
}

// RouteCondition selects when a route applies.
type RouteCondition struct {
	Intent  string   `json:"intent,omitempty"`
	Project string   `json:"project,omitempty"`
	Agent   string   `json:"agent,omitempty"`
	Groups  []string `json:"groups,omitempty"`
}

// KnowledgeRoute routes a query to a knowledge base.
type KnowledgeRoute struct {
	Name          string         `json:"name"`
	When          RouteCondition `json:"when"`
	KnowledgeBase string         `json:"knowledgeBase"`
	Priority      int            `json:"priority" default:"100"`
}

// GetName returns the route name.
func (r KnowledgeRoute) GetName() string { return r.Name }

// Routing configures routing between knowledge bases.
type Routing struct {
	Strategy  string           `json:"strategy" default:"semantic"`
	Fallback  string           `json:"fallback" default:"keyword"`
	Threshold float64          `json:"threshold" default:"0.75"`
	Routes    []KnowledgeRoute `json:"routes,omitempty"`
}

// KnowledgeAccess configures access control.
type KnowledgeAccess struct {
	AllowedGroups []string          `json:"allowedGroups,omitempty"`
	FilterClaims  map[string]string `json:"filterClaims,omitempty"`
}

// RefreshSchedule configures refresh.
type RefreshSchedule struct {
	Enabled     bool   `json:"enabled" default:"true"`
	Schedule    string `json:"schedule" default:"0 0 * * *"`
	Incremental bool   `json:"incremental" default:"true"`
}

// KnowledgeBase is a Foundry IQ knowledge base.
type KnowledgeBase struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Project     string             `json:"project,omitempty"`
	Sources     []KnowledgeSource  `json:"sources"`
	Index       IndexConfiguration `json:"index"`
	Retrieval   Retrieval          `json:"retrieval"`
	Routing     Routing            `json:"routing"`
	Access      KnowledgeAccess    `json:"access"`
	Refresh     RefreshSchedule    `json:"refresh"`
}

// GetName returns the knowledge base name.
func (k KnowledgeBase) GetName() string { return k.Name }

// FoundryIQ groups knowledge bases.
type FoundryIQ struct {
	Enabled        bool            `json:"enabled" default:"true"`
	Project        string          `json:"project,omitempty"`
	KnowledgeBases []KnowledgeBase `json:"knowledgeBases"`
}

// ---------------------------------------------------------------------- gateway

// GatewayAuthentication configures Entra authentication.
type GatewayAuthentication struct {
	Mode              string         `json:"mode" default:"entra"`
	TenantID          string         `json:"tenantId,omitempty"`
	TenantRestriction bool           `json:"tenantRestriction" default:"true"`
	Audiences         []string       `json:"audiences"`
	Issuers           []string       `json:"issuers,omitempty"`
	RequiredClaims    map[string]any `json:"requiredClaims,omitempty"`
	AllowedDomains    []string       `json:"allowedDomains,omitempty"`
}

// GatewayAuthorisation configures authorisation.
type GatewayAuthorisation struct {
	DefaultAction string      `json:"defaultAction" default:"deny"`
	Admins        []Principal `json:"admins,omitempty"`
	Developers    []Principal `json:"developers,omitempty"`
	Consumers     []Principal `json:"consumers,omitempty"`
	ClaimSource   string      `json:"claimSource" default:"groups"`
}

// GatewayEndpoint is an exposed gateway endpoint.
type GatewayEndpoint struct {
	Tracked
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	Methods        []string `json:"methods" default:"POST"`
	Target         string   `json:"target"`
	TargetType     string   `json:"targetType"`
	Project        string   `json:"project,omitempty"`
	AllowedGroups  []string `json:"allowedGroups,omitempty"`
	AllowedRoles   []string `json:"allowedRoles,omitempty"`
	RequiredScopes []string `json:"requiredScopes,omitempty"`
	QuotaProfile   string   `json:"quotaProfile,omitempty"`
	LimitProfile   string   `json:"limitProfile,omitempty"`
	RoutingProfile string   `json:"routingProfile,omitempty"`
	TokenTracking  bool     `json:"tokenTracking" default:"true"`
}

// GetName returns the endpoint name.
func (e GatewayEndpoint) GetName() string { return e.Name }

// GatewayModels configures model governance at the gateway.
type GatewayModels struct {
	Tracked
	Default                string   `json:"default,omitempty"`
	Allowed                []string `json:"allowed,omitempty"`
	Denied                 []string `json:"denied,omitempty"`
	RequireDeploymentAlias bool     `json:"requireDeploymentAlias" default:"true"`
}

// CircuitBreaker configures a backend circuit breaker.
type CircuitBreaker struct {
	Enabled          bool `json:"enabled" default:"true"`
	FailureThreshold int  `json:"failureThreshold" default:"5"`
	RecoverySeconds  int  `json:"recoverySeconds" default:"30"`
}

// GatewayBackend is a gateway backend.
type GatewayBackend struct {
	Name           string         `json:"name"`
	Target         string         `json:"target"`
	Priority       int            `json:"priority" default:"1"`
	Weight         int            `json:"weight" default:"100"`
	Enabled        bool           `json:"enabled" default:"true"`
	CircuitBreaker CircuitBreaker `json:"circuitBreaker"`
}

// GetName returns the backend name.
func (b GatewayBackend) GetName() string { return b.Name }

// GatewayRouting configures backend routing.
type GatewayRouting struct {
	Strategy         string           `json:"strategy" default:"priority"`
	Failover         bool             `json:"failover" default:"true"`
	RetryCount       int              `json:"retryCount" default:"2"`
	RetryStatusCodes []int            `json:"retryStatusCodes" default:"429,500,502,503,504"`
	Backends         []GatewayBackend `json:"backends,omitempty"`
}

// TokenTracking configures token logging.
type TokenTracking struct {
	Enabled                 bool     `json:"enabled" default:"true"`
	Dimensions              []string `json:"dimensions" default:"user,project,agent,model"`
	UserClaim               string   `json:"userClaim" default:"oid"`
	GroupClaim              string   `json:"groupClaim" default:"groups"`
	DepartmentClaim         string   `json:"departmentClaim,omitempty"`
	CapturePromptTokens     bool     `json:"capturePromptTokens" default:"true"`
	CaptureCompletionTokens bool     `json:"captureCompletionTokens" default:"true"`
	CaptureCachedTokens     bool     `json:"captureCachedTokens" default:"true"`
	RetentionDays           int      `json:"retentionDays" default:"365"`
}

// QuotaProfile is a token or request quota.
type QuotaProfile struct {
	Name            string   `json:"name"`
	Scope           string   `json:"scope"`
	DailyTokens     int      `json:"dailyTokens,omitempty"`
	MonthlyTokens   int      `json:"monthlyTokens,omitempty"`
	DailyRequests   int      `json:"dailyRequests,omitempty"`
	MonthlyRequests int      `json:"monthlyRequests,omitempty"`
	KeyClaim        string   `json:"keyClaim,omitempty"`
	Targets         []string `json:"targets,omitempty"`
}

// GetName returns the profile name.
func (q QuotaProfile) GetName() string { return q.Name }

// GatewayQuotas groups quota profiles.
type GatewayQuotas struct {
	Profiles []QuotaProfile `json:"profiles,omitempty"`
}

// LimitProfile is a rate limit.
type LimitProfile struct {
	Name                 string   `json:"name"`
	Scope                string   `json:"scope" default:"global"`
	Requests             int      `json:"requests" default:"100"`
	RenewalPeriodSeconds int      `json:"renewalPeriodSeconds" default:"60"`
	ConcurrentRequests   int      `json:"concurrentRequests" default:"20"`
	KeyClaim             string   `json:"keyClaim,omitempty"`
	Targets              []string `json:"targets,omitempty"`
}

// GetName returns the profile name.
func (l LimitProfile) GetName() string { return l.Name }

// GatewayLimits groups limit profiles.
type GatewayLimits struct {
	Profiles []LimitProfile `json:"profiles,omitempty"`
}

// GatewayCaching configures response caching.
type GatewayCaching struct {
	Enabled    bool     `json:"enabled"`
	Semantic   bool     `json:"semantic"`
	TTLSeconds int      `json:"ttlSeconds" default:"300"`
	VaryBy     []string `json:"varyBy" default:"model,endpoint"`
}

// GatewayLogging configures logging.
type GatewayLogging struct {
	Metadata      bool     `json:"metadata" default:"true"`
	Prompts       bool     `json:"prompts"`
	Completions   bool     `json:"completions"`
	Headers       bool     `json:"headers"`
	Redact        []string `json:"redact" default:"authorization,api-key,set-cookie"`
	RetentionDays int      `json:"retentionDays" default:"90"`
}

// GatewayAlerts configures alert thresholds.
type GatewayAlerts struct {
	ErrorRatePercent  float64 `json:"errorRatePercent" default:"5"`
	LatencyMs         int     `json:"latencyMs" default:"5000"`
	QuotaUsagePercent float64 `json:"quotaUsagePercent" default:"80"`
	TokenUsagePercent float64 `json:"tokenUsagePercent" default:"80"`
}

// GatewayMonitoring configures monitoring.
type GatewayMonitoring struct {
	TrackLatency    bool          `json:"trackLatency" default:"true"`
	TrackFailures   bool          `json:"trackFailures" default:"true"`
	TrackTokenUsage bool          `json:"trackTokenUsage" default:"true"`
	TrackBackend    bool          `json:"trackBackend" default:"true"`
	SampleRate      float64       `json:"sampleRate" default:"1"`
	Alerts          GatewayAlerts `json:"alerts"`
}

// Cors configures CORS.
type Cors struct {
	Enabled        bool     `json:"enabled"`
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
	AllowedMethods []string `json:"allowedMethods" default:"GET,POST,OPTIONS"`
	AllowedHeaders []string `json:"allowedHeaders" default:"authorization,content-type"`
}

// GatewaySecurity configures gateway security.
type GatewaySecurity struct {
	InternalOnly        bool     `json:"internalOnly" default:"true"`
	BlockAnonymous      bool     `json:"blockAnonymous" default:"true"`
	AllowIPs            []string `json:"allowIps,omitempty"`
	DenyIPs             []string `json:"denyIps,omitempty"`
	RequireHTTPS        bool     `json:"requireHttps" default:"true"`
	ValidateContentType bool     `json:"validateContentType" default:"true"`
	MaxRequestBytes     int      `json:"maxRequestBytes" default:"10485760"`
	Cors                Cors     `json:"cors"`
}

// Gateway is the APIM AI Gateway.
type Gateway struct {
	Enabled        bool                   `json:"enabled"`
	Name           string                 `json:"name,omitempty"`
	Mode           string                 `json:"mode" default:"ai-gateway"`
	SKU            string                 `json:"sku" default:"StandardV2"`
	Endpoint       string                 `json:"endpoint,omitempty"`
	Authentication *GatewayAuthentication `json:"authentication,omitempty"`
	Authorisation  GatewayAuthorisation   `json:"authorisation"`
	Endpoints      []GatewayEndpoint      `json:"endpoints,omitempty"`
	Models         GatewayModels          `json:"models"`
	Routing        GatewayRouting         `json:"routing"`
	TokenTracking  TokenTracking          `json:"tokenTracking"`
	Quotas         GatewayQuotas          `json:"quotas"`
	Limits         GatewayLimits          `json:"limits"`
	Caching        GatewayCaching         `json:"caching"`
	Logging        GatewayLogging         `json:"logging"`
	Monitoring     GatewayMonitoring      `json:"monitoring"`
	Security       GatewaySecurity        `json:"security"`
	Tags           Tags                   `json:"tags,omitempty"`
}

// ProjectGateway registers a project with the shared gateway.
type ProjectGateway struct {
	Enabled       bool     `json:"enabled" default:"true"`
	Path          string   `json:"path,omitempty"`
	AllowedGroups []string `json:"allowedGroups,omitempty"`
	QuotaProfile  string   `json:"quotaProfile,omitempty"`
}

// ------------------------------------------------------------ platform components

// StorageContainer is a blob container.
type StorageContainer struct {
	Name         string `json:"name"`
	Purpose      string `json:"purpose,omitempty"`
	PublicAccess string `json:"publicAccess" default:"none"`
}

// GetName returns the container name.
func (c StorageContainer) GetName() string { return c.Name }

// Storage is the storage account.
type Storage struct {
	Tracked
	Enabled               bool               `json:"enabled" default:"true"`
	Name                  string             `json:"name,omitempty"`
	ExistingResourceID    string             `json:"existingResourceId,omitempty"`
	SKU                   string             `json:"sku" default:"Standard_ZRS"`
	Purposes              []string           `json:"purposes" default:"knowledge"`
	Containers            []StorageContainer `json:"containers,omitempty"`
	HierarchicalNamespace bool               `json:"hierarchicalNamespace"`
	PublicNetworkAccess   bool               `json:"publicNetworkAccess"`
	LocalAuthentication   bool               `json:"localAuthentication"`
	RetentionDays         int                `json:"retentionDays" default:"30"`
	Tags                  Tags               `json:"tags,omitempty"`
}

// KeyVault is the Key Vault.
type KeyVault struct {
	Tracked
	Enabled             bool   `json:"enabled" default:"true"`
	Name                string `json:"name,omitempty"`
	ExistingResourceID  string `json:"existingResourceId,omitempty"`
	SKU                 string `json:"sku" default:"standard"`
	RBACAuthorisation   bool   `json:"rbacAuthorisation" default:"true"`
	SoftDeleteDays      int    `json:"softDeleteDays" default:"90"`
	PurgeProtection     bool   `json:"purgeProtection" default:"true"`
	PublicNetworkAccess bool   `json:"publicNetworkAccess"`
	Tags                Tags   `json:"tags,omitempty"`
}

// Cosmos is the Cosmos DB account used by the standard agent setup.
type Cosmos struct {
	Tracked
	Enabled             bool   `json:"enabled" default:"true"`
	Name                string `json:"name,omitempty"`
	ExistingResourceID  string `json:"existingResourceId,omitempty"`
	CapacityMode        string `json:"capacityMode" default:"provisioned"`
	Throughput          int    `json:"throughput" default:"3000"`
	ZoneRedundant       bool   `json:"zoneRedundant"`
	ContinuousBackup    bool   `json:"continuousBackup"`
	PublicNetworkAccess bool   `json:"publicNetworkAccess"`
	LocalAuthentication bool   `json:"localAuthentication"`
	Tags                Tags   `json:"tags,omitempty"`
}

// AgentService configures the Foundry Agent Service setup.
type AgentService struct {
	Setup string `json:"setup" default:"auto"`
}

// FederatedCredential is a workload identity federation credential.
type FederatedCredential struct {
	Name      string   `json:"name"`
	Issuer    string   `json:"issuer"`
	Subject   string   `json:"subject"`
	Audiences []string `json:"audiences" default:"api://AzureADTokenExchange"`
}

// GetName returns the credential name.
func (f FederatedCredential) GetName() string { return f.Name }

// ManagedIdentity is the managed identity configuration.
type ManagedIdentity struct {
	Tracked
	Enabled              bool                  `json:"enabled" default:"true"`
	Type                 string                `json:"type" default:"userAssigned"`
	Name                 string                `json:"name,omitempty"`
	ExistingResourceID   string                `json:"existingResourceId,omitempty"`
	FederatedCredentials []FederatedCredential `json:"federatedCredentials,omitempty"`
	Tags                 Tags                  `json:"tags,omitempty"`
}

// Observability configures telemetry.
type Observability struct {
	Enabled             bool     `json:"enabled" default:"true"`
	ApplicationInsights bool     `json:"applicationInsights" default:"true"`
	LogAnalytics        bool     `json:"logAnalytics" default:"true"`
	RetentionDays       int      `json:"retentionDays" default:"90"`
	SamplingPercentage  float64  `json:"samplingPercentage" default:"100"`
	CapturePrompts      bool     `json:"capturePrompts"`
	CaptureCompletions  bool     `json:"captureCompletions"`
	Dashboards          bool     `json:"dashboards" default:"true"`
	Alerts              bool     `json:"alerts" default:"true"`
	Export              []string `json:"export,omitempty"`
	Tags                Tags     `json:"tags,omitempty"`
}

// EvaluationDataset is an evaluation dataset.
type EvaluationDataset struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Format string `json:"format" default:"jsonl"`
}

// GetName returns the dataset name.
func (d EvaluationDataset) GetName() string { return d.Name }

// Evaluator is an evaluator.
type Evaluator struct {
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Threshold     float64        `json:"threshold,omitempty"`
	Configuration map[string]any `json:"configuration,omitempty"`
}

// GetName returns the evaluator name.
func (e Evaluator) GetName() string { return e.Name }

// Evaluation configures evaluation.
type Evaluation struct {
	Enabled       bool                `json:"enabled"`
	Project       string              `json:"project,omitempty"`
	Datasets      []EvaluationDataset `json:"datasets,omitempty"`
	Evaluators    []Evaluator         `json:"evaluators,omitempty"`
	Schedule      string              `json:"schedule,omitempty"`
	RetentionDays int                 `json:"retentionDays" default:"90"`
	FailThreshold float64             `json:"failThreshold,omitempty"`
}

// Budgets configures cost budgets.
type Budgets struct {
	MonthlyAmount float64   `json:"monthlyAmount,omitempty"`
	Currency      string    `json:"currency" default:"USD"`
	MonthlyTokens int       `json:"monthlyTokens,omitempty"`
	Thresholds    []float64 `json:"thresholds" default:"50,80,100"`
	Contacts      []string  `json:"contacts,omitempty"`
}

// ModelPolicy is the model governance policy.
type ModelPolicy struct {
	AllowedModels          []string `json:"allowedModels,omitempty"`
	DeniedModels           []string `json:"deniedModels,omitempty"`
	AllowedSKUs            []string `json:"allowedSkus,omitempty"`
	RequireApprovedVersion bool     `json:"requireApprovedVersion" default:"true"`
}

// Governance configures governance.
type Governance struct {
	Enabled           bool        `json:"enabled" default:"true"`
	Budgets           Budgets     `json:"budgets"`
	ModelPolicy       ModelPolicy `json:"modelPolicy"`
	ResourceLocks     bool        `json:"resourceLocks" default:"true"`
	RequiredTags      []string    `json:"requiredTags" default:"environment,project,managed-by"`
	PolicyAssignments []string    `json:"policyAssignments,omitempty"`
	DefenderPlans     []string    `json:"defenderPlans,omitempty"`
	DataResidency     []string    `json:"dataResidency,omitempty"`
}

// ------------------------------------------------------------- hub and projects

// Inheritance controls which hub resources spoke projects inherit.
type Inheritance struct {
	Models    bool `json:"models" default:"true"`
	Toolboxes bool `json:"toolboxes" default:"true"`
	Mcps      bool `json:"mcps" default:"true"`
	IQ        bool `json:"iq"`
	Search    bool `json:"search" default:"true"`
}

// Hub is the shared configuration for hub-spoke topologies.
type Hub struct {
	Name          string             `json:"name"`
	Location      string             `json:"location,omitempty"`
	ResourceGroup string             `json:"resourceGroup,omitempty"`
	Models        ModelConfiguration `json:"models"`
	Toolboxes     []Toolbox          `json:"toolboxes,omitempty"`
	Mcps          []Mcp              `json:"mcps,omitempty"`
	IQ            *FoundryIQ         `json:"iq,omitempty"`
	Search        *Search            `json:"search,omitempty"`
	Inheritance   Inheritance        `json:"inheritance"`
	Tags          Tags               `json:"tags,omitempty"`
}

// Project is a Foundry project.
type Project struct {
	Tracked
	Name          string             `json:"name"`
	DisplayName   string             `json:"displayName,omitempty"`
	Description   string             `json:"description,omitempty"`
	Location      string             `json:"location,omitempty"`
	ResourceGroup string             `json:"resourceGroup,omitempty"`
	InheritHub    bool               `json:"inheritHub" default:"true"`
	Roles         SecurityRoles      `json:"roles"`
	Models        ModelConfiguration `json:"models"`
	Agents        []Agent            `json:"agents,omitempty"`
	Toolboxes     []Toolbox          `json:"toolboxes,omitempty"`
	Mcps          []Mcp              `json:"mcps,omitempty"`
	IQ            *FoundryIQ         `json:"iq,omitempty"`
	Search        *Search            `json:"search,omitempty"`
	Gateway       *ProjectGateway    `json:"gateway,omitempty"`
	Connectors    []Connector        `json:"connectors,omitempty"`
	Evaluation    *Evaluation        `json:"evaluation,omitempty"`
	Tags          Tags               `json:"tags,omitempty"`
}

// GetName returns the project name.
func (p Project) GetName() string { return p.Name }

// ResolvedAgentSetup returns "basic" or "standard". The default "auto" selects standard
// in private mode or when a Cosmos DB account is configured.
func (c *XFoundry) ResolvedAgentSetup() string {
	if c.AgentService.Setup != "auto" {
		return c.AgentService.Setup
	}
	if c.Security.Network.Mode == "private" || (c.Cosmos != nil && c.Cosmos.Enabled) {
		return "standard"
	}
	return "basic"
}
