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

// PolicyExemption is an exemption summary.
type PolicyExemption struct {
	ID           string
	Name         string
	Scope        string
	AssignmentID string
	Category     string // "Waiver" or "Mitigated"
	ExpiresOn    string // RFC3339 or empty
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
	ListExemptions(ctx context.Context, scope string) ([]PolicyExemption, error)
	CheckRestrictions(ctx context.Context, req PolicyRestrictionRequest) ([]PolicyRestriction, error)
}

// ---------------------------------------------------------------------------
// What-if (opt-in only)

// ChangeType is the ARM what-if change type.
type ChangeType string

// ARM what-if change types; ChangeReplace is set only when the adapter can
// establish a delete-and-create of the same resource and is always destructive.
const (
	ChangeCreate   ChangeType = "Create"
	ChangeModify   ChangeType = "Modify"
	ChangeDelete   ChangeType = "Delete"
	ChangeDeploy   ChangeType = "Deploy"
	ChangeIgnore   ChangeType = "Ignore"
	ChangeNoChange ChangeType = "NoChange"
	ChangeReplace  ChangeType = "Replace"
	ChangeUnknown  ChangeType = "Unknown"
)

// WhatIfChange is a single predicted change. No before/after bodies are retained.
type WhatIfChange struct {
	ResourceID string
	ChangeType ChangeType
}

// Destructive reports whether the change deletes or replaces a resource.
func (c WhatIfChange) Destructive() bool {
	return c.ChangeType == ChangeDelete || c.ChangeType == ChangeReplace
}

// WhatIfRequest runs a what-if. OptIn must be true or Run fails without calling Azure.
type WhatIfRequest struct {
	// OptIn records the explicit --what-if flag.
	OptIn bool
	// Scope is "/subscriptions/<id>/resourceGroups/<rg>" or "/subscriptions/<id>" with Location.
	Scope    string
	Location string
	// Template is the compiled ARM JSON; Parameters are non-secret values by name.
	Template   []byte
	Parameters map[string]any
}

// WhatIfResult lists predicted changes sorted by resource ID.
type WhatIfResult struct {
	Changes []WhatIfChange
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
	Foundry     RuntimeFoundry
	RBAC        RuntimeRBAC
	DNS         RuntimeDNS
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
	ID                string
	Name              string
	AccountID         string
	PrincipalID       string
	IdentityType      string
	ProvisioningState string
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
	ResourceID   string
	Endpoint     string
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
	Target   ConnectionTarget
}

// AccountDeployment is the management-plane summary of one account deployment.
type AccountDeployment struct {
	ID                   string
	Type                 string
	Name                 string
	DeploymentState      string
	ProvisioningState    string
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

// CosmosSQLRoleAssignment is the metadata-only Cosmos SQL role assignment evidence.
type CosmosSQLRoleAssignment struct {
	RoleDefinitionID string
	PrincipalID      string
	Scope            string
}

// PrivateDNSVNetLink is one private DNS virtual network link.
type PrivateDNSVNetLink struct {
	Name  string
	State string
}

// MetricTotal is the aggregate count/value for one series.
type MetricTotal struct {
	Series string
	Total  float64
}

// RuntimeDiagnosticSetting is the RUN-007 projection of a diagnostic setting.
type RuntimeDiagnosticSetting struct {
	Name        string
	WorkspaceID string
	Categories  []string
	AgeHours    int
}

// RuntimeLogsResult is the count-only projection of a Log Analytics query.
type RuntimeLogsResult struct {
	Table  string
	Counts map[string]int64
}

// RuntimeFoundry reads the management metadata used by RUN-001/003/004.
type RuntimeFoundry interface {
	GetProject(ctx context.Context, subscriptionID, resourceGroup, account, project string) (FoundryProject, error)
	ListProjectCapabilityHosts(ctx context.Context, subscriptionID, resourceGroup, account, project string) ([]CapabilityHost, error)
	ListAccountCapabilityHosts(ctx context.Context, subscriptionID, resourceGroup, account string) ([]CapabilityHost, error)
	ListProjectConnections(ctx context.Context, subscriptionID, resourceGroup, account, project string) ([]FoundryConnection, error)
	ListAccountConnections(ctx context.Context, subscriptionID, resourceGroup, account string) ([]FoundryConnection, error)
	ListAccountDeployments(ctx context.Context, subscriptionID, resourceGroup, account string) ([]AccountDeployment, error)
}

// RuntimeRBAC reads the metadata-only RBAC evidence used by RUN-001.
type RuntimeRBAC interface {
	ListRoleAssignments(ctx context.Context, scope, principalID string) ([]RoleAssignment, error)
	ListCosmosSQLRoleAssignments(ctx context.Context, accountID, principalID string) ([]CosmosSQLRoleAssignment, error)
}

// RuntimeDNS reads private DNS metadata used by RUN-002.
type RuntimeDNS interface {
	ListPrivateDNSVNetLinks(ctx context.Context, zoneID string) ([]PrivateDNSVNetLink, error)
}

// RuntimeMonitor reads the aggregate monitor evidence used by RUN-004/007.
type RuntimeMonitor interface {
	QueryAccountMetrics(ctx context.Context, accountID, metricName, filterDimension, filterValue, seriesDimension string) ([]MetricTotal, error)
	ListDiagnosticSettings(ctx context.Context, resourceID string) ([]RuntimeDiagnosticSetting, error)
	QueryDiagnosticCounts(ctx context.Context, workspaceID, accountName string, hours int) (RuntimeLogsResult, error)
}

// RegisterAction returns the RBAC action needed to register a resource
// provider namespace, e.g. "Microsoft.CognitiveServices/register/action".
func RegisterAction(namespace string) string { return namespace + "/register/action" }
