package run

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	rt "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	foundrydp "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/foundry"
	searchprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/search"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// The fakes in run_test.go accept any argument, so a rule could ask for a path,
// query parameter or body that the allow-list rejects and the unit tests would
// never notice. These contract tests wire the REAL azure adapter (azure.New)
// and the real data-plane clients to a RoundTripper that serves synthetic,
// spec-shaped JSON and fails the test for any ARM or Log Analytics request that
// azure.IsAllowed rejects. Then they run the seven rules end to end.

const (
	laGUID   = "44444444-4444-4444-4444-444444444444"
	wsID     = rgScope + "/providers/Microsoft.OperationalInsights/workspaces/law1"
	acctPath = rgScope + "/providers/Microsoft.CognitiveServices/accounts/acct"
)

type hostRT struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]string // "METHOD host path" -> JSON body
	calls  []string
	urls   []string
}

func (h *hostRT) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	armLike := host == "management.test" || host == "la.test"
	if armLike && !azure.IsAllowed(req.Method, req.URL.Path) {
		h.t.Errorf("request outside the read-only allow-list: %s %s%s", req.Method, host, req.URL.Path)
	}
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
	}
	key := req.Method + " " + host + req.URL.Path
	h.mu.Lock()
	h.calls = append(h.calls, key)
	h.urls = append(h.urls, req.URL.String())
	body, ok := h.routes[key]
	h.mu.Unlock()
	status := 200
	if !ok {
		status, body = 404, `{"error":{"code":"ResourceNotFound"}}`
		if armLike {
			h.t.Errorf("no synthetic route for %s", key)
		}
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func (h *hostRT) called(prefix string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func raJSON(scope, role string) string {
	return `{"id":"x","name":"n","properties":{"roleDefinitionId":"` + roleBase + role + `","principalId":"` + principalID + `","scope":"` + scope + `","principalType":"ServicePrincipal"}}`
}

func contractRoutes() map[string]string {
	m := "GET management.test"
	const meta = `"type":"Microsoft.CognitiveServices/accounts/projects/connections"`
	return map[string]string{
		m + acctPath + "/projects/proj":                 `{"id":"` + acctPath + `/projects/proj","name":"proj","identity":{"type":"SystemAssigned","principalId":"` + principalID + `"},"properties":{"provisioningState":"Succeeded"}}`,
		m + acctPath:                                    `{"id":"` + acctPath + `","name":"acct","properties":{"publicNetworkAccess":"Disabled","customSubDomainName":"acct","privateEndpointConnections":[{"id":"pe1","properties":{"privateLinkServiceConnectionState":{"status":"Approved"}}}]}}`,
		m + acctPath + "/projects/proj/capabilityHosts": `{"value":[{"id":"h1","name":"default","properties":{"provisioningState":"Succeeded","storageConnections":["storage-conn"],"threadStorageConnections":["cosmos-conn"],"vectorStoreConnections":["search-conn"]}}]}`,
		m + acctPath + "/projects/proj/connections": `{"value":[
		  {"id":"c1","name":"search-conn",` + meta + `,"properties":{"authType":"AAD","category":"CognitiveSearch","target":"https://searchsvc.search.windows.net","peRequirement":"Required","peStatus":"Active","metadata":{"ResourceId":"` + searchID + `","index":"knowledge","indexer":"load-knowledge"}}},
		  {"id":"c2","name":"storage-conn",` + meta + `,"properties":{"authType":"AAD","category":"AzureStorageAccount","target":"https://storageacct.blob.core.windows.net","metadata":{"ResourceId":"` + storageID + `"}}},
		  {"id":"c3","name":"cosmos-conn",` + meta + `,"properties":{"authType":"AAD","category":"CosmosDb","target":"https://cosmosacct.documents.azure.com","metadata":{"ResourceId":"` + cosmosID + `"}}}]}`,
		m + acctPath + "/connections": `{"value":[]}`,
		m + acctPath + "/deployments": `{"value":[{"id":"d1","name":"gpt-main","sku":{"name":"GlobalStandard","capacity":10},"properties":{"provisioningState":"Succeeded"}},{"id":"d2","name":"gpt-ptu","sku":{"name":"ProvisionedManaged","capacity":50},"properties":{"provisioningState":"Succeeded"}}]}`,
		// Role assignments: search lacks Search Index Data Contributor; storage has everything.
		m + searchID + "/providers/Microsoft.Authorization/roleAssignments": `{"value":[` + raJSON(searchID, roleGUID["search-service-contributor"]) + `,` + raJSON(rgScope, "acdd72a7-3385-48ef-bd42-f606fba81ae7") + `]}`,
		m + storageID + "/providers/Microsoft.Authorization/roleAssignments": `{"value":[` +
			raJSON(storageID, roleGUID["storage-blob-data-contributor"]) + `,` + raJSON(storageID, roleGUID["storage-blob-data-owner"]) + `,` + raJSON(rgScope, roleGUID["storage-account-contributor"]) + `]}`,
		m + cosmosID + "/providers/Microsoft.Authorization/roleAssignments": `{"value":[` + raJSON(cosmosID, roleGUID["cosmos-db-operator"]) + `]}`,
		m + cosmosID + "/sqlRoleAssignments":                                `{"value":[{"id":"a","properties":{"roleDefinitionId":"` + cosmosRole + `","scope":"` + cosmosID + `","principalId":"` + principalID + `"}}]}`,
		m + acctPath + "/providers/Microsoft.Insights/metrics":              "METRICS",
		m + acctPath + "/providers/Microsoft.Insights/diagnosticSettings":   `{"value":[{"id":"ds","name":"to-law","systemData":{"lastModifiedAt":"2026-10-01T00:00:00Z"},"properties":{"workspaceId":"` + wsID + `","logs":[{"category":"Audit","enabled":true}]}}]}`,
		m + wsID: `{"id":"` + wsID + `","name":"law1","properties":{"customerId":"` + laGUID + `"}}`,
		"POST la.test/v1/workspaces/" + laGUID + "/query": `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Category","type":"string"},{"name":"count_","type":"long"}],"rows":[["Audit",4]]}]}`,
		// Foundry v1 data plane (spec shapes).
		"GET acct.services.ai.azure.com/api/projects/proj/agents":      `{"data":[{"object":"agent","id":"a1","name":"support-agent","state":"enabled","versions":{"latest":{"version":"3","status":"active"}}}],"first_id":"a1","last_id":"a1","has_more":false}`,
		"GET acct.services.ai.azure.com/api/projects/proj/connections": `{"value":[{"name":"search-conn","type":"CognitiveSearch"},{"name":"storage-conn","type":"AzureStorageAccount"},{"name":"cosmos-conn","type":"CosmosDB"}]}`,
		// Search data plane.
		"GET searchsvc.search.windows.net/indexes('knowledge')":                     `{"name":"knowledge","fields":[{"name":"v","type":"Collection(Edm.Single)","dimensions":1536,"vectorSearchProfile":"p"}],"vectorSearch":{"profiles":[{"name":"p","algorithm":"a"}]}}`,
		"GET searchsvc.search.windows.net/indexes('knowledge')/search.stats":        `{"documentCount":10,"storageSize":1,"vectorIndexSize":1}`,
		"GET searchsvc.search.windows.net/indexers('load-knowledge')/search.status": `{"name":"load-knowledge","status":"running","lastResult":{"status":"success","endTime":"2026-10-17T11:00:00Z","errors":[],"warnings":[],"itemsProcessed":10,"itemsFailed":0},"executionHistory":[],"limits":{}}`,
	}
}

const contractMetrics = `{"timespan":"x","value":[{"id":"m","type":"t","name":{"value":"m"},"unit":"Count","errorCode":"Success","timeseries":[
 {"metadatavalues":[{"name":{"value":"ModelDeploymentName"},"value":"gpt-main"},{"name":{"value":"StatusCode"},"value":"429"}],"data":[{"timeStamp":"a","total":7}]},
 {"metadatavalues":[{"name":{"value":"ModelDeploymentName"},"value":"gpt-main"},{"name":{"value":"StatusCode"},"value":"200"}],"data":[{"timeStamp":"a","total":70}]},
 {"metadatavalues":[{"name":{"value":"ModelDeploymentName"},"value":"gpt-ptu"}],"data":[{"timeStamp":"a","total":1,"maximum":100}]}]}]}`

func contractDeps(t *testing.T, h *hostRT) Deps {
	t.Helper()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	hc := &http.Client{Transport: h}
	a, err := azure.New(azure.Options{Credential: staticCred{}, Endpoint: "https://management.test", LogAnalyticsEndpoint: "https://la.test", HTTPClient: hc, Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	c := a.Client()
	return Deps{
		FoundryMgmt: c.Foundry, RBAC: c.RBAC, Monitor: c.Monitor,
		ProjectData: foundrydp.HTTPClient{HTTP: hc, Credential: staticCred{}},
		SearchData:  searchprobe.HTTPClient{HTTP: hc, Credential: staticCred{}, Now: func() time.Time { return now }},
		Resolver:    fakeResolver{},
		Target:      Target{SubscriptionID: subID, ResourceGroup: "rg", Account: "acct", Project: "proj"},
		Options:     rt.Options{Vantage: rt.VantageVNet, Timeout: 5 * time.Second, Parallelism: 4},
	}
}

func TestRealAdapterRoutesEveryRuntimeRuleThroughTheAllowList(t *testing.T) {
	routes := contractRoutes()
	routes["GET management.test"+acctPath+"/providers/Microsoft.Insights/metrics"] = contractMetrics
	h := &hostRT{t: t, routes: routes}
	d := contractDeps(t, h)
	in := baseInput()
	results := map[string]sdk.Result{}
	for _, r := range Register(d) {
		res, err := r.Evaluate(context.Background(), in)
		if err != nil {
			t.Fatalf("%s: %v", r.ID(), err)
		}
		if res.Skipped != nil && (strings.Contains(res.Skipped.Reason, "internal error") || strings.Contains(res.Skipped.Reason, "allow-list")) {
			t.Errorf("%s hit a defect: %q", r.ID(), res.Skipped.Reason)
		}
		results[r.ID()] = res
	}
	// 001: Search Index Data Contributor is missing, nothing else is.
	if f := results["FND-RUN-001"].Findings; len(f) != 1 || !strings.Contains(f[0].Evidence, "Search Index Data Contributor") || !strings.Contains(f[0].Evidence, searchID) {
		t.Errorf("RUN-001 = %+v", results["FND-RUN-001"])
	}
	// 002: all three account hosts and the search host resolve privately: clean.
	if r := results["FND-RUN-002"]; len(r.Findings) != 0 {
		t.Errorf("RUN-002 = %+v", r)
	}
	// 003: clean (all connections inside the resource group).
	if r := results["FND-RUN-003"]; r.Skipped != nil || len(r.Findings) != 0 {
		t.Errorf("RUN-003 = %+v", r)
	}
	// 004: the 429 finding is attributed to the deployment, utilisation to the other.
	var ids []string
	for _, f := range results["FND-RUN-004"].Findings {
		ids = append(ids, f.Resource.ID)
	}
	if len(ids) != 2 || ids[0] != acctPath+"/deployments/gpt-main" || ids[1] != acctPath+"/deployments/gpt-ptu" {
		t.Errorf("RUN-004 resource IDs = %v", ids)
	}
	// 005, 006, 007: clean.
	for _, id := range []string{"FND-RUN-005", "FND-RUN-006", "FND-RUN-007"} {
		if r := results[id]; r.Skipped != nil || len(r.Findings) != 0 {
			t.Errorf("%s = %+v", id, r)
		}
	}
	for _, want := range []string{"POST la.test/v1/workspaces/" + laGUID + "/query", "GET management.test" + wsID, "GET management.test" + searchID + "/providers/Microsoft.Authorization/roleAssignments"} {
		if !h.called(want) {
			t.Errorf("expected call %s; calls: %v", want, h.calls)
		}
	}
	var metricURL string
	for _, u := range h.urls {
		if strings.Contains(u, "/providers/Microsoft.Insights/metrics") && strings.Contains(u, "StatusCode") {
			metricURL = u
		}
	}
	for _, want := range []string{"timespan=2026-10-17T11%3A00%3A00Z%2F2026-10-17T12%3A00%3A00Z", "aggregation=Total", "interval=PT1M"} {
		if !strings.Contains(metricURL, want) {
			t.Errorf("metrics URL %q lacks %s", metricURL, want)
		}
	}
	if strings.Contains(metricURL, "429") {
		t.Errorf("the StatusCode value must not be hard-coded in the filter: %s", metricURL)
	}
}

func TestRealAdapterSurfacesAnAllowListDefectAsInternal(t *testing.T) {
	// A target whose IDs are valid for the rule but whose workspace ID is a hostile
	// value: the adapter refuses before any request and the rule says "internal".
	routes := contractRoutes()
	routes["GET management.test"+acctPath+"/providers/Microsoft.Insights/metrics"] = contractMetrics
	routes["GET management.test"+acctPath+"/providers/Microsoft.Insights/diagnosticSettings"] =
		`{"value":[{"id":"ds","name":"x","properties":{"workspaceId":"/subscriptions/` + subID + `/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/../../x","logs":[{"category":"Audit","enabled":true}]}}]}`
	h := &hostRT{t: t, routes: routes}
	d := contractDeps(t, h)
	for _, r := range Register(d) {
		if r.ID() != "FND-RUN-007" {
			continue
		}
		res, err := r.Evaluate(context.Background(), baseInput())
		if err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix+"internal error") {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	}
	if h.called("POST la.test") {
		t.Error("a hostile workspace ID reached the Log Analytics origin")
	}
}
