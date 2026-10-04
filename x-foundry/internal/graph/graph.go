// Package graph builds the deployment dependency graph and orders it.
package graph

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"
)

// CycleError reports a dependency cycle.
type CycleError struct{ Nodes []string }

// Error implements error.
func (e *CycleError) Error() string { return "dependency cycle among: " + strings.Join(e.Nodes, ", ") }

// Node is a logical resource.
type Node struct {
	ID       string
	Kind     string
	Stage    int
	Scope    string
	Existing bool
}

// Graph holds nodes; an edge a -> b means a depends on b.
type Graph struct {
	nodes map[string]Node
	deps  map[string]map[string]bool
	order []string // insertion order, for deterministic iteration
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{nodes: map[string]Node{}, deps: map[string]map[string]bool{}}
}

// AddNode adds a node; adding an existing id is a no-op.
func (g *Graph) AddNode(n Node) {
	if _, ok := g.nodes[n.ID]; ok {
		return
	}
	g.nodes[n.ID] = n
	g.deps[n.ID] = map[string]bool{}
	g.order = append(g.order, n.ID)
}

// Has reports whether the node exists.
func (g *Graph) Has(id string) bool { _, ok := g.nodes[id]; return ok }

// Node returns a node.
func (g *Graph) Node(id string) Node { return g.nodes[id] }

// IDs returns node ids in insertion order.
func (g *Graph) IDs() []string { return append([]string(nil), g.order...) }

// AddDependency records that id depends on dep.
func (g *Graph) AddDependency(id, dep string) error {
	if !g.Has(id) {
		return fmt.Errorf("unknown node %q", id)
	}
	if !g.Has(dep) {
		return fmt.Errorf("%q depends on unknown node %q", id, dep)
	}
	if id == dep {
		return &CycleError{Nodes: []string{id}}
	}
	g.deps[id][dep] = true
	return nil
}

// DependenciesOf returns the sorted dependencies of id.
func (g *Graph) DependenciesOf(id string) []string {
	out := make([]string, 0, len(g.deps[id]))
	for d := range g.deps[id] {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// DependentsOf returns the sorted nodes that depend on id.
func (g *Graph) DependentsOf(id string) []string {
	var out []string
	for n, d := range g.deps {
		if d[id] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

type item struct {
	stage int
	id    string
}
type queue []item

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if q[i].stage != q[j].stage {
		return q[i].stage < q[j].stage
	}
	return q[i].id < q[j].id
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)   { *q = append(*q, x.(item)) }
func (q *queue) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}

// TopologicalOrder lists dependencies first. Ties are broken by deployment stage, then id.
func (g *Graph) TopologicalOrder() ([]string, error) {
	remaining := map[string]map[string]bool{}
	dependents := map[string][]string{}
	for id, ds := range g.deps {
		remaining[id] = map[string]bool{}
		for d := range ds {
			remaining[id][d] = true
			dependents[d] = append(dependents[d], id)
		}
	}
	q := &queue{}
	for id, ds := range remaining {
		if len(ds) == 0 {
			heap.Push(q, item{g.nodes[id].Stage, id})
		}
	}
	order := make([]string, 0, len(g.nodes))
	for q.Len() > 0 {
		cur := heap.Pop(q).(item).id
		order = append(order, cur)
		for _, child := range dependents[cur] {
			delete(remaining[child], cur)
			if len(remaining[child]) == 0 {
				heap.Push(q, item{g.nodes[child].Stage, child})
			}
		}
	}
	if len(order) != len(g.nodes) {
		done := map[string]bool{}
		for _, id := range order {
			done[id] = true
		}
		var stuck []string
		for id := range g.nodes {
			if !done[id] {
				stuck = append(stuck, id)
			}
		}
		sort.Strings(stuck)
		return nil, &CycleError{Nodes: stuck}
	}
	return order, nil
}

// Layers groups nodes that can be deployed in parallel, in order.
func (g *Graph) Layers() ([][]string, error) {
	order, err := g.TopologicalOrder()
	if err != nil {
		return nil, err
	}
	depth := map[string]int{}
	byDepth := map[int][]string{}
	max := -1
	for _, id := range order {
		d := 0
		for dep := range g.deps[id] {
			if depth[dep]+1 > d {
				d = depth[dep] + 1
			}
		}
		depth[id] = d
		byDepth[d] = append(byDepth[d], id)
		if d > max {
			max = d
		}
	}
	layers := make([][]string, 0, max+1)
	for d := 0; d <= max; d++ {
		sort.Strings(byDepth[d])
		layers = append(layers, byDepth[d])
	}
	return layers, nil
}
