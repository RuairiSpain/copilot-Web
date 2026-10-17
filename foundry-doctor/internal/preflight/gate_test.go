package preflight

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Tests for the Phase 2 gate fixes F1-F11. They use the fakes of
// preflight_test.go; gate_contract_test.go routes the same rules through the
// real adapter and the real allow-list.

const (
	rgID  = "/subscriptions/s/resourceGroups/rg"
	subID = "/subscriptions/s"
)

func textOf(out sdk.Result) string {
	text := ""
	if out.Skipped != nil {
		text = out.Skipped.Reason
	}
	for _, fd := range out.Findings {
		text += fd.Evidence + " " + fd.Resource.ID
	}
	return text
}

func tmplJSON(schema, params string) []byte {
	return []byte(`{"$schema":"` + schema + `","parameters":{` + params + `},"resources":[]}`)
}

// ---------------------------------------------------------------- F1 parameters

func TestResolveParameters(t *testing.T) {
	str := func(n string, def bool) tmplParam { return tmplParam{name: n, typ: "string", hasDefault: def} }
	cases := []struct {
		name       string
		params     []tmplParam
		file       map[string]any
		want       map[string]any
		unresolved []string
	}{
		{"file string value", []tmplParam{str("location", false)}, map[string]any{"location": "eastus"}, map[string]any{"location": "eastus"}, nil},
		{"key lookup is case-insensitive", []tmplParam{str("Location", false)}, map[string]any{"location": "eastus"}, map[string]any{"Location": "eastus"}, nil},
		{"absent with default sends nothing", []tmplParam{str("tier", true)}, nil, map[string]any{}, nil},
		{"absent without default is unresolved", []tmplParam{str("tier", false)}, nil, map[string]any{}, []string{"tier"}},
		{"set but unresolved beats the default", []tmplParam{str("tier", true)}, map[string]any{"tier": nil}, map[string]any{}, []string{"tier"}},
		{"expression-looking string is unresolved", []tmplParam{str("tier", true)}, map[string]any{"tier": "[concat('a','b')]"}, map[string]any{}, []string{"tier"}},
		{"secret-looking string parameter is never sent", []tmplParam{str("adminToken", false)}, map[string]any{"adminToken": "abc"}, map[string]any{}, []string{"adminToken"}},
		{"secure parameter is never sent, with default", []tmplParam{{name: "pw", typ: "securestring", hasDefault: true}}, nil, map[string]any{}, nil},
		{"secure parameter without default is unresolved", []tmplParam{{name: "pw", typ: "securestring"}}, nil, map[string]any{}, []string{"pw"}},
		{"secure parameter set in the file is unresolved, value not sent", []tmplParam{{name: "pw", typ: "securestring", hasDefault: true}}, map[string]any{"pw": "value-from-file"}, map[string]any{}, []string{"pw"}},
		{"secureObject is secure", []tmplParam{{name: "o", typ: "secureobject"}}, map[string]any{"o": "x"}, map[string]any{}, []string{"o"}},
		{"int from number", []tmplParam{{name: "n", typ: "int"}}, map[string]any{"n": float64(3)}, map[string]any{"n": int64(3)}, nil},
		{"int from numeric string", []tmplParam{{name: "n", typ: "int"}}, map[string]any{"n": "7"}, map[string]any{"n": int64(7)}, nil},
		{"int type mismatch", []tmplParam{{name: "n", typ: "int"}}, map[string]any{"n": "seven"}, map[string]any{}, []string{"n"}},
		{"bool from string", []tmplParam{{name: "b", typ: "bool"}}, map[string]any{"b": "true"}, map[string]any{"b": true}, nil},
		{"bool type mismatch", []tmplParam{{name: "b", typ: "bool"}}, map[string]any{"b": "yes"}, map[string]any{}, []string{"b"}},
		{"object value is unresolved", []tmplParam{{name: "o", typ: "object", hasDefault: true}}, map[string]any{"o": map[string]any{"a": 1}}, map[string]any{}, []string{"o"}},
		{"array parameter without value is unresolved", []tmplParam{{name: "a", typ: "array"}}, nil, map[string]any{}, []string{"a"}},
		{"unreadable declaration is unresolved", []tmplParam{{name: "x"}}, map[string]any{"x": "v"}, map[string]any{}, []string{"x"}},
		{"unresolved names are sorted", []tmplParam{str("b", false), str("a", false)}, nil, map[string]any{}, []string{"a", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, unresolved := resolveParameters(c.params, c.file)
			if !reflect.DeepEqual(got, c.want) || !reflect.DeepEqual(unresolved, c.unresolved) {
				t.Errorf("got %v unresolved %v; want %v unresolved %v", got, unresolved, c.want, c.unresolved)
			}
		})
	}
}

func TestParseTemplate(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		scope  deployScope
		params []string
		bad    bool
	}{
		{"resource group", `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#"}`, scopeResourceGroup, nil, false},
		{"older resource group schema", `{"$schema":"https://schema.management.azure.com/schemas/2015-01-01/deploymentTemplate.json#"}`, scopeResourceGroup, nil, false},
		{"subscription", `{"$schema":"https://schema.management.azure.com/schemas/2018-05-01/subscriptionDeploymentTemplate.json#"}`, scopeSubscription, nil, false},
		{"management group", `{"$schema":"https://schema.management.azure.com/schemas/2019-08-01/managementGroupDeploymentTemplate.json#"}`, scopeManagementGroup, nil, false},
		{"tenant", `{"$schema":"https://schema.management.azure.com/schemas/2019-08-01/tenantDeploymentTemplate.json#"}`, scopeTenant, nil, false},
		{"missing schema", `{"resources":[]}`, scopeUnknown, nil, false},
		{"foreign schema", `{"$schema":"https://example.test/deploymentTemplate.json.evil"}`, scopeUnknown, nil, false},
		{"parameters are listed sorted", `{"$schema":"x/deploymentTemplate.json","parameters":{"b":{"type":"String","defaultValue":"x"},"a":{"type":"securestring"}}}`, scopeResourceGroup, []string{"a", "b"}, false},
		{"not json", `{`, scopeUnknown, nil, true},
		{"empty", ``, scopeUnknown, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tf := parseTemplate([]byte(c.raw))
			if (tf.err != nil) != c.bad || tf.scope != c.scope {
				t.Fatalf("got scope %v err %v", tf.scope, tf.err)
			}
			var names []string
			for _, p := range tf.params {
				names = append(names, p.name)
			}
			if !reflect.DeepEqual(names, c.params) {
				t.Errorf("params = %v, want %v", names, c.params)
			}
		})
	}
	tf := parseTemplate([]byte(`{"$schema":"x/deploymentTemplate.json","parameters":{"b":{"type":"String","defaultValue":"x"}}}`))
	if tf.params[0].typ != "string" || !tf.params[0].hasDefault {
		t.Errorf("type/default lost: %+v", tf.params[0])
	}
}

