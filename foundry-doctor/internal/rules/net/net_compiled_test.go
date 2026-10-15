package net

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// The tests in this file run on the golden ARM JSON compiled from
// internal/rules/rulestest/main.bicep (Bicep CLI 0.48.1): expression names
// built with format() and uniqueString(), resourceGroup().location,
// reference(...).subnets[N].id subnet references and conditional private
// endpoints.

type memModel []sdk.ARMResource

func (m memModel) Resources() []sdk.ARMResource { return m }

func goldenResources(t *testing.T) []sdk.ARMResource {
	t.Helper()
	return rulestest.Model(t).Resources()
}

func runRule(t *testing.T, r sdk.Rule, rs []sdk.ARMResource, pol sdk.Policy) sdk.Result {
	t.Helper()
	out, err := r.Evaluate(context.Background(), &sdk.Input{ARM: memModel(rs), Policy: pol})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func find(rs []sdk.ARMResource, typ, nameContains string) int {
	for i, r := range rs {
		if strings.EqualFold(r.Type, typ) && strings.Contains(r.Name, nameContains) {
			return i
		}
	}
	return -1
}

func TestCompiledNET001ConditionalEndpointIsNotProof(t *testing.T) {
	rs := goldenResources(t)
	pei := find(rs, "Microsoft.Network/privateEndpoints", "pe-ai")
	if pei < 0 || !rs[pei].Conditional {
		t.Fatalf("golden private endpoint must be conditional: %+v", rs[pei])
	}
	// Conditional endpoint: neither a pass nor a failure.
	out := runRule(t, net001{}, rs, nil)
	if len(out.Findings) != 0 || out.Skipped == nil {
		t.Fatalf("conditional endpoint must skip, got %+v", out)
	}
	// Endpoint removed: the private account has no endpoint.
	without := append(append([]sdk.ARMResource(nil), rs[:pei]...), rs[pei+1:]...)
	out = runRule(t, net001{}, without, nil)
	if len(out.Findings) != 1 || !strings.Contains(out.Findings[0].Evidence, "no deployed private endpoint") {
		t.Fatalf("want one finding, got %+v", out)
	}
	// Unconditional endpoint whose target is the same format() expression: pass.
	uncond := append([]sdk.ARMResource(nil), rs...)
	uncond[pei].Conditional, uncond[pei].Condition = false, ""
	out = runRule(t, net001{}, uncond, nil)
	if out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("want pass, got %+v", out)
	}
	// Literal false condition: not deployed, so it is not proof either.
	off := append([]sdk.ARMResource(nil), rs...)
	off[pei].Condition = "false"
	out = runRule(t, net001{}, off, nil)
	if len(out.Findings) != 1 {
		t.Fatalf("not-deployed endpoint must not satisfy the check, got %+v", out)
	}
}

func TestCompiledNET001PublicAccessPolicy(t *testing.T) {
	rs := []sdk.ARMResource{acct("a", m{})}
	for _, tc := range []struct{ policy, want string }{
		{"entra-only", "declared deliberate"},
		{"allowed", "declared deliberate"},
		{"forbidden", ""},
	} {
		out := runRule(t, net001{}, rs, fakePolicy{"network.publicAccess": tc.policy})
		if len(out.Findings) != 1 {
			t.Fatalf("%s: %+v", tc.policy, out)
		}
		if got := strings.Contains(out.Findings[0].Evidence, "declared deliberate"); got != (tc.want != "") {
			t.Errorf("%s: evidence %q", tc.policy, out.Findings[0].Evidence)
		}
	}
}

func TestCompiledNET003ScopeAndNotDeployed(t *testing.T) {
	rs := goldenResources(t)
	pol := fakePolicy{"network.cloud": "public"}
	out := runRule(t, net003{}, rs, pol)
	var targets []string
	for _, f := range out.Findings {
		targets = append(targets, f.Resource.Name)
	}
	joined := strings.Join(targets, ",")
	if !strings.Contains(joined, "pe-search") {
		t.Fatalf("the Foundry-linked search endpoint must be evaluated: %v", targets)
	}
	if strings.Contains(joined, "pe-other") {
		t.Fatalf("an endpoint of a storage account Foundry does not use must not be reported: %v", targets)
	}
	if strings.Contains(joined, "pe-ai") {
		t.Fatalf("a conditional endpoint must not be reported: %v", targets)
	}
	// A literal false condition removes the endpoint entirely.
	pei := find(rs, "Microsoft.Network/privateEndpoints", "pe-search")
	off := append([]sdk.ARMResource(nil), rs...)
	off[pei].Conditional, off[pei].Condition = true, "false"
	out = runRule(t, net003{}, off, pol)
	for _, f := range out.Findings {
		if strings.Contains(f.Resource.Name, "pe-search") {
			t.Fatalf("not-deployed endpoint reported: %+v", f)
		}
	}
}

func TestCompiledNET005RegionExpressionsSkip(t *testing.T) {
	out := runRule(t, net005{}, goldenResources(t), nil)
	if len(out.Findings) != 0 || out.Skipped == nil {
		t.Fatalf("resourceGroup().location and parameter regions are not comparable: %+v", out)
	}
	if !strings.Contains(out.Skipped.Reason, "not comparable") {
		t.Errorf("reason %q", out.Skipped.Reason)
	}
}

