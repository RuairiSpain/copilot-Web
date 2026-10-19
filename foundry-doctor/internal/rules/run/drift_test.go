package run

import (
	"errors"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	rt "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func armAccount(pna any) sdk.ARMResource {
	props := map[string]any{}
	if pna != nil {
		props["publicNetworkAccess"] = pna
	}
	return sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts", Name: "acct", Properties: props, Location: sdk.Location{File: "infra/main.bicep", Line: 12}}
}

func armProject(identityType any) sdk.ARMResource {
	r := sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/projects", Name: "acct/proj"}
	if identityType != nil {
		r.Identity = map[string]any{"type": identityType}
	}
	return r
}

func armDeployment(name string, sku any) sdk.ARMResource {
	r := sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: name}
	if sku != nil {
		r.SKU = map[string]any{"name": sku, "capacity": float64(10)}
	}
	return r
}

func driftDeps(net azure.AccountNetwork, deployments ...azure.AccountDeployment) Deps {
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{net: net, project: project(), deployments: deployments}
	return d
}

func driftInput(rs ...sdk.ARMResource) *sdk.Input {
	return &sdk.Input{AzureYAML: fakeAzureYAML{}, ARM: fakeARM{resources: rs}}
}

func TestRun008DriftOutcomes(t *testing.T) {
	enabled := azure.AccountNetwork{PublicNetworkAccess: "Enabled"}
	disabled := azure.AccountNetwork{PublicNetworkAccess: "Disabled"}
	ok := azure.AccountDeployment{Name: "gpt", SKUName: "GlobalStandard", ProvisioningState: "Succeeded"}
	cases := []struct {
		name        string
		net         azure.AccountNetwork
		deployments []azure.AccountDeployment
		arm         []sdk.ARMResource
		wantKeys    []string // finding keys
		wantNote    string   // uncertain note when no finding; "" and no keys: clean pass
	}{
		// positives
		{"publicNetworkAccess drift", enabled, nil, []sdk.ARMResource{armAccount("Disabled")}, []string{"drift:publicNetworkAccess"}, ""},
		{"publicNetworkAccess drift ignores case when equal", disabled, nil, []sdk.ARMResource{armAccount("disabled")}, nil, ""},
		{"project identity type drift", disabled, nil, []sdk.ARMResource{armProject("UserAssigned")}, []string{"drift:identity.type"}, ""},
		{"deployment sku drift", disabled, []azure.AccountDeployment{ok}, []sdk.ARMResource{armDeployment("acct/gpt", "Standard")}, []string{"drift:sku.name"}, ""},
		{"two drifted properties are two findings", enabled, []azure.AccountDeployment{ok}, []sdk.ARMResource{armAccount("Disabled"), armDeployment("acct/gpt", "ProvisionedManaged")}, []string{"drift:publicNetworkAccess", "drift:sku.name"}, ""},
		// negatives
		{"all inspected properties equal", disabled, []azure.AccountDeployment{ok}, []sdk.ARMResource{armAccount("Disabled"), armProject("SystemAssigned"), armDeployment("acct/gpt", "GlobalStandard")}, nil, ""},
		{"a property the template does not set is not compared", enabled, nil, []sdk.ARMResource{armAccount(nil)}, nil, ""},
		// uncertain
		{"expression-valued template property", enabled, nil, []sdk.ARMResource{armAccount("[parameters('pna')]")}, nil, "unresolved expression"},
		{"deployed value not reported", azure.AccountNetwork{}, nil, []sdk.ARMResource{armAccount("Disabled")}, nil, "not reported by the deployed resource"},
		{"non-scalar template property", enabled, nil, []sdk.ARMResource{armAccount(map[string]any{"x": 1})}, nil, "not a scalar"},
		{"conditional resource", enabled, nil, []sdk.ARMResource{func() sdk.ARMResource {
			r := armAccount("Disabled")
			r.Conditional, r.Condition = true, "[parameters('deploy')]"
			return r
		}()}, nil, "conditional or looped"},
		{"looped resource", enabled, nil, []sdk.ARMResource{func() sdk.ARMResource { r := armAccount("Disabled"); r.Looped = true; return r }()}, nil, "conditional or looped"},
		{"literal false condition is one-sided", enabled, nil, []sdk.ARMResource{func() sdk.ARMResource {
			r := armAccount("Disabled")
			r.Conditional, r.Condition = true, "false"
			return r
		}()}, nil, "never deploys it"},
		{"template account name is an expression", enabled, nil, []sdk.ARMResource{func() sdk.ARMResource { r := armAccount("Disabled"); r.Name = "[parameters('name')]"; return r }()}, nil, "not a literal"},
		// one-sided resources are notes, never findings
		{"template account other than the target", enabled, nil, []sdk.ARMResource{func() sdk.ARMResource { r := armAccount("Disabled"); r.Name = "other"; return r }()}, nil, "is not the target account"},
		{"template deployment missing in Azure", enabled, nil, []sdk.ARMResource{armDeployment("acct/gone", "Standard")}, nil, "is not deployed on the target account"},
		{"deployed deployment missing in the template", disabled, []azure.AccountDeployment{ok, {Name: "portal-made", SKUName: "Standard"}}, []sdk.ARMResource{armDeployment("acct/gpt", "GlobalStandard")}, nil, "portal-made is not declared"},
		{"template deployment name is an expression", enabled, nil, []sdk.ARMResource{armDeployment("[format('{0}/{1}', 'acct', parameters('d'))]", "Standard")}, nil, ""},
		{"template project is another project", enabled, nil, []sdk.ARMResource{func() sdk.ARMResource { r := armProject("SystemAssigned"); r.Name = "acct/other"; return r }()}, nil, "is not the target project"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := evalRule(t, "FND-RUN-008", driftDeps(c.net, c.deployments...), driftInput(c.arm...))
			if err != nil {
				t.Fatal(err)
			}
			var keys []string
			for _, f := range res.Findings {
				keys = append(keys, f.Key)
			}
			if strings.Join(keys, ",") != strings.Join(c.wantKeys, ",") {
				t.Fatalf("keys = %v, want %v (res %+v)", keys, c.wantKeys, res)
			}
			if len(c.wantKeys) > 0 {
				return
			}
			switch {
			case c.name == "template deployment name is an expression":
				if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, "not a literal") {
					t.Fatalf("skip = %+v", res.Skipped)
				}
			case c.wantNote == "":
				if res.Skipped != nil {
					t.Fatalf("want pass, got %+v", res.Skipped)
				}
			default:
				if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, c.wantNote) {
					t.Fatalf("skip = %+v, want uncertain containing %q", res.Skipped, c.wantNote)
				}
			}
		})
	}
}

