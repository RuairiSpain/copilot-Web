package mermaid

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderGolden(t *testing.T) {
	g := view.Graph{
		Mode: view.ModeCombined,
		Nodes: []view.Node{
			{ID: "n1", Label: "proj", Kind: "project", Origin: view.OriginLocal},
			{ID: "n2", Label: "gpt-4o-mini", Kind: "model", Origin: view.OriginBoth, Severity: "error", Unhealthy: true},
			{ID: "n3", Label: "storage", Kind: "storage", Origin: view.OriginAzure},
		},
		Edges: []view.Edge{
			{From: "n1", To: "n2", Kind: "deploys"},
			{From: "n1", To: "n3", Kind: "uses"},
		},
		Summary: view.Summary{Mode: view.ModeCombined, NodeCount: 3, EdgeCount: 2},
	}
	got := Render(g)
	checkGolden(t, "graph.mmd.golden", got)
}

func TestRenderEscapesInjection(t *testing.T) {
	g := view.Graph{
		Mode:  view.ModeSource,
		Nodes: []view.Node{{ID: "n1", Label: `x"]-->evil`, Kind: "service", Origin: view.OriginLocal}},
	}
	got := string(Render(g))
	if strings.Contains(got, `"]-->evil`) || strings.Contains(got, "-->evil") || strings.Contains(got, "`") {
		t.Fatalf("injection not escaped: %s", got)
	}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s: %v", name, err)
	}
	norm := func(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }
	if norm(want) != norm(got) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
