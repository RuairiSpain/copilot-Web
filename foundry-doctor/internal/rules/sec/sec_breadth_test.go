package sec

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// fakePolicy is a minimal sdk.Policy for tests.
type fakePolicy map[string]any

func (p fakePolicy) Get(key string) (any, bool) { v, ok := p[key]; return v, ok }

const (
	idLocalAuth  = "71ef260a-8f18-47b7-abcb-62d0673d94dc"
	idSoftDelete = "1e66c121-a66a-4b1f-9b83-0fd99bf0fc2d"
	idPurge      = "0b60c0b2-2dc2-4e1c-b5c9-abbed971de53"
	idKVRBAC     = "12d4fa5e-1f9f-4c21-97a9-b99b3c6611b5"
	idAICMK      = "67121cc7-ff39-4ab8-b7e3-95b84dab487d"
)

func policyID(guid string) string {
	return "/providers/Microsoft.Authorization/policyDefinitions/" + guid
}

func assignment(name, def, mode string) sdk.ARMResource {
	p := props{"policyDefinitionId": def}
	if mode != "" {
		p["enforcementMode"] = mode
	}
	return res(typePolicyAssignment, name, p)
}

func allMappedAssignments(mode string) []sdk.ARMResource {
	var rs []sdk.ARMResource
	for i, g := range []string{idLocalAuth, idSoftDelete, idPurge, idKVRBAC, idAICMK} {
		rs = append(rs, assignment("a"+string(rune('0'+i)), policyID(g), mode))
	}
	return rs
}

func hasSkipPrefix(out sdk.Result, prefix string) bool {
	if out.Skipped != nil && strings.HasPrefix(out.Skipped.Reason, prefix) {
		return true
	}
	for _, s := range out.Skips {
		if strings.HasPrefix(s.Reason, prefix) {
			return true
		}
	}
	return false
}

func acctWithProject() []sdk.ARMResource {
	return []sdk.ARMResource{res(typeAccounts, "acct", props{}), res(typeAcctProject, "acct/proj", props{})}
}

func deployment(name string, p props) sdk.ARMResource {
	return res(typeAcctDeploy, name, p)
}

// ---- FND-SEC-007 -----------------------------------------------------------

