// Package search contains metadata-only Azure AI Search runtime probes.
//
// Shapes follow Azure/azure-rest-api-specs specification/search/data-plane/Search/stable/2026-04-01/search.json:
//   - SearchIndexerStatus.status is IndexerStatus: unknown | error | running.
//   - lastResult is IndexerExecutionResult; its status is IndexerExecutionStatus:
//     transientFailure | success | inProgress | reset. There is no "failed" value
//     and no errorCode property (free-text errorMessage and errors[] are never read).
//   - SearchIndex.vectorSearch.profiles[].name are the profiles that
//     SearchField.vectorSearchProfile must refer to.
package search

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

// Client exposes only metadata operations needed by the runtime rules.
type Client interface {
	GetIndex(context.Context, string, string) (Index, error)
	GetIndexStatistics(context.Context, string, string) (Stats, error)
	GetIndexerStatus(context.Context, string, string) (IndexerStatus, error)
}

// Index holds only safe metadata.
type Index struct {
	Name   string
	Fields []Field
	// VectorProfiles are vectorSearch.profiles[].name.
	VectorProfiles []string
}

type Field struct {
	Name                string
	Type                string
	Dimensions          int
	VectorSearchProfile string
}

type Stats struct {
	DocumentCount int64
	StorageSize   int64
	VectorSize    int64
}

// Indexer status values (IndexerStatus) and last-result values (IndexerExecutionStatus).
const (
	StatusUnknown          = "unknown"
	StatusError            = "error"
	StatusRunning          = "running"
	ResultTransientFailure = "transientFailure"
	ResultSuccess          = "success"
	ResultInProgress       = "inProgress"
	ResultReset            = "reset"
)

// FreshSuccessMinutes is the window in which a successful run is "recent":
// a zero document count right after a success may be statistics lag (the spec
// says statistics are collected periodically and may not be current).
const FreshSuccessMinutes = 60

type IndexerStatus struct {
	// Status is IndexerStatus (unknown, error, running).
	Status string
	// LastResultStatus is lastResult.status; empty when the indexer never ran.
	LastResultStatus string
	ItemsFailed      int64
	// HasSuccess is true when a successful execution with a parseable end time
	// exists in lastResult or executionHistory.
	HasSuccess bool
	// LastSuccessMinutes is the age of that success in minutes; meaningful only
	// when HasSuccess is true.
	LastSuccessMinutes int64
}

// HTTPClient implements Client using Search data-plane metadata endpoints.
type HTTPClient struct {
	HTTP       *http.Client
	Credential azure.TokenCredential
	// Now is the clock for freshness (default time.Now).
	Now func() time.Time
}

const scope = "https://search.azure.com/.default"

const hostSuffix = ".search.windows.net"

// objectNameRe bounds index and indexer names before they enter a URL. Search
// object names are letters, digits, dashes and underscores (up to 128).
var objectNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidObjectName reports whether name is safe to place in a Search URL.
func ValidObjectName(name string) bool { return objectNameRe.MatchString(name) }

func endpoint(service string) (string, error) {
	if err := runtime.ValidateDataPlaneName("search service", service); err != nil {
		return "", err
	}
	return "https://" + service + hostSuffix, nil
}

func objectURL(service, collection, name, suffix string) (string, error) {
	base, err := endpoint(service)
	if err != nil {
		return "", err
	}
	if !ValidObjectName(name) {
		return "", fmt.Errorf("invalid %s name %q", collection, name)
	}
	return base + "/" + collection + "('" + url.PathEscape(name) + "')" + suffix + "?api-version=2026-04-01", nil
}

