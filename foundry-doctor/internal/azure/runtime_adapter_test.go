package azure

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fixtures in this file are "synthetic": hand-written to match the response
// shapes of the specs named next to each one, never recorded from a tenant.

const (
	rtAcct      = testRG + "/providers/Microsoft.CognitiveServices/accounts/acct"
	rtSearch    = testRG + "/providers/Microsoft.Search/searchServices/searchsvc"
	rtStorage   = testRG + "/providers/Microsoft.Storage/storageAccounts/storageacct"
	rtCosmos    = testRG + "/providers/Microsoft.DocumentDB/databaseAccounts/cosmosacct"
	rtWS        = testRG + "/providers/Microsoft.OperationalInsights/workspaces/law1"
	rtWSGUID    = "44444444-4444-4444-4444-444444444444"
	rtPrincipal = "55555555-5555-5555-5555-555555555555"
	rtLAHost    = "https://la.test"
)

// hostRT answers by "METHOD host path" and records every request in full.
type hostRT struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string][]reply
	reqs   []*http.Request
	bodies []string
}

func newHostRT(t *testing.T) *hostRT { return &hostRT{t: t, routes: map[string][]reply{}} }

func (h *hostRT) on(method, host, path string, rs ...reply) *hostRT {
	h.routes[method+" "+host+path] = rs
	return h
}

func (h *hostRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if !IsAllowed(r.Method, r.URL.Path) {
		h.t.Errorf("request outside the allow-list: %s %s", r.Method, r.URL.Path)
	}
	var body string
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}
	key := r.Method + " " + r.URL.Scheme + "://" + r.URL.Host + r.URL.Path
	h.mu.Lock()
	h.reqs = append(h.reqs, r)
	h.bodies = append(h.bodies, body)
	rs, ok := h.routes[key]
	var rep reply
	if ok {
		rep = rs[0]
		if len(rs) > 1 {
			h.routes[key] = rs[1:]
		}
	}
	h.mu.Unlock()
	if !ok {
		rep = reply{status: 404, body: `{"error":{"code":"ResourceNotFound"}}`}
	}
	hd := http.Header{"Content-Type": {"application/json"}}
	for k, v := range rep.header {
		hd[k] = v
	}
	return &http.Response{StatusCode: rep.status, Header: hd, Body: io.NopCloser(strings.NewReader(rep.body)), Request: r}, nil
}

func (h *hostRT) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.reqs)
}

// scopeCred records the scope of every token request.
type scopeCred struct {
	mu     sync.Mutex
	scopes []string
}