func TestWhatIfParameters(t *testing.T) {
	storage := res("Microsoft.Storage/storageAccounts", "stfoo")
	pw := `"admin` + `Pw":{"type":"securestring"}`
	cases := []struct {
		name       string
		params     string
		file       map[string]any
		wantRun    bool
		wantParams map[string]any
		wantText   string
	}{
		{"all resolvable values are passed", `"location":{"type":"string"},"count":{"type":"int","defaultValue":2}`, map[string]any{"location": "eastus"}, true, map[string]any{"location": "eastus"}, ""},
		{"no parameters at all", ``, nil, true, map[string]any{}, ""},
		{"missing required value skips with the names", `"location":{"type":"string"},"env":{"type":"string"}`, map[string]any{"location": "eastus"}, false, nil, "template parameters unresolved: env"},
		{"set but unresolved value skips", `"location":{"type":"string"}`, map[string]any{"location": nil}, false, nil, "template parameters unresolved: location"},
		{"secure parameter without default skips and is never sent", `"location":{"type":"string"},` + pw, map[string]any{"location": "eastus", "adminPw": "s3cret-value"}, false, nil, "template parameters unresolved: adminPw"},
		{"secure parameter with default is not sent", `"location":{"type":"string"},"adminPw":{"type":"securestring","defaultValue":"d"}`, map[string]any{"location": "eastus"}, true, map[string]any{"location": "eastus"}, ""},
		{"several unresolved names are all listed sorted", `"zeta":{"type":"string"},"alpha":{"type":"string"}`, nil, false, nil, "template parameters unresolved: alpha, zeta"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{}
			d := deps(f)
			d.Template = tmplJSON(rgSchema, c.params)
			d.Parameters = c.file
			out := run(t, d, "FND-DEP-008", storage)
			if c.wantRun {
				if f.whatIfRan != 1 || !reflect.DeepEqual(f.lastWhatIf.Parameters, c.wantParams) {
					t.Fatalf("ran %d, parameters %v, want %v (%+v)", f.whatIfRan, f.lastWhatIf.Parameters, c.wantParams, out)
				}
				if classify(out) != oUncertain {
					t.Errorf("clean what-if must be uncertain, got %s", classify(out))
				}
				for k, v := range f.lastWhatIf.Parameters {
					if strings.Contains(fmt.Sprint(k, v), "s3cret") {
						t.Errorf("a secret reached the what-if request: %v", f.lastWhatIf.Parameters)
					}
				}
				return
			}
			if f.whatIfRan != 0 {
				t.Fatal("what-if ran with unresolved template parameters")
			}
			if classify(out) != oSkipped || !strings.Contains(out.Skipped.Reason, c.wantText) || !strings.HasPrefix(out.Skipped.Reason, sdk.SkipInputUnavailable) {
				t.Fatalf("got %+v", out)
			}
		})
	}
}

func TestUnresolvedParametersNeverPassDEP010CanNotDeleteBranch(t *testing.T) {
	cnd := azure.Lock{ID: "/l/cnd", Name: "cnd", Scope: rgID, Level: "CanNotDelete"}
	f := &fake{locks: []azure.Lock{cnd}, deploys: azure.DeploymentCount{Count: 1}}
	d := deps(f)
	d.Template = tmplJSON(rgSchema, `"location":{"type":"string"}`)
	out := run(t, d, "FND-DEP-010", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	if classify(out) != oUncertain || !strings.Contains(out.Skipped.Reason, "could not be predicted without what-if") {
		t.Fatalf("got %+v", out)
	}
	if f.whatIfRan != 0 {
		t.Error("what-if ran with unresolved parameters")
	}
}

// ------------------------------------------------------------------ F2 scope

func TestWhatIfScope(t *testing.T) {
	storage := res("Microsoft.Storage/storageAccounts", "stfoo")
	cases := []struct {
		name      string
		tmpl      []byte
		mutate    func(*fake, *Deps)
		wantRun   bool
		wantScope string
		wantLoc   string
		skipText  string
	}{
		{"resource group scope", tmplJSON(rgSchema, ``), nil, true, rgID, "", ""},
		{"subscription scope uses the location", tmplJSON(subSchema, ``), nil, true, subID, "eastus", ""},
		{"subscription scope without location", tmplJSON(subSchema, ``), func(_ *fake, d *Deps) { d.Target.Location = "" }, false, "", "", "target location is not set"},
		{"subscription scope needs no resource group", tmplJSON(subSchema, ``), func(_ *fake, d *Deps) { d.Target.ResourceGroup = "" }, true, subID, "eastus", ""},
		{"management group scope", tmplJSON("https://schema.management.azure.com/schemas/2019-08-01/managementGroupDeploymentTemplate.json#", ``), nil, false, "", "", "managementGroup-scope template is not supported"},
		{"tenant scope", tmplJSON("https://schema.management.azure.com/schemas/2019-08-01/tenantDeploymentTemplate.json#", ``), nil, false, "", "", "tenant-scope template is not supported"},
		{"unknown scope", []byte(`{"resources":[]}`), nil, false, "", "", "could not be determined"},
		{"resource group does not exist", tmplJSON(rgSchema, ``), func(f *fake, _ *Deps) { f.rgMissing = true }, false, "", "", "target resource group rg does not exist"},
		{"resource group lookup forbidden names the capability", tmplJSON(rgSchema, ``), func(f *fake, _ *Deps) {
			f.err = &azure.UnavailableError{Capability: "resource group read", Permission: "Microsoft.Resources/subscriptions/resourceGroups/read"}
		}, false, "", "", "Microsoft.Resources/subscriptions/resourceGroups/read"},
		{"missing resource group in target", tmplJSON(rgSchema, ``), func(_ *fake, d *Deps) { d.Target.ResourceGroup = "" }, false, "", "", "target resource group is not set"},
		{"unreadable template", []byte(`{`), nil, false, "", "", "could not be read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{}
			d := deps(f)
			d.Template = c.tmpl
			if c.mutate != nil {
				c.mutate(f, &d)
			}
			out := run(t, d, "FND-DEP-008", storage)
			if c.wantRun {
				if f.whatIfRan != 1 || f.lastWhatIf.Scope != c.wantScope || f.lastWhatIf.Location != c.wantLoc {
					t.Fatalf("ran %d scope %q location %q; want scope %q location %q (%+v)", f.whatIfRan, f.lastWhatIf.Scope, f.lastWhatIf.Location, c.wantScope, c.wantLoc, out)
				}
				return
			}
			if f.whatIfRan != 0 || classify(out) != oSkipped || !strings.Contains(out.Skipped.Reason, c.skipText) {
				t.Fatalf("ran %d; got %+v, want skip containing %q", f.whatIfRan, out, c.skipText)
			}
			if strings.Contains(out.Skipped.Reason, "Azure call failed") {
				t.Errorf("the skip must be named, not generic: %q", out.Skipped.Reason)
			}
		})
	}
}

