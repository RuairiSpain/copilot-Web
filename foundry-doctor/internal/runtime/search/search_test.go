package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

type staticCred struct{}

func (staticCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "token"}, nil
}

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

func TestHTTPClientReadsMetadataOnly(t *testing.T) {
	dir := filepath.Join("testdata")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexes('knowledge')/search.stats"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "stats.json")))
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexes('knowledge')"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "index.json")))
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexers('load-knowledge')/search.status"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "indexer_status.json")))
		default:
			http.NotFound(w, r)
		}
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

	idx, err := c.GetIndex(context.Background(), "svc", "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if idx.Name != "knowledge" || strings.Contains(fmt.Sprint(idx), "DOCUMENT_SENTINEL_TEXT") {
		t.Fatalf("index = %+v", idx)
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
	if status.LastResultStatus != "success" || strings.Contains(fmt.Sprint(status), "DOCUMENT_SENTINEL_TEXT") {
		t.Fatalf("status = %+v", status)
	}
}

func TestLatestSuccess(t *testing.T) {
	ts := latestSuccess("success", "2026-10-05T10:00:00Z", nil)
	if ts.IsZero() {
		t.Fatal("expected timestamp")
	}
	hist := []struct {
		Status  string `json:"status"`
		EndTime string `json:"endTime"`
	}{{Status: "success", EndTime: "2026-10-05T10:00:00Z"}}
	if latestSuccess("error", "", hist).IsZero() {
		t.Fatal("expected history fallback")
	}
	if !latestSuccess("error", "", nil).IsZero() {
		t.Fatal("expected zero time")
	}
	got, err := endpoint("svc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://svc.search.windows.net" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestHTTPClientIgnoresFreeTextIndexerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"running","lastResult":{"status":"failed","itemsFailed":2,"errorMessage":"details"}}`)
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
	status, err := c.GetIndexerStatus(context.Background(), "svc", "load-knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if status.SafeErrorCode != "" || status.LastSuccessAgo != 0 {
		t.Fatalf("status = %+v", status)
	}
}

func TestEndpointRejectsHostileServiceNames(t *testing.T) {
	for _, name := range []string{"x.evil.com#", "..", "@", "svc:443", "Upper", "münchen"} {
		if _, err := endpoint(name); err == nil {
			t.Fatalf("service %q accepted", name)
		}
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
