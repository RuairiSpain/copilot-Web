package azure

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestErrorsAndTypes(t *testing.T) {
	cases := []struct {
		name string
		err  *UnavailableError
		want string
	}{
		{"permission", &UnavailableError{Capability: "X/read", Permission: "X/read"}, "capability unavailable: X/read (missing permission: X/read)"},
		{"permission and reason", &UnavailableError{Capability: "X", Permission: "P", Reason: "r"}, "capability unavailable: X (missing permission: P; r)"},
		{"reason only", &UnavailableError{Capability: "X", Reason: "not signed in"}, "capability unavailable: X (not signed in)"},
		{"bare", &UnavailableError{Capability: "X"}, "capability unavailable: X"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.err.Error() != c.want {
				t.Errorf("got %q", c.err.Error())
			}
			wrapped := errors.Join(errors.New("ctx"), c.err)
			if !errors.Is(wrapped, ErrUnavailable) {
				t.Error("errors.Is(ErrUnavailable) failed")
			}
			if u, ok := AsUnavailable(wrapped); !ok || u != c.err {
				t.Error("AsUnavailable failed")
			}
		})
	}
	if _, ok := AsUnavailable(errors.New("plain")); ok {
		t.Error("plain error must not be unavailable")
	}
	for ct, want := range map[ChangeType]bool{ChangeDelete: true, ChangeReplace: true, ChangeCreate: false, ChangeModify: false, ChangeNoChange: false, ChangeUnknown: false} {
		if got := (WhatIfChange{ChangeType: ct}).Destructive(); got != want {
			t.Errorf("Destructive(%s) = %v", ct, got)
		}
	}
}

func TestClientWiring(t *testing.T) {
	a, _ := newAdapter(t, newFake(t))
	c := a.Client()
	v := reflect.ValueOf(c)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsNil() {
			t.Errorf("Client.%s not wired", v.Type().Field(i).Name)
		}
	}
}

