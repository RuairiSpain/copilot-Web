package idn

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return m }

type props = map[string]any

const (
	guidA = "11111111-2222-3333-4444-555555555555"
	rg    = "/subscriptions/s1/resourceGroups/rg1"
	subsc = "/subscriptions/s1"
	resSc = rg + "/providers/Microsoft.CognitiveServices/accounts/acct"
)

const restrictiveCond = "((!(ActionMatches{'Microsoft.Authorization/roleAssignments/write'})) OR " +
	"(@Request[Microsoft.Authorization/roleAssignments:RoleDefinitionId] ForAnyOfAnyValues:GuidEquals {f6c7c914-8db3-469d-8ca1-694a8f32e121})) AND " +
	"((!(ActionMatches{'Microsoft.Authorization/roleAssignments/delete'})) OR " +
	"(@Resource[Microsoft.Authorization/roleAssignments:RoleDefinitionId] ForAnyOfAnyValues:GuidEquals {f6c7c914-8db3-469d-8ca1-694a8f32e121}))"

func condProps(role, cond string) props {
	return props{"roleDefinitionId": rdef(role), "principalId": guidA, "principalType": "ServicePrincipal", "condition": cond, "conditionVersion": "2.0"}
}

func rdef(g string) string {
	return "/subscriptions/s1/providers/Microsoft.Authorization/roleDefinitions/" + g
}

func ra(p props, scope, api string) sdk.ARMResource {
	return sdk.ARMResource{Type: typeRoleAssign, Name: "ra", Properties: p, Scope: scope, APIVersion: api}
}