func TestSEC007Deployments(t *testing.T) {
	decl := func(acct, name string, filters ...any) sdk.ARMResource {
		p := props{}
		if len(filters) > 0 {
			p["contentFilters"] = filters
		}
		return res(typeRaiPolicy, acct+"/"+name, p)
	}
	flt := func(enabled any) any { return props{"name": "Hate", "enabled": enabled, "source": "Prompt"} }
	cases := []struct {
		name     string
		rs       []sdk.ARMResource
		findings int
		skipHead string // reason prefix of Skipped, or "" for none
		contains string
		infoSev  bool
	}{
		{name: "no raiPolicyName is an info finding", rs: append(acctWithProject(), deployment("acct/gpt", props{"model": props{"format": "OpenAI", "name": "gpt-4o"}})),
			findings: 1, contains: "raiPolicyName", infoSev: true},
		{name: "empty raiPolicyName is an info finding", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": " "})),
			findings: 1, contains: "raiPolicyName", infoSev: true},
		{name: "Microsoft.DefaultV2 passes", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "Microsoft.DefaultV2"}))},
		{name: "Microsoft.Default is not verified so uncertain", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "Microsoft.Default"})), skipHead: "unresolved-expression"},
		{name: "expression raiPolicyName is uncertain", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "[parameters('rai')]"})), skipHead: "unresolved-expression"},
		{name: "declared on the same account passes", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "strict"}), decl("acct", "strict", flt(true)))},
		{name: "template-local name not declared is a finding", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "strict"})),
			findings: 1, contains: "not declared"},
		{name: "policy declared on another account is a finding", rs: append(acctWithProject(), res(typeAccounts, "other", props{}), deployment("acct/gpt", props{"raiPolicyName": "strict"}), decl("other", "strict", flt(true))),
			findings: 1, contains: "not declared"},
		{name: "parent account outside the template is uncertain", rs: []sdk.ARMResource{deployment("acct/gpt", props{"raiPolicyName": "strict"})}, skipHead: "unresolved-expression"},
		{name: "all content filters disabled is a finding", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "lax"}), decl("acct", "lax", flt(false), flt(false))),
			findings: 1, contains: "every content filter"},
		{name: "one enabled filter passes", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "mixed"}), decl("acct", "mixed", flt(false), flt(true)))},
		{name: "expression enabled flag is uncertain", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "x"}), decl("acct", "x", flt("[parameters('on')]"))), skipHead: "unresolved-expression"},
		{name: "policy without contentFilters is not judged", rs: append(acctWithProject(), deployment("acct/gpt", props{"raiPolicyName": "base"}), decl("acct", "base"))},
		{name: "conditional deployment is skipped", rs: append(acctWithProject(), func() sdk.ARMResource {
			d := deployment("acct/gpt", props{})
			d.Conditional, d.Condition = true, "[parameters('deploy')]"
			return d
		}()), skipHead: "conditional-resource"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, "FND-SEC-007", &sdk.Input{ARM: model(tc.rs)})
			if len(out.Findings) != tc.findings {
				t.Fatalf("findings=%d want %d: %+v skip=%+v", len(out.Findings), tc.findings, out.Findings, out.Skipped)
			}
			if tc.skipHead == "" && out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if tc.skipHead != "" && (out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, tc.skipHead)) {
				t.Fatalf("want skip %q got %+v", tc.skipHead, out.Skipped)
			}
			if tc.findings > 0 {
				if !strings.Contains(out.Findings[0].Evidence, tc.contains) {
					t.Errorf("evidence %q lacks %q", out.Findings[0].Evidence, tc.contains)
				}
				if out.Findings[0].Key == "" {
					t.Errorf("finding has no Key")
				}
				if tc.infoSev && out.Findings[0].Severity != sdk.SeverityInfo {
					t.Errorf("severity = %q want info", out.Findings[0].Severity)
				}
			}
		})
	}
}

func TestSEC007NoDeploymentsNotApplicable(t *testing.T) {
	out := run(t, "FND-SEC-007", &sdk.Input{ARM: model(acctWithProject())})
	if out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, sdk.SkipNotApplicable) {
		t.Fatalf("got %+v", out)
	}
}

func TestSEC007AzureYAMLGatedByAzdSchema(t *testing.T) {
	const hostedNoPolicies = "services:\n  a:\n    host: azure.ai.agent\n    kind: hosted\n"
	const hostedBare = "services:\n  a:\n    host: azure.ai.agent\n    kind: hosted\n    policies:\n      - type: rai_policy\n        raiPolicyName: my-policy\n"
	cases := []struct {
		name     string
		azd      string
		yaml     string
		findings int
		skipHead string
	}{
		{"azd 1.34.2 has no policies field so no finding", "1.34.2", hostedNoPolicies, 0, sdk.SkipNotApplicable},
		{"azd 1.35.0 has no policies field so no finding", "1.35.0", hostedNoPolicies, 0, sdk.SkipNotApplicable},
		{"azd 1.36.0-beta.1 evaluates missing policies", "1.36.0-beta.1", hostedNoPolicies, 1, ""},
		{"azd 1.36.0 evaluates missing policies", "1.36.0", hostedNoPolicies, 1, ""},
		{"azd 1.40.0 (newer than verified) evaluates", "1.40.0", hostedNoPolicies, 1, ""},
		{"unknown azd and no key is uncertain", "", hostedNoPolicies, 0, "uncertain"},
		{"old azd but key present is still evaluated", "1.34.2", hostedBare, 1, ""},
		{"unknown azd but key present is evaluated", "", hostedBare, 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, "FND-SEC-007", &sdk.Input{ARM: model{}, AzdVersion: tc.azd, AzureYAML: ydoc(t, tc.yaml)})
			if len(out.Findings) != tc.findings {
				t.Fatalf("findings=%d want %d: %+v", len(out.Findings), tc.findings, out)
			}
			if tc.skipHead == "" && out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if tc.skipHead != "" && (out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, tc.skipHead)) {
				t.Fatalf("want skip %q got %+v", tc.skipHead, out.Skipped)
			}
		})
	}
}