func TestContext(t *testing.T) {
	t.Run("ok user", func(t *testing.T) {
		f := newFake(t).onFile("GET", testScope, "subscription.json")
		a, _ := newAdapter(t, f)
		got, err := a.Context(context.Background(), ContextRequest{SubscriptionID: testSub})
		want := Context{TenantID: testTID, SubscriptionID: testSub, SubscriptionName: "Contoso Dev", SubscriptionState: "Enabled",
			SubscriptionTenantID: testTID, PrincipalObjectID: testOID, PrincipalType: "User"}
		if err != nil || got != want {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("service principal", func(t *testing.T) {
		f := newFake(t).onFile("GET", testScope, "subscription.json")
		a, _ := newAdapter(t, f, func(o *Options) { o.Credential = staticCred{token: fakeJWT(testOID, "app")} })
		got, err := a.Context(context.Background(), ContextRequest{SubscriptionID: testSub, TenantID: "explicit"})
		if err != nil || got.PrincipalType != "ServicePrincipal" || got.TenantID != "explicit" || got.SubscriptionTenantID != testTID {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("missing subscription", func(t *testing.T) {
		f := newFake(t)
		a, _ := newAdapter(t, f)
		_, err := a.Context(context.Background(), ContextRequest{})
		if !errors.Is(err, ErrUnavailable) || f.callCount() != 0 {
			t.Errorf("err=%v calls=%d", err, f.callCount())
		}
	})
	t.Run("invalid subscription", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t))
		if _, err := a.Context(context.Background(), ContextRequest{SubscriptionID: "../x"}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("not visible", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t))
		_, err := a.Context(context.Background(), ContextRequest{SubscriptionID: testSub})
		if u, ok := AsUnavailable(err); !ok || !strings.Contains(u.Reason, "not found") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("restricted identity cannot read subscription", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on403("GET", testScope))
		_, err := a.Context(context.Background(), ContextRequest{SubscriptionID: testSub})
		if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.Resources/subscriptions/read" {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("opaque token still yields context", func(t *testing.T) {
		f := newFake(t).onFile("GET", testScope, "subscription.json")
		a, _ := newAdapter(t, f, func(o *Options) { o.Credential = staticCred{token: "opaque"} })
		got, err := a.Context(context.Background(), ContextRequest{SubscriptionID: testSub})
		if err != nil || got.PrincipalObjectID != "" || got.TenantID != testTID {
			t.Errorf("got %+v, %v", got, err)
		}
	})
}

func TestCloudOf(t *testing.T) {
	for origin, want := range map[string]string{
		"https://management.azure.com": "AzureCloud", "https://management.usgovcloudapi.net": "AzureUSGovernment",
		"https://management.chinacloudapi.cn": "AzureChinaCloud", "https://management.test": "",
	} {
		if got := cloudOf(origin); got != want {
			t.Errorf("cloudOf(%s) = %q", origin, got)
		}
	}
}

func TestProviderStates(t *testing.T) {
	f := newFake(t).onFile("GET", provPath("Microsoft.CognitiveServices"), "provider.json")
	a, _ := newAdapter(t, f)
	got, err := a.ProviderStates(context.Background(), testSub, []string{"Microsoft.CognitiveServices", "Microsoft.Missing"})
	want := []ProviderState{{"Microsoft.CognitiveServices", "Registered"}, {"Microsoft.Missing", "NotFound"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, %v", got, err)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "POST") || strings.Contains(c, "register") {
			t.Errorf("provider registration attempted: %s", c)
		}
	}
	a2, _ := newAdapter(t, newFake(t).on403("GET", provPath("Microsoft.X")))
	if _, err := a2.ProviderStates(context.Background(), testSub, []string{"Microsoft.X"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("restricted err = %v", err)
	}
	if out, err := a.ProviderStates(context.Background(), testSub, nil); err != nil || len(out) != 0 {
		t.Errorf("empty input: %v %v", out, err)
	}
}

func TestInventory(t *testing.T) {
	ctx := context.Background()
	t.Run("types filter and properties", func(t *testing.T) {
		f := newFake(t).on("GET", testRG+"/resources", reply{status: 200, body: fixture(t, "resources_p2.json")})
		a, _ := newAdapter(t, f)
		got, err := a.ListResources(ctx, InventoryQuery{Scope: testRG, Types: []string{"microsoft.keyvault/vaults"}, PropertyPaths: []string{"properties.provisioningState"}})
		if err != nil || len(got.Resources) != 1 || got.Resources[0].Type != "Microsoft.KeyVault/vaults" {
			t.Fatalf("got %+v, %v", got, err)
		}
		if !strings.Contains(f.calls[0], "filter=resourceType%20eq%20%27microsoft.keyvault%2Fvaults%27") {
			t.Errorf("filter not sent: %s", f.calls[0])
		}
		got, _ = a.ListResources(ctx, InventoryQuery{Scope: testRG, Types: []string{"Other/type"}})
		if len(got.Resources) != 0 {
			t.Errorf("client-side type filter failed: %+v", got)
		}
	})
	t.Run("provisioning state projection", func(t *testing.T) {
		f := newFake(t).on("GET", testScope+"/resources", reply{status: 200, body: fixture(t, "resources_p1.json")}, reply{status: 200, body: fixture(t, "resources_p2.json")})
		a, _ := newAdapter(t, f)
		got, err := a.ListResources(ctx, InventoryQuery{Scope: testScope, PropertyPaths: []string{"properties.provisioningState", "properties.secret"}})
		if err != nil || got.Resources[0].Properties["properties.provisioningState"] != "Succeeded" || len(got.Resources[0].Properties) != 1 {
			t.Errorf("got %+v, %v", got, err)
		}
		if got.Resources[0].Tags["env"] != "dev" || got.Resources[0].SKUName != "S0" || got.Resources[0].Kind != "AIServices" {
			t.Errorf("fields: %+v", got.Resources[0])
		}
	})
	t.Run("missing scope", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t))
		if _, err := a.ListResources(ctx, InventoryQuery{}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("restricted", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on403("GET", testScope+"/resources"))
		if _, err := a.ListResources(ctx, InventoryQuery{Scope: testScope}); !errors.Is(err, ErrUnavailable) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("resource group", func(t *testing.T) {
		f := newFake(t).on("GET", testScope+"/resourcegroups/rg-demo", reply{status: 200, body: `{"name":"rg-demo","location":"swedencentral"}`})
		a, _ := newAdapter(t, f)
		got, err := a.ResourceGroup(ctx, testSub, "rg-demo")
		if err != nil || got != (ResourceGroupInfo{Name: "rg-demo", Location: "swedencentral", Exists: true}) {
			t.Errorf("got %+v, %v", got, err)
		}
		got, err = a.ResourceGroup(ctx, testSub, "absent")
		if err != nil || got.Exists || got.Name != "absent" {
			t.Errorf("absent: %+v, %v", got, err)
		}
		a2, _ := newAdapter(t, newFake(t).on403("GET", testScope+"/resourcegroups/rg-demo"))
		if _, err := a2.ResourceGroup(ctx, testSub, "rg-demo"); !errors.Is(err, ErrUnavailable) {
			t.Errorf("restricted err = %v", err)
		}
	})
	t.Run("locks", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).onFile("GET", testRG+"/providers/Microsoft.Authorization/locks", "locks.json"))
		got, err := a.ListLocks(ctx, testRG+"/")
		want := []Lock{{ID: testRG + "/providers/Microsoft.Authorization/locks/no-delete", Name: "no-delete", Scope: testRG, Level: "CanNotDelete"}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("subnet", func(t *testing.T) {
		id := "/subscriptions/" + testSub + "/resourceGroups/rg-net/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/agents"
		a, _ := newAdapter(t, newFake(t).onFile("GET", id, "subnet.json"))
		got, err := a.GetSubnet(ctx, id)
		if err != nil || got.VNetID != strings.TrimSuffix(id, "/subnets/agents") || got.AddressPrefixes[0] != "10.0.1.0/24" ||
			got.DelegationServices[0] != "Microsoft.App/environments" || got.NSGID == "" || got.RouteTableID != "" || len(got.PrivateEndpointIDs) != 1 {
			t.Errorf("got %+v, %v", got, err)
		}
		a2, _ := newAdapter(t, newFake(t))
		if _, err := a2.GetSubnet(ctx, id); !isNotFound(err) {
			t.Errorf("missing subnet err = %v", err)
		}
	})
}

func TestModelsAndUsages(t *testing.T) {
	ctx := context.Background()
	mp := testScope + "/providers/Microsoft.CognitiveServices/locations/swedencentral/models"
	up := testScope + "/providers/Microsoft.CognitiveServices/locations/swedencentral/usages"
	f := newFake(t).onFile("GET", mp, "models.json").onFile("GET", up, "usages.json")
	a, _ := newAdapter(t, f)
	all, err := a.ListModels(ctx, ModelQuery{SubscriptionID: testSub, Location: "swedencentral"})
	if err != nil || len(all) != 3 || all[0].Name != "mistral-large" || all[1].Name != "gpt-4o" || all[2].Name != "text-embedding-3-small" {
		t.Fatalf("all = %+v, %v", all, err)
	}
	g := all[1]
	if g.DeprecationDate != "2026-12-01T00:00:00Z" || len(g.SKUs) != 2 || g.SKUs[0].Name != "GlobalStandard" || g.SKUs[0].UsageName != "OpenAI.GlobalStandard.gpt-4o" || g.SKUs[1].DefaultCapacity != 30 {
		t.Errorf("gpt-4o = %+v", g)
	}
	if all[2].LifecycleStatus != "Deprecated" || all[2].DeprecationDate == "" {
		t.Errorf("lifecycle/deprecation lost: %+v", all)
	}
	one, _ := a.ListModels(ctx, ModelQuery{SubscriptionID: testSub, Location: "swedencentral", Format: "openai", Name: "GPT-4O"})
	if len(one) != 1 {
		t.Errorf("filter: %+v", one)
	}
	none, _ := a.ListModels(ctx, ModelQuery{SubscriptionID: testSub, Location: "swedencentral", Name: "absent"})
	if len(none) != 0 {
		t.Errorf("absent model returned %+v", none)
	}
	us, err := a.ListUsages(ctx, testSub, "swedencentral")
	if err != nil || len(us) != 2 || us[1].Name != "OpenAI.Standard.gpt-4o" || us[1].Current != 20 || us[1].Limit != 30 {
		t.Errorf("usages = %+v, %v", us, err)
	}
	for _, bad := range []string{"", "swe/../x", "a b"} {
		if _, err := a.ListUsages(ctx, testSub, bad); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("location %q err = %v", bad, err)
		}
		if _, err := a.ListModels(ctx, ModelQuery{SubscriptionID: testSub, Location: bad}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("models location %q err = %v", bad, err)
		}
	}
	if _, err := a.ListUsages(ctx, "bad", "swedencentral"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad sub err = %v", err)
	}
	// Restricted identity: no Cognitive Services Usages Reader role.
	ar, _ := newAdapter(t, newFake(t).on403("GET", up))
	_, err = ar.ListUsages(ctx, testSub, "swedencentral")
	if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.CognitiveServices/locations/usages/read" {
		t.Errorf("restricted err = %v", err)
	}
}

func TestPolicy(t *testing.T) {
	ctx := context.Background()
	t.Run("assignments", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).onFile("GET", testScope+"/providers/Microsoft.Authorization/policyAssignments", "policyassignments.json"))
		got, err := a.ListAssignments(ctx, testScope)
		if err != nil || len(got) != 2 || got[0].Name != "pa1" || got[0].EnforcementMode != "DoNotEnforce" || got[1].DisplayName != "Allowed locations" || len(got[1].NotScopes) != 1 {
			t.Errorf("got %+v, %v", got, err)
		}
		if reflect.TypeOf(PolicyAssignment{}).NumField() != 8 {
			t.Error("PolicyAssignment gained a field: confirm it carries no parameter values")
		}
	})
	t.Run("exemptions", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).onFile("GET", testRG+"/providers/Microsoft.Authorization/policyExemptions", "exemptions.json"))
		got, err := a.ListExemptions(ctx, testRG)
		if err != nil || len(got) != 1 || got[0].Category != "Waiver" || got[0].Scope != testRG || got[0].ExpiresOn != "2027-01-01T00:00:00Z" {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("restricted", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on403("GET", testScope+"/providers/Microsoft.Authorization/policyAssignments"))
		if _, err := a.ListAssignments(ctx, testScope); !errors.Is(err, ErrUnavailable) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("check restrictions", func(t *testing.T) {
		path := testRG + "/providers/Microsoft.PolicyInsights/checkPolicyRestrictions"
		f := newFake(t).onFile("POST", path, "checkrestrictions.json")
		a, _ := newAdapter(t, f)
		in := map[string]any{"properties": map[string]any{"publicNetworkAccess": "Disabled"}}
		got, err := a.CheckRestrictions(ctx, PolicyRestrictionRequest{Scope: testRG, ResourceType: "Microsoft.CognitiveServices/accounts", APIVersion: "2026-09-01", Location: "swedencentral", Content: in})
		if err != nil || len(got) != 3 {
			t.Fatalf("got %+v, %v", got, err)
		}
		// observed denials: inferred evaluation (no field) then the location field; audit stays a potential conflict.
		if got[0].Kind != RestrictionObservedDenial || got[0].Effect != "Deny" || got[0].Field != "" ||
			got[1].Kind != RestrictionObservedDenial || got[1].Field != "location" || got[1].Result != "Removed" ||
			got[2].Kind != RestrictionPotentialConflict || got[2].Effect != "Audit" || got[2].Field != "tags" {
			t.Fatalf("honest mapping broken: %+v", got)
		}
		if _, polluted := in["type"]; polluted {
			t.Error("caller content map was mutated")
		}
		body := f.bodies["POST "+path][0]
		for _, want := range []string{`"type":"Microsoft.CognitiveServices/accounts"`, `"location":"swedencentral"`, `"apiVersion":"2026-09-01"`, `"includeAuditEffect":false`} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %s: %s", want, body)
			}
		}
		if _, err := a.CheckRestrictions(ctx, PolicyRestrictionRequest{Scope: testRG}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("missing input err = %v", err)
		}
		a2, _ := newAdapter(t, newFake(t).on403("POST", path))
		_, err = a2.CheckRestrictions(ctx, PolicyRestrictionRequest{Scope: testRG, ResourceType: "T/t", APIVersion: "v"})
		if u, ok := AsUnavailable(err); !ok || !strings.Contains(u.Permission, "checkPolicyRestrictions/read") {
			t.Errorf("restricted err = %v", err)
		}
	})
}

