package graph

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

func scalar(key, v string, line int) *model.Node {
	return &model.Node{Kind: model.NodeScalar, Key: key, Value: v, Pos: model.Pos{Line: line, Column: 3}}
}

func mapping(key string, kids ...*model.Node) *model.Node {
	return &model.Node{Kind: model.NodeMapping, Key: key, Children: kids}
}

func sampleYAML() *model.AzureYAML {
	agentNode := mapping("agent",
		mapping("env", scalar("MODEL", "${MODEL_NAME}", 5), scalar("LOG", "${LOG_LEVEL:-info}", 6), scalar("X", "${{ project.endpoint }}", 7)),
		scalar("bad", "${1}", 8),
	)
	root := mapping("",
		scalar("name", "${AZURE_ENV_NAME}", 1),
		mapping("services", agentNode, mapping("tools"), mapping("conn")),
		mapping("resources", mapping("cosmos")),
	)
	return &model.AzureYAML{
		Name: "p", Root: root,
		Services: []model.Service{
			{Name: "agent", Uses: []model.Ref{{Name: "tools"}, {Name: "cosmos"}, {Name: "ghost"}}, Pos: model.Pos{Line: 4, Column: 1}},
			{Name: "tools", Uses: []model.Ref{{Name: "conn"}}},
			{Name: "conn", Uses: []model.Ref{{Name: "agent"}}},
		},
	}
}

func sampleARM() *model.ARMTemplate {
	return &model.ARMTemplate{
		Parameters: []string{"name"},
		Outputs:    []model.Output{{Name: "ENDPOINT"}, {Name: "MODEL_NAME"}},
		Resources: []model.Resource{
			{Type: "Microsoft.Storage/storageAccounts", Name: "[parameters('name')]", Symbolic: "sa"},
			{Type: "Microsoft.Network/virtualNetworks", Name: "vnet", Symbolic: "vnet"},
			{Type: "Microsoft.Network/virtualNetworks/subnets", Name: "vnet/snet", Symbolic: "snet",
				DependsOn: []string{"vnet"}},
			{
				Type: "Microsoft.Network/privateEndpoints", Name: "pe", DependsOn: []string{"[resourceId('Microsoft.Storage/storageAccounts', parameters('name'))]"},
				Body: map[string]any{
					"properties": map[string]any{
						"subnet": map[string]any{"id": "[resourceId('Microsoft.Network/virtualNetworks/subnets', 'vnet', 'snet')]"},
						"privateLinkServiceConnections": []any{map[string]any{"properties": map[string]any{
							"privateLinkServiceId": "[resourceId('Microsoft.Storage/storageAccounts', parameters('name'))]"}}},
						"manualPrivateLinkServiceConnections": []any{map[string]any{"properties": map[string]any{
							"privateLinkServiceId": "/subscriptions/s/resourceGroups/r/providers/Microsoft.Storage/storageAccounts/other"}}},
					},
				},
			},
			{
				Type: "Microsoft.Authorization/roleAssignments", Name: "[guid('a')]",
				Body: map[string]any{"scope": "[resourceId('Microsoft.Storage/storageAccounts', parameters('name'))]",
					"properties": map[string]any{"principalId": "[reference('sa').principalId]", "n": "[uniqueString('x')]"}},
			},
			{Type: "Microsoft.Foo/bar", Name: "loop", Body: map[string]any{"properties": map[string]any{"self": "[reference('loop')]", "bad": "[f(]"}}},
		},
	}
}

func buildSample() *Graph {
	env := model.NewEnvironment("dev", map[string]string{"LOG_LEVEL": "debug", "SECRET": "s3cr3t"})
	return Build(model.Input{AzureYAML: sampleYAML(), ARM: sampleARM(), Environment: &env})
}

const (
	saID   = "arm-resource:microsoft.storage/storageaccounts/parameters('name')"
	peID   = "arm-resource:microsoft.network/privateendpoints/pe"
	snetID = "arm-resource:microsoft.network/virtualnetworks/subnets/vnet/snet"
)

