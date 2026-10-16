package idn

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Golden ARM JSON compiled from internal/rules/rulestest/main.bicep with
// Bicep CLI 0.48.1: subscriptionResourceId role definitions, parameter
// principals, expression-named Foundry account and project.

func TestCompiledIDN001OnlyFoundryAccounts(t *testing.T) {
	out := run(t, "FND-IDN-001", &sdk.Input{ARM: rulestest.Model(t)})
	// The AIServices account and its project have identities; the Speech
	// account is not a Foundry account and is not evaluated.
	if out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("want pass, got %+v", out)
	}
}

func TestCompiledIDN003(t *testing.T) {
	out := run(t, "FND-IDN-003", &sdk.Input{ARM: rulestest.Model(t)})
	if len(out.Findings) != 1 {
		t.Fatalf("want exactly the uamiClientId assignment, got %+v", out.Findings)
	}
	f := out.Findings[0]
	if f.Key != "principalId" || !strings.Contains(f.Evidence, "clientId") {
		t.Errorf("finding %+v", f)
	}
	// appIdentityPrincipalId and principalId parameters were not reported.
	for _, g := range out.Findings {
		if strings.Contains(g.Evidence, "appIdentityPrincipalId") {
			t.Errorf("appIdentityPrincipalId is an object ID parameter: %+v", g)
		}
	}
}

func TestCompiledIDN004ResolvesCompiledRoleAndScope(t *testing.T) {
	out := run(t, "FND-IDN-004", &sdk.Input{ARM: rulestest.Model(t)})
	if out.Skipped != nil || len(out.Findings) != 1 {
		t.Fatalf("want the project Owner assignment, got %+v", out)
	}
	if !strings.Contains(out.Findings[0].Evidence, "Owner") || !strings.Contains(out.Findings[0].Evidence, "broad deployment scope") {
		t.Errorf("evidence %q", out.Findings[0].Evidence)
	}
}

func TestCompiledIDN005And006DoNotRegress(t *testing.T) {
	// Role GUIDs in compiled subscriptionResourceId form are read by IDN-005
	// too; the golden template has no human principals, so it is skipped by
	// design, not passed.
	out := run(t, "FND-IDN-005", &sdk.Input{ARM: rulestest.Model(t)})
	if out.Skipped == nil || len(out.Findings) != 0 {
		t.Fatalf("want skip, got %+v", out)
	}
}

// M2/M10: golden compiled from rulestest/goldens/idn.bicep. The Owner
// assignment to the project identity has no principalType and is still
// evaluated. The Owner whose role definition is held in variables('ownerRoleId')
// is now resolved through the model's compiled variables and is reported too.
// Contributor at the account scope is an info finding. Only the parameter
// principal stays uncertain and is named as such.
func TestCompiledIDN004GoldenNoPrincipalTypeAndVariables(t *testing.T) {
	out := run(t, "FND-IDN-004", &sdk.Input{ARM: rulestest.Golden(t, "idn")})
	if out.Skipped != nil {
		t.Fatalf("unexpected rule skip: %+v", out)
	}
	owners, infos := 0, 0
	for _, f := range out.Findings {
		ev := f.Evidence
		switch {
		case strings.Contains(ev, "Owner granted to an application identity at broad deployment scope"):
			owners++
		case f.Severity == "info" && strings.Contains(ev, "Contributor"):
			infos++
		default:
			t.Errorf("unexpected finding %q", ev)
		}
		if strings.Contains(ev, `""`) {
			t.Errorf("evidence must not print an empty scope: %q", ev)
		}
		if strings.Contains(ev, "not evaluated") {
			t.Errorf("evidence must not carry a note: %q", ev)
		}
	}
	if owners != 2 || infos != 1 {
		t.Fatalf("want 2 Owner findings (no principalType, role in variables()) and 1 info Contributor, got %d and %d: %+v", owners, infos, out.Findings)
	}
	var skips []string
	for _, s := range out.Skips {
		skips = append(skips, s.Reason)
	}
	all := strings.Join(skips, "\n")
	if !strings.Contains(all, "principalType is not set and principalId is not a provable Foundry identity") {
		t.Errorf("skips lack the parameter-principal cause: %q", all)
	}
	if strings.Contains(all, "variables('ownerRoleId')") {
		t.Errorf("the variable role id must be resolved now: %q", all)
	}
}

