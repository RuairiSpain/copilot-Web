package preflight

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fakeARM struct{ rs []sdk.ARMResource }

func (f fakeARM) Resources() []sdk.ARMResource { return f.rs }

type policyMap map[string]any

func (p policyMap) Get(key string) (any, bool) {
	v, ok := p[key]
	return v, ok
}

// fake implements every azure capability from canned data. Restricted
// identities are modelled by returning *azure.UnavailableError.
type fake struct {
	locations   []azure.LocationInfo
	ptypes      map[string][]azure.ProviderResourceType // lower namespace -> types
	deploys     azure.DeploymentCount
	links       azure.SubnetLinkSet
	evidenceRan bool
	ctxInfo     azure.Context
	providers   []azure.ProviderState
	resources   []azure.Resource
	locks       []azure.Lock
	subnet      azure.Subnet
	decisions   map[string]azure.Decision
	models      []azure.ModelAvailability
	usages      []azure.QuotaUsage
	restr       []azure.PolicyRestriction
	whatif      azure.WhatIfResult
	nameTaken   map[string]bool
	soft        []azure.SoftDeleted
	err         error // returned by every call when set
	whatIfRan   int
	asked       []string // actions requested from EffectiveActions
}

func (f *fake) Context(context.Context, azure.ContextRequest) (azure.Context, error) {
	return f.ctxInfo, f.err
}
func (f *fake) ProviderStates(context.Context, string, []string) ([]azure.ProviderState, error) {
	return f.providers, f.err
}
func (f *fake) ListResources(context.Context, azure.InventoryQuery) (azure.ResourceList, error) {
	return azure.ResourceList{Resources: f.resources}, f.err
}
func (f *fake) ResourceGroup(context.Context, string, string) (azure.ResourceGroupInfo, error) {
	return azure.ResourceGroupInfo{Exists: true}, f.err
}
func (f *fake) ListLocks(context.Context, string) ([]azure.Lock, error) { return f.locks, f.err }
func (f *fake) GetSubnet(context.Context, string) (azure.Subnet, error) { return f.subnet, f.err }
func (f *fake) EffectiveActions(_ context.Context, r azure.PermissionRequest) ([]azure.ActionDecision, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []azure.ActionDecision
	f.asked = append(f.asked, r.Actions...)
	for _, a := range r.Actions {
		d := f.decisions[a]
		if d == "" {
			d = azure.DecisionAllowed
		}
		out = append(out, azure.ActionDecision{Action: a, Decision: d})
	}
	return out, nil
}
func (f *fake) ListModels(context.Context, azure.ModelQuery) ([]azure.ModelAvailability, error) {
	return f.models, f.err
}
func (f *fake) ListUsages(context.Context, string, string) ([]azure.QuotaUsage, error) {
	return f.usages, f.err
}
func (f *fake) ListAssignments(context.Context, string) ([]azure.PolicyAssignment, error) {
	return nil, f.err
}
func (f *fake) ListExemptions(context.Context, string) ([]azure.PolicyExemption, error) {
	return nil, f.err
}
func (f *fake) CheckRestrictions(context.Context, azure.PolicyRestrictionRequest) ([]azure.PolicyRestriction, error) {
	return f.restr, f.err
}
func (f *fake) Run(_ context.Context, r azure.WhatIfRequest) (azure.WhatIfResult, error) {
	f.whatIfRan++
	if !r.OptIn {
		return azure.WhatIfResult{}, errors.New("what-if without opt-in")
	}
	return f.whatif, f.err
}
func (f *fake) CheckName(_ context.Context, c azure.NameCheck) (azure.NameResult, error) {
	if f.err != nil {
		return azure.NameResult{}, f.err
	}
	if f.nameTaken[c.Name] {
		return azure.NameResult{Reason: "AlreadyExists"}, nil
	}
	return azure.NameResult{Available: true}, nil
}
func (f *fake) ListSoftDeleted(context.Context, azure.NameKind, string) ([]azure.SoftDeleted, error) {
	return f.soft, f.err
}

func (f *fake) ListLocations(context.Context, string) ([]azure.LocationInfo, error) {
	return f.locations, f.err
}
func (f *fake) ProviderResourceTypes(_ context.Context, _, ns string) ([]azure.ProviderResourceType, error) {
	return f.ptypes[strings.ToLower(ns)], f.err
}
func (f *fake) CountDeployments(context.Context, string, string) (azure.DeploymentCount, error) {
	return f.deploys, f.err
}
func (f *fake) SubnetLinks(context.Context, string) (azure.SubnetLinkSet, error) {
	return f.links, f.err
}
func (f *fake) ReportedPermissions(context.Context, string) ([]azure.PermissionSet, error) {
	return nil, f.err
}
func (f *fake) CheckReportedActions(ctx context.Context, r azure.PermissionRequest) ([]azure.ActionDecision, error) {
	f.evidenceRan = true
	return f.EffectiveActions(ctx, r)
}

