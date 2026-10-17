package preflight

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// The fakes in preflight_test.go accept any argument, so a rule could ask for a
// path the allow-list rejects and the unit tests would never notice. These
// contract tests wire the REAL adapter (azure.New) to a RoundTripper that
// serves canned JSON and fails the test for any request that azure.IsAllowed
// rejects, then run the rules end to end.

const (
	cSub = "11111111-1111-1111-1111-111111111111"
	cRG  = "/subscriptions/" + cSub + "/resourceGroups/rg"
)

type routedRT struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]string // "METHOD path" -> JSON body
	calls  []string
	bodies map[string]string
}

func (r *routedRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if !azure.IsAllowed(req.Method, req.URL.Path) {
		r.t.Errorf("request outside the read-only allow-list: %s %s", req.Method, req.URL.Path)
	}
	var sent string
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		sent = string(b)
	}
	key := req.Method + " " + req.URL.Path
	r.mu.Lock()
	r.calls = append(r.calls, key)
	if r.bodies == nil {
		r.bodies = map[string]string{}
	}
	r.bodies[key] = sent
	body, ok := r.routes[key]
	r.mu.Unlock()
	if !ok {
		// One body every adapter can decode: an empty list that is also a
		// registered provider, an available name and a succeeded operation.
		body = `{"value":[],"registrationState":"Registered","nameAvailable":true,"status":"Succeeded","state":"Enabled","tenantId":"` + cSub + `","location":"eastus"}`
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func (r *routedRT) called(method, path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c == method+" "+path {
			return true
		}
	}
	return false
}

type tokenCred struct{}

func (tokenCred) Token(context.Context, string) (azure.AccessToken, error) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return azure.AccessToken{Token: enc(`{"alg":"none"}`) + "." + enc(`{"oid":"`+cSub+`","tid":"`+cSub+`"}`) + "." + enc("x")}, nil
}

func realDeps(t *testing.T, rt *routedRT) Deps {
	t.Helper()
	a, err := azure.New(azure.Options{Credential: tokenCred{}, Endpoint: "https://management.test", HTTPClient: &http.Client{Transport: rt}})
	if err != nil {
		t.Fatal(err)
	}
	c := a.Client()
	return Deps{
		Context: c.Context, Inventory: c.Inventory, Permissions: c.Permissions, Models: c.Models, Policy: c.Policy,
		WhatIf: c.WhatIf, Names: c.Names, Regions: c.Regions, Deployments: c.Deployments, SubnetLinks: c.SubnetLinks,
		Evidence: c.Evidence, Extras: c.Extras, Foundry: c.Foundry,
		Target:      Target{TenantID: cSub, SubscriptionID: cSub, ResourceGroup: "rg", Location: "eastus"},
		WhatIfOptIn: true,
	}
}

func contractTemplate(schema string) []byte {
	return []byte(`{"$schema":"` + schema + `","parameters":{"location":{"type":"string"}},"resources":[]}`)
}

func contractResources() []sdk.ARMResource {
	subnet := cRG + "/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/agents"
	acct := agentAccount(subnet)
	acct.Name = "acct"
	acct.Properties["customSubDomainName"] = "acct-sub"
	storage := res("Microsoft.Storage/storageAccounts", "stfoo123")
	vault := res("Microsoft.KeyVault/vaults", "kvfoo")
	search := res("Microsoft.Search/searchServices", "srchfoo")
	dep := gptDeployment("acct/gpt", "GlobalStandard", "2024-11-20", 10)
	return []sdk.ARMResource{acct, storage, vault, search, dep}
}

