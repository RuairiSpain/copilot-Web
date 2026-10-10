package azure

import "context"

// SearchServiceInfo is a secret-free control-plane projection used by Phase 8
// rules when wiring live metadata probes.
type SearchServiceInfo struct {
	Name               string
	ResourceID         string
	Location           string
	SKU                string
	SemanticSearch     string
	KnowledgeRetrieval string
	IdentityType       string
}

// SearchManagement is the narrow allow-listed Phase 8 contract for Search
// management metadata.
type SearchManagement interface {
	GetSearchService(ctx context.Context, subscriptionID, resourceGroup, name string) (SearchServiceInfo, error)
}
