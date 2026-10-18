package graphviz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

func TestRenderDOTEscapesLabels(t *testing.T) {
	got := string(RenderDOT(view.Graph{
		Nodes: []view.Node{{ID: "n1", Label: `x"; y->z`, Kind: "service"}},
	}))
	if strings.Contains(got, `"; y->z`) || strings.Contains(got, "->z") {
		t.Fatalf("dot injection not escaped: %s", got)
	}
}

func TestRenderDOTGolden(t *testing.T) {
	g := view.Graph{
		Mode: view.ModeCombined,
		Nodes: []view.Node{
			{ID: "n1", Label: "proj", Kind: "project", Origin: view.OriginLocal},
			{ID: "n2", Label: "gpt-4o-mini", Kind: "model", Origin: view.OriginBoth, Severity: "error", Unhealthy: true},
			{ID: "n3", Label: `st"; x->y`, Kind: "storage", Origin: view.OriginUnobserved},
			{ID: "n4", Label: "ghost", Kind: "missing", Origin: view.OriginMissing, Missing: true},
		},
		Edges: []view.Edge{
			{From: "n1", To: "n2", Kind: "deploys"},
			{From: "n1", To: "n4", Kind: "uses", Missing: true},
		},
	}
	want, err := os.ReadFile(filepath.Join("testdata", "graph.dot.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(RenderDOT(g)); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMissingBinary(t *testing.T) {
	_, err := (Binary{LookPath: func(string) (string, error) { return "", errors.New("missing") }}).Render(context.Background(), []byte("digraph{}"), "svg")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}
