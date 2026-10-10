package sec

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func ydoc(t *testing.T, s string) sdk.AzureYAMLView {
	t.Helper()
	d, err := azureyaml.Parse([]byte(s), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLaterRules(t *testing.T) {
	diag := func(p props) sdk.ARMResource { return res(typeAPIMAPIDiag, "apim/chat/azuremonitor", p) }
	api := func(url string) sdk.ARMResource { return res(typeAPIMAPI, "apim/chat", props{"serviceUrl": url}) }
	tests := []struct {
		name     string
		id       string
		in       *sdk.Input
		findings int
		skip     bool
		contains string
	}{
		{
			name:     "sec005 purge protection",
			id:       "FND-SEC-005",
			in:       &sdk.Input{ARM: model{res(typeKeyVault, "kv", props{"enableSoftDelete": true, "enablePurgeProtection": false, "enableRbacAuthorization": true})}},
			findings: 1, contains: "enablePurgeProtection",
		},
		{
			name:     "sec007 hosted agent bare policy id",
			id:       "FND-SEC-007",
			in:       &sdk.Input{ARM: model{}, AzureYAML: ydoc(t, "services:\n  a:\n    host: azure.ai.agent\n    kind: hosted\n    policies:\n      - type: rai_policy\n        raiPolicyName: my-policy\n")},
			findings: 1, contains: "full ARM resource ID",
		},
		{
			name:     "sec008 free ai plan",
			id:       "FND-SEC-008",
			in:       &sdk.Input{ARM: model{res(typeAccounts, "acct", props{}), res(typePricing, "AI", props{"pricingTier": "Free"})}},
			findings: 1, contains: "pricingTier",
		},
		{
			name:     "sec009 do not enforce",
			id:       "FND-SEC-009",
			in:       &sdk.Input{ARM: model{res(typePolicyAssignment, "p", props{"policyDefinitionId": "/providers/Microsoft.Authorization/policyDefinitions/71ef260a-8f18-47b7-abcb-62d0673d94dc", "enforcementMode": "DoNotEnforce"})}},
			findings: 1, contains: "DoNotEnforce",
		},
		{
			name:     "sec010 cmk info",
			id:       "FND-SEC-010",
			in:       &sdk.Input{ARM: model{res(typeAccounts, "acct", props{"encryption": props{"keySource": "Microsoft.KeyVault", "keyVaultProperties": props{"keyVaultUri": "https://kv"}}})}},
			findings: 1, contains: "CMK posture: cmk",
		},
		{
			name:     "sec011 unrestricted egress",
			id:       "FND-SEC-011",
			in:       &sdk.Input{ARM: model{res(typeAccounts, "acct", props{})}},
			findings: 1, contains: "outbound network posture",
		},
		{
			name:     "sec012 llm messages",
			id:       "FND-SEC-012",
			in:       &sdk.Input{ARM: model{diag(props{"largeLanguageModel": props{"requests": props{"messages": "all"}}}), api("https://my.openai.azure.com")}},
			findings: 1, contains: "raw LLM request",
		},
		{
			name:     "sec013 organizations tenant",
			id:       "FND-SEC-013",
			in:       &sdk.Input{ARM: model{res(typeAPIMAPIPolicy, "apim/apis/chat/policy", props{"value": "<policies><inbound><validate-azure-ad-token tenant-id=\"organizations\"><audiences><audience>a</audience></audiences></validate-azure-ad-token></inbound></policies>"}), api("https://my.services.ai.azure.com")}},
			findings: 1, contains: "multi-tenant",
		},
		{
			name:     "sec014 secret output name",
			id:       "FND-SEC-014",
			in:       &sdk.Input{ARM: armOutStub{names: []string{"accountPassword"}}},
			findings: 1, contains: "secret-shaped",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, tc.id, tc.in)
			if (out.Skipped != nil) != tc.skip {
				t.Fatalf("skip=%v want %v", out.Skipped, tc.skip)
			}
			if len(out.Findings) != tc.findings {
				t.Fatalf("findings=%d want %d: %+v", len(out.Findings), tc.findings, out.Findings)
			}
			if tc.contains != "" && tc.findings > 0 && !strings.Contains(out.Findings[0].Evidence, tc.contains) {
				t.Fatalf("evidence %q lacks %q", out.Findings[0].Evidence, tc.contains)
			}
		})
	}
}

type armOutStub struct{ names []string }

func (s armOutStub) Resources() []sdk.ARMResource { return nil }
func (s armOutStub) Outputs() []sdk.ARMOutput {
	var out []sdk.ARMOutput
	for _, name := range s.names {
		out = append(out, sdk.ARMOutput{Name: name, Type: "string"})
	}
	return out
}

func TestLaterRuleSkips(t *testing.T) {
	for _, id := range []string{"FND-SEC-008", "FND-SEC-009", "FND-SEC-014"} {
		var r sdk.Rule
		for _, rr := range Register() {
			if rr.ID() == id {
				r = rr
				break
			}
		}
		out, err := r.Evaluate(context.Background(), &sdk.Input{ARM: model{}})
		if err != nil || out.Skipped == nil {
			t.Fatalf("%s: want skip, got %+v err=%v", id, out, err)
		}
	}
}