// TestRealAdapterRoutesEveryRuleThroughTheAllowList runs all twelve rules with a
// template that exercises every Azure call the rules make, in both deployment
// scopes. The routedRT fails the test for any path the allow-list rejects, and
// no rule may skip because the adapter refused a request.
func TestRealAdapterRoutesEveryRuleThroughTheAllowList(t *testing.T) {
	for _, schema := range []string{rgSchema, subSchema} {
		t.Run(schema[strings.LastIndex(schema, "/")+1:], func(t *testing.T) {
			vnet := cRG + "/providers/Microsoft.Network/virtualNetworks/vnet1"
			rt := &routedRT{t: t, routes: map[string]string{
				"GET " + vnet + "/subnets/agents": `{"id":"` + vnet + `/subnets/agents","name":"agents","properties":{"addressPrefix":"10.0.1.0/24","delegations":[{"properties":{"serviceName":"Microsoft.App/environments"}}]}}`,
				"GET /subscriptions/" + cSub + "/providers/Microsoft.CognitiveServices/locations/eastus/models": `{"value":[{"model":{"format":"OpenAI","name":"gpt-4o","version":"2024-11-20","skus":[{"name":"GlobalStandard","usageName":"OpenAI.GlobalStandard.gpt-4o","capacity":{"default":10}}]}}]}`,
				"GET /subscriptions/" + cSub + "/providers/Microsoft.CognitiveServices/locations/eastus/usages": `{"value":[{"name":{"value":"OpenAI.GlobalStandard.gpt-4o"},"currentValue":0,"limit":1000,"unit":"Count"}]}`,
				// A CanNotDelete lock on the group makes DEP-010 read the deployment history count.
				"GET " + cRG + "/providers/Microsoft.Authorization/locks": `{"value":[{"id":"` + cRG + `/providers/Microsoft.Authorization/locks/cnd","name":"cnd","properties":{"level":"CanNotDelete"}}]}`,
			}}
			d := realDeps(t, rt)
			d.Template = contractTemplate(schema)
			d.Parameters = map[string]any{"location": "eastus"}
			in := &sdk.Input{ARM: fakeARM{contractResources()}}
			for _, r := range Rules(d) {
				out, err := r.Evaluate(context.Background(), in)
				if err != nil {
					t.Fatalf("%s: %v", r.ID(), err)
				}
				if out.Skipped != nil && (strings.Contains(out.Skipped.Reason, "allow-list") || strings.Contains(out.Skipped.Reason, "Azure call failed")) {
					t.Errorf("%s skipped because of a transport problem: %q", r.ID(), out.Skipped.Reason)
				}
			}
			wantPaths := []string{
				"GET /subscriptions/" + cSub,
				"GET /subscriptions/" + cSub + "/providers/Microsoft.App",
				"GET /subscriptions/" + cSub + "/locations",
				"GET " + cRG + "/providers/Microsoft.Authorization/locks",
				"GET /subscriptions/" + cSub + "/providers/Microsoft.Authorization/locks",
				"GET " + cRG + "/providers/Microsoft.Storage/storageAccounts/stfoo123/providers/Microsoft.Authorization/locks",
				"GET " + cRG + "/providers/Microsoft.CognitiveServices/accounts/acct/deployments/gpt/providers/Microsoft.Authorization/locks",
				"GET " + cRG + "/providers/Microsoft.Network/virtualNetworks/vnet1",
				"GET " + cRG + "/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/agents",
				"GET " + cRG + "/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/agents/ServiceAssociationLinks",
				"GET " + cRG + "/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/agents/ResourceNavigationLinks",
				"GET /subscriptions/" + cSub + "/providers/Microsoft.CognitiveServices/locations/eastus/models",
				"GET /subscriptions/" + cSub + "/providers/Microsoft.CognitiveServices/locations/eastus/usages",
				"GET " + cRG + "/providers/Microsoft.Resources/deployments",
				"POST " + cRG + "/providers/Microsoft.PolicyInsights/checkPolicyRestrictions",
				"POST /subscriptions/" + cSub + "/providers/Microsoft.CognitiveServices/checkDomainAvailability",
				"POST /subscriptions/" + cSub + "/providers/Microsoft.Storage/checkNameAvailability",
			}
			if schema == rgSchema {
				wantPaths = append(wantPaths,
					"GET /subscriptions/"+cSub+"/resourcegroups/rg",
					"GET "+cRG+"/providers/Microsoft.Authorization/permissions",
					"POST "+cRG+"/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/whatIf")
			} else {
				wantPaths = append(wantPaths, "POST /subscriptions/"+cSub+"/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/whatIf")
			}
			for _, p := range wantPaths {
				method, path, _ := strings.Cut(p, " ")
				if !rt.called(method, path) {
					calls := append([]string(nil), rt.calls...)
					sort.Strings(calls)
					t.Errorf("expected request not made: %s\nmade:\n%s", p, strings.Join(calls, "\n"))
				}
			}
			// The what-if body carries the resolved parameter and no secret.
			body := rt.bodies["POST "+map[bool]string{true: cRG, false: "/subscriptions/" + cSub}[schema == rgSchema]+"/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/whatIf"]
			if !strings.Contains(body, `"parameters":{"location":{"value":"eastus"}}`) {
				t.Errorf("what-if body lacks the resolved parameter: %s", body)
			}
		})
	}
}