func TestDEP003Scope(t *testing.T) {
	r := res("Microsoft.CognitiveServices/accounts", "acct")
	cases := []struct {
		name       string
		tmpl       []byte
		mutate     func(*Deps)
		want       outcome
		wantScope  string
		evidence   bool
		wantText   string
		decisionOf map[string]azure.Decision
	}{
		{"resource group template uses the permissions API at the group", tmplJSON(rgSchema, ``), nil, oUncertain, rgID, true, "", nil},
		{"subscription template uses role assignments at the subscription", tmplJSON(subSchema, ``), nil, oUncertain, subID, false, "non-conclusive", nil},
		{"subscription template: missing write is a finding at the subscription", tmplJSON(subSchema, ``), nil, oFail, subID, false, "at the subscription", map[string]azure.Decision{"Microsoft.Authorization/roleAssignments/write": azure.DecisionDenied, "Microsoft.CognitiveServices/accounts/write": azure.DecisionDenied}},
		{"subscription template without role-assignment evaluation is skipped", tmplJSON(subSchema, ``), func(d *Deps) { d.Permissions = nil }, oSkipped, "", false, "no subscription-scope variant", nil},
		{"subscription template needs no resource group", tmplJSON(subSchema, ``), func(d *Deps) { d.Target.ResourceGroup = "" }, oUncertain, subID, false, "", nil},
		{"resource group template needs a resource group", tmplJSON(rgSchema, ``), func(d *Deps) { d.Target.ResourceGroup = "" }, oSkipped, "", false, "resource group is not set", nil},
		{"management group template is skipped", tmplJSON("x/managementGroupDeploymentTemplate.json#", ``), nil, oSkipped, "", false, "managementGroup-scope", nil},
		{"tenant template is skipped", tmplJSON("x/tenantDeploymentTemplate.json#", ``), nil, oSkipped, "", false, "tenant-scope", nil},
		{"unknown scope assumes the resource group and says so", []byte(`{"resources":[]}`), nil, oUncertain, rgID, true, "scope is unknown", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{providers: []azure.ProviderState{{Namespace: "Microsoft.CognitiveServices", State: "Registered"}}, decisions: c.decisionOf}
			d := deps(f)
			d.Template = c.tmpl
			if c.mutate != nil {
				c.mutate(&d)
			}
			if c.name == "subscription template: missing write is a finding at the subscription" {
				f.decisions = c.decisionOf
			}
			out := run(t, d, "FND-DEP-003", r)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if c.wantScope != "" {
				if len(f.permScopes) == 0 || f.permScopes[0] != c.wantScope {
					t.Errorf("permissions read at %v, want %s", f.permScopes, c.wantScope)
				}
				if f.evidenceRan != c.evidence {
					t.Errorf("Permissions API evidence used = %v, want %v", f.evidenceRan, c.evidence)
				}
			}
			if !strings.Contains(textOf(out), c.wantText) {
				t.Errorf("output lacks %q: %q", c.wantText, textOf(out))
			}
		})
	}
}

// ------------------------------------------------------------- F3 DEP-002

func TestDEP002(t *testing.T) {
	sid := rgID + "/providers/Microsoft.Network/virtualNetworks/v/subnets/agents"
	all := func(state string) []azure.ProviderState {
		var out []azure.ProviderState
		for _, ns := range networkSecuredNamespaces {
			out = append(out, azure.ProviderState{Namespace: ns, State: state})
		}
		return out
	}
	with := func(base []azure.ProviderState, ns, state string) []azure.ProviderState {
		out := append([]azure.ProviderState(nil), base...)
		for i := range out {
			if out[i].Namespace == ns {
				out[i].State = state
			}
		}
		return out
	}
	storage := res("Microsoft.Storage/storageAccounts", "stfoo")
	cases := []struct {
		name      string
		rs        []sdk.ARMResource
		providers []azure.ProviderState
		mutate    func(*Deps)
		want      outcome
		text      string
	}{
		{"network-secured: all required providers registered", []sdk.ARMResource{agentAccount(sid)}, all("Registered"), nil, oPass, ""},
		{"network-secured: Microsoft.App not registered", []sdk.ARMResource{agentAccount(sid)}, with(all("Registered"), "Microsoft.App", "NotRegistered"), nil, oFail, "Microsoft.App is NotRegistered"},
		{"network-secured: Microsoft.ContainerService missing", []sdk.ARMResource{agentAccount(sid)}, with(all("Registered"), "Microsoft.ContainerService", "NotFound"), nil, oFail, "Microsoft.ContainerService"},
		{"network-secured: Registering is not a pass", []sdk.ARMResource{agentAccount(sid)}, with(all("Registered"), "Microsoft.App", "Registering"), nil, oUncertain, "Microsoft.App is Registering"},
		{"network-secured: a finding wins over a Registering note", []sdk.ARMResource{agentAccount(sid)}, with(with(all("Registered"), "Microsoft.App", "Registering"), "Microsoft.Search", "NotRegistered"), nil, oFail, "Microsoft.Search"},
		{"network-secured: state not returned is uncertain", []sdk.ARMResource{agentAccount(sid)}, all("Registered")[1:], nil, oUncertain, "was not returned"},
		{"not network-secured: Microsoft.App is not required", []sdk.ARMResource{res(typeAccount, "acct"), storage}, with(all("Registered"), "Microsoft.App", "NotRegistered"), nil, oPass, ""},
		{"template-defined provider that is unregistered is not reported here", []sdk.ARMResource{storage}, []azure.ProviderState{{Namespace: "Microsoft.Storage", State: "NotRegistered"}}, nil, oPass, ""},
		{"no Azure context is skipped naming the capability", []sdk.ARMResource{agentAccount(sid)}, nil, func(d *Deps) { d.Context = nil }, oSkipped, "Microsoft.Resources/subscriptions/providers/read"},
		{"no subscription is skipped", []sdk.ARMResource{agentAccount(sid)}, nil, func(d *Deps) { d.Target.SubscriptionID = "" }, oSkipped, "subscription"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps(&fake{providers: c.providers})
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-002", c.rs...)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
		})
	}
}

// ------------------------------------------------------------- F4 locks (DEP-010)