func (c *scopeCred) Token(_ context.Context, scope string) (AccessToken, error) {
	c.mu.Lock()
	c.scopes = append(c.scopes, scope)
	c.mu.Unlock()
	return AccessToken{Token: "tok", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func newRuntimeAdapter(t *testing.T, rt http.RoundTripper, cred TokenCredential, now time.Time) (*Adapter, *sleeps) {
	t.Helper()
	sl := &sleeps{}
	a, err := New(Options{
		Credential: cred, Endpoint: "https://management.test", LogAnalyticsEndpoint: rtLAHost,
		HTTPClient: &http.Client{Transport: rt}, Sleep: sl.fn, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, sl
}

const mgmt = "https://management.test"

// ---- R1(a) metrics ----------------------------------------------------------

// synthetic body shaped like Metrics_List 2024-02-01 (Response, Metric,
// TimeSeriesElement, MetadataValue, MetricValue).
const metricsBodyRequests = `{"cost":0,"timespan":"2026-10-17T11:00:00Z/2026-10-17T12:00:00Z","interval":"PT1M","namespace":"Microsoft.CognitiveServices/accounts","resourceregion":"swedencentral","value":[{
 "id":"x","type":"Microsoft.Insights/metrics","name":{"value":"AzureOpenAIRequests","localizedValue":"Azure OpenAI Requests"},"unit":"Count","errorCode":"Success",
 "timeseries":[
  {"metadatavalues":[{"name":{"value":"StatusCode"},"value":"429"},{"name":{"value":"ModelDeploymentName"},"value":"gpt-main"}],"data":[{"timeStamp":"2026-10-17T11:00:00Z","total":3},{"timeStamp":"2026-10-17T11:01:00Z","total":2},{"timeStamp":"2026-10-17T11:02:00Z"}]},
  {"metadatavalues":[{"name":{"value":"StatusCode"},"value":"200"},{"name":{"value":"ModelDeploymentName"},"value":"gpt-main"}],"data":[{"timeStamp":"2026-10-17T11:00:00Z","total":40}]}
 ]}]}`

func TestQueryAccountMetricsSendsSpecShapedQuery(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	h := newHostRT(t).on("GET", mgmt, rtAcct+"/providers/Microsoft.Insights/metrics", reply{status: 200, body: metricsBodyRequests})
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, now)
	res, err := a.QueryAccountMetrics(context.Background(), rtAcct, MetricQuery{
		MetricName: "AzureOpenAIRequests", Aggregation: "Total", SplitDimensions: []string{"ModelDeploymentName", "StatusCode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	q := h.reqs[0].URL.Query()
	want := map[string]string{
		"metricnames": "AzureOpenAIRequests", "aggregation": "Total", "interval": "PT1M", "api-version": "2024-02-01",
		"timespan": "2026-10-17T11:00:00Z/2026-10-17T12:00:00Z",
		"$filter":  "ModelDeploymentName eq '*' and StatusCode eq '*'",
		"top":      "50",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q (raw %s)", k, q.Get(k), v, h.reqs[0].URL.RawQuery)
		}
	}
	if strings.Contains(h.reqs[0].URL.RawQuery, "PT1H") || strings.Contains(h.reqs[0].URL.RawQuery, "eq%20'429'") || strings.Contains(h.reqs[0].URL.RawQuery, "eq '429'") {
		t.Errorf("query must not use PT1H or a hard-coded StatusCode value: %s", h.reqs[0].URL.RawQuery)
	}
	if len(res.Series) != 2 || res.Truncated {
		t.Fatalf("series = %+v", res)
	}
	for _, s := range res.Series {
		if s.Dimensions["modeldeploymentname"] != "gpt-main" {
			t.Errorf("series not attributed to the deployment: %+v", s)
		}
		switch s.Dimensions["statuscode"] {
		case "429":
			if s.Value != 5 || s.Points != 2 {
				t.Errorf("429 series = %+v (null points must be skipped)", s)
			}
		case "200":
			if s.Value != 40 {
				t.Errorf("200 series = %+v", s)
			}
		default:
			t.Errorf("unexpected series %+v", s)
		}
	}
}

func TestQueryAccountMetricsUtilisationUsesMaximum(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	body := `{"timespan":"x","value":[{"id":"x","type":"t","name":{"value":"AzureOpenAIProvisionedManagedUtilizationV2"},"unit":"Percent","errorCode":"Success","timeseries":[
	 {"metadatavalues":[{"name":{"value":"ModelDeploymentName"},"value":"ptu-dep"}],"data":[{"timeStamp":"a","average":40,"maximum":70},{"timeStamp":"b","average":60,"maximum":100}]}]}]}`
	h := newHostRT(t).on("GET", mgmt, rtAcct+"/providers/Microsoft.Insights/metrics", reply{status: 200, body: body})
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, now)
	res, err := a.QueryAccountMetrics(context.Background(), rtAcct, MetricQuery{MetricName: "AzureOpenAIProvisionedManagedUtilizationV2", Aggregation: "Maximum", SplitDimensions: []string{"ModelDeploymentName"}, Window: 30 * time.Minute})
	if err != nil || len(res.Series) != 1 || res.Series[0].Value != 100 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	q := h.reqs[0].URL.Query()
	if q.Get("aggregation") != "Maximum" || q.Get("timespan") != "2026-10-17T11:30:00Z/2026-10-17T12:00:00Z" || q.Get("$filter") != "ModelDeploymentName eq '*'" {
		t.Errorf("query = %s", h.reqs[0].URL.RawQuery)
	}
	// Average is the other supported aggregation.
	h2 := newHostRT(t).on("GET", mgmt, rtAcct+"/providers/Microsoft.Insights/metrics", reply{status: 200, body: body})
	a2, _ := newRuntimeAdapter(t, h2, &scopeCred{}, now)
	res, err = a2.QueryAccountMetrics(context.Background(), rtAcct, MetricQuery{MetricName: "AzureOpenAIProvisionedManagedUtilizationV2", Aggregation: "Average"})
	if err != nil || len(res.Series) != 1 || res.Series[0].Value != 50 {
		t.Fatalf("average res=%+v err=%v", res, err)
	}
}

func TestQueryAccountMetricsNoSeriesAndErrorCodeAndTruncation(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a, _ := newRuntimeAdapter(t, newHostRT(t).on("GET", mgmt, rtAcct+"/providers/Microsoft.Insights/metrics", reply{status: 200, body: `{"timespan":"x","value":[{"id":"x","type":"t","name":{"value":"m"},"unit":"Count","errorCode":"BadRequest","timeseries":[]}]}`}), &scopeCred{}, now)
	res, err := a.QueryAccountMetrics(context.Background(), rtAcct, MetricQuery{MetricName: "AzureOpenAIRequests", Aggregation: "Total"})
	if err != nil || len(res.Series) != 0 || res.ErrorCode != "BadRequest" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var sb strings.Builder
	sb.WriteString(`{"timespan":"x","value":[{"id":"x","type":"t","name":{"value":"m"},"unit":"Count","errorCode":"Success","timeseries":[`)
	for i := 0; i < metricsTop; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"metadatavalues":[{"name":{"value":"ModelDeploymentName"},"value":"d` + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + `"}],"data":[{"timeStamp":"a","total":1}]}`)
	}
	sb.WriteString(`]}]}`)
	a2, _ := newRuntimeAdapter(t, newHostRT(t).on("GET", mgmt, rtAcct+"/providers/Microsoft.Insights/metrics", reply{status: 200, body: sb.String()}), &scopeCred{}, now)
	res, err = a2.QueryAccountMetrics(context.Background(), rtAcct, MetricQuery{MetricName: "AzureOpenAIRequests", Aggregation: "Total", SplitDimensions: []string{"ModelDeploymentName"}})
	if err != nil || !res.Truncated {
		t.Fatalf("res.Truncated=%v err=%v", res.Truncated, err)
	}
}

func TestQueryAccountMetricsRejectsHostileInput(t *testing.T) {
	h := newHostRT(t)
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	ctx := context.Background()
	bad := []MetricQuery{
		{MetricName: "m'; drop", Aggregation: "Total"},
		{MetricName: "m", Aggregation: "Total&x=1"},
		{MetricName: "m", Aggregation: "Total", SplitDimensions: []string{"A eq 'x' or B"}},
		{MetricName: "m", Aggregation: "Total", SplitDimensions: []string{"Dim'"}},
	}
	for _, q := range bad {
		if _, err := a.QueryAccountMetrics(ctx, rtAcct, q); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%+v err = %v", q, err)
		}
	}
	for _, id := range []string{testRG + "/providers/Microsoft.Storage/storageAccounts/x", "/x", rtAcct + "/../x"} {
		if _, err := a.QueryAccountMetrics(ctx, id, MetricQuery{MetricName: "m", Aggregation: "Total"}); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
	if h.count() != 0 {
		t.Errorf("hostile input reached the network: %d requests", h.count())
	}
}

func TestMetricQueryKeysArePerOperation(t *testing.T) {
	h := newHostRT(t)
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	ctx := context.Background()
	// metricnames is only allowed on runtime.metrics.
	var out any
	err := a.c.get(ctx, rtAcct+"/providers/Microsoft.Authorization/locks", url.Values{"metricnames": {"x"}}, &out)
	if !errors.Is(err, ErrNotAllowed) {
		t.Errorf("metricnames on locks: %v", err)
	}
	err = a.c.get(ctx, rtAcct+"/providers/Microsoft.Insights/metrics", url.Values{"$expand": {"x"}}, &out)
	if !errors.Is(err, ErrNotAllowed) {
		t.Errorf("$expand on metrics: %v", err)
	}
	err = a.c.get(ctx, rtAcct+"/providers/Microsoft.Insights/metrics", url.Values{"resultType": {"Data"}, "metricnamespace": {"x"}}, &out)
	if errors.Is(err, ErrNotAllowed) {
		t.Errorf("spec'd metrics parameters rejected: %v", err)
	}
	for _, o := range AllowedOperations() {
		if o.Name == "runtime.metrics" && len(o.QueryKeys) == 0 {
			t.Error("runtime.metrics must declare its query keys")
		}
	}
}

// ---- R1(b) role assignments --------------------------------------------------

// synthetic body shaped like RoleAssignmentListResult (2022-04-01).
func raItem(scope, role string) string {
	return `{"id":"` + scope + `/providers/Microsoft.Authorization/roleAssignments/` + role[len(role)-4:] + `","name":"n","type":"Microsoft.Authorization/roleAssignments","properties":{"roleDefinitionId":"` + role + `","principalId":"` + rtPrincipal + `","scope":"` + scope + `","principalType":"ServicePrincipal"}}`
}

func TestListRoleAssignmentsAtResourceScopeSplitsByScope(t *testing.T) {
	roleA := testScope + "/providers/Microsoft.Authorization/roleDefinitions/7ca78c08-252a-4471-8644-bb5ff32d4ba0"
	roleB := testScope + "/providers/Microsoft.Authorization/roleDefinitions/8ebe5a00-799e-43f5-93ac-243d3dce84a7"
	items := []string{
		raItem(rtSearch, roleA),                // exact
		raItem(testRG, roleB),                  // ancestor RG
		raItem(testScope, roleA),               // ancestor subscription
		raItem(rtSearch+"/indexes/idx", roleB), // below
		raItem(testRG+"/providers/Microsoft.Search/searchServices/searchsvc2", roleA), // sibling, same prefix up to the name
		raItem(rtSearch+"2", roleA), // name prefix is not a path boundary
		raItem("/providers/Microsoft.Management/managementGroups/mg1", roleB),
	}
	h := newHostRT(t).on("GET", mgmt, rtSearch+"/providers/Microsoft.Authorization/roleAssignments", reply{status: 200, body: `{"value":[` + strings.Join(items, ",") + `]}`})
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	set, err := a.ListRoleAssignments(context.Background(), rtSearch, rtPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Covering) != 3 || len(set.Below) != 1 || len(set.Unresolved) != 1 {
		t.Fatalf("covering=%d below=%d unresolved=%d: %+v", len(set.Covering), len(set.Below), len(set.Unresolved), set)
	}
	q := h.reqs[0].URL.Query()
	if q.Get("api-version") != "2022-04-01" || q.Get("$filter") != "principalId eq "+rtPrincipal {
		t.Errorf("query = %s", h.reqs[0].URL.RawQuery)
	}
}

func TestListRoleAssignmentsAtResourceGroupAndSubscriptionScope(t *testing.T) {
	for _, scope := range []string{testRG, testScope} {
		h := newHostRT(t).on("GET", mgmt, scope+"/providers/Microsoft.Authorization/roleAssignments", reply{status: 200, body: `{"value":[]}`})
		a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
		if _, err := a.ListRoleAssignments(context.Background(), scope, rtPrincipal); err != nil {
			t.Errorf("%s: %v", scope, err)
		}
	}
}

func TestListRoleAssignmentsHostileInput(t *testing.T) {
	h := newHostRT(t)
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	ctx := context.Background()
	for _, scope := range []string{
		"", "relative/path", rtSearch + "/../../x", rtSearch + "?x=1", rtSearch + "#f", rtSearch + "/providers/../x",
		testRG + "/providers/Microsoft.Search/searchServices/a b", "/subscriptions/not-a-guid/resourceGroups/rg",
	} {
		if _, err := a.ListRoleAssignments(ctx, scope, rtPrincipal); err == nil {
			t.Errorf("scope %q accepted", scope)
		}
	}
	for _, p := range []string{"", "x", rtPrincipal + " or 1 eq 1", "not-a-guid"} {
		if _, err := a.ListRoleAssignments(ctx, rtSearch, p); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("principal %q err = %v", p, err)
		}
	}
	if h.count() != 0 {
		t.Errorf("hostile input reached the network: %d", h.count())
	}
	if IsAllowed("PUT", rtSearch+"/providers/Microsoft.Authorization/roleAssignments/x") || IsAllowed("POST", rtSearch+"/providers/Microsoft.Authorization/roleAssignments") {
		t.Error("role assignment writes must not be allow-listed")
	}
}

func TestListRoleAssignmentsRestricted(t *testing.T) {
	h := newHostRT(t).on("GET", mgmt, rtSearch+"/providers/Microsoft.Authorization/roleAssignments", reply{status: 403, body: `{"error":{"code":"AuthorizationFailed"}}`})
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	_, err := a.ListRoleAssignments(context.Background(), rtSearch, rtPrincipal)
	if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.Authorization/roleAssignments/read" {
		t.Errorf("err = %v", err)
	}
}

func TestWithinScope(t *testing.T) {
	if !WithinScope(rtSearch, testRG) || !WithinScope(rtSearch, "/subscriptions/other", testScope) || WithinScope(rtSearch, "", "/subscriptions/other") || WithinScope(rtSearch, rtSearch+"2") {
		t.Error("WithinScope")
	}
}

// ---- R1(c) Log Analytics through the allow-list ------------------------------

const wsBody = `{"id":"` + rtWS + `","name":"law1","type":"Microsoft.OperationalInsights/workspaces","location":"swedencentral","properties":{"customerId":"` + rtWSGUID + `","provisioningState":"Succeeded"}}`

// synthetic body shaped like the Logs query response (response-format.md).
const laBody = `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Category","type":"string"},{"name":"count_","type":"long"}],"rows":[["Audit",12],["RequestResponse",3]]}]}`