// ---- FND-SEC-008 -----------------------------------------------------------

func pricing(plan, tier string) sdk.ARMResource {
	return res(typePricing, plan, props{"pricingTier": tier})
}

func TestSEC008Plans(t *testing.T) {
	acct := res(typeAccounts, "acct", props{})
	cases := []struct {
		name     string
		rs       []sdk.ARMResource
		findings int
		skipHead string
		contains string
	}{
		{name: "AI and Arm Standard with an account passes", rs: []sdk.ARMResource{acct, pricing("AI", "Standard"), pricing("Arm", "Standard")}},
		{name: "Arm Free with an account fails", rs: []sdk.ARMResource{acct, pricing("AI", "Standard"), pricing("Arm", "Free")}, findings: 1, contains: "Arm"},
		{name: "KeyVaults Free with a vault fails", rs: []sdk.ARMResource{acct, res(typeKeyVault, "kv", props{}), pricing("AI", "Standard"), pricing("Arm", "Standard"), pricing("KeyVaults", "Free")}, findings: 1, contains: "KeyVaults"},
		{name: "AppServices relevant when a web app exists", rs: []sdk.ARMResource{acct, res("Microsoft.Web/sites", "site", props{}), pricing("AI", "Standard"), pricing("Arm", "Standard"), pricing("AppServices", "Free")}, findings: 1, contains: "AppServices"},
		{name: "absent pricing for a present resource type is input-unavailable", rs: []sdk.ARMResource{acct, res(typeStorage, "st", props{}), pricing("AI", "Standard"), pricing("Arm", "Standard")}, skipHead: sdk.SkipInputUnavailable + ": subscription-scope Defender state not in template"},
		{name: "no pricings at all is input-unavailable", rs: []sdk.ARMResource{acct}, skipHead: sdk.SkipInputUnavailable + ": subscription-scope Defender state not in template"},
		{name: "expression tier is uncertain", rs: []sdk.ARMResource{acct, pricing("AI", "[parameters('tier')]"), pricing("Arm", "Standard")}, skipHead: "unresolved-expression"},
		{name: "no relevant resources is not applicable", rs: []sdk.ARMResource{res("Microsoft.Network/virtualNetworks", "v", props{})}, skipHead: sdk.SkipNotApplicable},
		{name: "conditional pricing is uncertain", rs: []sdk.ARMResource{acct, func() sdk.ARMResource {
			p := pricing("AI", "Standard")
			p.Conditional, p.Condition = true, "[parameters('x')]"
			return p
		}(), pricing("Arm", "Standard")}, skipHead: "conditional-resource"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, "FND-SEC-008", &sdk.Input{ARM: model(tc.rs)})
			if len(out.Findings) != tc.findings {
				t.Fatalf("findings=%d want %d: %+v skip=%+v", len(out.Findings), tc.findings, out.Findings, out.Skipped)
			}
			if tc.skipHead == "" && out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if tc.skipHead != "" && (out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, tc.skipHead)) {
				t.Fatalf("want skip %q got %+v", tc.skipHead, out.Skipped)
			}
			if tc.findings > 0 && !strings.Contains(out.Findings[0].Evidence, tc.contains) {
				t.Errorf("evidence %q lacks %q", out.Findings[0].Evidence, tc.contains)
			}
		})
	}
}

func TestSEC008MissingCapabilityNamed(t *testing.T) {
	out := run(t, "FND-SEC-008", &sdk.Input{ARM: model{res(typeAccounts, "acct", props{})}})
	if out.Skipped == nil || out.Skipped.MissingCapability == "" {
		t.Fatalf("skip must name the missing capability: %+v", out.Skipped)
	}
	for _, plan := range []string{"AI", "Arm"} {
		if !strings.Contains(out.Skipped.Reason, plan) {
			t.Errorf("reason %q does not name plan %s", out.Skipped.Reason, plan)
		}
	}
}

