// Package catalog is the supported-meter catalogue: how a fixed-capacity
// driver maps to a public retail meter. Every mapping records whether its
// meter name was seen in a Retail Prices API response (Verified) or follows
// the naming pattern only (UNVERIFIED). A wrong meter name yields "no retail
// price" for the line, never a guessed price.
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
	// Verified is the dated evidence for the meter name, or "" when the
	// name follows the naming pattern only (UNVERIFIED).
	Verified string
}

const (
	searchEvidence = "meter name seen in a Retail Prices API response for serviceName 'Azure Cognitive Search', productName 'Azure AI Search' (prices.azure.com, 2026-10-18)"
	apimEvidence   = "meter name seen in a Retail Prices API response for serviceName and productName 'API Management' (prices.azure.com, 2026-10-18)"
)

type meter struct {
	name     string
	verified bool
}

// searchMeters maps sku.name to the dedicated-unit meter. "standard" is the
// S1 tier (meter "Standard S1 Unit").
var searchMeters = map[string]meter{
	"basic":                {"Basic Unit", true},
	"standard":             {"Standard S1 Unit", true},
	"standard2":            {"Standard S2 Unit", false},
	"standard3":            {"Standard S3 Unit", true},
	"storage_optimized_l1": {"Storage Optimized L1 Unit", false},
	"storage_optimized_l2": {"Storage Optimized L2 Unit", false},
}

// Search returns the Azure AI Search dedicated-tier mapping for the default
// compute type. Confidential compute uses separate "... CC Unit" meters and
// is not mapped.
func Search(region, currency, sku string) (MeterSpec, bool) {
	key := strings.ToLower(strings.TrimSpace(sku))
	m, ok := searchMeters[key]
	if !ok {
		return MeterSpec{}, false
	}
	spec := MeterSpec{
		Key:         "search/" + key,
		Description: fmt.Sprintf("Azure AI Search %s dedicated unit", sku),
		Query: prices.Query{
			ServiceName:   "Azure Cognitive Search",
			ProductName:   "Azure AI Search",
			Region:        region,
			CurrencyCode:  currency,
			MeterName:     m.name,
			RequireHourly: true,
		},
		QuantityDivisor: 1,
	}
	if m.verified {
		spec.Verified = searchEvidence
	}
	return spec, true
}

// apimMeters maps a lower-case sku.name to its unit meter. PremiumV2 and any
// other value are not mapped.
var apimMeters = map[string]meter{
	"developer":  {"Developer Unit", true},
	"basic":      {"Basic Unit", false},
	"standard":   {"Standard Unit", true},
	"premium":    {"Premium Unit", false},
	"isolated":   {"Isolated Unit", false},
	"basicv2":    {"Basic v2 Unit", true},
	"standardv2": {"Standard v2 Unit", false},
}

// APIM returns the API Management fixed-capacity mapping.
func APIM(region, currency, sku string) (MeterSpec, bool) {
	key := strings.ToLower(strings.TrimSpace(sku))
	m, ok := apimMeters[key]
	if !ok {
		return MeterSpec{}, false
	}
	spec := MeterSpec{
		Key:         "apim/" + key,
		Description: fmt.Sprintf("API Management %s fixed-capacity unit", strings.TrimSuffix(m.name, " Unit")),
		Query: prices.Query{
			ServiceName:   "API Management",
			ProductName:   "API Management",
			Region:        region,
			CurrencyCode:  currency,
			MeterName:     m.name,
			RequireHourly: true,
		},
		QuantityDivisor: 1,
	}
	if m.verified {
		spec.Verified = apimEvidence
	}
	return spec, true
}

// CosmosProvisioned returns the standard (manual) provisioned-throughput
// mapping for a single-region-write account region. The meter name "100
// RU/s" is UNVERIFIED (no recorded API response); the per-100 RU/s hourly
// billing is documented on the Cosmos DB standard-provisioned pricing page.
func CosmosProvisioned(region, currency string) MeterSpec {
	return MeterSpec{
		Key:         "cosmos/provisioned-rus",
		Description: "Azure Cosmos DB standard provisioned throughput (100 RU/s hour)",
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