func deps(f *fake) Deps {
	return Deps{
		Context: f, Inventory: f, Permissions: f, Models: f, Policy: f, WhatIf: f, Names: f,
		Regions: f, Deployments: f, SubnetLinks: f, Evidence: f,
		Target:      Target{TenantID: "t", SubscriptionID: "s", ResourceGroup: "rg", Location: "eastus"},
		WhatIfOptIn: true, Template: []byte(`{"resources":[]}`),
	}
}

func run(t *testing.T, d Deps, id string, rs ...sdk.ARMResource) sdk.Result {
	t.Helper()
	in := &sdk.Input{ARM: fakeARM{rs}}
	return runInput(t, d, id, in)
}

func runInput(t *testing.T, d Deps, id string, in *sdk.Input) sdk.Result {
	t.Helper()
	for _, r := range Rules(d) {
		if r.ID() == id {
			res, err := r.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if res.Skipped != nil && len(res.Findings) > 0 {
				t.Fatalf("%s: findings with skip", id)
			}
			return res
		}
	}
	t.Fatalf("no rule %s", id)
	return sdk.Result{}
}

type outcome string

const (
	oPass      outcome = "pass"
	oFail      outcome = "fail"
	oSkipped   outcome = "skipped"
	oUncertain outcome = "uncertain"
)

func classify(r sdk.Result) outcome {
	switch {
	case len(r.Findings) > 0:
		return oFail
	case r.Skipped != nil && strings.HasPrefix(r.Skipped.Reason, UncertainPrefix):
		return oUncertain
	case r.Skipped != nil:
		return oSkipped
	}
	return oPass
}

func res(typ, name string) sdk.ARMResource {
	return sdk.ARMResource{Type: typ, Name: name, APIVersion: "2024-01-01", Region: "eastus"}
}

func TestRuleIDsAndRegistration(t *testing.T) {
	rs := Rules(Deps{})
	if len(rs) != 12 {
		t.Fatalf("got %d rules", len(rs))
	}
	for i, r := range rs {
		if r.ID() != RuleIDs()[i] {
			t.Fatalf("rule %d id %s", i, r.ID())
		}
	}
}

func TestNilInputsAndMissingTemplate(t *testing.T) {
	for _, r := range Rules(deps(&fake{})) {
		out, err := r.Evaluate(context.Background(), nil)
		if err != nil || classify(out) != oSkipped || out.Skipped.RuleID != r.ID() {
			t.Fatalf("%s nil input: %+v %v", r.ID(), out, err)
		}
	}
}

