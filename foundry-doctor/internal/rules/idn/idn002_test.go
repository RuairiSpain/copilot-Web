package idn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model002 []sdk.ARMResource

func (m model002) Resources() []sdk.ARMResource { return m }

const (
	gSearchIndex   = "8ebe5a00-799e-43f5-93ac-243d3dce84a7"
	gSearchService = "7ca78c08-252a-4471-8644-bb5ff32d4ba0"
	gBlobContrib   = "ba92f5b4-2d11-453d-a403-e96b0029c9fe"
	gBlobOwner     = "b7e6dc6d-f1e8-4753-8033-0f276bb0955b"
	gCosmosOp      = "230815da-be43-4aae-9cb4-875f7bd000aa"
	gAcctContrib   = "17d1049b-9a84-46fb-8f53-869881c3d3ab"
)

const (
	sysPrincipal   = "[reference(resourceId('Microsoft.CognitiveServices/accounts/projects', 'acct', 'proj'), '2025-06-01', 'full').identity.principalId]"
	otherPrincipal = "[reference(resourceId('Microsoft.CognitiveServices/accounts/projects', 'acct', 'other'), '2025-06-01', 'full').identity.principalId]"
	uaiPrincipal   = "[reference(resourceId('Microsoft.ManagedIdentity/userAssignedIdentities', 'uai'), '2023-01-31').principalId]"
	uaiKey         = "[resourceId('Microsoft.ManagedIdentity/userAssignedIdentities', 'uai')]"
)

func proj(name string, identity map[string]any) sdk.ARMResource {
	return sdk.ARMResource{Type: typeProjects, Name: "acct/" + name, Identity: identity, Properties: props{}}
}

func sysID() map[string]any { return map[string]any{"type": "SystemAssigned"} }

func uaiID() map[string]any {
	return map[string]any{"type": "UserAssigned", "userAssignedIdentities": map[string]any{uaiKey: map[string]any{}}}
}

func conn(project, target string) sdk.ARMResource {
	return sdk.ARMResource{Type: typeProjects + "/connections", Name: "acct/" + project + "/c-" + strings.ReplaceAll(strings.ReplaceAll(target, "https://", ""), ".", "-"), Properties: props{"target": target}}
}

func target(typ, name string) sdk.ARMResource {
	return sdk.ARMResource{Type: typ, Name: name, Properties: props{}}
}

func roleOn(typ, name, role, principal string) sdk.ARMResource {
	return sdk.ARMResource{
		Type:  typeRoleAssign,
		Name:  "ra-" + name + "-" + role[:4],
		Scope: "[resourceId('" + typ + "', '" + name + "')]",
		Properties: props{
			"roleDefinitionId": "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '" + role + "')]",
			"principalId":      principal,
		},
	}
}

func searchRoles(principal string) []sdk.ARMResource {
	return []sdk.ARMResource{
		roleOn(typeSearch, "srch", gSearchIndex, principal),
		roleOn(typeSearch, "srch", gSearchService, principal),
	}
}

func storageRoles(principal string) []sdk.ARMResource {
	return []sdk.ARMResource{
		roleOn(typeStorage, "st", gBlobContrib, principal),
		roleOn(typeStorage, "st", gBlobOwner, principal),
	}
}

func join(parts ...[]sdk.ARMResource) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

const (
	searchHost  = "https://srch.search.windows.net"
	storageHost = "https://st.blob.core.windows.net"
	cosmosHost  = "https://db.documents.azure.com"
	kvHost      = "https://kv.vault.azure.net"
)

