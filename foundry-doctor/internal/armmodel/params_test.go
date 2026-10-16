package armmodel_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func byName(t *testing.T, m *armmodel.Model, typ, name string) sdk.ARMResource {
	t.Helper()
	for _, r := range m.Resources() {
		if strings.EqualFold(r.Type, typ) && strings.EqualFold(r.Name, name) {
			return r
		}
	}
	var have []string
	for _, r := range m.Resources() {
		have = append(have, r.Type+" "+r.Name)
	}
	t.Fatalf("no %s %q in %v", typ, name, have)
	return sdk.ARMResource{}
}

const (
	typeAI      = "Microsoft.CognitiveServices/accounts"
	typeSearch  = "Microsoft.Search/searchServices"
	typeStorage = "Microsoft.Storage/storageAccounts"
)

// Parameters are injected into the root template: the conditional resource
// is deployed, publicNetworkAccess takes the file value, names and the
// module binding resolve to literals.
func TestParameterFileValuesResolveRoot(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters: map[string]any{
			"prefix":              "dev",
			"deploySearch":        true,
			"publicNetworkAccess": "Disabled",
			"location":            "swedencentral",
			"capacity":            json.Number("3"),
		},
	})
	ai := byName(t, m, typeAI, "dev-ai")
	if ai.Region != "swedencentral" {
		t.Errorf("region %q", ai.Region)
	}
	if got := ai.Properties["publicNetworkAccess"]; got != "Disabled" {
		t.Errorf("publicNetworkAccess %v, want Disabled (file value over default Enabled)", got)
	}
	if got := ai.Properties["customSubDomainName"]; got != "dev-ai" {
		t.Errorf("customSubDomainName %v", got)
	}
	srch := byName(t, m, typeSearch, "dev-srch")
	if got := presence(srch); got != armexpr.Deployed {
		t.Errorf("deploySearch=true must deploy the search service, presence %v (condition %q)", got, srch.Condition)
	}
	if srch.Properties["replicaCount"] != float64(3) {
		t.Errorf("replicaCount %#v, want the number 3", srch.Properties["replicaCount"])
	}
	// The module binding resolves through the root injection.
	mod := byName(t, m, typeSearch, "dev-msrch")
	if mod.Properties["publicNetworkAccess"] != "Disabled" || mod.Region != "swedencentral" {
		t.Errorf("module resource %+v", mod)
	}
	if got := presence(mod); got != armexpr.Deployed {
		t.Errorf("module presence %v, want Deployed", got)
	}
	for _, r := range m.Resources() {
		if len(m.FromDefault(r)) != 0 {
			t.Errorf("%s: every used parameter is set by the file, FromDefault = %v", r.Name, m.FromDefault(r))
		}
	}
}

// A deploySearch=false file value never deploys the resource or the module.
func TestParameterFileFalseConditionNotDeployed(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters:         map[string]any{"prefix": "dev", "deploySearch": false},
	})
	if got := presence(byName(t, m, typeSearch, "dev-srch")); got != armexpr.NotDeployed {
		t.Errorf("presence %v, want NotDeployed", got)
	}
	if got := presence(byName(t, m, typeSearch, "dev-msrch")); got != armexpr.NotDeployed {
		t.Errorf("module presence %v, want NotDeployed", got)
	}
}

// Without any parameter source nothing is injected and no default is a fact.
func TestNoParameterSourceKeepsExpressions(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{})
	ai := byName(t, m, typeAI, "[format('{0}-ai', parameters('prefix'))]")
	if ai.Properties["publicNetworkAccess"] != "[parameters('publicNetworkAccess')]" {
		t.Errorf("default must not be applied without a parameter file: %v", ai.Properties["publicNetworkAccess"])
	}
	if len(m.FromDefault(ai)) != 0 {
		t.Errorf("FromDefault %v", m.FromDefault(ai))
	}
}

