package graph

import "sort"

// Cycles returns the strongly connected components that contain a cycle (size > 1 or a self-loop),
// computed with Tarjan's algorithm over edges of the given kinds (all kinds when none are given).
// Each component and the list are sorted.
func (g *Graph) Cycles(kinds ...string) [][]string {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	adj := map[string][]string{}
	selfLoop := map[string]bool{}
	for _, e := range g.Edges {
		if len(want) > 0 && !want[e.Kind] {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		if e.From == e.To {
			selfLoop[e.From] = true
		}
	}
	ids := make([]string, 0, len(adj))
	for id := range adj {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string
	next := 0

	type frame struct {
		id string
		i  int
	}
	for _, root := range ids {
		if _, seen := index[root]; seen {
			continue
		}
		work := []frame{{id: root}}
		index[root], low[root] = next, next
		next++
		stack = append(stack, root)
		onStack[root] = true
		for len(work) > 0 {
			f := &work[len(work)-1]
			if f.i < len(adj[f.id]) {
				w := adj[f.id][f.i]
				f.i++
				if _, seen := index[w]; !seen {
					index[w], low[w] = next, next
					next++
					stack = append(stack, w)
					onStack[w] = true
					work = append(work, frame{id: w})
				} else if onStack[w] && index[w] < low[f.id] {
					low[f.id] = index[w]
				}
				continue
			}
			v := f.id
			work = work[:len(work)-1]
			if len(work) > 0 {
				p := work[len(work)-1].id
				if low[v] < low[p] {
					low[p] = low[v]
				}
			}
			if low[v] == index[v] {
				var comp []string
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				if len(comp) > 1 || selfLoop[comp[0]] {
					sort.Strings(comp)
					out = append(out, comp)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
