package rel

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return []sdk.ARMResource(m) }

type pol map[string]any

func (p pol) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

func findRule(id string) sdk.Rule {
	for _, r := range Register() {
		if r.ID() == id {
			return r
		}
	}
	return nil
}

func eval(t *testing.T, id string, in *sdk.Input) sdk.Result {
	t.Helper()
	out, err := findRule(id).Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func res(typ, name, sku string, props map[string]any) sdk.ARMResource {
	return sdk.ARMResource{Type: typ, Name: name, SKUName: sku, Properties: props}
}

func TestRegisterIDs(t *testing.T) {
	want := []string{"FND-REL-001", "FND-REL-002", "FND-REL-003", "FND-REL-004", "FND-REL-005", "FND-REL-006", "FND-REL-008", "FND-REL-009"}
	got := Register()
	if len(got) != len(want) {
		t.Fatalf("got %d rules", len(got))
	}
	for i, r := range got {
		if r.ID() != want[i] {
			t.Fatalf("rule[%d]=%s want %s", i, r.ID(), want[i])
		}
	}
}

func TestREL001SearchReplicaPosture(t *testing.T) {
	got := eval(t, "FND-REL-001", &sdk.Input{ARM: model{
		res(typeSearch, "search", "standard", map[string]any{"replicaCount": float64(1)}),
	}})
	if len(got.Findings) != 1 {
		t.Fatalf("expected finding, got %+v", got)
	}
	clean := eval(t, "FND-REL-001", &sdk.Input{ARM: model{
		res(typeSearch, "search", "standard", map[string]any{"replicaCount": float64(3)}),
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestREL005SearchSizing(t *testing.T) {
	got := eval(t, "FND-REL-005", &sdk.Input{ARM: model{
		res(typeSearch, "bad", "standard", map[string]any{"replicaCount": float64(4), "partitionCount": float64(12)}),
	}})
	if len(got.Findings) == 0 {
		t.Fatalf("expected finding, got %+v", got)
	}
	clean := eval(t, "FND-REL-005", &sdk.Input{ARM: model{
		res(typeSearch, "ok", "standard3", map[string]any{"replicaCount": float64(3), "partitionCount": float64(2), "hostingMode": "HighDensity"}),
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass/uncertain-free result, got %+v", clean)
	}
}

func TestREL008Spillover(t *testing.T) {
	got := eval(t, "FND-REL-008", &sdk.Input{ARM: model{
		res(typeDeployment, "acct/ptu", "GlobalProvisionedManaged", map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt", "version": "1"}}),
	}})
	if len(got.Findings) != 1 {
		t.Fatalf("expected finding, got %+v", got)
	}
	clean := eval(t, "FND-REL-008", &sdk.Input{ARM: model{
		res(typeDeployment, "acct/ptu", "GlobalProvisionedManaged", map[string]any{"spilloverDeploymentName": "std", "model": map[string]any{"format": "OpenAI", "name": "gpt", "version": "1"}}),
		res(typeDeployment, "acct/std", "Standard", map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt", "version": "1"}}),
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestREL009APIMObjectives(t *testing.T) {
	got := eval(t, "FND-REL-009", &sdk.Input{
		Policy: pol{"reliability.apimSlaRequired": true},
		ARM:    model{{Type: typeAPIM, Name: "apim", SKUName: "Developer"}},
	})
	if len(got.Findings) != 1 {
		t.Fatalf("expected finding, got %+v", got)
	}
	skip := eval(t, "FND-REL-009", &sdk.Input{ARM: model{{Type: typeAPIM, Name: "apim", SKUName: "Developer"}}})
	if skip.Skipped == nil {
		t.Fatalf("expected skip without objective, got %+v", skip)
	}
}