func TestNoAzureClientSkipsEverythingWithCapability(t *testing.T) {
	tmpl := []sdk.ARMResource{
		res("Microsoft.Storage/storageAccounts", "stfoo"),
		res("Microsoft.CognitiveServices/accounts", "acct"),
	}
	in := &sdk.Input{ARM: fakeARM{tmpl}}
	// Rules that read only the template pass without a client; every other
	// rule must skip and name the capability it could not use.
	templateOnly := map[string]bool{"FND-DEP-004": true, "FND-DEP-005": true, "FND-DEP-011": true}
	for _, r := range Rules(Deps{Target: Target{SubscriptionID: "s", ResourceGroup: "rg", Location: "eastus"}}) {
		out, err := r.Evaluate(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		got := classify(out)
		if templateOnly[r.ID()] {
			if got != oPass {
				t.Errorf("%s with no client = %s, want pass", r.ID(), got)
			}
			continue
		}
		if got != oSkipped {
			t.Errorf("%s with no client = %s, want skipped", r.ID(), got)
			continue
		}
		if !strings.HasPrefix(out.Skipped.Reason, sdk.SkipInputUnavailable) || !strings.Contains(out.Skipped.Reason, "capability") && !strings.Contains(out.Skipped.Reason, "what-if") && !strings.Contains(out.Skipped.Reason, "not available") {
			t.Errorf("%s skip reason does not name a capability: %q", r.ID(), out.Skipped.Reason)
		}
	}
}

func TestRestrictedIdentityNamesCapability(t *testing.T) {
	un := &azure.UnavailableError{Capability: "what-if", Permission: "Microsoft.Resources/deployments/whatIf/action", Reason: "forbidden"}
	f := &fake{err: un}
	out := run(t, deps(f), "FND-DEP-008", res("Microsoft.Storage/storageAccounts", "stfoo"))
	if classify(out) != oSkipped || !strings.Contains(out.Skipped.Reason, "Microsoft.Resources/deployments/whatIf/action") {
		t.Fatalf("got %+v", out)
	}
	for _, id := range RuleIDs() {
		o := run(t, deps(f), id, res("Microsoft.Storage/storageAccounts", "stfoo"))
		if classify(o) == oFail {
			t.Errorf("%s: restricted identity produced a finding", id)
		}
	}
}

func TestWhatIfRequiresOptIn(t *testing.T) {
	f := &fake{}
	d := deps(f)
	d.WhatIfOptIn = false
	out := run(t, d, "FND-DEP-008", res("Microsoft.Storage/storageAccounts", "stfoo"))
	if classify(out) != oSkipped {
		t.Fatalf("got %s", classify(out))
	}
	if f.whatIfRan != 0 {
		t.Fatal("what-if ran without opt-in")
	}
}

func TestDEP008(t *testing.T) {
	id := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/old"
	cases := []struct {
		name    string
		changes []azure.WhatIfChange
		allowed []string
		want    outcome
	}{
		{"delete", []azure.WhatIfChange{{ResourceID: id, ChangeType: azure.ChangeDelete}}, nil, oFail},
		{"replace", []azure.WhatIfChange{{ResourceID: id, ChangeType: azure.ChangeReplace}}, nil, oFail},
		{"allowed delete", []azure.WhatIfChange{{ResourceID: id, ChangeType: azure.ChangeDelete}}, []string{strings.ToUpper(id)}, oPass},
		{"modify", []azure.WhatIfChange{{ResourceID: id, ChangeType: azure.ChangeModify}}, nil, oPass},
		{"unknown is uncertain", []azure.WhatIfChange{{ResourceID: id, ChangeType: azure.ChangeUnknown}}, nil, oUncertain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{whatif: azure.WhatIfResult{Changes: c.changes}}
			d := deps(f)
			d.AllowedDeletes = c.allowed
			d.Template = []byte(`{"resources":[]}`)
			out := run(t, d, "FND-DEP-008", res("Microsoft.Storage/storageAccounts", "stfoo"))
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if c.want == oFail && !strings.Contains(out.Findings[0].Evidence, id) {
				t.Fatalf("evidence lacks resource id: %q", out.Findings[0].Evidence)
			}
		})
	}
}

func TestDEP007(t *testing.T) {
	cases := []struct {
		name  string
		r     sdk.ARMResource
		taken map[string]bool
		want  outcome
	}{
		{"available", res("Microsoft.Storage/storageAccounts", "stfoo123"), nil, oPass},
		{"taken", res("Microsoft.Storage/storageAccounts", "stfoo123"), map[string]bool{"stfoo123": true}, oFail},
		{"invalid format", res("Microsoft.Storage/storageAccounts", "Bad_Name"), nil, oFail},
		{"expression", res("Microsoft.Storage/storageAccounts", "[parameters('n')]"), nil, oSkipped},
		{"cosmos has no check", res("Microsoft.DocumentDB/databaseAccounts", "cosmosfoo"), nil, oSkipped},
		{"registry available", res("Microsoft.ContainerRegistry/registries", "acrfoo"), nil, oPass},
		{"registry taken", res("Microsoft.ContainerRegistry/registries", "acrfoo"), map[string]bool{"acrfoo": true}, oFail},
		{"registry invalid format", res("Microsoft.ContainerRegistry/registries", "acr-name"), nil, oFail},
		{"unrelated type passes", res("Microsoft.Network/virtualNetworks", "vnet"), nil, oPass},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := run(t, deps(&fake{nameTaken: c.taken}), "FND-DEP-007", c.r)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
		})
	}
}

func TestDEP007RedeployIsNotConflict(t *testing.T) {
	f := &fake{nameTaken: map[string]bool{"stfoo123": true}, resources: []azure.Resource{{
		ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/stfoo123", Name: "stfoo123", Type: "Microsoft.Storage/storageAccounts",
	}}}
	out := run(t, deps(f), "FND-DEP-007", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	if got := classify(out); got != oPass {
		t.Fatalf("got %s: %+v", got, out)
	}
}

func TestDEP009(t *testing.T) {
	cases := []struct {
		name string
		rst  []azure.PolicyRestriction
		want outcome
	}{
		{"deny", []azure.PolicyRestriction{{Kind: azure.RestrictionObservedDenial, Effect: "Deny", AssignmentID: "a"}}, oFail},
		{"audit is informational", []azure.PolicyRestriction{{Effect: "Audit", AssignmentID: "a"}}, oUncertain},
		{"clean is never a pass", nil, oUncertain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := run(t, deps(&fake{restr: c.rst}), "FND-DEP-009", res("Microsoft.Storage/storageAccounts", "stfoo123"))
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
		})
	}
}

