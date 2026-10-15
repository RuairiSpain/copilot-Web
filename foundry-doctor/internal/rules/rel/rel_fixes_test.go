package rel

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func kind(res sdk.Result) string {
	switch {
	case len(res.Findings) > 0:
		return "finding"
	case res.Skipped != nil:
		return "skipped"
	}
	return "pass"
}

const (
	searchName  = "[format('srch-{0}', uniqueString(resourceGroup().id))]"
	searchScope = "[resourceId('Microsoft.Search/searchServices', format('srch-{0}', uniqueString(resourceGroup().id)))]"
)

func lock(scope string) sdk.ARMResource {
	return sdk.ARMResource{Type: typeLock, Name: "protect", Scope: scope, Properties: map[string]any{"level": "CanNotDelete"}}
}

func TestREL004LockScopes(t *testing.T) {
	search := res(typeSearch, searchName, "standard", nil)
	cond := func(r sdk.ARMResource, c string) sdk.ARMResource { r.Conditional, r.Condition = true, c; return r }
	cases := []struct {
		name string
		arm  model
		want string // "pass" means covered; "skipped" is the uncertain outcome
		note string
	}{
		{"scope-less lock covers the deployment resource group", model{search, lock("")}, "pass", ""},
		{"resourceGroup().id lock covers the deployment group", model{search, lock("[resourceGroup().id]")}, "pass", ""},
		{"lock on the resource by identical expression", model{search, lock(searchScope)}, "pass", ""},
		{"lock on another resource type does not cover", model{search, lock("[resourceId('Microsoft.Storage/storageAccounts', 'x')]")}, "skipped", "outside the template"},
		{"lock on a literal resource group is not provably the deployment group", model{search, lock("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/other-rg")}, "skipped", "could not be correlated"},
		{"lock on another resource group by expression is unknown", model{search, lock("[resourceGroup(parameters('otherRg')).id]")}, "skipped", "could not be correlated"},
		{"lock on a different expression name is unknown", model{search, lock("[resourceId('Microsoft.Search/searchServices', variables('searchName'))]")}, "skipped", "could not be correlated"},
		{"no lock at all", model{search}, "skipped", "no lock resources"},
		{"literal false lock does not count", model{search, cond(lock(""), "[false()]")}, "skipped", "no lock resources"},
		{"parameter-driven lock is uncertain", model{search, cond(lock(""), "[parameters('lock')]")}, "skipped", ""},
		{"literal false target is not deployed", model{cond(search, "false")}, "pass", ""},
		{"no targets", model{}, "pass", ""},
		{"ReadOnly lock counts", model{search, func() sdk.ARMResource { l := lock(""); l.Properties = map[string]any{"level": "ReadOnly"}; return l }()}, "pass", ""},
		{"lock level from a parameter is unknown", model{search, func() sdk.ARMResource {
			l := lock("")
			l.Properties = map[string]any{"level": "[parameters('level')]"}
			return l
		}()}, "skipped", "could not be correlated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, "FND-REL-004", &sdk.Input{ARM: tc.arm})
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
			if tc.note != "" && !strings.Contains(strings.ToLower(got.Skipped.Reason), tc.note) {
				t.Fatalf("reason %q does not contain %q", got.Skipped.Reason, tc.note)
			}
		})
	}
	if got := eval(t, "FND-REL-004", &sdk.Input{}); kind(got) != "skipped" {
		t.Fatalf("missing ARM must skip: %+v", got)
	}
}

func TestRELConditionalResources(t *testing.T) {
	falseCond := func(r sdk.ARMResource) sdk.ARMResource { r.Conditional, r.Condition = true, "[false()]"; return r }
	paramCond := func(r sdk.ARMResource) sdk.ARMResource {
		r.Conditional, r.Condition = true, "[parameters('deploy')]"
		return r
	}
	oneReplica := res(typeSearch, searchName, "standard", map[string]any{"replicaCount": float64(1)})
	zrs := res(typeStorage, "[format('st{0}', uniqueString(resourceGroup().id))]", "Standard_LRS", nil)
	cosmos := res(typeCosmos, "cosmos", "", map[string]any{"locations": []any{map[string]any{"isZoneRedundant": false}}, "backupPolicy": map[string]any{"type": "Continuous"}})
	badSearch := res(typeSearch, "s", "standard", map[string]any{"replicaCount": float64(2), "partitionCount": float64(5)})
	cases := []struct {
		rule string
		r    sdk.ARMResource
	}{
		{"FND-REL-001", oneReplica},
		{"FND-REL-002", zrs},
		{"FND-REL-003", cosmos},
		{"FND-REL-005", badSearch},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			if got := eval(t, tc.rule, &sdk.Input{ARM: model{tc.r}}); kind(got) != "finding" {
				t.Fatalf("live resource must fail: %+v", got)
			}
			if got := eval(t, tc.rule, &sdk.Input{ARM: model{falseCond(tc.r)}}); kind(got) != "pass" {
				t.Fatalf("literal false must be ignored: %+v", got)
			}
			if got := eval(t, tc.rule, &sdk.Input{ARM: model{paramCond(tc.r)}}); kind(got) != "skipped" {
				t.Fatalf("parameter condition must be uncertain: %+v", got)
			}
			looped := tc.r
			looped.Looped = true
			if got := eval(t, tc.rule, &sdk.Input{ARM: model{looped}}); kind(got) != "skipped" {
				t.Fatalf("looped resource must be uncertain: %+v", got)
			}
			if got := eval(t, tc.rule, &sdk.Input{}); kind(got) != "skipped" {
				t.Fatalf("missing ARM must skip: %+v", got)
			}
		})
	}
}

