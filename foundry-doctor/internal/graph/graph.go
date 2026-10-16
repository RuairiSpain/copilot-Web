// Package graph builds a typed local dependency graph over azure.yaml
// services (and their `uses` references) and ARM resources. It defines its own
// small input structs so it does not depend on other internal packages.
package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
)

// Kind is a node kind.
type Kind string

// Node kinds.
const (
	KindService  Kind = "service"
	KindResource Kind = "resource"
)

// EdgeKind is an edge kind.
type EdgeKind string

// Edge kinds.
const (
	EdgeUses      EdgeKind = "uses"      // service -> service/resource it declares in `uses`
	EdgeDependsOn EdgeKind = "dependsOn" // resource -> resource
	EdgeHosts     EdgeKind = "hosts"     // service -> resource that hosts it (Host binding)
)

// Service is an azure.yaml service input.
type Service struct {
	Name string
	Host string
	Uses []string
	// Resource optionally names the ARM resource (by Name) backing the service.
	Resource string
}

// Resource is an ARM resource input.
type Resource struct {
	ID           string // unique within the input, e.g. the JSON pointer
	Type         string
	Name         string
	SymbolicName string
	DependsOn    []string // ARM dependsOn entries (resourceId expressions or symbolic names)
	// DependsOnIDs are the IDs of resources this one depends on directly, such
	// as every resource declared in a module deployment the resource's module
	// depends on. IDs that are not deployed nodes, and the resource itself, are
	// ignored.
	DependsOnIDs []string
	// Conditional, Condition and Looped carry the ARM condition and copy loop.
	// A literal false condition removes the resource from the graph; any other
	// condition or a loop marks the node as conditional.
	Conditional bool
	Condition   string
	Looped      bool
}

// Input is everything the graph is built from.
type Input struct {
	Services  []Service
	Resources []Resource
}

// Node is a graph vertex.
type Node struct {
	ID   string
	Kind Kind
	Name string
	Type string
	// Conditional is true when the resource is deployed only under a
	// condition or loop the template cannot resolve.
	Conditional bool
}

// Edge is a directed edge From -> To.
type Edge struct {
	From string
	To   string
	Kind EdgeKind
}

// Unresolved is a reference that could not be resolved to a node.
type Unresolved struct {
	From string
	Ref  string
	Kind EdgeKind
	// InTemplate is true when the reference may name an in-template resource
	// whose name could not be correlated; it must not be shown as external.
	InTemplate bool
}

// Graph is an immutable dependency graph with deterministic ordering.
type Graph struct {
	Nodes      []Node
	Edges      []Edge
	Unresolved []Unresolved
	byID       map[string]Node
	out        map[string][]Edge
	in         map[string][]Edge
}

// ServiceID returns the node id of a service.
func ServiceID(name string) string { return "service:" + name }

// ResourceID returns the node id of a resource.
func ResourceID(id string) string { return "resource:" + id }

// Build constructs the graph. Duplicate ids return an error.
func Build(in Input) (*Graph, error) {
	g := &Graph{byID: map[string]Node{}, out: map[string][]Edge{}, in: map[string][]Edge{}}
	svcByName := map[string]string{}
	for _, s := range in.Services {
		if s.Name == "" {
			return nil, fmt.Errorf("graph: service with empty name")
		}
		id := ServiceID(s.Name)
		if _, dup := g.byID[id]; dup {
			return nil, fmt.Errorf("graph: duplicate service %q", s.Name)
		}
		g.byID[id] = Node{ID: id, Kind: KindService, Name: s.Name, Type: s.Host}
		svcByName[s.Name] = id
	}
	resBySym := map[string]string{}
	resByName := map[string][]string{}
	var deployed []Resource
	notDeployed := map[string]bool{}
	for _, r := range in.Resources {
		if r.ID == "" {
			return nil, fmt.Errorf("graph: resource with empty id")
		}
		id := ResourceID(r.ID)
		pr := armexpr.PresenceOf(r.Conditional, r.Condition, r.Looped)
		if pr == armexpr.NotDeployed {
			notDeployed[id] = true
			continue
		}
		if _, dup := g.byID[id]; dup {
			return nil, fmt.Errorf("graph: duplicate resource %q", r.ID)
		}
		deployed = append(deployed, r)
		g.byID[id] = Node{ID: id, Kind: KindResource, Name: r.Name, Type: r.Type, Conditional: pr == armexpr.MaybeDeployed}
		if r.SymbolicName != "" {
			resBySym[r.SymbolicName] = id
		}
		if r.Name != "" {
			resByName[r.Name] = append(resByName[r.Name], id)
		}
	}

	seen := map[Edge]bool{}
	add := func(e Edge) {
		if seen[e] {
			return
		}
		seen[e] = true
		g.Edges = append(g.Edges, e)
	}
	for _, s := range in.Services {
		from := ServiceID(s.Name)
		for _, u := range s.Uses {
			if to, ok := svcByName[u]; ok {
				add(Edge{from, to, EdgeUses})
			} else if to, ok := resBySym[u]; ok {
				add(Edge{from, to, EdgeUses})
			} else if ids := resByName[u]; len(ids) == 1 {
				add(Edge{from, ids[0], EdgeUses})
			} else {
				g.Unresolved = append(g.Unresolved, Unresolved{From: from, Ref: u, Kind: EdgeUses})
			}
		}
		if s.Resource != "" {
			if ids := resByName[s.Resource]; len(ids) == 1 {
				add(Edge{from, ids[0], EdgeHosts})
			} else if to, ok := resBySym[s.Resource]; ok {
				add(Edge{from, to, EdgeHosts})
			} else {
				g.Unresolved = append(g.Unresolved, Unresolved{From: from, Ref: s.Resource, Kind: EdgeHosts})
			}
		}
	}
	for _, r := range deployed {
		from := ResourceID(r.ID)
		for _, id := range r.DependsOnIDs {
			if to := ResourceID(id); to != from {
				if _, ok := g.byID[to]; ok {
					add(Edge{from, to, EdgeDependsOn})
				}
			}
		}
		for _, d := range r.DependsOn {
			to, ok, inTemplate := resolveDep(d, resBySym, resByName, deployed)
			switch {
			case ok:
				add(Edge{from, to, EdgeDependsOn})
			case inTemplate:
				g.Unresolved = append(g.Unresolved, Unresolved{From: from, Ref: d, Kind: EdgeDependsOn, InTemplate: true})
			default:
				g.Unresolved = append(g.Unresolved, Unresolved{From: from, Ref: d, Kind: EdgeDependsOn})
			}
		}
	}

	for id, n := range g.byID {
		_ = id
		g.Nodes = append(g.Nodes, n)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Kind < b.Kind
	})
	sort.Slice(g.Unresolved, func(i, j int) bool {
		a, b := g.Unresolved[i], g.Unresolved[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		return a.Kind < b.Kind
	})
	for _, e := range g.Edges {
		g.out[e.From] = append(g.out[e.From], e)
		g.in[e.To] = append(g.in[e.To], e)
	}
	return g, nil
}