func TestDEP010LockApplicability(t *testing.T) {
	planned := rgID + "/providers/Microsoft.Storage/storageAccounts/stfoo123"
	lock := func(level, scope string) azure.Lock {
		return azure.Lock{ID: scope + "/providers/Microsoft.Authorization/locks/l", Name: "l", Scope: scope, Level: level}
	}
	cases := []struct {
		name     string
		locks    []azure.Lock
		resLocks map[string][]azure.Lock
		mutate   func(*Deps)
		rs       []sdk.ARMResource
		want     outcome
		text     string
	}{
		{"ReadOnly lock on an unrelated resource group is ignored", []azure.Lock{lock("ReadOnly", subID+"/resourceGroups/other")}, nil, nil, nil, oPass, ""},
		{"ReadOnly lock on an unrelated resource in the group is ignored", []azure.Lock{lock("ReadOnly", rgID+"/providers/Microsoft.Storage/storageAccounts/other")}, nil, nil, nil, oPass, ""},
		{"ReadOnly lock on a child of a planned resource does not cover it", []azure.Lock{lock("ReadOnly", planned+"/blobServices/default")}, nil, nil, nil, oPass, ""},
		{"ReadOnly lock on the target group fails", []azure.Lock{lock("ReadOnly", rgID)}, nil, nil, nil, oFail, "blocks writes"},
		{"ReadOnly lock on the subscription fails", []azure.Lock{lock("ReadOnly", subID)}, nil, nil, nil, oFail, "blocks writes"},
		{"ReadOnly lock on a planned resource, listed by the group, fails", []azure.Lock{lock("ReadOnly", planned)}, nil, nil, nil, oFail, planned},
		{"ReadOnly lock on a planned resource, found only at resource level, fails", nil, map[string][]azure.Lock{planned: {lock("ReadOnly", planned)}}, nil, nil, oFail, planned},
		{"resource-level lock on a parent covers a planned child", nil, map[string][]azure.Lock{
			rgID + "/providers/Microsoft.CognitiveServices/accounts/acct/deployments/gpt": {lock("ReadOnly", rgID+"/providers/Microsoft.CognitiveServices/accounts/acct")}},
			nil, []sdk.ARMResource{res("Microsoft.CognitiveServices/accounts/deployments", "acct/gpt")}, oFail, "accounts/acct"},
		{"CanNotDelete alone is not a write block", []azure.Lock{lock("CanNotDelete", rgID)}, nil, func(d *Deps) { d.WhatIfOptIn = true }, nil, oUncertain, ""},
		{"NotSpecified lock is ignored", []azure.Lock{lock("NotSpecified", rgID)}, nil, nil, nil, oPass, ""},
		{"a lock without a readable scope is uncertain", []azure.Lock{{ID: "/x/locks/l", Name: "l", Level: "ReadOnly"}}, nil, nil, nil, oUncertain, "no readable scope"},
		{"resource-level reads unavailable is uncertain", nil, nil, func(d *Deps) { d.Extras = nil }, nil, oUncertain, "resource-level lock reads are unavailable"},
		{"a planned resource with an expression name is uncertain", nil, nil, nil, []sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "[parameters('n')]")}, oUncertain, "unresolved name or type"},
		{"no locks at all passes", nil, nil, nil, nil, oPass, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{locks: c.locks, resLocks: c.resLocks}
			d := deps(f)
			if c.mutate != nil {
				c.mutate(&d)
			}
			rs := c.rs
			if rs == nil {
				rs = []sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "stfoo123")}
			}
			out := run(t, d, "FND-DEP-010", rs...)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
		})
	}
}

func TestDEP010ReadsOnlySubscriptionGroupAndPlannedResources(t *testing.T) {
	f := &fake{}
	run(t, deps(f), "FND-DEP-010", res("Microsoft.Storage/storageAccounts", "stfoo123"), res("Microsoft.CognitiveServices/accounts/deployments", "acct/gpt"))
	if !reflect.DeepEqual(f.scopes, []string{subID, rgID}) {
		t.Errorf("ListLocks scopes = %v", f.scopes)
	}
	want := []string{
		rgID + "/providers/Microsoft.CognitiveServices/accounts/acct/deployments/gpt",
		rgID + "/providers/Microsoft.Storage/storageAccounts/stfoo123",
	}
	if !reflect.DeepEqual(f.resLockAsks, want) {
		t.Errorf("resource-level lock reads = %v, want %v", f.resLockAsks, want)
	}
}

func TestDEP010ResourceLockLookupIsBounded(t *testing.T) {
	var rs []sdk.ARMResource
	for i := 0; i < maxResourceLockLookups+10; i++ {
		rs = append(rs, res("Microsoft.Storage/storageAccounts", fmt.Sprintf("st%03d", i)))
	}
	f := &fake{}
	out := run(t, deps(f), "FND-DEP-010", rs...)
	if len(f.resLockAsks) != maxResourceLockLookups || classify(out) != oUncertain || !strings.Contains(out.Skipped.Reason, "only the first") {
		t.Errorf("asks=%d out=%+v", len(f.resLockAsks), out)
	}
}

func TestPlannedIDs(t *testing.T) {
	rs := []sdk.ARMResource{
		res("Microsoft.Storage/storageAccounts", "st1"),
		res("Microsoft.Storage/storageAccounts", "ST1"), // same ID, different case
		res("Microsoft.CognitiveServices/accounts/projects", "acct/proj"),
		res("Microsoft.CognitiveServices/accounts/projects", "onlyone"), // segment mismatch
		res("Microsoft.Storage/storageAccounts", "[x]"),
		res("Microsoft.Authorization/roleAssignments", "ra"),
	}
	got, unresolved := plannedIDs(rgID, rs)
	var ids []string
	for _, g := range got {
		ids = append(ids, g.id)
	}
	want := []string{
		rgID + "/providers/Microsoft.CognitiveServices/accounts/acct/projects/proj",
		rgID + "/providers/Microsoft.Storage/storageAccounts/st1",
	}
	if !reflect.DeepEqual(ids, want) || unresolved != 2 {
		t.Errorf("ids=%v unresolved=%d", ids, unresolved)
	}
}

// ------------------------------------------------------------- F5 DEP-009