func TestRun008IdentitySetOrderAndNone(t *testing.T) {
	if identitySet("UserAssigned, SystemAssigned") != identitySet("systemassigned,userassigned") {
		t.Error("identity sets must compare as unordered, case-insensitive sets")
	}
	d := driftDeps(azure.AccountNetwork{})
	m := d.FoundryMgmt.(fakeMgmt)
	p := project()
	p.IdentityType = "SystemAssigned, UserAssigned"
	m.project = p
	d.FoundryMgmt = m
	res, err := evalRule(t, "FND-RUN-008", d, driftInput(armProject("UserAssigned,SystemAssigned")))
	if err != nil || len(res.Findings) != 0 || res.Skipped != nil {
		t.Fatalf("reordered identity set must not drift: res=%+v err=%v", res, err)
	}
}

func TestRun008FindingEvidenceAndSecrecy(t *testing.T) {
	res, err := evalRule(t, "FND-RUN-008", driftDeps(azure.AccountNetwork{PublicNetworkAccess: "Enabled"}), driftInput(armAccount("Disabled")))
	if err != nil || len(res.Findings) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	f := res.Findings[0]
	for _, want := range []string{"publicNetworkAccess", "Disabled", "Enabled", "compiled template", "deployed"} {
		if !strings.Contains(f.Evidence, want) {
			t.Errorf("evidence %q lacks %q", f.Evidence, want)
		}
	}
	if f.Resource.Type != "Microsoft.CognitiveServices/accounts" || f.Resource.ID != accountID(baseDeps().Target) || f.Location.File != "infra/main.bicep" || f.Location.Line != 12 {
		t.Errorf("finding resource/location = %+v %+v", f.Resource, f.Location)
	}
	// Only allow-listed properties are ever read, so another property never appears.
	r := armAccount("Disabled")
	r.Properties["someOtherProperty"] = "unrelated-sentinel-value"
	res, err = evalRule(t, "FND-RUN-008", driftDeps(azure.AccountNetwork{PublicNetworkAccess: "Enabled"}), driftInput(r))
	if err != nil || strings.Contains(renderAll(t, res.Findings, nil), "unrelated-sentinel-value") {
		t.Fatalf("a non-allow-listed property reached the output: %+v err=%v", res, err)
	}
}

