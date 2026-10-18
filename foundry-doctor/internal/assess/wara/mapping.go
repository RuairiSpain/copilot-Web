// Package wara holds reliability-specific WARA (APRL) references.
//
// Each ID must appear in the rule's own APRL source note in the catalogue
// (mapping_test.go cross-checks this). WARA is a recommendation set, not a
// verified platform fact; pgVerified records the APRL flag as noted in the
// catalogue ("unrecorded" when the note does not state it).
package wara

import (
	"slices"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
)

const (
	aprlSearch  = "https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Search/searchServices/recommendations.yaml"
	aprlStorage = "https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/Storage/storageAccounts/recommendations.yaml"
	aprlCosmos  = "https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/DocumentDB/databaseAccounts/recommendations.yaml"
	aprlCogSvc  = "https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/CognitiveServices/accounts/recommendations.yaml"
	aprlAPIM    = "https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/ApiManagement/service/recommendations.yaml"
)

var references = map[string][]assess.WARARef{
	"FND-REL-001": {
		{ID: "b376281d-bfec-4695-8f90-9a44544fdfa4", Label: "AI Search replicas for availability zone support", PGVerified: "false", SourceURL: aprlSearch},
		{ID: "dff62efe-c3a3-4621-98b3-c877c65cb195", Label: "AI Search multi-region", PGVerified: "false", SourceURL: aprlSearch},
	},
	"FND-REL-002": {
		{ID: "e6c7e1cc-2f47-264d-aa50-1da421314472", Label: "Zone or region redundant storage", PGVerified: "true", SourceURL: aprlStorage},
	},
	"FND-REL-003": {
		{ID: "921631f6-ed59-49a5-94c1-f0f3ececa580", Label: "Cosmos DB availability zones", PGVerified: "false", SourceURL: aprlCosmos},
		{ID: "e544520b-8505-7841-9e77-1f1974ee86ec", Label: "Cosmos DB continuous backup", PGVerified: "true", SourceURL: aprlCosmos},
	},
	"FND-REL-007": {
		{ID: "dff62efe-c3a3-4621-98b3-c877c65cb195", Label: "AI Search multi-region", PGVerified: "false", SourceURL: aprlSearch},
		{ID: "43663217-a1d3-844b-80ea-571a2ce37c6c", Label: "Cosmos DB in at least two regions", PGVerified: "unrecorded", SourceURL: aprlCosmos},
		{ID: "9cabded7-a1fc-6e4a-944b-d7dd98ea31a2", Label: "Cosmos DB service-managed failover", PGVerified: "unrecorded", SourceURL: aprlCosmos},
		{ID: "61187af4-7d36-4b48-b16e-de78bef143a0", Label: "Azure OpenAI in multiple regions", PGVerified: "false", SourceURL: aprlCogSvc},
	},
	"FND-REL-008": {
		{ID: "0c193899-da60-4a52-b4a0-77d75ac8c5c5", Label: "Pay-as-you-go instance to manage provisioned overflow", PGVerified: "true", SourceURL: aprlCogSvc},
	},
	"FND-REL-009": {
		{ID: "baf3bfc0-32a2-4c0c-926d-c9bf0b49808e", Label: "API Management Premium for availability zones", PGVerified: "true", SourceURL: aprlAPIM},
		{ID: "740f2c1c-8857-4648-80eb-47d2c56d5a50", Label: "API Management availability zones", PGVerified: "true", SourceURL: aprlAPIM},
		{ID: "af4f88cb-35e8-4371-b29e-3a32b1d2f40a", Label: "API Management multi-region", PGVerified: "true", SourceURL: aprlAPIM},
		{ID: "e35cf148-8eee-49d1-a1c9-956160f99e0b", Label: "API Management stv2 platform (platform migration, context only)", PGVerified: "true", SourceURL: aprlAPIM},
	},
}

// Lookup returns the WARA references associated with ruleID.
func Lookup(ruleID string) []assess.WARARef {
	return slices.Clone(references[ruleID])
}

// Map returns a cloned copy of the rule-to-WARA mapping.
func Map() map[string][]assess.WARARef {
	out := make(map[string][]assess.WARARef, len(references))
	for k, v := range references {
		out[k] = slices.Clone(v)
	}
	return out
}
