package model_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

type fakeAzd struct{}

func (fakeAzd) ProjectRoot(context.Context) (string, error)        { return "", model.ErrUnavailable }
func (fakeAzd) ReadFile(context.Context, string) ([]byte, error)   { return nil, model.ErrUnavailable }
func (fakeAzd) Environments(context.Context) ([]string, error)     { return nil, model.ErrUnavailable }
func (fakeAzd) CurrentEnvironment(context.Context) (string, error) { return "", model.ErrUnavailable }
func (fakeAzd) EnvValues(context.Context, string) (model.Environment, error) {
	return model.Environment{}, model.ErrUnavailable
}
func (fakeAzd) Versions(context.Context) (model.Versions, error) {
	return model.Versions{}, model.ErrUnavailable
}

var _ model.AzdContext = fakeAzd{}

func TestResourceGet(t *testing.T) {
	r := model.Resource{Body: map[string]any{
		"properties": map[string]any{
			"publicNetworkAccess": "Disabled",
			"disableLocalAuth":    true,
			"name":                "[parameters('n')]",
			"ipRules":             []any{map[string]any{"value": "1.2.3.4"}},
			"nested":              "[variables('x')]",
		},
	}}
	cases := []struct {
		path  string
		state model.ValueState
	}{
		{"properties.publicNetworkAccess", model.Literal},
		{"properties.disableLocalAuth", model.Literal},
		{"properties.name", model.Unresolved},
		{"properties.nested.deeper", model.Unresolved},
		{"properties.ipRules[0].value", model.Literal},
		{"properties.ipRules[1].value", model.Absent},
		{"properties.missing", model.Absent},
		{"properties.publicNetworkAccess.x", model.Absent},
	}
	for _, c := range cases {
		if got := r.Get(c.path).State; got != c.state {
			t.Errorf("%s: got %v want %v", c.path, got, c.state)
		}
	}
	if b, ok := r.Get("properties.disableLocalAuth").Bool(); !ok || !b {
		t.Error("bool")
	}
}

func TestEnvValueNeverPrints(t *testing.T) {
	e := model.NewEnvironment("dev", map[string]string{"B": "secret-b", "A": "secret-a"})
	out := fmt.Sprintf("%v %+v %#v %s", e, e, e, e.Keys())
	b, _ := json.Marshal(map[string]model.EnvValue{"A": mustGet(t, e, "A")})
	out += string(b)
	for _, s := range []string{"secret-a", "secret-b"} {
		if contains(out, s) {
			t.Fatalf("leaked %q in %q", s, out)
		}
	}
	if k := e.Keys(); len(k) != 2 || k[0] != "A" {
		t.Fatalf("keys not sorted: %v", k)
	}
}

func mustGet(t *testing.T, e model.Environment, k string) model.EnvValue {
	t.Helper()
	v, ok := e.Get(k)
	if !ok {
		t.Fatal("missing")
	}
	return v
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPolicyZeroMeansMissing(t *testing.T) {
	var p model.Policy
	if p.ResourceScope != nil || p.Environments.Production != nil || p.LogRetention.MinimumDays != nil {
		t.Fatal("zero policy must have every key missing")
	}
	if p.EffectiveResourceScope() != model.ScopeSameResourceGroup || p.EffectivePublicAccess() != model.PublicForbidden || p.EffectivePublicTelemetry() {
		t.Fatal("baselines wrong")
	}
}

func TestInputZero(t *testing.T) {
	var in model.Input
	if in.Project.HasARM() {
		t.Fatal("zero project has no ARM")
	}
	if (model.Project{IaC: model.IaCSynthetic}).HasARM() {
		t.Fatal("synthetic has no ARM")
	}
}