func TestRun008SkipsAndErrors(t *testing.T) {
	good := driftInput(armAccount("Disabled"))
	d := driftDeps(azure.AccountNetwork{PublicNetworkAccess: "Disabled"})

	// no compiled template
	for _, in := range []*sdk.Input{{AzureYAML: fakeAzureYAML{}}} {
		res, err := evalRule(t, "FND-RUN-008", d, in)
		if err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, "uncertain:") || !strings.Contains(res.Skipped.Reason, "compiled ARM model") {
			t.Errorf("no ARM: res=%+v err=%v", res, err)
		}
	}
	// template without any inspected type
	res, err := evalRule(t, "FND-RUN-008", d, driftInput(sdk.ARMResource{Type: "Microsoft.Search/searchServices", Name: "s"}))
	if err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipNotApplicable) {
		t.Errorf("no inspected types: res=%+v err=%v", res, err)
	}
	// no deployed read available
	nd := baseDeps()
	nd.FoundryMgmt = nil
	if res, err := evalRule(t, "FND-RUN-008", nd, good); err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable) || !strings.Contains(res.Skipped.Reason, "capability") {
		t.Errorf("no mgmt: res=%+v err=%v", res, err)
	}
	// missing target
	td := driftDeps(azure.AccountNetwork{})
	td.Target.SubscriptionID = ""
	if res, err := evalRule(t, "FND-RUN-008", td, good); err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "--subscription") {
		t.Errorf("no target: res=%+v err=%v", res, err)
	}
	// the caller cannot read the deployed resource: skipped with the capability
	bad := baseDeps()
	bad.FoundryMgmt = fakeMgmt{netErr: &azure.UnavailableError{Capability: "Microsoft.CognitiveServices/accounts read", Permission: "Microsoft.CognitiveServices/accounts/read"}}
	res, err = evalRule(t, "FND-RUN-008", bad, good)
	if err != nil || len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable) || !strings.Contains(res.Skipped.Reason, "Microsoft.CognitiveServices/accounts/read") {
		t.Errorf("unreadable: res=%+v err=%v", res, err)
	}
	// project and deployment reads fail the same way
	bad.FoundryMgmt = fakeMgmt{projectErr: errors.New("boom")}
	if res, err := evalRule(t, "FND-RUN-008", bad, driftInput(armProject("SystemAssigned"))); err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable) {
		t.Errorf("project read: res=%+v err=%v", res, err)
	}
	bad.FoundryMgmt = fakeMgmt{deploymentsErr: errors.New("boom")}
	if res, err := evalRule(t, "FND-RUN-008", bad, driftInput(armDeployment("acct/gpt", "Standard"))); err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable) {
		t.Errorf("deployments read: res=%+v err=%v", res, err)
	}
	// project in the template but no --project
	np := driftDeps(azure.AccountNetwork{})
	np.Target.Project = ""
	if res, err := evalRule(t, "FND-RUN-008", np, driftInput(armProject("SystemAssigned"))); err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, "--project") {
		t.Errorf("no project: res=%+v err=%v", res, err)
	}
}

func TestRun008OneSidedNotesAreBounded(t *testing.T) {
	var deployed []azure.AccountDeployment
	for i := 0; i < 15; i++ {
		deployed = append(deployed, azure.AccountDeployment{Name: "d" + string(rune('a'+i)), SKUName: "Standard"})
	}
	res, err := evalRule(t, "FND-RUN-008", driftDeps(azure.AccountNetwork{}, deployed...), driftInput(armDeployment("acct/none", "Standard")))
	if err != nil || len(res.Findings) != 0 || res.Skipped == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Skipped.Reason, "further resources exist on one side only") {
		t.Errorf("notes were not bounded: %q", res.Skipped.Reason)
	}
}

func TestRun008RegisteredWithClass(t *testing.T) {
	if classes["FND-RUN-008"] != rt.ClassControlPlane {
		t.Errorf("class = %q", classes["FND-RUN-008"])
	}
	found := false
	for _, id := range RuleIDs() {
		found = found || id == "FND-RUN-008"
	}
	if !found {
		t.Error("FND-RUN-008 is not in RuleIDs")
	}
}