func (c HTTPClient) GetIndex(ctx context.Context, service, index string) (Index, error) {
	u, err := objectURL(service, "indexes", index, "")
	if err != nil {
		return Index{}, err
	}
	var resp struct {
		Name   string `json:"name"`
		Fields []struct {
			Name                string `json:"name"`
			Type                string `json:"type"`
			Dimensions          int    `json:"dimensions"`
			VectorSearchProfile string `json:"vectorSearchProfile"`
		} `json:"fields"`
		VectorSearch struct {
			Profiles []struct {
				Name string `json:"name"`
			} `json:"profiles"`
		} `json:"vectorSearch"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, hostSuffix, nil, &resp); err != nil {
		return Index{}, err
	}
	out := Index{Name: resp.Name}
	for _, f := range resp.Fields {
		out.Fields = append(out.Fields, Field{Name: f.Name, Type: f.Type, Dimensions: f.Dimensions, VectorSearchProfile: f.VectorSearchProfile})
	}
	for _, p := range resp.VectorSearch.Profiles {
		out.VectorProfiles = append(out.VectorProfiles, p.Name)
	}
	return out, nil
}

func (c HTTPClient) GetIndexStatistics(ctx context.Context, service, index string) (Stats, error) {
	u, err := objectURL(service, "indexes", index, "/search.stats")
	if err != nil {
		return Stats{}, err
	}
	var resp struct {
		DocumentCount int64 `json:"documentCount"`
		StorageSize   int64 `json:"storageSize"`
		VectorSize    int64 `json:"vectorIndexSize"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, hostSuffix, nil, &resp); err != nil {
		return Stats{}, err
	}
	return Stats{DocumentCount: resp.DocumentCount, StorageSize: resp.StorageSize, VectorSize: resp.VectorSize}, nil
}

type execution struct {
	Status      string `json:"status"`
	ItemsFailed int64  `json:"itemsFailed"`
	EndTime     string `json:"endTime"`
}

func (c HTTPClient) GetIndexerStatus(ctx context.Context, service, indexer string) (IndexerStatus, error) {
	u, err := objectURL(service, "indexers", indexer, "/search.status")
	if err != nil {
		return IndexerStatus{}, err
	}
	var resp struct {
		Status           string      `json:"status"`
		LastResult       *execution  `json:"lastResult"`
		ExecutionHistory []execution `json:"executionHistory"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, hostSuffix, nil, &resp); err != nil {
		return IndexerStatus{}, err
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	out := IndexerStatus{Status: resp.Status}
	if resp.LastResult != nil {
		out.LastResultStatus = resp.LastResult.Status
		out.ItemsFailed = resp.LastResult.ItemsFailed
	}
	if ts, ok := latestSuccess(resp.LastResult, resp.ExecutionHistory); ok {
		out.HasSuccess = true
		out.LastSuccessMinutes = max(int64(now().Sub(ts)/time.Minute), 0)
	}
	return out, nil
}

// HasUsableVectorField reports whether idx exposes at least one vector field.
func HasUsableVectorField(idx Index) bool {
	for _, f := range idx.Fields {
		if strings.HasPrefix(f.Type, "Collection(") && f.Dimensions >= 2 && f.Dimensions <= 4096 && f.VectorSearchProfile != "" {
			return true
		}
	}
	return false
}

// UnresolvedProfiles returns the vectorSearchProfile names that vector fields
// use but that vectorSearch.profiles does not define. When the index defines no
// profiles at all every referenced profile is unresolved.
func UnresolvedProfiles(idx Index) []string {
	have := map[string]bool{}
	for _, p := range idx.VectorProfiles {
		have[p] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range idx.Fields {
		if f.VectorSearchProfile != "" && !have[f.VectorSearchProfile] && !seen[f.VectorSearchProfile] {
			seen[f.VectorSearchProfile] = true
			out = append(out, f.VectorSearchProfile)
		}
	}
	return out
}

// MissingSchema returns a deterministic schema failure message.
func MissingSchema(idx Index) string {
	return fmt.Sprintf("index %s lacks a vector field with dimensions and vectorSearchProfile", idx.Name)
}

// latestSuccess returns the end time of the most recent successful execution.
func latestSuccess(last *execution, history []execution) (time.Time, bool) {
	var best time.Time
	found := false
	consider := func(e execution) {
		if !strings.EqualFold(e.Status, ResultSuccess) {
			return
		}
		ts, err := time.Parse(time.RFC3339, e.EndTime)
		if err != nil {
			return
		}
		if !found || ts.After(best) {
			best, found = ts, true
		}
	}
	if last != nil {
		consider(*last)
	}
	for _, h := range history {
		consider(h)
	}
	return best, found
}
