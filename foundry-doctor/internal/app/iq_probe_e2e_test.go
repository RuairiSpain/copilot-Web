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
	mu     sync.Mutex
	reqs   []*http.Request
	body   map[string]string // path prefix -> fixture file
	status map[string]int    // path prefix -> status to answer instead of the fixture
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	path := req.URL.Path
	for prefix, code := range r.status {
		if strings.HasPrefix(path, prefix) {
			return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"denied or missing"}}`)), Request: req}, nil
		}
	}
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
		"/skillsets":                    "skillset.json",
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
	in := RunInput{AzureYAML: doc, ARM: searchARM()}
	snap := buildPhase8Snapshot(context.Background(), in, client)
	if len(snap.ProbeIssues) != 0 {
		t.Fatalf("every recorded object must be readable: %v", snap.ProbeIssues)
	}
	if len(snap.Bases) == 0 || len(snap.Sources) != 2 || len(snap.Indexes) == 0 || len(snap.Skillsets) == 0 {
		t.Fatalf("the snapshot is not populated: bases=%d sources=%d indexes=%d skillsets=%d", len(snap.Bases), len(snap.Sources), len(snap.Indexes), len(snap.Skillsets))
	}
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

	if len(cred.scopes) == 0 || len(cred.scopes) != len(rt.reqs) {
		t.Errorf("every request needs its own token: %d scopes for %d requests", len(cred.scopes), len(rt.reqs))
	}

	// Every IQ rule runs on the snapshot. None may be skipped because an object
	// was unreadable, the rules that have data must report, and no result may
	// repeat a stored filter value or a key.
	model := iqSnapshotModel{ARMModel: in.ARM, snap: snap}
	input := &sdk.Input{ARM: model, AzureYAML: doc, Profile: "foundry-prod"}
	results := map[string]sdk.Result{}
	for _, r := range iqrules.Register() {
		res, err := r.Evaluate(context.Background(), input)
		if err != nil {
			t.Fatalf("%s: %v", r.ID(), err)
		}
		results[r.ID()] = res
		if res.Skipped != nil && strings.Contains(res.Skipped.Reason, "input-unavailable") {
			t.Errorf("%s was skipped for an unreadable object: %s", r.ID(), res.Skipped.Reason)
		}
		for _, f := range res.Findings {
			for _, forbidden := range []string{"eq true", "'public'", "fake-search-token"} {
				if strings.Contains(f.Evidence, forbidden) {
					t.Errorf("%s evidence repeats %q: %s", r.ID(), forbidden, f.Evidence)
				}
			}
		}
	}
	keys := func(id string) map[string]bool {
		out := map[string]bool{}
		for _, f := range results[id].Findings {
			out[f.Key] = true
		}
		return out
	}
	// The recorded knowledge source filters on fields the recorded index does not
	// have (positive), and the recorded blob source carries an API key (positive).
	for _, want := range []string{"base-filter:accessScope", "base-filter:isPublished", "query-hint-filter:category"} {
		if !keys("FND-IQ-001")[want] {
			t.Errorf("FND-IQ-001 must report %s: %+v", want, results["FND-IQ-001"].Findings)
		}
	}
	apiKeys := 0
	for k := range keys("FND-IQ-008") {
		if strings.HasPrefix(k, "api-key:") {
			apiKeys++
		}
	}
	if apiKeys != 2 {
		t.Errorf("FND-IQ-008 must report the two recorded API keys, got %+v", results["FND-IQ-008"].Findings)
	}
	// Rules that have their data and find nothing (negative) are evaluated, not skipped.
	for _, id := range []string{"FND-IQ-003", "FND-IQ-004", "FND-IQ-006", "FND-IQ-007"} {
		if len(results[id].Findings) != 0 || results[id].Skipped != nil {
			t.Errorf("%s must pass on the recorded objects: %+v", id, results[id])
		}
	}
}

// A recorded 403 and a recorded 404 are different conditions. Neither may turn
// into a pass, and no rule may report a finding it could not support.
func TestIQProbeDeniedAndMissingAreSkipsNotPasses(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		want   string
	}{
		"forbidden": {http.StatusForbidden, "not readable"},
		"not found": {http.StatusNotFound, "was not found"},
	} {
		t.Run(name, func(t *testing.T) {
			rt := &recordingTransport{status: map[string]int{"/knowledgebases/": tc.status}}
			client := runtimeiq.HTTPClient{HTTP: &http.Client{Transport: rt, Timeout: 5 * time.Second}, Credential: &scopeCredential{}}
			doc := testAzureYAML(t, `name: sample
services:
  iq:
    host: azure.ai.connection
    target: https://sample-search.search.windows.net/knowledgebases/my-kb/mcp?api-version=2026-08-01-preview
    authType: ProjectManagedIdentity
`)
			in := RunInput{AzureYAML: doc, ARM: searchARM()}
			snap := buildPhase8Snapshot(context.Background(), in, client)
			if len(snap.ProbeIssues) == 0 || !strings.Contains(strings.Join(snap.ProbeIssues, " "), tc.want) {
				t.Fatalf("probe issues = %v, want one containing %q", snap.ProbeIssues, tc.want)
			}
			input := &sdk.Input{ARM: iqSnapshotModel{ARMModel: in.ARM, snap: snap}, AzureYAML: doc, Profile: "foundry-prod"}
			for _, r := range iqrules.Register() {
				res, err := r.Evaluate(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				if len(res.Findings) != 0 {
					t.Errorf("%s reported findings without data: %+v", r.ID(), res.Findings)
				}
				if res.Skipped == nil {
					t.Errorf("%s passed without any data", r.ID())
				}
			}
		})
	}
}