func TestNames(t *testing.T) {
	ctx := context.Background()
	t.Run("taken keyvault", func(t *testing.T) {
		path := testScope + "/providers/Microsoft.KeyVault/checkNameAvailability"
		f := newFake(t).onFile("POST", path, "name_taken.json")
		a, _ := newAdapter(t, f)
		got, err := a.CheckName(ctx, NameCheck{Kind: NameKeyVault, Name: "kv-demo", SubscriptionID: testSub})
		if err != nil || got != (NameResult{Available: false, Reason: "AlreadyExists"}) {
			t.Errorf("got %+v, %v", got, err)
		}
		if !strings.Contains(f.bodies["POST "+path][0], `"Microsoft.KeyVault/vaults"`) {
			t.Error("type missing from body")
		}
	})
	t.Run("foundry available", func(t *testing.T) {
		path := testScope + "/providers/Microsoft.CognitiveServices/checkDomainAvailability"
		a, _ := newAdapter(t, newFake(t).onFile("POST", path, "name_foundry_ok.json"))
		got, err := a.CheckName(ctx, NameCheck{Kind: NameFoundry, Name: "zeta", SubscriptionID: testSub, Location: "swedencentral"})
		if err != nil || !got.Available || got.Reason != "" {
			t.Errorf("got %+v, %v", got, err)
		}
		// FND-DEP-007: the check is subscription-level; Location is not required.
		if _, err := a.CheckName(ctx, NameCheck{Kind: NameFoundry, Name: "zeta", SubscriptionID: testSub}); err != nil {
			t.Errorf("no-location err = %v", err)
		}
	})
	t.Run("each kind routes to its provider", func(t *testing.T) {
		f := newFake(t)
		for p, body := range map[string]string{"Microsoft.Storage": `{"nameAvailable":true}`, "Microsoft.ContainerRegistry": `{"nameAvailable":true}`, "Microsoft.ApiManagement": `{"nameAvailable":true}`, "Microsoft.Search": `{"isNameAvailable":true}`} {
			f.on("POST", testScope+"/providers/"+p+"/checkNameAvailability", reply{status: 200, body: body})
		}
		a, _ := newAdapter(t, f)
		for _, k := range []NameKind{NameStorage, NameACR, NameAPIM, NameSearch} {
			if r, err := a.CheckName(ctx, NameCheck{Kind: k, Name: "abc", SubscriptionID: testSub}); err != nil || !r.Available {
				t.Errorf("kind %s: %+v, %v", k, r, err)
			}
		}
	})
	t.Run("invalid kind and malformed response", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on("POST", testScope+"/providers/Microsoft.KeyVault/checkNameAvailability", reply{status: 200, body: `{}`}))
		if _, err := a.CheckName(ctx, NameCheck{Kind: "bogus", Name: "a", SubscriptionID: testSub}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("kind err = %v", err)
		}
		if _, err := a.CheckName(ctx, NameCheck{Kind: NameKeyVault, Name: "a", SubscriptionID: testSub}); err == nil || errors.Is(err, ErrUnavailable) {
			t.Errorf("empty response must be a hard error, got %v", err)
		}
	})
	t.Run("restricted skips with permission", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on403("POST", testScope+"/providers/Microsoft.Search/checkNameAvailability"))
		_, err := a.CheckName(ctx, NameCheck{Kind: NameSearch, Name: "srch", SubscriptionID: testSub})
		if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.Search/checkNameAvailability/action" {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("soft deleted", func(t *testing.T) {
		f := newFake(t).onFile("GET", testScope+"/providers/Microsoft.KeyVault/deletedVaults", "deletedvaults.json")
		a, _ := newAdapter(t, f)
		got, err := a.ListSoftDeleted(ctx, NameKeyVault, testSub)
		if err != nil || len(got) != 1 || got[0].Name != "kv-old" || !got[0].PurgeProtection || got[0].Location != "swedencentral" || got[0].ScheduledPurgeDate == "" {
			t.Errorf("got %+v, %v", got, err)
		}
		before := f.callCount()
		for _, k := range []NameKind{NameStorage, NameSearch} {
			if out, err := a.ListSoftDeleted(ctx, k, testSub); err != nil || len(out) != 0 {
				t.Errorf("kind %s: %v %v", k, out, err)
			}
		}
		if f.callCount() != before {
			t.Error("kinds without soft-delete must not call Azure")
		}
		if _, err := a.ListSoftDeleted(ctx, "bogus", testSub); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("kind err = %v", err)
		}
		for _, c := range f.calls {
			if !strings.HasPrefix(c, "GET ") {
				t.Errorf("soft-delete listing used non-GET: %s", c)
			}
		}
		for _, k := range []NameKind{NameFoundry, NameAPIM} {
			if _, err := a.ListSoftDeleted(ctx, k, testSub); !isNotFound(err) {
				t.Errorf("kind %s routed wrongly: %v", k, err)
			}
		}
	})
}