func TestSEC008FindingKeepsMissingPlansVisible(t *testing.T) {
	out := run(t, "FND-SEC-008", &sdk.Input{ARM: model{res(typeAccounts, "acct", props{}), res(typeStorage, "st", props{}), pricing("AI", "Free")}})
	if len(out.Findings) != 1 || out.Skipped != nil {
		t.Fatalf("got %+v", out)
	}
	if !hasSkipPrefix(out, sdk.SkipInputUnavailable+": subscription-scope Defender state not in template") {
		t.Errorf("plans absent from the template must stay visible as skips: %+v", out.Skips)
	}
}

// ---- FND-SEC-009 -----------------------------------------------------------

func TestSEC009Coverage(t *testing.T) {
	cases := []struct {
		name     string
		rs       []sdk.ARMResource
		policy   fakePolicy
		findings int
		skipHead string
		contains string
	}{
		{name: "all mapped policies enforced passes", rs: allMappedAssignments("Default")},
		{name: "absent enforcementMode is Default per spec", rs: allMappedAssignments("")},
		{name: "DoNotEnforce is a finding", rs: append(allMappedAssignments("Default")[1:], assignment("x", policyID(idLocalAuth), "DoNotEnforce")), findings: 1, contains: "DoNotEnforce"},
		{name: "missing mapped policy is uncertain", rs: allMappedAssignments("Default")[:4], skipHead: "uncertain", contains: idAICMK},
		{name: "Enroll mode is uncertain", rs: append(allMappedAssignments("Default")[1:], assignment("x", policyID(idLocalAuth), "Enroll")), skipHead: "uncertain"},
		{name: "initiative assignment cannot be expanded", rs: []sdk.ARMResource{assignment("i", "/providers/Microsoft.Authorization/policySetDefinitions/abc", "Default")}, skipHead: "uncertain"},
		{name: "expression definition id is uncertain", rs: []sdk.ARMResource{assignment("e", "[parameters('def')]", "Default")}, skipHead: "uncertain"},
		{name: "unmapped assignment only is uncertain", rs: []sdk.ARMResource{assignment("u", policyID("11111111-1111-1111-1111-111111111111"), "Default")}, skipHead: "uncertain"},
		{name: "no assignments is input-unavailable", rs: []sdk.ARMResource{res(typeAccounts, "a", props{})}, skipHead: sdk.SkipInputUnavailable},
		{name: "policy list override replaces the default list", rs: []sdk.ARMResource{assignment("o", policyID("22222222-2222-2222-2222-222222222222"), "Default")},
			policy: fakePolicy{PolicyKeyMappedDefinitions: []any{"22222222-2222-2222-2222-222222222222"}}},
		{name: "policy list override DoNotEnforce is a finding", rs: []sdk.ARMResource{assignment("o", policyID("22222222-2222-2222-2222-222222222222"), "DoNotEnforce")},
			policy: fakePolicy{PolicyKeyMappedDefinitions: []string{"22222222-2222-2222-2222-222222222222"}}, findings: 1, contains: "DoNotEnforce"},
		{name: "empty override falls back to the catalogue list", rs: allMappedAssignments("Default"), policy: fakePolicy{PolicyKeyMappedDefinitions: []any{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &sdk.Input{ARM: model(tc.rs)}
			if tc.policy != nil {
				in.Policy = tc.policy
			}
			out := run(t, "FND-SEC-009", in)
			if len(out.Findings) != tc.findings {
				t.Fatalf("findings=%d want %d: %+v skip=%+v", len(out.Findings), tc.findings, out.Findings, out.Skipped)
			}
			if tc.skipHead == "" && out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if tc.skipHead != "" && (out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, tc.skipHead)) {
				t.Fatalf("want skip %q got %+v", tc.skipHead, out.Skipped)
			}
			if tc.contains != "" {
				text := ""
				if out.Skipped != nil {
					text = out.Skipped.Reason
				}
				for _, f := range out.Findings {
					text += f.Evidence
				}
				if !strings.Contains(text, tc.contains) {
					t.Errorf("%q lacks %q", text, tc.contains)
				}
			}
			for _, f := range out.Findings {
				if f.Key == "" {
					t.Errorf("finding without Key")
				}
			}
		})
	}
}