func TestDEP010(t *testing.T) {
	ro := azure.Lock{ID: "/l/ro", Name: "ro", Scope: "/subscriptions/s/resourceGroups/rg", Level: "ReadOnly"}
	out := run(t, deps(&fake{locks: []azure.Lock{ro}}), "FND-DEP-010", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	if classify(out) != oFail {
		t.Fatalf("readonly lock: %+v", out)
	}
	out = run(t, deps(&fake{}), "FND-DEP-010", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	if classify(out) != oPass {
		t.Fatalf("no locks: %+v", out)
	}
	// CanNotDelete + predicted delete.
	rid := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/x"
	cnd := azure.Lock{ID: "/l/cnd", Name: "cnd", Scope: "/subscriptions/s/resourceGroups/rg", Level: "CanNotDelete"}
	f := &fake{locks: []azure.Lock{cnd}, whatif: azure.WhatIfResult{Changes: []azure.WhatIfChange{{ResourceID: rid, ChangeType: azure.ChangeDelete}}}}
	out = run(t, deps(f), "FND-DEP-010", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	if classify(out) != oFail {
		t.Fatalf("cannotdelete + delete: %+v", out)
	}
}

func TestDEP010PolicyMarginOverride(t *testing.T) {
	lock := azure.Lock{ID: "/l/cnd", Name: "cnd", Scope: "/subscriptions/s/resourceGroups/rg", Level: "CanNotDelete"}
	f := &fake{locks: []azure.Lock{lock}, deploys: azure.DeploymentCount{Count: 759}}
	in := &sdk.Input{
		ARM:    fakeARM{[]sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "stfoo123")}},
		Policy: policyMap{"preflight.deploymentHistoryMargin": 40},
	}
	out := runInput(t, deps(f), "FND-DEP-010", in)
	if got := classify(out); got != oPass {
		t.Fatalf("got %s want pass: %+v", got, out)
	}
}

func agentAccount(subnet string) sdk.ARMResource {
	r := res(typeAccount, "acct")
	r.Properties = map[string]any{"networkInjections": []any{map[string]any{"scenario": "agent", "subnetArmId": subnet}}}
	return r
}

func TestDEP011(t *testing.T) {
	sid := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v/subnets/agents"
	ok := azure.Subnet{ID: sid, Name: "agents", VNetID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v",
		AddressPrefixes: []string{"10.0.1.0/24"}, DelegationServices: []string{"Microsoft.App/environments"}}
	cases := []struct {
		name   string
		mutate func(*azure.Subnet)
		sub    string
		want   outcome
	}{
		{"valid has no links and passes", func(*azure.Subnet) {}, sid, oPass},
		{"no delegation", func(s *azure.Subnet) { s.DelegationServices = nil }, sid, oFail},
		{"prefix too small", func(s *azure.Subnet) { s.AddressPrefixes = []string{"10.0.1.0/28"} }, sid, oFail},
		{"reserved range", func(s *azure.Subnet) { s.AddressPrefixes = []string{"172.30.1.0/24"} }, sid, oFail},
		{"public range", func(s *azure.Subnet) { s.AddressPrefixes = []string{"8.8.8.0/24"} }, sid, oFail},
		{"expression subnet", func(*azure.Subnet) {}, "[parameters('s')]", oSkipped},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sn := ok
			c.mutate(&sn)
			f := &fake{subnet: sn, resources: []azure.Resource{{ID: sn.VNetID, Location: "eastus"}}}
			out := run(t, deps(f), "FND-DEP-011", agentAccount(c.sub))
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
		})
	}
	out := run(t, deps(&fake{}), "FND-DEP-011", res("Microsoft.Storage/storageAccounts", "s"))
	if classify(out) != oPass {
		t.Fatalf("no agent subnet should pass: %+v", out)
	}
}

func TestDEP011ExpressionNamedAccountDoesNotSelfFlag(t *testing.T) {
	sid := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v/subnets/agents"
	sn := azure.Subnet{ID: sid, Name: "agents", VNetID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v",
		AddressPrefixes: []string{"10.0.1.0/24"}, DelegationServices: []string{"Microsoft.App/environments"}}
	f := &fake{
		subnet:    sn,
		resources: []azure.Resource{{ID: sn.VNetID, Location: "eastus"}},
		links: azure.SubnetLinkSet{
			ServiceAssociationLinks: []azure.NetworkLink{{
				Name: "sal", Link: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct-existing/projects/p1", LinkedResourceType: "Microsoft.CognitiveServices/accounts/projects",
			}},
		},
	}
	r := agentAccount(sid)
	r.Name = "[parameters('accountName')]"
	out := run(t, deps(f), "FND-DEP-011", r)
	if got := classify(out); got != oUncertain {
		t.Fatalf("got %s want uncertain: %+v", got, out)
	}
}

