package view

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
)

// With a parameter file the graph carries the resolved names and conditions
// (a module whose condition resolved to true adds no guard, ADR-018), and the
// placement of every resource stays parallel to the resources.
func TestModelResourcesWithParameterValues(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters:         map[string]any{"prefix": "dev", "deploySearch": true},
	})
	rs, ps := m.Resources(), m.Placements()
	if len(rs) != len(ps) {
		t.Fatalf("%d resources, %d placements", len(rs), len(ps))
	}
	cr := ModelResources(rs, ps)
	names := map[string]string{}
	for _, r := range cr {
		names[r.Name] = r.Condition
	}
	if _, ok := names["dev-ai"]; !ok {
		t.Errorf("resolved name missing: %v", names)
	}
	if c, ok := names["dev-msrch"]; !ok || c != "" {
		t.Errorf("module resource condition %q: a module deployed unconditionally adds no guard", c)
	}
	if _, err := coregraph.Build(coregraph.Input{Resources: cr}); err != nil {
		t.Fatal(err)
	}
}
