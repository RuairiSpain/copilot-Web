package armmodel_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func searches(m *armmodel.Model) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range m.Resources() {
		if strings.EqualFold(r.Type, "Microsoft.Search/searchServices") {
			out = append(out, r)
		}
	}
	return out
}

func presence(r sdk.ARMResource) armexpr.Presence {
	return armexpr.PresenceOf(r.Conditional, r.Condition, r.Looped)
}

// B1: a conditional module makes its resources MaybeDeployed.
func TestConditionalModulePropagates(t *testing.T) {
	ss := searches(rulestest.Scenario(t, "condmod"))
	if len(ss) != 2 {
		t.Fatalf("want 2 search services, got %d", len(ss))
	}
	if got := presence(ss[0]); got != armexpr.MaybeDeployed {
		t.Errorf("if (deploySearch) module resource: presence %v, want MaybeDeployed (%+v)", got, ss[0])
	}
	if got := presence(ss[1]); got != armexpr.NotDeployed {
		t.Errorf("if (false) module resource: presence %v, want NotDeployed (%+v)", got, ss[1])
	}
}

// B1: a looped module makes its resources looped.
func TestLoopedModulePropagates(t *testing.T) {
	ss := searches(rulestest.Scenario(t, "loopmod"))
	if len(ss) != 1 || !ss[0].Looped || presence(ss[0]) != armexpr.MaybeDeployed {
		t.Fatalf("looped module resource must be looped/MaybeDeployed: %+v", ss)
	}
}

// B1: ancestors propagate through nested modules, and a literal-true bound
// inner condition does not hide the outer one.
func TestNestedModulesPropagate(t *testing.T) {
	ss := searches(rulestest.Scenario(t, "nested"))
	if len(ss) != 2 {
		t.Fatalf("want 2 search services, got %d", len(ss))
	}
	for _, r := range ss {
		if presence(r) != armexpr.MaybeDeployed || r.Condition != "[parameters('deployOuter')]" {
			t.Errorf("nested resource %q: condition %q presence %v, want the outer condition", r.Name, r.Condition, presence(r))
		}
	}
}

// An unconditional top-level resource stays deployed.
func TestTopLevelStaysDeployed(t *testing.T) {
	for _, r := range rulestest.Scenario(t, "twice").Resources() {
		if presence(r) != armexpr.Deployed {
			t.Errorf("%q should be deployed: %+v", r.Name, r)
		}
	}
}

// B2: the caller's parameter binding replaces the child's parameters('name').
func TestModuleParametersSubstituted(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	ss := searches(m)
	if len(ss) != 2 {
		t.Fatalf("want 2 search services, got %d", len(ss))
	}
	if want := "[format('srch-f-{0}', variables('uniq'))]"; ss[0].Name != want {
		t.Errorf("first search name %q, want %q", ss[0].Name, want)
	}
	if want := "[format('srch-x-{0}', variables('uniq'))]"; ss[1].Name != want {
		t.Errorf("second search name %q, want %q", ss[1].Name, want)
	}
	if ss[0].Region != "[parameters('location')]" {
		t.Errorf("region %q should be the caller's expression", ss[0].Region)
	}
	var conn sdk.ARMResource
	for _, r := range m.Resources() {
		if strings.HasSuffix(strings.ToLower(r.Type), "/connections") {
			conn = r
		}
	}
	if !strings.Contains(conn.Name, "'searchconn'") || strings.Contains(conn.Name, "parameters(") {
		t.Errorf("connection name %q should hold the literal bound to parameter name", conn.Name)
	}
	target, _ := conn.Properties["target"].(string)
	if !strings.Contains(target, "srch-f-{0}") {
		t.Errorf("connection target %q should name the caller's search expression", target)
	}
}

// B2: the same parameter text in two scopes is Unknown, never Same.
func TestUnbindableParametersAreScopeQualified(t *testing.T) {
	// The module parameter is secure, so it cannot be substituted.
	raw := []byte(`{"resources":[
	 {"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"a","properties":{"mode":"Incremental",
	  "expressionEvaluationOptions":{"scope":"inner"},
	  "parameters":{"name":{"value":"x"}},
	  "template":{"parameters":{"name":{"type":"securestring"}},"resources":[
	   {"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('name')]","properties":{}}]}}},
	 {"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"b","properties":{"mode":"Incremental",
	  "expressionEvaluationOptions":{"scope":"inner"},
	  "parameters":{"name":{"value":"y"}},
	  "template":{"parameters":{"name":{"type":"securestring"}},"resources":[
	   {"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('name')]","properties":{}}]}}},
	 {"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('name')]","properties":{}}
	]}`)
	m, err := armmodel.FromARM(raw, armmodel.Options{Entry: "main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	rs := m.Resources()
	if len(rs) != 3 {
		t.Fatalf("want 3 resources, got %d", len(rs))
	}
	ref := func(i int) armexpr.Ref { return armexpr.ParseName(rs[i].Type, rs[i].Name) }
	if got := armexpr.Compare(ref(0), ref(1)); got != armexpr.Unknown {
		t.Errorf("module-local names in different modules: %v, want Unknown (%q vs %q)", got, rs[0].Name, rs[1].Name)
	}
	if got := armexpr.Compare(ref(0), ref(2)); got != armexpr.Unknown {
		t.Errorf("module-local vs entry-template name: %v, want Unknown", got)
	}
	if got := armexpr.Compare(ref(2), ref(2)); got != armexpr.Same {
		t.Errorf("same entry-template name: %v, want Same", got)
	}
	if strings.Contains(armexpr.StripScope(rs[0].Name), armexpr.ScopeSep) {
		t.Errorf("StripScope left a scope marker in %q", rs[0].Name)
	}
}

func TestPlacementsAreParallelAndDistinct(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	ps := m.Placements()
	if len(ps) != len(m.Resources()) {
		t.Fatalf("placements %d != resources %d", len(ps), len(m.Resources()))
	}
	seen := map[string]bool{}
	for _, p := range ps {
		if p.Pointer == "" || seen[p.Pointer] {
			t.Errorf("pointer %q empty or duplicated", p.Pointer)
		}
		seen[p.Pointer] = true
	}
	if ps[1].Module == "" || ps[1].Module == ps[2].Module {
		t.Errorf("two instances of one module must have different paths: %q %q", ps[1].Module, ps[2].Module)
	}
	if ps[0].Module != "" {
		t.Errorf("entry template resource has module path %q", ps[0].Module)
	}
	var nilModel *armmodel.Model
	if nilModel.Placements() != nil {
		t.Error("nil model placements")
	}
}

// Outer-scope evaluation uses the caller's own references unchanged.
func TestOuterScopeDeploymentIsNotQualified(t *testing.T) {
	raw := []byte(`{"resources":[{"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"a","properties":{"mode":"Incremental",
	  "expressionEvaluationOptions":{"scope":"outer"},
	  "template":{"resources":[{"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('n')]","properties":{}}]}}}]}`)
	m, err := armmodel.FromARM(raw, armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Resources()[0].Name; got != "[parameters('n')]" {
		t.Errorf("outer-scope name %q should be unchanged", got)
	}
}