func laPath() string { return "/v1/workspaces/" + rtWSGUID + "/query" }

func TestQueryDiagnosticCountsRoutesThroughAllowList(t *testing.T) {
	h := newHostRT(t).
		on("GET", mgmt, rtWS, reply{status: 200, body: wsBody}).
		on("POST", rtLAHost, laPath(), reply{status: 200, body: laBody})
	cred := &scopeCred{}
	a, _ := newRuntimeAdapter(t, h, cred, time.Now())
	res, err := a.QueryDiagnosticCounts(context.Background(), rtWS, "My-Acct", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counts["Audit"] != 12 || res.Counts["RequestResponse"] != 3 {
		t.Errorf("counts = %v", res.Counts)
	}
	if len(h.reqs) != 2 {
		t.Fatalf("requests = %d", len(h.reqs))
	}
	ws, la := h.reqs[0], h.reqs[1]
	if ws.URL.Host != "management.test" || ws.URL.Query().Get("api-version") != "2025-02-01" {
		t.Errorf("workspace read = %s", ws.URL)
	}
	if la.URL.Host != "la.test" || la.Method != http.MethodPost || la.URL.RawQuery != "" {
		t.Errorf("log query = %s %s (no api-version query is sent; v1 is in the path)", la.Method, la.URL)
	}
	if la.Header.Get("Content-Type") != "application/json" || !strings.HasPrefix(la.Header.Get("Authorization"), "Bearer ") {
		t.Errorf("headers = %v", la.Header)
	}
	if len(cred.scopes) != 2 || cred.scopes[0] != "https://management.test/.default" || cred.scopes[1] != "https://api.loganalytics.io/.default" {
		t.Errorf("token scopes = %v", cred.scopes)
	}
	body := h.bodies[1]
	for _, want := range []string{`"timespan":"PT1H"`, `ResourceId endswith \"/ACCOUNTS/MY-ACCT\"`, `summarize count() by Category`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s lacks %s", body, want)
		}
	}
	if strings.Contains(body, " has ") {
		t.Errorf("query must not use has: %s", body)
	}
}