func TestDEP012PolicyStalenessOverride(t *testing.T) {
	tmpl := res("Microsoft.Storage/storageAccounts", "s")
	acct := res(typeAccount, "acct")
	in := &sdk.Input{
		ARM:    fakeARM{[]sdk.ARMResource{tmpl, acct}},
		Policy: policyMap{"preflight.regionMatrixStalenessDays": 1},
	}
	f := &fake{
		locations: []azure.LocationInfo{{Name: "eastus", DisplayName: "East US"}},
		ptypes:    map[string][]azure.ProviderResourceType{"microsoft.storage": {{ResourceType: "storageAccounts", Locations: []string{"eastus"}}}},
	}
	d := deps(f)
	d.Now = func() time.Time { return regionMatrixDate.Add(48 * time.Hour) }
	out := runInput(t, d, "FND-DEP-012", in)
	if got := classify(out); got != oSkipped {
		t.Fatalf("got %s want skipped: %+v", got, out)
	}
	if out.Skipped == nil || !strings.Contains(out.Skipped.Reason, "staleness threshold") {
		t.Fatalf("unexpected skip: %+v", out)
	}
}

func TestDEP012(t *testing.T) {
	locs := []azure.LocationInfo{{Name: "eastus", DisplayName: "East US"}, {Name: "westus", DisplayName: "West US"}, {Name: "mars", DisplayName: "Mars Central"}}
	storage := func(l ...string) map[string][]azure.ProviderResourceType {
		return map[string][]azure.ProviderResourceType{"microsoft.storage": {{ResourceType: "storageAccounts", Locations: l}}}
	}
	tmpl := res("Microsoft.Storage/storageAccounts", "s")
	acct := func() sdk.ARMResource { r := res(typeAccount, "acct"); return r }
	cases := []struct {
		name   string
		f      *fake
		mutate func(*Deps)
		rs     []sdk.ARMResource
		want   outcome
		text   string
	}{
		{"offered by name", &fake{locations: locs, ptypes: storage("eastus")}, nil, []sdk.ARMResource{tmpl}, oPass, ""},
		{"offered by display name", &fake{locations: locs, ptypes: storage("East US")}, nil, []sdk.ARMResource{tmpl}, oPass, ""},
		{"not offered", &fake{locations: locs, ptypes: storage("westus")}, nil, []sdk.ARMResource{tmpl}, oFail, "does not offer"},
		{"target not a subscription location", &fake{locations: locs[1:2], ptypes: storage("eastus")}, nil, []sdk.ARMResource{tmpl}, oFail, "subscription's location list"},
		{"global resource has no locations", &fake{locations: locs, ptypes: storage()}, nil, []sdk.ARMResource{tmpl}, oUncertain, ""},
		{"type missing from provider", &fake{locations: locs, ptypes: map[string][]azure.ProviderResourceType{"microsoft.storage": nil}}, nil, []sdk.ARMResource{tmpl}, oUncertain, ""},
		{"nil regions", &fake{}, func(d *Deps) { d.Regions = nil }, []sdk.ARMResource{tmpl}, oSkipped, "Subscriptions_ListLocations"},
		{"location unresolved", &fake{locations: locs}, func(d *Deps) { d.Target.Location = "" }, []sdk.ARMResource{tmpl}, oSkipped, "--location"},
		{"restricted identity", &fake{err: &azure.UnavailableError{Capability: "subscription locations", Permission: "Microsoft.Resources/subscriptions/locations/read"}}, nil, []sdk.ARMResource{tmpl}, oSkipped, "Microsoft.Resources/subscriptions/locations/read"},
		{"documented region, nothing mismatched", &fake{locations: locs, ptypes: map[string][]azure.ProviderResourceType{"microsoft.cognitiveservices": {{ResourceType: "accounts", Locations: []string{"eastus"}}}}}, nil, []sdk.ARMResource{acct()}, oPass, ""},
		{"region missing from documented matrix is uncertain, never a violation", &fake{locations: locs, ptypes: map[string][]azure.ProviderResourceType{"microsoft.cognitiveservices": {{ResourceType: "accounts", Locations: []string{"mars"}}}}},
			func(d *Deps) { d.Target.Location = "mars" }, []sdk.ARMResource{func() sdk.ARMResource { r := acct(); r.Region = "mars"; return r }()}, oUncertain, ""},
		{"matrix older than staleness threshold is skipped", &fake{locations: locs, ptypes: map[string][]azure.ProviderResourceType{"microsoft.cognitiveservices": {{ResourceType: "accounts", Locations: []string{"eastus"}}}}},
			func(d *Deps) { d.Now = func() time.Time { return regionMatrixDate.Add(2 * regionMatrixStaleAfter) } }, []sdk.ARMResource{acct()}, oSkipped, "staleness"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps(c.f)
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-012", c.rs...)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			text := ""
			if out.Skipped != nil {
				text = out.Skipped.Reason
			}
			for _, fd := range out.Findings {
				text += fd.Evidence
			}
			if c.text != "" && !strings.Contains(text, c.text) {
				t.Fatalf("output lacks %q: %q", c.text, text)
			}
		})
	}
}

