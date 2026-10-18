package cost

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const paramDeployment = `{
  "parameters": {"deploymentSku": {"type": "string"}},
  "resources": [
    {"type": "Microsoft.CognitiveServices/accounts/deployments", "apiVersion": "2025-06-01", "name": "acct/chat",
     "sku": {"name": "[parameters('deploymentSku')]", "capacity": 50}, "properties": {}}
  ]
}`

func paramModel(t *testing.T, params map[string]any) *armmodel.Model {
	t.Helper()
	m, err := armmodel.FromARM([]byte(paramDeployment), armmodel.Options{ParametersProvided: params != nil, Parameters: params})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Phase 7 review B1: a PTU SKU that comes from a parameter file is seen.
func TestCost002ParameterResolvedPTU(t *testing.T) {
	devIn := func(m sdk.ARMModel) *sdk.Input {
		return &sdk.Input{Profile: "foundry-dev", Policy: pol{"policy.cost.devMaxCosmosThroughput": 4000}, ARM: m}
	}
	got := run(t, "FND-COST-002", devIn(paramModel(t, map[string]any{"deploymentSku": "ProvisionedManaged"})))
	if kind(got) != "finding" || got.Findings[0].Key != "dev:provisioned-throughput" {
		t.Fatalf("parameter-resolved PTU must be a finding: %+v", got)
	}
	got = run(t, "FND-COST-002", devIn(paramModel(t, map[string]any{"deploymentSku": "GlobalStandard"})))
	if kind(got) != "pass" {
		t.Fatalf("GlobalStandard: %+v", got)
	}
	got = run(t, "FND-COST-002", devIn(paramModel(t, nil)))
	if kind(got) != "skipped" || got.Skipped.Reason != "unresolved-expression" {
		t.Fatalf("unresolved sku.name must be skipped as unresolved: %+v", got)
	}
}

func keys(res sdk.Result) string {
	var out []string
	for _, f := range res.Findings {
		out = append(out, f.Resource.Name+"="+f.Key)
	}
	return strings.Join(out, ",")
}

// Phase 7 review B3/N9: FND-COST-004 reads Cosmos databases and containers
// and the parent-account region the same way as the cost engine.
func TestCost004CosmosSharedExtraction(t *testing.T) {
	acct := sdk.ARMResource{Type: "Microsoft.DocumentDB/databaseAccounts", Name: "acct", Properties: map[string]any{
		"locations": []any{map[string]any{"locationName": "eastus", "isZoneRedundant": false}},
	}}
	db := sdk.ARMResource{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases", Name: "acct/db", Properties: map[string]any{"options": map[string]any{"throughput": float64(400)}}}
	if got := run(t, "FND-COST-004", &sdk.Input{ARM: model{acct, db}}); kind(got) != "pass" {
		t.Fatalf("database with literal throughput and resolved account: %+v", got)
	}
	auto := sdk.ARMResource{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers", Name: "acct/db/c", Properties: map[string]any{"options": map[string]any{"autoscaleSettings": map[string]any{"maxThroughput": float64(4000)}}}}
	orphan := sdk.ARMResource{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/throughputSettings", Name: "other/db/default", Properties: map[string]any{"resource": map[string]any{"throughput": float64(400)}}}
	got := keys(run(t, "FND-COST-004", &sdk.Input{ARM: model{acct, db, auto, orphan}}))
	for _, want := range []string{"acct/db/c=incomplete:cosmos-autoscale-meter", "other/db/default=incomplete:cosmos-region"} {
		if !strings.Contains(got, want) {
			t.Errorf("findings %q lack %q", got, want)
		}
	}
}

func TestCost004DeploymentSKUs(t *testing.T) {
	dep := func(name, sku string) sdk.ARMResource {
		return sdk.ARMResource{Type: typeDeployment, Name: name, SKUName: sku}
	}
	got := run(t, "FND-COST-004", &sdk.Input{ARM: model{dep("a/tok", "datazonestandard"), dep("a/batch", "GlobalBatch")}})
	if kind(got) != "skipped" {
		t.Fatalf("token-based deployments only: %+v", got)
	}
	got = run(t, "FND-COST-004", &sdk.Input{ARM: model{dep("a/odd", "SuperFast"), dep("a/ptu", "DATAZONEPROVISIONEDMANAGED")}})
	k := keys(got)
	if !strings.Contains(k, "a/odd=incomplete:deployment-sku") || !strings.Contains(k, "a/ptu=incomplete:ptu-meter") {
		t.Fatalf("findings %q", k)
	}
}

func TestCost001ThresholdRange(t *testing.T) {
	email := map[string]any{"contactEmails": []any{"finops@example.com"}}
	over := notification("Actual", email)
	over["threshold"] = float64(1500)
	got := run(t, "FND-COST-001", &sdk.Input{ARM: model{budget(map[string]any{"o": over, "f": notification("Forecasted", email)})}})
	if !strings.Contains(keys(got), "budget-threshold-out-of-range") {
		t.Fatalf("findings %q", keys(got))
	}
}