func TestQueryDiagnosticCountsIgnoresOtherTables(t *testing.T) {
	h := newHostRT(t).
		on("GET", mgmt, rtWS, reply{status: 200, body: wsBody}).
		on("POST", rtLAHost, laPath(), reply{status: 200, body: `{"tables":[{"name":"Other","columns":[],"rows":[["Audit",99]]}]}`})
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	res, err := a.QueryDiagnosticCounts(context.Background(), rtWS, "acct", 1)
	if err != nil || len(res.Counts) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestQueryDiagnosticCountsHostileNameReplayFailsClosed(t *testing.T) {
	h := newHostRT(t)
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	ctx := context.Background()
	for _, name := range []string{
		`x" | take 5 //`, `x" or 1==1 | project Message //`, "a b", "a\nb", `a"`, "a;b", "*", "", strings.Repeat("a", 65), `x") | where 1==1 | summarize count() by Category //`,
	} {
		if _, err := a.QueryDiagnosticCounts(ctx, rtWS, name, 1); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("name %q err = %v", name, err)
		}
	}
	for _, hours := range []int{0, -1, 1000} {
		if _, err := a.QueryDiagnosticCounts(ctx, rtWS, "acct", hours); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("hours %d err = %v", hours, err)
		}
	}
	for _, ws := range []string{"", "law", rtAcct, rtWS + "/../x", rtWS + "?a=b", testRG + "/providers/Microsoft.Storage/storageAccounts/x"} {
		if _, err := a.QueryDiagnosticCounts(ctx, ws, "acct", 1); err == nil {
			t.Errorf("workspace %q accepted", ws)
		}
	}
	if h.count() != 0 {
		t.Errorf("hostile input reached the network: %d requests", h.count())
	}
}

