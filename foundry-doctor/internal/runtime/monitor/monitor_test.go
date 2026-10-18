package monitor

import (
	"context"
	"errors"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

type fakeInner struct {
	queries []azure.MetricQuery
	err     error
}

func (f *fakeInner) QueryAccountMetrics(_ context.Context, _ string, q azure.MetricQuery) (azure.MetricResult, error) {
	f.queries = append(f.queries, q)
	if f.err != nil {
		return azure.MetricResult{}, f.err
	}
	return azure.MetricResult{
		Series:    []azure.MetricSeries{{Dimensions: map[string]string{"modeldeploymentname": "dep1", "StatusCode": "429"}, Value: 2, Points: 1}},
		Truncated: true, ErrorCode: "BadRequest",
	}, nil
}
func (*fakeInner) ListDiagnosticSettings(context.Context, string) ([]azure.RuntimeDiagnosticSetting, error) {
	return []azure.RuntimeDiagnosticSetting{{Name: "diag", WorkspaceID: "law", Categories: []string{"Audit"}, CategoryGroups: []string{"allLogs"}, LogAnalyticsDestinationType: "Dedicated", AgeKnown: true, AgeMinutes: 180}}, nil
}
func (*fakeInner) QueryDiagnosticCounts(context.Context, string, string, int) (azure.RuntimeLogsResult, error) {
	return azure.RuntimeLogsResult{Counts: map[string]int64{"Audit": 1}}, nil
}

func TestBuildQuery(t *testing.T) {
	q, ok := BuildQuery("acct-01", 1)
	if !ok || q == "" {
		t.Fatal("expected valid query")
	}
	if want := `AzureDiagnostics | where ResourceProvider == "MICROSOFT.COGNITIVESERVICES" | where ResourceId endswith "/ACCOUNTS/ACCT-01" | where TimeGenerated > ago(1h) | summarize count() by Category`; q != want {
		t.Fatalf("query = %q", q)
	}
	if bad, ok := BuildQuery("acct\"bad", 1); ok || bad != "" {
		t.Fatalf("invalid account accepted: %q", bad)
	}
	if bad, ok := BuildQuery("acct", 0); ok || bad != "" {
		t.Fatalf("invalid duration accepted: %q", bad)
	}
}

func TestFixedClientSendsTheDocumentedQueries(t *testing.T) {
	f := &fakeInner{}
	c := FixedClient{Inner: f}
	ctx := context.Background()
	if got, err := c.QueryTraffic(ctx, "id"); err != nil || len(got.Series) != 1 || got.Series[0].Dimensions["modeldeploymentname"] != "dep1" || !got.Truncated || got.ErrorCode != "BadRequest" {
		t.Fatalf("traffic = %+v err=%v", got, err)
	}
	if got, err := c.QueryRequestsByStatus(ctx, "id"); err != nil || got.Series[0].Dimensions["statuscode"] != "429" {
		t.Fatalf("status = %+v err=%v (dimension keys are lower-cased)", got, err)
	}
	if _, err := c.QueryProvisionedUtilization(ctx, "id"); err != nil {
		t.Fatal(err)
	}
	want := []azure.MetricQuery{
		{MetricName: "AzureOpenAIRequests", Aggregation: "Total"},
		{MetricName: "AzureOpenAIRequests", Aggregation: "Total", SplitDimensions: []string{"ModelDeploymentName", "StatusCode"}},
		{MetricName: "AzureOpenAIProvisionedManagedUtilizationV2", Aggregation: "Maximum", SplitDimensions: []string{"ModelDeploymentName"}},
	}
	for i, w := range want {
		g := f.queries[i]
		if g.MetricName != w.MetricName || g.Aggregation != w.Aggregation || len(g.SplitDimensions) != len(w.SplitDimensions) {
			t.Errorf("query %d = %+v, want %+v", i, g, w)
		}
	}
	if got, err := c.ListDiagnosticSettings(ctx, "id"); err != nil || len(got) != 1 || got[0].WorkspaceID != "law" || !got[0].DedicatedTables() {
		t.Fatalf("diags = %+v err=%v", got, err)
	}
	if got, err := c.QueryDiagnosticCounts(ctx, "law", "acct", 1); err != nil || got.Counts["Audit"] != 1 {
		t.Fatalf("logs = %+v err=%v", got, err)
	}
}

func TestFixedClientPropagatesErrors(t *testing.T) {
	boom := errors.New("boom")
	c := FixedClient{Inner: &fakeInner{err: boom}}
	if _, err := c.QueryTraffic(context.Background(), "id"); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestDiagnosticSettingPredicates(t *testing.T) {
	cases := []struct {
		name             string
		s                DiagnosticSetting
		sends, dedicated bool
	}{
		{"audit category", DiagnosticSetting{Categories: []string{"Audit"}}, true, false},
		{"request response category", DiagnosticSetting{Categories: []string{"RequestResponse"}}, true, false},
		{"allLogs group", DiagnosticSetting{CategoryGroups: []string{"allLogs"}}, true, false},
		{"other category", DiagnosticSetting{Categories: []string{"Trace"}}, false, false},
		{"audit group is not assumed", DiagnosticSetting{CategoryGroups: []string{"audit"}}, false, false},
		{"dedicated", DiagnosticSetting{Categories: []string{"Audit"}, LogAnalyticsDestinationType: "dedicated"}, true, true},
	}
	for _, c := range cases {
		if c.s.SendsAuditOrRequestResponse() != c.sends || c.s.DedicatedTables() != c.dedicated {
			t.Errorf("%s: sends=%v dedicated=%v", c.name, c.s.SendsAuditOrRequestResponse(), c.s.DedicatedTables())
		}
	}
}