func TestCompiledIDN004GoldenOnlyUnresolvedNamesRealCause(t *testing.T) {
	m := rulestest.Golden(t, "idn")
	// Keep only the variable-role and parameter-principal assignments.
	var keep []sdk.ARMResource
	for _, r := range m.Resources() {
		if r.Type == typeRoleAssign && strings.Contains(r.Name, "owner-notype") {
			continue
		}
		keep = append(keep, r)
	}
	out := run(t, "FND-IDN-004", &sdk.Input{ARM: model(keep)})
	if out.Skipped == nil || len(out.Findings) != 0 {
		t.Fatalf("want skip, got %+v", out)
	}
	r := out.Skipped.Reason
	if !strings.HasPrefix(r, SkipUnresolved+":") || !strings.Contains(r, "variables('ownerRoleId')") || !strings.Contains(r, "principalType is not set") {
		t.Errorf("reason does not name the real causes: %q", r)
	}
}

// varModel is a model that exposes compiled variables, as the compiled
// template does in its "variables" section.
type varModel struct {
	model
	vars map[string]any
}

func (v varModel) Variable(name string) (any, bool) { x, ok := v.vars[name]; return x, ok }

func TestIDN004ResolvesLiteralVariableRoleWhenModelExposesVariables(t *testing.T) {
	m := rulestest.Golden(t, "idn")
	vm := varModel{model: model(m.Resources()), vars: map[string]any{
		"ownerRoleId":       roleOwner,
		"contributorRoleId": roleContributor,
	}}
	out := run(t, "FND-IDN-004", &sdk.Input{ARM: vm})
	info := 0
	for _, f := range out.Findings {
		if f.Severity == sdk.SeverityInfo {
			info++
		}
	}
	if len(out.Findings) != 3 || info != 1 {
		t.Fatalf("want the no-principalType Owner, the variable Owner and an info Contributor, got %+v", out)
	}
	// A variable that is itself an expression is reported with that cause.
	vm.vars["ownerRoleId"] = "[parameters('x')]"
	out = run(t, "FND-IDN-004", &sdk.Input{ARM: vm})
	hasCause := false
	for _, sk := range out.Skips {
		hasCause = hasCause || strings.Contains(sk.Reason, "not a literal string")
	}
	if len(out.Findings) != 2 || !hasCause {
		t.Fatalf("got %+v", out)
	}
}

func TestIDN003NameOnlyClientIDWording(t *testing.T) {
	rs := model{ra(props{"principalId": "[parameters('uamiClientId')]", "principalType": "ServicePrincipal"}, "", "2022-04-01")}
	out := run(t, "FND-IDN-003", &sdk.Input{ARM: rs})
	if len(out.Findings) != 1 || !strings.Contains(out.Findings[0].Evidence, "name match only") || !strings.Contains(out.Findings[0].Evidence, "likely") {
		t.Fatalf("name-only match must say so: %+v", out)
	}
	if out.Findings[0].Confidence != sdk.ConfidenceMedium {
		t.Fatalf("name-only match must lower confidence to likely (medium), got %q", out.Findings[0].Confidence)
	}
	rs = model{ra(props{"principalId": "[reference(resourceId('Microsoft.ManagedIdentity/userAssignedIdentities', 'u'), '2023-01-31').clientId]", "principalType": "ServicePrincipal"}, "", "2022-04-01")}
	out = run(t, "FND-IDN-003", &sdk.Input{ARM: rs})
	if len(out.Findings) != 1 || strings.Contains(out.Findings[0].Evidence, "name match only") {
		t.Fatalf("property access must stay certain: %+v", out)
	}
}
