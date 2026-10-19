package cfg

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// armPropStub returns one resource with the given properties.
// Fake values are assembled at run time so the repository secret scan does
// not see credential-shaped literals in source.
var (
	fakeCred  = "Tr0ub4" + "dor3xample"
	fakeCred2 = "Tr0ub4" + "dor-3xample"
	fakeKey   = "a1b2c3" + "d4e5f6g7"
)

type armPropStub struct{ props map[string]any }

func (a armPropStub) Resources() []sdk.ARMResource {
	return []sdk.ARMResource{{Type: "Microsoft.Example/things", Name: "thing1", Properties: a.props}}
}

// FND-CFG-005 version 5.
func TestCFG005EnvNameLiteral(t *testing.T) {
	env := func(k, v string) string {
		return "services:\n  a:\n    host: azure.ai.agent\n    env:\n      " + k + ": " + v + "\n"
	}
	runReviewCases(t, []reviewCase{
		{name: "password-name-with-strong-literal-is-finding", id: "FND-CFG-005", yaml: env("DB_PASSWORD", fakeCred2), want: 1, contains: "env.DB_PASSWORD"},
		{name: "client-secret-with-literal-is-finding", id: "FND-CFG-005", yaml: env("CLIENT_SECRET", "q8W3e9R2t7Y1u"), want: 1},
		{name: "api-key-pair-with-literal-is-finding", id: "FND-CFG-005", yaml: env("SERVICE_API_KEY", fakeKey), want: 1},
		{name: "default-literal-is-finding", id: "FND-CFG-005", yaml: env("DB_PASSWORD", "${DB_PW:-"+fakeCred+"}"), want: 1},
		{name: "reference-passes", id: "FND-CFG-005", yaml: env("DB_PASSWORD", "${DB_PASSWORD}")},
		{name: "empty-passes", id: "FND-CFG-005", yaml: env("DB_PASSWORD", "\"\"")},
		{name: "placeholder-is-not-a-finding", id: "FND-CFG-005", yaml: env("DB_PASSWORD", "changeme123"), skip: uncertainPrefix + "a: env.DB_PASSWORD looks secret-bearing by name only"},
		{name: "angle-bracket-placeholder-uncertain", id: "FND-CFG-005", yaml: env("DB_PASSWORD", "\"<your-password1>\""), skip: uncertainPrefix},
		{name: "short-plain-word-stays-uncertain", id: "FND-CFG-005", yaml: env("SERVICE_API_KEY", "plain"), skip: uncertainPrefix + "a: env.SERVICE_API_KEY looks secret-bearing by name only"},
		{name: "long-lowercase-word-stays-uncertain", id: "FND-CFG-005", yaml: env("DB_PASSWORD", "production"), skip: uncertainPrefix},
		{name: "partition-key-is-not-credential-named", id: "FND-CFG-005", yaml: env("COSMOS_PARTITION_KEY", "tenant-id-2024"), skip: uncertainPrefix},
		{name: "secret-name-suffix-is-not-credential-named", id: "FND-CFG-005", yaml: env("KEY_VAULT_SECRET_NAME", "my-secret-2024"), skip: uncertainPrefix},
		{name: "quantity-name-excluded", id: "FND-CFG-005", yaml: env("TOKEN_LIMIT", fakeCred)},
		{name: "url-value-is-not-a-literal-secret", id: "FND-CFG-005", yaml: env("AUTH_TOKEN", "https://login.example.com/oauth2/token1"), skip: uncertainPrefix},
		{name: "whitespace-sentence-is-not-a-secret", id: "FND-CFG-005", yaml: env("DB_PASSWORD", "\"set this in the portal 1\""), skip: uncertainPrefix},
	})
}

func TestCFG005EnvNameLiteralKeyAndRedaction(t *testing.T) {
	y := "services:\n  a:\n    host: azure.ai.agent\n    env:\n      DB_PASSWORD: " + fakeCred2 + "\n"
	res := run(t, find(Register(), "FND-CFG-005"), inputFor(t, y))
	if len(res.Findings) != 1 || res.Findings[0].Key != "env-secret-name-literal:DB_PASSWORD" {
		t.Fatalf("unexpected %+v", res.Findings)
	}
	if res.Findings[0].Confidence != sdk.ConfidenceMedium {
		t.Errorf("confidence = %q", res.Findings[0].Confidence)
	}
	if strings.Contains(res.Findings[0].Evidence, "Tr0ub4") {
		t.Errorf("evidence leaks the value: %q", res.Findings[0].Evidence)
	}
}

func TestCFG005NewURLKeys(t *testing.T) {
	conn := func(extra string) string {
		return "services:\n  c:\n    host: azure.ai.connection\n" + extra
	}
	runReviewCases(t, []reviewCase{
		{name: "tokenUrl-with-userinfo", id: "FND-CFG-005", yaml: conn("    tokenUrl: https://user:pw@login.example.com/token\n"), want: 1, contains: "tokenUrl"},
		{name: "authorizationUrl-clean-passes", id: "FND-CFG-005", yaml: conn("    authorizationUrl: https://login.example.com/authorize\n")},
	})
}

