package sec

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func findingKeys(r sdk.Result) []string {
	var ks []string
	for _, f := range r.Findings {
		ks = append(ks, f.Key)
	}
	sort.Strings(ks)
	return ks
}

// Compiled goldens/sec014.bicep (Bicep CLI 0.48.1).
func TestSEC014CompiledGolden(t *testing.T) {
	out := run(t, "FND-SEC-014", &sdk.Input{ARM: rulestest.Golden(t, "sec014")})
	if out.Skipped != nil {
		t.Fatalf("skipped: %+v", out.Skipped)
	}
	want := []string{"k:list-function", "pw:secure-param", "viaVar:secure-param"}
	if got := findingKeys(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("finding keys = %v, want %v", got, want)
	}
	for _, f := range out.Findings {
		if f.Resource.Type != "ARM/output" || f.Evidence == "" {
			t.Errorf("bad finding %+v", f)
		}
		if strings.Contains(f.Evidence, "[parameters(") {
			t.Errorf("evidence echoes the expression: %q", f.Evidence)
		}
	}
	// apiKeyName is name-only: an uncertain skip, not a finding.
	if len(out.Skips) != 1 || !strings.Contains(out.Skips[0].Reason, "apiKeyName") || !strings.HasPrefix(out.Skips[0].Reason, "uncertain:") {
		t.Fatalf("skips = %+v, want one uncertain skip for apiKeyName", out.Skips)
	}
	// keyVaultUri, storageId, the literal text and the @secure() output
	// safePw produce neither a finding nor a skip.
	for _, name := range []string{"keyVaultUri", "storageId", "literalText", "safePw"} {
		for _, f := range out.Findings {
			if f.Resource.Name == name {
				t.Errorf("unexpected finding for %s", name)
			}
		}
		for _, s := range out.Skips {
			if strings.Contains(s.Reason, name) {
				t.Errorf("unexpected skip for %s: %s", name, s.Reason)
			}
		}
	}
}

// outModel is a hand-built root template for the expression variants.
type outModel struct {
	outs   []armmodel.OutputDetail
	secure []string
	vars   map[string]string
}

func (outModel) Resources() []sdk.ARMResource { return nil }
func (m outModel) OutputDetails() []armmodel.OutputDetail {
	return m.outs
}
func (m outModel) SecureParams() []string { return m.secure }
func (m outModel) VariableText(n string) (string, bool) {
	v, ok := m.vars[strings.ToLower(n)]
	return v, ok
}

