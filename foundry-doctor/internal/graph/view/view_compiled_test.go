package view

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
)

// goldenResources mirrors internal/app templateResources: it feeds the graph
// the raw compiled names and dependsOn entries of the golden template that
// Bicep CLI 0.48.1 produced from internal/rules/rulestest/main.bicep.
func goldenResources(t *testing.T) []coregraph.Resource {
	t.Helper()
	tpl, err := bicep.ParseARM(rulestest.JSON())
	if err != nil {
		t.Fatal(err)
	}
	var out []coregraph.Resource
	tpl.Walk(func(r bicep.Resource, _ int) {
		if r.IsModule() {
			return
		}
		out = append(out, coregraph.Resource{
			ID: r.Pointer, Type: r.Type, Name: r.Name, SymbolicName: r.SymbolicName, DependsOn: append([]string(nil), r.DependsOn...),
			Conditional: r.Conditional, Condition: r.Condition, Looped: r.Copy != nil,
		})
	})
	return out
}

func TestCompiledDependsOnResolvesExpressionNames(t *testing.T) {
	rs := goldenResources(t)
	g, err := coregraph.Build(coregraph.Input{Resources: rs})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range g.Unresolved {
		t.Errorf("in-template dependsOn left unresolved: %+v", u)
	}
	var depEdges int
	for _, e := range g.Edges {
		if e.Kind == coregraph.EdgeDependsOn {
			depEdges++
		}
	}
	if depEdges < 8 {
		t.Fatalf("expected the compiled dependsOn edges, got %d", depEdges)
	}
}

func TestCompiledGraphHasNoExternalNodes(t *testing.T) {
	g, err := Build(Input{Source: SourceInput{Resources: goldenResources(t)}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.External || n.Missing {
			t.Errorf("in-template resource shown as external or missing: %+v", n)
		}
	}
	var conditional int
	for _, n := range g.Nodes {
		if n.Conditional {
			conditional++
		}
	}
	if conditional != 2 { // the conditional Search service and private endpoint
		t.Errorf("want 2 conditional nodes, got %d", conditional)
	}
}

func TestLiteralFalseConditionIsOmitted(t *testing.T) {
	rs := goldenResources(t)
	removed := ""
	for i := range rs {
		if strings.EqualFold(rs[i].Type, "Microsoft.Network/privateEndpoints") && rs[i].Conditional {
			rs[i].Condition = "false"
			removed = rs[i].ID
		}
	}
	g, err := coregraph.Build(coregraph.Input{Resources: rs})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Node(coregraph.ResourceID(removed)); ok {
		t.Fatal("a resource with a literal false condition must not be a node")
	}
}

func TestAmbiguousInTemplateRefIsNotExternal(t *testing.T) {
	rs := []coregraph.Resource{
		{ID: "0", Type: "Microsoft.Storage/storageAccounts", Name: "[parameters('stName')]"},
		{ID: "1", Type: "Microsoft.App/containerApps", Name: "app", DependsOn: []string{"[resourceId('Microsoft.Storage/storageAccounts', variables('other'))]"}},
	}
	g, err := Build(Input{Source: SourceInput{Resources: rs}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.External {
			t.Fatalf("a reference that may name an in-template resource must not be external: %+v", n)
		}
	}
}
