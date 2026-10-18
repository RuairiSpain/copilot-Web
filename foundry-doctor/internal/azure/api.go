// Package azure defines the read-only Azure capability interfaces consumed by
// internal/preflight (and later runtime phases), plus plain data types.
//
// Contract rules (ADR-006):
//   - There is no generic "send request" method. Every operation is named and
//     is on the allow-list enforced by tests in this package.
//   - A capability that cannot run returns *UnavailableError, which the
//     caller maps to a skipped check naming the exact capability and the
//     missing permission. Unavailable is never a pass.
//   - Secrets, outputs, parameters, what-if before/after bodies and document
//     content never appear in these types.
//
// Every I/O method takes context.Context first and must honour cancellation.
package azure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Typed errors

// UnavailableError reports that a capability could not be evaluated.
// Error() is "capability unavailable: <Capability> (missing permission: <Permission>)"
// (or "(<Reason>)" when no single permission is at fault).
type UnavailableError struct {
	// Capability is the exact capability, e.g. "Microsoft.Authorization/roleAssignments/read at /subscriptions/<id>".
	Capability string
	// Permission is the missing RBAC action or role requirement; empty when not permission-related.
	Permission string
	// Reason is a short, secret-free explanation (e.g. "not signed in").
	Reason string
	// Err is the underlying cause, never containing tokens or response bodies.
	Err error
}

func (e *UnavailableError) Error() string {
	detail := e.Reason
	if e.Permission != "" {
		detail = "missing permission: " + e.Permission
		if e.Reason != "" {
			detail += "; " + e.Reason
		}
	}
	if detail == "" {
		return "capability unavailable: " + e.Capability
	}
	return fmt.Sprintf("capability unavailable: %s (%s)", e.Capability, detail)
}

// Unwrap returns the underlying cause.
func (e *UnavailableError) Unwrap() error { return e.Err }

// ErrUnavailable lets callers use errors.Is(err, azure.ErrUnavailable).
var ErrUnavailable = errors.New("capability unavailable")

// Is makes every *UnavailableError match ErrUnavailable.
func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

