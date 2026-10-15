package net

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Microsoft sample 15 shape: networkInjections is an ARM expression string
// (compiled from a ternary over a parameter). Review B3.
func TestSample15ExpressionNetworkInjectionsIsUncertain(t *testing.T) {
	rs := rulestest.Golden(t, "netinj").Resources()
	acc := find(rs, "Microsoft.CognitiveServices/accounts", "aiservices")
	if acc < 0 {
		t.Fatal("golden account missing")
	}
	if v, _ := rs[acc].Properties["networkInjections"].(string); !strings.HasPrefix(v, "[if(") {
		t.Fatalf("golden must hold an expression-valued networkInjections, got %v", rs[acc].Properties["networkInjections"])
	}
	for _, r := range []sdk.Rule{net002{}, net004{}, net005{}, net006{}, net011{}} {
		t.Run(r.ID(), func(t *testing.T) {
			out := runRule(t, r, rs, nil)
			if len(out.Findings) != 0 {
				t.Fatalf("expression-valued networkInjections must not produce findings: %+v", out.Findings)
			}
			if out.Skipped == nil {
				t.Fatal("want an uncertain skip")
			}
			reason := out.Skipped.Reason
			if strings.HasPrefix(reason, sdk.SkipNotApplicable) {
				t.Fatalf("must not be not-applicable: %q", reason)
			}
			if !strings.Contains(reason, "uncertain") || !strings.Contains(reason, "networkInjections") {
				t.Fatalf("reason must say uncertain and name networkInjections: %q", reason)
			}
		})
	}
}

// NET-011 on sample 15 with a vnet that has an unrelated subnet without an NSG:
// the subnet may be the agent subnet, so it is uncertain and never a warning.
func TestSample15NET011NoPlainWarning(t *testing.T) {
	rs := append([]sdk.ARMResource(nil), rulestest.Golden(t, "netinj").Resources()...)
	out := runRule(t, net011{}, rs, nil)
	if len(out.Findings) != 0 || out.Skipped == nil || !strings.Contains(out.Skipped.Reason, "uncertain") {
		t.Fatalf("want uncertain skip, got %+v", out)
	}
}

// With an array-valued networkInjections the same sample still evaluates, so
// the uncertainty is caused by the expression and not by the rule skipping.
func TestSample15LiteralInjectionStillEvaluated(t *testing.T) {
	rs := append([]sdk.ARMResource(nil), rulestest.Golden(t, "netinj").Resources()...)
	acc := find(rs, "Microsoft.CognitiveServices/accounts", "aiservices")
	props := map[string]any{}
	for k, v := range rs[acc].Properties {
		props[k] = v
	}
	props["networkInjections"] = l{m{"scenario": "agent", "subnetArmId": "[reference(resourceId('Microsoft.Network/virtualNetworks', 'agent-vnet'), '2024-05-01').subnets[0].id]"}}
	rs[acc].Properties = props
	out := runRule(t, net004{}, rs, nil)
	if out.Skipped != nil || len(out.Findings) != 1 || !strings.Contains(out.Findings[0].Evidence, "/28") {
		t.Fatalf("a /28 agent subnet must be reported once the injection is a literal: %+v", out)
	}
}

// M3: findings and unevaluated resources together keep the unevaluated visible.
func TestFindingsKeepUnevaluatedVisible(t *testing.T) {
	rs := []sdk.ARMResource{
		{Type: "Microsoft.Network/virtualNetworks", Name: "vn", Properties: m{"subnets": l{m{"name": "x", "properties": m{"addressPrefix": "10.0.1.0/24"}}}}},
		acct("a", m{"networkInjections": "[parameters('inj')]"}),
	}
	out := runRule(t, net011{}, rs, nil)
	// The only subnet may be the agent subnet, so there is no finding.
	if len(out.Findings) != 0 || out.Skipped == nil {
		t.Fatalf("got %+v", out)
	}
	rs = append(rs, sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks/subnets", Name: "vn2/y", Properties: m{"addressPrefix": "10.1.0.0/24"}})
	rs = append(rs, sdk.ARMResource{Type: "Microsoft.Network/publicIPAddresses", Name: "pip", Properties: m{}})
	out = runRule(t, net011{}, rs, nil)
	if len(out.Findings) == 0 {
		t.Fatalf("the public IP finding must be kept: %+v", out)
	}
	if !strings.Contains(out.Findings[0].Evidence, "not evaluated") || !strings.Contains(out.Findings[0].Evidence, "uncertain") {
		t.Fatalf("first finding must name unevaluated resources: %q", out.Findings[0].Evidence)
	}
}

// M11: subnets declared as child resources are checked.
func TestNET011ChildSubnetResources(t *testing.T) {
	vnet := sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks", Name: "vn", Properties: m{}}
	child := func(props m) sdk.ARMResource {
		return sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks/subnets", Name: "vn/workload", Properties: props}
	}
	tests := []struct {
		name string
		rs   []sdk.ARMResource
		want int
	}{
		{"child without NSG", []sdk.ARMResource{vnet, child(m{"addressPrefix": "10.0.1.0/24"})}, 1},
		{"child with NSG", []sdk.ARMResource{vnet, child(m{"addressPrefix": "10.0.1.0/24", "networkSecurityGroup": m{"id": "[resourceId('Microsoft.Network/networkSecurityGroups', 'nsg')]"}})}, 0},
		{"exempt child", []sdk.ARMResource{vnet, {Type: "Microsoft.Network/virtualNetworks/subnets", Name: "vn/GatewaySubnet", Properties: m{}}}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := runRule(t, net011{}, tc.rs, nil)
			if len(out.Findings) != tc.want {
				t.Fatalf("want %d findings, got %+v", tc.want, out)
			}
			if tc.want == 0 && out.Skipped != nil && strings.HasPrefix(out.Skipped.Reason, "uncertain") {
				t.Fatalf("unexpected uncertain: %+v", out.Skipped)
			}
		})
	}
}

// M6: NET-003 not-applicable without an in-scope endpoint; profile-key-missing
// when the cloud is needed.
func TestNET003CloudReasons(t *testing.T) {
	noPE := []sdk.ARMResource{acct("a", m{})}
	out := runRule(t, net003{}, noPE, nil)
	if out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, sdk.SkipNotApplicable) {
		t.Fatalf("no private endpoint must be not-applicable: %+v", out)
	}
	// Golden: in-scope endpoint, regions are parameters, no policy.
	rs := goldenResources(t)
	out = runRule(t, net003{}, rs, nil)
	if out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, "profile-key-missing:network.cloud") {
		t.Fatalf("want profile-key-missing:network.cloud, got %+v", out)
	}
	if out.Skipped.MissingCapability == "" {
		t.Error("missing capability must be named")
	}
	// With the policy key the rule runs.
	out = runRule(t, net003{}, rs, fakePolicy{"network.cloud": "public"})
	if out.Skipped != nil && strings.HasPrefix(out.Skipped.Reason, "profile-key-missing") {
		t.Fatalf("policy supplied, rule must not ask for it again: %+v", out)
	}
}