func TestWhatIf(t *testing.T) {
	ctx := context.Background()
	runPath := testRG + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/whatIf"
	tmpl := []byte(`{"$schema":"x","resources":[]}`)
	req := WhatIfRequest{OptIn: true, Scope: testRG, Template: tmpl, Parameters: map[string]any{"name": "zeta"}}

	t.Run("no opt-in makes no call", func(t *testing.T) {
		f := newFake(t)
		a, _ := newAdapter(t, f)
		r := req
		r.OptIn = false
		if _, err := a.Run(ctx, r); !errors.Is(err, ErrWhatIfNotOptedIn) || f.callCount() != 0 {
			t.Errorf("err=%v calls=%d", err, f.callCount())
		}
	})
	t.Run("synchronous result", func(t *testing.T) {
		f := newFake(t).onFile("POST", runPath, "whatif_done.json")
		a, _ := newAdapter(t, f)
		got, err := a.Run(ctx, req)
		if err != nil || len(got.Changes) != 3 {
			t.Fatalf("got %+v, %v", got, err)
		}
		want := []ChangeType{ChangeReplace, ChangeNoChange, ChangeModify}
		for i, c := range got.Changes {
			if c.ChangeType != want[i] {
				t.Errorf("change %d = %s, want %s", i, c.ChangeType, want[i])
			}
		}
		if !got.Changes[0].Destructive() || got.Changes[2].Destructive() {
			t.Error("destructive classification wrong")
		}
		body := f.bodies["POST "+runPath][0]
		for _, w := range []string{`"ResourceIdOnly"`, `"Incremental"`, `"name":{"value":"zeta"}`} {
			if !strings.Contains(body, w) {
				t.Errorf("body missing %s: %s", w, body)
			}
		}
		for _, c := range f.calls {
			if strings.HasPrefix(c, "PUT") || strings.Contains(c, "/deployments/foundry-doctor-whatif?") {
				t.Errorf("a deployment write was attempted: %s", c)
			}
		}
	})
	t.Run("asynchronous poll", func(t *testing.T) {
		poll := testRG + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/operationStatuses/op1"
		f := newFake(t).
			on("POST", runPath, reply{status: 202, header: http.Header{"Location": {"https://management.test" + poll + "?api-version=2026-06-01"}, "Retry-After": {"3"}}}).
			on("GET", poll, reply{status: 202, header: http.Header{"Location": {"https://management.test" + poll}}}, reply{status: 200, body: fixture(t, "whatif_done.json")})
		a, sl := newAdapter(t, f)
		got, err := a.Run(ctx, req)
		if err != nil || len(got.Changes) != 3 || len(sl.d) != 2 || sl.d[0] != 3*time.Second {
			t.Errorf("got %+v, %v, sleeps %v", got, err, sl.d)
		}
	})
	t.Run("poll location off origin", func(t *testing.T) {
		f := newFake(t).on("POST", runPath, reply{status: 202, header: http.Header{"Location": {"https://evil.example/x"}}})
		a, _ := newAdapter(t, f)
		if _, err := a.Run(ctx, req); !errors.Is(err, ErrNotAllowed) || f.callCount() != 1 {
			t.Errorf("err=%v calls=%d", err, f.callCount())
		}
	})
	t.Run("failed keeps only codes", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).onFile("POST", runPath, "whatif_failed.json"))
		got, err := a.Run(ctx, req)
		if err != nil || len(got.Changes) != 0 || !reflect.DeepEqual(got.DiagnosticCodes, []string{"InvalidTemplate", "ResourceNotFound"}) {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("restricted identity", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on403("POST", runPath))
		_, err := a.Run(ctx, req)
		u, ok := AsUnavailable(err)
		if !ok || u.Permission != "Microsoft.Resources/deployments/whatIf/action" || !strings.Contains(err.Error(), "Microsoft.Resources/deployments/whatIf/action") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("invalid input", func(t *testing.T) {
		f := newFake(t)
		a, _ := newAdapter(t, f)
		for name, r := range map[string]WhatIfRequest{
			"no scope":             {OptIn: true, Template: tmpl},
			"bad template":         {OptIn: true, Scope: testRG, Template: []byte("{")},
			"sub without location": {OptIn: true, Scope: testScope, Template: tmpl},
		} {
			if _, err := a.Run(ctx, r); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%s err = %v", name, err)
			}
		}
		if f.callCount() != 0 {
			t.Error("invalid input reached the network")
		}
	})
	t.Run("subscription scope with location", func(t *testing.T) {
		p := testScope + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/whatIf"
		f := newFake(t).onFile("POST", p, "whatif_done.json")
		a, _ := newAdapter(t, f)
		if _, err := a.Run(ctx, WhatIfRequest{OptIn: true, Scope: testScope, Location: "swedencentral", Template: tmpl}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(f.bodies["POST "+p][0], `"location":"swedencentral"`) {
			t.Error("location missing")
		}
	})
}