func TestDEP009NeverSendsExpressionsOrSecrets(t *testing.T) {
	r := res("Microsoft.CognitiveServices/accounts", "[parameters('accountName')]")
	pwKey := "admin" + "Password"
	r.Properties = map[string]any{
		"publicNetworkAccess": "[parameters('pna')]",
		"disableLocalAuth":    true,
		"customSubDomainName": "literal-sub",
		pwKey:                 "literal-pw",
		"networkAcls":         map[string]any{"defaultAction": "Deny", "ipRules": []any{map[string]any{"value": "[parameters('ip')]"}}},
	}
	f := &fake{}
	out := run(t, deps(f), "FND-DEP-009", r)
	if classify(out) != oUncertain {
		t.Fatalf("partial content must be uncertain: %+v", out)
	}
	if len(f.restrReqs) != 1 {
		t.Fatalf("requests = %d", len(f.restrReqs))
	}
	body := fmt.Sprintf("%#v", f.restrReqs[0].Content)
	for _, bad := range []string{"parameters(", "literal-pw", pwKey, "accountName", "publicNetworkAccess"} {
		if strings.Contains(body, bad) {
			t.Errorf("content sent to the policy service contains %q: %s", bad, body)
		}
	}
	for _, want := range []string{"literal-sub", "disableLocalAuth", "defaultAction"} {
		if !strings.Contains(body, want) {
			t.Errorf("literal content %q was dropped: %s", want, body)
		}
	}
	if !strings.Contains(out.Skipped.Reason, "partial content") || !strings.Contains(out.Skipped.Reason, "publicNetworkAccess") {
		t.Errorf("omissions not reported: %q", out.Skipped.Reason)
	}
	if strings.Contains(out.Skipped.Reason, "literal-pw") {
		t.Error("a secret value leaked into the report")
	}
}

func TestDEP009Mapping(t *testing.T) {
	cases := []struct {
		name string
		rst  []azure.PolicyRestriction
		want outcome
		text string
	}{
		{"deny effect fails", []azure.PolicyRestriction{{Kind: azure.RestrictionObservedDenial, Effect: "deny", AssignmentID: "a"}}, oFail, "likely denies"},
		{"field restriction result Deny (mapped to a deny effect) fails", []azure.PolicyRestriction{{Kind: azure.RestrictionObservedDenial, Effect: "deny", Result: "Deny", Field: "location", AssignmentID: "a"}}, oFail, "likely denies"},
		{"observed denial kind fails even when the effect text is empty", []azure.PolicyRestriction{{Kind: azure.RestrictionObservedDenial, AssignmentID: "a"}}, oFail, ""},
		{"audit is informational", []azure.PolicyRestriction{{Kind: azure.RestrictionPotentialConflict, Effect: "audit", AssignmentID: "a"}}, oUncertain, "informational"},
		{"required field restriction is informational", []azure.PolicyRestriction{{Kind: azure.RestrictionPotentialConflict, Result: "Required", AssignmentID: "a"}}, oUncertain, "informational"},
		{"clean is never a pass", nil, oUncertain, "cannot prove"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := run(t, deps(&fake{restr: c.rst}), "FND-DEP-009", res("Microsoft.Storage/storageAccounts", "stfoo123"))
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
		})
	}
}

func TestDEP009MissingCapabilityAndInputs(t *testing.T) {
	d := deps(&fake{})
	d.Policy = nil
	out := run(t, d, "FND-DEP-009", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	if classify(out) != oSkipped || !strings.Contains(out.Skipped.Reason, "Microsoft.PolicyInsights/checkPolicyRestrictions/read") {
		t.Errorf("got %+v", out)
	}
	if strings.Contains(out.Skipped.Reason, "policyStates") {
		t.Errorf("stale permission text: %q", out.Skipped.Reason)
	}
	r := res("Microsoft.Storage/storageAccounts", "stfoo123")
	r.APIVersion = "[parameters('v')]"
	f := &fake{}
	out = run(t, deps(f), "FND-DEP-009", r)
	if len(f.restrReqs) != 0 || classify(out) != oUncertain {
		t.Errorf("expression API version must not be sent: %d %+v", len(f.restrReqs), out)
	}
	d = deps(&fake{err: &azure.UnavailableError{Capability: "policy", Permission: "Microsoft.PolicyInsights/checkPolicyRestrictions/read"}})
	out = run(t, d, "FND-DEP-009", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	if classify(out) != oSkipped || !strings.Contains(out.Skipped.Reason, "checkPolicyRestrictions/read") {
		t.Errorf("restricted: %+v", out)
	}
}

// ------------------------------------------------------------- F9 DEP-001

func TestDEP001(t *testing.T) {
	ok := azure.Context{SubscriptionID: "s", SubscriptionState: "Enabled", SubscriptionTenantID: "t", TenantID: "t"}
	cases := []struct {
		name   string
		ctx    func(azure.Context) azure.Context
		mutate func(*Deps)
		err    error
		want   outcome
		text   string
	}{
		{"aligned", func(c azure.Context) azure.Context { return c }, nil, nil, oPass, ""},
		{"tenant mismatch fails", func(c azure.Context) azure.Context { c.SubscriptionTenantID = "other"; return c }, nil, nil, oFail, "belongs to tenant other, expected t"},
		{"tenant comparison is case-insensitive", func(c azure.Context) azure.Context { c.SubscriptionTenantID = "T"; return c }, nil, nil, oPass, ""},
		{"disabled subscription fails", func(c azure.Context) azure.Context { c.SubscriptionState = "Disabled"; return c }, nil, nil, oFail, "state is Disabled"},
		{"warned subscription fails", func(c azure.Context) azure.Context { c.SubscriptionState = "Warned"; return c }, nil, nil, oFail, "state is Warned"},
		{"past due subscription fails", func(c azure.Context) azure.Context { c.SubscriptionState = "PastDue"; return c }, nil, nil, oFail, "PastDue"},
		{"unreported state is not a failure", func(c azure.Context) azure.Context { c.SubscriptionState = ""; return c }, nil, nil, oPass, ""},
		{"subscription id mismatch fails", func(c azure.Context) azure.Context { c.SubscriptionID = "other"; return c }, nil, nil, oFail, "differs from the requested subscription"},
		{"unset expected tenant is uncertain", func(c azure.Context) azure.Context { return c }, func(d *Deps) { d.Target.TenantID = "" }, nil, oUncertain, "AZURE_TENANT_ID"},
		{"unreported owning tenant is uncertain", func(c azure.Context) azure.Context { c.SubscriptionTenantID = ""; return c }, nil, nil, oUncertain, "owning tenant was not reported"},
		{"no Azure context is skipped", nil, func(d *Deps) { d.Context = nil }, nil, oSkipped, "Microsoft.Resources/subscriptions/read"},
		{"restricted identity names the capability", nil, nil, &azure.UnavailableError{Capability: "subscription", Permission: "Microsoft.Resources/subscriptions/read"}, oSkipped, "Microsoft.Resources/subscriptions/read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{err: c.err}
			if c.ctx != nil {
				f.ctxInfo = c.ctx(ok)
			}
			d := deps(f)
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-001")
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
		})
	}
}

// ------------------------------------------------------------- F9 DEP-004

func gptModel(version, status, deprecation string, skus ...string) azure.ModelAvailability {
	m := azure.ModelAvailability{Format: "OpenAI", Name: "gpt-4o", Version: version, LifecycleStatus: status, DeprecationDate: deprecation}
	for _, s := range skus {
		m.SKUs = append(m.SKUs, azure.ModelSKU{Name: s, UsageName: "OpenAI." + s + ".gpt-4o"})
	}
	return m
}

func gptDeployment(name, sku, version string, capacity float64) sdk.ARMResource {
	r := res(typeDeployment, name)
	r.SKUName = sku
	r.SKU = map[string]any{"name": sku, "capacity": capacity}
	r.Properties = map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt-4o", "version": version}}
	return r
}

func TestDEP004(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		models []azure.ModelAvailability
		r      func() sdk.ARMResource
		mutate func(*Deps)
		want   outcome
		text   string
	}{
		{"offered with the SKU", []azure.ModelAvailability{gptModel("2024-11-20", "GenerallyAvailable", "", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "2024-11-20", 10) }, nil, oPass, ""},
		{"model not offered", []azure.ModelAvailability{gptModel("2024-11-20", "", "", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "1999-01-01", 10) }, nil, oFail, "is not offered in eastus"},
		{"model absent from the region", nil, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "2024-11-20", 10) }, nil, oFail, "is not offered"},
		{"wrong SKU", []azure.ModelAvailability{gptModel("2024-11-20", "", "", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "ProvisionedManaged", "2024-11-20", 10) }, nil, oFail, "SKU ProvisionedManaged is not offered"},
		{"deprecated lifecycle", []azure.ModelAvailability{gptModel("2024-11-20", "Deprecated", "", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "2024-11-20", 10) }, nil, oFail, "Deprecated"},
		{"deprecating lifecycle is uncertain", []azure.ModelAvailability{gptModel("2024-11-20", "Deprecating", "", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "2024-11-20", 10) }, nil, oUncertain, "Deprecating"},
		{"legacy lifecycle is uncertain", []azure.ModelAvailability{gptModel("2024-11-20", "Legacy", "", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "2024-11-20", 10) }, nil, oUncertain, "Legacy"},
		{"deprecation date passed", []azure.ModelAvailability{gptModel("2024-11-20", "", "2026-01-01T00:00:00Z", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "2024-11-20", 10) }, func(d *Deps) { d.Now = func() time.Time { return now } }, oFail, "deprecation date"},
		{"deprecation date in the future passes", []azure.ModelAvailability{gptModel("2024-11-20", "", "2027-01-01T00:00:00Z", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "2024-11-20", 10) }, func(d *Deps) { d.Now = func() time.Time { return now } }, oPass, ""},
		{"version expression matches any offered version", []azure.ModelAvailability{gptModel("2024-11-20", "", "", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "[parameters('v')]", 10) }, nil, oPass, ""},
		{"model name expression is uncertain", nil, func() sdk.ARMResource {
			r := gptDeployment("a/d", "GlobalStandard", "1", 10)
			r.Properties = map[string]any{"model": map[string]any{"name": "[parameters('m')]"}}
			return r
		}, nil, oUncertain, "not a literal"},
		{"SKU expression is uncertain", []azure.ModelAvailability{gptModel("2024-11-20", "", "", "GlobalStandard")}, func() sdk.ARMResource { return gptDeployment("a/d", "[parameters('s')]", "2024-11-20", 10) }, nil, oUncertain, "SKU"},
		{"no Models capability is skipped", nil, func() sdk.ARMResource { return gptDeployment("a/d", "GlobalStandard", "1", 10) }, func(d *Deps) { d.Models = nil }, oSkipped, "Microsoft.CognitiveServices/locations/models/read"},
		{"unknown location is uncertain", nil, func() sdk.ARMResource {
			r := gptDeployment("a/d", "GlobalStandard", "1", 10)
			r.Region = "[x]"
			return r
		}, func(d *Deps) { d.Target.Location = "" }, oUncertain, "location"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps(&fake{models: c.models})
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-004", c.r())
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
		})
	}
}

