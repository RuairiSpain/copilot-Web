package foundry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// fixtureClient serves the synthetic, spec-shaped fixtures (Foundry v1 openapi3
// AgentObject, PagedConnection) and records every request URL.
func fixtureClient(t *testing.T, extra func(w http.ResponseWriter, r *http.Request) bool) (HTTPClient, *[]string) {
	t.Helper()
	dir := filepath.Join("testdata")
	var urls []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		urls = append(urls, r.URL.String())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if extra != nil && extra(w, r) {
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/agents") && r.URL.Query().Get("after") == "agent_support":
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "agents_page2.json")))
		case strings.HasSuffix(r.URL.Path, "/agents"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "agents.json")))
		case strings.HasSuffix(r.URL.Path, "/connections") && r.URL.Query().Get("skiptoken") == "page2":
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "connections_page2.json")))
		case strings.HasSuffix(r.URL.Path, "/connections"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "connections.json")))
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
	return HTTPClient{HTTP: hc, Credential: staticCred{}}, &urls
}

func TestListAgentsFollowsHasMoreAndReadsLatestVersion(t *testing.T) {
	c, urls := fixtureClient(t, nil)
	endpoint, _ := Endpoint("acct", "proj")
	got, err := c.ListAgents(context.Background(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if got.Truncated || len(got.Agents) != 2 {
		t.Fatalf("agents = %+v", got)
	}
	a := got.Agents[0]
	if a.Name != "support-agent" || a.LatestVersion != "2" || a.LatestStatus != "failed" || a.State != "enabled" {
		t.Errorf("agent 0 = %+v", a)
	}
	b := got.Agents[1]
	if b.Name != "other-agent" || b.LatestStatus != "" || b.State != "disabled" {
		t.Errorf("agent 1 (absent status) = %+v", b)
	}
	if strings.Contains(fmt.Sprint(got), "PROMPT_SENTINEL_TEXT") || strings.Contains(fmt.Sprint(got), "COMPLETION_SENTINEL_TEXT") {
		t.Errorf("content leaked into %+v", got)
	}
	if len(*urls) != 2 || !strings.Contains((*urls)[0], "limit=100") || strings.Contains((*urls)[0], "after=") || !strings.Contains((*urls)[1], "after=agent_support") || !strings.Contains((*urls)[0], "api-version=v1") {
		t.Errorf("urls = %v", *urls)
	}
}

func TestListAgentsPageCapMarksTruncated(t *testing.T) {
	n := 0
	c, urls := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) bool {
		n++
		fmt.Fprintf(w, `{"data":[{"name":"a%d"}],"last_id":"id%d","has_more":true}`, n, n)
		return true
	})
	endpoint, _ := Endpoint("acct", "proj")
	got, err := c.ListAgents(context.Background(), endpoint)
	if err != nil || !got.Truncated || len(*urls) != maxPages || len(got.Agents) != maxPages {
		t.Fatalf("got=%+v urls=%d err=%v", got, len(*urls), err)
	}
	// has_more without a usable last_id stops instead of looping.
	c2, urls2 := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) bool {
		fmt.Fprint(w, `{"data":[],"has_more":true}`)
		return true
	})
	got, err = c2.ListAgents(context.Background(), endpoint)
	if err != nil || !got.Truncated || len(*urls2) != 1 {
		t.Fatalf("got=%+v urls=%d err=%v", got, len(*urls2), err)
	}
}

func TestListConnectionsDecodesPagedConnectionAndFollowsNextLink(t *testing.T) {
	c, urls := fixtureClient(t, nil)
	endpoint, _ := Endpoint("acct", "proj")
	got, err := c.ListConnections(context.Background(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if got.Truncated || len(got.Connections) != 2 || got.Connections[0] != (Connection{Name: "search-conn", Type: "CognitiveSearch"}) || got.Connections[1].Type != "AzureStorageAccount" {
		t.Fatalf("connections = %+v", got)
	}
	if strings.Contains(fmt.Sprint(got), "DOCUMENT_SENTINEL_TEXT") {
		t.Errorf("metadata leaked: %+v", got)
	}
	if len(*urls) != 2 || !strings.Contains((*urls)[1], "skiptoken=page2") {
		t.Errorf("urls = %v", *urls)
	}
}

func TestListConnectionsRefusesForeignNextLink(t *testing.T) {
	c, urls := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) bool {
		fmt.Fprint(w, `{"value":[{"name":"a","type":"ApiKey"}],"nextLink":"https://evil.example.com/steal?api-version=v1"}`)
		return true
	})
	endpoint, _ := Endpoint("acct", "proj")
	_, err := c.ListConnections(context.Background(), endpoint)
	if !errors.Is(err, azure.ErrNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	if len(*urls) != 1 {
		t.Errorf("the foreign link was followed: %v", *urls)
	}
	c2, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) bool {
		fmt.Fprintf(w, `{"value":[],"nextLink":"https://acct.services.ai.azure.com/api/projects/proj/connections?api-version=v1&skiptoken=%d"}`, time.Now().UnixNano())
		return true
	})
	got, err := c2.ListConnections(context.Background(), endpoint)
	if err != nil || !got.Truncated {
		t.Errorf("page cap: got=%+v err=%v", got, err)
	}
}

func TestDataPlaneErrorsAreTyped(t *testing.T) {
	c, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) bool {
		http.NotFound(w, r)
		return true
	})
	endpoint, _ := Endpoint("acct", "proj")
	if _, err := c.ListAgents(context.Background(), endpoint); !runtime.IsNotFound(err) {
		t.Errorf("ListAgents err = %v", err)
	}
	if err := c.Ping(context.Background(), endpoint); !runtime.IsNotFound(err) {
		t.Errorf("Ping err = %v", err)
	}
	c2, _ := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) bool {
		fmt.Fprint(w, `{"data":"not-a-list"}`)
		return true
	})
	if _, err := c2.ListAgents(context.Background(), endpoint); !errors.Is(err, azure.ErrDecode) {
		t.Errorf("decode err = %v", err)
	}
}

func TestPingReadsOneAgent(t *testing.T) {
	c, urls := fixtureClient(t, nil)
	endpoint, _ := Endpoint("acct", "proj")
	if err := c.Ping(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	if len(*urls) != 1 || !strings.Contains((*urls)[0], "limit=1") {
		t.Errorf("urls = %v", *urls)
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
