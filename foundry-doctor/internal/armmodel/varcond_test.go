package armmodel_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
)

// P3: the "use an existing account" shape. The condition reads a variable that
// is built from a parameter; a parameter file resolves it, no file does not.
func TestVariableMediatedConditionResolvesWithParameterFile(t *testing.T) {
	const compiledCond = "[not(variables('useExisting'))]"
	for _, tc := range []struct {
		name string
		opt  armmodel.Options
		want armexpr.Presence
	}{
		{"file leaves the existing account empty", armmodel.Options{ParametersProvided: true, Parameters: map[string]any{"existingAccountResourceId": ""}}, armexpr.Deployed},
		{"file default applies", armmodel.Options{ParametersProvided: true}, armexpr.Deployed},
		{"file names an existing account", armmodel.Options{ParametersProvided: true, Parameters: map[string]any{"existingAccountResourceId": "/subscriptions/s/resourceGroups/r/providers/Microsoft.CognitiveServices/accounts/a"}}, armexpr.NotDeployed},
		{"no parameter file", armmodel.Options{}, armexpr.MaybeDeployed},
		{"file value unresolved", armmodel.Options{ParametersProvided: true, Parameters: map[string]any{"existingAccountResourceId": nil}}, armexpr.MaybeDeployed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := rulestest.ScenarioWith(t, "varcond", tc.opt)
			var acct, stg *int
			for i, r := range m.Resources() {
				switch r.Type {
				case typeAI:
					acct = &i
					if got := presence(r); got != tc.want {
						t.Errorf("account presence %v want %v (condition %q)", got, tc.want, r.Condition)
					}
					if tc.want == armexpr.MaybeDeployed && r.Condition != compiledCond {
						t.Errorf("without a usable value the condition must stay as compiled, got %q", r.Condition)
					}
				case typeStorage:
					stg = &i
					// viaVariable = !useExisting && contains(accountName, 'ai'): a
					// variable that reads variables and uses contains()/length().
					if got := presence(r); got != tc.want {
						t.Errorf("storage presence %v want %v (condition %q)", got, tc.want, r.Condition)
					}
				}
			}
			if acct == nil || stg == nil {
				t.Fatal("resources missing")
			}
		})
	}
}

// A variable folded into a name resolves it; a custom name changes it.
func TestVariableFoldsIntoName(t *testing.T) {
	m := rulestest.ScenarioWith(t, "varcond", armmodel.Options{ParametersProvided: true, Parameters: map[string]any{"customName": "my-acct"}})
	byName(t, m, typeAI, "my-acct")
	m = rulestest.ScenarioWith(t, "varcond", armmodel.Options{ParametersProvided: true})
	byName(t, m, typeAI, "auto-ai")
	// customName 'my-acct' does not contain 'ai', so the storage account is off.
	m = rulestest.ScenarioWith(t, "varcond", armmodel.Options{ParametersProvided: true, Parameters: map[string]any{"customName": "my-acct"}})
	if got := presence(byName(t, m, typeStorage, "stgvarcond")); got != armexpr.NotDeployed {
		t.Errorf("contains(accountName,'ai') is false for my-acct: presence %v", got)
	}
}

// A default-derived variable value records provenance on the condition path.
func TestVariableConditionDefaultProvenance(t *testing.T) {
	m := rulestest.ScenarioWith(t, "varcond", armmodel.Options{ParametersProvided: true})
	acct := byName(t, m, typeAI, "auto-ai")
	if got := m.FromDefault(acct); !contains(got, "existingAccountResourceId") || !contains(got, "customName") {
		t.Errorf("FromDefault %v", got)
	}
	paths := m.FromDefaultPaths(acct)
	if !contains(paths, "condition") || !contains(paths, "name") {
		t.Errorf("FromDefaultPaths %v, want condition and name", paths)
	}
	// With the file setting both, nothing is default-derived.
	m = rulestest.ScenarioWith(t, "varcond", armmodel.Options{ParametersProvided: true,
		Parameters: map[string]any{"existingAccountResourceId": "", "customName": ""}})
	if got := m.FromDefaultPaths(byName(t, m, typeAI, "auto-ai")); len(got) != 0 {
		t.Errorf("file values are not defaults: %v", got)
	}
}