func TestModelCatalogueIsCachedPerLocation(t *testing.T) {
	f := &fake{models: []azure.ModelAvailability{gptModel("1", "", "", "GlobalStandard")}, usages: []azure.QuotaUsage{{Name: "OpenAI.GlobalStandard.gpt-4o", Limit: 1000, Unit: "Count"}}}
	d := deps(f)
	in := &sdk.Input{ARM: fakeARM{[]sdk.ARMResource{gptDeployment("a/d1", "GlobalStandard", "1", 10), gptDeployment("a/d2", "GlobalStandard", "1", 10)}}}
	// One Rules(d) set shares one evaluation environment (and its cache).
	for _, r := range Rules(d) {
		if r.ID() == "FND-DEP-004" || r.ID() == "FND-DEP-005" {
			if _, err := r.Evaluate(context.Background(), in); err != nil {
				t.Fatal(err)
			}
		}
	}
	if f.modelCalls != 1 {
		t.Errorf("ListModels called %d times for one region, want 1", f.modelCalls)
	}
}

// ------------------------------------------------------------- F9 DEP-005

func TestDEP005(t *testing.T) {
	usage := func(current, limit float64, unit string) []azure.QuotaUsage {
		return []azure.QuotaUsage{{Name: "OpenAI.GlobalStandard.gpt-4o", Current: current, Limit: limit, Unit: unit}}
	}
	models := []azure.ModelAvailability{gptModel("1", "", "", "GlobalStandard")}
	cases := []struct {
		name   string
		f      *fake
		rs     []sdk.ARMResource
		mutate func(*Deps)
		want   outcome
		text   string
	}{
		{"enough quota", &fake{models: models, usages: usage(100, 1000, "Count")}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oPass, ""},
		{"exactly the free quota passes", &fake{models: models, usages: usage(900, 1000, "Count")}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oPass, ""},
		{"insufficient quota fails", &fake{models: models, usages: usage(950, 1000, "Count")}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, func(d *Deps) { d.Foundry = &fake{} }, oFail, "requested 100 of quota OpenAI.GlobalStandard.gpt-4o in eastus but only 50 of 1000 is free"},
		{"two deployments on one quota line are summed", &fake{models: models, usages: usage(950, 1000, "Count")}, []sdk.ARMResource{gptDeployment("a/d1", "GlobalStandard", "1", 30), gptDeployment("a/d2", "GlobalStandard", "1", 30)}, func(d *Deps) { d.Foundry = &fake{} }, oFail, "requested 60"},
		{"same-name deployment offsets its current capacity", &fake{models: models, usages: usage(950, 1000, "Count"),
			acctDeps: []azure.AccountDeployment{{Name: "d", SKUName: "GlobalStandard", CurrentCapacity: 90}}}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oPass, ""},
		{"offset smaller than the shortfall still fails", &fake{models: models, usages: usage(950, 1000, "Count"),
			acctDeps: []azure.AccountDeployment{{Name: "d", SKUName: "GlobalStandard", CurrentCapacity: 10}}}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oFail, "after adding back 10"},
		{"same name on a different SKU cannot be offset", &fake{models: models, usages: usage(950, 1000, "Count"),
			acctDeps: []azure.AccountDeployment{{Name: "d", SKUName: "Standard", CurrentCapacity: 90}}}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oUncertain, "different quota line"},
		{"existing deployment SKU not reported is uncertain", &fake{models: models, usages: usage(950, 1000, "Count"),
			acctDeps: []azure.AccountDeployment{{Name: "d", CurrentCapacity: 90}}}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oUncertain, "SKU of the existing deployment was not reported"},
		{"a different deployment name is not an offset", &fake{models: models, usages: usage(950, 1000, "Count"),
			acctDeps: []azure.AccountDeployment{{Name: "other", SKUName: "GlobalStandard", CurrentCapacity: 90}}}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oFail, ""},
		{"account not created yet means no existing deployments", &fake{models: models, usages: usage(950, 1000, "Count"),
			acctDepsErr: &azure.APIError{Operation: "x", Status: 404}}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oFail, ""},
		{"unreadable existing deployments is uncertain", &fake{models: models, usages: usage(950, 1000, "Count"),
			acctDepsErr: &azure.UnavailableError{Capability: "deployments", Permission: "Microsoft.CognitiveServices/accounts/deployments/read"}}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oUncertain, "Microsoft.CognitiveServices/accounts/deployments/read"},
		{"no Foundry capability makes a shortfall uncertain", &fake{models: models, usages: usage(950, 1000, "Count")}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, func(d *Deps) { d.Foundry = nil }, oUncertain, "accounts/deployments/read"},
		{"a name that is not <account>/<deployment> makes a shortfall uncertain", &fake{models: models, usages: usage(950, 1000, "Count")}, []sdk.ARMResource{gptDeployment("[x]", "GlobalStandard", "1", 100)}, nil, oUncertain, "not a literal"},
		{"unknown unit is uncertain", &fake{models: models, usages: usage(0, 1000, "")}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oUncertain, "unknown unit mapping"},
		{"quota line missing from the usages is uncertain", &fake{models: models, usages: []azure.QuotaUsage{{Name: "other", Limit: 10, Unit: "Count"}}}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oUncertain, "unknown unit mapping"},
		{"usage name unknown is uncertain", &fake{models: nil, usages: usage(0, 1000, "Count")}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, nil, oUncertain, "quota usage name"},
		{"requested capacity not in the model is uncertain", &fake{models: models, usages: usage(0, 1000, "Count")}, []sdk.ARMResource{func() sdk.ARMResource { r := gptDeployment("a/d", "GlobalStandard", "1", 1); r.SKU = nil; return r }()}, nil, oUncertain, "requested capacity"},
		{"no Models capability is skipped", &fake{}, []sdk.ARMResource{gptDeployment("a/d", "GlobalStandard", "1", 100)}, func(d *Deps) { d.Models = nil }, oSkipped, "usages/read"},
		{"no deployments passes", &fake{}, []sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "s")}, nil, oPass, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps(c.f)
			if c.mutate == nil || c.name != "insufficient quota fails" && c.name != "two deployments on one quota line are summed" {
				d.Foundry = c.f
			}
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-005", c.rs...)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
		})
	}
}