func TestSEC009MappedListIsVerifiedGUIDs(t *testing.T) {
	got := mappedSecurityPolicies()
	if len(got) != 5 {
		t.Fatalf("mapped list has %d entries", len(got))
	}
	for _, g := range []string{idLocalAuth, idSoftDelete, idPurge, idKVRBAC, idAICMK} {
		if !got[g] {
			t.Errorf("missing %s", g)
		}
	}
}

// ---- FND-SEC-010 -----------------------------------------------------------

func TestSEC010ViolationsOnly(t *testing.T) {
	requirePolicy := fakePolicy{PolicyKeyRequireCMK: true}
	cmkAcct := res(typeAccounts, "acct", props{"encryption": props{"keySource": "Microsoft.KeyVault", "keyVaultProperties": props{"keyVaultUri": "https://kv.vault.azure.net"}}})
	pmAcct := res(typeAccounts, "acct", props{"encryption": props{"keySource": "Microsoft.CognitiveServices"}})
	conn := func(target string) sdk.ARMResource {
		return res(typeAccounts+"/connections", "acct/c-"+target, props{"target": target})
	}
	cases := []struct {
		name     string
		rs       []sdk.ARMResource
		policy   sdk.Policy
		findings int
		skipHead string // Skipped head
		notes    string // a Skips prefix expected
		contains string
	}{
		{name: "compliant account passes", rs: []sdk.ARMResource{cmkAcct}},
		{name: "compliant account passes even when required", rs: []sdk.ARMResource{cmkAcct}, policy: requirePolicy},
		{name: "platform-managed without requirement is a note", rs: []sdk.ARMResource{pmAcct}, notes: "not-applicable"},
		{name: "platform-managed with policy-key requirement is a finding", rs: []sdk.ARMResource{pmAcct}, policy: requirePolicy, findings: 1, contains: "platform-managed"},
		{name: "false policy key declares no requirement", rs: []sdk.ARMResource{pmAcct}, policy: fakePolicy{PolicyKeyRequireCMK: false}, notes: "not-applicable"},
		{name: "enforced CMK policy assignment declares the requirement", rs: []sdk.ARMResource{pmAcct, assignment("cmk", policyID(idAICMK), "Default")}, findings: 1},
		{name: "DoNotEnforce CMK policy assignment declares nothing", rs: []sdk.ARMResource{pmAcct, assignment("cmk", policyID(idAICMK), "DoNotEnforce")}, notes: "not-applicable"},
		{name: "account KeyVault source without key properties is uncertain", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"encryption": props{"keySource": "Microsoft.KeyVault"}})}, skipHead: "unresolved-expression"},
		{name: "expression keySource is uncertain", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"encryption": props{"keySource": "[parameters('ks')]"}})}, skipHead: "unresolved-expression"},
		{name: "search enforcement Enabled passes", rs: []sdk.ARMResource{conn("srch"), res(typeSearch, "srch", props{"encryptionWithCmk": props{"enforcement": "Enabled"}})}, policy: requirePolicy},
		{name: "search Unspecified is a violation when required", rs: []sdk.ARMResource{conn("srch"), res(typeSearch, "srch", props{"encryptionWithCmk": props{"enforcement": "Unspecified"}})}, policy: requirePolicy, findings: 1, contains: "platform-managed"},
		{name: "cosmos key uri passes", rs: []sdk.ARMResource{conn("db"), res(typeCosmos, "db", props{"keyVaultKeyUri": "https://kv.vault.azure.net/keys/k"})}, policy: requirePolicy},
		{name: "cosmos without key uri is a violation when required", rs: []sdk.ARMResource{conn("db"), res(typeCosmos, "db", props{})}, policy: requirePolicy, findings: 1},
		{name: "storage Keyvault source passes", rs: []sdk.ARMResource{conn("st"), res(typeStorage, "st", props{"encryption": props{"keySource": "Microsoft.Keyvault"}})}, policy: requirePolicy},
		{name: "storage Microsoft.Storage is a violation when required", rs: []sdk.ARMResource{conn("st"), res(typeStorage, "st", props{"encryption": props{"keySource": "Microsoft.Storage"}})}, policy: requirePolicy, findings: 1},
		{name: "registry CMK passes", rs: []sdk.ARMResource{res(typeRegistry, "acr", props{"encryption": props{"status": "enabled", "keyVaultProperties": props{"keyIdentifier": "https://kv.vault.azure.net/keys/k"}}})}, policy: requirePolicy},
		{name: "registry without encryption is a violation when required", rs: []sdk.ARMResource{res(typeRegistry, "acr", props{})}, policy: requirePolicy, findings: 1},
		{name: "unlinked storage is out of scope", rs: []sdk.ARMResource{conn("other"), res(typeStorage, "st", props{})}, policy: requirePolicy, skipHead: "not-applicable"},
		{name: "no resources is not applicable", rs: []sdk.ARMResource{res("Microsoft.Network/virtualNetworks", "v", props{})}, skipHead: sdk.SkipNotApplicable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &sdk.Input{ARM: model(tc.rs)}
			if tc.policy != nil {
				in.Policy = tc.policy
			}
			out := run(t, "FND-SEC-010", in)
			if len(out.Findings) != tc.findings {
				t.Fatalf("findings=%d want %d: %+v skip=%+v skips=%+v", len(out.Findings), tc.findings, out.Findings, out.Skipped, out.Skips)
			}
			for _, f := range out.Findings {
				if strings.Contains(f.Evidence, "CMK posture: cmk") {
					t.Errorf("compliant state must not be a finding: %q", f.Evidence)
				}
			}
			if tc.skipHead == "" && out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if tc.skipHead != "" && (out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, tc.skipHead)) {
				t.Fatalf("want skip %q got %+v", tc.skipHead, out.Skipped)
			}
			if tc.notes != "" && !hasSkipPrefix(out, tc.notes) {
				t.Fatalf("want a %q note, got %+v", tc.notes, out)
			}
			if tc.contains != "" && !strings.Contains(out.Findings[0].Evidence, tc.contains) {
				t.Errorf("evidence %q lacks %q", out.Findings[0].Evidence, tc.contains)
			}
		})
	}
}