func TestSEC014Variants(t *testing.T) {
	str := func(name, value string) armmodel.OutputDetail {
		return armmodel.OutputDetail{Name: name, Type: "string", Value: value}
	}
	tests := []struct {
		name   string
		m      outModel
		keys   []string
		skips  int
		reason string
	}{
		{name: "secure param in format", m: outModel{secure: []string{"adminPw"},
			outs: []armmodel.OutputDetail{str("o", "[format('x {0}', PARAMETERS('adminpw'))]")}},
			keys: []string{"o:secure-param"}},
		{name: "secureObject param in object output", m: outModel{secure: []string{"cfg"},
			outs: []armmodel.OutputDetail{{Name: "o", Type: "object", Value: `{"a":"[parameters('cfg').x]"}`}}},
			keys: []string{"o:secure-param"}},
		{name: "plain parameter passes", m: outModel{secure: []string{"s"},
			outs: []armmodel.OutputDetail{str("o", "[parameters('location')]")}}},
		{name: "list function case-insensitive", m: outModel{
			outs: []armmodel.OutputDetail{str("o", "[LISTAccountSas(resourceId('a/b','c'), '2023-05-01', createObject()).accountSasToken]")}},
			keys: []string{"o:list-function"}},
		{name: "list function name inside string literal", m: outModel{
			outs: []armmodel.OutputDetail{str("o", "[concat('see listKeys(x) and parameters(''s'')', 'y')]")}, secure: []string{"s"}}},
		{name: "member call with list prefix is not an ARM list function", m: outModel{
			outs: []armmodel.OutputDetail{str("o", "[ns.listThings()]")}}},
		{name: "unrelated function starting with list text only", m: outModel{
			outs: []armmodel.OutputDetail{str("o", "[list]")}}},
		{name: "escaped literal bracket", m: outModel{
			outs: []armmodel.OutputDetail{str("o", "[[listKeys(x)")}}},
		{name: "variable chain to secure param", m: outModel{secure: []string{"s"},
			vars: map[string]string{"a": `"[variables('b')]"`, "b": `"[parameters('s')]"`},
			outs: []armmodel.OutputDetail{str("o", "[variables('a')]")}},
			keys: []string{"o:secure-param"}},
		{name: "variable cycle terminates", m: outModel{
			vars: map[string]string{"a": `"[variables('b')]"`, "b": `"[variables('a')]"`},
			outs: []armmodel.OutputDetail{str("o", "[variables('a')]")}}},
		{name: "both kinds give two findings", m: outModel{secure: []string{"s"},
			outs: []armmodel.OutputDetail{str("o", "[concat(parameters('s'), listKeys('a','v').k)]")}},
			keys: []string{"o:list-function", "o:secure-param"}},
		{name: "secure output type is exempt", m: outModel{secure: []string{"s"},
			outs: []armmodel.OutputDetail{{Name: "passwordOut", Type: "securestring", Value: "[parameters('s')]"}}}},
		{name: "password name only is a finding", m: outModel{
			outs: []armmodel.OutputDetail{str("accountPassword", "x")}},
			keys: []string{"accountPassword:name"}},
		{name: "secret name only is uncertain", m: outModel{
			outs: []armmodel.OutputDetail{str("kv_secret_name", "x")}},
			skips: 1, reason: "kv_secret_name"},
		{name: "name plus secure param is one finding of the value kind", m: outModel{secure: []string{"s"},
			outs: []armmodel.OutputDetail{str("db"+"Password", "[parameters('s')]")}},
			keys: []string{"db" + "Password" + ":secure-param"}},
		{name: "no outputs", m: outModel{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, "FND-SEC-014", &sdk.Input{ARM: tc.m})
			if out.Skipped != nil {
				t.Fatalf("skipped: %+v", out.Skipped)
			}
			got := findingKeys(out)
			if len(got) != len(tc.keys) || (len(got) > 0 && !reflect.DeepEqual(got, tc.keys)) {
				t.Fatalf("keys = %v, want %v", got, tc.keys)
			}
			if len(out.Skips) != tc.skips {
				t.Fatalf("skips = %+v, want %d", out.Skips, tc.skips)
			}
			if tc.reason != "" && !strings.Contains(out.Skips[0].Reason, tc.reason) {
				t.Fatalf("skip %q lacks %q", out.Skips[0].Reason, tc.reason)
			}
		})
	}
}

func TestSEC014NameTokens(t *testing.T) {
	hit := map[string]string{
		"accountPassword": "password", "db_pwd": "pwd", "ADMIN-PASSWD": "passwd",
		"clientSecret": "secret", "sasToken": "token", "apiKey": "key", "primary_key": "key",
		"accessKey": "key", "sharedKey": "key", "storageKey": "key", "apiKeyName": "key",
		"connectionString": "connectionString", "APIKey": "key", "searchSecondaryKeys": "keys",
	}
	for n, tok := range hit {
		if got, _ := secretNameToken(n); got != tok {
			t.Errorf("%s: token %q, want %q", n, got, tok)
		}
	}
	for _, n := range []string{"keyVaultUri", "monkey", "tokenizerName", "keyVaultName", "storageId", "turkey", "passwordless", "keyboard", ""} {
		if got, _ := secretNameToken(n); got != "" {
			t.Errorf("%s must not match, got %q", n, got)
		}
	}
	if _, pw := secretNameToken("apiKey"); pw {
		t.Error("apiKey is not the password family")
	}
	if _, pw := secretNameToken("userPwd"); !pw {
		t.Error("userPwd is the password family")
	}
}

func TestSEC014NamesOnlyModel(t *testing.T) {
	// A model that only implements sdk.ARMOutputs: a password name is a
	// finding, and a clean set is a skip, not a pass.
	out := run(t, "FND-SEC-014", &sdk.Input{ARM: armOutStub{names: []string{"storageId"}}})
	if len(out.Findings) != 0 || len(out.Skips) != 1 || !strings.Contains(out.Skips[0].Reason, sdk.SkipInputUnavailable) {
		t.Fatalf("got %+v", out)
	}
}

func TestSEC014MissingInput(t *testing.T) {
	if out := run(t, "FND-SEC-014", &sdk.Input{}); out.Skipped == nil {
		t.Fatalf("no ARM model must skip, got %+v", out)
	}
}
