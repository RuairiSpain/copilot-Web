package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/preflight"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const flowSub = "11111111-1111-1111-1111-111111111111"

type flowCred struct{}

func (flowCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "not-a-real-token"}, nil
}

type flowRT struct {
	mu     sync.Mutex
	posts  []string
	bodies []string
}

func (r *flowRT) RoundTrip(req *http.Request) (*http.Response, error) {
	var sent string
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		sent = string(b)
	}
	if req.Method == http.MethodPost {
		r.mu.Lock()
		r.posts = append(r.posts, req.URL.Path)
		r.bodies = append(r.bodies, sent)
		r.mu.Unlock()
	}
	body := `{"name":"rg","location":"eastus","status":"Succeeded","properties":{"changes":[]},"value":[]}`
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// TestParameterFileToWhatIfRequestBody runs armLoader.DeploymentParameters on a
// real parameter file, feeds the values to the real preflight rules and the
// real Azure adapter (fake transport), and inspects the what-if request body.
func TestParameterFileToWhatIfRequestBody(t *testing.T) {
	const secretValue = "abc123-not-a-real-value"
	src := func(dir string) Source {
		return Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}
	}
	arm := func(params string) string {
		return `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#","parameters":{` + params + `},"resources":[]}`
	}
	run := func(t *testing.T, template, paramFile string) (*flowRT, sdk.Result) {
		t.Helper()
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", "name: demo\n")
		writeFile(t, dir, "infra/main.json", template)
		writeFile(t, dir, "infra/main.parameters.json", paramFile)
		values := armLoader{}.DeploymentParameters(context.Background(), src(dir))
		rt := &flowRT{}
		a, err := azure.New(azure.Options{Credential: flowCred{}, Endpoint: "https://management.test", HTTPClient: &http.Client{Transport: rt}})
		if err != nil {
			t.Fatal(err)
		}
		c := a.Client()
		d := preflight.Deps{
			Context: c.Context, Inventory: c.Inventory, Permissions: c.Permissions, Models: c.Models, Policy: c.Policy,
			WhatIf: c.WhatIf, Names: c.Names, Regions: c.Regions, Deployments: c.Deployments, SubnetLinks: c.SubnetLinks,
			Evidence: c.Evidence, Extras: c.Extras,
			Target:      preflight.Target{TenantID: flowSub, SubscriptionID: flowSub, ResourceGroup: "rg", Location: "eastus"},
			WhatIfOptIn: true, Template: []byte(template), Parameters: values,
		}
		for _, r := range preflight.Rules(d) {
			if r.ID() == "FND-DEP-008" {
				out, err := r.Evaluate(context.Background(), &sdk.Input{ARM: stubModel{}})
				if err != nil {
					t.Fatal(err)
				}
				return rt, out
			}
		}
		t.Fatal("FND-DEP-008 not registered")
		return nil, sdk.Result{}
	}

	t.Run("undeclared unresolved and secret-looking file keys never reach the request", func(t *testing.T) {
		rt, out := run(t, arm(`"location":{"type":"string"}`),
			`{"parameters":{"location":{"value":"eastus"},"adminToken":{"value":"`+secretValue+`"},"fromEnv":{"value":"${NOT_SET_ANYWHERE}"}}}`)
		if len(rt.posts) != 1 {
			t.Fatalf("what-if posts = %v (%+v)", rt.posts, out)
		}
		body := rt.bodies[0]
		if !strings.Contains(body, `"parameters":{"location":{"value":"eastus"}}`) {
			t.Errorf("resolved parameter missing: %s", body)
		}
		for _, bad := range []string{secretValue, "adminToken", "fromEnv", "NOT_SET_ANYWHERE", "${"} {
			if strings.Contains(body, bad) {
				t.Errorf("request body contains %q: %s", bad, body)
			}
		}
	})
	t.Run("a declared parameter with an unresolved ${VAR} stops the what-if", func(t *testing.T) {
		rt, out := run(t, arm(`"location":{"type":"string"},"fromEnv":{"type":"string"}`),
			`{"parameters":{"location":{"value":"eastus"},"fromEnv":{"value":"${NOT_SET_ANYWHERE}"}}}`)
		if len(rt.posts) != 0 {
			t.Fatalf("what-if must not run with an unresolved parameter: %v", rt.posts)
		}
		if out.Skipped == nil || !strings.Contains(out.Skipped.Reason, "template parameters unresolved: fromEnv") {
			t.Errorf("skip = %+v", out.Skipped)
		}
	})
	t.Run("a declared secret-looking parameter is withheld and stops the what-if", func(t *testing.T) {
		rt, out := run(t, arm(`"location":{"type":"string"},"adminToken":{"type":"string","defaultValue":"d"}`),
			`{"parameters":{"location":{"value":"eastus"},"adminToken":{"value":"`+secretValue+`"}}}`)
		if len(rt.posts) != 0 {
			t.Fatalf("what-if must not run: %v", rt.posts)
		}
		if out.Skipped == nil || !strings.Contains(out.Skipped.Reason, "adminToken") || strings.Contains(out.Skipped.Reason, secretValue) {
			t.Errorf("skip = %+v", out.Skipped)
		}
	})
}
