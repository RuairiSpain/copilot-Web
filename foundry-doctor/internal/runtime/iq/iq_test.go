package iq

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type staticCred struct{}

func (staticCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "token"}, nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEndpointRejectsHostileServiceNames(t *testing.T) {
	for _, name := range []string{"x.evil.com#", "..", "@", "svc:443", "Upper", "münchen"} {
		if _, err := Endpoint(name); err == nil {
			t.Fatalf("service %q accepted", name)
		}
	}
}

func TestParseConnectionEndpoint(t *testing.T) {
	serviceRoot, ok := ParseConnectionEndpoint("https://sample-search.search.windows.net")
	if !ok || serviceRoot.TargetKind != ConnectionTargetCognitiveSearchTarget || serviceRoot.SearchService != "sample-search" || serviceRoot.KnowledgeBase != "" || serviceRoot.APIVersion != "" {
		t.Fatalf("service root = %+v ok=%v", serviceRoot, ok)
	}
	kb, ok := ParseConnectionEndpoint("https://sample-search.search.windows.net/knowledgebases/sample-kb/mcp?api-version=2026-04-01")
	if !ok || kb.TargetKind != ConnectionTargetKnowledgeBaseMCP || kb.SearchService != "sample-search" || kb.KnowledgeBase != "sample-kb" || kb.APIVersion != "2026-04-01" {
		t.Fatalf("kb endpoint = %+v ok=%v", kb, ok)
	}
	if _, ok := ParseConnectionEndpoint("https://sample-search.search.windows.net/not-a-kb"); ok {
		t.Fatal("unexpectedly accepted non-knowledge-base path")
	}
}

func TestConnectionsFromAzureYAMLParsesHeadersAndTargets(t *testing.T) {
	doc, err := azureyaml.Parse([]byte(`name: sample
services:
  iq:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net/knowledgebases/sample-kb/mcp?api-version=2026-04-01
    authType: ProjectManagedIdentity
    headers:
      x-ms-query-source-authorization: "{user_token}"
  search:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net
    authType: ProjectManagedIdentity
    requestHeaders:
      x-ms-query-work-iq-source-authorization: "{user_token}"
`), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	conns := ConnectionsFromAzureYAML(&sdk.Input{AzureYAML: doc})
	if len(conns) != 2 {
		t.Fatalf("connections = %+v", conns)
	}
	if conns[0].Name != "iq" || conns[0].TargetKind != ConnectionTargetKnowledgeBaseMCP || !conns[0].ForwardSourceAuth || conns[0].ForwardWorkIQAuth {
		t.Fatalf("kb connection = %+v", conns[0])
	}
	if conns[0].APIVersion != "2026-04-01" {
		t.Fatalf("kb api version = %q", conns[0].APIVersion)
	}
	if conns[1].Name != "search" || conns[1].TargetKind != ConnectionTargetCognitiveSearchTarget || conns[1].KnowledgeBase != "" || conns[1].ForwardSourceAuth || !conns[1].ForwardWorkIQAuth {
		t.Fatalf("service connection = %+v", conns[1])
	}
	if !strings.EqualFold(conns[1].SearchService, "sample-search") {
		t.Fatalf("unexpected search service %q", conns[1].SearchService)
	}
}

func TestConnectionUsesSecretDetectsSQLAuth(t *testing.T) {
	if !ConnectionUsesSecret("Server=tcp:sql.database.windows.net;Database=db;User ID=reader;Password=supersecret") {
		t.Fatal("expected SQL auth connection string to be classified as secret-backed")
	}
	if ConnectionUsesSecret("Database=db;ResourceId=/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Sql/servers/sql") {
		t.Fatal("expected resource-id connection string to remain secretless")
	}
}

func TestHTTPClientGetKnowledgeSourceUsesRequestedVersionAndNestedIngestion(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
  "name":"blob-src",
  "kind":"azureBlob",
  "azureBlobParameters":{
    "connectionString":"ResourceId=/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/docs;AccountKey=secret",
    "isADLSGen2":true,
    "ingestionParameters":{
      "assetStore":{"container":"cache"},
      "ingestionPermissionOptions":["document"],
      "ingestionSchedule":{"interval":"PT10M","startTime":"2026-10-04T00:00:00Z"}
    }
  }
}`)
	}))
	defer srv.Close()

	hc := srv.Client()
	base := hc.Transport
	hc.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return base.RoundTrip(r)
	})
	c := HTTPClient{HTTP: hc, Credential: staticCred{}}
	got, err := c.GetKnowledgeSource(context.Background(), "svc", "blob-src", "2026-08-01-preview")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "api-version=2026-08-01-preview") {
		t.Fatalf("request uri = %q", gotPath)
	}
	if !got.HasSecretConnection || got.ResourceIDConnection == "" || !got.AssetStorePresent || len(got.IngestionPermissionOptions) != 1 || got.IngestionPermissionOptions[0] != "document" || got.RefreshSchedule == nil || got.RefreshSchedule.Interval != "PT10M" || got.APIVersion != "2026-08-01-preview" {
		t.Fatalf("knowledge source = %+v", got)
	}
}
