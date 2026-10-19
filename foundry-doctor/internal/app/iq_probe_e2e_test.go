package app

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	iqrules "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/iq"
	runtimeiq "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/iq"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// recordingTransport answers Search data-plane requests from the Learn-shaped
// fixtures in internal/runtime/iq/testdata and records what was asked.
type recordingTransport struct {
	mu   sync.Mutex
	reqs []*http.Request
	body map[string]string // path prefix -> fixture file
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	path := req.URL.Path
	for prefix, file := range r.body {
		if strings.HasPrefix(path, prefix) {
			data, err := os.ReadFile(filepath.Join("..", "runtime", "iq", "testdata", file))
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(data))), Request: req}, nil
		}
	}
	return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"not found"}}`)), Request: req}, nil
}

type scopeCredential struct {
	mu     sync.Mutex
	scopes []string
}

func (c *scopeCredential) Token(_ context.Context, scope string) (azure.AccessToken, error) {
	c.mu.Lock()
	c.scopes = append(c.scopes, scope)
	c.mu.Unlock()
	return azure.AccessToken{Token: "fake-search-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// Phase 8 gate (both reviewers, DoD 5): the real HTTP client, the URL and
// host checks, the snapshot builder and every FND-IQ rule run together on
// recorded Search responses. Nothing but metadata is asked for.
func TestIQProbeEndToEndOnRecordedResponses(t *testing.T) {
	rt := &recordingTransport{body: map[string]string{
		"/knowledgebases/my-kb":         "knowledgebase.json",
		"/knowledgesources/my-index-ks": "knowledgesource-searchindex.json",
		"/knowledgesources/my-blob-ks":  "knowledgesource-azureblob.json",
		"/indexes":                      "index.json",
	}}
	cred := &scopeCredential{}
	client := runtimeiq.HTTPClient{HTTP: &http.Client{Transport: rt, Timeout: 5 * time.Second}, Credential: cred}
	doc := testAzureYAML(t, `name: sample
services:
  iq:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net/knowledgebases/my-kb/mcp?api-version=2026-08-01-preview
    authType: ProjectManagedIdentity
`)
	in := RunInput{AzureYAML: doc, ARM: &phase8ARM{}}
	snap := buildPhase8Snapshot(context.Background(), in, client)
	if !snap.LiveRead {
		t.Fatalf("the probe did not read the snapshot: issues=%+v", snap.ProbeIssues)
	}

	// Metadata only: GET, https, one host, no key, only the four definition families.
	if len(rt.reqs) == 0 {
		t.Fatal("no request was made")
	}
	for _, req := range rt.reqs {
		if req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != "sample-search.search.windows.net" {
			t.Errorf("unexpected request %s %s", req.Method, req.URL)
		}
		if req.Header.Get("api-key") != "" || req.Header.Get("Authorization") != "Bearer fake-search-token" {
			t.Errorf("request must carry only the bearer token: %v", req.Header)
		}
		if q := req.URL.Query(); len(q) != 1 || q.Get("api-version") == "" {
			t.Errorf("only api-version may be sent as a query: %s", req.URL.RawQuery)
		}
		p := req.URL.Path
		if !(strings.HasPrefix(p, "/knowledgebases/") || strings.HasPrefix(p, "/knowledgesources/") || strings.HasPrefix(p, "/indexes(") || strings.HasPrefix(p, "/skillsets(")) {
			t.Errorf("path %q is not a definition read", p)
		}
		if strings.Contains(p, "/docs") || strings.Contains(p, "retrieve") {
			t.Errorf("path %q reads content", p)
		}
	}
	for _, sc := range cred.scopes {
		if sc != "https://search.azure.com/.default" {
			t.Errorf("token scope %q, want the Search scope", sc)
		}
	}

	// Every IQ rule runs on the snapshot without error, and no result carries a
	// stored filter value from the knowledge source.
	model := iqSnapshotModel{ARMModel: in.ARM, snap: snap}
	input := &sdk.Input{ARM: model, AzureYAML: doc, Profile: "foundry-prod"}
	for _, r := range iqrules.Register() {
		res, err := r.Evaluate(context.Background(), input)
		if err != nil {
			t.Fatalf("%s: %v", r.ID(), err)
		}
		for _, f := range res.Findings {
			if strings.Contains(f.Evidence, "accessScope eq") || strings.Contains(f.Evidence, "isPublished") {
				t.Errorf("%s evidence repeats a filter value: %s", r.ID(), f.Evidence)
			}
		}
	}
}
