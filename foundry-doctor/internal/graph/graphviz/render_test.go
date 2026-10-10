package graphviz

import (
	"context"
	"errors"
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

func TestMissingBinary(t *testing.T) {
	_, err := (Binary{LookPath: func(string) (string, error) { return "", errors.New("missing") }}).Render(context.Background(), []byte("digraph{}"), "svg")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}
