package view

import (
	"reflect"
	"testing"
)

func diffInputs() (Graph, Graph) {
	base := Graph{SchemaVersion: SchemaVersion, Mode: ModeDeployed,
		Nodes: []Node{
			{ID: "n1", Label: "acct", Kind: "foundry-account", Origin: OriginAzure, Type: "Microsoft.CognitiveServices/accounts", Name: "acct", Resource: "/subscriptions/s/resourceGroups/dev/providers/Microsoft.CognitiveServices/accounts/acct"},
			{ID: "n2", Label: "appi", Kind: "monitoring", Origin: OriginAzure, Type: "Microsoft.Insights/components", Name: "appi", Unhealthy: true, Health: "failed"},
			{ID: "n3", Label: "old", Kind: "storage", Origin: OriginAzure, Type: "Microsoft.Storage/storageAccounts", Name: "old"},
		},
		Edges: []Edge{{From: "n1", To: "n3", Kind: "usesIdentity"}, {From: "n1", To: "n2", Kind: "uses"}},
	}
	target := Graph{SchemaVersion: SchemaVersion, Mode: ModeDeployed,
		Nodes: []Node{
			{ID: "n1", Label: "acct", Kind: "foundry-account", Origin: OriginAzure, Type: "Microsoft.CognitiveServices/accounts", Name: "acct", Resource: "/subscriptions/s/resourceGroups/prod/providers/Microsoft.CognitiveServices/accounts/acct"},
			{ID: "n2", Label: "appi", Kind: "monitoring", Origin: OriginAzure, Type: "Microsoft.Insights/components", Name: "appi", Health: "succeeded"},
			{ID: "n3", Label: "new", Kind: "search", Origin: OriginAzure, Type: "Microsoft.Search/searchServices", Name: "new"},
		},
		Edges: []Edge{{From: "n1", To: "n2", Kind: "uses"}, {From: "n1", To: "n3", Kind: "uses"}},
	}
	return base, target
}

func TestDiffMarksAddedRemovedChanged(t *testing.T) {
	base, target := diffInputs()
	g, err := Diff(base, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if g.Mode != ModeDiff || g.Summary.Mode != ModeDiff {
		t.Fatalf("mode: %+v", g.Summary)
	}
	want := map[string]string{"acct": "", "appi": DiffChanged, "old": DiffRemoved, "new": DiffAdded}
	for _, n := range g.Nodes {
		w, ok := want[n.Label]
		if !ok || n.Diff != w {
			t.Errorf("%s: diff %q, want %q", n.Label, n.Diff, w)
		}
		delete(want, n.Label)
	}
	if len(want) != 0 {
		t.Fatalf("nodes missing from diff: %v", want)
	}
	if g.Summary.AddedNodes != 1 || g.Summary.RemovedNodes != 1 || g.Summary.ChangedNodes != 1 || g.Summary.NodeCount != 4 {
		t.Fatalf("summary: %+v", g.Summary)
	}
	acct, appi := nodeByLabel(t, g, "acct"), nodeByLabel(t, g, "appi")
	if appi.Unhealthy || appi.Health != "succeeded" {
		t.Fatalf("changed node must carry the target state: %+v", appi)
	}
	if e, ok := hasEdge(g, acct.ID, nodeByLabel(t, g, "old").ID, "usesIdentity"); !ok || e.Diff != DiffRemoved {
		t.Fatalf("want removed edge: %+v", g.Edges)
	}
	if e, ok := hasEdge(g, acct.ID, nodeByLabel(t, g, "new").ID, "uses"); !ok || e.Diff != DiffAdded {
		t.Fatalf("want added edge: %+v", g.Edges)
	}
	if e, ok := hasEdge(g, acct.ID, appi.ID, "uses"); !ok || e.Diff != "" {
		t.Fatalf("want unchanged edge: %+v", g.Edges)
	}
}

func TestDiffIsDeterministicAndOrderIndependent(t *testing.T) {
	base, target := diffInputs()
	first, err := Diff(base, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	base.Nodes[0], base.Nodes[2] = base.Nodes[2], base.Nodes[0]
	base.Edges[0], base.Edges[1] = base.Edges[1], base.Edges[0]
	second, err := Diff(base, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Summary, second.Summary) || len(first.Nodes) != len(second.Nodes) {
		t.Fatalf("diff depends on input order:\n%+v\n%+v", first, second)
	}
	for i := range first.Nodes {
		if first.Nodes[i].Label != second.Nodes[i].Label || first.Nodes[i].Diff != second.Nodes[i].Diff {
			t.Fatalf("node %d differs: %+v vs %+v", i, first.Nodes[i], second.Nodes[i])
		}
	}
}

func TestDiffRejectsDiffAndUnknownSchema(t *testing.T) {
	base, target := diffInputs()
	d, err := Diff(base, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Diff(d, target, Options{}); err == nil {
		t.Fatal("diffing a diff graph must fail")
	}
	target.SchemaVersion = "99"
	if _, err := Diff(base, target, Options{}); err == nil {
		t.Fatal("unknown schemaVersion must fail")
	}
}

func TestDiffRedactsIDs(t *testing.T) {
	base, target := diffInputs()
	g, err := Diff(base, target, Options{RedactIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Name != "" || n.Resource != "" || n.Label == "acct" {
			t.Fatalf("diff output not redacted: %+v", n)
		}
	}
}
