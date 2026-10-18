package view

import (
	"cmp"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Diff builds the environment-difference graph of two graphs rendered by
// Build (for example `graph --deployed --format json` for two resource
// groups). Nodes are matched by kind, type and name (the label when the name
// is empty), never by ARM ID, because IDs differ between environments by
// design. A node only in target is added, only in base is removed, and in both
// with a different origin, health, finding or reference state is changed.
// Edges are matched by their endpoints' identities and kind and are added or
// removed. Node IDs are reassigned in identity order, so the output is
// deterministic for the same inputs. Collapse limits are not applied.
func Diff(base, target Graph, opt Options) (Graph, error) {
	for _, in := range []struct {
		name string
		g    Graph
	}{{"base", base}, {"target", target}} {
		if in.g.SchemaVersion != "" && in.g.SchemaVersion != SchemaVersion {
			return Graph{}, fmt.Errorf("%s graph: unsupported schemaVersion %q (want %q)", in.name, in.g.SchemaVersion, SchemaVersion)
		}
		if in.g.Mode == ModeDiff {
			return Graph{}, fmt.Errorf("%s graph: a diff graph cannot be diffed again", in.name)
		}
	}
	baseKeys := identityKeys(base.Nodes)
	targetKeys := identityKeys(target.Nodes)
	type pair struct{ base, target *Node }
	pairs := map[string]*pair{}
	get := func(k string) *pair {
		p, ok := pairs[k]
		if !ok {
			p = &pair{}
			pairs[k] = p
		}
		return p
	}
	for i := range base.Nodes {
		get(baseKeys[base.Nodes[i].ID]).base = &base.Nodes[i]
	}
	for i := range target.Nodes {
		get(targetKeys[target.Nodes[i].ID]).target = &target.Nodes[i]
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := Graph{SchemaVersion: SchemaVersion, Mode: ModeDiff, Nodes: make([]Node, 0, len(keys)), Edges: []Edge{}}
	idByKey := map[string]string{}
	for i, k := range keys {
		p := pairs[k]
		var n Node
		switch {
		case p.base == nil:
			n = *p.target
			n.Diff = DiffAdded
			out.Summary.AddedNodes++
		case p.target == nil:
			n = *p.base
			n.Diff = DiffRemoved
			out.Summary.RemovedNodes++
		default:
			n = *p.target
			n.Diff = ""
			if nodeChanged(*p.base, *p.target) {
				n.Diff = DiffChanged
				out.Summary.ChangedNodes++
			}
		}
		n.ID = "n" + strconv.Itoa(i+1)
		idByKey[k] = n.ID
		out.Nodes = append(out.Nodes, n)
	}

	type edgeState struct {
		edge           Edge
		inBase, inTarg bool
	}
	edges := map[string]*edgeState{}
	collect := func(g Graph, ids map[string]string, isBase bool) {
		for _, e := range g.Edges {
			from, to := ids[e.From], ids[e.To]
			if from == "" || to == "" {
				continue
			}
			k := from + "\x01" + to + "\x01" + e.Kind
			st, ok := edges[k]
			if !ok {
				st = &edgeState{edge: Edge{From: idByKey[from], To: idByKey[to], Kind: e.Kind}}
				edges[k] = st
			}
			st.edge.External = st.edge.External || e.External
			st.edge.Missing = st.edge.Missing || e.Missing
			st.edge.Collapsed = st.edge.Collapsed || e.Collapsed
			if isBase {
				st.inBase = true
			} else {
				st.inTarg = true
			}
		}
	}
	collect(base, baseKeys, true)
	collect(target, targetKeys, false)
	for _, st := range edges {
		e := st.edge
		switch {
		case st.inBase && !st.inTarg:
			e.Diff = DiffRemoved
		case st.inTarg && !st.inBase:
			e.Diff = DiffAdded
		}
		out.Edges = append(out.Edges, e)
	}
	sort.Slice(out.Edges, func(i, j int) bool {
		a, b := out.Edges[i], out.Edges[j]
		return cmp.Or(
			cmp.Compare(nodeOrdinal(a.From), nodeOrdinal(b.From)),
			cmp.Compare(nodeOrdinal(a.To), nodeOrdinal(b.To)),
			strings.Compare(a.Kind, b.Kind),
		) < 0
	})
	out.Summary.Mode = ModeDiff
	out.Summary.NodeCount = len(out.Nodes)
	out.Summary.EdgeCount = len(out.Edges)
	if opt.RedactIDs {
		redactGraph(&out)
	}
	return out, nil
}

// identityKeys maps each node ID to its environment-independent identity.
// Duplicate identities within one graph get a deterministic ordinal suffix.
func identityKeys(nodes []Node) map[string]string {
	type entry struct {
		id, base, tie string
	}
	entries := make([]entry, 0, len(nodes))
	for _, n := range nodes {
		name := n.Name
		if strings.TrimSpace(name) == "" {
			name = n.Label
		}
		base := strings.ToLower(n.Kind) + "\x00" + strings.ToLower(n.Type) + "\x00" + strings.ToLower(name)
		entries = append(entries, entry{id: n.ID, base: base, tie: n.Label + "\x00" + n.Resource + "\x00" + string(n.Origin)})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return cmp.Or(strings.Compare(entries[i].base, entries[j].base), strings.Compare(entries[i].tie, entries[j].tie)) < 0
	})
	out := make(map[string]string, len(entries))
	seen := map[string]int{}
	for _, e := range entries {
		seen[e.base]++
		k := e.base
		if n := seen[e.base]; n > 1 {
			k += "\x00#" + strconv.Itoa(n)
		}
		out[e.id] = k
	}
	return out
}

func nodeChanged(a, b Node) bool {
	return a.Origin != b.Origin || a.Unhealthy != b.Unhealthy || a.Health != b.Health ||
		a.Findings != b.Findings || a.Baselined != b.Baselined || a.Severity != b.Severity ||
		a.Missing != b.Missing || a.External != b.External || a.Conditional != b.Conditional
}

// nodeOrdinal orders "n12" after "n2".
func nodeOrdinal(id string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "n"))
	if err != nil {
		return -1
	}
	return n
}
