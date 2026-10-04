package foundry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func token(context.Context) (string, error) { return "tok", nil }

func server(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return &Client{BaseURL: s.URL + "/api/projects/finance", Token: token, Sleep: func(time.Duration) {}}
}

func TestProjectURLAndToolboxMCPURL(t *testing.T) {
	if got := ProjectURL("acct", "finance"); got != "https://acct.services.ai.azure.com/api/projects/finance" {
		t.Fatal(got)
	}
	c := &Client{BaseURL: "https://x/api/projects/p/"}
	if got := c.ToolboxMCPURL("my box", "3"); got != "https://x/api/projects/p/toolboxes/my%20box/versions/3/mcp?api-version=v1" {
		t.Fatal(got)
	}
}

func TestCreateAgentVersion(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/projects/finance/agents/bot/versions" || r.URL.Query().Get("api-version") != "v1" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers %v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["definition"].(map[string]any)["kind"] != "prompt" {
			t.Errorf("body %v", body)
		}
		_, _ = io.WriteString(w, `{"object":"agent.version","name":"bot","version":"4"}`)
	})
	got, err := c.CreateAgentVersion(context.Background(), "bot", map[string]any{"definition": map[string]any{"kind": "prompt"}})
	if err != nil || got.Name != "bot" || got.Version != "4" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestGetReadsTheLatestVersion(t *testing.T) {
	for body, want := range map[string]string{
		`{"name":"bot","versions":{"latest":{"version":"7"}}}`: "7",
		`{"name":"box","default_version":"2"}`:                 "2",
		`{"name":"x","version":"1"}`:                           "1",
	} {
		c := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
		r, err := c.GetAgent(context.Background(), "bot")
		if err != nil || r.Version != want {
			t.Errorf("%s -> %+v %v", body, r, err)
		}
	}
}

func TestToolboxCalls(t *testing.T) {
	var seen []string
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			_, _ = io.WriteString(w, `{"version":"1"}`) // no name echoed back
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"name":"box","default_version":"1"}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	ctx := context.Background()
	created, err := c.CreateToolboxVersion(ctx, "box", map[string]any{"tools": []any{}})
	if err != nil || created.Name != "box" || created.Version != "1" {
		t.Fatalf("%+v %v", created, err)
	}
	if got, err := c.GetToolbox(ctx, "box"); err != nil || got.Version != "1" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := c.DeleteToolbox(ctx, "box"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteAgent(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	want := "POST /api/projects/finance/toolboxes/box/versions,GET /api/projects/finance/toolboxes/box,DELETE /api/projects/finance/toolboxes/box,DELETE /api/projects/finance/agents/bot"
	if strings.Join(seen, ",") != want {
		t.Fatalf("calls = %v", seen)
	}
}

func TestNotFoundIsNilForGetAndIgnoredForDelete(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"no such agent"}}`)
	})
	ctx := context.Background()
	if r, err := c.GetAgent(ctx, "x"); r != nil || err != nil {
		t.Fatalf("%v %v", r, err)
	}
	if r, err := c.GetToolbox(ctx, "x"); r != nil || err != nil {
		t.Fatalf("%v %v", r, err)
	}
	if err := c.DeleteAgent(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	_, err := c.CreateAgentVersion(ctx, "x", map[string]any{})
	var api *APIError
	if !errors.As(err, &api) || !IsNotFound(err) || api.Code != "not_found" || !strings.Contains(err.Error(), "404 not_found: no such agent") {
		t.Fatalf("%v", err)
	}
}

func TestErrorWithoutABody(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	_, err := c.GetAgent(context.Background(), "x")
	var api *APIError
	if !errors.As(err, &api) || api.Status != 403 || !strings.Contains(err.Error(), "403 Forbidden") || IsNotFound(err) {
		t.Fatalf("%v", err)
	}
}

func TestThrottledRequestsAreRetried(t *testing.T) {
	calls := 0
	var slept []time.Duration
	c := server(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		switch calls {
		case 1:
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = io.WriteString(w, `{"name":"bot","version":"1"}`)
		}
	})
	c.Retries = 3
	c.Sleep = func(d time.Duration) { slept = append(slept, d) }
	if _, err := c.GetAgent(context.Background(), "bot"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(slept) != 2 || slept[0] != 3*time.Second || slept[1] != 2*time.Second {
		t.Fatalf("calls %d, slept %v", calls, slept)
	}
	// Out of retries: the last response is the error.
	calls = 0
	c.Retries = 0
	c.HTTP = &http.Client{}
	if _, err := c.GetAgent(context.Background(), "bot"); err == nil {
		t.Fatal("expected the 429 to surface")
	}
}

func TestTokenFailureAndTransportFailure(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", Token: func(context.Context) (string, error) { return "", errors.New("no login") }}
	if _, err := c.GetAgent(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "no login") {
		t.Fatalf("%v", err)
	}
	c.Token = token
	if _, err := c.GetAgent(context.Background(), "x"); err == nil {
		t.Fatal("expected a connection error")
	}
	c.BaseURL = "http://[::1"
	if _, err := c.GetAgent(context.Background(), "x"); err == nil {
		t.Fatal("expected a URL error")
	}
}

func TestBadResponseBodies(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "not json") })
	if _, err := c.GetAgent(context.Background(), "x"); err == nil {
		t.Fatal("expected a decode error")
	}
	if _, err := c.CreateAgentVersion(context.Background(), "x", map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("expected a marshal error")
	}
}

func TestRetryAfterFallsBackToBackoff(t *testing.T) {
	if retryAfter("", 0) != time.Second || retryAfter("junk", 2) != 4*time.Second || retryAfter("0", 1) != 0 {
		t.Fatal("retryAfter")
	}
}

func TestDefaultSleepAndClient(t *testing.T) {
	c := &Client{}
	c.sleep(time.Millisecond)
	if c.client().Timeout == 0 {
		t.Fatal("default client needs a timeout")
	}
}