// AsUnavailable extracts an *UnavailableError from err.
func AsUnavailable(err error) (*UnavailableError, bool) {
	var u *UnavailableError
	if errors.As(err, &u) {
		return u, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Context, subscription, tenant, providers

// Context describes the authenticated target. It never carries tokens.
type Context struct {
	TenantID         string
	SubscriptionID   string
	SubscriptionName string
	// SubscriptionState is the ARM state, e.g. "Enabled", "Disabled", "Warned".
	SubscriptionState string
	// SubscriptionTenantID is the tenant that owns the subscription.
	SubscriptionTenantID string
	// PrincipalObjectID and PrincipalType identify the caller when known.
	PrincipalObjectID string
	PrincipalType     string
	Cloud             string // "AzureCloud", "AzureUSGovernment", "AzureChinaCloud"
}

// ContextRequest names the intended target (resolved from azd by the caller).
// Empty fields mean "use the credential's default".
type ContextRequest struct {
	TenantID       string
	SubscriptionID string
}

// ProviderState is the registration state of one resource provider namespace.
type ProviderState struct {
	Namespace string
	// State is the ARM registrationState: "Registered", "NotRegistered", "Registering", "Unregistering".
	State string
}

// ContextProvider resolves and validates the deployment target.
type ContextProvider interface {
	// Context authenticates and returns the target. Returns *UnavailableError
	// when no credential is available or the subscription is not readable.
	Context(ctx context.Context, req ContextRequest) (Context, error)
	// ProviderStates reads (never registers) provider registration state.
	ProviderStates(ctx context.Context, subscriptionID string, namespaces []string) ([]ProviderState, error)
}

// ---------------------------------------------------------------------------
// Inventory

// Resource is a deployed resource summary. Properties are an allow-listed,
// secret-free projection.
type Resource struct {
	ID            string
	Name          string
	Type          string
	Location      string
	ResourceGroup string
	Kind          string
	SKUName       string
	Tags          map[string]string
	// Properties holds only allow-listed scalar properties requested by the caller.
	Properties map[string]string
}

// InventoryQuery selects resources. Scope is a subscription or resource group ID.
type InventoryQuery struct {
	Scope string
	// Types filters by resource type (case-insensitive), empty means all.
	Types []string
	// PropertyPaths are allow-listed properties to project, e.g. "properties.provisioningState".
	PropertyPaths []string
	// MaxResults bounds pagination; <= 0 uses the adapter default.
	MaxResults int
}

// Lock is a management lock (read only).
type Lock struct {
	ID    string
	Name  string
	Scope string
	Level string // "CanNotDelete" or "ReadOnly"
}

// Subnet is a subnet summary used for conflict checks.
type Subnet struct {
	ID                 string
	Name               string
	VNetID             string
	AddressPrefixes    []string
	DelegationServices []string
	// NSGID and RouteTableID are empty when none attached.
	NSGID        string
	RouteTableID string
	// PrivateEndpointIDs lists private endpoints connected to this subnet.
	PrivateEndpointIDs []string
}

// ResourceGroupInfo is the existence and location of a resource group.
type ResourceGroupInfo struct {
	Name     string
	Location string
	Exists   bool
}

// ResourceList is a bounded, deterministic (sorted by ID) listing.
type ResourceList struct {
	Resources []Resource
	// Truncated is true when MaxResults cut the listing; checks must then be uncertain.
	Truncated bool
	// Source is "resource-graph" or "arm-list".
	Source string
}

// Inventory reads deployed state. Resource Graph is preferred; adapters fall
// back to ARM list calls and say so via ResourceList.Source.
type Inventory interface {
	ListResources(ctx context.Context, q InventoryQuery) (ResourceList, error)
	ResourceGroup(ctx context.Context, subscriptionID, name string) (ResourceGroupInfo, error)
	// ListLocks lists the management locks at a subscription or resource group
	// scope. The Microsoft.Authorization locks list returns locks at AND BELOW
	// the scope (spec 2020-05-01), so callers must filter by Lock.Scope
	// (see LockCovers) before treating a lock as relevant. A scope other than a
	// subscription or resource group is rejected with ErrInvalidInput; use
	// InventoryExtras.ListResourceLocks for one resource.
	ListLocks(ctx context.Context, scope string) ([]Lock, error)
	GetSubnet(ctx context.Context, subnetID string) (Subnet, error)
}

// ---------------------------------------------------------------------------
// Permissions

// Decision is the outcome for one action at one scope.
type Decision string

const (
	// DecisionAllowed means a role assignment grants the action and no deny
	// assignment or evaluated condition blocks it.
	DecisionAllowed Decision = "allowed"
	// DecisionDenied means no assignment grants the action, or a deny assignment blocks it.
	DecisionDenied Decision = "denied"
	// DecisionUnknown means evaluation was not possible (see Reason).
	DecisionUnknown Decision = "unknown"
)

// Stable ActionDecision.Reason codes.
const (
	ReasonNoGrantingRole            = "no-granting-role"
	ReasonDenyAssignment            = "deny-assignment"
	ReasonConditionNotEvaluated     = "condition-not-evaluated"
	ReasonDenyAssignmentsUnreadable = "deny-assignments-unreadable"
	ReasonAssignmentsUnreadable     = "role-assignments-unreadable"
	// ReasonReportedNotProof marks an Allowed decision taken from the
	// permissions API (PermissionEvidence): the action is listed for the
	// caller, which is not proof the operation will succeed (FND-DEP-003).
	ReasonReportedNotProof = "reported-not-proof"
)

// ActionDecision is the effective permission decision for one action.
type ActionDecision struct {
	Action   string
	Decision Decision
	// Reason is non-empty for DecisionDenied/DecisionUnknown (Reason* constants).
	Reason string
	// DenyAssignmentIDs lists deny assignments that match, when readable.
	DenyAssignmentIDs []string
	// ConditionSkipped is true when a granting assignment has an ABAC condition
	// that the adapter does not evaluate; the decision is then Unknown, not Allowed.
	ConditionSkipped bool
}

// PermissionRequest asks about actions at a scope. IsDataAction selects dataActions.
type PermissionRequest struct {
	Scope        string
	Actions      []string
	IsDataAction bool
}

// Permissions computes effective permissions of the current principal.
type Permissions interface {
	// EffectiveActions returns one decision per requested action, in request order.
	// Returns *UnavailableError if role assignments cannot be read at all.
	EffectiveActions(ctx context.Context, req PermissionRequest) ([]ActionDecision, error)
}

// ---------------------------------------------------------------------------
// Model availability and quota

// ModelSKU is a deployable SKU for a model version in a region.
type ModelSKU struct {
	Name            string // e.g. "GlobalStandard", "Standard"
	DefaultCapacity int32
	MinCapacity     int32
	MaxCapacity     int32
	// UsageName is the quota usage key this SKU consumes.
	UsageName string
}

// ModelAvailability is one model version offered in a location.
type ModelAvailability struct {
	Format  string // e.g. "OpenAI"
	Name    string
	Version string
	SKUs    []ModelSKU
	// LifecycleStatus is the API-reported status; may be empty.
	LifecycleStatus string
	// DeprecationDate is RFC3339 date text when the API reports one; empty otherwise.
	DeprecationDate string
}

// QuotaUsage is one usage counter.
type QuotaUsage struct {
	Name    string
	Current float64
	Limit   float64
	Unit    string
}

// ModelQuery filters availability. Empty Name lists all models.
type ModelQuery struct {
	SubscriptionID string
	Location       string
	Format         string
	Name           string
}

// Models reads model catalogue availability and quota usage.
type Models interface {
	ListModels(ctx context.Context, q ModelQuery) ([]ModelAvailability, error)
	ListUsages(ctx context.Context, subscriptionID, location string) ([]QuotaUsage, error)
}

// ---------------------------------------------------------------------------
// Policy

// PolicyAssignment is an assignment summary. Parameter values are not carried.
type PolicyAssignment struct {
	ID              string
	Name            string
	Scope           string
	DefinitionID    string
	DisplayName     string
	EnforcementMode string // "Default" or "DoNotEnforce"
	NotScopes       []string
	// Effects lists effects declared by the definition when resolvable.
	Effects []string
}

// PolicyRestrictionRequest describes a planned resource, built from non-secret template content only.
type PolicyRestrictionRequest struct {
	Scope        string // resource group ID (subscription ID also accepted)
	ResourceType string
	APIVersion   string
	Location     string
	Content      map[string]any
	// ResourceScope is the optional resourceDetails.scope (parent resource ID for child resources).
	ResourceScope string
	// IncludeAudit sets includeAuditEffect so audit effects are returned (informational only).
	IncludeAudit bool
}

// Restriction kinds.
const (
	RestrictionObservedDenial    = "observed-denial"
	RestrictionPotentialConflict = "potential-conflict"
)

// PolicyRestriction is one field restriction or deny finding.
type PolicyRestriction struct {
	// Kind is RestrictionObservedDenial or RestrictionPotentialConflict.
	Kind         string
	AssignmentID string
	// Effect is the policy effect reported by the service (restrictions[].policyEffect);
	// empty when the service did not say. RestrictionObservedDenial requires "deny".
	Effect  string
	Field   string
	Message string
	// Result is the field restriction result (e.g. "Required", "Removed"); empty for evaluations.
	Result string
}

// Policy reads policy evidence.
type Policy interface {
	ListAssignments(ctx context.Context, scope string) ([]PolicyAssignment, error)
	CheckRestrictions(ctx context.Context, req PolicyRestrictionRequest) ([]PolicyRestriction, error)
}

// ---------------------------------------------------------------------------
// What-if (opt-in only)

// ChangeType is the ARM what-if change type.
type ChangeType string

// ARM what-if change types (Microsoft.Resources deployments spec 2026-06-01,
// ChangeType enum: Create, Delete, Ignore, Deploy, NoChange, Modify,
// Unsupported). There is no Replace value: a recreation caused by an immutable
// property change is NOT reported by ARM and is never inferred here.
// ChangeUnknown is Foundry Doctor's own value for a string outside the enum.
const (
	ChangeCreate      ChangeType = "Create"
	ChangeModify      ChangeType = "Modify"
	ChangeDelete      ChangeType = "Delete"
	ChangeDeploy      ChangeType = "Deploy"
	ChangeIgnore      ChangeType = "Ignore"
	ChangeNoChange    ChangeType = "NoChange"
	ChangeUnsupported ChangeType = "Unsupported"
	ChangeUnknown     ChangeType = "Unknown"
)

// WhatIfChange is a single predicted change. No before/after bodies are retained.
type WhatIfChange struct {
	ResourceID string
	ChangeType ChangeType
}

// Destructive reports whether ARM predicts a delete. The adapter requests
// deployment mode Incremental, in which what-if essentially never reports
// Delete, and ARM has no change type for recreation.
func (c WhatIfChange) Destructive() bool { return c.ChangeType == ChangeDelete }

// WhatIfRequest runs a what-if. OptIn must be true or Run fails without calling Azure.
type WhatIfRequest struct {
	// OptIn records the explicit --what-if flag.
	OptIn bool
	// Scope is "/subscriptions/<id>/resourceGroups/<rg>" or "/subscriptions/<id>" with Location.
	Scope    string
	Location string
	// Template is the compiled ARM JSON; Parameters are non-secret values by
	// name (the caller must never put a secure-parameter value here).
	Template   []byte
	Parameters map[string]any
}

// WhatIfResult lists predicted changes sorted by resource ID.
type WhatIfResult struct {
	// Status is the final operation status; Run returns a result only for "Succeeded".
	Status  string
	Changes []WhatIfChange
	// PotentialChanges are the properties.potentialChanges entries of the
	// spec. ARM does not say how they differ from changes; they are uncertain
	// evidence and never a pass or a fail by themselves.
	PotentialChanges []WhatIfChange
	// DiagnosticCodes are error codes only (no free text from response bodies).
	DiagnosticCodes []string
}

// WhatIf runs ARM what-if. It needs deployment-level permission; missing
// permission yields *UnavailableError naming Microsoft.Resources/deployments/whatIf/action.
type WhatIf interface {
	Run(ctx context.Context, req WhatIfRequest) (WhatIfResult, error)
}

// ---------------------------------------------------------------------------
// Names and soft-delete

// NameKind selects the name-availability service.
type NameKind string

// Supported name kinds.
const (
	NameKeyVault NameKind = "keyvault"
	NameStorage  NameKind = "storage"
	NameACR      NameKind = "acr"
	NameAPIM     NameKind = "apim"
	NameSearch   NameKind = "search"
	NameFoundry  NameKind = "foundry" // Cognitive Services account domain
)

// NameCheck is a request for name availability. Location is needed for Foundry.
type NameCheck struct {
	Kind           NameKind
	Name           string
	SubscriptionID string
	Location       string
}

// NameResult is the availability result.
type NameResult struct {
	Available bool
	// Reason is the ARM reason code ("AlreadyExists", "Invalid", ...); empty when available.
	Reason string
}

// SoftDeleted describes a soft-deleted resource that holds a name.
type SoftDeleted struct {
	Kind               NameKind
	Name               string
	ID                 string
	Location           string
	DeletionDate       string // RFC3339 or empty
	ScheduledPurgeDate string
	PurgeProtection    bool
}

// Names checks name availability and lists (never recovers or purges)
// soft-deleted resources.
type Names interface {
	CheckName(ctx context.Context, c NameCheck) (NameResult, error)
	ListSoftDeleted(ctx context.Context, kind NameKind, subscriptionID string) ([]SoftDeleted, error)
}

// ---------------------------------------------------------------------------
// Aggregate

// Client bundles all capabilities for wiring. Preflight depends on the
// individual interfaces, not on Client.
type Client struct {
	Context     ContextProvider
	Inventory   Inventory
	Permissions Permissions
	Models      Models
	Policy      Policy
	WhatIf      WhatIf
	Names       Names
	// Additive (review fixes): nil in older wirings; New always sets them.
	Regions     RegionAvailability
	Deployments DeploymentHistory
	SubnetLinks SubnetLinks
	Evidence    PermissionEvidence
	Extras      InventoryExtras
	Foundry     RuntimeFoundry
	RBAC        RuntimeRBAC
	Monitor     RuntimeMonitor
}

// ---------------------------------------------------------------------------
// Region availability (FND-DEP-012)

// LocationInfo is a subscription location (Subscriptions_ListLocations).
type LocationInfo struct {
	Name                string
	DisplayName         string
	RegionalDisplayName string
}

// ProviderResourceType is one resourceTypes[] entry of a provider. Locations
// are exactly as reported (names or display names; the spec does not say), so
// compare with LocationOffered.
type ProviderResourceType struct {
	ResourceType string
	Locations    []string
}

// RegionAvailability reads which regions and resource types a subscription can use.
type RegionAvailability interface {
	ListLocations(ctx context.Context, subscriptionID string) ([]LocationInfo, error)
	ProviderResourceTypes(ctx context.Context, subscriptionID, namespace string) ([]ProviderResourceType, error)
}

// ---------------------------------------------------------------------------
// Deployment history (FND-DEP-010)

// DeploymentHistoryLimit is the documented per-resource-group deployment
// history limit (rules/catalog/dep/FND-DEP-010.yaml).
const DeploymentHistoryLimit = 800

// DeploymentCount is the number of deployments in a resource group's history.
// Only the count is kept; deployment contents, parameters and outputs are never read.
type DeploymentCount struct {
	Count int
	// Truncated is true when the adapter stopped counting at its cap; Count is then a lower bound.
	Truncated bool
}

// DeploymentHistory counts deployment history entries.
type DeploymentHistory interface {
	CountDeployments(ctx context.Context, subscriptionID, resourceGroup string) (DeploymentCount, error)
}

// ---------------------------------------------------------------------------
// Subnet links (FND-DEP-011)

// NetworkLink is a service association link or resource navigation link.
type NetworkLink struct {
	Name               string
	LinkedResourceType string
	// Link is the resource ID of the linked resource.
	Link string
}

// SubnetLinkSet holds both link kinds of one subnet, sorted by name.
type SubnetLinkSet struct {
	ServiceAssociationLinks []NetworkLink
	ResourceNavigationLinks []NetworkLink
}

// SubnetLinks reads the links that bind a subnet to other resources.
type SubnetLinks interface {
	SubnetLinks(ctx context.Context, subnetID string) (SubnetLinkSet, error)
}

// ---------------------------------------------------------------------------
// Inventory extras (FND-DEP-010, FND-DEP-011)

// VNet is the minimal virtual network projection used for the location check.
type VNet struct {
	ID       string
	Name     string
	Location string
}

// InventoryExtras are single-resource reads kept apart from Inventory so older
// Inventory implementations keep compiling.
type InventoryExtras interface {
	// GetVNet reads one virtual network by resource ID (VirtualNetworks_Get,
	// api-version 2025-05-01); it never lists the networks of a subscription.
	GetVNet(ctx context.Context, vnetID string) (VNet, error)
	// ListResourceLocks lists the locks of one resource and any level below it
	// (ManagementLocks_ListAtResourceLevel, api-version 2020-05-01). A resource
	// that does not exist has no locks (empty list, no error).
	ListResourceLocks(ctx context.Context, resourceID string) ([]Lock, error)
}

// LockCovers reports whether a lock at lockScope applies to the resource or
// scope id: the lock scope is id itself or one of its ancestors (locks are
// inherited by child resources, and a lock never applies upward or sideways).
// Comparison is case-insensitive on whole path segments.
func LockCovers(lockScope, id string) bool {
	l := strings.ToLower(strings.TrimRight(lockScope, "/"))
	t := strings.ToLower(strings.TrimRight(id, "/"))
	return l != "" && (t == l || strings.HasPrefix(t, l+"/"))
}

// ---------------------------------------------------------------------------
// Permissions API evidence (FND-DEP-003)

// PermissionSet is one entry of the Microsoft.Authorization/permissions result.
type PermissionSet struct {
	Actions        []string
	NotActions     []string
	DataActions    []string
	NotDataActions []string
}

// PermissionEvidence reads the permissions the Azure Permissions API reports
// for the caller at a resource group or resource scope. The API has no
// subscription-scope variant (use Permissions.EffectiveActions there).
// Results are evidence, never proof: deny assignments, ABAC conditions, locks,
// policy and PIM are not reflected.
type PermissionEvidence interface {
	ReportedPermissions(ctx context.Context, scope string) ([]PermissionSet, error)
	// CheckReportedActions maps req.Actions onto the reported permissions:
	// DecisionAllowed (Reason ReasonReportedNotProof) when listed, DecisionDenied
	// (ReasonNoGrantingRole) when absent or excluded by notActions.
	CheckReportedActions(ctx context.Context, req PermissionRequest) ([]ActionDecision, error)
}

// ---------------------------------------------------------------------------
// Runtime management metadata (Phase 3)

// FoundryProject is the management-plane summary of one project.
type FoundryProject struct {
	ID        string
	Name      string
	AccountID string
	// PrincipalID is the system-assigned identity principal (identity.principalId).
	PrincipalID string
	// UserAssignedPrincipalIDs are identity.userAssignedIdentities.*.principalId
	// (cognitiveservices.json 2026-09-01, Identity), sorted and de-duplicated.
	UserAssignedPrincipalIDs []string
	IdentityType             string
	ProvisioningState        string
}

// IdentityPrincipals returns the principals the project may act as: the
// system-assigned principal first, then the user-assigned ones.
func (p FoundryProject) IdentityPrincipals() []string {
	var out []string
	if p.PrincipalID != "" {
		out = append(out, p.PrincipalID)
	}
	for _, u := range p.UserAssignedPrincipalIDs {
		if u != p.PrincipalID {
			out = append(out, u)
		}
	}
	return out
}

// CapabilityHost is the management-plane summary of one capability host.
type CapabilityHost struct {
	ID                       string
	Type                     string
	Name                     string
	ProvisioningState        string
	AIServiceConnections     []string
	StorageConnections       []string
	ThreadStorageConnections []string
	VectorStoreConnections   []string
}

// ConnectionTarget is the metadata-only connection target projection.
type ConnectionTarget struct {
	// ResourceID is metadata.ResourceId when present, else properties.target
	// when that is itself an ARM resource ID. The spec types metadata as a free
	// string map; the key name "ResourceId" is UNVERIFIED as a schema fact (it is
	// the key Foundry connections use in practice).
	ResourceID string
	// Endpoint is properties.target.
	Endpoint string
	// IndexNames and IndexerNames come from connection metadata keys that the
	// spec does not define (UNVERIFIED, optional hints only).
	IndexNames   []string
	IndexerNames []string
}

// FoundryConnection is the management-plane summary of one project or account connection.
type FoundryConnection struct {
	ID       string
	Type     string
	Name     string
	Category string
	AuthType string
	Error    string
	// PERequirement is properties.peRequirement (Required, NotRequired,
	// NotApplicable) and PEStatus is properties.peStatus (Inactive, Active,
	// NotApplicable) per cognitiveservices.json 2026-09-01 (ManagedPERequirement,
	// ManagedPEStatus). They describe a managed private endpoint.
	PERequirement string
	PEStatus      string
	Target        ConnectionTarget
}

// AccountDeployment is the management-plane summary of one account deployment.
type AccountDeployment struct {
	ID                string
	Type              string
	Name              string
	DeploymentState   string
	ProvisioningState string
	// SKUName is sku.name of the deployment (for example "GlobalStandard"); empty when absent.
	SKUName              string
	DynamicThrottling    bool
	CurrentCapacity      int
	ProvisionedRateLimit int
}

// RoleAssignment is the metadata-only RBAC assignment evidence used at runtime.
type RoleAssignment struct {
	RoleDefinitionID string
	PrincipalID      string
	Scope            string
	Condition        string
}

// RoleAssignmentSet is the result of listing a principal's assignments around a target.
// The list API with $filter=principalId eq {id} returns assignments at, above or
// below the scope (authorization-RoleAssignmentsCalls.json 2022-04-01).
type RoleAssignmentSet struct {
	// Covering holds assignments whose scope equals or is a path-boundary
	// ancestor of the target: the only ones that grant access at the target.
	Covering []RoleAssignment
	// Below holds assignments strictly below the target (for example a
	// container scope). They never grant access at the target.
	Below []RoleAssignment
	// Unresolved holds assignments whose relation to the target cannot be
	// proven: no scope in the response, or a root or management-group scope.
	Unresolved []RoleAssignment
	// Truncated is true when paging was cut short.
	Truncated bool
}

// CosmosSQLRoleAssignment is the metadata-only Cosmos SQL role assignment evidence.
type CosmosSQLRoleAssignment struct {
	// RoleDefinitionID is the full ARM ID of the sqlRoleDefinition.
	RoleDefinitionID string
	PrincipalID      string
	// Scope is the full ARM data-plane path (account, /dbs/{db} or /dbs/{db}/colls/{coll}).
	Scope string
}

// MetricQuery describes one Azure Monitor metrics read (Metrics_List 2024-02-01).
type MetricQuery struct {
	// MetricName is the REST name, for example AzureOpenAIRequests.
	MetricName string
	// Aggregation is Total, Maximum or Average. The metric must support it.
	Aggregation string
	// SplitDimensions become "$filter=Dim eq '*'" terms joined by "and".
	// Names must match [A-Za-z][A-Za-z0-9]{0,63}.
	SplitDimensions []string
	// Window is the look-back (default 1h). The query sends an explicit
	// timespan "startISO/endISO" computed from the adapter clock.
	Window time.Duration
}

// MetricSeries is one time series of a metrics query.
type MetricSeries struct {
	// Dimensions maps lower-cased metadata names to values (metadatavalues).
	Dimensions map[string]string
	// Value aggregates the points: Total sums, Maximum takes the maximum,
	// Average averages the non-null points.
	Value float64
	// Points counts non-null data points.
	Points int
}

// MetricResult is the outcome of a metrics query.
type MetricResult struct {
	Series []MetricSeries
	// Truncated is true when the series count reached the requested top, so
	// further series may exist.
	Truncated bool
	// ErrorCode is the per-metric errorCode when it is not "Success".
	ErrorCode string
}

// RuntimeDiagnosticSetting is the RUN-007 projection of a diagnostic setting.
type RuntimeDiagnosticSetting struct {
	Name string
	// WorkspaceID is the full ARM resource ID of the Log Analytics workspace.
	WorkspaceID string
	Categories  []string
	// CategoryGroups are enabled logs[].categoryGroup values (for example allLogs).
	CategoryGroups []string
	// LogAnalyticsDestinationType is logAnalyticsDestinationType: "Dedicated"
	// (resource-specific tables) or empty (AzureDiagnostics).
	LogAnalyticsDestinationType string
	// AgeKnown is true when systemData carried a parseable timestamp; AgeMinutes
	// is the age of the last modification (or creation) and means nothing otherwise.
	AgeKnown   bool
	AgeMinutes int
}

// RuntimeLogsResult is the count-only projection of a Log Analytics query.
// The response table is always named PrimaryResult, so it is not recorded.
type RuntimeLogsResult struct {
	Counts map[string]int64
}

// AccountNetwork is the network exposure of a Foundry account (Accounts_Get,
// cognitiveservices.json 2026-09-01).
type AccountNetwork struct {
	// PublicNetworkAccess is properties.publicNetworkAccess (Enabled or Disabled);
	// empty when the service did not report it.
	PublicNetworkAccess string
	// PrivateEndpointConnections counts properties.privateEndpointConnections
	// in any state.
	PrivateEndpointConnections int
	// ApprovedPrivateEndpointConnections counts those whose
	// properties.privateLinkServiceConnectionState.status is Approved
	// (PrivateEndpointServiceConnectionStatus: Pending, Approved, Rejected).
	// Only an Approved connection is proof of private-endpoint backing.
	ApprovedPrivateEndpointConnections int
	// CustomSubDomainName is properties.customSubDomainName, the real
	// subdomain of the account's data-plane host names; it can differ from the
	// ARM account name. Empty when the service did not report it.
	CustomSubDomainName string
}

// RuntimeFoundry reads the management metadata used by RUN-001/002/003/004.
type RuntimeFoundry interface {
	GetAccountNetwork(ctx context.Context, subscriptionID, resourceGroup, account string) (AccountNetwork, error)
	GetProject(ctx context.Context, subscriptionID, resourceGroup, account, project string) (FoundryProject, error)
	ListProjectCapabilityHosts(ctx context.Context, subscriptionID, resourceGroup, account, project string) ([]CapabilityHost, error)
	ListAccountCapabilityHosts(ctx context.Context, subscriptionID, resourceGroup, account string) ([]CapabilityHost, error)
	ListProjectConnections(ctx context.Context, subscriptionID, resourceGroup, account, project string) ([]FoundryConnection, error)
	ListAccountConnections(ctx context.Context, subscriptionID, resourceGroup, account string) ([]FoundryConnection, error)
	ListAccountDeployments(ctx context.Context, subscriptionID, resourceGroup, account string) ([]AccountDeployment, error)
}

// RuntimeRBAC reads the metadata-only RBAC evidence used by RUN-001.
type RuntimeRBAC interface {
	ListRoleAssignments(ctx context.Context, scope, principalID string) (RoleAssignmentSet, error)
	ListCosmosSQLRoleAssignments(ctx context.Context, accountID, principalID string) ([]CosmosSQLRoleAssignment, error)
}

// RuntimeMonitor reads the aggregate monitor evidence used by RUN-004/007.
type RuntimeMonitor interface {
	QueryAccountMetrics(ctx context.Context, accountID string, q MetricQuery) (MetricResult, error)
	ListDiagnosticSettings(ctx context.Context, resourceID string) ([]RuntimeDiagnosticSetting, error)
	// QueryDiagnosticCounts resolves the workspace customerId from the
	// workspace ARM resource ID, then runs the fixed AzureDiagnostics count
	// template through the allow-listed Log Analytics operation.
	QueryDiagnosticCounts(ctx context.Context, workspaceResourceID, accountName string, hours int) (RuntimeLogsResult, error)
}

// WithinScope reports whether id equals or lies under any non-empty scope
// (path-boundary, case-insensitive). It is the shared cross-resource-group
// scope policy test (ADR-021 e, ADR-022).
func WithinScope(id string, scopes ...string) bool {
	for _, s := range scopes {
		if LockCovers(s, id) {
			return true
		}
	}
	return false
}

// RegisterAction returns the RBAC action needed to register a resource
// provider namespace, e.g. "Microsoft.CognitiveServices/register/action".
func RegisterAction(namespace string) string { return namespace + "/register/action" }
