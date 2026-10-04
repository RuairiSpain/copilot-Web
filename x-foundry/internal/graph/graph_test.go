package graph_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/graph"
)

func build(t *testing.T, stages map[string]int, edges [][2]string) *graph.Graph {
	t.Helper()
	g := graph.New()
	for id, stage := range stages {
		g.AddNode(graph.Node{ID: id, Kind: "k", Stage: stage})
	}
	for _, e := range edges {
		if err := g.AddDependency(e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

func TestTopologicalOrderPutsDependenciesFirstAndBreaksTiesByStageThenID(t *testing.T) {
	g := build(t, map[string]int{"c": 2, "a": 1, "b": 1, "d": 0}, [][2]string{{"c", "a"}, {"a", "d"}, {"b", "d"}})
	order, err := g.TopologicalOrder()
	if err != nil || !reflect.DeepEqual(order, []string{"d", "a", "b", "c"}) {
		t.Fatalf("order = %v, err = %v", order, err)
	}
	layers, err := g.Layers()
	if err != nil || !reflect.DeepEqual(layers, [][]string{{"d"}, {"a", "b"}, {"c"}}) {
		t.Fatalf("layers = %v, err = %v", layers, err)
	}
	if !reflect.DeepEqual(g.DependenciesOf("c"), []string{"a"}) || !reflect.DeepEqual(g.DependentsOf("d"), []string{"a", "b"}) {
		t.Fatal("dependency queries")
	}
	if !reflect.DeepEqual(g.IDs(), g.IDs()) || len(g.IDs()) != 4 {
		t.Fatal("IDs")
	}
}

func TestAddNodeIsIdempotent(t *testing.T) {
	g := graph.New()
	g.AddNode(graph.Node{ID: "a", Kind: "first"})
	g.AddNode(graph.Node{ID: "a", Kind: "second"})
	if g.Node("a").Kind != "first" || !g.Has("a") || g.Has("b") {
		t.Fatal("AddNode must keep the first definition")
	}
}

func TestCyclesAreDetected(t *testing.T) {
	g := build(t, map[string]int{"a": 0, "b": 0, "c": 0}, [][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}})
	_, err := g.TopologicalOrder()
	var cycle *graph.CycleError
	if !errors.As(err, &cycle) || !reflect.DeepEqual(cycle.Nodes, []string{"a", "b", "c"}) || !strings.Contains(err.Error(), "a, b, c") {
		t.Fatalf("err = %v", err)
	}
	if _, err := g.Layers(); err == nil {
		t.Fatal("Layers must fail on a cycle")
	}
}

func TestInvalidEdges(t *testing.T) {
	g := graph.New()
	g.AddNode(graph.Node{ID: "a"})
	var cycle *graph.CycleError
	if err := g.AddDependency("a", "a"); !errors.As(err, &cycle) {
		t.Fatalf("self dependency: %v", err)
	}
	if err := g.AddDependency("a", "ghost"); err == nil {
		t.Fatal("unknown dependency")
	}
	if err := g.AddDependency("ghost", "a"); err == nil {
		t.Fatal("unknown node")
	}
}

func TestEmptyGraph(t *testing.T) {
	g := graph.New()
	order, err := g.TopologicalOrder()
	layers, err2 := g.Layers()
	if err != nil || err2 != nil || len(order) != 0 || len(layers) != 0 {
		t.Fatal("empty graph")
	}
}