func TestDEP012MatrixDataIsDated(t *testing.T) {
	if regionMatrixDate.IsZero() || len(documentedRegions) != 30 {
		t.Fatalf("matrix date %v, %d regions", regionMatrixDate, len(documentedRegions))
	}
	if s := documentedRegions[normLoc("Poland Central")]; s.voice {
		t.Fatal("Poland Central has no documented voice support")
	}
}

func TestDEP003RegisterActionAndEvidence(t *testing.T) {
	r := res("Microsoft.CognitiveServices/accounts", "acct")
	// Unregistered namespace: <ns>/register/action is required and checked.
	f := &fake{providers: []azure.ProviderState{{Namespace: "Microsoft.CognitiveServices", State: "NotRegistered"}}}
	run(t, deps(f), "FND-DEP-003", r)
	if !f.evidenceRan {
		t.Fatal("permissions/read evidence was not used")
	}
	if !contains(f.asked, "Microsoft.CognitiveServices/register/action") {
		t.Fatalf("register/action not requested: %v", f.asked)
	}
	// Registered namespace: no register action.
	f = &fake{providers: []azure.ProviderState{{Namespace: "Microsoft.CognitiveServices", State: "Registered"}}}
	run(t, deps(f), "FND-DEP-003", r)
	if contains(f.asked, "Microsoft.CognitiveServices/register/action") {
		t.Fatalf("register/action requested for a registered namespace: %v", f.asked)
	}
	// Missing register/action grant is a finding naming the action.
	f = &fake{providers: []azure.ProviderState{{Namespace: "Microsoft.CognitiveServices", State: "NotRegistered"}},
		decisions: map[string]azure.Decision{"Microsoft.CognitiveServices/register/action": azure.DecisionDenied}}
	out := run(t, deps(f), "FND-DEP-003", r)
	if classify(out) != oFail || !strings.Contains(out.Findings[0].Evidence, "register/action") {
		t.Fatalf("got %+v", out)
	}
	// Whole grant present is still uncertain (evidence, not proof).
	out = run(t, deps(&fake{}), "FND-DEP-003", r)
	if classify(out) != oUncertain {
		t.Fatalf("granted: %s", classify(out))
	}
	// Provider state unreadable: register/action cannot be evaluated => note, not pass.
	f = &fake{}
	d := deps(f)
	d.Context = nil
	if got := classify(run(t, d, "FND-DEP-003", r)); got != oUncertain {
		t.Fatalf("no provider state: %s", got)
	}
	// No permissions evidence and no permissions capability: skipped naming it.
	d = deps(&fake{})
	d.Evidence, d.Permissions = nil, nil
	out = run(t, d, "FND-DEP-003", r)
	if classify(out) != oSkipped || !strings.Contains(out.Skipped.Reason, "Microsoft.Authorization/permissions/read") {
		t.Fatalf("got %+v", out)
	}
	// Fallback to role-assignment evaluation is flagged.
	d = deps(&fake{})
	d.Evidence = nil
	if got := classify(run(t, d, "FND-DEP-003", r)); got != oUncertain {
		t.Fatalf("fallback: %s", got)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestDEP010DeploymentHistory(t *testing.T) {
	rg := "/subscriptions/s/resourceGroups/rg"
	cnd := azure.Lock{ID: "/l/cnd", Name: "cnd", Scope: rg, Level: "CanNotDelete"}
	storage := res("Microsoft.Storage/storageAccounts", "stfoo123")
	cases := []struct {
		name   string
		f      *fake
		mutate func(*Deps)
		want   outcome
		text   string
	}{
		{"near limit with CanNotDelete on the group", &fake{locks: []azure.Lock{cnd}, deploys: azure.DeploymentCount{Count: 790}}, nil, oFail, "790 of 800"},
		{"at limit", &fake{locks: []azure.Lock{cnd}, deploys: azure.DeploymentCount{Count: 800}}, nil, oFail, "800 of 800"},
		{"truncated count is reported as a lower bound", &fake{locks: []azure.Lock{cnd}, deploys: azure.DeploymentCount{Count: 800, Truncated: true}}, nil, oFail, "at least"},
		{"history well under the limit", &fake{locks: []azure.Lock{cnd}, deploys: azure.DeploymentCount{Count: 100}}, nil, oPass, ""},
		{"no lock, large history is not this rule's failure", &fake{deploys: azure.DeploymentCount{Count: 799}}, nil, oPass, ""},
		{"CanNotDelete on another scope", &fake{locks: []azure.Lock{{ID: "/l/x", Name: "x", Scope: rg + "/providers/Microsoft.Storage/storageAccounts/other", Level: "CanNotDelete"}}, deploys: azure.DeploymentCount{Count: 799}}, nil, oPass, ""},
		{"history capability missing", &fake{locks: []azure.Lock{cnd}}, func(d *Deps) { d.Deployments = nil }, oSkipped, "Microsoft.Resources/deployments/read"},
		{"CanNotDelete without what-if is uncertain", &fake{locks: []azure.Lock{cnd}, deploys: azure.DeploymentCount{Count: 1}}, func(d *Deps) { d.WhatIfOptIn = false }, oUncertain, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps(c.f)
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-010", storage)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			text := ""
			if out.Skipped != nil {
				text = out.Skipped.Reason
			}
			for _, fd := range out.Findings {
				text += fd.Evidence
			}
			if c.text != "" && !strings.Contains(text, c.text) {
				t.Fatalf("output lacks %q: %q", c.text, text)
			}
		})
	}
}

func TestDEP011SubnetLinks(t *testing.T) {
	vnet := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v"
	sid := vnet + "/subnets/agents"
	sn := azure.Subnet{ID: sid, Name: "agents", VNetID: vnet, AddressPrefixes: []string{"10.0.1.0/24"}, DelegationServices: []string{"Microsoft.App/environments"}}
	own := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct/capabilityHosts/h"
	other := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/other/capabilityHosts/h"
	cases := []struct {
		name   string
		links  azure.SubnetLinkSet
		mutate func(*Deps)
		want   outcome
		text   string
	}{
		{"no links", azure.SubnetLinkSet{}, nil, oPass, ""},
		{"link owned by this account", azure.SubnetLinkSet{ServiceAssociationLinks: []azure.NetworkLink{{Name: "l", Link: own}}}, nil, oPass, ""},
		{"service association link from another resource", azure.SubnetLinkSet{ServiceAssociationLinks: []azure.NetworkLink{{Name: "sal", LinkedResourceType: "Microsoft.App/environments", Link: other}}}, nil, oFail, "service association link sal"},
		{"resource navigation link from another resource", azure.SubnetLinkSet{ResourceNavigationLinks: []azure.NetworkLink{{Name: "rnl", Link: other}}}, nil, oFail, "resource navigation link rnl"},
		{"links capability missing", azure.SubnetLinkSet{}, func(d *Deps) { d.SubnetLinks = nil }, oSkipped, "serviceAssociationLinks"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{subnet: sn, links: c.links, resources: []azure.Resource{{ID: vnet, Location: "eastus"}}}
			d := deps(f)
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-011", agentAccount(sid))
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			text := ""
			if out.Skipped != nil {
				text = out.Skipped.Reason
			}
			for _, fd := range out.Findings {
				text += fd.Evidence
			}
			if c.text != "" && !strings.Contains(text, c.text) {
				t.Fatalf("output lacks %q: %q", c.text, text)
			}
		})
	}
	// Restricted identity on the links read names the capability.
	f := &fake{subnet: sn, resources: []azure.Resource{{ID: vnet, Location: "eastus"}}}
	d := deps(f)
	d.SubnetLinks = unavailableLinks{}
	out := run(t, d, "FND-DEP-011", agentAccount(sid))
	if classify(out) != oSkipped || !strings.Contains(out.Skipped.Reason, "Microsoft.Network/virtualNetworks/subnets/read") {
		t.Fatalf("got %+v", out)
	}
}