// ------------------------------------------------------------- F9 DEP-006

func TestDEP006(t *testing.T) {
	soft := func(kind azure.NameKind, name, rg, loc string) azure.SoftDeleted {
		return azure.SoftDeleted{Kind: kind, Name: name, Location: loc, ScheduledPurgeDate: "2027-01-01",
			ID: "/subscriptions/s/providers/Microsoft.CognitiveServices/locations/" + loc + "/resourceGroups/" + rg + "/deletedAccounts/" + name}
	}
	cases := []struct {
		name   string
		soft   []azure.SoftDeleted
		r      sdk.ARMResource
		mutate func(*Deps)
		want   outcome
		text   string
		conf   sdk.Confidence
	}{
		{"match in the same group and region", []azure.SoftDeleted{soft(azure.NameFoundry, "acct", "rg", "eastus")}, res(typeAccount, "acct"), nil, oFail, "deletedAccounts/acct", sdk.ConfidenceHigh},
		{"evidence names the deleted entry location and purge date", []azure.SoftDeleted{soft(azure.NameFoundry, "acct", "rg", "eastus")}, res(typeAccount, "acct"), nil, oFail, "in eastus (scheduled purge 2027-01-01)", sdk.ConfidenceHigh},
		{"name match is case-insensitive", []azure.SoftDeleted{soft(azure.NameFoundry, "ACCT", "rg", "eastus")}, res(typeAccount, "acct"), nil, oFail, "", sdk.ConfidenceHigh},
		{"different resource group lowers the confidence", []azure.SoftDeleted{soft(azure.NameFoundry, "acct", "rg-other", "eastus")}, res(typeAccount, "acct"), nil, oFail, "resource group rg-other is not the target group rg", sdk.ConfidenceLow},
		{"different location lowers the confidence", []azure.SoftDeleted{soft(azure.NameFoundry, "acct", "rg", "westus")}, res(typeAccount, "acct"), nil, oFail, "location westus is not the planned location eastus", sdk.ConfidenceLow},
		{"no match passes", []azure.SoftDeleted{soft(azure.NameFoundry, "other", "rg", "eastus")}, res(typeAccount, "acct"), nil, oPass, "", ""},
		{"key vault match is likely", []azure.SoftDeleted{{Kind: azure.NameKeyVault, Name: "kvfoo", ID: "/subscriptions/s/providers/Microsoft.KeyVault/locations/eastus/deletedVaults/kvfoo", Location: "eastus", PurgeProtection: true}}, res("Microsoft.KeyVault/vaults", "kvfoo"), nil, oFail, "purge protection is on", sdk.ConfidenceMedium},
		{"expression name is uncertain", nil, res(typeAccount, "[parameters('n')]"), nil, oUncertain, "not a literal", ""},
		{"no Names capability is skipped", nil, res(typeAccount, "acct"), func(d *Deps) { d.Names = nil }, oSkipped, "soft-deleted resource listing", ""},
		{"restricted identity names the capability", nil, res(typeAccount, "acct"), func(d *Deps) {
			d.Names = &fake{err: &azure.UnavailableError{Capability: "deletedAccounts", Permission: "Microsoft.CognitiveServices/deletedAccounts/read"}}
		}, oSkipped, "Microsoft.CognitiveServices/deletedAccounts/read", ""},
		{"unrelated types pass", nil, res("Microsoft.Storage/storageAccounts", "acct"), nil, oPass, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps(&fake{soft: c.soft})
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-006", c.r)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
			if c.want == oFail && out.Findings[0].Confidence != c.conf {
				t.Errorf("confidence = %q, want %q", out.Findings[0].Confidence, c.conf)
			}
		})
	}
}