func TestBuildNodesAndEdges(t *testing.T) {
	g := buildSample()
	tests := []struct {
		name       string
		from, to   string
		kind       string
		wantEdge   bool
		wantTarget string
	}{
		{"uses service", "service:agent", "service:tools", model.EdgeUses, true, ""},
		{"uses yaml resource", "service:agent", "yaml-resource:cosmos", model.EdgeUses, true, ""},
		{"uses unknown absent", "service:agent", "service:ghost", model.EdgeUses, false, ""},
		{"consumes", "service:agent", "env-key:MODEL_NAME", model.EdgeConsumes, true, ""},
		{"consumes default", "service:agent", "env-key:LOG_LEVEL", model.EdgeConsumes, true, ""},
		{"project consumes", "azure-yaml:azure.yaml", "env-key:AZURE_ENV_NAME", model.EdgeConsumes, true, ""},
		{"output produces env", "arm-output:MODEL_NAME", "env-key:MODEL_NAME", model.EdgeProduces, true, ""},
		{"dependsOn symbolic", snetID, "arm-resource:microsoft.network/virtualnetworks/vnet", model.EdgeDependsOn, true, ""},
		{"dependsOn expression", peID, saID, model.EdgeDependsOn, true, ""},
		{"private link", peID, saID, EdgePrivateLink, true, ""},
		{"subnet", peID, snetID, EdgeSubnet, true, ""},
		{"scope", "arm-resource:microsoft.authorization/roleassignments/guid('a')", saID, EdgeScope, true, ""},
		{"reference symbolic", "arm-resource:microsoft.authorization/roleassignments/guid('a')", saID, EdgeReferences, true, ""},
		{"parameter ref", saID, "arm-parameter:name", EdgeReferences, true, ""},
		{"self reference", "arm-resource:microsoft.foo/bar/loop", "arm-resource:microsoft.foo/bar/loop", EdgeReferences, true, ""},
		{"no wrong kind", peID, saID, EdgeScope, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(g.Out(tt.from, tt.kind), tt.to); got != tt.wantEdge {
				t.Errorf("edge %s -%s-> %s = %v, want %v", tt.from, tt.kind, tt.to, got, tt.wantEdge)
			}
			if tt.wantEdge && !contains(g.In(tt.to, tt.kind), tt.from) {
				t.Errorf("In(%s) lacks %s", tt.to, tt.from)
			}
		})
	}
	if _, ok := g.Node("service:agent"); !ok {
		t.Error("service node missing")
	}
	if n, _ := g.Node("service:agent"); n.Pos.Line != 4 {
		t.Errorf("pos = %+v", n.Pos)
	}
	if _, ok := g.Node("nope"); ok {
		t.Error("unexpected node")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestDeterministicAndDeduplicated(t *testing.T) {
	a, b := buildSample(), buildSample()
	if !reflect.DeepEqual(a.Graph, b.Graph) {
		t.Fatal("graphs differ between runs")
	}
	seen := map[model.GraphEdge]bool{}
	for i, e := range a.Edges {
		if seen[e] {
			t.Errorf("duplicate edge %+v", e)
		}
		seen[e] = true
		if i > 0 && a.Edges[i-1].Kind > e.Kind {
			t.Errorf("edges not sorted at %d", i)
		}
	}
	for i := 1; i < len(a.Nodes); i++ {
		if a.Nodes[i-1].ID >= a.Nodes[i].ID {
			t.Errorf("nodes not sorted/unique at %d", i)
		}
	}
}

func TestQueries(t *testing.T) {
	g := buildSample()
	check := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("ServicesUsing", g.ServicesUsing("service:tools"), []string{"service:agent"})
	check("ServicesUsing none", g.ServicesUsing("service:nothing"), nil)
	check("ProducersOf", g.ProducersOf("env-key:ENDPOINT"), []string{"arm-output:ENDPOINT"})
	check("ConsumersOf", g.ConsumersOf("env-key:MODEL_NAME"), []string{"service:agent"})
	check("PrivateEndpointsFor", g.PrivateEndpointsFor(saID), []string{peID})
	check("RoleAssignmentsAt", g.RoleAssignmentsAt(saID), []string{"arm-resource:microsoft.authorization/roleassignments/guid('a')"})
	check("RoleAssignmentsAt non-target", g.RoleAssignmentsAt(peID), nil)
	check("SubnetsOf", g.SubnetsOf(peID), []string{snetID})

	if !g.EnvironmentHas("LOG_LEVEL") || g.EnvironmentHas("MODEL_NAME") || !g.HasEnvironment() {
		t.Error("EnvironmentHas wrong")
	}
	if _, ok := g.Node("env-key:SECRET"); !ok {
		t.Error("environment key node missing")
	}
	for _, n := range g.Nodes {
		if strings.Contains(n.ID, "s3cr3t") {
			t.Error("value leaked into the graph")
		}
	}

	uu := g.UnresolvedUses()
	if len(uu) != 1 || uu[0].Name != "ghost" || uu[0].Reason != ReasonUnknownService {
		t.Errorf("UnresolvedUses = %+v", uu)
	}
	reasons := map[string]bool{}
	for _, u := range g.Unresolved() {
		reasons[u.Reason] = true
	}
	for _, r := range []string{ReasonUnknownService, "unsupported-function:uniquestring", ReasonMalformedExpression, ReasonMalformedInterp, ReasonTargetNotInTemplate} {
		if !reasons[r] {
			t.Errorf("missing unresolved reason %q in %v", r, reasons)
		}
	}
}

func TestEnvRefsAndForms(t *testing.T) {
	g := buildSample()
	wantForms := []Form{FormExpression, FormBraced, FormDefault}
	got := g.FormsSeen()
	if len(got) != 3 {
		t.Fatalf("forms = %v, want %v", got, wantForms)
	}
	for _, f := range wantForms {
		if !contains(formStrings(got), string(f)) {
			t.Errorf("form %q not seen", f)
		}
	}
	refs := g.EnvRefsOf("service:agent")
	if len(refs) != 3 {
		t.Fatalf("refs = %+v", refs)
	}
	var expr, def int
	for _, r := range refs {
		if r.Expression {
			expr++
			if _, ok := g.Node("env-key:"); ok {
				t.Error("expression must not create an env key")
			}
		}
		if r.HasDefault {
			def++
		}
	}
	if expr != 1 || def != 1 {
		t.Errorf("expression=%d default=%d", expr, def)
	}
	if len(g.EnvRefs()) != 4 {
		t.Errorf("EnvRefs = %d, want 4", len(g.EnvRefs()))
	}
}

func formStrings(f []Form) []string {
	out := make([]string, len(f))
	for i, x := range f {
		out[i] = string(x)
	}
	return out
}

func TestCaveat(t *testing.T) {
	g := buildSample()
	if g.Caveat(model.GraphEdge{From: "arm-output:A", To: "env-key:A", Kind: model.EdgeProduces}) != ProducesCaveat {
		t.Error("produces edge needs the caveat")
	}
	if g.Caveat(model.GraphEdge{From: "service:a", To: "service:b", Kind: model.EdgeUses}) != "" {
		t.Error("uses edge needs no caveat")
	}
}

func TestMissingInputs(t *testing.T) {
	g := Build(model.Input{})
	if len(g.Nodes) != 0 || len(g.Edges) != 0 || g.HasEnvironment() || g.EnvironmentHas("A") {
		t.Errorf("empty input produced %+v", g.Graph)
	}
	if g.Cycles() != nil || g.Unresolved() != nil || len(g.FormsSeen()) != 0 {
		t.Error("empty graph must have no findings")
	}
	// A plain model.Graph can carry the result.
	var in model.Input
	in.Graph = g.Graph
	_ = in

	// azure.yaml without Root falls back to the service nodes.
	a := &model.AzureYAML{Services: []model.Service{{Name: "s", Node: mapping("s", scalar("v", "${A}", 2))}}}
	g = Build(model.Input{AzureYAML: a})
	if !contains(g.Out("service:s", model.EdgeConsumes), "env-key:A") {
		t.Error("service node scan failed")
	}
	// A service key in Root with no Services entry is owned by the project node.
	a = &model.AzureYAML{Root: mapping("", mapping("services", mapping("orphan", scalar("v", "${B}", 3))))}
	g = Build(model.Input{AzureYAML: a})
	if !contains(g.Out("azure-yaml:azure.yaml", model.EdgeConsumes), "env-key:B") {
		t.Error("orphan service value must be owned by the project")
	}
}

func TestUnicodeAndExpressionNames(t *testing.T) {
	arm := &model.ARMTemplate{Resources: []model.Resource{
		{Type: "Microsoft.Foo/Bar", Name: "日本語"},
		{Type: "Microsoft.Foo/user", Name: "u", DependsOn: []string{"[resourceId('microsoft.foo/bar', '日本語')]", "/subscriptions/s/resourceGroups/r/providers/Microsoft.Foo/Bar/日本語", "missing", "[bad(]"}},
		{Type: "Microsoft.Foo/x", Name: "[bad(]"},
	}}
	g := Build(model.Input{ARM: arm})
	targets := g.Out("arm-resource:microsoft.foo/user/u", model.EdgeDependsOn)
	if !reflect.DeepEqual(targets, []string{"arm-resource:microsoft.foo/bar/日本語"}) {
		t.Errorf("targets = %v", targets)
	}
	if _, ok := g.Node("arm-resource:microsoft.foo/x/unparsed-2"); !ok {
		t.Error("unparsed name node missing")
	}
}

func TestLiteralIDKey(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"/subscriptions/s/resourceGroups/r/providers/Microsoft.A/b/n", "microsoft.a/b/n", true},
		{"/subscriptions/s/providers/Microsoft.A/b/n/c/m", "microsoft.a/b/c/n/m", true},
		{"/providers/Microsoft.A/b", "", false},
		{"plain", "", false},
		{"/providers/Microsoft.A/b/n/c", "", false},
	}
	for _, tt := range tests {
		got, ok := literalIDKey(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("literalIDKey(%q) = %q, %v", tt.in, got, ok)
		}
	}
}