type unavailableLinks struct{}

func (unavailableLinks) SubnetLinks(context.Context, string) (azure.SubnetLinkSet, error) {
	return azure.SubnetLinkSet{}, &azure.UnavailableError{Capability: "subnet links", Permission: "Microsoft.Network/virtualNetworks/subnets/read"}
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fake{err: context.Canceled}
	// Rules that call Azure must surface cancellation, never a skip or pass.
	callsAzure := map[string]bool{
		"FND-DEP-001": true, "FND-DEP-002": true, "FND-DEP-003": true, "FND-DEP-006": true,
		"FND-DEP-007": true, "FND-DEP-008": true, "FND-DEP-009": true, "FND-DEP-010": true, "FND-DEP-012": true,
	}
	in := &sdk.Input{ARM: fakeARM{[]sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "stfoo123"), res(typeAccount, "acct")}}}
	for _, r := range Rules(deps(f)) {
		out, err := r.Evaluate(ctx, in)
		if callsAzure[r.ID()] {
			if !errors.Is(err, context.Canceled) {
				t.Errorf("%s: err = %v (result %s), want context.Canceled", r.ID(), err, classify(out))
			}
			continue
		}
		if err != nil {
			t.Errorf("%s makes no Azure call but returned %v", r.ID(), err)
		}
	}
}

