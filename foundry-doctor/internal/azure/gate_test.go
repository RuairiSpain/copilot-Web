package azure

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// verifiedCapabilities lists every RBAC action string an allow-list entry may
// name. Each one was found (case-insensitively) in the Azure RBAC permission
// tables of MicrosoftDocs/azure-docs, articles/role-based-access-control/
// permissions/{management-and-governance,networking,ai-machine-learning,
// security,storage,containers,integration,monitor,databases}.md on 2026-10-17.
// A new allow-list entry must add its capability here after the same check.
var verifiedCapabilities = map[string]bool{
	"Microsoft.ApiManagement/checkNameAvailability/read":                     true,
	"Microsoft.ApiManagement/deletedservices/read":                           true,
	"Microsoft.Authorization/denyAssignments/read":                           true,
	"Microsoft.Authorization/locks/read":                                     true,
	"Microsoft.Authorization/permissions/read":                               true,
	"Microsoft.Authorization/policyAssignments/read":                         true,
	"Microsoft.Authorization/roleAssignments/read":                           true,
	"Microsoft.Authorization/roleDefinitions/read":                           true,
	"Microsoft.CognitiveServices/accounts/capabilityHosts/read":              true,
	"Microsoft.CognitiveServices/accounts/connections/read":                  true,
	"Microsoft.CognitiveServices/accounts/deployments/read":                  true,
	"Microsoft.CognitiveServices/accounts/projects/capabilityHosts/read":     true,
	"Microsoft.CognitiveServices/accounts/projects/connections/read":         true,
	"Microsoft.CognitiveServices/accounts/projects/read":                     true,
	"Microsoft.CognitiveServices/checkDomainAvailability/action":             true,
	"Microsoft.CognitiveServices/deletedAccounts/read":                       true,
	"Microsoft.CognitiveServices/locations/models/read":                      true,
	"Microsoft.CognitiveServices/locations/usages/read":                      true,
	"Microsoft.ContainerRegistry/checkNameAvailability/read":                 true,
	"Microsoft.DocumentDB/databaseAccounts/sqlRoleAssignments/read":          true,
	"Microsoft.Insights/diagnosticSettings/read":                             true,
	"Microsoft.Insights/metrics/read":                                        true,
	"Microsoft.KeyVault/checkNameAvailability/read":                          true,
	"Microsoft.KeyVault/deletedVaults/read":                                  true,
	"Microsoft.Network/privateDnsZones/virtualNetworkLinks/read":             true,
	"Microsoft.Network/virtualNetworks/read":                                 true,
	"Microsoft.Network/virtualNetworks/subnets/read":                         true,
	"Microsoft.Network/virtualNetworks/subnets/resourceNavigationLinks/read": true,
	"Microsoft.Network/virtualNetworks/subnets/serviceAssociationLinks/read": true,
	"Microsoft.PolicyInsights/checkPolicyRestrictions/read":                  true,
	"Microsoft.Resources/deployments/read":                                   true,
	"Microsoft.Resources/deployments/whatIf/action":                          true,
	"Microsoft.Resources/deployments/operationstatuses/read":                 true,
	"Microsoft.Resources/subscriptions/locations/read":                       true,
	"Microsoft.Resources/subscriptions/providers/read":                       true,
	"Microsoft.Resources/subscriptions/read":                                 true,
	"Microsoft.Resources/subscriptions/resourceGroups/read":                  true,
	"Microsoft.Resources/subscriptions/resourcegroups/resources/read":        true,
	"Microsoft.Resources/subscriptions/resources/read":                       true,
	"Microsoft.Search/checkNameAvailability/action":                          true,
	"Microsoft.Storage/checknameavailability/read":                           true,
}