func TestNET005LiteralRegionsStillCompared(t *testing.T) {
	rs := []sdk.ARMResource{
		{Type: "Microsoft.Network/virtualNetworks", Name: "vn", Region: "northeurope", Properties: m{}},
		{Type: "Microsoft.Network/privateEndpoints", Name: "pe", Region: "swedencentral", Properties: m{
			"subnet": m{"id": "[reference(resourceId('Microsoft.Network/virtualNetworks', 'vn'), '2024-05-01').subnets[0].id]"}}},
	}
	rs[0].Properties["subnets"] = l{m{"name": "pe", "properties": m{"addressPrefix": "10.0.1.0/24"}}}
	out := runRule(t, net005{}, rs, nil)
	if len(out.Findings) != 1 {
		t.Fatalf("want a region mismatch finding, got %+v", out)
	}
}

func TestCompiledAgentSubnetInjection(t *testing.T) {
	rs := append([]sdk.ARMResource(nil), goldenResources(t)...)
	ai := find(rs, "Microsoft.CognitiveServices/accounts", "'ai-{0}'")
	props := map[string]any{}
	for k, v := range rs[ai].Properties {
		props[k] = v
	}
	props["networkInjections"] = l{m{"scenario": "agent", "subnetArmId": "[reference(resourceId('Microsoft.Network/virtualNetworks', format('vnet-{0}', variables('token'))), '2024-05-01').subnets[1].id]"}}
	rs[ai].Properties = props

	if out := runRule(t, net004{}, rs, nil); out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("NET-004 must resolve the injected subnet and pass: %+v", out)
	}
	if out := runRule(t, net006{}, rs, nil); out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("NET-006 must resolve the injected subnet and pass: %+v", out)
	}
	// NET-011 exempts the resolved agent subnet and still reports the other.
	out := runRule(t, net011{}, rs, nil)
	if len(out.Findings) != 1 || !strings.HasSuffix(out.Findings[0].Resource.Name, "/pe") {
		t.Fatalf("want only the pe subnet reported, got %+v", out.Findings)
	}
	// Without the injection both subnets are reported.
	if out := runRule(t, net011{}, goldenResources(t), nil); len(out.Findings) != 2 {
		t.Fatalf("want 2 findings, got %+v", out.Findings)
	}
}

func TestNET011UnresolvedInjectionIsUncertain(t *testing.T) {
	rs := []sdk.ARMResource{
		acct("a", m{"networkInjections": l{m{"scenario": "agent", "subnetArmId": "[parameters('agentSubnetId')]"}}}),
		{Type: "Microsoft.Network/virtualNetworks", Name: "vn", Properties: m{"subnets": l{m{"name": "x", "properties": m{"addressPrefix": "10.0.1.0/24"}}}}},
	}
	out := runRule(t, net011{}, rs, nil)
	if len(out.Findings) != 0 || out.Skipped == nil {
		t.Fatalf("want skip, got %+v", out)
	}
}

func TestNET006CGNAT(t *testing.T) {
	mk := func(prefix string) []sdk.ARMResource {
		return []sdk.ARMResource{
			acct("a", m{"networkInjections": l{m{"scenario": "agent", "subnetArmId": "/subscriptions/s/resourceGroups/r/providers/Microsoft.Network/virtualNetworks/vn/subnets/agents"}}}),
			{Type: "Microsoft.Network/virtualNetworks", Name: "vn", Properties: m{
				"addressSpace": m{"addressPrefixes": l{"10.0.0.0/16"}},
				"subnets":      l{m{"name": "agents", "properties": m{"addressPrefix": prefix}}}}},
		}
	}
	tests := []struct {
		prefix string
		want   int
		text   string
	}{
		{"10.0.2.0/24", 0, ""},
		{"100.64.1.0/24", 0, ""},          // RFC 6598 is supported
		{"100.100.1.0/24", 1, "reserved"}, // inside 100.100.0.0/17
		{"100.100.200.0/24", 1, "reserved"},
		{"100.100.128.0/24", 0, ""}, // 100.100.128.0 - 100.100.191.255 is not reserved
		{"100.127.0.0/24", 0, ""},
		{"100.128.0.0/24", 1, "RFC 1918 or RFC 6598"},
		{"44.0.0.0/24", 1, "RFC 1918 or RFC 6598"},
		{"172.30.1.0/24", 1, "reserved"},
	}
	for _, tc := range tests {
		out := runRule(t, net006{}, mk(tc.prefix), nil)
		if len(out.Findings) != tc.want || (out.Skipped != nil && tc.want > 0) {
			t.Errorf("%s: %+v", tc.prefix, out)
			continue
		}
		if tc.want > 0 && !strings.Contains(out.Findings[0].Evidence, tc.text) {
			t.Errorf("%s: evidence %q", tc.prefix, out.Findings[0].Evidence)
		}
		if out.Skipped != nil && strings.Contains(out.Skipped.Reason, "documentation conflicts") {
			t.Errorf("%s: CGNAT must be evaluated, not skipped", tc.prefix)
		}
	}
}

func TestPolicyKeysAreRelative(t *testing.T) {
	// Effective policy keys carry no "policy." prefix (ADR-007).
	if !policyBool(&sdk.Input{Policy: fakePolicy{"network.centralDns": true}}, "network.centralDns") {
		t.Fatal("relative key must be read")
	}
	if policyBool(&sdk.Input{Policy: fakePolicy{"policy.network.centralDns": true}}, "network.centralDns") {
		t.Fatal("prefixed key must not be read")
	}
}
