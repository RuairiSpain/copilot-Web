package app

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// armWithParams is an ARM loader double that also supplies deployment parameters.
type armWithParams struct {
	vals  map[string]any
	calls int
}

func (a *armWithParams) Load(context.Context, Source) (sdk.ARMModel, error) {
	return stubModel{}, nil
}

func (a *armWithParams) DeploymentParameters(context.Context, Source) map[string]any {
	a.calls++
	return a.vals
}

type stubModel struct{}

func (stubModel) Resources() []sdk.ARMResource { return nil }

func TestPreflightPassesParametersOnlyForWhatIf(t *testing.T) {
	vals := map[string]any{"location": "eastus", "adminToken": nil}
	cases := []struct {
		name    string
		whatIf  bool
		want    map[string]any
		wantRan int
	}{
		{"what-if requested", true, vals, 1},
		{"no what-if means the parameter files are not read again", false, nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pf := &fakePreflight{}
			ld := &armWithParams{vals: vals}
			svc, _ := newSvc(&fakeEngine{})
			svc.Preflight = pf
			svc.ARM = ld
			if _, err := Preflight(context.Background(), svc, PreflightRequest{WhatIf: c.whatIf}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(pf.in.Parameters, c.want) || ld.calls != c.wantRan {
				t.Errorf("parameters = %v (reads %d), want %v (reads %d)", pf.in.Parameters, ld.calls, c.want, c.wantRan)
			}
		})
	}
}

func TestPreflightWithoutParameterSourceOrModelSendsNone(t *testing.T) {
	// A loader without DeploymentParameters, and a run without a model.
	pf := &fakePreflight{}
	svc, _ := newSvc(&fakeEngine{})
	svc.Preflight = pf
	if _, err := Preflight(context.Background(), svc, PreflightRequest{WhatIf: true}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if pf.in.Parameters != nil {
		t.Errorf("parameters = %v", pf.in.Parameters)
	}
}

func TestDeploymentParametersFromParameterFile(t *testing.T) {
	src := func(dir string) Source {
		return Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}
	}
	const armJSON = `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#","parameters":{"location":{"type":"string"}},"resources":[]}`
	t.Run("values, unresolved references and secret-looking keys", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", "name: demo\n")
		writeFile(t, dir, "infra/main.json", armJSON)
		writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{"location":{"value":"eastus"},"adminToken":{"value":"abc123"},"fromEnv":{"value":"${NOT_SET_ANYWHERE}"},"obj":{"value":{"a":1}}}}`)
		got := armLoader{}.DeploymentParameters(context.Background(), src(dir))
		if got["location"] != "eastus" {
			t.Fatalf("location = %v (%v)", got["location"], got)
		}
		for _, k := range []string{"adminToken", "fromEnv", "obj"} {
			v, set := got[k]
			if !set || v != nil {
				t.Errorf("%s = %v (set %v), want set but unresolved (nil)", k, v, set)
			}
		}
		if strings.Contains(strings.Join(keys(got), ","), "abc123") {
			t.Error("a secret value is present")
		}
	})
	t.Run("no parameter file means no values", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", "name: demo\n")
		writeFile(t, dir, "infra/main.json", armJSON)
		if got := (armLoader{}).DeploymentParameters(context.Background(), src(dir)); got != nil {
			t.Errorf("got %v", got)
		}
	})
	t.Run("malformed parameter file means no values", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", "name: demo\n")
		writeFile(t, dir, "infra/main.json", armJSON)
		writeFile(t, dir, "infra/main.parameters.json", `{not json`)
		if got := (armLoader{}).DeploymentParameters(context.Background(), src(dir)); got != nil {
			t.Errorf("got %v", got)
		}
	})
	t.Run("no project directory", func(t *testing.T) {
		if got := (armLoader{}).DeploymentParameters(context.Background(), Source{}); got != nil {
			t.Errorf("got %v", got)
		}
	})
	t.Run("azd environment substitution is used", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", "name: demo\n")
		writeFile(t, dir, "infra/main.json", armJSON)
		writeFile(t, dir, ".azure/dev/.env", "AZURE_LOCATION=\"swedencentral\"\n")
		writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{"location":{"value":"${AZURE_LOCATION}"}}}`)
		s := src(dir)
		s.Environment = "dev"
		got := armLoader{}.DeploymentParameters(context.Background(), s)
		if got["location"] != "swedencentral" {
			t.Errorf("got %v", got)
		}
	})
}

func keys(m map[string]any) []string {
	var out []string
	for k, v := range m {
		out = append(out, k)
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestAzureClientForTenantBuildsWithoutNetwork(t *testing.T) {
	for _, tenant := range []string{"", "00000000-0000-0000-0000-000000000001", "contoso.onmicrosoft.com", "--bad"} {
		c, err := azureClientForTenant(tenant)
		if err != nil {
			t.Fatalf("tenant %q: %v", tenant, err)
		}
		if c.Extras == nil || c.Foundry == nil || c.Context == nil {
			t.Errorf("tenant %q: client not fully wired: %+v", tenant, c)
		}
	}
}