// TestRealAdapterResourceLevelLockFinding: a ReadOnly lock that exists only at
// resource level is found through the real allow-list, and an unrelated group's
// lock in the same subscription listing is ignored.
func TestRealAdapterResourceLevelLockFinding(t *testing.T) {
	stor := cRG + "/providers/Microsoft.Storage/storageAccounts/stfoo123"
	rt := &routedRT{t: t, routes: map[string]string{
		"GET " + stor + "/providers/Microsoft.Authorization/locks":                `{"value":[{"id":"` + stor + `/providers/Microsoft.Authorization/locks/ro","name":"ro","properties":{"level":"ReadOnly"}}]}`,
		"GET /subscriptions/" + cSub + "/providers/Microsoft.Authorization/locks": `{"value":[{"id":"/subscriptions/` + cSub + `/resourceGroups/other/providers/Microsoft.Authorization/locks/otherlock","name":"otherlock","properties":{"level":"ReadOnly"}}]}`,
	}}
	d := realDeps(t, rt)
	d.Template = contractTemplate(rgSchema)
	in := &sdk.Input{ARM: fakeARM{[]sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "stfoo123")}}}
	out := runInput(t, d, "FND-DEP-010", in)
	if classify(out) != oFail || len(out.Findings) != 1 || !strings.Contains(out.Findings[0].Evidence, stor) {
		t.Fatalf("got %+v", out)
	}
	if strings.Contains(out.Findings[0].Evidence, "other") {
		t.Errorf("the unrelated group's lock leaked into the finding: %q", out.Findings[0].Evidence)
	}
}

// TestRealAdapterUnrelatedGroupLockIsIgnored is the false-fail regression: a
// ReadOnly lock anywhere else in the subscription must not fail the run.
func TestRealAdapterUnrelatedGroupLockIsIgnored(t *testing.T) {
	rt := &routedRT{t: t, routes: map[string]string{
		"GET /subscriptions/" + cSub + "/providers/Microsoft.Authorization/locks": `{"value":[{"id":"/subscriptions/` + cSub + `/resourceGroups/other/providers/Microsoft.Authorization/locks/otherlock","name":"otherlock","properties":{"level":"ReadOnly"}}]}`,
	}}
	d := realDeps(t, rt)
	in := &sdk.Input{ARM: fakeARM{[]sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "stfoo123")}}}
	out := runInput(t, d, "FND-DEP-010", in)
	if classify(out) != oPass {
		t.Fatalf("got %s: %+v", classify(out), out)
	}
}

// TestRealAdapterWhatIfFailedStatusIsNotAPass: a Failed operation must surface
// as a skip naming the capability, never as an empty (passing) change list.
func TestRealAdapterWhatIfFailedStatusIsNotAPass(t *testing.T) {
	rt := &routedRT{t: t, routes: map[string]string{
		"POST " + cRG + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/whatIf": `{"status":"Failed","error":{"code":"InvalidTemplate"}}`,
	}}
	d := realDeps(t, rt)
	d.Template = contractTemplate(rgSchema)
	d.Parameters = map[string]any{"location": "eastus"}
	out := runInput(t, d, "FND-DEP-008", &sdk.Input{ARM: fakeARM{[]sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "stfoo123")}}})
	if classify(out) != oSkipped || !strings.Contains(out.Skipped.Reason, "status Failed") || !strings.Contains(out.Skipped.Reason, "InvalidTemplate") {
		t.Fatalf("got %+v", out)
	}
}

func TestRealAdapterDEP009SendsNoExpression(t *testing.T) {
	rt := &routedRT{t: t, routes: map[string]string{}}
	d := realDeps(t, rt)
	r := res("Microsoft.CognitiveServices/accounts", "acct")
	r.Properties = map[string]any{"publicNetworkAccess": "[parameters('pna')]", "disableLocalAuth": true}
	out := runInput(t, d, "FND-DEP-009", &sdk.Input{ARM: fakeARM{[]sdk.ARMResource{r}}})
	body := rt.bodies["POST "+cRG+"/providers/Microsoft.PolicyInsights/checkPolicyRestrictions"]
	if strings.Contains(body, "parameters(") || !strings.Contains(body, "disableLocalAuth") {
		t.Errorf("request body: %s", body)
	}
	if classify(out) != oUncertain {
		t.Errorf("got %+v", out)
	}
}

func TestRealAdapterRejectsDisallowedScopesBeforeTheNetwork(t *testing.T) {
	rt := &routedRT{t: t, routes: map[string]string{}}
	d := realDeps(t, rt)
	if _, err := d.Inventory.ListLocks(context.Background(), cRG+"/providers/Microsoft.Storage/storageAccounts/x"); err == nil {
		t.Error("ListLocks accepted a resource scope")
	}
	if len(rt.calls) != 0 {
		t.Errorf("disallowed request reached the network: %v", rt.calls)
	}
	_ = fmt.Sprint()
}
