package cost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return []sdk.ARMResource(m) }

func TestEstimateIncludesSupportedAndExcludedResources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"Items":[]}`
		switch {
		case strings.Contains(r.URL.RawQuery, "Standard+S2+Unit"):
			body = `{"Items":[{"currencyCode":"USD","unitPrice":1.344,"armRegionName":"eastus","productName":"Azure AI Search","skuName":"Standard S2","meterName":"Standard S2 Unit","serviceName":"Azure Cognitive Search","unitOfMeasure":"1 Hour","effectiveStartDate":"2025-10-01T00:00:00Z","type":"Consumption"}]}`
		case strings.Contains(r.URL.RawQuery, "Premium+Unit"):
			body = `{"Items":[{"currencyCode":"USD","unitPrice":2.795,"armRegionName":"eastus","productName":"API Management","skuName":"Premium","meterName":"Premium Unit","serviceName":"API Management","unitOfMeasure":"1 Hour","effectiveStartDate":"2025-10-01T00:00:00Z","type":"Consumption"}]}`
		case strings.Contains(r.URL.RawQuery, "100+RU%2Fs"):
			body = `{"Items":[{"currencyCode":"USD","unitPrice":0.008,"armRegionName":"eastus","productName":"Azure Cosmos DB","skuName":"RUs","meterName":"100 RU/s","serviceName":"Azure Cosmos DB","unitOfMeasure":"1/Hour","effectiveStartDate":"2025-10-01T00:00:00Z","type":"Consumption"}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	doc, err := Engine{}.Estimate(context.Background(), Request{
		Environments: []Environment{{
			Name:    "prod",
			Profile: "foundry-prod",
			ARM: model{
				{Type: "Microsoft.Search/searchServices", Name: "search", Region: "eastus", SKUName: "standard2", Properties: map[string]any{"replicaCount": float64(2), "partitionCount": float64(1)}},
				{Type: "Microsoft.ApiManagement/service", Name: "apim", Region: "eastus", SKUName: "Premium", SKU: map[string]any{"capacity": float64(1)}},
				{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/throughputSettings", Name: "cosmos", Region: "eastus", Properties: map[string]any{"resource": map[string]any{"throughput": float64(4000)}}},
				{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "chat", SKUName: "GlobalStandard"},
				{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "ptu", SKUName: "ProvisionedManaged"},
			},
		}},
		BaseURL:        srv.URL,
		HTTPClient:     srv.Client(),
		PriceCachePath: "",
		Now:            func() time.Time { return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	env := doc.Environments[0]
	if len(env.Estimated) != 3 {
		t.Fatalf("estimated lines = %d, want 3", len(env.Estimated))
	}
	if len(env.Excluded) != 1 || env.Excluded[0].ResourceName != "chat" {
		t.Fatalf("excluded = %+v", env.Excluded)
	}
	if len(env.Unsupported) != 1 || env.Unsupported[0].ResourceName != "ptu" {
		t.Fatalf("unsupported = %+v", env.Unsupported)
	}
	if env.TotalHigh <= 0 {
		t.Fatalf("total = %+v", env)
	}
}

func TestEstimateOfflineMissingCacheMarksIncomplete(t *testing.T) {
	doc, err := Engine{}.Estimate(context.Background(), Request{
		Environments: []Environment{{Name: "prod", Profile: "foundry-prod", ARM: model{
			{Type: "Microsoft.Search/searchServices", Name: "search", Region: "eastus", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1)}},
		}}},
		Offline: true,
		Now:     func() time.Time { return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Environments[0].Incomplete || len(doc.Environments[0].Unsupported) == 0 {
		t.Fatalf("expected incomplete offline report: %+v", doc.Environments[0])
	}
}
