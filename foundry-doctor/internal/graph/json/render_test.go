package graphjson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

func TestRenderMatchesSchemaEnvelope(t *testing.T) {
	got, err := Render(view.Graph{
		SchemaVersion: "1",
		Mode:          view.ModeSource,
		Nodes:         []view.Node{{ID: "n1", Label: "proj", Kind: "project", Origin: view.OriginLocal}},
		Edges:         []view.Edge{},
		Summary:       view.Summary{Mode: view.ModeSource, NodeCount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schemaVersion", "mode", "nodes", "edges", "summary"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing %s in %s", key, got)
		}
	}
	if _, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "graph.schema.json")); err != nil {
		t.Fatalf("missing schema file: %v", err)
	}
}