func TestEveryAllowListCapabilityIsVerified(t *testing.T) {
	for _, o := range AllowedOperations() {
		if !verifiedCapabilities[o.Capability] {
			t.Errorf("operation %s names capability %q which is not in the verified list", o.Name, o.Capability)
		}
	}
	used := map[string]bool{}
	for _, o := range AllowedOperations() {
		used[o.Capability] = true
	}
	var stale []string
	for c := range verifiedCapabilities {
		if !used[c] {
			stale = append(stale, c)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("verified capabilities used by no operation (remove them): %v", stale)
	}
}

func TestFindingF8CapabilityTexts(t *testing.T) {
	want := map[string]string{
		"subnet.navigationlinks":       "Microsoft.Network/virtualNetworks/subnets/resourceNavigationLinks/read",
		"subnet.associationlinks":      "Microsoft.Network/virtualNetworks/subnets/serviceAssociationLinks/read",
		"policy.checkrestrictions":     "Microsoft.PolicyInsights/checkPolicyRestrictions/read",
		"resources.list.resourcegroup": "Microsoft.Resources/subscriptions/resourcegroups/resources/read",
		"resources.list.subscription":  "Microsoft.Resources/subscriptions/resources/read",
	}
	got := map[string]string{}
	for _, o := range AllowedOperations() {
		got[o.Name] = o.Capability
	}
	for name, c := range want {
		if got[name] != c {
			t.Errorf("%s capability = %q, want %q", name, got[name], c)
		}
	}
}

func TestAPIVersionsOfFindingF6(t *testing.T) {
	versions := map[string]string{}
	for _, o := range AllowedOperations() {
		versions[o.Name] = o.APIVersion
	}
	// Microsoft.Resources/resources 2022-12-01 is not published (refuted); 2025-04-01 is.
	for _, n := range []string{"resourcegroup.get", "resources.list.subscription", "resources.list.resourcegroup"} {
		if versions[n] != "2025-04-01" {
			t.Errorf("%s api-version = %q, want 2025-04-01", n, versions[n])
		}
	}
	// subscriptions 2022-12-01 is verified and stays.
	for _, n := range []string{"subscription.get", "locations.list"} {
		if versions[n] != "2022-12-01" {
			t.Errorf("%s api-version = %q, want 2022-12-01", n, versions[n])
		}
	}
	if _, ok := versions["policyexemptions.list"]; ok {
		t.Error("policyexemptions.list must not be allow-listed")
	}
	for n, v := range versions {
		if strings.Contains(v, "preview") && n != "runtime.diagnosticsettings" {
			t.Errorf("%s uses preview api-version %s", n, v)
		}
	}
}

// recordingRT answers every request with a body every adapter can decode and
// records method and path. It fails the test for a request outside the
// allow-list, so a fake capability can never hide a disallowed path.
type recordingRT struct {
	t     *testing.T
	mu    sync.Mutex
	calls []string
}

func (r *recordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if !IsAllowed(req.Method, req.URL.Path) {
		r.t.Errorf("request outside the allow-list: %s %s", req.Method, req.URL.Path)
	}
	r.mu.Lock()
	r.calls = append(r.calls, req.Method+" "+req.URL.Path)
	r.mu.Unlock()
	body := `{"value":[],"nameAvailable":true,"status":"Succeeded","registrationState":"Registered","displayName":"s","state":"Enabled","tenantId":"` + testTID + `"}`
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// TestAdapterMethodsRouteThroughAllowList calls every adapter method the
// preflight rules use, against the real allow-list, with the real request
// paths: unlike the fakes of internal/preflight, the paths cannot be invented.
func TestAdapterMethodsRouteThroughAllowList(t *testing.T) {
	rt := &recordingRT{t: t}
	a, err := New(Options{Credential: staticCred{token: fakeJWT(testOID, "")}, Endpoint: "https://management.test",
		HTTPClient: &http.Client{Transport: rt}, Sleep: (&sleeps{}).fn})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	vnet := testRG + "/providers/Microsoft.Network/virtualNetworks/vnet1"
	acct := testRG + "/providers/Microsoft.CognitiveServices/accounts/acct"
	tmpl := []byte(`{"$schema":"x","resources":[]}`)
	type call struct {
		name string
		fn   func() error
	}
	calls := []call{
		{"Context", func() error { _, e := a.Context(ctx, ContextRequest{SubscriptionID: testSub}); return e }},
		{"ProviderStates", func() error { _, e := a.ProviderStates(ctx, testSub, []string{"Microsoft.App"}); return e }},
		{"ListResources/sub", func() error {
			_, e := a.ListResources(ctx, InventoryQuery{Scope: testScope, Types: []string{"Microsoft.Storage/storageAccounts"}})
			return e
		}},
		{"ListResources/rg", func() error { _, e := a.ListResources(ctx, InventoryQuery{Scope: testRG}); return e }},
		{"ResourceGroup", func() error { _, e := a.ResourceGroup(ctx, testSub, "rg-demo"); return e }},
		{"ListLocks/sub", func() error { _, e := a.ListLocks(ctx, testScope); return e }},
		{"ListLocks/rg", func() error { _, e := a.ListLocks(ctx, testRG); return e }},
		{"ListResourceLocks", func() error { _, e := a.ListResourceLocks(ctx, acct); return e }},
		{"ListResourceLocks/child", func() error { _, e := a.ListResourceLocks(ctx, acct+"/deployments/gpt"); return e }},
		{"GetSubnet", func() error { _, e := a.GetSubnet(ctx, vnet+"/subnets/agents"); return e }},
		{"GetVNet", func() error { _, e := a.GetVNet(ctx, vnet); return e }},
		{"EffectiveActions/rg", func() error {
			_, e := a.EffectiveActions(ctx, PermissionRequest{Scope: testRG, Actions: []string{"Microsoft.Resources/deployments/write"}})
			return e
		}},
		{"EffectiveActions/sub", func() error {
			_, e := a.EffectiveActions(ctx, PermissionRequest{Scope: testScope, Actions: []string{"Microsoft.Resources/deployments/write"}})
			return e
		}},
		{"ReportedPermissions/rg", func() error { _, e := a.ReportedPermissions(ctx, testRG); return e }},
		{"ListModels", func() error {
			_, e := a.ListModels(ctx, ModelQuery{SubscriptionID: testSub, Location: "swedencentral"})
			return e
		}},
		{"ListUsages", func() error { _, e := a.ListUsages(ctx, testSub, "swedencentral"); return e }},
		{"ListAssignments", func() error { _, e := a.ListAssignments(ctx, testRG); return e }},
		{"CheckRestrictions", func() error {
			_, e := a.CheckRestrictions(ctx, PolicyRestrictionRequest{Scope: testRG, ResourceType: "Microsoft.Storage/storageAccounts", APIVersion: "2024-01-01", Content: map[string]any{"name": "x"}})
			return e
		}},
		{"WhatIf/rg", func() error { _, e := a.Run(ctx, WhatIfRequest{OptIn: true, Scope: testRG, Template: tmpl}); return e }},
		{"WhatIf/sub", func() error {
			_, e := a.Run(ctx, WhatIfRequest{OptIn: true, Scope: testScope, Location: "swedencentral", Template: tmpl})
			return e
		}},
		{"ListLocations", func() error { _, e := a.ListLocations(ctx, testSub); return e }},
		{"ProviderResourceTypes", func() error { _, e := a.ProviderResourceTypes(ctx, testSub, "Microsoft.Storage"); return e }},
		{"CountDeployments", func() error { _, e := a.CountDeployments(ctx, testSub, "rg-demo"); return e }},
		{"SubnetLinks", func() error { _, e := a.SubnetLinks(ctx, vnet+"/subnets/agents"); return e }},
		{"ListAccountDeployments", func() error { _, e := a.ListAccountDeployments(ctx, testSub, "rg-demo", "acct"); return e }},
	}
	for _, k := range []NameKind{NameKeyVault, NameStorage, NameACR, NameAPIM, NameSearch, NameFoundry} {
		calls = append(calls, call{"CheckName/" + string(k), func() error {
			_, e := a.CheckName(ctx, NameCheck{Kind: k, Name: "abc123", SubscriptionID: testSub})
			return e
		}})
	}
	for _, k := range []NameKind{NameKeyVault, NameAPIM, NameFoundry} {
		calls = append(calls, call{"ListSoftDeleted/" + string(k), func() error { _, e := a.ListSoftDeleted(ctx, k, testSub); return e }})
	}
	for _, c := range calls {
		before := len(rt.calls)
		if err := c.fn(); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if len(rt.calls) == before && !strings.HasPrefix(c.name, "ListSoftDeleted/") {
			t.Errorf("%s made no request", c.name)
		}
	}
}

func TestLockListingScopes(t *testing.T) {
	rt := &recordingRT{t: t}
	a, err := New(Options{Credential: staticCred{token: "x"}, Endpoint: "https://management.test", HTTPClient: &http.Client{Transport: rt}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res := testRG + "/providers/Microsoft.CognitiveServices/accounts/acct"
	// ListLocks is for a subscription or resource group; a resource ID is refused before any network I/O.
	if _, err := a.ListLocks(ctx, res); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("ListLocks(resource) err = %v", err)
	}
	if _, err := a.ListResourceLocks(ctx, testRG); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("ListResourceLocks(rg) err = %v", err)
	}
	if len(rt.calls) != 0 {
		t.Errorf("rejected scopes reached the network: %v", rt.calls)
	}
	if !IsAllowed("GET", res+"/providers/Microsoft.Authorization/locks") {
		t.Error("resource-level lock listing (ManagementLocks_ListAtResourceLevel) must be allow-listed")
	}
	if IsAllowed("PUT", res+"/providers/Microsoft.Authorization/locks/x") || IsAllowed("DELETE", res+"/providers/Microsoft.Authorization/locks/x") {
		t.Error("lock writes must not be allow-listed")
	}
}

func TestResourceLocksNotFoundIsEmpty(t *testing.T) {
	res := testRG + "/providers/Microsoft.CognitiveServices/accounts/acct"
	a, _ := newAdapter(t, newFake(t)) // every route answers 404
	got, err := a.ListResourceLocks(context.Background(), res)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
	a2, _ := newAdapter(t, newFake(t).on403("GET", res+"/providers/Microsoft.Authorization/locks"))
	if _, err := a2.ListResourceLocks(context.Background(), res); !errors.Is(err, ErrUnavailable) {
		t.Errorf("restricted err = %v", err)
	}
}

func TestLockCovers(t *testing.T) {
	rg := testRG
	cases := []struct {
		name, lock, id string
		want           bool
	}{
		{"same scope", rg, rg, true},
		{"ancestor subscription", testScope, rg + "/providers/Microsoft.Storage/storageAccounts/a", true},
		{"resource group over child", rg, rg + "/providers/Microsoft.Storage/storageAccounts/a", true},
		{"resource over its child", rg + "/providers/Microsoft.CognitiveServices/accounts/a", rg + "/providers/Microsoft.CognitiveServices/accounts/a/deployments/d", true},
		{"unrelated resource group", testScope + "/resourceGroups/rg-other", rg + "/providers/Microsoft.Storage/storageAccounts/a", false},
		{"sibling resource", rg + "/providers/Microsoft.Storage/storageAccounts/b", rg + "/providers/Microsoft.Storage/storageAccounts/a", false},
		{"child lock does not cover parent", rg + "/providers/Microsoft.CognitiveServices/accounts/a/deployments/d", rg + "/providers/Microsoft.CognitiveServices/accounts/a", false},
		{"name prefix is not a path boundary", rg + "/providers/Microsoft.Storage/storageAccounts/ab", rg + "/providers/Microsoft.Storage/storageAccounts/abc", false},
		{"case-insensitive", strings.ToUpper(rg), rg + "/x", true},
		{"empty lock scope covers nothing", "", rg, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LockCovers(c.lock, c.id); got != c.want {
				t.Errorf("LockCovers(%q, %q) = %v", c.lock, c.id, got)
			}
		})
	}
}

func TestGetVNet(t *testing.T) {
	vnet := testRG + "/providers/Microsoft.Network/virtualNetworks/vnet1"
	f := newFake(t).on("GET", vnet, reply{status: 200, body: `{"id":"` + vnet + `","name":"vnet1","location":"swedencentral","properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}`})
	a, _ := newAdapter(t, f)
	got, err := a.GetVNet(context.Background(), vnet)
	if err != nil || got != (VNet{ID: vnet, Name: "vnet1", Location: "swedencentral"}) {
		t.Errorf("got %+v, %v", got, err)
	}
	if f.callCount() != 1 || !strings.Contains(f.calls[0], "api-version=2025-05-01") {
		t.Errorf("calls = %v", f.calls)
	}
	for name, bad := range map[string]string{"subnet id": vnet + "/subnets/s", "rg": testRG, "other type": testRG + "/providers/Microsoft.Storage/storageAccounts/a", "traversal": testRG + "/providers/Microsoft.Network/virtualNetworks/../x"} {
		if _, err := a.GetVNet(context.Background(), bad); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	a2, _ := newAdapter(t, newFake(t))
	if _, err := a2.GetVNet(context.Background(), vnet); !isNotFound(err) {
		t.Errorf("missing vnet err = %v", err)
	}
	a3, _ := newAdapter(t, newFake(t).on403("GET", vnet))
	_, err = a3.GetVNet(context.Background(), vnet)
	if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.Network/virtualNetworks/read" {
		t.Errorf("restricted err = %v", err)
	}
}

func TestSubnetLinkRestrictedNamesExactPermission(t *testing.T) {
	sid := testSubnet
	for suffix, perm := range map[string]string{
		"ServiceAssociationLinks": "Microsoft.Network/virtualNetworks/subnets/serviceAssociationLinks/read",
		"ResourceNavigationLinks": "Microsoft.Network/virtualNetworks/subnets/resourceNavigationLinks/read",
	} {
		other := "ServiceAssociationLinks"
		if suffix == other {
			other = "ResourceNavigationLinks"
		}
		a, _ := newAdapter(t, newFake(t).on403("GET", sid+"/"+suffix).on("GET", sid+"/"+other, reply{status: 200, body: `{"value":[]}`}))
		_, err := a.SubnetLinks(context.Background(), sid)
		if u, ok := AsUnavailable(err); !ok || u.Permission != perm {
			t.Errorf("%s: err = %v, want permission %s", suffix, err, perm)
		}
	}
}

func TestPolicyRestrictionsSanitiseContent(t *testing.T) {
	cases := []struct {
		name    string
		in      map[string]any
		want    map[string]any
		dropped []string
	}{
		{"literals kept", map[string]any{"properties": map[string]any{"publicNetworkAccess": "Disabled", "n": 3.0, "b": true}},
			map[string]any{"properties": map[string]any{"publicNetworkAccess": "Disabled", "n": 3.0, "b": true}}, nil},
		{"expression property dropped, siblings kept", map[string]any{"properties": map[string]any{"publicNetworkAccess": "[parameters('pna')]", "kind": "AIServices"}},
			map[string]any{"properties": map[string]any{"kind": "AIServices"}}, []string{"properties.publicNetworkAccess"}},
		{"expression name dropped", map[string]any{"name": "[format('{0}-x', parameters('p'))]"}, map[string]any{}, []string{"name"}},
		{"escaped bracket is a literal", map[string]any{"description": "[[not an expression"}, map[string]any{"description": "[[not an expression"}, nil},
		{"array with an expression element dropped whole", map[string]any{"properties": map[string]any{"ipRules": []any{map[string]any{"value": "1.2.3.4"}, map[string]any{"value": "[parameters('ip')]"}}, "ok": "x"}},
			map[string]any{"properties": map[string]any{"ok": "x"}}, []string{"properties.ipRules"}},
		{"secret-looking keys dropped", map[string]any{"properties": map[string]any{"adminPassword": "hunter2", "apiKey": "k", "connectionString": "c", "sasToken": "t", "name": "n"}},
			map[string]any{"properties": map[string]any{"name": "n"}}, []string{"properties.adminPassword", "properties.apiKey", "properties.connectionString", "properties.sasToken"}},
		{"credential-shaped values dropped", map[string]any{"properties": map[string]any{"target": "DefaultEndpointsProtocol=https;AccountName=a;" + "Account" + "Key=zzzz==", "x": "y"}},
			map[string]any{"properties": map[string]any{"x": "y"}}, []string{"properties.target"}},
		{"nil input", nil, map[string]any{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, dropped := SanitizePolicyContent(c.in)
			if !reflect.DeepEqual(got, c.want) || !reflect.DeepEqual(dropped, c.dropped) {
				t.Errorf("got %v dropped %v; want %v dropped %v", got, dropped, c.want, c.dropped)
			}
		})
	}
}

func TestCheckRestrictionsNeverSendsExpressionsOrSecrets(t *testing.T) {
	path := testRG + "/providers/Microsoft.PolicyInsights/checkPolicyRestrictions"
	f := newFake(t).on("POST", path, reply{status: 200, body: `{}`})
	a, _ := newAdapter(t, f)
	_, err := a.CheckRestrictions(context.Background(), PolicyRestrictionRequest{
		Scope: testRG, ResourceType: "Microsoft.CognitiveServices/accounts", APIVersion: "2026-09-01", Location: "[parameters('location')]",
		Content: map[string]any{"name": "[parameters('accountName')]", "properties": map[string]any{
			"publicNetworkAccess": "[parameters('pna')]", "customSubDomainName": "literal-name", "admin" + "Pass" + "word": "pw" + "-literal-123"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := f.bodies["POST "+path][0]
	for _, bad := range []string{"parameters(", "pw-literal-123", "adminPassword", "accountName"} {
		if strings.Contains(body, bad) {
			t.Errorf("request body contains %q: %s", bad, body)
		}
	}
	for _, want := range []string{`"customSubDomainName":"literal-name"`, `"type":"Microsoft.CognitiveServices/accounts"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body lacks %s: %s", want, body)
		}
	}
}

func TestCheckRestrictionsMapsResultDenyAndEffectDetails(t *testing.T) {
	path := testRG + "/providers/Microsoft.PolicyInsights/checkPolicyRestrictions"
	pa := func(n string) string { return testScope + "/providers/Microsoft.Authorization/policyAssignments/" + n }
	body := `{"fieldRestrictions":[
	  {"field":"location","restrictions":[{"result":"Deny","values":["westus"],"policy":{"policyAssignmentId":"` + pa("a1") + `"},"reason":"not allowed"}]},
	  {"field":"tags","restrictions":[{"result":"Required","policy":{"policyAssignmentId":"` + pa("a2") + `"}}]},
	  {"field":"sku","restrictions":[{"result":"Audit","policyEffect":"audit","policy":{"policyAssignmentId":"` + pa("a3") + `"}}]}],
	 "contentEvaluationResult":{"policyEvaluations":[
	  {"policyInfo":{"policyAssignmentId":"` + pa("a4") + `"},"evaluationResult":"NonCompliant","effectDetails":{"policyEffect":"deny"}},
	  {"policyInfo":{"policyAssignmentId":"` + pa("a5") + `"},"evaluationResult":"NonCompliant","effectDetails":{"policyEffect":"audit"}}]}}`
	a, _ := newAdapter(t, newFake(t).on("POST", path, reply{status: 200, body: body}))
	got, err := a.CheckRestrictions(context.Background(), PolicyRestrictionRequest{Scope: testRG, ResourceType: "T/t", APIVersion: "v"})
	if err != nil {
		t.Fatal(err)
	}
	byAssignment := map[string]PolicyRestriction{}
	for _, r := range got {
		byAssignment[r.AssignmentID[strings.LastIndex(r.AssignmentID, "/")+1:]] = r
	}
	wantKind := map[string]string{"a1": RestrictionObservedDenial, "a2": RestrictionPotentialConflict, "a3": RestrictionPotentialConflict, "a4": RestrictionObservedDenial, "a5": RestrictionPotentialConflict}
	for id, k := range wantKind {
		if byAssignment[id].Kind != k {
			t.Errorf("%s kind = %q, want %q (%+v)", id, byAssignment[id].Kind, k, byAssignment[id])
		}
	}
	if !strings.EqualFold(byAssignment["a1"].Effect, "deny") || byAssignment["a1"].Result != "Deny" {
		t.Errorf("result Deny must map to a deny effect: %+v", byAssignment["a1"])
	}
	if !strings.EqualFold(byAssignment["a4"].Effect, "deny") {
		t.Errorf("effectDetails.policyEffect not read: %+v", byAssignment["a4"])
	}
}

func TestChainPassesTenant(t *testing.T) {
	t.Setenv("AZURE_ACCESS_TOKEN", "")
	cases := []struct {
		name, tenant string
		azdFails     bool
		wantCalls    []string
	}{
		{"azd gets --tenant-id", testTID, false, []string{"azd auth token --output json --scope " + scope + " --tenant-id " + testTID}},
		{"az gets --tenant", testTID, true, []string{
			"azd auth token --output json --scope " + scope + " --tenant-id " + testTID,
			"az account get-access-token --output json --resource https://management.azure.com --tenant " + testTID}},
		{"no tenant means no flag", "", false, []string{"azd auth token --output json --scope " + scope}},
		{"domain tenant", "contoso.onmicrosoft.com", false, []string{"azd auth token --output json --scope " + scope + " --tenant-id contoso.onmicrosoft.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &scriptRunner{fn: func(name string) ([]byte, error) {
				if name == "azd" && c.azdFails {
					return nil, errors.New("fail")
				}
				if name == "az" {
					return []byte(`{"accessToken":"tok-az","expires_on":4102444800}`), nil
				}
				return azdOut(time.Now().Add(time.Hour)), nil
			}}
			if _, err := newChainTenant(r.run, time.Now, c.tenant).Token(context.Background(), scope); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.calls, c.wantCalls) {
				t.Errorf("calls = %q, want %q", r.calls, c.wantCalls)
			}
		})
	}
}

func TestChainRefusesOptionLikeTenant(t *testing.T) {
	t.Setenv("AZURE_ACCESS_TOKEN", "")
	for _, bad := range []string{"--help", "-x", "a b", "t;rm", "a\nb", "$(x)"} {
		r := &scriptRunner{fn: func(string) ([]byte, error) { return azdOut(time.Now().Add(time.Hour)), nil }}
		_, err := newChainTenant(r.run, time.Now, bad).Token(context.Background(), scope)
		if !errors.Is(err, ErrNoCredential) || len(r.calls) != 0 {
			t.Errorf("tenant %q: err=%v calls=%v", bad, err, r.calls)
		}
	}
}

func TestNewPassesTenantToDefaultCredential(t *testing.T) {
	// Options.TenantID reaches the default credential without any network use.
	a, err := New(Options{TenantID: testTID, Endpoint: "https://management.test"})
	if err != nil || a == nil {
		t.Fatalf("New: %v", err)
	}
	if c, ok := a.c.cred.(*chainCredential); !ok || c.tenant != testTID {
		t.Errorf("credential = %#v", a.c.cred)
	}
}

func TestAccountDeploymentsCarrySKUName(t *testing.T) {
	p := testRG + "/providers/Microsoft.CognitiveServices/accounts/acct/deployments"
	a, _ := newAdapter(t, newFake(t).on("GET", p, reply{status: 200, body: `{"value":[{"id":"x","name":"gpt","sku":{"name":"GlobalStandard","capacity":10},"properties":{"currentCapacity":10}}]}`}))
	got, err := a.ListAccountDeployments(context.Background(), testSub, "rg-demo", "acct")
	if err != nil || len(got) != 1 || got[0].SKUName != "GlobalStandard" || got[0].CurrentCapacity != 10 {
		t.Errorf("got %+v, %v", got, err)
	}
}
