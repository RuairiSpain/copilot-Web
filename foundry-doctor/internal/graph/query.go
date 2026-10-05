package graph

import (
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Node returns the node with the ID in O(1).
func (g *Graph) Node(id string) (model.GraphNode, bool) {
	i, ok := g.nodeIdx[id]
	if !ok {
		return model.GraphNode{}, false
	}
	return g.Nodes[i], true
}

// Out returns the targets of edges of kind leaving from, sorted.
func (g *Graph) Out(from, kind string) []string { return slices.Clone(g.adj[adjKey{from, kind}]) }

// In returns the sources of edges of kind arriving at to, sorted.
func (g *Graph) In(to, kind string) []string { return slices.Clone(g.radj[adjKey{to, kind}]) }

// ServicesUsing returns the services whose uses list names target (a node ID), sorted.
func (g *Graph) ServicesUsing(target string) []string { return g.In(target, model.EdgeUses) }

// ProducersOf returns the producers (ARM outputs) of the env-key node, sorted.
func (g *Graph) ProducersOf(envKey string) []string { return g.In(envKey, model.EdgeProduces) }

// ConsumersOf returns the services or azure.yaml nodes that read the env-key node, sorted.
func (g *Graph) ConsumersOf(envKey string) []string { return g.In(envKey, model.EdgeConsumes) }

// Caveat returns the confidence caveat of an edge, or "" when it needs none. A produces edge from an ARM
// output is only likely (FND-CFG-006).
func (g *Graph) Caveat(e model.GraphEdge) string {
	if e.Kind == model.EdgeProduces && strings.HasPrefix(e.From, model.NodeOutput+":") {
		return ProducesCaveat
	}
	return ""
}

// UnresolvedUses returns the services.*.uses entries that match no service or resources entry.
func (g *Graph) UnresolvedUses() []Unresolved {
	var out []Unresolved
	for _, u := range g.unresolved {
		if u.Kind == model.EdgeUses {
			out = append(out, u)
		}
	}
	return out
}

// Unresolved returns every reference that could not be resolved statically, sorted. A rule must treat
// these as "cannot tell", not as a failure.
func (g *Graph) Unresolved() []Unresolved { return slices.Clone(g.unresolved) }

// PrivateEndpointsFor returns the resources whose private-link connection targets the node, sorted.
func (g *Graph) PrivateEndpointsFor(target string) []string { return g.In(target, EdgePrivateLink) }

// RoleAssignmentsAt returns the role assignment resources scoped to the node, sorted.
func (g *Graph) RoleAssignmentsAt(target string) []string {
	var out []string
	for _, id := range g.In(target, EdgeScope) {
		if strings.EqualFold(g.resType[id], roleAssignmentType) {
			out = append(out, id)
		}
	}
	return out
}

// SubnetsOf returns the subnets the node is placed in, sorted.
func (g *Graph) SubnetsOf(id string) []string { return g.Out(id, EdgeSubnet) }

// EnvRefs returns every interpolation found in azure.yaml, sorted by owner and position.
func (g *Graph) EnvRefs() []EnvRef { return slices.Clone(g.envRefs) }

// EnvRefsOf returns the interpolations held by one owner node.
func (g *Graph) EnvRefsOf(owner string) []EnvRef {
	var out []EnvRef
	for _, r := range g.envRefs {
		if r.Owner == owner {
			out = append(out, r)
		}
	}
	return out
}

// FormsSeen returns the interpolation forms seen in azure.yaml, sorted.
func (g *Graph) FormsSeen() []Form { return slices.Clone(g.forms) }

// EnvironmentHas reports whether the selected azd environment sets key. It is false when no environment
// was supplied; use HasEnvironment to tell the two apart.
func (g *Graph) EnvironmentHas(key string) bool { _, ok := g.envKeys[key]; return ok }

// HasEnvironment reports whether an environment was supplied to Build.
func (g *Graph) HasEnvironment() bool { return g.hasEnv }