// A file that does not set a parameter: the literal default is applied and
// recorded as default-derived; an expression default stays unresolved.
func TestLiteralDefaultAppliedWithProvenance(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters:         map[string]any{"prefix": "dev"},
	})
	ai := byName(t, m, typeAI, "dev-ai")
	if ai.Properties["publicNetworkAccess"] != "Enabled" {
		t.Errorf("literal default Enabled should apply, got %v", ai.Properties["publicNetworkAccess"])
	}
	if got := m.FromDefault(ai); len(got) != 1 || got[0] != "publicNetworkAccess" {
		t.Errorf("FromDefault %v, want [publicNetworkAccess]", got)
	}
	// location's default is resourceGroup().location: never a fact.
	if !armexpr.IsExpression(ai.Region) {
		t.Errorf("expression default must stay unresolved, region %q", ai.Region)
	}
	// The false default of deploySearch drives the condition and the module.
	srch := byName(t, m, typeSearch, "dev-srch")
	if got := presence(srch); got != armexpr.NotDeployed {
		t.Errorf("default false: presence %v", got)
	}
	if got := m.FromDefault(srch); !contains(got, "deploySearch") {
		t.Errorf("search FromDefault %v, want deploySearch", got)
	}
	mod := byName(t, m, typeSearch, "dev-msrch")
	if got := m.FromDefault(mod); !contains(got, "deploySearch") || !contains(got, "publicNetworkAccess") {
		t.Errorf("module resource FromDefault %v, want deploySearch and publicNetworkAccess", got)
	}
	found := false
	for i, r := range m.Resources() {
		if r.Name == "dev-msrch" {
			found = contains(m.Placements()[i].DefaultParams, "deploySearch")
		}
	}
	if !found {
		t.Error("Placement.DefaultParams must carry the provenance")
	}
	// A parameter with no default and no value stays unresolved.
	m2 := rulestest.ScenarioWith(t, "params", armmodel.Options{ParametersProvided: true})
	if got := m2.Resources()[0].Name; !armexpr.IsExpression(got) {
		t.Errorf("prefix without value or default must stay an expression, got %q", got)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// A set-but-unresolved file value (nil) is neither injected nor replaced by
// the Bicep default.
func TestUnresolvedFileValueBlocksDefault(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters:         map[string]any{"prefix": "dev", "publicNetworkAccess": nil},
	})
	ai := byName(t, m, typeAI, "dev-ai")
	if ai.Properties["publicNetworkAccess"] != "[parameters('publicNetworkAccess')]" {
		t.Errorf("unresolved value must stay an expression, got %v", ai.Properties["publicNetworkAccess"])
	}
	if len(m.FromDefault(ai)) != 0 {
		t.Errorf("FromDefault %v", m.FromDefault(ai))
	}
}

// Type coercion: a "true" string for a bool parameter, a numeric string for
// an int; a wrong-typed value is not injected.
func TestParameterCoercionAndTypeMismatch(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters: map[string]any{
			"prefix": "dev", "deploySearch": "true", "capacity": "5", "publicNetworkAccess": true,
		},
	})
	srch := byName(t, m, typeSearch, "dev-srch")
	if presence(srch) != armexpr.Deployed || srch.Properties["replicaCount"] != float64(5) {
		t.Errorf("coercion failed: %v %v", presence(srch), srch.Properties["replicaCount"])
	}
	ai := byName(t, m, typeAI, "dev-ai")
	if ai.Properties["publicNetworkAccess"] != "[parameters('publicNetworkAccess')]" {
		t.Errorf("a bool for a string parameter must not be injected: %v", ai.Properties["publicNetworkAccess"])
	}
	// A string that would be read as an expression is refused.
	m = rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters:         map[string]any{"prefix": "[concat('a','b')]"},
	})
	if !armexpr.IsExpression(m.Resources()[0].Name) {
		t.Errorf("an expression-shaped file string must not be injected: %q", m.Resources()[0].Name)
	}
}

// Secure parameters are never substituted, from a file or a default; the
// tag value built from one stays an expression.
func TestSecureParameterNeverSubstituted(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters:         map[string]any{"prefix": "dev", "secretTagValue": "fake-file-value"},
	})
	st := byName(t, m, typeStorage, "devst")
	if got := st.Tags["token"]; got != "[parameters('secretTagValue')]" {
		t.Errorf("secure parameter was substituted: %v", got)
	}
	for _, r := range m.Resources() {
		if strings.Contains(strings.ToLower(r.Name+r.Region), "fake-file-value") {
			t.Errorf("secure value leaked into %s", r.Name)
		}
	}
}