func TestAmbiguousPlainNameIsNotResolved(t *testing.T) {
	arm := &model.ARMTemplate{Resources: []model.Resource{
		{Type: "A.B/x", Name: "same"}, {Type: "A.B/y", Name: "same"},
		{Type: "A.B/z", Name: "z", DependsOn: []string{"same"}},
	}}
	g := Build(model.Input{ARM: arm})
	if got := g.Out("arm-resource:a.b/z/z", model.EdgeDependsOn); len(got) != 0 {
		t.Errorf("ambiguous name resolved to %v", got)
	}
}

func TestLargeFanOut(t *testing.T) {
	const n = 10000
	a := &model.AzureYAML{Services: []model.Service{{Name: "hub"}}}
	for i := 0; i < n; i++ {
		a.Services = append(a.Services, model.Service{Name: fmt.Sprintf("s%05d", i), Uses: []model.Ref{{Name: "hub"}}})
	}
	g := Build(model.Input{AzureYAML: a})
	if got := len(g.ServicesUsing("service:hub")); got != n {
		t.Errorf("fan-in = %d", got)
	}
	if g.Cycles() != nil {
		t.Error("star must have no cycle")
	}
}

func TestDeepNestedBody(t *testing.T) {
	var v any = "[parameters('p')]"
	for i := 0; i < 500; i++ {
		v = map[string]any{"k": []any{v}}
	}
	arm := &model.ARMTemplate{Parameters: []string{"p"}, Resources: []model.Resource{{Type: "A.B/x", Name: "x", Body: map[string]any{"properties": v}}}}
	g := Build(model.Input{ARM: arm})
	if !contains(g.Out("arm-resource:a.b/x/x", EdgeReferences), "arm-parameter:p") {
		t.Error("deeply nested parameter reference lost")
	}
}

func TestBodyFromJSON(t *testing.T) {
	var body map[string]any
	raw := `{"dependsOn":["other"],"properties":{"subnet":{"id":"/subscriptions/s/providers/Microsoft.Network/virtualNetworks/v/subnets/s"}}}`
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	arm := &model.ARMTemplate{Resources: []model.Resource{
		{Type: "A.B/x", Name: "x", Body: body},
		{Type: "A.B/o", Name: "o", Symbolic: "other"},
		{Type: "Microsoft.Network/virtualNetworks/subnets", Name: "v/s"},
	}}
	g := Build(model.Input{ARM: arm})
	if !contains(g.Out("arm-resource:a.b/x/x", model.EdgeDependsOn), "arm-resource:a.b/o/o") {
		t.Error("body dependsOn lost")
	}
	if len(g.SubnetsOf("arm-resource:a.b/x/x")) != 1 {
		t.Error("literal subnet id lost")
	}
}