// ---- FND-SEC-011 -----------------------------------------------------------

func TestSEC011Egress(t *testing.T) {
	mn := func(mode any) sdk.ARMResource {
		return res(typeManagedNetwork, "acct/default", props{"managedNetwork": props{"isolationMode": mode}})
	}
	cases := []struct {
		name     string
		rs       []sdk.ARMResource
		findings int
		skipHead string
		notes    string
		contains string
	}{
		{name: "public account without egress setting is one info finding", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{})}, findings: 1, contains: "outbound network posture"},
		{name: "explicit public access without egress setting is one finding", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"publicNetworkAccess": "Enabled"})}, findings: 1},
		{name: "private account without egress setting yields a note not a finding", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"publicNetworkAccess": "Disabled"})}, notes: "uncertain"},
		{name: "network injection yields a note not a finding", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"networkInjections": []any{props{"scenario": "agent", "subnetArmId": "x"}}})}, notes: "uncertain"},
		{name: "expression publicNetworkAccess is uncertain", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"publicNetworkAccess": "[parameters('p')]"})}, skipHead: "unresolved-expression"},
		{name: "restrictOutboundNetworkAccess with allowed list passes", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"restrictOutboundNetworkAccess": true, "allowedFqdnList": []any{"api.contoso.com"}})}},
		{name: "restrictOutboundNetworkAccess with empty list is a note", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"restrictOutboundNetworkAccess": true})}, notes: "uncertain"},
		{name: "restrictOutboundNetworkAccess false on public account is a finding", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"restrictOutboundNetworkAccess": false})}, findings: 1},
		{name: "managed network AllowOnlyApprovedOutbound passes", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{}), mn("AllowOnlyApprovedOutbound")}},
		{name: "managed network AllowInternetOutbound is a finding", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{}), mn("AllowInternetOutbound")}, findings: 1, contains: "AllowInternetOutbound"},
		{name: "managed network Disabled is a finding", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{}), mn("Disabled")}, findings: 1, contains: "Disabled"},
		{name: "managed network expression is uncertain", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{}), mn("[parameters('m')]")}, skipHead: "unresolved-expression"},
		{name: "unverified account-level managedNetworkSettings is ignored", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{"managedNetworkSettings": props{"isolationMode": "AllowOnlyApprovedOutbound"}})}, findings: 1},
		{name: "managed network of another account does not count", rs: []sdk.ARMResource{res(typeAccounts, "acct", props{}), func() sdk.ARMResource {
			r := mn("AllowOnlyApprovedOutbound")
			r.Name = "other/default"
			return r
		}()}, findings: 1},
		{name: "two accounts give one finding each", rs: []sdk.ARMResource{res(typeAccounts, "a1", props{}), res(typeAccounts, "a2", props{}), res(typeAcctProject, "a1/p", props{}), res(typeAcctProject, "a2/p", props{})}, findings: 2},
		{name: "no account is not applicable", rs: []sdk.ARMResource{res(typeStorage, "st", props{})}, skipHead: sdk.SkipNotApplicable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, "FND-SEC-011", &sdk.Input{ARM: model(tc.rs)})
			if len(out.Findings) != tc.findings {
				t.Fatalf("findings=%d want %d: %+v skip=%+v skips=%+v", len(out.Findings), tc.findings, out.Findings, out.Skipped, out.Skips)
			}
			if tc.skipHead == "" && out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if tc.skipHead != "" && (out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, tc.skipHead)) {
				t.Fatalf("want skip %q got %+v", tc.skipHead, out.Skipped)
			}
			if tc.notes != "" && !hasSkipPrefix(out, tc.notes) {
				t.Fatalf("want %q note: %+v", tc.notes, out)
			}
			for _, f := range out.Findings {
				if f.Severity != sdk.SeverityInfo {
					t.Errorf("SEC-011 findings are informational, got %q", f.Severity)
				}
			}
			if tc.contains != "" && !strings.Contains(out.Findings[0].Evidence, tc.contains) {
				t.Errorf("evidence %q lacks %q", out.Findings[0].Evidence, tc.contains)
			}
		})
	}
}

