package catalog

import (
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/prices"
)

// DriverKind identifies one supported fixed-capacity billing driver.
type DriverKind string

const (
	DriverSearchUnits       DriverKind = "search-units"
	DriverAPIMUnits         DriverKind = "apim-units"
	DriverCosmosProvisioned DriverKind = "cosmos-provisioned-rus"
	DriverFoundryPTU        DriverKind = "foundry-provisioned-throughput"
	DriverTokenConsumption  DriverKind = "token-consumption"
)

// MeterSpec describes how one driver maps to a public retail-price meter.
type MeterSpec struct {
	Key             string
	Description     string
	Query           prices.Query
	QuantityDivisor float64
}

// Search returns the supported Azure AI Search dedicated-tier mapping.
func Search(region, currency, sku string) (MeterSpec, bool) {
	meters := map[string]string{
		"basic":                "Basic Unit",
		"standard":             "Standard Unit",
		"standard2":            "Standard S2 Unit",
		"standard3":            "Standard S3 Unit",
		"storage_optimized_l1": "Storage Optimized L1 Unit",
		"storage_optimized_l2": "Storage Optimized L2 Unit",
	}
	meter, ok := meters[strings.ToLower(strings.TrimSpace(sku))]
	if !ok {
		return MeterSpec{}, false
	}
	return MeterSpec{
		Key:         "search/" + strings.ToLower(strings.TrimSpace(sku)),
		Description: fmt.Sprintf("Azure AI Search %s dedicated unit", sku),
		Query: prices.Query{
			ServiceName:   "Azure Cognitive Search",
			ProductName:   "Azure AI Search",
			Region:        region,
			CurrencyCode:  currency,
			MeterName:     meter,
			RequireHourly: true,
		},
		QuantityDivisor: 1,
	}, true
}

// APIM returns the supported API Management fixed-capacity mapping.
func APIM(region, currency, sku string) (MeterSpec, bool) {
	normalized := strings.TrimSpace(sku)
	if normalized == "" {
		return MeterSpec{}, false
	}
	meter := normalized + " Unit"
	switch strings.ToLower(normalized) {
	case "developer", "basic", "standard", "premium", "isolated":
	case "basicv2":
		normalized, meter = "Basic v2", "Basic v2 Unit"
	case "standardv2":
		normalized, meter = "Standard v2", "Standard v2 Unit"
	default:
		return MeterSpec{}, false
	}
	return MeterSpec{
		Key:         "apim/" + strings.ToLower(strings.ReplaceAll(normalized, " ", "")),
		Description: fmt.Sprintf("API Management %s fixed-capacity unit", normalized),
		Query: prices.Query{
			ServiceName:   "API Management",
			ProductName:   "API Management",
			Region:        region,
			CurrencyCode:  currency,
			MeterName:     meter,
			RequireHourly: true,
		},
		QuantityDivisor: 1,
	}, true
}

// CosmosProvisioned returns the verified provisioned-throughput meter mapping.
func CosmosProvisioned(region, currency string) MeterSpec {
	return MeterSpec{
		Key:         "cosmos/provisioned-rus",
		Description: "Azure Cosmos DB provisioned throughput (100 RU/s hour)",
		Query: prices.Query{
			ServiceName:   "Azure Cosmos DB",
			ProductName:   "Azure Cosmos DB",
			Region:        region,
			CurrencyCode:  currency,
			MeterName:     "100 RU/s",
			RequireHourly: true,
		},
		QuantityDivisor: 100,
	}
}
