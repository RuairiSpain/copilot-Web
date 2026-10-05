package monitor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

type fakeInner struct{}

func (fakeInner) QueryAccountMetrics(context.Context, string, string, string, string, string) ([]azure.MetricTotal, error) {
	return []azure.MetricTotal{{Series: "dep1", Total: 2}}, nil
}
func (fakeInner) ListDiagnosticSettings(context.Context, string) ([]azure.RuntimeDiagnosticSetting, error) {
	return []azure.RuntimeDiagnosticSetting{{Name: "diag", WorkspaceID: "law", Categories: []string{"Audit"}, AgeHours: 3}}, nil
}
func (fakeInner) QueryDiagnosticCounts(context.Context, string, string, int) (azure.RuntimeLogsResult, error) {
	return azure.RuntimeLogsResult{Table: "AzureDiagnostics", Counts: map[string]int64{"Audit": 1}}, nil
}

func TestBuildQuery(t *testing.T) {
	q, ok := BuildQuery("acct-01", 1)
	if !ok || q == "" {
		t.Fatal("expected valid query")
	}
	if bad, ok := BuildQuery("acct\"bad", 1); ok || bad != "" {
		t.Fatalf("invalid account accepted: %q", bad)
	}
	if bad, ok := BuildQuery("acct", 0); ok || bad != "" {
		t.Fatalf("invalid duration accepted: %q", bad)
	}
}

func TestFixedClientAdapters(t *testing.T) {
	c := FixedClient{Inner: fakeInner{}}
	if got, err := c.QueryTraffic(context.Background(), "id"); err != nil || len(got) != 1 || got[0].Series != "dep1" {
		t.Fatalf("traffic = %+v err=%v", got, err)
	}
	if got, err := c.Query429(context.Background(), "id"); err != nil || len(got) != 1 {
		t.Fatalf("429 = %+v err=%v", got, err)
	}
	if got, err := c.QueryProvisionedUtilization(context.Background(), "id"); err != nil || len(got) != 1 {
		t.Fatalf("util = %+v err=%v", got, err)
	}
	if got, err := c.ListDiagnosticSettings(context.Background(), "id"); err != nil || len(got) != 1 || got[0].WorkspaceID != "law" {
		t.Fatalf("diags = %+v err=%v", got, err)
	}
	if got, err := c.QueryDiagnosticCounts(context.Background(), "law", "acct", 1); err != nil || got.Counts["Audit"] != 1 {
		t.Fatalf("logs = %+v err=%v", got, err)
	}
}

func TestWorkspaceClientQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tables":[{"name":"PrimaryResult"}]}`)
	}))
	defer srv.Close()

	var out struct {
		Tables []struct {
			Name string `json:"name"`
		} `json:"tables"`
	}
	c := WorkspaceClient{HTTP: srv.Client(), Credential: staticCred{}}
	if err := c.Query(context.Background(), srv.URL, map[string]string{"query": "x"}, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Tables) != 1 || out.Tables[0].Name != "PrimaryResult" {
		t.Fatalf("out = %+v", out)
	}
}

type staticCred struct{}

func (staticCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "token"}, nil
}