func TestSEC011AzureYAMLIsolationMode(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		findings int
	}{{"AllowInternetOutbound", 1}, {"AllowOnlyApprovedOutbound", 0}} {
		y := "services:\n  p:\n    host: azure.ai.project\n    network:\n      isolationMode: " + tc.mode + "\n"
		out := run(t, "FND-SEC-011", &sdk.Input{ARM: model{}, AzureYAML: ydoc(t, y)})
		if len(out.Findings) != tc.findings {
			t.Errorf("%s: findings=%d want %d (%+v)", tc.mode, len(out.Findings), tc.findings, out)
		}
	}
}

// ---- catalogue compatibility ----------------------------------------------

func TestCatalogueAzdRangesAreOpenEnded(t *testing.T) {
	rules, err := catalog.Load(context.Background(), os.DirFS("../../.."), "rules/catalog")
	if err != nil {
		t.Fatalf("load catalogue: %v", err)
	}
	want := map[string]bool{"FND-SEC-007": true, "FND-SEC-011": true, "FND-SEC-014": true}
	seen := 0
	for _, r := range rules {
		if !want[r.ID] {
			continue
		}
		seen++
		if r.Compatibility.Azd.Contains("1.34.1") {
			t.Errorf("%s accepts azd 1.34.1", r.ID)
		}
		for _, v := range []string{"1.34.2", "1.36.0", "1.37.0", "2.0.0"} {
			if !r.Compatibility.Azd.Contains(v) {
				t.Errorf("%s rejects azd %s (would skip as unsupported-version)", r.ID, v)
			}
		}
	}
	if seen != len(want) {
		t.Fatalf("found %d of %d rules", seen, len(want))
	}
}

// SEC-014 reads only compiled outputs; the azd version must not change its result.
func TestSEC014IgnoresAzdVersion(t *testing.T) {
	for _, v := range []string{"", "1.34.2", "1.36.0", "1.99.0"} {
		out := run(t, "FND-SEC-014", &sdk.Input{ARM: armOutStub{names: []string{"accountPassword"}}, AzdVersion: v})
		if out.Skipped != nil || len(out.Findings) != 1 {
			t.Errorf("azd %q: got %+v", v, out)
		}
	}
}
