package graphjson

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func goldenGraph() view.Graph {
	return view.Graph{
		SchemaVersion: view.SchemaVersion,
		Mode:          view.ModeCombined,
		Nodes: []view.Node{
			{ID: "n1", Label: "proj", Kind: "project", Origin: view.OriginLocal, Type: "azure.ai.project", Name: "proj", Source: sdk.Location{File: "azure.yaml", Line: 4, Column: 11}},
			{ID: "n2", Label: "st", Kind: "storage", Origin: view.OriginBoth, Type: "Microsoft.Storage/storageAccounts", Name: "st",
				Resource: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/st", Findings: 1, Severity: sdk.SeverityError, Unhealthy: true, Health: "succeeded"},
			{ID: "n3", Label: "external-url", Kind: "external", Origin: view.OriginExternal, External: true},
		},
		Edges: []view.Edge{
			{From: "n1", To: "n2", Kind: "uses"},
			{From: "n1", To: "n3", Kind: "targets", External: true},
		},
		Summary: view.Summary{Mode: view.ModeCombined, NodeCount: 3, EdgeCount: 2, UnmatchedFindings: 1,
			Notes: []string{"project capability hosts overlay skipped: not readable with the current permissions or unavailable"}},
	}
}

func TestRenderGolden(t *testing.T) {
	got, err := Render(goldenGraph())
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "graph.json.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestRenderMatchesSchema validates real Build and Diff output against
// schemas/graph.schema.json: required fields, types, enums and that every
// emitted field is documented in the schema.
func TestRenderMatchesSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "graph.schema.json"))
	if err != nil {
		t.Fatalf("missing schema file: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	d, err := azureyaml.Parse([]byte("name: s\nservices:\n  proj:\n    host: azure.ai.project\n  agent:\n    host: azure.ai.agent\n    uses: [proj, ghost]\n"), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	stID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/st"
	in := view.Input{
		Source: view.SourceInput{
			Document: d,
			Services: []coregraph.Service{{Name: "proj", Host: "azure.ai.project"}, {Name: "agent", Host: "azure.ai.agent", Uses: []string{"proj", "ghost"}}},
			Resources: []coregraph.Resource{
				{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "st"},
				{ID: "/resources/1", Type: "Microsoft.Authorization/roleAssignments", Name: "ra", Conditional: true, Condition: "[parameters('x')]"},
			},
		},
		Inventory: []azure.Resource{
			{ID: stID, Name: "st", Type: "Microsoft.Storage/storageAccounts", Properties: map[string]string{"properties.provisioningState": "Failed"}},
		},
		Findings: []sdk.Finding{
			{RuleID: "FND-X", Severity: sdk.SeverityWarning, Resource: sdk.ResourceRef{ID: stID}},
			{RuleID: "FND-Y", Severity: sdk.SeverityError, Resource: sdk.ResourceRef{Type: "Microsoft.Storage/storageAccounts", Name: "st"}, Baselined: true},
			{RuleID: "FND-Z", Severity: sdk.SeverityInfo, Resource: sdk.ResourceRef{Type: "nope", Name: "none"}},
		},
	}
	combined, err := view.Build(in, view.Options{Mode: view.ModeCombined, CollapseAfter: 3, MaxEdges: 1})
	if err != nil {
		t.Fatal(err)
	}
	source, err := view.Build(in, view.Options{Mode: view.ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := view.Diff(source, combined, view.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, g := range map[string]view.Graph{"golden": goldenGraph(), "combined": combined, "source": source, "diff": diff} {
		out, err := Render(g)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatal(err)
		}
		for _, problem := range validate(doc, schema, "$") {
			t.Errorf("%s: %s", name, problem)
		}
	}
}

// validate checks the subset of JSON Schema that graph.schema.json uses. It
// also reports properties the schema does not document.
func validate(v any, s map[string]any, path string) []string {
	var out []string
	if typ, ok := s["type"].(string); ok && !hasType(v, typ) {
		return []string{fmt.Sprintf("%s: want %s, got %T", path, typ, v)}
	}
	if c, ok := s["const"]; ok && v != c {
		out = append(out, fmt.Sprintf("%s: want const %v, got %v", path, c, v))
	}
	if enum, ok := s["enum"].([]any); ok && !slices.Contains(enum, v) {
		out = append(out, fmt.Sprintf("%s: %v not in enum %v", path, v, enum))
	}
	if minimum, ok := s["minimum"].(float64); ok {
		if f, ok := v.(float64); ok && f < minimum {
			out = append(out, fmt.Sprintf("%s: %v below minimum %v", path, f, minimum))
		}
	}
	switch val := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if _, ok := val[r.(string)]; !ok {
					out = append(out, fmt.Sprintf("%s: missing required %q", path, r))
				}
			}
		}
		if props != nil {
			for k, child := range val {
				ps, ok := props[k].(map[string]any)
				if !ok {
					out = append(out, fmt.Sprintf("%s: property %q is not documented in the schema", path, k))
					continue
				}
				out = append(out, validate(child, ps, path+"."+k)...)
			}
		}
	case []any:
		if items, ok := s["items"].(map[string]any); ok {
			for i, item := range val {
				out = append(out, validate(item, items, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}
	return out
}

func hasType(v any, typ string) bool {
	switch typ {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	default:
		return true
	}
}
