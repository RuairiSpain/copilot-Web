// Package ids holds stable identifiers for scopes and deployment-graph nodes.
//
// A scope is where a configuration item is declared: "root" (top-level shared
// items), "hub" or "project:<name>".
package ids

import (
	"regexp"
	"strings"
)

// Scope and well-known node identifiers.
const (
	RootScope = "root"
	HubScope  = "hub"

	ResourceGroup  = "resource-group"
	Identity       = "identity"
	Network        = "network"
	PrivateDNS     = "private-dns"
	Workspace      = "observability"
	Alerts         = "alerts"
	Storage        = "storage"
	KeyVault       = "key-vault"
	Redis          = "redis"
	Cosmos         = "cosmos"
	Events         = "events"
	Registry       = "registry"
	Foundry        = "foundry"
	Gateway        = "gateway"
	Governance     = "governance"
	CapabilityHost = "capability-host"
)

// ProjectScope returns the scope id of a project.
func ProjectScope(name string) string { return "project:" + name }

// ProjectName returns the project name of a project scope id.
func ProjectName(scope string) string { return strings.TrimPrefix(scope, "project:") }

// SearchNode is the node id of the Search service declared in a scope.
func SearchNode(scope string) string { return "search:" + scope }

// ProjectNode is the node of the Foundry project backing a scope.
func ProjectNode(scope string) string { return "foundry-project:" + scope }

// ItemNode is the node id of a named item of the given kind hosted in a scope.
func ItemNode(kind, scope, name string) string { return kind + ":" + scope + ":" + name }

// PrivateEndpointNode is the node id of a private endpoint.
func PrivateEndpointNode(component, group string) string {
	return "private-endpoint:" + component + ":" + group
}

var nonName = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// Slug makes a value usable as a resource name (letters, digits and hyphens).
func Slug(value string) string {
	cleaned := strings.Trim(nonName.ReplaceAllString(value, "-"), "-")
	if cleaned == "" {
		return "item"
	}
	return cleaned
}
