package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

type staticCred struct{}

func (staticCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "token"}, nil
}

var fixedNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func TestHasUsableVectorField(t *testing.T) {
	if !HasUsableVectorField(Index{
		Name:   "good",
		Fields: []Field{{Name: "vector", Type: "Collection(Edm.Single)", Dimensions: 1536, VectorSearchProfile: "default"}},
	}) {
		t.Fatal("expected vector field to be accepted")
	}
	if HasUsableVectorField(Index{
		Name:   "bad",
		Fields: []Field{{Name: "vector", Type: "Collection(Edm.Single)", Dimensions: 0, VectorSearchProfile: ""}},
	}) {
		t.Fatal("expected bad vector field to be rejected")
	}
	if msg := MissingSchema(Index{Name: "bad"}); !strings.Contains(msg, "bad") {
		t.Fatalf("missing schema = %q", msg)
	}
}

func TestUnresolvedProfiles(t *testing.T) {
	idx := Index{
		Fields:         []Field{{Name: "a", VectorSearchProfile: "p1"}, {Name: "b", VectorSearchProfile: "ghost"}, {Name: "c", VectorSearchProfile: "ghost"}, {Name: "d"}},
		VectorProfiles: []string{"p1"},
	}
	if got := UnresolvedProfiles(idx); !reflect.DeepEqual(got, []string{"ghost"}) {
		t.Errorf("got %v", got)
	}
	idx.VectorProfiles = nil
	if got := UnresolvedProfiles(idx); !reflect.DeepEqual(got, []string{"p1", "ghost"}) {
		t.Errorf("no profiles defined: got %v", got)
	}
	if got := UnresolvedProfiles(Index{Fields: []Field{{Name: "id"}}}); len(got) != 0 {
		t.Errorf("no vector fields: got %v", got)
	}
}

func fixtureClient(t *testing.T, statusBody string) HTTPClient {
	t.Helper()
	dir := filepath.Join("testdata")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexes('knowledge')/search.stats"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "stats.json")))
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexes('knowledge')"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "index.json")))
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexers('load-knowledge')/search.status"):
			if statusBody != "" {
				fmt.Fprint(w, statusBody)
				return
			}
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "indexer_status.json")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	hc := srv.Client()
	base := hc.Transport
	hc.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return base.RoundTrip(r)
	})
	return HTTPClient{HTTP: hc, Credential: staticCred{}, Now: func() time.Time { return fixedNow }}
}

func TestHTTPClientReadsMetadataOnly(t *testing.T) {
	c := fixtureClient(t, "")
	idx, err := c.GetIndex(context.Background(), "svc", "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if idx.Name != "knowledge" || len(idx.Fields) != 3 || !reflect.DeepEqual(idx.VectorProfiles, []string{"default"}) || strings.Contains(fmt.Sprint(idx), "DOCUMENT_SENTINEL_TEXT") {
		t.Fatalf("index = %+v", idx)
	}
	if got := UnresolvedProfiles(idx); !reflect.DeepEqual(got, []string{"undefined-profile"}) {
		t.Errorf("unresolved = %v", got)
	}
	stats, err := c.GetIndexStatistics(context.Background(), "svc", "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if stats.DocumentCount != 8 {
		t.Fatalf("stats = %+v", stats)
	}
	status, err := c.GetIndexerStatus(context.Background(), "svc", "load-knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if status.LastResultStatus != ResultSuccess || status.Status != StatusRunning || !status.HasSuccess || status.LastSuccessMinutes != 5 || strings.Contains(fmt.Sprint(status), "DOCUMENT_SENTINEL_TEXT") {
		t.Fatalf("status = %+v", status)
	}
}

