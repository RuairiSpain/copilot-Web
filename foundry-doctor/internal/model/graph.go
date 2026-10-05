package model

// Graph node and edge kinds.
const (
	NodeService  = "service"
	NodeResource = "arm-resource"
	NodeEnvKey   = "env-key"
	NodeOutput   = "arm-output"

	EdgeUses      = "uses"      // services.*.uses
	EdgeProduces  = "produces"  // output or resource produces an env key
	EdgeConsumes  = "consumes"  // service reads an env key
	EdgeDependsOn = "dependsOn" // ARM dependency
)

// GraphNode is a vertex of the local dependency graph.
type GraphNode struct {
	ID   string // "<kind>:<name>"
	Kind string
	Name string
	Pos  Pos
}

// GraphEdge is a directed edge.
type GraphEdge struct {
	From, To string // node IDs
	Kind     string
}

// Graph is the typed local dependency graph (read-only view). Nodes and Edges are in deterministic order.
type Graph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

// NodeID builds a node ID.
func NodeID(kind, name string) string { return kind + ":" + name }

// Node returns the node with the ID.
func (g Graph) Node(id string) (GraphNode, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return GraphNode{}, false
}

// Out returns the targets of edges of the given kind leaving from.
func (g Graph) Out(from, kind string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.From == from && e.Kind == kind {
			out = append(out, e.To)
		}
	}
	return out
}

// In returns the sources of edges of the given kind arriving at to.
func (g Graph) In(to, kind string) []string {
	var in []string
	for _, e := range g.Edges {
		if e.To == to && e.Kind == kind {
			in = append(in, e.From)
		}
	}
	return in
}
