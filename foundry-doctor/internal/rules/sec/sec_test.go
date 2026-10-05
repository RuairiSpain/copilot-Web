package sec

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return m }

type props = map[string]any

func res(t, n string, p props) sdk.ARMResource {
	return sdk.ARMResource{Type: t, Name: n, Properties: p}
}

func run(t *testing.T, id string, in *sdk.Input) sdk.Result {
	t.Helper()
	for _, r := range Register() {
		if r.ID() == id {
			out, err := r.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			return out
		}
	}
	t.Fatalf("rule %s not registered", id)
	return sdk.Result{}
}

func TestRegisterIDs(t *testing.T) {
	want := []string{"FND-SEC-001", "FND-SEC-002", "FND-SEC-003", "FND-SEC-004", "FND-SEC-006"}
	got := Register()
	if len(got) != len(want) {
		t.Fatalf("got %d rules", len(got))
	}
	for i, r := range got {
		if r.ID() != want[i] {
			t.Errorf("rule %d = %s want %s", i, r.ID(), want[i])
		}
	}
}

func TestNilInputsSkip(t *testing.T) {
	for _, r := range Register() {
		for name, in := range map[string]*sdk.Input{"nil input": nil, "nil ARM": {}} {
			out, err := r.Evaluate(context.Background(), in)
			if err != nil || out.Skipped == nil || out.Skipped.Reason != sdk.SkipInputUnavailable || len(out.Findings) != 0 {
				t.Errorf("%s %s: got %+v err=%v", r.ID(), name, out, err)
			}
		}
	}
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Register()[0].Evaluate(ctx, &sdk.Input{ARM: model{}}); err == nil {
		t.Fatal("want error")
	}
}

type tc struct {
	name     string
	rule     string
	res      []sdk.ARMResource
	findings int
	skip     string // "" = not skipped
	contains string
}

