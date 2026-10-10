package net

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestNET006009010011(t *testing.T) {
	tests := []struct {
		name     string
		rule     sdk.Rule
		rs       []sdk.ARMResource
		policy   sdk.Policy
		findings int
		skip     bool
		contains string
	}{
		{
			name: "net006 public range",
			rule: net006{},
			rs: []sdk.ARMResource{
				acct("a", m{"publicNetworkAccess": "Disabled", "networkInjections": l{m{"scenario": "agent", "subnetArmId": "/s/providers/Microsoft.Network/virtualNetworks/vn/subnets/agents"}}}),
				{Type: "Microsoft.Network/virtualNetworks", Name: "vn", Properties: m{"addressSpace": m{"addressPrefixes": l{"44.0.0.0/16"}}, "subnets": l{m{"name": "agents", "properties": m{"addressPrefix": "44.0.0.0/24"}}}}},
			},
			findings: 1, contains: "private address space",
		},
		{
			name:     "net009 classic sku mismatch",
			rule:     net009{},
			rs:       []sdk.ARMResource{{Type: "Microsoft.ApiManagement/service", Name: "apim", SKUName: "Standard", Properties: m{"virtualNetworkType": "External"}}},
			findings: 1, contains: "Developer or Premium",
		},
		{
			name:     "net010 missing ampls",
			rule:     net010{},
			rs:       []sdk.ARMResource{acct("a", m{"publicNetworkAccess": "Disabled"}), {Type: "Microsoft.Insights/components", Name: "appi", Properties: m{"publicNetworkAccessForIngestion": "Disabled"}}},
			findings: 1, contains: "Private Link Scope",
		},
		{
			name:     "net011 subnet without nsg",
			rule:     net011{},
			rs:       []sdk.ARMResource{{Type: "Microsoft.Network/virtualNetworks", Name: "vn", Properties: m{"subnets": l{m{"name": "pe", "properties": m{"addressPrefix": "10.0.1.0/24"}}}}}},
			findings: 1, contains: "networkSecurityGroup",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := &sdk.Input{ARM: fakeARM{tc.rs}, Policy: tc.policy}
			res, err := tc.rule.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if (res.Skipped != nil) != tc.skip {
				t.Fatalf("skip=%v want %v", res.Skipped, tc.skip)
			}
			if len(res.Findings) != tc.findings {
				t.Fatalf("findings=%d want %d: %+v", len(res.Findings), tc.findings, res.Findings)
			}
			if tc.contains != "" && tc.findings > 0 {
				found := false
				for _, f := range res.Findings {
					if strings.Contains(f.Evidence, tc.contains) {
						found = true
					}
				}
				if !found {
					t.Fatalf("no evidence contains %q: %+v", tc.contains, res.Findings)
				}
			}
		})
	}
}