func TestPermissions(t *testing.T) {
	ctx := context.Background()
	raPath := testRG + "/providers/Microsoft.Authorization/roleAssignments"
	daPath := testRG + "/providers/Microsoft.Authorization/denyAssignments"
	req := func(actions ...string) PermissionRequest { return PermissionRequest{Scope: testRG, Actions: actions} }

	t.Run("contributor with deny assignments", func(t *testing.T) {
		f := newFake(t).onFile("GET", raPath, "roleassignments_contributor.json").onFile("GET", roleContr, "roledef_contributor.json").onFile("GET", daPath, "denyassignments.json")
		a, _ := newAdapter(t, f)
		got, err := a.EffectiveActions(ctx, req(
			"Microsoft.CognitiveServices/accounts/write",
			"Microsoft.Authorization/roleAssignments/write",
			"Microsoft.CognitiveServices/accounts/delete",
			"Microsoft.Network/virtualNetworks/write"))
		if err != nil {
			t.Fatal(err)
		}
		want := []ActionDecision{
			{Action: "Microsoft.CognitiveServices/accounts/write", Decision: DecisionAllowed},
			{Action: "Microsoft.Authorization/roleAssignments/write", Decision: DecisionDenied, Reason: ReasonNoGrantingRole},
			{Action: "Microsoft.CognitiveServices/accounts/delete", Decision: DecisionDenied, Reason: ReasonDenyAssignment, DenyAssignmentIDs: []string{testScope + "/providers/Microsoft.Authorization/denyAssignments/d1"}},
			{Action: "Microsoft.Network/virtualNetworks/write", Decision: DecisionAllowed},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got  %+v\nwant %+v", got, want)
		}
		if !strings.Contains(f.calls[0], "assignedTo") {
			t.Errorf("assignedTo filter missing: %s", f.calls[0])
		}
	})
	t.Run("reader plus conditional contributor", func(t *testing.T) {
		f := newFake(t).onFile("GET", raPath, "roleassignments_reader_conditional.json").onFile("GET", roleContr, "roledef_contributor.json").onFile("GET", roleRead, "roledef_reader.json").onFile("GET", daPath, "empty_list.json")
		a, _ := newAdapter(t, f)
		got, err := a.EffectiveActions(ctx, req("Microsoft.CognitiveServices/accounts/read", "Microsoft.Resources/deployments/write"))
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Decision != DecisionAllowed || got[1].Decision != DecisionUnknown || got[1].Reason != ReasonConditionNotEvaluated || !got[1].ConditionSkipped {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("reader only is denied for writes (restricted identity)", func(t *testing.T) {
		f := newFake(t).on("GET", raPath, reply{status: 200, body: `{"value":[{"properties":{"roleDefinitionId":"` + roleRead + `","scope":"` + testScope + `"}}]}`}).
			onFile("GET", roleRead, "roledef_reader.json").on403("GET", daPath)
		a, _ := newAdapter(t, f)
		got, err := a.EffectiveActions(ctx, req("Microsoft.Resources/deployments/whatIf/action", "Microsoft.CognitiveServices/accounts/read"))
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Decision != DecisionDenied || got[0].Reason != ReasonNoGrantingRole {
			t.Errorf("write decision = %+v", got[0])
		}
		if got[1].Decision != DecisionUnknown || got[1].Reason != ReasonDenyAssignmentsUnreadable {
			t.Errorf("read decision with unreadable deny assignments = %+v", got[1])
		}
	})
	t.Run("cannot read role assignments", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on403("GET", raPath))
		_, err := a.EffectiveActions(ctx, req("A/b/c"))
		if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.Authorization/roleAssignments/read" {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("role definition unreadable", func(t *testing.T) {
		f := newFake(t).onFile("GET", raPath, "roleassignments_contributor.json").on403("GET", roleContr).onFile("GET", daPath, "empty_list.json")
		a, _ := newAdapter(t, f)
		got, err := a.EffectiveActions(ctx, req("A/b/c"))
		if err != nil || got[0].Decision != DecisionUnknown || got[0].Reason != ReasonAssignmentsUnreadable {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("missing input and identity", func(t *testing.T) {
		f := newFake(t)
		a, _ := newAdapter(t, f)
		if _, err := a.EffectiveActions(ctx, PermissionRequest{Actions: []string{"a/b"}}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("scope err = %v", err)
		}
		if got, err := a.EffectiveActions(ctx, PermissionRequest{Scope: testRG}); err != nil || len(got) != 0 {
			t.Errorf("empty actions: %v %v", got, err)
		}
		for name, tok := range map[string]string{"no oid": fakeJWT("", ""), "opaque": "opaque"} {
			a2, _ := newAdapter(t, f, func(o *Options) { o.Credential = staticCred{token: tok} })
			if _, err := a2.EffectiveActions(ctx, req("a/b")); !errors.Is(err, ErrUnavailable) {
				t.Errorf("%s err = %v", name, err)
			}
		}
		if f.callCount() != 0 {
			t.Error("identity failures reached the network")
		}
	})
	t.Run("data actions", func(t *testing.T) {
		role := `{"properties":{"permissions":[{"actions":[],"notActions":[],"dataActions":["Microsoft.CognitiveServices/*"],"notDataActions":["Microsoft.CognitiveServices/accounts/Foo/*"]}]}}`
		f := newFake(t).on("GET", raPath, reply{status: 200, body: `{"value":[{"properties":{"roleDefinitionId":"` + roleRead + `","scope":"` + testScope + `"}}]}`}).
			on("GET", roleRead, reply{status: 200, body: role}).onFile("GET", daPath, "empty_list.json")
		a, _ := newAdapter(t, f)
		got, err := a.EffectiveActions(ctx, PermissionRequest{Scope: testRG, IsDataAction: true, Actions: []string{"Microsoft.CognitiveServices/accounts/OpenAI/deployments/read", "Microsoft.CognitiveServices/accounts/Foo/x"}})
		if err != nil || got[0].Decision != DecisionAllowed || got[1].Decision != DecisionDenied {
			t.Errorf("got %+v, %v", got, err)
		}
	})
}

func TestActionMatch(t *testing.T) {
	cases := []struct {
		pattern, action string
		want            bool
	}{
		{"*", "A/b/c", true}, {"*/read", "Microsoft.X/y/read", true}, {"*/read", "Microsoft.X/y/write", false},
		{"Microsoft.Authorization/*/Write", "microsoft.authorization/roleAssignments/write", true},
		{"Microsoft.X/y/z", "Microsoft.X/y/z", true}, {"Microsoft.X/y.z", "Microsoft.X/yQz", false}, {"", "a", false},
	}
	for _, c := range cases {
		if got := actionMatch(c.pattern, c.action); got != c.want {
			t.Errorf("actionMatch(%q,%q) = %v", c.pattern, c.action, got)
		}
	}
	if !scopeCovers(testScope, testRG) || scopeCovers(testRG, testScope) || scopeCovers(testScope, testScope+"x") {
		t.Error("scopeCovers wrong")
	}
}
