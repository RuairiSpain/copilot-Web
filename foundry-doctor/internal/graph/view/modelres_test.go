package view

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
)

func searchNodes(g Graph) []Node {
	var out []Node
	for _, n := range g.Nodes {
		if strings.EqualFold(n.Type, "Microsoft.Search/searchServices") {
			out = append(out, n)
		}
	}
	return out
}

func TestTwoSearchModulesAreTwoNodes(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	g, err := Build(Input{Source: SourceInput{Resources: ModelResources(m.Resources(), m.Placements())}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	ns := searchNodes(g)
	if len(ns) != 2 {
		t.Fatalf("want 2 search nodes, got %d: %+v", len(ns), ns)
	}
	if ns[0].ID == ns[1].ID || ns[0].Name == ns[1].Name {
		t.Errorf("search nodes must differ: %+v", ns)
	}
}

func TestConditionalModuleNodeIsConditional(t *testing.T) {
	m := rulestest.Scenario(t, "condmod")
	g, err := Build(Input{Source: SourceInput{Resources: ModelResources(m.Resources(), m.Placements())}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	ns := searchNodes(g)
	// The if (false) module is not deployed, so it has no node.
	if len(ns) != 1 || !ns[0].Conditional {
		t.Fatalf("want one conditional search node, got %+v", ns)
	}
}

func TestModelResourcesWithoutPlacements(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	rs := ModelResources(m.Resources(), nil)
	if len(rs) != len(m.Resources()) {
		t.Fatalf("got %d resources", len(rs))
	}
	seen := map[string]bool{}
	for _, r := range rs {
		if r.ID == "" || seen[r.ID] {
			t.Errorf("id %q empty or duplicated", r.ID)
		}
		seen[r.ID] = true
	}
}