func hasSkip(out sdk.Result, prefix string) bool {
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

func TestIDN002(t *testing.T) {
	acct := sdk.ARMResource{Type: typeAccounts, Name: "acct", Kind: "AIServices", Properties: props{}}
	unrelated := roleOn(typeStorage, "elsewhere", gBlobContrib, otherPrincipal)
	cases := []struct {
		name     string
		rs       []sdk.ARMResource
		findings []string // substrings, one per expected finding
		skipHead string   // Skipped reason head; "" = not skipped
		skips    string   // expected Skips prefix
	}{
		// positive: missing role
		{name: "search linked with no role assignment for it",
			rs:       join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch"), unrelated}),
			findings: []string{"Search Index Data Contributor (" + gSearchIndex, "Search Service Contributor (" + gSearchService}},
		{name: "search with Index Data Contributor only",
			rs:       join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch"), roleOn(typeSearch, "srch", gSearchIndex, sysPrincipal)}),
			findings: []string{"Search Service Contributor"}},
		{name: "storage linked without blob roles",
			rs:       join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", storageHost), target(typeStorage, "st"), unrelated}),
			findings: []string{"Storage Blob Data Contributor", "Storage Blob Data Owner"}},
		{name: "cosmos linked without Cosmos DB Operator",
			rs:       join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", cosmosHost), target(typeCosmos, "db"), unrelated}),
			findings: []string{"Cosmos DB Operator"}},
		{name: "role granted to another project does not count",
			rs:       join([]sdk.ARMResource{acct, proj("proj", sysID()), proj("other", sysID()), conn("proj", searchHost), target(typeSearch, "srch")}, searchRoles(otherPrincipal)),
			findings: []string{"Search Index Data Contributor", "Search Service Contributor"}},
		{name: "role on a different store does not count",
			rs: join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch"), target(typeSearch, "srch2")},
				[]sdk.ARMResource{roleOn(typeSearch, "srch2", gSearchIndex, sysPrincipal), roleOn(typeSearch, "srch2", gSearchService, sysPrincipal)}),
			findings: []string{"Search Index Data Contributor", "Search Service Contributor"}},
		{name: "literal principal GUID cannot be a system-assigned identity",
			rs:       join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch")}, searchRoles("11111111-1111-1111-1111-111111111111")),
			findings: []string{"Search Index Data Contributor", "Search Service Contributor"}},

		// negative: roles present
		{name: "system-assigned identity holds both search roles",
			rs: join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch")}, searchRoles(sysPrincipal))},
		{name: "user-assigned identity holds both search roles",
			rs: join([]sdk.ARMResource{acct, proj("proj", uaiID()), conn("proj", searchHost), target(typeSearch, "srch")}, searchRoles(uaiPrincipal))},
		{name: "storage roles present (account contributor missing is only a note)",
			rs:    join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", storageHost), target(typeStorage, "st")}, storageRoles(sysPrincipal)),
			skips: sdk.SkipNotApplicable},
		{name: "roles at resource group scope cover the store",
			rs: []sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch"),
				{Type: typeRoleAssign, Name: "r1", Scope: "[resourceGroup().id]", Properties: props{"principalId": sysPrincipal, "roleDefinitionId": "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '" + gSearchIndex + "')]"}},
				{Type: typeRoleAssign, Name: "r2", Properties: props{"principalId": sysPrincipal, "roleDefinitionId": "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '" + gSearchService + "')]"}}}},
		{name: "account-level connection applies to the project",
			rs: join([]sdk.ARMResource{acct, proj("proj", sysID()), {Type: typeAccounts + "/connections", Name: "acct/shared", Properties: props{"target": searchHost}}, target(typeSearch, "srch")}, searchRoles(sysPrincipal))},
		{name: "store linked only to another project is not evaluated for this one",
			rs: join([]sdk.ARMResource{acct, proj("proj", sysID()), proj("other", sysID()), conn("other", searchHost), target(typeSearch, "srch")}, searchRoles(otherPrincipal))},
		{name: "key vault linked has no verified role requirement",
			rs:    []sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", kvHost), target(typeKeyVault, "kv"), unrelated},
			skips: sdk.SkipNotApplicable},
		{name: "unlinked store is not applicable",
			rs:       []sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", "https://other.search.windows.net"), target(typeSearch, "srch"), unrelated},
			skipHead: sdk.SkipNotApplicable},

		// uncertain
		{name: "principalId is a parameter",
			rs:       join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch")}, searchRoles("[parameters('principalId')]")),
			skipHead: SkipUnresolved},
		{name: "role assigned on a container of the storage account",
			rs: []sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", storageHost), target(typeStorage, "st"),
				{Type: typeRoleAssign, Name: "c1", Scope: "[resourceId('Microsoft.Storage/storageAccounts/blobServices/containers', 'st', 'default', 'agents')]", Properties: props{"principalId": sysPrincipal, "roleDefinitionId": "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '" + gBlobOwner + "')]"}},
				roleOn(typeStorage, "st", gBlobContrib, sysPrincipal)},
			skipHead: SkipUnresolved},
		{name: "matching assignment is conditional",
			rs: join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch"), func() sdk.ARMResource {
				r := roleOn(typeSearch, "srch", gSearchIndex, sysPrincipal)
				r.Conditional, r.Condition = true, "[parameters('assign')]"
				return r
			}(), roleOn(typeSearch, "srch", gSearchService, sysPrincipal)}),
			skipHead: SkipUnresolved},
		{name: "role definition is an unresolved variable",
			rs: join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch"),
				{Type: typeRoleAssign, Name: "v", Scope: "[resourceId('Microsoft.Search/searchServices', 'srch')]", Properties: props{"principalId": sysPrincipal, "roleDefinitionId": "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', variables('role'))]"}}}),
			skipHead: SkipUnresolved},
		{name: "role scope is an expression that cannot be classified",
			rs: join([]sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch"),
				{Type: typeRoleAssign, Name: "s", Scope: "[parameters('scope')]", Properties: props{"principalId": sysPrincipal, "roleDefinitionId": "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '" + gSearchIndex + "')]"}},
				roleOn(typeSearch, "srch", gSearchService, sysPrincipal)}),
			skipHead: SkipUnresolved},
		{name: "cosmos operator present but SQL data role is unverified",
			rs:       []sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", cosmosHost), target(typeCosmos, "db"), roleOn(typeCosmos, "db", gCosmosOp, sysPrincipal)},
			skipHead: SkipUnresolved},
		{name: "connection target is a parameter so the store link is uncertain",
			rs:       []sdk.ARMResource{acct, proj("proj", sysID()), {Type: typeProjects + "/connections", Name: "acct/proj/c", Properties: props{"target": "[parameters('endpoint')]"}}, target(typeSearch, "srch"), unrelated},
			skipHead: "scope-uncertain"},
		{name: "project identity type is an expression",
			rs:       []sdk.ARMResource{acct, proj("proj", map[string]any{"type": "[parameters('t')]"}), conn("proj", searchHost), target(typeSearch, "srch"), unrelated},
			skipHead: SkipUnresolved},

		// skipped
		{name: "no role assignment resource in the template",
			rs:       []sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch")},
			skipHead: sdk.SkipInputUnavailable},
		{name: "project without identity is left to FND-IDN-001",
			rs:       []sdk.ARMResource{acct, proj("proj", nil), conn("proj", searchHost), target(typeSearch, "srch"), unrelated},
			skipHead: sdk.SkipNotApplicable},
		{name: "no Foundry project or account",
			rs:       []sdk.ARMResource{target(typeSearch, "srch"), unrelated},
			skipHead: sdk.SkipNotApplicable},
		{name: "store deployed under a condition",
			rs: []sdk.ARMResource{acct, proj("proj", sysID()), conn("proj", searchHost), func() sdk.ARMResource {
				r := target(typeSearch, "srch")
				r.Conditional, r.Condition = true, "[parameters('deploySearch')]"
				return r
			}(), unrelated},
			skipHead: "conditional-resource"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, "FND-IDN-002", &sdk.Input{ARM: model002(tc.rs)})
			if len(out.Findings) != len(tc.findings) {
				t.Fatalf("findings=%d want %d: %+v skip=%+v skips=%+v", len(out.Findings), len(tc.findings), out.Findings, out.Skipped, out.Skips)
			}
			text := ""
			for _, f := range out.Findings {
				text += f.Evidence + "\n"
				if f.Key == "" {
					t.Errorf("finding without Key: %+v", f)
				}
				if !strings.Contains(f.Evidence, "proj") {
					t.Errorf("evidence must name the project: %q", f.Evidence)
				}
			}
			for _, want := range tc.findings {
				if !strings.Contains(text, want) {
					t.Errorf("evidence lacks %q:\n%s", want, text)
				}
			}
			if tc.skipHead == "" && out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if tc.skipHead != "" && (out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, tc.skipHead)) {
				t.Fatalf("want skip %q got %+v", tc.skipHead, out.Skipped)
			}
			if tc.skips != "" && !hasSkip(out, tc.skips) {
				t.Errorf("want a %q note, got %+v", tc.skips, out.Skips)
			}
		})
	}
}

