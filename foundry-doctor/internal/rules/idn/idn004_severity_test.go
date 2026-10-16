package idn

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// P1/P1b: Contributor at a single-resource scope is info only for a Foundry
// account or project whose account has disableLocalAuth true.
func TestCompiledIDN004ContributorSeverityByTarget(t *testing.T) {
	out := run(t, "FND-IDN-004", &sdk.Input{ARM: rulestest.Golden(t, "idn004res")})
	if out.Skipped != nil {
		t.Fatalf("skipped: %+v", out.Skipped)
	}
	want := map[string]sdk.Severity{
		"locked": sdk.SeverityInfo,    // Foundry account, disableLocalAuth true
		"open":   sdk.SeverityWarning, // Foundry account, local auth not disabled
		"stg":    "",                  // Storage: catalogue severity
		"kv":     "",                  // Key Vault: catalogue severity
	}
	seen := map[string]bool{}
	for _, f := range out.Findings {
		var key string
		switch {
		case strings.Contains(f.Evidence, "'locked'"):
			key = "locked"
		case strings.Contains(f.Evidence, "'open'"):
			key = "open"
		case strings.Contains(f.Evidence, "storageAccounts"):
			key = "stg"
		case strings.Contains(f.Evidence, "vaults"):
			key = "kv"
		default:
			t.Fatalf("unexpected finding %q", f.Evidence)
		}
		seen[key] = true
		if f.Severity != want[key] {
			t.Errorf("%s: severity override %q want %q (%s)", key, f.Severity, want[key], f.Evidence)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("want a finding for each target, got %v", seen)
	}
}

func TestIDN004ContributorProjectScopeAndMissingAccount(t *testing.T) {
	sys := props{"type": "SystemAssigned", "principalId": guidA}
	acct := func(name string, p props) sdk.ARMResource {
		return sdk.ARMResource{Type: typeAccounts, Name: name, Identity: sys, Properties: p}
	}
	projScope := "[resourceId('Microsoft.CognitiveServices/accounts/projects', 'a', 'p')]"
	accScope := "[resourceId('Microsoft.CognitiveServices/accounts', 'a')]"
	for _, tc := range []struct {
		name  string
		res   []sdk.ARMResource
		scope string
		want  sdk.Severity
	}{
		{"project of locked account", []sdk.ARMResource{acct("a", props{"disableLocalAuth": true})}, projScope, sdk.SeverityInfo},
		{"project of open account", []sdk.ARMResource{acct("a", props{})}, projScope, sdk.SeverityWarning},
		{"project of account not in model", []sdk.ARMResource{acct("other", props{"disableLocalAuth": true}), acct("a2", props{})}, projScope, sdk.SeverityWarning},
		{"account with expression disableLocalAuth", []sdk.ARMResource{acct("a", props{"disableLocalAuth": "[parameters('x')]"})}, accScope, sdk.SeverityWarning},
		{"account with disableLocalAuth false", []sdk.ARMResource{acct("a", props{"disableLocalAuth": false})}, accScope, sdk.SeverityWarning},
		{"search service", []sdk.ARMResource{acct("a", props{"disableLocalAuth": true})}, "[resourceId('Microsoft.Search/searchServices', 'a')]", ""},
		{"cosmos account", []sdk.ARMResource{acct("a", props{"disableLocalAuth": true})}, "[resourceId('Microsoft.DocumentDB/databaseAccounts', 'a')]", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := append(append([]sdk.ARMResource(nil), tc.res...), ra(props{
				"roleDefinitionId": rdef(roleContributor), "principalId": guidA, "principalType": "ServicePrincipal",
			}, tc.scope, ""))
			out := run(t, "FND-IDN-004", &sdk.Input{ARM: model(res)})
			if len(out.Findings) != 1 {
				t.Fatalf("%+v", out)
			}
			if got := out.Findings[0].Severity; got != tc.want {
				t.Errorf("severity override %q want %q: %s", got, tc.want, out.Findings[0].Evidence)
			}
		})
	}
}

// P6: a name-only client ID match is a warning with likely confidence.
func TestIDN003NameOnlyMatchIsWarning(t *testing.T) {
	mk := func(expr string) sdk.Result {
		return run(t, "FND-IDN-003", &sdk.Input{ARM: model{ra(props{
			"roleDefinitionId": rdef(roleOwner), "principalType": "ServicePrincipal", "principalId": expr,
		}, rg, "2022-04-01")}})
	}
	name := mk("[parameters('uamiClientId')]")
	if len(name.Findings) != 1 || name.Findings[0].Severity != sdk.SeverityWarning || name.Findings[0].Confidence != sdk.ConfidenceMedium {
		t.Fatalf("name-only match: %+v", name.Findings)
	}
	prop := mk("[reference(resourceId('Microsoft.ManagedIdentity/userAssignedIdentities', 'u'), '2023-01-31').clientId]")
	if len(prop.Findings) != 1 || prop.Findings[0].Severity != "" || prop.Findings[0].Confidence != "" {
		t.Fatalf("property access keeps the catalogue severity and confidence: %+v", prop.Findings)
	}
}
