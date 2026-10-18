package iq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestConnectionsFromAzureYAMLParsesTargets(t *testing.T) {
	doc, err := azureyaml.Parse([]byte(`name: sample
services:
  iq:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net/knowledgebases/sample-kb/mcp?api-version=2026-08-01-preview
    authType: ProjectManagedIdentity
    category: RemoteTool
    audience: https://search.azure.com/
  search:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net
    authType: ProjectManagedIdentity
  pending:
    host: azure.ai.connection
    target: https://${SEARCH_NAME}.search.windows.net/knowledgebases/kb/mcp
    authType: ProjectManagedIdentity
  other:
    host: azure.ai.connection
    target: ${OPENAI_ENDPOINT}
    authType: ApiKey
`), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	conns := ConnectionsFromAzureYAML(&sdk.Input{AzureYAML: doc})
	if len(conns) != 3 {
		t.Fatalf("connections = %+v", conns)
	}
	if conns[0].Name != "iq" || conns[0].TargetKind != ConnectionTargetKnowledgeBaseMCP || conns[0].APIVersion != "2026-08-01-preview" ||
		conns[0].Category != "RemoteTool" || conns[0].Audience != "https://search.azure.com/" {
		t.Fatalf("kb connection = %+v", conns[0])
	}
	if conns[1].Name != "pending" || conns[1].TargetKind != ConnectionTargetUnresolved {
		t.Fatalf("unresolved connection = %+v", conns[1])
	}
	if conns[2].Name != "search" || conns[2].TargetKind != ConnectionTargetCognitiveSearchTarget || conns[2].KnowledgeBase != "" {
		t.Fatalf("service connection = %+v", conns[2])
	}
}

func TestParseConnectionEndpointRejectsUnsafeKnowledgeBaseNames(t *testing.T) {
	for _, raw := range []string{
		"https://svc.search.windows.net/knowledgebases/a%2Fb/mcp",
		"https://svc.search.windows.net/knowledgebases/a%20b/mcp",
		"https://svc.search.windows.net/knowledgebases/%27x%27/mcp",
	} {
		if _, ok := ParseConnectionEndpoint(raw); ok {
			t.Fatalf("accepted %q", raw)
		}
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

func fixtureServer(t *testing.T, body string, gotPath *string, calls *int) HTTPClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		*gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	hc := srv.Client()
	base := hc.Transport
	hc.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return base.RoundTrip(r)
	})
	return HTTPClient{HTTP: hc, Credential: staticCred{}}
}

func mustFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var knownVersions = []string{"2026-04-01", "2026-05-01-preview", "2026-08-01-preview"}

func TestHTTPClientGetKnowledgeSourceUsesRequestedVersionAndNestedIngestion(t *testing.T) {
	var gotPath string
	var calls int
	c := fixtureServer(t, `{
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
}`, &gotPath, &calls)
	got, err := c.GetKnowledgeSource(context.Background(), "svc", "blob-src", "2026-08-01-preview")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotPath, "/knowledgesources/blob-src?") || !strings.Contains(gotPath, "api-version=2026-08-01-preview") {
		t.Fatalf("request uri = %q", gotPath)
	}
	if !got.HasSecretConnection || got.ResourceIDConnection == "" || !got.AssetStorePresent || len(got.IngestionPermissionOptions) != 1 || got.IngestionPermissionOptions[0] != "document" || got.RefreshSchedule == nil || got.RefreshSchedule.Interval != "PT10M" || got.APIVersion != "2026-08-01-preview" {
		t.Fatalf("knowledge source = %+v", got)
	}
}