// M1: absent tags and an expression-valued tags property are told apart.
func TestTagsAreExpression(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true, Parameters: map[string]any{"prefix": "dev"},
	})
	cases := []struct {
		typ, name string
		want      bool
	}{
		{typeAI, "dev-ai", true},         // tags: tags (parameter expression)
		{typeStorage, "devplain", false}, // tags absent
		{typeStorage, "devst", false},    // tags: { token: <expression> } is a map
	}
	for _, c := range cases {
		r := byName(t, m, c.typ, c.name)
		if got := m.TagsAreExpression(r); got != c.want {
			t.Errorf("%s: TagsAreExpression = %v, want %v (tags %v)", c.name, got, c.want, r.Tags)
		}
	}
	if len(byName(t, m, typeAI, "dev-ai").Tags) != 0 {
		t.Error("Resource.Tags must stay empty for expression tags")
	}
	if got := byName(t, m, typeSearch, "dev-srch").Tags["owner"]; got != "platform" {
		t.Errorf("map tags unchanged, got %v", got)
	}
	var nilModel *armmodel.Model
	if nilModel.TagsAreExpression(sdk.ARMResource{}) {
		t.Error("nil model")
	}
	if m.TagsAreExpression(sdk.ARMResource{Type: "x", Name: "y"}) {
		t.Error("unknown resource must be false")
	}
}

// Variable exposes literal variables and folds the ones that depend on a
// parameter value.
func TestVariable(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true, Parameters: map[string]any{"prefix": "dev"},
	})
	if v, ok := m.Variable("ownerRoleId"); !ok || v != "8e3af657-a8ff-443c-a75c-2fe8c4bcb635" {
		t.Errorf("ownerRoleId = %v, %v", v, ok)
	}
	if v, ok := m.Variable("OWNERROLEID"); !ok || v == nil {
		t.Errorf("lookup is case-insensitive: %v %v", v, ok)
	}
	// Depends on the role literal and on a pure function only.
	v, ok := m.Variable("ownerRoleDefinitionId")
	if !ok {
		t.Fatal("ownerRoleDefinitionId should be resolvable")
	}
	s, _ := v.(string)
	if guid, ok := armexpr.RoleDefinitionGUID(s); !ok || guid != "8e3af657-a8ff-443c-a75c-2fe8c4bcb635" {
		t.Errorf("role definition %q -> %q %v", s, guid, ok)
	}
	// Folded through the parameter file value.
	if v, ok := m.Variable("searchName"); !ok || v != "dev-srch" {
		t.Errorf("searchName = %v, %v", v, ok)
	}
	// Without the parameter value it is unknown, not wrong.
	m0 := rulestest.ScenarioWith(t, "params", armmodel.Options{})
	if v, ok := m0.Variable("searchName"); ok {
		t.Errorf("searchName without prefix must be unresolved, got %v", v)
	}
	if v, ok := m0.Variable("ownerRoleId"); !ok || v == nil {
		t.Errorf("literal variables never need parameters: %v %v", v, ok)
	}
	if _, ok := m.Variable("nope"); ok {
		t.Error("missing variable")
	}
	var nilModel *armmodel.Model
	if _, ok := nilModel.Variable("x"); ok {
		t.Error("nil model")
	}
}

// A hostile template: a variable cycle and a deep chain terminate.
func TestVariableCycleTerminates(t *testing.T) {
	tpl := `{"variables":{"a":"[variables('b')]","b":"[variables('a')]","c":"[concat(variables('c'), 'x')]"},"resources":[]}`
	m, err := armmodel.FromARM([]byte(tpl), armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b", "c"} {
		if _, ok := m.Variable(n); ok {
			t.Errorf("cyclic variable %s must not resolve", n)
		}
	}
}

// A literal and an object variable round-trip; oversize and excess variables
// are bounded.
func TestVariableKindsAndBounds(t *testing.T) {
	tpl := `{"variables":{"s":"x","b":true,"n":4,"o":{"k":"v","n":[1,"two"]},"e":"[[literal]","big":"` + strings.Repeat("z", 9000) + `"},"resources":[]}`
	m, err := armmodel.FromARM([]byte(tpl), armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := m.Variable("s"); v != "x" {
		t.Errorf("s %v", v)
	}
	if v, _ := m.Variable("b"); v != true {
		t.Errorf("b %v", v)
	}
	if v, _ := m.Variable("n"); v != float64(4) {
		t.Errorf("n %v", v)
	}
	if v, _ := m.Variable("e"); v != "[literal]" {
		t.Errorf("escaped bracket: %v", v)
	}
	o, ok := m.Variable("o")
	if !ok {
		t.Fatal("object variable")
	}
	if om := o.(map[string]any); om["k"] != "v" || om["n"].([]any)[1] != "two" {
		t.Errorf("object %v", o)
	}
	if _, ok := m.Variable("big"); ok {
		t.Error("an oversize variable must be dropped")
	}
}
