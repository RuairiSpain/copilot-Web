package graph

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

func graphOf(edges ...model.GraphEdge) *Graph {
	b := &builder{nodes: map[string]model.GraphNode{}, edges: map[model.GraphEdge]struct{}{}, unres: map[Unresolved]struct{}{},
		forms: map[Form]struct{}{}, resType: map[string]string{}}
	for _, e := range edges {
		b.node("n", e.From, model.Pos{})
		b.edge("n:"+e.From, "n:"+e.To, e.Kind)
	}
	return b.finish()
}

func e(from, to, kind string) model.GraphEdge { return model.GraphEdge{From: from, To: to, Kind: kind} }

func TestCycles(t *testing.T) {
	tests := []struct {
		name  string
		edges []model.GraphEdge
		kinds []string
		want  [][]string
	}{
		{"none", []model.GraphEdge{e("a", "b", "uses"), e("b", "c", "uses")}, nil, nil},
		{"self loop", []model.GraphEdge{e("a", "a", "uses")}, nil, [][]string{{"n:a"}}},
		{"two cycle", []model.GraphEdge{e("b", "a", "uses"), e("a", "b", "uses")}, nil, [][]string{{"n:a", "n:b"}}},
		{"two components sorted", []model.GraphEdge{e("z", "y", "uses"), e("y", "z", "uses"), e("b", "a", "uses"), e("a", "b", "uses")}, nil,
			[][]string{{"n:a", "n:b"}, {"n:y", "n:z"}}},
		{"cycle with tail", []model.GraphEdge{e("t", "a", "uses"), e("a", "b", "uses"), e("b", "c", "uses"), e("c", "a", "uses")}, nil,
			[][]string{{"n:a", "n:b", "n:c"}}},
		{"kind filter excludes", []model.GraphEdge{e("a", "b", "uses"), e("b", "a", "dependsOn")}, []string{"uses"}, nil},
		{"kind filter includes", []model.GraphEdge{e("a", "b", "uses"), e("b", "a", "dependsOn")}, []string{"uses", "dependsOn"}, [][]string{{"n:a", "n:b"}}},
		{"all kinds", []model.GraphEdge{e("a", "b", "uses"), e("b", "a", "dependsOn")}, nil, [][]string{{"n:a", "n:b"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graphOf(tt.edges...).Cycles(tt.kinds...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestCyclesInBuiltGraph(t *testing.T) {
	g := buildSample()
	got := g.Cycles(model.EdgeUses)
	want := [][]string{{"service:agent", "service:conn", "service:tools"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uses cycles = %v", got)
	}
	if loops := g.Cycles(EdgeReferences); len(loops) != 1 || loops[0][0] != "arm-resource:microsoft.foo/bar/loop" {
		t.Errorf("self reference not reported: %v", loops)
	}
}

func TestCyclesLargeChainAndRing(t *testing.T) {
	const n = 20000
	var edges []model.GraphEdge
	for i := 0; i < n-1; i++ {
		edges = append(edges, e(fmt.Sprintf("%06d", i), fmt.Sprintf("%06d", i+1), "uses"))
	}
	if c := graphOf(edges...).Cycles(); c != nil {
		t.Fatalf("chain has no cycle, got %d", len(c))
	}
	edges = append(edges, e(fmt.Sprintf("%06d", n-1), "000000", "uses"))
	c := graphOf(edges...).Cycles()
	if len(c) != 1 || len(c[0]) != n {
		t.Fatalf("ring: %d components", len(c))
	}
}
