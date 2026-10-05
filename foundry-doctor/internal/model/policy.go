package model

// Policy mirrors the key table of ADR-007 one for one. A nil pointer or nil slice means the key is
// missing, which makes a dependent rule skip with profile-key-missing:<key>. An empty non-nil slice
// means the key was set to an empty list. The yaml tags are the closed schema's key names.
type Policy struct {
	ResourceScope         *ResourceScope         `yaml:"resourceScope,omitempty"`
	AllowedExternalScopes []string               `yaml:"allowedExternalScopes,omitempty"`
	Environments          PolicyEnvironments     `yaml:"environments,omitempty"`
	Tags                  PolicyTags             `yaml:"tags,omitempty"`
	LogRetention          PolicyLogRetention     `yaml:"logRetention,omitempty"`
	Models                PolicyModels           `yaml:"models,omitempty"`
	DataResidency         PolicyDataResidency    `yaml:"dataResidency,omitempty"`
	DisasterRecovery      PolicyDisasterRecovery `yaml:"disasterRecovery,omitempty"`
	Network               PolicyNetwork          `yaml:"network,omitempty"`
	Monitoring            PolicyMonitoring       `yaml:"monitoring,omitempty"`
	ManagedByAzurePolicy  []ManagedBy            `yaml:"managedByAzurePolicy,omitempty"`
	Knowledge             PolicyKnowledge        `yaml:"knowledge,omitempty"`
	Cost                  PolicyCost             `yaml:"cost,omitempty"`
}

// ResourceScope is policy.resourceScope.
type ResourceScope string

// ResourceScope values.
const (
	ScopeSameResourceGroup ResourceScope = "same-resource-group"
	ScopeSameSubscription  ResourceScope = "same-subscription"
	ScopeAny               ResourceScope = "any"
)

// PublicAccess is policy.network.publicAccess.
type PublicAccess string

// PublicAccess values.
const (
	PublicForbidden PublicAccess = "forbidden"
	PublicEntraOnly PublicAccess = "entra-only"
	PublicAllowed   PublicAccess = "allowed"
)

// DataResidencyScope is policy.dataResidency.scope.
type DataResidencyScope string

// DataResidencyScope values.
const (
	ResidencyGlobal       DataResidencyScope = "global"
	ResidencyDatazoneUS   DataResidencyScope = "datazone-us"
	ResidencyDatazoneEU   DataResidencyScope = "datazone-eu"
	ResidencyDatazoneAPAC DataResidencyScope = "datazone-apac"
	ResidencyGeography    DataResidencyScope = "geography"
)

// ManagedBy is an entry of policy.managedByAzurePolicy.
type ManagedBy string

// ManagedBy values.
const (
	ManagedPrivateDNSZoneGroup ManagedBy = "private-dns-zone-group"
	ManagedDiagnosticSettings  ManagedBy = "diagnostic-settings"
)

// PolicyEnvironments is policy.environments.*.
type PolicyEnvironments struct {
	Production    []string `yaml:"production,omitempty"`
	NonProduction []string `yaml:"nonProduction,omitempty"`
	Development   []string `yaml:"development,omitempty"`
}

// TagRequirement is an entry of policy.tags.required. Format is an optional regular expression for the value.
type TagRequirement struct {
	Name   string `yaml:"name"`
	Format string `yaml:"format,omitempty"`
}

// PolicyTags is policy.tags.*.
type PolicyTags struct {
	Required      []TagRequirement `yaml:"required,omitempty"`
	ResourceTypes []string         `yaml:"resourceTypes,omitempty"`
}

// PolicyLogRetention is policy.logRetention.*.
type PolicyLogRetention struct {
	MinimumDays *int `yaml:"minimumDays,omitempty"`
}

// PolicyModels is policy.models.*; entries are format/name[/version].
type PolicyModels struct {
	Allow []string `yaml:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty"`
}

// PolicyDataResidency is policy.dataResidency.*.
type PolicyDataResidency struct {
	Scope          *DataResidencyScope `yaml:"scope,omitempty"`
	Regions        []string            `yaml:"regions,omitempty"`
	DeploymentSkus []string            `yaml:"deploymentSkus,omitempty"`
}

// PolicyDisasterRecovery is policy.disasterRecovery.*.
type PolicyDisasterRecovery struct {
	Declared  *bool   `yaml:"declared,omitempty"`
	Reference *string `yaml:"reference,omitempty"`
}

// PolicyNetwork is policy.network.*.
type PolicyNetwork struct {
	PublicAccess *PublicAccess `yaml:"publicAccess,omitempty"`
}

// PolicyMonitoring is policy.monitoring.*.
type PolicyMonitoring struct {
	PublicTelemetry *bool `yaml:"publicTelemetry,omitempty"`
}

// PolicyKnowledge is policy.knowledge.* (Phase 8).
type PolicyKnowledge struct {
	RequireDocumentLevelAccess *bool `yaml:"requireDocumentLevelAccess,omitempty"`
}

// PolicyCost is policy.cost.*.
type PolicyCost struct {
	DevMaxCosmosThroughput       *int     `yaml:"devMaxCosmosThroughput,omitempty"`
	ProductionSizedSkuExemptions []string `yaml:"productionSizedSkuExemptions,omitempty"`
}

// Policy keys as written in profile-key-missing:<key> skip reasons.
const (
	KeyResourceScope                  = "policy.resourceScope"
	KeyAllowedExternalScopes          = "policy.allowedExternalScopes"
	KeyEnvironmentsProduction         = "policy.environments.production"
	KeyEnvironmentsNonProduction      = "policy.environments.nonProduction"
	KeyEnvironmentsDevelopment        = "policy.environments.development"
	KeyTagsRequired                   = "policy.tags.required"
	KeyTagsResourceTypes              = "policy.tags.resourceTypes"
	KeyLogRetentionMinimumDays        = "policy.logRetention.minimumDays"
	KeyModelsAllow                    = "policy.models.allow"
	KeyModelsDeny                     = "policy.models.deny"
	KeyDataResidencyScope             = "policy.dataResidency.scope"
	KeyDataResidencyRegions           = "policy.dataResidency.regions"
	KeyDataResidencyDeploymentSkus    = "policy.dataResidency.deploymentSkus"
	KeyDisasterRecoveryDeclared       = "policy.disasterRecovery.declared"
	KeyNetworkPublicAccess            = "policy.network.publicAccess"
	KeyMonitoringPublicTelemetry      = "policy.monitoring.publicTelemetry"
	KeyManagedByAzurePolicy           = "policy.managedByAzurePolicy"
	KeyKnowledgeRequireDocLevelAccess = "policy.knowledge.requireDocumentLevelAccess"
	KeyCostDevMaxCosmosThroughput     = "policy.cost.devMaxCosmosThroughput"
	KeyCostProductionSizedSkuExempt   = "policy.cost.productionSizedSkuExemptions"
)

// EffectiveResourceScope applies the ADR-007 baseline (same-resource-group).
func (p Policy) EffectiveResourceScope() ResourceScope {
	if p.ResourceScope == nil {
		return ScopeSameResourceGroup
	}
	return *p.ResourceScope
}

// EffectivePublicAccess applies the ADR-007 baseline (forbidden).
func (p Policy) EffectivePublicAccess() PublicAccess {
	if p.Network.PublicAccess == nil {
		return PublicForbidden
	}
	return *p.Network.PublicAccess
}

// EffectivePublicTelemetry applies the ADR-007 baseline (false).
func (p Policy) EffectivePublicTelemetry() bool {
	return p.Monitoring.PublicTelemetry != nil && *p.Monitoring.PublicTelemetry
}
