package graphreport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

func reportGraph() view.Graph {
	return view.Graph{
		SchemaVersion: view.SchemaVersion,
		Mode:          view.ModeCombined,
		Nodes: []view.Node{
			{ID: "n1", Label: "proj", Kind: "project", Origin: view.OriginLocal, Conditional: true},
			{ID: "n2", Label: "gpt|4o <b>", Kind: "model", Origin: view.OriginBoth, Severity: "error", Unhealthy: true, Findings: 2, Health: "failed"},
			{ID: "n3", Label: "Collapsed azure storage nodes", Kind: "storage", Origin: view.OriginAzure, Collapsed: true},
			{ID: "n4", Label: "role", Kind: "role", Origin: view.OriginUnobserved, Baselined: 1},
		},
		Edges: []view.Edge{
			{From: "n1", To: "n2", Kind: "deploys"},
			{From: "n1", To: "n3", Kind: "uses", Collapsed: true},
		},
		Summary: view.Summary{Mode: view.ModeCombined, NodeCount: 4, EdgeCount: 2, CollapsedNodes: 3, CollapsedEdges: 1, UnmatchedFindings: 2,
			Notes: []string{"project capability hosts overlay skipped: not readable with the current permissions or unavailable"}},
	}
}

func TestMarkdownGolden(t *testing.T) {
	checkGolden(t, "graph.md.golden", Markdown(reportGraph()))
}

func TestHTMLGolden(t *testing.T) {
	got := HTML(reportGraph())
	checkGolden(t, "graph.html.golden", got)
	if strings.Contains(string(got), "<b>") {
		t.Fatalf("label markup must be escaped: %s", got)
	}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s: %v", name, err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != string(got) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// Phase 5 gate (black belt N1): a hostile label from azure.yaml must not become
// a live Markdown link or image in the node table.
func TestMarkdownHostileLabelIsNotALink(t *testing.T) {
	g := reportGraph()
	g.Nodes[0].Label = "[click](javascript:alert(1))![i](http://evil/x.png)"
	// The Mermaid fence holds the raw label and is inert; only table rows render.
	for _, line := range strings.Split(string(Markdown(g)), "\n") {
		if strings.HasPrefix(line, "|") && strings.Contains(line, "](") {
			t.Fatalf("table row produced Markdown link syntax: %s", line)
		}
	}
}