func TestLogQueryBodyValidationRejectsNonTemplates(t *testing.T) {
	good, _ := DiagnosticCountsKQL("desktop-top", 2) // a name containing "top" is fine
	if err := validateLogQueryBody(logQueryBody{Query: good, Timespan: "PT2H"}); err != nil {
		t.Fatalf("shipped template rejected: %v", err)
	}
	h := newHostRT(t)
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	path := "/v1/workspaces/" + rtWSGUID + "/query"
	bad := []any{
		logQueryBody{Query: good + " | take 5", Timespan: "PT1H"},
		logQueryBody{Query: good + " | top 5 by Category", Timespan: "PT1H"},
		logQueryBody{Query: good + " | project Message", Timespan: "PT1H"},
		logQueryBody{Query: strings.Replace(good, "summarize count() by Category", "summarize count() by *", 1), Timespan: "PT1H"},
		logQueryBody{Query: "AzureDiagnostics | take 10", Timespan: "PT1H"},
		logQueryBody{Query: strings.Replace(good, "AzureDiagnostics", "AzureDiagnostics | where Message has 'x' | union Other", 1), Timespan: "PT1H"},
		logQueryBody{Query: good, Timespan: "P30D"},
		logQueryBody{Query: good, Timespan: ""},
		map[string]any{"query": good, "timespan": "PT1H"},
		logQueryBody{},
		"raw string",
		nil,
	}
	for i, b := range bad {
		if err := a.c.post(context.Background(), path, b, &struct{}{}); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("case %d err = %v", i, err)
		}
	}
	// Other verbs and hosts' paths never match the log operation.
	if IsAllowed("GET", path) || IsAllowed("PUT", path) || IsAllowed("POST", "/v1/workspaces/not-a-guid/query") || IsAllowed("POST", path+"/../x") || IsAllowed("POST", "/v1/workspaces/"+rtWSGUID+"/query/extra") {
		t.Error("log query path shapes outside the template are allow-listed")
	}
	if h.count() != 0 {
		t.Errorf("rejected bodies reached the network: %d", h.count())
	}
}