// Contract tests: the Learn response shapes (read 2026-10-18) parse the same
// way under every verified API version.
func TestKnowledgeSourceContractShapes(t *testing.T) {
	for _, version := range knownVersions {
		t.Run(version, func(t *testing.T) {
			var path string
			var calls int

			blob, err := fixtureServer(t, mustFixture(t, "knowledgesource-azureblob.json"), &path, &calls).GetKnowledgeSource(context.Background(), "svc", "my-blob-ks", version)
			if err != nil {
				t.Fatal(err)
			}
			if !blob.ParametersRead || !blob.IngestionRead || blob.ResourceIDConnection == "" || blob.HasSecretConnection || blob.ConnectionUnclassified ||
				!blob.IsADLSGen2 || blob.AssetStorePresent || blob.GeneratedIndexName != "my-blob-ks-index" || blob.SkillsetName != "my-blob-ks-skillset" ||
				blob.BackingIndex() != "my-blob-ks-index" || len(blob.IngestionPermissionOptions) != 2 {
				t.Fatalf("blob = %+v", blob)
			}
			if len(blob.Models) != 3 {
				t.Fatalf("blob models = %+v", blob.Models)
			}
			if blob.Models[0].APIKeySet || blob.Models[0].Path != "azureBlobParameters.ingestionParameters.embeddingModel" {
				t.Fatalf("embedding model = %+v", blob.Models[0])
			}
			if !blob.Models[1].APIKeySet || blob.Models[1].Path != "azureBlobParameters.ingestionParameters.chatCompletionModel" {
				t.Fatalf("chat model apiKey inside azureOpenAIParameters not detected: %+v", blob.Models[1])
			}
			if blob.Models[2].Kind != "aiServices" || blob.Models[2].APIKeySet {
				t.Fatalf("aiServices = %+v", blob.Models[2])
			}

			sp, err := fixtureServer(t, mustFixture(t, "knowledgesource-indexedsharepoint.json"), &path, &calls).GetKnowledgeSource(context.Background(), "svc", "my-indexed-sharepoint-ks", version)
			if err != nil {
				t.Fatal(err)
			}
			if !sp.ParametersRead || !sp.IngestionRead || !sp.ConnectionUnclassified || sp.HasSecretConnection ||
				len(sp.IngestionPermissionOptions) != 2 || sp.GeneratedIndexName != "my-indexed-sharepoint-ks-index" || sp.AssetStorePresent {
				t.Fatalf("sharepoint = %+v", sp)
			}

			lake, err := fixtureServer(t, mustFixture(t, "knowledgesource-indexedonelake.json"), &path, &calls).GetKnowledgeSource(context.Background(), "svc", "my-onelake-ks", version)
			if err != nil {
				t.Fatal(err)
			}
			if !lake.ParametersRead || !lake.IngestionRead || len(lake.IngestionPermissionOptions) != 0 || lake.RefreshSchedule == nil ||
				lake.RefreshSchedule.Interval != "PT1H" || lake.SkillsetName != "my-onelake-ks-skillset" {
				t.Fatalf("onelake = %+v", lake)
			}
			if len(lake.Models) != 2 || !lake.Models[1].APIKeySet || lake.Models[1].Path != "indexedOneLakeParameters.ingestionParameters.aiServices" {
				t.Fatalf("onelake models = %+v", lake.Models)
			}

			idx, err := fixtureServer(t, mustFixture(t, "knowledgesource-searchindex.json"), &path, &calls).GetKnowledgeSource(context.Background(), "svc", "my-index-ks", version)
			if err != nil {
				t.Fatal(err)
			}
			if idx.SearchIndexName != "hotels" || idx.BaseFilter == "" || len(idx.FilterFields) != 1 || idx.IngestionRead {
				t.Fatalf("searchIndex = %+v", idx)
			}
		})
	}
}

func TestKnowledgeBaseAndIndexContractShapes(t *testing.T) {
	for _, version := range knownVersions {
		t.Run(version, func(t *testing.T) {
			var path string
			var calls int
			kb, err := fixtureServer(t, mustFixture(t, "knowledgebase.json"), &path, &calls).GetKnowledgeBase(context.Background(), "svc", "my-kb", version)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(path, "/knowledgebases/my-kb?") {
				t.Fatalf("request uri = %q", path)
			}
			if kb.RetrievalReasoningKind != "low" || len(kb.KnowledgeSources) != 2 || len(kb.Models) != 1 || kb.Models[0].APIKeySet || !kb.Models[0].AuthIdentitySet {
				t.Fatalf("knowledge base = %+v", kb)
			}
			idx, err := fixtureServer(t, mustFixture(t, "index.json"), &path, &calls).GetIndex(context.Background(), "svc", "hotels", version)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(path, "/indexes('hotels')?") {
				t.Fatalf("request uri = %q", path)
			}
			if len(idx.Fields) != 4 || len(idx.Vectorizers) != 1 || !idx.Vectorizers[0].APIKeySet || idx.DefaultSemanticConfiguration != "default" {
				t.Fatalf("index = %+v", idx)
			}
		})
	}
}

func TestUnsupportedVersionOrNameSendsNothing(t *testing.T) {
	var path string
	var calls int
	c := fixtureServer(t, `{}`, &path, &calls)
	if _, err := c.GetKnowledgeBase(context.Background(), "svc", "kb", "2027-01-01-preview"); !errors.Is(err, ErrUnsupportedAPIVersion) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.GetKnowledgeSource(context.Background(), "svc", "ks", ""); !errors.Is(err, ErrUnsupportedAPIVersion) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.GetIndex(context.Background(), "svc", "../x", "2026-04-01"); !errors.Is(err, ErrInvalidObjectName) {
		t.Fatalf("err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("requests sent: %d", calls)
	}
}