// ------------------------------------------------------------- F10 DEP-007 / DEP-011

func TestDEP007UsesCustomSubDomainName(t *testing.T) {
	acct := func(props map[string]any) sdk.ARMResource {
		r := res(typeAccount, "acct")
		r.Properties = props
		return r
	}
	cases := []struct {
		name  string
		r     sdk.ARMResource
		taken map[string]bool
		want  outcome
		text  string
	}{
		{"subdomain available", acct(map[string]any{"customSubDomainName": "mysub-1"}), nil, oPass, ""},
		{"subdomain taken is checked, not the resource name", acct(map[string]any{"customSubDomainName": "mysub-1"}), map[string]bool{"mysub-1": true}, oFail, "customSubDomainName \"mysub-1\" is not available"},
		{"the ARM name being free does not matter", acct(map[string]any{"customSubDomainName": "mysub-1"}), map[string]bool{"acct": true}, oPass, ""},
		{"invalid subdomain", acct(map[string]any{"customSubDomainName": "-bad-"}), nil, oFail, "customSubDomainName"},
		{"expression subdomain is skipped", acct(map[string]any{"customSubDomainName": "[parameters('s')]"}), nil, oSkipped, "customSubDomainName"},
		{"no subdomain is skipped", acct(nil), nil, oSkipped, "no customSubDomainName"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := run(t, deps(&fake{nameTaken: c.taken}), "FND-DEP-007", c.r)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
		})
	}
}

func TestDEP011PrefixBandAndVNet(t *testing.T) {
	vnet := rgID + "/providers/Microsoft.Network/virtualNetworks/v"
	sid := vnet + "/subnets/agents"
	base := azure.Subnet{ID: sid, Name: "agents", VNetID: vnet, AddressPrefixes: []string{"10.0.1.0/24"}, DelegationServices: []string{"Microsoft.App/environments"}}
	cases := []struct {
		name   string
		prefix string
		f      func(*fake)
		mutate func(*Deps)
		want   outcome
		text   string
	}{
		{"/24 passes", "10.0.1.0/24", nil, nil, oPass, ""},
		{"/23 passes", "10.0.0.0/23", nil, nil, oPass, ""},
		{"/25 is an advisory", "10.0.1.0/25", nil, nil, oUncertain, "recommended /24"},
		{"/26 is an advisory", "10.0.1.0/26", nil, nil, oUncertain, "recommended /24"},
		{"/27 is an advisory", "10.0.1.0/27", nil, nil, oUncertain, "recommended /24"},
		{"/28 is a violation", "10.0.1.0/28", nil, nil, oFail, "smaller than /27"},
		{"vnet in another region fails", "10.0.1.0/24", func(f *fake) { f.vnet.Location = "westus" }, nil, oFail, "virtual network is in westus"},
		{"vnet location compares names, not spelling", "10.0.1.0/24", func(f *fake) { f.vnet.Location = "East US" }, nil, oPass, ""},
		{"vnet not found is uncertain", "10.0.1.0/24", func(f *fake) { f.vnet = azure.VNet{} }, nil, oUncertain, "was not found"},
		{"no vnet capability is skipped naming it", "10.0.1.0/24", nil, func(d *Deps) { d.Extras = nil }, oSkipped, "Microsoft.Network/virtualNetworks/read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sn := base
			sn.AddressPrefixes = []string{c.prefix}
			f := &fake{subnet: sn, vnet: azure.VNet{ID: vnet, Location: "eastus"}}
			if c.f != nil {
				c.f(f)
			}
			d := deps(f)
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-011", agentAccount(sid))
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
			if c.mutate == nil && f.vnetCalls != 1 {
				t.Errorf("GetVNet calls = %d, want exactly 1 direct read", f.vnetCalls)
			}
		})
	}
}

// ------------------------------------------------------------- F11 coverage

// TestScopePolicyCoverage documents, in an executable form, which references
// the cross-resource-group scope policy covers: the resource IDs a what-if
// predicts a change for (DEP-008) and the agent network-injection subnet
// (DEP-011). A literal resource ID in other template properties (connection
// targets, private endpoint subnets, role assignment scopes, Key Vault
// references) is deliberately NOT evaluated: their property names are not
// backed by a verified catalogue entry, so no rule reads them.
func TestScopePolicyCoverage(t *testing.T) {
	other := "/subscriptions/s/resourceGroups/other/providers/Microsoft.Storage/storageAccounts/b"
	r := res("Microsoft.CognitiveServices/accounts/connections", "acct/conn")
	r.Properties = map[string]any{"metadata": map[string]any{"ResourceId": other}}
	for _, id := range []string{"FND-DEP-003", "FND-DEP-008", "FND-DEP-010", "FND-DEP-011"} {
		out := run(t, deps(&fake{whatif: azure.WhatIfResult{}}), id, r)
		for _, fd := range out.Findings {
			if strings.Contains(fd.Evidence, other) {
				t.Errorf("%s evaluates connection metadata, which is outside the verified scope-policy coverage", id)
			}
		}
	}
}

func TestErrorWrappingKeepsCapability(t *testing.T) {
	un := &azure.UnavailableError{Capability: "x", Permission: "p/read"}
	if got := azureReason(fmt.Errorf("wrap: %w", un)); !strings.Contains(got, "p/read") {
		t.Errorf("reason %q lost the permission", got)
	}
	if got := azureReason(errors.New("boom")); !strings.Contains(got, "Azure call failed") {
		t.Errorf("reason %q", got)
	}
}

func TestDEP011LinkCapabilityTextNamesExactActions(t *testing.T) {
	vnet := rgID + "/providers/Microsoft.Network/virtualNetworks/v"
	sid := vnet + "/subnets/agents"
	sn := azure.Subnet{ID: sid, Name: "agents", VNetID: vnet, AddressPrefixes: []string{"10.0.1.0/24"}, DelegationServices: []string{"Microsoft.App/environments"}}
	d := deps(&fake{subnet: sn, vnet: azure.VNet{ID: vnet, Location: "eastus"}})
	d.SubnetLinks = nil
	out := run(t, d, "FND-DEP-011", agentAccount(sid))
	for _, want := range []string{
		"Microsoft.Network/virtualNetworks/subnets/serviceAssociationLinks/read",
		"Microsoft.Network/virtualNetworks/subnets/resourceNavigationLinks/read",
	} {
		if classify(out) != oSkipped || !strings.Contains(textOf(out), want) {
			t.Errorf("skip lacks %q: %+v", want, out)
		}
	}
}