func fic(iss, sub string) sdk.ARMResource {
	return sdk.ARMResource{Type: typeFederated, Name: "uai/f", Properties: props{"issuer": iss, "subject": sub}}
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
	want := []string{"FND-IDN-001", "FND-IDN-003", "FND-IDN-004", "FND-IDN-005", "FND-IDN-006"}
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

func TestRules(t *testing.T) {
	acct := func(id map[string]any) sdk.ARMResource {
		return sdk.ARMResource{Type: typeAccounts, Name: "acct", Identity: id}
	}
	app := func(id map[string]any, name, val string) sdk.ARMResource {
		return sdk.ARMResource{Type: typeContainer, Name: "app", Identity: id, Properties: props{
			"template": props{"containers": []any{props{"env": []any{props{"name": name, "value": val}}}}},
		}}
	}
	sp := func(role, pt string) props {
		p := props{"roleDefinitionId": rdef(role), "principalId": guidA}
		if pt != "" {
			p["principalType"] = pt
		}
		return p
	}
	sys := props{"type": "SystemAssigned"}
	appA := acct(props{"type": "SystemAssigned", "principalId": guidA})
	cases := []struct {
		name     string
		rule     string
		res      []sdk.ARMResource
		findings int
		skip     string
		contains string
	}{
		// IDN-001
		{name: "001 no identity", rule: "FND-IDN-001", res: []sdk.ARMResource{acct(nil)}, findings: 1, contains: "no identity"},
		{name: "001 None", rule: "FND-IDN-001", res: []sdk.ARMResource{acct(props{"type": "None"})}, findings: 1},
		{name: "001 system assigned", rule: "FND-IDN-001", res: []sdk.ARMResource{acct(sys)}},
		{name: "001 user assigned empty", rule: "FND-IDN-001", res: []sdk.ARMResource{acct(props{"type": "UserAssigned", "userAssignedIdentities": props{}})}, findings: 1},
		{name: "001 user assigned missing map", rule: "FND-IDN-001", res: []sdk.ARMResource{acct(props{"type": "UserAssigned"})}, findings: 1},
		{name: "001 user assigned ok", rule: "FND-IDN-001", res: []sdk.ARMResource{acct(props{"type": "UserAssigned", "userAssignedIdentities": props{"/id": props{}}})}},
		{name: "001 expression type", rule: "FND-IDN-001", res: []sdk.ARMResource{acct(props{"type": "[parameters('t')]"})}, skip: SkipUnresolved},
		{name: "001 expression map", rule: "FND-IDN-001", res: []sdk.ARMResource{acct(props{"type": "UserAssigned", "userAssignedIdentities": "[variables('u')]"})}, skip: SkipUnresolved},
		{name: "001 project", rule: "FND-IDN-001", res: []sdk.ARMResource{{Type: typeProjects, Name: "acct/p"}}, findings: 1},
		{name: "001 workload foundry no identity", rule: "FND-IDN-001", res: []sdk.ARMResource{app(nil, "AZURE_AI_PROJECT_ENDPOINT", "x")}, findings: 1},
		{name: "001 workload foundry value", rule: "FND-IDN-001", res: []sdk.ARMResource{app(nil, "X", "https://a.services.ai.azure.com/api")}, findings: 1},
		{name: "001 workload foundry identity ok", rule: "FND-IDN-001", res: []sdk.ARMResource{app(sys, "FOUNDRY_URL", "x")}},
		{name: "001 workload unrelated not evaluated", rule: "FND-IDN-001", res: []sdk.ARMResource{app(nil, "LOG", "debug")}, skip: sdk.SkipInputUnavailable},
		{name: "001 no resources", rule: "FND-IDN-001", res: nil, skip: sdk.SkipInputUnavailable},
		// IDN-003
		{name: "003 good", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": guidA, "principalType": "ServicePrincipal"}, rg, "2022-04-01")}},
		{name: "003 missing principalType", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": guidA}, rg, "2022-04-01")}, findings: 1, contains: "principalType"},
		{name: "003 non-guid principalId", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": "my-app", "principalType": "ServicePrincipal"}, rg, "2022-04-01")}, findings: 1, contains: "GUID"},
		{name: "003 missing principalId", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalType": "ServicePrincipal"}, rg, "2022-04-01")}, findings: 1},
		{name: "003 clientId expression", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": "[reference('x').clientId]", "principalType": "ServicePrincipal"}, rg, "2022-04-01")}, findings: 1, contains: "clientId"},
		{name: "003 parameter expression", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": "[parameters('p')]", "principalType": "ServicePrincipal"}, rg, "2022-04-01")}, skip: SkipUnresolved},
		{name: "003 principalType expression", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": guidA, "principalType": "[parameters('t')]"}, rg, "2022-04-01")}, skip: SkipUnresolved},
		{name: "003 old api with principalType", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": guidA, "principalType": "ServicePrincipal"}, rg, "2020-04-01-preview")}, findings: 1, contains: "apiVersion"},
		{name: "003 old api without principalType", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": guidA}, rg, "2020-04-01-preview")}, findings: 1},
		{name: "003 unparseable api ignored", rule: "FND-IDN-003", res: []sdk.ARMResource{ra(props{"principalId": guidA, "principalType": "User"}, rg, "weird")}},
		{name: "003 none", rule: "FND-IDN-003", res: []sdk.ARMResource{{Type: typeAccounts}}, skip: sdk.SkipInputUnavailable},
		// IDN-004
		{name: "004 owner subscription", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleOwner, "ServicePrincipal"), subsc, "")}, findings: 1, contains: "Owner"},
		{name: "004 contributor rg", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleContributor, "ServicePrincipal"), rg, "")}, findings: 1},
		{name: "004 mgmt group", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleUAA, "ServicePrincipal"), "/providers/Microsoft.Management/managementGroups/mg", "")}, findings: 1},
		{name: "004 owner resource scope", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleOwner, "ServicePrincipal"), resSc, "")}, findings: 1},
		{name: "004 rbac admin resource scope", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleRBACAdmin, "ServicePrincipal"), resSc, "")}, findings: 1},
		{name: "004 contributor resource scope passes", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleContributor, "ServicePrincipal"), resSc, "")}},
		{name: "004 human ignored", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleOwner, "User"), subsc, "")}},
		{name: "004 non-privileged role", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp("5e0bd9bd-7b93-4f28-af87-19fc36ad61bd", "ServicePrincipal"), subsc, "")}},
		{name: "004 missing principalType skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleOwner, ""), subsc, "")}, skip: SkipUnresolved},
		{name: "004 expression scope skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleOwner, "ServicePrincipal"), "[resourceGroup().id]", "")}, skip: SkipUnresolved},
		{name: "004 expression role skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(props{"roleDefinitionId": "[variables('r')]", "principalType": "ServicePrincipal"}, subsc, "")}, skip: SkipUnresolved},
		{name: "004 unknown scope shape skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleOwner, "ServicePrincipal"), "/weird", "")}, skip: SkipUnresolved},
		{name: "004 finding beats skip", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleOwner, ""), subsc, ""), ra(sp(roleOwner, "ServicePrincipal"), subsc, "")}, findings: 1},
		{name: "004 no role id", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(props{}, subsc, "")}, skip: sdk.SkipInputUnavailable},
		{name: "004 pipeline principal not flagged", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(props{"roleDefinitionId": rdef(roleContributor), "principalId": "99999999-2222-3333-4444-555555555555", "principalType": "ServicePrincipal"}, rg, "")}, skip: SkipUnresolved},
		{name: "004 pipeline principal alongside app clean", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleContributor, "ServicePrincipal"), rg, ""), ra(props{"roleDefinitionId": rdef(roleOwner), "principalId": "99999999-2222-3333-4444-555555555555", "principalType": "ServicePrincipal"}, rg, "")}, findings: 1},
		{name: "004 no app identities skips", rule: "FND-IDN-004", res: []sdk.ARMResource{ra(sp(roleOwner, "ServicePrincipal"), subsc, "")}, skip: SkipUnresolved},
		{name: "004 parameter principal skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(props{"roleDefinitionId": rdef(roleOwner), "principalId": "[parameters('p')]", "principalType": "ServicePrincipal"}, subsc, "")}, skip: SkipUnresolved},
		{name: "004 expression principal correlated to account", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, {Type: typeAccounts, Name: "acct", Identity: sys}, ra(props{"roleDefinitionId": rdef(roleOwner), "principalId": "[reference(resourceId('Microsoft.CognitiveServices/accounts', 'acct'), '2025-06-01', 'full').identity.principalId]", "principalType": "ServicePrincipal"}, subsc, "")}, findings: 1},
		{name: "004 expression principal other resource skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, {Type: typeAccounts, Name: "acct", Identity: sys}, ra(props{"roleDefinitionId": rdef(roleOwner), "principalId": "[reference(resourceId('Microsoft.Storage/storageAccounts', 'st'), '2023-01-01', 'full').identity.principalId]", "principalType": "ServicePrincipal"}, subsc, "")}, skip: SkipUnresolved},
		{name: "004 uaa no condition", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(sp(roleUAA, "ServicePrincipal"), rg, "")}, findings: 1},
		{name: "004 rbac admin restrictive condition", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(condProps(roleRBACAdmin, restrictiveCond), rg, "")}},
		{name: "004 uaa restrictive condition", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(condProps(roleUAA, restrictiveCond), subsc, "")}},
		{name: "004 rbac admin condition allows privileged role", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(condProps(roleRBACAdmin, strings.Replace(restrictiveCond, "f6c7c914-8db3-469d-8ca1-694a8f32e121", roleOwner, -1)), rg, "")}, findings: 1, contains: "ineffective"},
		{name: "004 rbac admin condition without guid set skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(condProps(roleRBACAdmin, "((!(ActionMatches{'Microsoft.Authorization/roleAssignments/write'})))"), rg, "")}, skip: SkipUnresolved},
		{name: "004 rbac admin write-only condition ineffective", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(condProps(roleRBACAdmin, "((!(ActionMatches{'Microsoft.Authorization/roleAssignments/write'})) OR (@Request[Microsoft.Authorization/roleAssignments:RoleDefinitionId] ForAnyOfAnyValues:GuidEquals {f6c7c914-8db3-469d-8ca1-694a8f32e121}))"), rg, "")}, findings: 1, contains: "deletes"},
		{name: "004 rbac admin expression condition skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(condProps(roleRBACAdmin, "[parameters('c')]"), rg, "")}, skip: SkipUnresolved},
		{name: "004 rbac admin wrong conditionVersion skips", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(func() props { p := condProps(roleRBACAdmin, restrictiveCond); p["conditionVersion"] = "1.0"; return p }(), rg, "")}, skip: SkipUnresolved},
		{name: "004 owner ignores condition", rule: "FND-IDN-004", res: []sdk.ARMResource{appA, ra(condProps(roleOwner, restrictiveCond), rg, "")}, findings: 1},
		// IDN-005
		{name: "005 user owner rg", rule: "FND-IDN-005", res: []sdk.ARMResource{ra(props{"roleDefinitionId": rdef(roleOwner), "principalType": "User", "principalId": guidA}, rg, "")}, findings: 1, contains: "standing human admin access"},
		{name: "005 four subscription owners", rule: "FND-IDN-005", res: []sdk.ARMResource{
			ra(props{"roleDefinitionId": rdef(roleOwner), "principalType": "User", "principalId": guidA}, subsc, ""),
			ra(props{"roleDefinitionId": rdef(roleOwner), "principalType": "User", "principalId": "aaaaaaaa-2222-3333-4444-555555555555"}, subsc, ""),
			ra(props{"roleDefinitionId": rdef(roleOwner), "principalType": "Group", "principalId": "bbbbbbbb-2222-3333-4444-555555555555"}, subsc, ""),
			ra(props{"roleDefinitionId": rdef(roleOwner), "principalType": "Group", "principalId": "cccccccc-2222-3333-4444-555555555555"}, subsc, ""),
		}, findings: 5},
		{name: "005 no live inventory skips", rule: "FND-IDN-005", res: nil, skip: sdk.SkipInputUnavailable},
		// IDN-006
		{name: "006 github ok", rule: "FND-IDN-006", res: []sdk.ARMResource{fic(githubIssuer, "repo:org/repo:ref:refs/heads/main")}},
		{name: "006 github env ok", rule: "FND-IDN-006", res: []sdk.ARMResource{fic(githubIssuer+"/", "repo:org/repo:environment:prod")}},
		{name: "006 github bad subject", rule: "FND-IDN-006", res: []sdk.ARMResource{fic(githubIssuer, "org/repo")}, findings: 1, contains: "GitHub"},
		{name: "006 aks ok", rule: "FND-IDN-006", res: []sdk.ARMResource{fic("https://eastus.oic.prod-aks.azure.com/t/c/", "system:serviceaccount:ns:sa")}},
		{name: "006 aks bad subject", rule: "FND-IDN-006", res: []sdk.ARMResource{fic("https://eastus.oic.prod-aks.azure.com/t/c/", "sa")}, findings: 1, contains: "AKS"},
		{name: "006 http issuer", rule: "FND-IDN-006", res: []sdk.ARMResource{fic("http://issuer.example", "x")}, findings: 1, contains: "https"},
		{name: "006 wildcard subject", rule: "FND-IDN-006", res: []sdk.ARMResource{fic("https://issuer.example", "repo:org/*:ref")}, findings: 1, contains: "wildcard"},
		{name: "006 empty subject", rule: "FND-IDN-006", res: []sdk.ARMResource{fic("https://issuer.example", " ")}, findings: 1, contains: "empty"},
		{name: "006 missing issuer", rule: "FND-IDN-006", res: []sdk.ARMResource{{Type: typeFederated, Name: "f", Properties: props{"subject": "s"}}}, findings: 1},
		{name: "006 other issuer ok", rule: "FND-IDN-006", res: []sdk.ARMResource{fic("https://issuer.example", "anything")}},
		{name: "006 expression issuer", rule: "FND-IDN-006", res: []sdk.ARMResource{fic("[parameters('i')]", "s")}, skip: SkipUnresolved},
		{name: "006 expression subject", rule: "FND-IDN-006", res: []sdk.ARMResource{fic(githubIssuer, "[parameters('s')]")}, skip: SkipUnresolved},
		{name: "006 none", rule: "FND-IDN-006", res: nil, skip: sdk.SkipInputUnavailable},
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
			if c.contains != "" && !strings.Contains(out.Findings[0].Evidence, c.contains) {
				t.Errorf("evidence %q lacks %q", out.Findings[0].Evidence, c.contains)
			}
			for _, f := range out.Findings {
				if f.Resource.Type == "" || f.Evidence == "" {
					t.Errorf("incomplete finding %+v", f)
				}
			}
		})
	}
}

type fakePolicy map[string]any

func (p fakePolicy) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

func TestIDN004DeploymentIdentityExclusion(t *testing.T) {
	in := &sdk.Input{
		ARM: model{
			{Type: typeAccounts, Name: "acct", Identity: props{"type": "SystemAssigned", "principalId": guidA}},
			ra(props{"roleDefinitionId": rdef(roleOwner), "principalId": guidA, "principalType": "ServicePrincipal"}, subsc, ""),
		},
		Policy: fakePolicy{"identity.deploymentPrincipalIds": []string{guidA}},
	}
	out := run(t, "FND-IDN-004", in)
	if out.Skipped == nil || out.Skipped.Reason != sdk.SkipInputUnavailable {
		t.Fatalf("want input-unavailable skip after exclusion, got %+v", out)
	}
}
