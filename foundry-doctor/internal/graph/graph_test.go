package graph

import (
	"reflect"
	"testing"
)

func sample() Input {
	return Input{
		Services: []Service{
			{Name: "web", Host: "containerapp", Uses: []string{"api", "store", "missing"}},
			{Name: "api", Host: "containerapp", Uses: []string{"kv"}, Resource: "apiapp"},
		},
		Resources: []Resource{
			{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "kv", SymbolicName: "store"},
			{ID: "/resources/1", Type: "Microsoft.App/containerApps", Name: "apiapp", DependsOn: []string{"store", "[resourceId('Microsoft.Storage/storageAccounts', 'kv')]", "ghost"}},
		},
	}
}

func TestBuild(t *testing.T) {
	g, err := Build(sample())
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 4 {
		t.Fatalf("nodes %d", len(g.Nodes))
	}
	want := []Edge{
		{"resource:/resources/1", "resource:/resources/0", EdgeDependsOn},
		{"service:api", "resource:/resources/0", EdgeUses},
		{"service:api", "resource:/resources/1", EdgeHosts},
		{"service:web", "resource:/resources/0", EdgeUses},
		{"service:web", "service:api", EdgeUses},
	}
	if !reflect.DeepEqual(g.Edges, want) {
		t.Fatalf("edges %+v", g.Edges)
	}
	if len(g.Unresolved) != 2 || g.Unresolved[0].Ref != "ghost" || g.Unresolved[1].Ref != "missing" {
		t.Fatalf("unresolved %+v", g.Unresolved)
	}
	if got := g.Reachable("service:web"); len(got) != 3 {
		t.Errorf("reachable %v", got)
	}
	if len(g.In("resource:/resources/0")) != 3 || len(g.Out("service:web")) != 2 {
		t.Error("in/out")
	}
	if _, ok := g.Node("service:web"); !ok {
		t.Error("node lookup")
	}
	if c := g.Cycles(); len(c) != 0 {
		t.Errorf("unexpected cycles %v", c)
	}
}

func TestBuildDeterministic(t *testing.T) {
	a, _ := Build(sample())
	for i := 0; i < 20; i++ {
		b, _ := Build(sample())
		if !reflect.DeepEqual(a.Edges, b.Edges) || !reflect.DeepEqual(a.Nodes, b.Nodes) {
			t.Fatal("non-deterministic")
		}
	}
}

func TestBuildErrors(t *testing.T) {
	tests := []struct {
		name string
		in   Input
	}{
		{"empty service", Input{Services: []Service{{}}}},
		{"dup service", Input{Services: []Service{{Name: "a"}, {Name: "a"}}}},
		{"empty resource id", Input{Resources: []Resource{{}}}},
		{"dup resource", Input{Resources: []Resource{{ID: "x"}, {ID: "x"}}}},
	}
	for _, tc := range tests {
		if _, err := Build(tc.in); err == nil {
			t.Errorf("%s: want error", tc.name)
		}
	}
}

func TestBuildEmpty(t *testing.T) {
	g, err := Build(Input{})
	if err != nil || len(g.Nodes) != 0 || len(g.Cycles()) != 0 {
		t.Fatal("empty graph")
	}
}

func TestAmbiguousName(t *testing.T) {
	g, _ := Build(Input{
		Services:  []Service{{Name: "s", Uses: []string{"dup"}}},
		Resources: []Resource{{ID: "1", Name: "dup"}, {ID: "2", Name: "dup"}},
	})
	if len(g.Edges) != 0 || len(g.Unresolved) != 1 {
		t.Fatalf("ambiguous must stay unresolved: %+v", g)
	}
}

func TestCycles(t *testing.T) {
	g, _ := Build(Input{
		Services: []Service{{Name: "a", Uses: []string{"b"}}, {Name: "b", Uses: []string{"a"}}, {Name: "self", Uses: []string{"self"}}, {Name: "ok", Uses: []string{"a"}}},
	})
	c := g.Cycles()
	want := [][]string{{"service:a", "service:b"}, {"service:self"}}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("cycles %v", c)
	}
}
