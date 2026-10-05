package azure

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestInterfaceSurface enforces ADR-006: the client exposes only named
// operations. Adding a method to any capability interface fails this test until
// the allow-list below is reviewed and updated deliberately.
func TestInterfaceSurface(t *testing.T) {
	ifaces := map[string]struct {
		typ     reflect.Type
		allowed []string
	}{
		"ContextProvider":    {reflect.TypeOf((*ContextProvider)(nil)).Elem(), []string{"Context", "ProviderStates"}},
		"Inventory":          {reflect.TypeOf((*Inventory)(nil)).Elem(), []string{"GetSubnet", "ListLocks", "ListResources", "ResourceGroup"}},
		"Permissions":        {reflect.TypeOf((*Permissions)(nil)).Elem(), []string{"EffectiveActions"}},
		"Models":             {reflect.TypeOf((*Models)(nil)).Elem(), []string{"ListModels", "ListUsages"}},
		"Policy":             {reflect.TypeOf((*Policy)(nil)).Elem(), []string{"CheckRestrictions", "ListAssignments", "ListExemptions"}},
		"WhatIf":             {reflect.TypeOf((*WhatIf)(nil)).Elem(), []string{"Run"}},
		"Names":              {reflect.TypeOf((*Names)(nil)).Elem(), []string{"CheckName", "ListSoftDeleted"}},
		"RegionAvailability": {reflect.TypeOf((*RegionAvailability)(nil)).Elem(), []string{"ListLocations", "ProviderResourceTypes"}},
		"DeploymentHistory":  {reflect.TypeOf((*DeploymentHistory)(nil)).Elem(), []string{"CountDeployments"}},
		"SubnetLinks":        {reflect.TypeOf((*SubnetLinks)(nil)).Elem(), []string{"SubnetLinks"}},
		"PermissionEvidence": {reflect.TypeOf((*PermissionEvidence)(nil)).Elem(), []string{"CheckReportedActions", "ReportedPermissions"}},
	}
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	for name, c := range ifaces {
		var got []string
		for i := 0; i < c.typ.NumMethod(); i++ {
			m := c.typ.Method(i)
			got = append(got, m.Name)
			if m.Type.NumIn() == 0 || m.Type.In(0) != ctxType {
				t.Errorf("%s.%s must take context.Context first", name, m.Name)
			}
			if strings.Contains(strings.ToLower(m.Name), "send") || strings.Contains(strings.ToLower(m.Name), "do") && m.Name != "Do" && false {
				t.Errorf("%s.%s looks like a generic request method", name, m.Name)
			}
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, c.allowed) {
			t.Errorf("%s methods = %v, allow-list = %v", name, got, c.allowed)
		}
	}
	if n := reflect.TypeOf(Client{}).NumField(); n != len(ifaces) {
		t.Errorf("Client has %d fields, want %d", n, len(ifaces))
	}
}

func TestAllowListIsReadOnly(t *testing.T) {
	allowedPOST := map[string]bool{
		"policy.checkrestrictions": true, "whatif.run": true, "name.keyvault": true, "name.storage": true,
		"name.apim": true, "name.search": true, "name.foundry": true,
	}
	seen := map[string]bool{}
	for _, o := range AllowedOperations() {
		if seen[o.Name] {
			t.Errorf("duplicate operation %s", o.Name)
		}
		seen[o.Name] = true
		switch o.Method {
		case http.MethodGet:
		case http.MethodPost:
			if !allowedPOST[o.Name] {
				t.Errorf("POST operation %s is not an ADR-006 read-only POST", o.Name)
			}
		default:
			t.Errorf("operation %s uses forbidden verb %s", o.Name, o.Method)
		}
		low := strings.ToLower(o.Template)
		for _, bad := range []string{"listkeys", "purge", "recover", "register", "/write", "regeneratekey", "listcredentials", "/delete/"} {
			if strings.Contains(low, bad) {
				t.Errorf("operation %s template contains forbidden %q", o.Name, bad)
			}
		}
		if o.APIVersion == "" || o.Capability == "" {
			t.Errorf("operation %s lacks api-version or capability", o.Name)
		}
	}
	for name := range allowedPOST {
		if !seen[name] {
			t.Errorf("expected POST operation %s missing", name)
		}
	}
}

