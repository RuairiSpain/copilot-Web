package cost

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return []sdk.ARMResource(m) }

type pol map[string]any

func (p pol) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

func find(rs []sdk.Rule, id string) sdk.Rule {
	for _, r := range rs {
		if r.ID() == id {
			return r
		}
	}
	return nil
}

func TestCost001BudgetChecks(t *testing.T) {
	r := find(Register(), "FND-COST-001")
	res, err := r.Evaluate(context.Background(), &sdk.Input{ARM: model{
		{Type: typeBudget, Name: "b", Properties: map[string]any{"properties": map[string]any{
			"category":   "Cost",
			"amount":     float64(500),
			"timeGrain":  "Monthly",
			"timePeriod": map[string]any{"startDate": "2026-11-01T00:00:00Z"},
			"notifications": map[string]any{
				"actual80":     map[string]any{"enabled": true, "operator": "GreaterThan", "threshold": float64(80), "thresholdType": "Actual", "contactEmails": []any{"finops@example.com"}},
				"forecasted90": map[string]any{"enabled": true, "operator": "GreaterThan", "threshold": float64(90), "thresholdType": "Forecasted", "contactGroups": []any{"/subscriptions/x/resourceGroups/y/providers/microsoft.insights/actionGroups/ag"}},
			},
		}}},
	}})
	if err != nil || res.Skipped != nil || len(res.Findings) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	res, err = r.Evaluate(context.Background(), &sdk.Input{ARM: model{{Type: typeBudget, Name: "b", Properties: map[string]any{"properties": map[string]any{
		"category": "Cost", "notifications": map[string]any{},
	}}}}})
	if err != nil || len(res.Findings) == 0 {
		t.Fatalf("expected finding, got res=%+v err=%v", res, err)
	}
}

func TestCost002DevSkusAndCosmosLimit(t *testing.T) {
	r := find(Register(), "FND-COST-002")
	res, err := r.Evaluate(context.Background(), &sdk.Input{
		Profile:     "foundry-dev",
		Environment: "dev",
		Policy: pol{
			"policy.cost.devMaxCosmosThroughput":       4000,
			"policy.cost.productionSizedSkuExemptions": []string{"skipme"},
		},
		ARM: model{
			{Type: typeDeployment, Name: "deploy", SKUName: "ProvisionedManaged"},
			{Type: typeSearch, Name: "search", SKUName: "standard3", Properties: map[string]any{"replicaCount": float64(2), "partitionCount": float64(1)}},
			{Type: typeAPIM, Name: "apim", SKUName: "Premium", SKU: map[string]any{"capacity": float64(2)}},
			{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/throughputSettings", Name: "cosmos", Properties: map[string]any{"resource": map[string]any{"throughput": float64(5000)}}},
			{Type: typeAPIM, Name: "skipme", SKUName: "Premium", SKU: map[string]any{"capacity": float64(2)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) < 4 {
		t.Fatalf("expected multiple dev-cost findings, got %+v", res)
	}

	res, err = r.Evaluate(context.Background(), &sdk.Input{Profile: "foundry-prod", ARM: model{}})
	if err != nil || res.Skipped == nil {
		t.Fatalf("expected non-dev skip, got %+v err=%v", res, err)
	}
}

func TestCost003GatewayTokenLimit(t *testing.T) {
	r := find(Register(), "FND-COST-003")
	res, err := r.Evaluate(context.Background(), &sdk.Input{ARM: model{
		{Type: "Microsoft.ApiManagement/service", Name: "apim"},
		{Type: "Microsoft.ApiManagement/service/apis", Name: "apim/chat", Properties: map[string]any{"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai"}},
		{Type: "Microsoft.ApiManagement/service/apis/policies", Name: "apim/chat/policy", Properties: map[string]any{"format": "xml", "value": `<policies><inbound /></policies>`}},
	}})
	if err != nil || len(res.Findings) == 0 {
		t.Fatalf("expected finding, got %+v err=%v", res, err)
	}

	res, err = r.Evaluate(context.Background(), &sdk.Input{ARM: model{
		{Type: "Microsoft.ApiManagement/service", Name: "apim"},
		{Type: "Microsoft.ApiManagement/service/apis", Name: "apim/chat", Properties: map[string]any{"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai"}},
		{Type: "Microsoft.ApiManagement/service/apis/policies", Name: "apim/chat/policy", Properties: map[string]any{"format": "xml", "value": `<policies><inbound><llm-token-limit counter-key="tenant" estimate-prompt-tokens="true" tokens-per-minute="1000" /></inbound></policies>`}},
	}})
	if err != nil || len(res.Findings) != 0 || res.Skipped != nil {
		t.Fatalf("expected pass, got %+v err=%v", res, err)
	}
}

func TestCost004EstimateCompleteness(t *testing.T) {
	r := find(Register(), "FND-COST-004")
	res, err := r.Evaluate(context.Background(), &sdk.Input{ARM: model{
		{Type: typeSearch, Name: "search", Region: "eastus", SKUName: "standard"},
	}})
	if err != nil || len(res.Findings) == 0 {
		t.Fatalf("expected incomplete-estimate finding, got %+v err=%v", res, err)
	}

	res, err = r.Evaluate(context.Background(), &sdk.Input{ARM: model{
		{Type: typeSearch, Name: "search", Region: "eastus", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(2), "partitionCount": float64(1)}},
		{Type: typeAPIM, Name: "apim", Region: "eastus", SKUName: "Premium", SKU: map[string]any{"capacity": float64(1)}},
	}})
	if err != nil || len(res.Findings) != 0 || res.Skipped != nil {
		t.Fatalf("expected pass, got %+v err=%v", res, err)
	}
}
