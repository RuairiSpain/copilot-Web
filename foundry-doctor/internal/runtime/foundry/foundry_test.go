package foundry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

type staticCred struct{}

func (staticCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "token"}, nil
}

func TestEndpoint(t *testing.T) {
	got, err := Endpoint("acct", "proj")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://acct.services.ai.azure.com/api/projects/proj"
	if got != want {
		t.Fatalf("Endpoint() = %q, want %q", got, want)
	}
}

func TestEndpointRejectsHostileNames(t *testing.T) {
	for _, tc := range []struct {
		account string
		project string
	}{
		{account: "x.evil.com#", project: "proj"},
		{account: "acct", project: ".."},
		{account: "acct:443", project: "proj"},
		{account: "Upper", project: "proj"},
		{account: "acct", project: "münchen"},
	} {
		if _, err := Endpoint(tc.account, tc.project); err == nil {
			t.Fatalf("accepted account=%q project=%q", tc.account, tc.project)
		}
	}
}

func TestHTTPClientReadsMetadataOnly(t *testing.T) {
	dir := filepath.Join("testdata")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/agents/support-agent/versions/v2"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "agent_version.json")))
		case strings.HasSuffix(r.URL.Path, "/agents"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "agents.json")))
		case strings.HasSuffix(r.URL.Path, "/connections"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "connections.json")))
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
	endpoint, err := Endpoint("acct", "proj")
	if err != nil {
		t.Fatal(err)
	}
	agents, err := c.ListAgents(context.Background(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Name != "support-agent" || strings.Contains(fmt.Sprint(agents), "PROMPT_SENTINEL_TEXT") {
		t.Fatalf("agents = %+v", agents)
	}
	version, err := c.GetAgentVersion(context.Background(), endpoint, "support-agent", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if version.Status != "Failed" || strings.Contains(fmt.Sprint(version), "COMPLETION_SENTINEL_TEXT") {
		t.Fatalf("version = %+v", version)
	}
	conns, err := c.ListConnections(context.Background(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 || conns[0].Name != "search-conn" || strings.Contains(fmt.Sprint(conns), "DOCUMENT_SENTINEL_TEXT") {
		t.Fatalf("connections = %+v", conns)
	}
	if err := c.Ping(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetAgentVersion(context.Background(), endpoint, "support-agent", ""); err == nil {
		t.Fatal("expected missing version error")
	}
}

func TestProbeWrapper(t *testing.T) {
	p := Probe{
		ProbeID:    "probe-1",
		ProbeClass: runtime.ClassDataPlane,
		ProbeTime:  time.Second,
		RunFunc: func(context.Context) (runtime.Result, error) {
			return runtime.Result{State: runtime.StatePass}, nil
		},
	}
	if p.ID() != "probe-1" || p.Class() != runtime.ClassDataPlane || p.Timeout() != time.Second {
		t.Fatalf("probe metadata mismatch: %+v", p)
	}
	res, err := p.Run(context.Background())
	if err != nil || res.State != runtime.StatePass {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