func TestLogQueryRetriesOn429AndHonoursRetryAfter(t *testing.T) {
	h := newHostRT(t).
		on("GET", mgmt, rtWS, reply{status: 200, body: wsBody}).
		on("POST", rtLAHost, laPath(),
			reply{status: 429, header: http.Header{"Retry-After": {"7"}}, body: `{"error":{"code":"ThrottledRequest"}}`},
			reply{status: 200, body: laBody})
	a, sl := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	res, err := a.QueryDiagnosticCounts(context.Background(), rtWS, "acct", 1)
	if err != nil || res.Counts["Audit"] != 12 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(sl.d) != 1 || sl.d[0] != 7*time.Second {
		t.Errorf("sleeps = %v, want [7s]", sl.d)
	}
	// Persistent 429 ends as an APIError without leaking the body.
	h2 := newHostRT(t).
		on("GET", mgmt, rtWS, reply{status: 200, body: wsBody}).
		on("POST", rtLAHost, laPath(), reply{status: 429, body: `{"error":{"code":"ThrottledRequest","message":"SECRET-BODY"}}`})
	a2, _ := newRuntimeAdapter(t, h2, &scopeCred{}, time.Now())
	_, err = a2.QueryDiagnosticCounts(context.Background(), rtWS, "acct", 1)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 429 || strings.Contains(err.Error(), "SECRET-BODY") {
		t.Errorf("err = %v", err)
	}
}

func TestLogQueryPermissionAndRedirectAndSizeCap(t *testing.T) {
	ws := reply{status: 200, body: wsBody}
	// 403 names the exact permission.
	h := newHostRT(t).on("GET", mgmt, rtWS, ws).on("POST", rtLAHost, laPath(), reply{status: 403, body: `{"error":{"code":"InsufficientAccessError"}}`})
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	_, err := a.QueryDiagnosticCounts(context.Background(), rtWS, "acct", 1)
	if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.OperationalInsights/workspaces/query/read" {
		t.Errorf("403 err = %v", err)
	}
	// A redirect is refused: the bearer token must not travel to Location.
	h = newHostRT(t).on("GET", mgmt, rtWS, ws).on("POST", rtLAHost, laPath(), reply{status: 302, header: http.Header{"Location": {"https://evil.test/steal"}}})
	a, _ = newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	_, err = a.QueryDiagnosticCounts(context.Background(), rtWS, "acct", 1)
	if err == nil {
		t.Error("redirect accepted")
	}
	for _, r := range h.reqs {
		if r.URL.Host == "evil.test" {
			t.Error("redirect was followed")
		}
	}
	// Response size cap.
	big := reply{status: 200, body: `{"tables":[],"pad":"` + strings.Repeat("a", maxBodyBytes+10) + `"}`}
	h = newHostRT(t).on("GET", mgmt, rtWS, ws).on("POST", rtLAHost, laPath(), big)
	a, _ = newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	if _, err = a.QueryDiagnosticCounts(context.Background(), rtWS, "acct", 1); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("oversized response err = %v", err)
	}
}

func TestLogQueryWorkspaceWithoutCustomerIDIsDecodeError(t *testing.T) {
	h := newHostRT(t).on("GET", mgmt, rtWS, reply{status: 200, body: `{"properties":{}}`})
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	_, err := a.QueryDiagnosticCounts(context.Background(), rtWS, "acct", 1)
	if !errors.Is(err, ErrDecode) {
		t.Errorf("err = %v", err)
	}
	h = newHostRT(t).on("GET", mgmt, rtWS, reply{status: 200, body: `[`})
	a, _ = newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	if _, err = a.QueryDiagnosticCounts(context.Background(), rtWS, "acct", 1); !errors.Is(err, ErrDecode) {
		t.Errorf("malformed err = %v", err)
	}
}

func TestLogAnalyticsEndpointMustBeHTTPSOrigin(t *testing.T) {
	for _, bad := range []string{"http://la.test", "https://la.test/v1", "la.test"} {
		if _, err := New(Options{LogAnalyticsEndpoint: bad, Credential: &scopeCred{}}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%q err = %v", bad, err)
		}
	}
	for _, o := range AllowedOperations() {
		if o.Name == "runtime.loganalytics.query" {
			if o.Origin != "loganalytics" || o.TokenScope != "https://api.loganalytics.io/.default" || o.Method != http.MethodPost {
				t.Errorf("operation = %+v", o)
			}
			return
		}
	}
	t.Error("runtime.loganalytics.query is not declared")
}

