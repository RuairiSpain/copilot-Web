package view

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
)

func edgeBetween(t *testing.T, scenario, fromSuffix, toType string) bool {
	t.Helper()
	m := rulestest.Scenario(t, scenario)
	rs := m.Resources()
	g, err := coregraph.Build(coregraph.Input{Resources: ModelResources(rs, m.Placements())})
	if err != nil {
		t.Fatal(err)
	}
	typeOf := map[string]string{}
	for _, n := range g.Nodes {
		typeOf[n.ID] = n.Type
	}
	for _, e := range g.Edges {
		if e.Kind == coregraph.EdgeDependsOn && strings.HasSuffix(strings.ToLower(typeOf[e.From]), fromSuffix) && strings.EqualFold(typeOf[e.To], toType) {
			return true
		}
	}
	return false
}

// R7: a module's dependsOn on a resource reaches its flattened children.
func TestModuleDependsOnGivesGraphEdge(t *testing.T) {
	if !edgeBetween(t, "twice", "/connections", "Microsoft.CognitiveServices/accounts") {
		t.Error("connection inside the conn module should depend on the account the module depends on")
	}
}

// R7: a dependsOn on another module reaches that module's resources, and R3
// (module outputs) makes the same dependency visible as a reference.
func TestModuleToModuleDependsOnGivesGraphEdge(t *testing.T) {
	if !edgeBetween(t, "avm", "/connections", "Microsoft.Search/searchServices") {
		t.Error("connection module reads s1 outputs: expected an edge to the Search service")
	}
}

// R2: graph labels never show the internal scope token, while two unbound
// instances stay two nodes.
func TestModelGraphLabelsHaveNoScopeToken(t *testing.T) {
	raw := []byte(`{"resources":[
	 {"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"a","properties":{"mode":"Incremental","expressionEvaluationOptions":{"scope":"inner"},
	  "parameters":{"n":{"value":"x"}},"template":{"parameters":{"n":{"type":"securestring"}},"resources":[{"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('n')]","properties":{}}]}}},
	 {"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"b","properties":{"mode":"Incremental","expressionEvaluationOptions":{"scope":"inner"},
	  "parameters":{"n":{"value":"y"}},"template":{"parameters":{"n":{"type":"securestring"}},"resources":[{"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('n')]","properties":{}}]}}}]}`)
	m := mustModel(t, raw)
	g, err := Build(Input{Source: SourceInput{Resources: ModelResources(m.Resources(), m.Placements())}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("two module instances must stay two nodes, got %d", len(g.Nodes))
	}
	for _, n := range g.Nodes {
		if strings.Contains(n.Label+n.Name, "@") {
			t.Errorf("scope token in node %+v", n)
		}
	}
}

func mustModel(t *testing.T, raw []byte) *armmodel.Model {
	t.Helper()
	m, err := armmodel.FromARM(raw, armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