func TestScopePolicyDEP011(t *testing.T) {
	const vnet = "/subscriptions/s/resourceGroups/other/providers/Microsoft.Network/virtualNetworks/v"
	sid := vnet + "/subnets/agents"
	sn := azure.Subnet{ID: sid, Name: "agents", VNetID: vnet, AddressPrefixes: []string{"10.0.1.0/24"}, DelegationServices: []string{"Microsoft.App/environments"}}
	inRG := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v/subnets/agents"
	cases := []struct {
		name     string
		subnet   string
		approved []string
		mutate   func(*Deps)
		want     outcome
		contains string
	}{
		{"in scope", inRG, nil, nil, oPass, ""},
		{"approved resource group", sid, []string{"/subscriptions/s/resourceGroups/other"}, nil, oPass, ""},
		{"approved scope is case-insensitive", sid, []string{"/SUBSCRIPTIONS/S/RESOURCEGROUPS/OTHER/"}, nil, oPass, ""},
		{"prefix is not a path boundary", sid, []string{"/subscriptions/s/resourceGroups/oth"}, nil, oFail, sid},
		{"unapproved", sid, nil, nil, oFail, sid},
		{"unresolved target", sid, nil, func(d *Deps) { d.Target.ResourceGroup = "" }, oSkipped, "cross-resource-group scope policy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{subnet: sn, resources: []azure.Resource{{ID: vnet, Location: "eastus"}}}
			d := deps(f)
			d.ApprovedScopes = c.approved
			if c.mutate != nil {
				c.mutate(&d)
			}
			out := run(t, d, "FND-DEP-011", agentAccount(c.subnet))
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if c.contains != "" {
				text := ""
				if out.Skipped != nil {
					text = out.Skipped.Reason
				}
				for _, fd := range out.Findings {
					text += fd.Evidence + fd.Resource.ID
				}
				if !strings.Contains(text, c.contains) {
					t.Fatalf("output lacks %q: %q", c.contains, text)
				}
			}
		})
	}
}

func TestScopePolicyDEP008(t *testing.T) {
	in := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/a"
	out := "/subscriptions/s/resourceGroups/other/providers/Microsoft.Storage/storageAccounts/b"
	cases := []struct {
		name     string
		id       string
		ct       azure.ChangeType
		approved []string
		noRG     bool
		want     outcome
	}{
		{"in scope", in, azure.ChangeCreate, nil, false, oPass},
		{"approved", out, azure.ChangeCreate, []string{"/subscriptions/s/resourceGroups/other"}, false, oPass},
		{"unapproved create", out, azure.ChangeCreate, nil, false, oFail},
		{"unapproved modify", out, azure.ChangeModify, nil, false, oFail},
		{"unapproved no-change is ignored", out, azure.ChangeNoChange, nil, false, oPass},
		{"unresolved target", out, azure.ChangeCreate, nil, true, oSkipped},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps(&fake{whatif: azure.WhatIfResult{Changes: []azure.WhatIfChange{{ResourceID: c.id, ChangeType: c.ct}}}})
			d.ApprovedScopes = c.approved
			if c.noRG {
				d.Target.ResourceGroup = ""
			}
			got := run(t, d, "FND-DEP-008", res("Microsoft.Storage/storageAccounts", "stfoo"))
			if cl := classify(got); cl != c.want {
				t.Fatalf("got %s want %s: %+v", cl, c.want, got)
			}
			if c.want == oFail && got.Findings[0].Resource.ID != c.id {
				t.Fatalf("finding does not name %s: %+v", c.id, got.Findings[0])
			}
		})
	}
}