// ---- R1(d) project, connections, cosmos, diagnostics --------------------------

func TestGetProjectReadsUserAssignedPrincipal(t *testing.T) {
	path := rtAcct + "/projects/proj"
	ua := "66666666-6666-6666-6666-666666666666"
	// synthetic, shaped like Project (cognitiveservices.json 2026-09-01) with identity.type UserAssigned.
	body := `{"id":"` + path + `","name":"proj","identity":{"type":"UserAssigned","userAssignedIdentities":{"/subscriptions/x/resourceGroups/r/providers/Microsoft.ManagedIdentity/userAssignedIdentities/mi":{"principalId":"` + strings.ToUpper(ua) + `","clientId":"c"}}},"properties":{"provisioningState":"Succeeded"}}`
	a, _ := newAdapter(t, newFake(t).on("GET", path, reply{status: 200, body: body}))
	p, err := a.GetProject(context.Background(), testSub, "rg-demo", "acct", "proj")
	if err != nil || p.PrincipalID != "" || len(p.UserAssignedPrincipalIDs) != 1 || p.UserAssignedPrincipalIDs[0] != ua {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	if got := p.IdentityPrincipals(); len(got) != 1 || got[0] != ua {
		t.Errorf("IdentityPrincipals = %v", got)
	}
	both := FoundryProject{PrincipalID: "s", UserAssignedPrincipalIDs: []string{"u", "s"}}
	if got := both.IdentityPrincipals(); len(got) != 2 || got[0] != "s" || got[1] != "u" {
		t.Errorf("IdentityPrincipals = %v", got)
	}
	if got := (FoundryProject{}).IdentityPrincipals(); len(got) != 0 {
		t.Errorf("empty identity = %v", got)
	}
}

func TestGetProjectSystemAssignedAndHostile(t *testing.T) {
	path := rtAcct + "/projects/proj"
	body := `{"id":"x","name":"proj","identity":{"type":"SystemAssigned","principalId":"` + rtPrincipal + `"},"properties":{"provisioningState":"Succeeded"}}`
	a, _ := newAdapter(t, newFake(t).on("GET", path, reply{status: 200, body: body}))
	p, err := a.GetProject(context.Background(), testSub, "rg-demo", "acct", "proj")
	if err != nil || p.PrincipalID != rtPrincipal || p.IdentityType != "SystemAssigned" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	for _, c := range [][3]string{{"rg/../x", "acct", "proj"}, {"rg-demo", "a?b", "proj"}, {"rg-demo", "acct", ""}} {
		if _, err := a.GetProject(context.Background(), testSub, c[0], c[1], c[2]); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%v err = %v", c, err)
		}
	}
	a2, _ := newAdapter(t, newFake(t).on("GET", path, reply{status: 200, body: `{`}))
	if _, err := a2.GetProject(context.Background(), testSub, "rg-demo", "acct", "proj"); !errors.Is(err, ErrDecode) {
		t.Errorf("decode err = %v", err)
	}
}

func TestListConnectionsDecodesSpecFields(t *testing.T) {
	path := rtAcct + "/projects/proj/connections"
	// synthetic, shaped like ConnectionPropertiesV2BasicResource (2026-09-01): credentials are
	// present in the shape of the secret-free list and must never be decoded.
	body := `{"value":[
	 {"id":"c1","name":"search-conn","type":"Microsoft.CognitiveServices/accounts/projects/connections","properties":{"authType":"AAD","category":"CognitiveSearch","target":"https://searchsvc.search.windows.net","peRequirement":"Required","peStatus":"Active","metadata":{"ResourceId":"` + rtSearch + `","ApiType":"Azure"}}},
	 {"id":"c2","name":"arm-target","properties":{"authType":"AAD","category":"AzureStorageAccount","target":"` + rtStorage + `","peRequirement":"NotApplicable","peStatus":"NotApplicable"}},
	 {"id":"c3","name":"key-conn","properties":{"authType":"ApiKey","category":"ApiKey","target":"https://example.test","metadata":{"resourceid":"[parameters('rid')]"},"credentials":{"key":"SECRET-VALUE"}}}]}`
	a, _ := newAdapter(t, newFake(t).on("GET", path, reply{status: 200, body: body}))
	got, err := a.ListProjectConnections(context.Background(), testSub, "rg-demo", "acct", "proj")
	if err != nil || len(got) != 3 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got[0].Target.ResourceID != rtSearch || got[0].PEStatus != "Active" || got[0].PERequirement != "Required" || got[0].Target.Endpoint != "https://searchsvc.search.windows.net" {
		t.Errorf("conn0 = %+v", got[0])
	}
	if got[1].Target.ResourceID != rtStorage {
		t.Errorf("an ARM-ID target must be the resource ID: %+v", got[1])
	}
	if got[2].Target.ResourceID != "[parameters('rid')]" || strings.Contains(strings.Join([]string{got[2].Name, got[2].Error, got[2].AuthType}, ""), "SECRET") {
		t.Errorf("conn2 = %+v", got[2])
	}
}