func TestIndexerStatusFreshnessAndNeverSucceeded(t *testing.T) {
	cases := []struct {
		name, body    string
		hasSuccess    bool
		minutes       int64
		lastResult    string
		itemsFailed   int64
		overallStatus string
	}{
		{"never ran", `{"name":"x","status":"unknown","executionHistory":[],"limits":{}}`, false, 0, "", 0, "unknown"},
		{"only failures", `{"name":"x","status":"error","lastResult":{"status":"transientFailure","itemsFailed":4,"endTime":"2026-10-17T11:00:00Z","errors":[],"warnings":[],"itemsProcessed":0},"executionHistory":[{"status":"transientFailure","endTime":"2026-10-17T11:00:00Z","errors":[],"warnings":[],"itemsProcessed":0,"itemsFailed":4}],"limits":{}}`, false, 0, "transientFailure", 4, "error"},
		{"success just now", `{"name":"x","status":"running","lastResult":{"status":"success","endTime":"2026-10-17T12:00:00Z","errors":[],"warnings":[],"itemsProcessed":1,"itemsFailed":0},"executionHistory":[],"limits":{}}`, true, 0, "success", 0, "running"},
		{"success in history only picks the newest", `{"name":"x","status":"running","lastResult":{"status":"inProgress","errors":[],"warnings":[],"itemsProcessed":0,"itemsFailed":0},"executionHistory":[{"status":"success","endTime":"2026-10-17T10:00:00Z","errors":[],"warnings":[],"itemsProcessed":1,"itemsFailed":0},{"status":"success","endTime":"2026-10-17T11:30:00Z","errors":[],"warnings":[],"itemsProcessed":1,"itemsFailed":0}],"limits":{}}`, true, 30, "inProgress", 0, "running"},
		{"unparseable end time is not a success", `{"name":"x","status":"running","lastResult":{"status":"success","endTime":"yesterday","errors":[],"warnings":[],"itemsProcessed":1,"itemsFailed":0},"executionHistory":[],"limits":{}}`, false, 0, "success", 0, "running"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, tc.body)
			st, err := c.GetIndexerStatus(context.Background(), "svc", "load-knowledge")
			if err != nil {
				t.Fatal(err)
			}
			if st.HasSuccess != tc.hasSuccess || st.LastSuccessMinutes != tc.minutes || st.LastResultStatus != tc.lastResult || st.ItemsFailed != tc.itemsFailed || st.Status != tc.overallStatus {
				t.Errorf("status = %+v", st)
			}
		})
	}
}

func TestHTTPClientIgnoresFreeTextIndexerErrors(t *testing.T) {
	c := fixtureClient(t, `{"name":"x","status":"error","lastResult":{"status":"transientFailure","itemsFailed":2,"errorMessage":"SECRET-DETAILS","errorCode":"invented","errors":[{"errorMessage":"SECRET-DETAILS"}],"warnings":[],"itemsProcessed":0},"executionHistory":[],"limits":{}}`)
	status, err := c.GetIndexerStatus(context.Background(), "svc", "load-knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%+v", status), "SECRET") || status.ItemsFailed != 2 || status.HasSuccess {
		t.Fatalf("status = %+v", status)
	}
}

func TestEndpointRejectsHostileServiceNames(t *testing.T) {
	for _, name := range []string{"x.evil.com#", "..", "@", "svc:443", "Upper", "münchen"} {
		if _, err := endpoint(name); err == nil {
			t.Fatalf("service %q accepted", name)
		}
	}
	got, err := endpoint("svc")
	if err != nil || got != "https://svc.search.windows.net" {
		t.Fatalf("endpoint = %q, %v", got, err)
	}
}

func TestObjectNamesAreValidatedBeforeAnyRequest(t *testing.T) {
	var calls int
	hc := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("network must not be used")
	})}
	c := HTTPClient{HTTP: hc, Credential: staticCred{}}
	for _, name := range []string{"", "x')/docs", "a/b", "a b", "x?y", "..", "a'", strings.Repeat("a", 129), "-lead"} {
		if _, err := c.GetIndex(context.Background(), "svc", name); err == nil {
			t.Errorf("index %q accepted", name)
		}
		if _, err := c.GetIndexStatistics(context.Background(), "svc", name); err == nil {
			t.Errorf("stats %q accepted", name)
		}
		if _, err := c.GetIndexerStatus(context.Background(), "svc", name); err == nil {
			t.Errorf("indexer %q accepted", name)
		}
	}
	if calls != 0 {
		t.Errorf("requests = %d", calls)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
