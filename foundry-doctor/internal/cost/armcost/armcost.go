// Package armcost holds the compiled-plan extraction shared by the cost
// estimator (internal/cost) and the COST rules (internal/rules/cost), so that
// `foundry-doctor cost` and `foundry-doctor check` read the same facts.
//
// It never evaluates an ARM expression: a string starting with "[" (other
// than the "[[" escape) is unresolved.
package armcost

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// IsExpr reports whether v is an unresolved ARM expression string.
func IsExpr(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[[")
}

// Number converts a literal JSON number (float64, int, int64, json.Number) or
// a numeric literal string to float64. Expressions are never numbers.
func Number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		if IsExpr(x) {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// Presence is the deployment state of a resource in the compiled plan.
type Presence int

const (
	Deployed Presence = iota
	NotDeployed
	Uncertain
)

// PresenceOf classifies r from Conditional, Condition and Looped. A literal
// false condition means not deployed; any other condition or a copy loop
// (whose instance count is not resolved) is uncertain.
func PresenceOf(r sdk.ARMResource) Presence {
	if r.Conditional {
		c := strings.TrimSpace(r.Condition)
		if IsExpr(c) && strings.HasSuffix(c, "]") {
			c = c[1 : len(c)-1]
		}
		c = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(c), " ", ""))
		switch c {
		case "false", "false()":
			return NotDeployed
		case "true", "true()":
			if !r.Looped {
				return Deployed
			}
		}
		return Uncertain
	}
	if r.Looped {
		return Uncertain
	}
	return Deployed
}

// SKUName returns the literal sku.name of r (from SKUName, else SKU["name"])
// and whether a sku.name is present but unresolved.
func SKUName(r sdk.ARMResource) (name string, unresolved bool) {
	if n := strings.TrimSpace(r.SKUName); n != "" {
		return n, false
	}
	if r.SKU == nil {
		return "", false
	}
	switch v := r.SKU["name"].(type) {
	case string:
		if IsExpr(v) {
			return "", true
		}
		return strings.TrimSpace(v), false
	case nil:
		return "", false
	default:
		return "", true
	}
}

// SKUCapacity returns the literal sku.capacity of r and whether it is present
// but unresolved.
func SKUCapacity(r sdk.ARMResource) (capacity float64, ok, unresolved bool) {
	if r.SKU == nil {
		return 0, false, false
	}
	v, present := r.SKU["capacity"]
	if !present || v == nil {
		return 0, false, false
	}
	if n, ok := Number(v); ok {
		return n, true, false
	}
	return 0, false, true
}

// DeploymentClass is the billing model of a Foundry model deployment type.
type DeploymentClass int

const (
	DeploymentUnknown DeploymentClass = iota
	DeploymentPayPerToken
	DeploymentProvisioned
	DeploymentBatch
	DeploymentDeveloper
)

// DeploymentSKU describes one documented deployment type.
type DeploymentSKU struct {
	// Code is the documented sku.name spelling.
	Code string
	// Scope is the data-processing scope: Global, DataZone or Regional.
	Scope string
	Class DeploymentClass
}

// deploymentSKUs is the documented deployment-type table, verified
// 2026-10-18 at
// https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types
// (section "Deployment type comparison"): pay-per-token, reserved PTU, batch
// at a 50% discount, and the Developer type for fine-tuned model evaluation.
var deploymentSKUs = []DeploymentSKU{
	{Code: "GlobalStandard", Scope: "Global", Class: DeploymentPayPerToken},
	{Code: "GlobalProvisionedManaged", Scope: "Global", Class: DeploymentProvisioned},
	{Code: "GlobalBatch", Scope: "Global", Class: DeploymentBatch},
	{Code: "DataZoneStandard", Scope: "DataZone", Class: DeploymentPayPerToken},
	{Code: "DataZoneProvisionedManaged", Scope: "DataZone", Class: DeploymentProvisioned},
	{Code: "DataZoneBatch", Scope: "DataZone", Class: DeploymentBatch},
	{Code: "Standard", Scope: "Regional", Class: DeploymentPayPerToken},
	{Code: "ProvisionedManaged", Scope: "Regional", Class: DeploymentProvisioned},
	{Code: "DeveloperTier", Scope: "Global", Class: DeploymentDeveloper},
}

// ClassifyDeploymentSKU matches name case-insensitively against the
// documented deployment types. ok is false for any other value.
func ClassifyDeploymentSKU(name string) (DeploymentSKU, bool) {
	n := strings.TrimSpace(name)
	for _, s := range deploymentSKUs {
		if strings.EqualFold(n, s.Code) {
			return s, true
		}
	}
	return DeploymentSKU{}, false
}

const (
	cosmosSQLDatabase  = "microsoft.documentdb/databaseaccounts/sqldatabases"
	cosmosSQLContainer = "microsoft.documentdb/databaseaccounts/sqldatabases/containers"
	cosmosPrefix       = "microsoft.documentdb/databaseaccounts/"
	// TypeCosmosAccount is the Cosmos DB account resource type.
	TypeCosmosAccount = "Microsoft.DocumentDB/databaseAccounts"
)

// IsCosmosThroughputType matches throughputSettings resources and the Cosmos
// DB SQL databases and containers, whose properties.options.throughput and
// properties.options.autoscaleSettings.maxThroughput set throughput at
// creation (CreateUpdateOptions, Microsoft.DocumentDB 2025-04-15).
func IsCosmosThroughputType(typ string) bool {
	t := strings.ToLower(typ)
	return strings.HasSuffix(t, "/throughputsettings") || t == cosmosSQLDatabase || t == cosmosSQLContainer
}

// IsCosmosChildWithOptions reports whether r is another Cosmos DB child
// resource (MongoDB, Cassandra, Gremlin, Table) that carries
// properties.options; the estimator does not model these API types.
func IsCosmosChildWithOptions(r sdk.ARMResource) bool {
	t := strings.ToLower(r.Type)
	if !strings.HasPrefix(t, cosmosPrefix) || IsCosmosThroughputType(r.Type) || r.Properties == nil {
		return false
	}
	_, ok := r.Properties["options"].(map[string]any)
	return ok
}

func bagKey(typ string) string {
	t := strings.ToLower(typ)
	if t == cosmosSQLDatabase || t == cosmosSQLContainer {
		return "options"
	}
	return "resource"
}

// ThroughputBag returns the object that holds throughput:
// properties.resource for throughputSettings, properties.options for SQL
// databases and containers. nil when absent.
func ThroughputBag(r sdk.ARMResource) map[string]any {
	if r.Properties == nil {
		return nil
	}
	m, _ := r.Properties[bagKey(r.Type)].(map[string]any)
	return m
}

// Throughput is the provisioned throughput read from one Cosmos resource.
type Throughput struct {
	Manual       float64
	HasManual    bool
	AutoscaleMax float64
	HasAutoscale bool
	// Unresolved is true when the options/resource object, throughput,
	// autoscaleSettings or maxThroughput is an unresolved expression.
	Unresolved bool
}

// None reports whether the resource sets no dedicated throughput at all.
func (t Throughput) None() bool { return !t.HasManual && !t.HasAutoscale && !t.Unresolved }

// CosmosThroughput reads manual and autoscale throughput from r.
func CosmosThroughput(r sdk.ARMResource) Throughput {
	var out Throughput
	bag := ThroughputBag(r)
	if bag == nil {
		if r.Properties != nil {
			out.Unresolved = IsExpr(r.Properties[bagKey(r.Type)])
		}
		return out
	}
	if v, present := bag["throughput"]; present && v != nil {
		if n, ok := Number(v); ok {
			out.Manual, out.HasManual = n, true
		} else {
			out.Unresolved = true
		}
	}
	switch auto := bag["autoscaleSettings"].(type) {
	case map[string]any:
		if v, present := auto["maxThroughput"]; present && v != nil {
			if n, ok := Number(v); ok {
				out.AutoscaleMax, out.HasAutoscale = n, true
			} else {
				out.Unresolved = true
			}
		}
	case string:
		if IsExpr(auto) {
			out.Unresolved = true
		}
	}
	return out
}

// ParentName returns the first segment of a child resource name
// ("acct/db/container" -> "acct"). ok is false for an expression name or a
// name without a parent segment.
func ParentName(name string) (string, bool) {
	n := strings.TrimSpace(name)
	if n == "" || IsExpr(n) {
		return "", false
	}
	i := strings.Index(n, "/")
	if i <= 0 {
		return "", false
	}
	return n[:i], true
}

// FindParent returns the resource of type parentType whose name equals the
// first segment of child's name (case-insensitive).
func FindParent(all []sdk.ARMResource, child sdk.ARMResource, parentType string) (sdk.ARMResource, bool) {
	parent, ok := ParentName(child.Name)
	if !ok {
		return sdk.ARMResource{}, false
	}
	for _, r := range all {
		if strings.EqualFold(r.Type, parentType) && strings.EqualFold(strings.TrimSpace(r.Name), parent) {
			return r, true
		}
	}
	return sdk.ARMResource{}, false
}

// CosmosRegion is one region of a Cosmos DB account.
type CosmosRegion struct {
	Name          string
	ZoneRedundant bool
}

// CosmosAccount is the billing-relevant shape of a Cosmos DB account:
// properties.locations[].locationName and isZoneRedundant,
// enableMultipleWriteLocations, enableFreeTier and capabilities
// (Microsoft.DocumentDB/databaseAccounts template reference).
type CosmosAccount struct {
	Regions          []CosmosRegion
	MultiRegionWrite bool
	FreeTier         bool
	Serverless       bool
	// UnresolvedReason is set when a field that changes the bill cannot be
	// read from the compiled plan.
	UnresolvedReason string
}

// ReadCosmosAccount reads the billing-relevant fields of a Cosmos DB account.
func ReadCosmosAccount(acct sdk.ARMResource) CosmosAccount {
	var out CosmosAccount
	p := acct.Properties
	if p == nil {
		p = map[string]any{}
	}
	switch v := p["enableMultipleWriteLocations"].(type) {
	case bool:
		out.MultiRegionWrite = v
	case nil:
	default:
		out.UnresolvedReason = "enableMultipleWriteLocations is unresolved"
	}
	switch v := p["enableFreeTier"].(type) {
	case bool:
		out.FreeTier = v
	case nil:
	default:
		out.UnresolvedReason = "enableFreeTier is unresolved"
	}
	switch caps := p["capabilities"].(type) {
	case []any:
		for _, c := range caps {
			m, _ := c.(map[string]any)
			name, _ := m["name"].(string)
			if m == nil || IsExpr(name) {
				out.UnresolvedReason = "capabilities are unresolved"
				continue
			}
			if strings.EqualFold(name, "EnableServerless") {
				out.Serverless = true
			}
		}
	case nil:
	default:
		out.UnresolvedReason = "capabilities are unresolved"
	}
	switch locs := p["locations"].(type) {
	case []any:
		for _, l := range locs {
			m, _ := l.(map[string]any)
			if m == nil {
				out.UnresolvedReason = "locations are unresolved"
				continue
			}
			name, _ := m["locationName"].(string)
			if strings.TrimSpace(name) == "" || IsExpr(name) {
				out.UnresolvedReason = "a locations[].locationName is unresolved"
				continue
			}
			reg := CosmosRegion{Name: strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", ""))}
			switch z := m["isZoneRedundant"].(type) {
			case bool:
				reg.ZoneRedundant = z
			case nil:
			default:
				out.UnresolvedReason = "a locations[].isZoneRedundant is unresolved"
			}
			out.Regions = append(out.Regions, reg)
		}
	case nil:
		out.UnresolvedReason = "properties.locations is not set in the compiled plan"
	default:
		out.UnresolvedReason = "locations are unresolved"
	}
	if len(out.Regions) == 0 && out.UnresolvedReason == "" {
		out.UnresolvedReason = "account has no locations"
	}
	return out
}

// LiteralRegion lower-cases a literal region. ok is false for an empty or
// expression value.
func LiteralRegion(region string) (string, bool) {
	region = strings.TrimSpace(region)
	if region == "" || IsExpr(region) {
		return "", false
	}
	return strings.ToLower(region), true
}

// SovereignRegion reports whether region belongs to a sovereign cloud that
// the Azure Retail Prices API does not cover ("This version only supports
// prices on Commercial Cloud", Retail Prices API overview, verified
// 2026-10-18).
func SovereignRegion(region string) bool {
	r := strings.ToLower(strings.TrimSpace(region))
	return strings.HasPrefix(r, "usgov") || strings.HasPrefix(r, "usdod") || strings.HasPrefix(r, "usnat") ||
		strings.HasPrefix(r, "ussec") || strings.HasPrefix(r, "china")
}
