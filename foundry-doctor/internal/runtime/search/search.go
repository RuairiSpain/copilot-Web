// Package search contains metadata-only Azure AI Search runtime probes.
package search

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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

type IndexerStatus struct {
	Status           string
	LastResultStatus string
	ItemsFailed      int64
	SafeErrorCode    string
	LastSuccessAgo   int64
}

// HTTPClient implements Client using Search data-plane metadata endpoints.
type HTTPClient struct {
	HTTP       *http.Client
	Credential azure.TokenCredential
}

const scope = "https://search.azure.com/.default"

func endpoint(service string) string { return "https://" + service + ".search.windows.net" }

func (c HTTPClient) GetIndex(ctx context.Context, service, index string) (Index, error) {
	u := endpoint(service) + "/indexes('" + url.PathEscape(index) + "')?api-version=2026-04-01"
	var resp struct {
		Name   string `json:"name"`
		Fields []struct {
			Name                string `json:"name"`
			Type                string `json:"type"`
			Dimensions          int    `json:"dimensions"`
			VectorSearchProfile string `json:"vectorSearchProfile"`
		} `json:"fields"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, nil, &resp); err != nil {
		return Index{}, err
	}
	out := Index{Name: resp.Name}
	for _, f := range resp.Fields {
		out.Fields = append(out.Fields, Field{Name: f.Name, Type: f.Type, Dimensions: f.Dimensions, VectorSearchProfile: f.VectorSearchProfile})
	}
	return out, nil
}

func (c HTTPClient) GetIndexStatistics(ctx context.Context, service, index string) (Stats, error) {
	u := endpoint(service) + "/indexes('" + url.PathEscape(index) + "')/search.stats?api-version=2026-04-01"
	var resp struct {
		DocumentCount int64 `json:"documentCount"`
		StorageSize   int64 `json:"storageSize"`
		VectorSize    int64 `json:"vectorIndexSize"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, nil, &resp); err != nil {
		return Stats{}, err
	}
	return Stats{DocumentCount: resp.DocumentCount, StorageSize: resp.StorageSize, VectorSize: resp.VectorSize}, nil
}

func (c HTTPClient) GetIndexerStatus(ctx context.Context, service, indexer string) (IndexerStatus, error) {
	u := endpoint(service) + "/indexers('" + url.PathEscape(indexer) + "')/search.status?api-version=2026-04-01"
	var resp struct {
		Status     string `json:"status"`
		LastResult struct {
			Status       string `json:"status"`
			ItemsFailed  int64  `json:"itemsFailed"`
			ErrorMessage string `json:"errorMessage"`
			ErrorCode    string `json:"errorCode"`
			EndTime      string `json:"endTime"`
		} `json:"lastResult"`
		ExecutionHistory []struct {
			Status  string `json:"status"`
			EndTime string `json:"endTime"`
		} `json:"executionHistory"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, nil, &resp); err != nil {
		return IndexerStatus{}, err
	}
	code := resp.LastResult.ErrorCode
	if code == "" && resp.LastResult.ErrorMessage != "" {
		code = "search-indexer-error"
	}
	lastSuccessAgo := int64(0)
	if ts := latestSuccess(resp.LastResult.Status, resp.LastResult.EndTime, resp.ExecutionHistory); !ts.IsZero() {
		lastSuccessAgo = int64(time.Since(ts).Hours())
	}
	return IndexerStatus{
		Status:           resp.Status,
		LastResultStatus: resp.LastResult.Status,
		ItemsFailed:      resp.LastResult.ItemsFailed,
		SafeErrorCode:    strings.TrimSpace(code),
		LastSuccessAgo:   lastSuccessAgo,
	}, nil
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

// MissingSchema returns a deterministic schema failure message.
func MissingSchema(idx Index) string {
	return fmt.Sprintf("index %s lacks a vector field with dimensions and vectorSearchProfile", idx.Name)
}

func latestSuccess(status, end string, history []struct {
	Status  string `json:"status"`
	EndTime string `json:"endTime"`
}) time.Time {
	if strings.EqualFold(status, "success") {
		if ts, err := time.Parse(time.RFC3339, end); err == nil {
			return ts
		}
	}
	for _, h := range history {
		if !strings.EqualFold(h.Status, "success") {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, h.EndTime); err == nil {
			return ts
		}
	}
	return time.Time{}
}