func TestListCosmosSQLRoleAssignmentsDecodesFullScopeAndRole(t *testing.T) {
	path := rtCosmos + "/sqlRoleAssignments"
	role := rtCosmos + "/sqlRoleDefinitions/00000000-0000-0000-0000-000000000002"
	// synthetic, shaped like SqlRoleAssignmentListResult (rbac.json 2025-10-15).
	body := `{"value":[
	 {"id":"a","name":"a","properties":{"roleDefinitionId":"` + role + `","scope":"` + rtCosmos + `/dbs/enterprise_memory","principalId":"` + strings.ToUpper(rtPrincipal) + `"}},
	 {"id":"b","name":"b","properties":{"roleDefinitionId":"` + role + `","scope":"` + rtCosmos + `","principalId":"77777777-7777-7777-7777-777777777777"}}]}`
	a, _ := newAdapter(t, newFake(t).on("GET", path, reply{status: 200, body: body}))
	got, err := a.ListCosmosSQLRoleAssignments(context.Background(), rtCosmos, rtPrincipal)
	if err != nil || len(got) != 1 || got[0].RoleDefinitionID != role || got[0].Scope != rtCosmos+"/dbs/enterprise_memory" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	for _, bad := range []string{"", rtSearch, rtCosmos + "/../x"} {
		if _, err := a.ListCosmosSQLRoleAssignments(context.Background(), bad, rtPrincipal); err == nil {
			t.Errorf("account %q accepted", bad)
		}
	}
}

func TestListDiagnosticSettingsReadsDestinationTypeAndGroups(t *testing.T) {
	path := rtAcct + "/providers/Microsoft.Insights/diagnosticSettings"
	// synthetic, shaped like DiagnosticSettingsResourceCollection (2021-05-01-preview).
	body := `{"value":[
	 {"id":"1","name":"dedicated","systemData":{"createdAt":"2026-10-01T00:00:00Z","lastModifiedAt":"2026-10-17T10:30:00Z"},"properties":{"workspaceId":"` + rtWS + `","logAnalyticsDestinationType":"Dedicated","logs":[{"category":"Audit","enabled":true},{"category":"Trace","enabled":false}]}},
	 {"id":"2","name":"groups","properties":{"workspaceId":"` + rtWS + `","logs":[{"categoryGroup":"allLogs","enabled":true},{"categoryGroup":"audit","enabled":false}]}}]}`
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a, _ := newRuntimeAdapter(t, newHostRT(t).on("GET", mgmt, path, reply{status: 200, body: body}), &scopeCred{}, now)
	got, err := a.ListDiagnosticSettings(context.Background(), rtAcct)
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if !got[0].AgeKnown || got[0].AgeMinutes != 90 || got[1].AgeKnown {
		t.Errorf("ages = %+v / %+v", got[0], got[1])
	}
	if got[0].LogAnalyticsDestinationType != "Dedicated" || len(got[0].Categories) != 1 || got[0].Categories[0] != "Audit" {
		t.Errorf("dedicated = %+v", got[0])
	}
	if got[1].LogAnalyticsDestinationType != "" || len(got[1].CategoryGroups) != 1 || got[1].CategoryGroups[0] != "allLogs" || got[1].WorkspaceID != rtWS {
		t.Errorf("groups = %+v", got[1])
	}
}

func TestRuntimeOperationsDeclared(t *testing.T) {
	want := map[string]string{
		"runtime.metrics":                       "2024-02-01",
		"runtime.roleassignments.list.resource": "2022-04-01",
		"runtime.workspace.get":                 "2025-02-01",
		"runtime.loganalytics.query":            "v1",
		"runtime.cosmos.sqlroleassignments":     "2025-10-15",
		"runtime.diagnosticsettings":            "2021-05-01-preview",
	}
	got := map[string]string{}
	for _, o := range AllowedOperations() {
		got[o.Name] = o.APIVersion
	}
	for n, v := range want {
		if got[n] != v {
			t.Errorf("%s api-version = %q, want %q", n, got[n], v)
		}
	}
}
