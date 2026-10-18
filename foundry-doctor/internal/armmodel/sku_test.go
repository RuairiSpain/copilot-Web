package armmodel_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
)

const typeDeployment = "Microsoft.CognitiveServices/accounts/deployments"

// skuTemplate has a root deployment whose sku comes from root parameters and a
// module deployment whose sku.name is bound by the module call.
const skuTemplate = `{
  "parameters": {"skuName": {"type": "string"}, "cap": {"type": "int"}},
  "resources": [
    {"type": "Microsoft.CognitiveServices/accounts/deployments", "apiVersion": "2025-06-01", "name": "acct/root",
     "sku": {"name": "[parameters('skuName')]", "capacity": "[parameters('cap')]"}, "properties": {}},
    {"type": "Microsoft.Resources/deployments", "apiVersion": "2025-04-01", "name": "mod",
     "properties": {"mode": "Incremental", "expressionEvaluationOptions": {"scope": "inner"},
       "parameters": {"inner": {"value": "ProvisionedManaged"}},
       "template": {"parameters": {"inner": {"type": "string"}}, "resources": [
         {"type": "Microsoft.CognitiveServices/accounts/deployments", "apiVersion": "2025-06-01", "name": "acct/mod",
          "sku": {"name": "[parameters('inner')]", "capacity": 50}, "properties": {}}]}}}
  ]
}`

// Phase 7 review B1: SKUName follows the substituted sku.name.
func TestSKUNameFromParameterFile(t *testing.T) {
	m, err := armmodel.FromARM([]byte(skuTemplate), armmodel.Options{
		ParametersProvided: true,
		Parameters:         map[string]any{"skuName": "GlobalProvisionedManaged", "cap": json.Number("100")},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := byName(t, m, typeDeployment, "acct/root")
	if r.SKUName != "GlobalProvisionedManaged" {
		t.Errorf("SKUName = %q, want the parameter-file value", r.SKUName)
	}
	if got, _ := r.SKU["name"].(string); got != r.SKUName {
		t.Errorf("SKU[name] = %q and SKUName = %q must agree", got, r.SKUName)
	}
	if got, ok := r.SKU["capacity"].(float64); !ok || got != 100 {
		t.Errorf("SKU[capacity] = %#v, want 100", r.SKU["capacity"])
	}
}

func TestSKUNameFromModuleParameter(t *testing.T) {
	m, err := armmodel.FromARM([]byte(skuTemplate), armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := byName(t, m, typeDeployment, "acct/mod")
	if r.SKUName != "ProvisionedManaged" {
		t.Errorf("SKUName = %q, want the module binding", r.SKUName)
	}
	if got, _ := r.SKU["name"].(string); got != r.SKUName {
		t.Errorf("SKU[name] = %q and SKUName = %q must agree", got, r.SKUName)
	}
}

func TestSKUNameUnresolvedStaysEmpty(t *testing.T) {
	m, err := armmodel.FromARM([]byte(skuTemplate), armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := byName(t, m, typeDeployment, "acct/root")
	if r.SKUName != "" {
		t.Errorf("SKUName = %q, want empty for an unresolved expression", r.SKUName)
	}
	if got, _ := r.SKU["name"].(string); !strings.HasPrefix(got, "[") {
		t.Errorf("SKU[name] = %q, want the unresolved expression", got)
	}
}