// P4: FromDefaultPaths names where a default landed, not just that one did.
func TestFromDefaultPathsNamesTheLandingPlace(t *testing.T) {
	m := rulestest.ScenarioWith(t, "params", armmodel.Options{
		ParametersProvided: true,
		Parameters:         map[string]any{"prefix": "dev"},
	})
	ai := byName(t, m, typeAI, "dev-ai")
	if got := m.FromDefaultPaths(ai); strings.Join(got, ",") != "properties.publicNetworkAccess" {
		t.Errorf("account paths %v", got)
	}
	srch := byName(t, m, typeSearch, "dev-srch")
	got := m.FromDefaultPaths(srch)
	for _, want := range []string{"condition", "properties.publicNetworkAccess", "properties.replicaCount"} {
		if !contains(got, want) {
			t.Errorf("search paths %v lack %s", got, want)
		}
	}
	if contains(got, "name") || contains(got, "location") {
		t.Errorf("name and location come from the file and an expression: %v", got)
	}
}

func tmpl(vars string, cond string) []byte {
	return []byte(fmt.Sprintf(`{
  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  "contentVersion": "1.0.0.0",
  "parameters": {"p": {"type": "string", "defaultValue": "x"}},
  "variables": {%s},
  "resources": [
    {"type": "Microsoft.Storage/storageAccounts", "apiVersion": "2023-05-01", "name": "s1", "location": "westeurope",
     "condition": %q, "kind": "StorageV2", "sku": {"name": "Standard_LRS"}}
  ]
}`, vars, cond))
}

// Cycles, objects and unsupported functions are refused, not guessed.
func TestVariableFoldingRefusals(t *testing.T) {
	opt := armmodel.Options{ParametersProvided: true}
	for _, tc := range []struct {
		name, vars, cond string
		want             armexpr.Presence
	}{
		{"literal variable folds", `"v": "[not(empty(parameters('p')))]"`, "[variables('v')]", armexpr.Deployed},
		{"cycle", `"a": "[variables('b')]", "b": "[variables('a')]"`, "[variables('a')]", armexpr.MaybeDeployed},
		{"self reference", `"a": "[not(variables('a'))]"`, "[variables('a')]", armexpr.MaybeDeployed},
		{"unsupported function", `"a": "[bool(parameters('p'))]"`, "[variables('a')]", armexpr.MaybeDeployed},
		{"object variable", `"a": {"k": "v"}`, "[variables('a')]", armexpr.MaybeDeployed},
		{"array variable", `"a": ["x"]`, "[variables('a')]", armexpr.MaybeDeployed},
		{"unknown variable", `"a": true`, "[variables('nope')]", armexpr.MaybeDeployed},
		{"depends on function call", `"a": "[resourceGroup().name]"`, "[variables('a')]", armexpr.MaybeDeployed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := armmodel.FromARM(tmpl(tc.vars, tc.cond), opt)
			if err != nil {
				t.Fatal(err)
			}
			r := m.Resources()[0]
			if got := presence(r); got != tc.want {
				t.Errorf("presence %v want %v (condition %q)", got, tc.want, r.Condition)
			}
		})
	}
}

// A variable fan-out that would take exponential time without memoisation
// finishes, and the folded text stays bounded.
func TestVariableFoldingFanOutIsBounded(t *testing.T) {
	var vars []string
	vars = append(vars, `"v0": "[parameters('p')]"`)
	for i := 1; i <= 40; i++ {
		vars = append(vars, fmt.Sprintf(`"v%d": "[concat(variables('v%d'), variables('v%d'))]"`, i, i-1, i-1))
	}
	m, err := armmodel.FromARM(tmpl(strings.Join(vars, ","), "[equals(length(variables('v40')), 5)]"), armmodel.Options{ParametersProvided: true})
	if err != nil {
		t.Fatal(err)
	}
	// 2^40 characters would be needed: the value must be refused, the
	// condition left as an expression.
	if got := presence(m.Resources()[0]); got != armexpr.MaybeDeployed {
		t.Errorf("oversize variable must stay unresolved, presence %v", got)
	}
}