func TestIDN002MissingCapabilityNamed(t *testing.T) {
	rs := []sdk.ARMResource{
		{Type: typeAccounts, Name: "acct", Kind: "AIServices", Properties: props{}},
		proj("proj", sysID()), conn("proj", searchHost), target(typeSearch, "srch"),
	}
	out := run(t, "FND-IDN-002", &sdk.Input{ARM: model002(rs)})
	if out.Skipped == nil || out.Skipped.MissingCapability == "" || !strings.Contains(out.Skipped.MissingCapability, "roleAssignments") {
		t.Fatalf("skip must name the missing capability: %+v", out.Skipped)
	}
}

func TestIDN002AccountIdentityWithCapabilitySettings(t *testing.T) {
	acct := sdk.ARMResource{Type: typeAccounts, Name: "acct", Kind: "AIServices", Identity: sysID(),
		Properties: props{"capabilitySettings": props{"vectorStore": "https://srch.search.windows.net"}}}
	acctPrincipal := "[reference(resourceId('Microsoft.CognitiveServices/accounts', 'acct'), '2025-06-01', 'full').identity.principalId]"
	rs := []sdk.ARMResource{acct, target(typeSearch, "srch"), roleOn(typeStorage, "elsewhere", gBlobContrib, acctPrincipal)}
	out := run(t, "FND-IDN-002", &sdk.Input{ARM: model002(rs)})
	if len(out.Findings) != 2 || !strings.Contains(out.Findings[0].Evidence, "account acct") {
		t.Fatalf("want two findings on the account identity, got %+v", out)
	}
	out = run(t, "FND-IDN-002", &sdk.Input{ARM: model002(append(rs, searchRoles(acctPrincipal)...))})
	if out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("want pass, got %+v", out)
	}
}