// resolveDep resolves an ARM dependsOn entry: a symbolic name, a plain name,
// or a resourceId-style expression / resource ID. Expressions are resolved by
// resource type plus name segments (armexpr), so compiled names such as
// format('ai-{0}', uniqueString(...)) resolve to the in-template resource that
// carries the same expression. inTemplate is true when no resource matched
// provably but one of the same type could not be ruled out.
func resolveDep(d string, sym map[string]string, byName map[string][]string, all []Resource) (id string, ok, inTemplate bool) {
	if id, ok := sym[d]; ok {
		return id, true, false
	}
	if !armexpr.IsExpression(d) && !strings.Contains(strings.ToLower(d), "/providers/") {
		if ids := byName[d]; len(ids) == 1 {
			return ids[0], true, false
		}
		return "", false, false
	}
	ref := armexpr.ParseRef(d)
	if ref.Opaque {
		return "", false, false
	}
	var match string
	same := 0
	for _, r := range all {
		if r.Type == "" || r.Name == "" {
			continue
		}
		switch armexpr.Compare(ref, armexpr.ParseName(r.Type, r.Name)) {
		case armexpr.Same:
			match = ResourceID(r.ID)
			same++
		case armexpr.Unknown:
			inTemplate = true
		}
	}
	if same == 1 {
		return match, true, false
	}
	return "", false, inTemplate || same > 1
}

// Node returns a node by id.
func (g *Graph) Node(id string) (Node, bool) { n, ok := g.byID[id]; return n, ok }

// Out returns outgoing edges of id (sorted).
func (g *Graph) Out(id string) []Edge { return append([]Edge(nil), g.out[id]...) }

// In returns incoming edges of id (sorted).
func (g *Graph) In(id string) []Edge { return append([]Edge(nil), g.in[id]...) }

// Reachable returns the sorted ids reachable from id (excluding id unless on a cycle).
func (g *Graph) Reachable(id string) []string {
	seen := map[string]bool{}
	stack := []string{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range g.out[cur] {
			if !seen[e.To] {
				seen[e.To] = true
				stack = append(stack, e.To)
			}
		}
	}
	res := make([]string, 0, len(seen))
	for k := range seen {
		res = append(res, k)
	}
	sort.Strings(res)
	return res
}

// Cycles returns the strongly connected components that contain a cycle
// (size>1 or a self loop), each sorted, in sorted order.
func (g *Graph) Cycles() [][]string {
	index := map[string]int{}
	low := map[string]int{}
	on := map[string]bool{}
	var stack []string
	var res [][]string
	next := 0
	var strong func(v string)
	strong = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		on[v] = true
		for _, e := range g.out[v] {
			w := e.To
			if _, ok := index[w]; !ok {
				strong(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if on[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			self := false
			for _, e := range g.out[v] {
				if e.To == v {
					self = true
				}
			}
			if len(comp) > 1 || self {
				sort.Strings(comp)
				res = append(res, comp)
			}
		}
	}
	for _, n := range g.Nodes {
		if _, ok := index[n.ID]; !ok {
			strong(n.ID)
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i][0] < res[j][0] })
	return res
}