func TestIsAllowedNegatives(t *testing.T) {
	good := testScope + "/providers/Microsoft.Authorization/locks"
	cases := []struct {
		name, method, path string
		want               bool
	}{
		{"get lock list", "GET", good, true},
		{"put lock", "PUT", good + "/x", false},
		{"delete", "DELETE", testRG, false},
		{"patch", "PATCH", testRG, false},
		{"purge deleted vault", "POST", testScope + "/providers/Microsoft.KeyVault/locations/swedencentral/deletedVaults/kv/purge", false},
		{"list keys", "POST", testRG + "/providers/Microsoft.CognitiveServices/accounts/a/listKeys", false},
		{"register provider", "POST", testScope + "/providers/Microsoft.Foo/register", false},
		{"role assignment write", "PUT", testScope + "/providers/Microsoft.Authorization/roleAssignments/" + testSub, false},
		{"query in path", "GET", good + "?x=1", false},
		{"traversal", "GET", testScope + "/resourceGroups/../providers/Microsoft.Authorization/locks", false},
		{"get on POST-only op", "GET", testScope + "/providers/Microsoft.KeyVault/checkNameAvailability", false},
		{"post on GET-only op", "POST", good, false},
		{"bad subscription", "GET", "/subscriptions/not-a-guid/providers/Microsoft.Authorization/locks", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsAllowed(c.method, c.path); got != c.want {
				t.Errorf("IsAllowed(%s %s) = %v, want %v", c.method, c.path, got, c.want)
			}
		})
	}
}

func TestDoRejectsDisallowedBeforeNetwork(t *testing.T) {
	f := newFake(t)
	a, _ := newAdapter(t, f)
	ctx := context.Background()
	if _, err := a.c.do(ctx, "PUT", testRG, nil, nil); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("PUT err = %v", err)
	}
	if _, err := a.c.do(ctx, "DELETE", testRG, nil, nil); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("DELETE err = %v", err)
	}
	if _, err := a.c.do(ctx, "GET", testScope, url.Values{"foo": {"bar"}}, nil); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("unknown query key err = %v", err)
	}
	if _, err := a.c.do(ctx, "GET", testScope, nil, map[string]string{"a": "b"}); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("body on GET err = %v", err)
	}
	if f.callCount() != 0 {
		t.Errorf("network was called %d times", f.callCount())
	}
}