func TestCFG005ARMLiterals(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]any
		want  int
		key   string
	}{
		{"credential-shaped-literal", map[string]any{"settings": map[string]any{"conn": "Server=x;Pass" + "word=" + fakeCred}}, 1, "properties.settings.conn"},
		{"credential-named-property-literal", map[string]any{"administratorLoginPassword": fakeCred}, 1, "properties.administratorLoginPassword"},
		{"nested-array-literal", map[string]any{"items": []any{map[string]any{"apiKey": fakeKey}}}, 1, "properties.items[0].apiKey"},
		{"expression-is-not-literal", map[string]any{"administratorLoginPassword": "[parameters('pw')]"}, 0, ""},
		{"reference-is-not-literal", map[string]any{"apiKey": "${KEY}"}, 0, ""},
		{"placeholder-is-not-a-finding", map[string]any{"adminPassword": "changeme123"}, 0, ""},
		{"non-credential-property", map[string]any{"displayName": fakeCred}, 0, ""},
		{"key-vault-uri-is-not-credential", map[string]any{"keyVaultUri": "https://kv.vault.azure.net/"}, 0, ""},
		{"no-properties", nil, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := run(t, find(Register(), "FND-CFG-005"), &sdk.Input{AzureYAML: doc(t, "services: {}\n"), ARM: armPropStub{tc.props}})
			if len(res.Findings) != tc.want {
				t.Fatalf("findings = %+v want %d", res.Findings, tc.want)
			}
			if tc.want == 1 {
				f := res.Findings[0]
				if !strings.HasSuffix(f.Key, ":"+tc.key) || !strings.Contains(f.Evidence, tc.key) {
					t.Errorf("key/evidence = %q / %q", f.Key, f.Evidence)
				}
				if strings.Contains(f.Evidence, "Tr0ub4") || strings.Contains(f.Evidence, "a1b2c3") {
					t.Errorf("evidence leaks the value: %q", f.Evidence)
				}
				if f.Resource.Name != "thing1" {
					t.Errorf("resource = %+v", f.Resource)
				}
			}
		})
	}
	t.Run("arm-only-without-azure-yaml", func(t *testing.T) {
		res := run(t, find(Register(), "FND-CFG-005"), &sdk.Input{ARM: armPropStub{map[string]any{"adminPassword": fakeCred}}})
		if len(res.Findings) != 1 {
			t.Fatalf("got %+v", res)
		}
	})
	t.Run("no-inputs-still-skips", func(t *testing.T) {
		res := run(t, find(Register(), "FND-CFG-005"), &sdk.Input{})
		if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable) {
			t.Fatalf("got %+v", res)
		}
	})
}

func TestCFG005SelectedEnvironmentValues(t *testing.T) {
	y := "services:\n  a:\n    host: azure.ai.agent\n"
	withEnv := func(sel bool, vals map[string]string) Providers { return Providers{Env: fakeEnv{sel, vals}} }
	runReviewCases(t, []reviewCase{
		{name: "env-credential-shape-is-uncertain-not-finding", id: "FND-CFG-005", yaml: y,
			p: withEnv(true, map[string]string{"OPENAI_THING": "sk-abcdefghijklmnopqrstuvwxyz123456"}), skip: uncertainPrefix + "the selected azd environment holds a credential-like value under OPENAI_THING", notInSkip: "sk-abcdef"},
		{name: "env-credential-name-literal-is-uncertain", id: "FND-CFG-005", yaml: y,
			p: withEnv(true, map[string]string{"DB_PASSWORD": fakeCred}), skip: "under DB_PASSWORD", notInSkip: "Tr0ub4"},
		{name: "env-ordinary-values-pass", id: "FND-CFG-005", yaml: y,
			p: withEnv(true, map[string]string{"AZURE_LOCATION": "eastus", "AZURE_ENV_NAME": "dev"})},
		{name: "env-reference-values-pass", id: "FND-CFG-005", yaml: y,
			p: withEnv(true, map[string]string{"DB_PASSWORD": "${X}"})},
		{name: "unselected-environment-is-not-read", id: "FND-CFG-005", yaml: y,
			p: withEnv(false, map[string]string{"DB_PASSWORD": fakeCred})},
		{name: "no-environment-provider-passes", id: "FND-CFG-005", yaml: y},
	})
}

func TestPlausibleLiteralSecret(t *testing.T) {
	for v, want := range map[string]bool{
		fakeCred: true, "a1b2c3d4e5": true, "Abc!defGh": true,
		"short1": false, "": false, "production": false, "alllowercaseletters": false,
		"has space 12345": false, "$VAR12345": false, "[expr12345]": false, "your-key-1234": false,
		"https://example.com/x1": false, "TODO-set-me-1": false,
	} {
		if got := plausibleLiteralSecret(v); got != want {
			t.Errorf("plausibleLiteralSecret(%q) = %v want %v", v, got, want)
		}
	}
	for n, want := range map[string]bool{
		"DB_PASSWORD": true, "dbPassword": true, "API_KEY": true, "apiKey": true, "CONNECTION_STRING": true,
		"PARTITION_KEY": false, "KEY_VAULT_NAME": false, "SECRET_NAME": false, "TOKEN_URL": false, "": false, "KEY": false,
	} {
		if got := endsInCredentialWord(n); got != want {
			t.Errorf("endsInCredentialWord(%q) = %v want %v", n, got, want)
		}
	}
}