func TestRules(t *testing.T) {
	proj := res(typeAcctProject, "acct/proj", nil)
	conn := func(target string) sdk.ARMResource {
		return res(typeAccounts+"/connections", "acct/c", props{"target": target})
	}
	cases := []tc{
		// SEC-001
		{name: "001 key auth enabled", rule: "FND-SEC-001", res: []sdk.ARMResource{res(typeAccounts, "acct", props{"disableLocalAuth": false}), proj}, findings: 1},
		{name: "001 absent", rule: "FND-SEC-001", res: []sdk.ARMResource{res(typeAccounts, "acct", props{}), proj}, findings: 1, contains: "absent"},
		{name: "001 disabled passes", rule: "FND-SEC-001", res: []sdk.ARMResource{res(typeAccounts, "acct", props{"disableLocalAuth": true}), proj}},
		{name: "001 expression skips", rule: "FND-SEC-001", res: []sdk.ARMResource{res(typeAccounts, "acct", props{"disableLocalAuth": "[parameters('d')]"}), proj}, skip: SkipUnresolved},
		{name: "001 no children not in scope", rule: "FND-SEC-001", res: []sdk.ARMResource{res(typeAccounts, "acct", props{})}, skip: sdk.SkipInputUnavailable},
		{name: "001 child of other account", rule: "FND-SEC-001", res: []sdk.ARMResource{res(typeAccounts, "a", props{}), res(typeAccounts, "b", props{"disableLocalAuth": true}), res(typeAcctDeploy, "b/d", nil)}},
		{name: "001 case-insensitive type", rule: "FND-SEC-001", res: []sdk.ARMResource{res("microsoft.cognitiveservices/ACCOUNTS", "acct", props{}), res(typeAcctDeploy, "acct/d", nil)}, findings: 1},
		// SEC-002
		{name: "002 apiKeyOnly", rule: "FND-SEC-002", res: []sdk.ARMResource{res(typeSearch, "srch", props{"authOptions": props{"apiKeyOnly": props{}}}), conn("https://srch.search.windows.net")}, findings: 1, contains: "apiKeyOnly"},
		{name: "002 aadOrApiKey", rule: "FND-SEC-002", res: []sdk.ARMResource{res(typeSearch, "srch", props{"authOptions": props{"aadOrApiKey": props{"aadAuthFailureMode": "http401WithBearerChallenge"}}}), conn("https://srch.search.windows.net")}, findings: 1, contains: "http401WithBearerChallenge"},
		{name: "002 neither", rule: "FND-SEC-002", res: []sdk.ARMResource{res(typeSearch, "srch", props{}), conn("srch")}, findings: 1, contains: "neither"},
		{name: "002 disabled", rule: "FND-SEC-002", res: []sdk.ARMResource{res(typeSearch, "srch", props{"disableLocalAuth": true}), conn("srch")}},
		{name: "002 out of scope", rule: "FND-SEC-002", res: []sdk.ARMResource{res(typeSearch, "srch", props{})}, skip: SkipOutOfScope},
		{name: "002 unrelated connection", rule: "FND-SEC-002", res: []sdk.ARMResource{res(typeSearch, "srch", props{}), conn("other")}, skip: SkipOutOfScope},
		{name: "002 expression", rule: "FND-SEC-002", res: []sdk.ARMResource{res(typeSearch, "srch", props{"disableLocalAuth": "[x]"}), conn("srch")}, skip: SkipUnresolved},
		{name: "002 no search", rule: "FND-SEC-002", res: []sdk.ARMResource{}, skip: sdk.SkipInputUnavailable},
		// SEC-003
		{name: "003 cosmos keys", rule: "FND-SEC-003", res: []sdk.ARMResource{res(typeCosmos, "db", props{}), conn("db")}, findings: 1},
		{name: "003 disabled", rule: "FND-SEC-003", res: []sdk.ARMResource{res(typeCosmos, "db", props{"disableLocalAuth": true}), conn("db")}},
		{name: "003 out of scope", rule: "FND-SEC-003", res: []sdk.ARMResource{res(typeCosmos, "db", props{})}, skip: SkipOutOfScope},
		{name: "003 expression", rule: "FND-SEC-003", res: []sdk.ARMResource{res(typeCosmos, "db", props{"disableLocalAuth": "[x]"}), conn("db")}, skip: SkipUnresolved},
		// SEC-004
		{name: "004 all four", rule: "FND-SEC-004", res: []sdk.ARMResource{res(typeStorage, "st", props{"allowBlobPublicAccess": true, "supportsHttpsTrafficOnly": false, "minimumTlsVersion": "TLS1_0"})}, findings: 4},
		{name: "004 hardened", rule: "FND-SEC-004", res: []sdk.ARMResource{res(typeStorage, "st", props{"allowSharedKeyAccess": false, "allowBlobPublicAccess": false, "supportsHttpsTrafficOnly": true, "minimumTlsVersion": "TLS1_2"})}},
		{name: "004 tls absent", rule: "FND-SEC-004", res: []sdk.ARMResource{res(typeStorage, "st", props{"allowSharedKeyAccess": false})}, findings: 1, contains: "minimumTlsVersion"},
		{name: "004 tls1_1", rule: "FND-SEC-004", res: []sdk.ARMResource{res(typeStorage, "st", props{"allowSharedKeyAccess": false, "minimumTlsVersion": "tls1_1"})}, findings: 1},
		{name: "004 expressions skip", rule: "FND-SEC-004", res: []sdk.ARMResource{res(typeStorage, "st", props{"allowSharedKeyAccess": "[p]", "allowBlobPublicAccess": "[p]", "supportsHttpsTrafficOnly": "[p]", "minimumTlsVersion": "[p]"})}, skip: SkipUnresolved},
		{name: "004 none", rule: "FND-SEC-004", res: []sdk.ARMResource{}, skip: sdk.SkipInputUnavailable},
		// SEC-006
		{name: "006 storage open", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeStorage, "st", props{"networkAcls": props{"ipRules": []any{props{"value": "0.0.0.0/0"}}}})}, findings: 1, contains: "all addresses"},
		{name: "006 storage private", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeStorage, "st", props{"networkAcls": props{"ipRules": []any{props{"value": "10.1.0.0/16"}, props{"value": "203.0.113.4"}}}})}, findings: 1, contains: "private"},
		{name: "006 cosmos open", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeCosmos, "db", props{"ipRules": []any{props{"ipAddressOrRange": "0.0.0.0"}}})}, findings: 1},
		{name: "006 cosmos cgnat", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeCosmos, "db", props{"ipRules": []any{props{"ipAddressOrRange": "100.64.1.1"}}})}, findings: 1},
		{name: "006 keyvault private not evaluated", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeKeyVault, "kv", props{"networkAcls": props{"ipRules": []any{props{"value": "10.0.0.0/8"}}}})}},
		{name: "006 search open", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeSearch, "s", props{"networkRuleSet": props{"ipRules": []any{props{"value": "0.0.0.0/0"}}}})}, findings: 1},
		{name: "006 cogsvc open", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeAccounts, "a", props{"networkAcls": props{"ipRules": []any{props{"value": "0.0.0.0/0"}}}})}, findings: 1},
		{name: "006 public ip ok", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeStorage, "st", props{"networkAcls": props{"ipRules": []any{props{"value": "203.0.113.0/24"}}}})}},
		{name: "006 param array skips", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeStorage, "st", props{"networkAcls": props{"ipRules": "[parameters('rules')]"}})}, skip: SkipUnresolved},
		{name: "006 param value skips", rule: "FND-SEC-006", res: []sdk.ARMResource{res(typeStorage, "st", props{"networkAcls": props{"ipRules": []any{props{"value": "[parameters('ip')]"}}}})}, skip: SkipUnresolved},
		{name: "006 no applicable", rule: "FND-SEC-006", res: []sdk.ARMResource{res("Microsoft.Web/sites", "w", nil)}, skip: sdk.SkipInputUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := run(t, c.rule, &sdk.Input{ARM: model(c.res)})
			if c.skip != "" {
				if out.Skipped == nil || out.Skipped.Reason != c.skip || len(out.Findings) != 0 {
					t.Fatalf("want skip %q, got %+v", c.skip, out)
				}
				return
			}
			if out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if len(out.Findings) != c.findings {
				t.Fatalf("findings = %d want %d: %+v", len(out.Findings), c.findings, out.Findings)
			}
			if c.contains != "" {
				ok := false
				for _, f := range out.Findings {
					ok = ok || strings.Contains(f.Evidence, c.contains)
				}
				if !ok {
					t.Errorf("no evidence contains %q: %+v", c.contains, out.Findings)
				}
			}
			for _, f := range out.Findings {
				if f.Resource.Type == "" || f.Resource.Name == "" || f.Evidence == "" {
					t.Errorf("incomplete finding %+v", f)
				}
			}
		})
	}
}