func TestIDN002EvidenceHasNoSecrets(t *testing.T) {
	rs := []sdk.ARMResource{
		{Type: typeAccounts, Name: "acct", Kind: "AIServices", Properties: props{}},
		proj("proj", sysID()),
		{Type: typeProjects + "/connections", Name: "acct/proj/c", Properties: props{"target": searchHost, "credentials": props{"key": "super-secret-value"}}},
		target(typeSearch, "srch"), roleOn(typeStorage, "x", gBlobContrib, otherPrincipal),
	}
	out := run(t, "FND-IDN-002", &sdk.Input{ARM: model002(rs)})
	for _, f := range out.Findings {
		if strings.Contains(f.Evidence, "super-secret-value") {
			t.Fatalf("secret leaked into evidence: %q", f.Evidence)
		}
	}
}

func loadCompiled(t *testing.T, name string) sdk.ARMModel {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "idn002", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := armmodel.FromARM(b, armmodel.Options{Entry: name + ".bicep"})
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return m
}

// TestIDN002CompiledBicep runs the rule over ARM JSON compiled from the Bicep
// files in testdata/idn002 (real compiled expression shapes).
func TestIDN002CompiledBicep(t *testing.T) {
	t.Run("system-assigned identity with every listed role passes", func(t *testing.T) {
		out := run(t, "FND-IDN-002", &sdk.Input{ARM: loadCompiled(t, "good")})
		if out.Skipped != nil || len(out.Findings) != 0 {
			t.Fatalf("want pass, got %+v", out)
		}
	})
	t.Run("user-assigned identity missing Search Service Contributor", func(t *testing.T) {
		out := run(t, "FND-IDN-002", &sdk.Input{ARM: loadCompiled(t, "uai-missing")})
		if out.Skipped != nil || len(out.Findings) != 1 {
			t.Fatalf("want one finding, got %+v", out)
		}
		ev := out.Findings[0].Evidence
		for _, want := range []string{"Search Service Contributor", gSearchService, "proj", "user-assigned"} {
			if !strings.Contains(ev, want) {
				t.Errorf("evidence %q lacks %q", ev, want)
			}
		}
	})
}
