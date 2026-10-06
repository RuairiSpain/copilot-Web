package net

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fakeARM struct{ rs []sdk.ARMResource }

func (f fakeARM) Resources() []sdk.ARMResource { return f.rs }

type m = map[string]any
type l = []any

func eval(t *testing.T, r sdk.Rule, rs []sdk.ARMResource) sdk.Result {
	t.Helper()
	res, err := r.Evaluate(context.Background(), &sdk.Input{ARM: fakeARM{rs}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != nil && len(res.Findings) > 0 {
		t.Fatal("skip with findings")
	}
	return res
}

func acct(name string, props m) sdk.ARMResource {
	return sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts", Name: name, Kind: "AIServices", Region: "swedencentral", Properties: props}
}

func pe(name, target, group string) sdk.ARMResource {
	return sdk.ARMResource{Type: "Microsoft.Network/privateEndpoints", Name: name, Region: "swedencentral", Properties: m{
		"subnet":                        m{"id": "/subscriptions/s/resourceGroups/r/providers/Microsoft.Network/virtualNetworks/vn/subnets/pes"},
		"privateLinkServiceConnections": l{m{"properties": m{"privateLinkServiceId": "/x/providers/Foo/bar/" + target, "groupIds": l{group}}}},
	}}
}

func TestAllRulesSkipWithoutARM(t *testing.T) {
	rules := Register()
	if len(rules) != 11 {
		t.Fatalf("got %d rules", len(rules))
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.ID()] {
			t.Fatal("duplicate " + r.ID())
		}
		seen[r.ID()] = true
		for _, in := range []*sdk.Input{{}, nil} {
			res, err := r.Evaluate(context.Background(), in)
			if err != nil || res.Skipped == nil || res.Skipped.Reason != sdk.SkipInputUnavailable || len(res.Findings) != 0 {
				t.Fatalf("%s: want input-unavailable skip, got %+v %v", r.ID(), res, err)
			}
		}
		// ARM present but nothing applicable -> skipped, not passed.
		if res := eval(t, r, nil); res.Skipped == nil {
			t.Fatalf("%s: empty template must skip", r.ID())
		}
	}
}

func TestNET001(t *testing.T) {
	tests := []struct {
		name string
		rs   []sdk.ARMResource
		want int
		skip bool
	}{
		{"open", []sdk.ARMResource{acct("a", m{})}, 1, false},
		{"restricted ok", []sdk.ARMResource{acct("a", m{"publicNetworkAccess": "Enabled", "networkAcls": m{"defaultAction": "Deny"}})}, 0, false},
		{"disabled no pe", []sdk.ARMResource{acct("a", m{"publicNetworkAccess": "Disabled"})}, 1, false},
		{"disabled with pe", []sdk.ARMResource{acct("a", m{"publicNetworkAccess": "Disabled"}), pe("p", "a", "account")}, 0, false},
		{"disabled wrong group", []sdk.ARMResource{acct("a", m{"publicNetworkAccess": "Disabled"}), pe("p", "a", "blob")}, 1, false},
		{"param value", []sdk.ARMResource{acct("a", m{"publicNetworkAccess": "[parameters('x')]"})}, 0, true},
		{"non foundry kind", []sdk.ARMResource{{Type: "Microsoft.CognitiveServices/accounts", Name: "a", Kind: "FormRecognizer", Properties: m{}}}, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := eval(t, net001{}, tc.rs)
			if (res.Skipped != nil) != tc.skip || len(res.Findings) != tc.want {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestNET002(t *testing.T) {
	priv := func(extra m) sdk.ARMResource {
		p := m{"publicNetworkAccess": "Disabled", "capabilitySettings": m{"blobStore": "/x/providers/Microsoft.Storage/storageAccounts/st"}}
		for k, v := range extra {
			p[k] = v
		}
		return acct("a", p)
	}
	st := func(pa string) sdk.ARMResource {
		return sdk.ARMResource{Type: "Microsoft.Storage/storageAccounts", Name: "st", Properties: m{"publicNetworkAccess": pa}}
	}
	tests := []struct {
		name string
		rs   []sdk.ARMResource
		want int
		skip bool
	}{
		{"public storage", []sdk.ARMResource{priv(nil), st("Enabled"), pe("p", "st", "blob")}, 1, false},
		{"missing pe", []sdk.ARMResource{priv(nil), st("Disabled")}, 1, false},
		{"both fail", []sdk.ARMResource{priv(nil), st("Enabled")}, 2, false},
		{"good", []sdk.ARMResource{priv(nil), st("Disabled"), pe("p", "st", "blob")}, 0, false},
		{"external store", []sdk.ARMResource{priv(nil)}, 0, true},
		{"public account", []sdk.ARMResource{acct("a", m{}), st("Enabled")}, 0, true},
		{"managed network", []sdk.ARMResource{priv(m{"networkInjections": l{m{"scenario": "agent", "useMicrosoftManagedNetwork": true}}}), st("Enabled")}, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := eval(t, net002{}, tc.rs)
			if (res.Skipped != nil) != tc.skip || len(res.Findings) != tc.want {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestNET003(t *testing.T) {
	zg := sdk.ARMResource{Type: "Microsoft.Network/privateEndpoints/privateDnsZoneGroups", Name: "p/default", Properties: m{
		"privateDnsZoneConfigs": l{m{"properties": m{"privateDnsZoneId": "/z/privateDnsZones/privatelink.search.windows.net"}}}}}
	zone := sdk.ARMResource{Type: "Microsoft.Network/privateDnsZones", Name: "privatelink.search.windows.net"}
	link := sdk.ARMResource{Type: "Microsoft.Network/privateDnsZones/virtualNetworkLinks", Name: "privatelink.search.windows.net/l",
		Properties: m{"virtualNetwork": m{"id": "/subscriptions/s/resourceGroups/r/providers/Microsoft.Network/virtualNetworks/vn"}}}
	tests := []struct {
		name string
		rs   []sdk.ARMResource
		want int
		skip bool
	}{
		{"no zone group", []sdk.ARMResource{pe("p", "s", "searchService")}, 1, false},
		{"wrong zone", []sdk.ARMResource{pe("p", "s", "blob"), zg}, 1, false},
		{"missing link", []sdk.ARMResource{pe("p", "s", "searchService"), zg, zone}, 1, false},
		{"complete", []sdk.ARMResource{pe("p", "s", "searchService"), zg, zone, link}, 0, false},
		{"zone not visible (zone group verified, link unverifiable)", []sdk.ARMResource{pe("p", "s", "searchService"), zg}, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := eval(t, net003{}, tc.rs)
			if (res.Skipped != nil) != tc.skip || len(res.Findings) != tc.want {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestNET004And005(t *testing.T) {
	subnet := func(prefix string, deleg bool) sdk.ARMResource {
		p := m{"addressPrefix": prefix}
		if deleg {
			p["delegations"] = l{m{"properties": m{"serviceName": "Microsoft.App/environments"}}}
		}
		return sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks/subnets", Name: "vn/agents", Properties: p}
	}
	vnet := func(region string) sdk.ARMResource {
		return sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks", Name: "vn", Region: region, Properties: m{}}
	}
	inj := m{"networkInjections": l{m{"scenario": "agent", "subnetArmId": "/s/providers/Microsoft.Network/virtualNetworks/vn/subnets/agents"}}}
	tests := []struct {
		name string
		rule sdk.Rule
		rs   []sdk.ARMResource
		want int
		skip bool
	}{
		{"004 good", net004{}, []sdk.ARMResource{acct("a", inj), vnet("swedencentral"), subnet("10.0.0.0/24", true)}, 0, false},
		{"004 undelegated", net004{}, []sdk.ARMResource{acct("a", inj), vnet("swedencentral"), subnet("10.0.0.0/24", false)}, 1, false},
		{"004 too small", net004{}, []sdk.ARMResource{acct("a", inj), vnet("swedencentral"), subnet("10.0.0.0/28", true)}, 1, false},
		{"004 shared", net004{}, []sdk.ARMResource{acct("a", inj), acct("b", inj), vnet("swedencentral"), subnet("10.0.0.0/24", true)}, 2, false},
		{"004 unresolved", net004{}, []sdk.ARMResource{acct("a", inj)}, 0, true},
		{"005 same", net005{}, []sdk.ARMResource{acct("a", inj), vnet("Sweden Central"), subnet("10.0.0.0/24", true)}, 0, false},
		{"005 differ", net005{}, []sdk.ARMResource{acct("a", inj), vnet("northeurope"), subnet("10.0.0.0/24", true)}, 1, false},
		{"005 unresolved", net005{}, []sdk.ARMResource{acct("a", inj)}, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := eval(t, tc.rule, tc.rs)
			if (res.Skipped != nil) != tc.skip || len(res.Findings) != tc.want {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestNET007(t *testing.T) {
	mk := func(acls m) sdk.ARMResource {
		return acct("a", m{"publicNetworkAccess": "Enabled", "networkAcls": acls})
	}
	tests := []struct {
		name string
		rs   []sdk.ARMResource
		want int
		skip bool
	}{
		{"empty", []sdk.ARMResource{mk(m{"defaultAction": "Deny"})}, 1, false},
		{"ip ok", []sdk.ARMResource{mk(m{"defaultAction": "Deny", "ipRules": l{m{"value": "203.0.113.0/24"}}})}, 0, false},
		{"ipv6", []sdk.ARMResource{mk(m{"defaultAction": "Deny", "ipRules": l{m{"value": "2001:db8::/32"}}})}, 1, false},
		{"garbage", []sdk.ARMResource{mk(m{"defaultAction": "Deny", "ipRules": l{m{"value": "nope"}}})}, 1, false},
		{"vnet rule", []sdk.ARMResource{mk(m{"defaultAction": "Deny", "virtualNetworkRules": l{m{"id": "x"}}})}, 0, false},
		{"allow mode", []sdk.ARMResource{mk(m{"defaultAction": "Allow"})}, 0, true},
		{"pe present", []sdk.ARMResource{mk(m{"defaultAction": "Deny"}), pe("p", "a", "account")}, 0, true},
		{"param rules", []sdk.ARMResource{mk(m{"defaultAction": "Deny", "ipRules": "[parameters('ips')]"})}, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := eval(t, net007{}, tc.rs)
			if (res.Skipped != nil) != tc.skip || len(res.Findings) != tc.want {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestNET008(t *testing.T) {
	srch := func(sku, pa string) sdk.ARMResource {
		p := m{}
		if pa != "" {
			p["publicNetworkAccess"] = pa
		}
		return sdk.ARMResource{Type: "Microsoft.Search/searchServices", Name: "s", SKUName: sku, Properties: p}
	}
	tests := []struct {
		name string
		rs   []sdk.ARMResource
		want int
		skip bool
	}{
		{"free with pe", []sdk.ARMResource{srch("free", ""), pe("p", "s", "searchService")}, 1, false},
		{"Free mixed case disabled", []sdk.ARMResource{srch("Free", "Disabled"), pe("p", "s", "searchService")}, 1, false},
		{"free vector store", []sdk.ARMResource{srch("free", ""), acct("a", m{"publicNetworkAccess": "Disabled", "capabilitySettings": m{"vectorStore": "/x/searchServices/s"}})}, 1, false},
		{"free public alone", []sdk.ARMResource{srch("free", "")}, 0, false},
		{"standard with pe", []sdk.ARMResource{srch("standard", ""), pe("p", "s", "searchService")}, 0, false},
		{"s1 not free", []sdk.ARMResource{srch("standard3", "Disabled"), pe("p", "s", "searchService")}, 0, false},
		{"param sku", []sdk.ARMResource{srch("[parameters('sku')]", "")}, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := eval(t, net008{}, tc.rs)
			if (res.Skipped != nil) != tc.skip || len(res.Findings) != tc.want {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestDeterministicAndNoSecrets(t *testing.T) {
	rs := []sdk.ARMResource{acct("b", m{"apiKey": "SECRET-" + "VALUE"}), acct("a", m{"apiKey": "SECRET-" + "VALUE"})}
	res := eval(t, net001{}, rs)
	if len(res.Findings) != 2 || res.Findings[0].Resource.Name != "a" {
		t.Fatalf("unsorted: %+v", res.Findings)
	}
	for _, f := range res.Findings {
		if strings.Contains(f.Evidence, "SECRET") {
			t.Fatal("secret leaked")
		}
	}
}

type fakePolicy map[string]any

func (p fakePolicy) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

func TestNET003Cloud(t *testing.T) {
	build := func(region, group, zoneName string) []sdk.ARMResource {
		p := pe("p", "s", group)
		p.Region = region
		zgr := sdk.ARMResource{Type: "Microsoft.Network/privateEndpoints/privateDnsZoneGroups", Name: "p/default", Properties: m{
			"privateDnsZoneConfigs": l{m{"properties": m{"privateDnsZoneId": "/z/privateDnsZones/" + zoneName}}}}}
		return []sdk.ARMResource{p, zgr}
	}
	tests := []struct {
		name   string
		rs     []sdk.ARMResource
		policy fakePolicy
		want   int
		skip   string
	}{
		{"public ok", build("swedencentral", "blob", "privatelink.blob.core.windows.net"), nil, 0, ""},
		{"public wrong", build("swedencentral", "blob", "privatelink.blob.core.usgovcloudapi.net"), nil, 1, ""},
		{"usgov ok", build("usgovvirginia", "blob", "privatelink.blob.core.usgovcloudapi.net"), nil, 0, ""},
		{"usgov foundry ok", build("usgovvirginia", "account", "privatelink.cognitiveservices.azure.us"), nil, 0, ""},
		{"usgov public names", build("usgovvirginia", "blob", "privatelink.blob.core.windows.net"), nil, 1, ""},
		{"china ok", build("chinanorth3", "vault", "privatelink.vaultcore.azure.cn"), nil, 0, ""},
		{"china public names", build("chinanorth3", "vault", "privatelink.vaultcore.azure.net"), nil, 1, ""},
		{"china uncatalogued group", build("chinanorth3", "account", "privatelink.cognitiveservices.azure.cn"), nil, 0, "skip"},
		{"unknown region", build("[parameters('loc')]", "blob", "privatelink.blob.core.windows.net"), nil, 0, "skip"},
		{"policy overrides region", build("swedencentral", "blob", "privatelink.blob.core.usgovcloudapi.net"), fakePolicy{"policy.network.cloud": "usgov"}, 0, ""},
		{"bad policy value", build("swedencentral", "blob", "privatelink.blob.core.windows.net"), fakePolicy{"policy.network.cloud": "mars"}, 0, "skip"},
		{"central dns", build("swedencentral", "blob", "x"), fakePolicy{"policy.network.centralDns": true}, 0, "skip"},
		{"policy managed", build("swedencentral", "blob", "x"), fakePolicy{"policy.network.dnsManagedByPolicy": true}, 0, "skip"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var pol sdk.Policy
			if tc.policy != nil {
				pol = tc.policy
			}
			res, err := net003{}.Evaluate(context.Background(), &sdk.Input{ARM: fakeARM{tc.rs}, Policy: pol})
			if err != nil {
				t.Fatal(err)
			}
			if (res.Skipped != nil) != (tc.skip != "") || len(res.Findings) != tc.want {
				t.Fatalf("got %+v", res)
			}
		})
	}
}