func TestREL008SpilloverConditionalAndExpressions(t *testing.T) {
	ptu := func(spill string) sdk.ARMResource {
		return sdk.ARMResource{Type: typeDeployment, Name: "acct/ptu", SKUName: "GlobalProvisionedManaged",
			Properties: map[string]any{"spilloverDeploymentName": spill, "model": map[string]any{"format": "OpenAI", "name": "gpt-4o", "version": "1"}}}
	}
	std := sdk.ARMResource{Type: typeDeployment, Name: "acct/std", SKUName: "GlobalStandard",
		Properties: map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt-4o", "version": "1"}}}
	if got := eval(t, "FND-REL-008", &sdk.Input{ARM: model{ptu("std"), std}}); kind(got) != "pass" {
		t.Fatalf("got %+v", got)
	}
	if got := eval(t, "FND-REL-008", &sdk.Input{ARM: model{ptu("[parameters('spill')]")}}); kind(got) != "skipped" {
		t.Fatalf("expression spillover name must be uncertain: %+v", got)
	}
	cond := std
	cond.Conditional, cond.Condition = true, "[parameters('createStd')]"
	if got := eval(t, "FND-REL-008", &sdk.Input{ARM: model{ptu("std"), cond}}); kind(got) != "skipped" {
		t.Fatalf("conditional spillover target must be uncertain: %+v", got)
	}
	exprModel := std
	exprModel.Properties = map[string]any{"model": map[string]any{"format": "OpenAI", "name": "[parameters('modelName')]", "version": "1"}}
	if got := eval(t, "FND-REL-008", &sdk.Input{ARM: model{ptu("std"), exprModel}}); kind(got) != "skipped" {
		t.Fatalf("expression model must be uncertain: %+v", got)
	}
	other := std
	other.Properties = map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt-4.1", "version": "1"}}
	if got := eval(t, "FND-REL-008", &sdk.Input{ARM: model{ptu("std"), other}}); kind(got) != "finding" {
		t.Fatalf("different literal model must fail: %+v", got)
	}
}

func TestREL009SKUExpression(t *testing.T) {
	apim := sdk.ARMResource{Type: typeAPIM, Name: "apim", SKU: map[string]any{"name": "[parameters('apimSku')]"}}
	in := &sdk.Input{Policy: pol{"reliability.apimZoneRequired": true}, ARM: model{apim}}
	if got := eval(t, "FND-REL-009", in); kind(got) != "skipped" {
		t.Fatalf("unresolved SKU must not fail: %+v", got)
	}
	apim.SKUName = "Developer"
	in = &sdk.Input{Policy: pol{"reliability.apimZoneRequired": true}, ARM: model{apim}}
	if got := eval(t, "FND-REL-009", in); kind(got) != "finding" {
		t.Fatalf("literal Developer tier fails the zone objective: %+v", got)
	}
}

func TestRelExprHelpers(t *testing.T) {
	if !isExpr("[format('a')]") || isExpr("[[a") || isExpr("a") {
		t.Fatal("isExpr")
	}
	if stateOf(sdk.ARMResource{Conditional: true, Condition: "[false()]"}) != stateAbsent {
		t.Fatal("false() must be absent")
	}
	if stateOf(sdk.ARMResource{Conditional: true, Condition: "[parameters('x')]"}) != stateUncertain {
		t.Fatal("parameter condition must be uncertain")
	}
	fn, args, ok := splitCall("resourceId('A/b', format('{0},{1}', x(1, 2), 'y'))")
	if !ok || fn != "resourceId" || len(args) != 2 {
		t.Fatalf("fn=%q args=%q ok=%v", fn, args, ok)
	}
	if _, _, ok := splitCall("f(("); ok {
		t.Fatal("unbalanced call must not parse")
	}
}