func TestHostileInputsNeverReachNetwork(t *testing.T) {
	f := newFake(t)
	a, _ := newAdapter(t, f)
	ctx := context.Background()
	hostile := []string{"a/../b", "x?y=1", "name with space", "a#b", "%2e%2e", strings.Repeat("a", 65), "", "a\nb"}
	for _, h := range hostile {
		if _, err := a.CheckName(ctx, NameCheck{Kind: NameKeyVault, Name: h, SubscriptionID: testSub}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("CheckName(%q) err = %v", h, err)
		}
		if len(h) == 65 {
			continue // valid length for resource groups (max 90); only name checks cap at 64
		}
		if _, err := a.ResourceGroup(ctx, testSub, h); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ResourceGroup(%q) err = %v", h, err)
		}
		if _, err := a.ProviderStates(ctx, testSub, []string{h}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ProviderStates(%q) err = %v", h, err)
		}
	}
	for _, scope := range []string{testScope + "?x=1", testRG + "/../x", "/subscriptions/bad", "https://evil.example/x"} {
		if _, err := a.ListLocks(ctx, scope); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("ListLocks(%q) err = %v", scope, err)
		}
	}
	if _, err := a.ListResources(ctx, InventoryQuery{Scope: testScope, Types: []string{"A' or 1 eq 1"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("hostile type filter err = %v", err)
	}
	if _, err := a.GetSubnet(ctx, testScope+"/../../x"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("hostile subnet err = %v", err)
	}
	if f.callCount() != 0 {
		t.Errorf("network was called %d times for hostile input", f.callCount())
	}
}

func TestNewRejectsInsecureEndpoint(t *testing.T) {
	for _, ep := range []string{"http://management.azure.com", "https://", "https://host/path", "ftp://x"} {
		if _, err := New(Options{Endpoint: ep, Credential: staticCred{token: "x"}}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("endpoint %q err = %v", ep, err)
		}
	}
}

func provPath(ns string) string { return testScope + "/providers/" + ns }

func TestRetryHonoursRetryAfter(t *testing.T) {
	f := newFake(t).on("GET", provPath("Microsoft.CognitiveServices"),
		reply{status: 429, header: http.Header{"Retry-After": {"2"}}},
		reply{status: 503},
		reply{status: 200, body: fixture(t, "provider.json")})
	a, sl := newAdapter(t, f)
	got, err := a.ProviderStates(context.Background(), testSub, []string{"Microsoft.CognitiveServices"})
	if err != nil || len(got) != 1 || got[0].State != "Registered" {
		t.Fatalf("got %v, %v", got, err)
	}
	if len(sl.d) != 2 || sl.d[0] != 2*time.Second || sl.d[1] != 1*time.Second {
		t.Errorf("sleeps = %v, want [2s 1s]", sl.d)
	}
}

func TestRetryBoundedAndCapped(t *testing.T) {
	f := newFake(t).on("GET", provPath("Microsoft.A"), reply{status: 429, header: http.Header{"Retry-After": {"999"}}})
	a, sl := newAdapter(t, f)
	_, err := a.ProviderStates(context.Background(), testSub, []string{"Microsoft.A"})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 429 {
		t.Fatalf("err = %v", err)
	}
	if f.callCount() != 4 || len(sl.d) != 3 {
		t.Errorf("calls=%d sleeps=%v, want 4 calls / 3 sleeps", f.callCount(), sl.d)
	}
	for _, d := range sl.d {
		if d != maxRetryAfter {
			t.Errorf("retry delay %v not capped at %v", d, maxRetryAfter)
		}
	}
}

func TestForbiddenAndUnauthorizedAreUnavailable(t *testing.T) {
	f := newFake(t).on403("GET", testScope+"/providers/Microsoft.Authorization/locks")
	a, _ := newAdapter(t, f)
	_, err := a.ListLocks(context.Background(), testScope)
	u, ok := AsUnavailable(err)
	if !ok || !errors.Is(err, ErrUnavailable) || u.Permission != "Microsoft.Authorization/locks/read" {
		t.Fatalf("err = %v", err)
	}
	if want := "capability unavailable: Microsoft.Authorization/locks/read at " + testScope + "/providers/Microsoft.Authorization/locks (missing permission: Microsoft.Authorization/locks/read)"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if strings.Contains(err.Error(), "secret-free text") {
		t.Error("response message leaked into error")
	}
	if f.callCount() != 1 {
		t.Errorf("403 must not be retried; calls=%d", f.callCount())
	}
	f401 := newFake(t).on("GET", testScope+"/providers/Microsoft.Authorization/locks", reply{status: 401, body: `{}`})
	a2, _ := newAdapter(t, f401)
	if _, err := a2.ListLocks(context.Background(), testScope); !errors.Is(err, ErrUnavailable) {
		t.Errorf("401 err = %v", err)
	}
}

func TestNoCredentialIsUnavailableWithoutNetwork(t *testing.T) {
	f := newFake(t)
	a, _ := newAdapter(t, f, func(o *Options) { o.Credential = staticCred{err: ErrNoCredential} })
	_, err := a.ListLocks(context.Background(), testScope)
	if u, ok := AsUnavailable(err); !ok || !strings.Contains(u.Reason, "azd auth login") {
		t.Errorf("err = %v", err)
	}
	if f.callCount() != 0 {
		t.Error("network used without credential")
	}
}

func TestContextCancellation(t *testing.T) {
	f := newFake(t)
	a, _ := newAdapter(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ListLocks(ctx, testScope); !errors.Is(err, context.Canceled) {
		t.Errorf("pre-cancelled err = %v", err)
	}
	f2 := newFake(t).on("GET", testScope+"/providers/Microsoft.Authorization/locks", reply{status: 200, body: "{}", delay: 5 * time.Second})
	a2, _ := newAdapter(t, f2)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()
	start := time.Now()
	if _, err := a2.ListLocks(ctx2, testScope); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("in-flight cancel err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("cancellation was not prompt")
	}
}

func TestConcurrencyIsBounded(t *testing.T) {
	f := newFake(t)
	var ns []string
	for _, n := range []string{"Microsoft.A", "Microsoft.B", "Microsoft.C", "Microsoft.D", "Microsoft.E", "Microsoft.F", "Microsoft.G", "Microsoft.H"} {
		ns = append(ns, n)
		f.on("GET", provPath(n), reply{status: 200, body: `{"registrationState":"Registered"}`, delay: 20 * time.Millisecond})
	}
	a, _ := newAdapter(t, f, func(o *Options) { o.MaxConcurrency = 2 })
	got, err := a.ProviderStates(context.Background(), testSub, ns)
	if err != nil || len(got) != 8 {
		t.Fatalf("got %v, %v", got, err)
	}
	for i, g := range got {
		if g.Namespace != ns[i] {
			t.Errorf("order not preserved at %d: %s", i, g.Namespace)
		}
	}
	if f.maxSeen > 2 {
		t.Errorf("max in-flight = %d, want <= 2", f.maxSeen)
	}
}

func TestPaginationAndTruncation(t *testing.T) {
	path := testScope + "/resources"
	f := newFake(t).on("GET", path,
		reply{status: 200, body: fixture(t, "resources_p1.json")},
		reply{status: 200, body: fixture(t, "resources_p2.json")})
	a, _ := newAdapter(t, f)
	got, err := a.ListResources(context.Background(), InventoryQuery{Scope: testScope})
	if err != nil || got.Truncated || got.Source != "arm-list" || len(got.Resources) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got.Resources[0].Name != "zeta" || got.Resources[1].Name != "alpha" || got.Resources[1].ResourceGroup != "rg-demo" {
		t.Errorf("not sorted by ID: %+v", got.Resources)
	}
	if !strings.Contains(f.calls[1], "skiptoken=abc") {
		t.Errorf("second page did not carry the skiptoken: %s", f.calls[1])
	}
	f2 := newFake(t).on("GET", path,
		reply{status: 200, body: fixture(t, "resources_p1.json")},
		reply{status: 200, body: fixture(t, "resources_p2.json")})
	a2, _ := newAdapter(t, f2)
	got, err = a2.ListResources(context.Background(), InventoryQuery{Scope: testScope, MaxResults: 1})
	if err != nil || !got.Truncated || len(got.Resources) != 1 {
		t.Errorf("truncation: %+v, %v", got, err)
	}
}

func TestNextLinkLeavingOriginIsRejected(t *testing.T) {
	f := newFake(t).on("GET", testScope+"/resources", reply{status: 200, body: `{"value":[],"nextLink":"https://evil.example/subscriptions/` + testSub + `/resources"}`})
	a, _ := newAdapter(t, f)
	if _, err := a.ListResources(context.Background(), InventoryQuery{Scope: testScope}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	if f.callCount() != 1 {
		t.Errorf("followed foreign nextLink: %d calls", f.callCount())
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	f := newFake(t).on("GET", testScope+"/providers/Microsoft.Authorization/locks",
		reply{status: 302, header: http.Header{"Location": {"https://evil.example/steal"}}})
	a, _ := newAdapter(t, f)
	_, err := a.ListLocks(context.Background(), testScope)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 302 || f.callCount() != 1 {
		t.Errorf("err=%v calls=%d", err, f.callCount())
	}
}

func TestErrorsNeverContainBodiesOrTokens(t *testing.T) {
	tok := fakeJWT(testOID, "")
	f := newFake(t).on("GET", testScope+"/providers/Microsoft.Authorization/locks", reply{status: 400, body: `{"error":{"code":"Bad Code!","message":"` + tok + `"}}`})
	a, _ := newAdapter(t, f)
	_, err := a.ListLocks(context.Background(), testScope)
	if err == nil || strings.Contains(err.Error(), tok) || strings.Contains(err.Error(), "Bad Code") {
		t.Errorf("err = %v", err)
	}
}
