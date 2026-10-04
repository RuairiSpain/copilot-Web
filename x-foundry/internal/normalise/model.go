// Package normalise resolves defaults, implicit resources and inheritance.
package normalise

import "github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"

// ScopeResources are the resources declared in one scope ("root", "hub" or "project:<name>").
type ScopeResources struct {
	Scope          string                    `json:"scope"`
	Models         config.ModelConfiguration `json:"models"`
	Agents         []config.Agent            `json:"agents,omitempty"`
	Toolboxes      []config.Toolbox          `json:"toolboxes,omitempty"`
	Mcps           []config.Mcp              `json:"mcps,omitempty"`
	Connectors     []config.Connector        `json:"connectors,omitempty"`
	KnowledgeBases []config.KnowledgeBase    `json:"knowledgeBases,omitempty"`
	Search         *config.Search            `json:"search,omitempty"`
	Evaluation     *config.Evaluation        `json:"evaluation,omitempty"`
}

// Implicit is a resource the normaliser derived because another setting requires it.
type Implicit struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

// PrivateEndpoint is a private endpoint to a component.
type PrivateEndpoint struct {
	Component string   `json:"component"`
	Group     string   `json:"group"`
	Zones     []string `json:"zones,omitempty"` // private DNS zones the endpoint registers in
}

// Network is the resolved network configuration.
type Network struct {
	Mode                            string            `json:"mode"`
	ModeSource                      string            `json:"modeSource"`
	VNet                            string            `json:"vnet,omitempty"` // create | existing
	ExistingVnetResourceID          string            `json:"existingVnetResourceId,omitempty"`
	AddressSpace                    string            `json:"addressSpace,omitempty"`
	AgentSubnet                     string            `json:"agentSubnet,omitempty"` // create | existing
	AgentSubnetPrefixLength         int               `json:"agentSubnetPrefixLength,omitempty"`
	AgentSubnetResourceID           string            `json:"agentSubnetResourceId,omitempty"`
	PrivateEndpointSubnetResourceID string            `json:"privateEndpointSubnetResourceId,omitempty"`
	PrivateDNS                      bool              `json:"privateDns"`
	AllowedIPs                      []string          `json:"allowedIps,omitempty"`
	PrivateEndpoints                []PrivateEndpoint `json:"privateEndpoints,omitempty"`
	PrivateDNSZones                 []string          `json:"privateDnsZones,omitempty"`
}

// Roles are the merged, de-duplicated principals per role.
type Roles struct {
	Admins     []config.Principal `json:"admins,omitempty"`
	Developers []config.Principal `json:"developers,omitempty"`
	Consumers  []config.Principal `json:"consumers,omitempty"`
	Operators  []config.Principal `json:"operators,omitempty"`
}

// EffectiveProject is a project's resources after inheritance (root < hub < project).
type EffectiveProject struct {
	Name           string                    `json:"name"`
	DisplayName    string                    `json:"displayName"`
	Description    string                    `json:"description,omitempty"`
	Location       string                    `json:"location,omitempty"`
	ResourceGroup  string                    `json:"resourceGroup,omitempty"`
	InheritsHub    bool                      `json:"inheritsHub"`
	Roles          Roles                     `json:"roles"`
	Tags           config.Tags               `json:"tags,omitempty"`
	Models         config.ModelConfiguration `json:"models"`
	Agents         []config.Agent            `json:"agents,omitempty"`
	Toolboxes      []config.Toolbox          `json:"toolboxes,omitempty"`
	Mcps           []config.Mcp              `json:"mcps,omitempty"`
	Connectors     []config.Connector        `json:"connectors,omitempty"`
	KnowledgeBases []config.KnowledgeBase    `json:"knowledgeBases,omitempty"`
	SearchScope    string                    `json:"searchScope,omitempty"`
	Evaluation     *config.Evaluation        `json:"evaluation,omitempty"`
	Gateway        *config.ProjectGateway    `json:"gateway,omitempty"`
	Origins        map[string]string         `json:"origins,omitempty"`
}

// HubView is the resolved hub.
type HubView struct {
	Name          string             `json:"name"`
	Location      string             `json:"location,omitempty"`
	ResourceGroup string             `json:"resourceGroup,omitempty"`
	Inheritance   config.Inheritance `json:"inheritance"`
	SearchScope   string             `json:"searchScope,omitempty"`
	Tags          config.Tags        `json:"tags,omitempty"`
}

// Config is the fully resolved configuration used for planning.
type Config struct {
	SchemaVersion       string                  `json:"schemaVersion"`
	TopologyMode        string                  `json:"topologyMode"`
	NamingPrefix        string                  `json:"namingPrefix,omitempty"`
	Environment         string                  `json:"environment"`
	Location            string                  `json:"location,omitempty"`
	ResourceGroup       string                  `json:"resourceGroup,omitempty"`
	Tags                config.Tags             `json:"tags,omitempty"`
	Roles               Roles                   `json:"roles"`
	Network             Network                 `json:"network"`
	LocalAuthentication bool                    `json:"localAuthentication"`
	PurgeProtection     bool                    `json:"purgeProtection"`
	AgentSetup          string                  `json:"agentSetup"`
	FoundryIdentity     string                  `json:"foundryIdentity"`
	Hub                 *HubView                `json:"hub,omitempty"`
	Scopes              []*ScopeResources       `json:"scopes"`
	Projects            []*EffectiveProject     `json:"projects"`
	Gateway             *config.Gateway         `json:"gateway,omitempty"`
	Storage             *config.Storage         `json:"storage,omitempty"`
	KeyVault            *config.KeyVault        `json:"keyVault,omitempty"`
	Cosmos              *config.Cosmos          `json:"cosmos,omitempty"`
	ManagedIdentity     *config.ManagedIdentity `json:"managedIdentity,omitempty"`
	Observability       *config.Observability   `json:"observability,omitempty"`
	Governance          *config.Governance      `json:"governance,omitempty"`
	Implicit            []Implicit              `json:"implicit,omitempty"`
}

// Scope returns the resources of a scope, or nil.
func (c *Config) Scope(id string) *ScopeResources {
	for _, s := range c.Scopes {
		if s.Scope == id {
			return s
		}
	}
	return nil
}

// Project returns the named project, or nil.
func (c *Config) Project(name string) *EffectiveProject {
	for _, p := range c.Projects {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// HasImplicit reports whether a resource of the given kind and name was derived.
func (c *Config) HasImplicit(kind, name string) bool {
	for _, i := range c.Implicit {
		if i.Kind == kind && i.Name == name {
			return true
		}
	}
	return false
}
