package app

import (
	"bytes"
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
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

// The privacy requirement (CLAUDE.md: never emit secrets, tokens, prompts or
// document content) is tested end to end: the production runtime pipeline
// (project discovery, config, real azure adapter and data-plane clients,
// scheduler, engine, baseline, reporters) runs against synthetic, spec-shaped
// responses that carry content sentinels in every field a service could put
// content in, and no sentinel may appear in any report format.

const (
	pvSub    = "11111111-1111-1111-1111-111111111111"
	pvPrin   = "22222222-2222-2222-2222-222222222222"
	pvLA     = "44444444-4444-4444-4444-444444444444"
	pvRG     = "/subscriptions/" + pvSub + "/resourceGroups/rg"
	pvOther  = "/subscriptions/" + pvSub + "/resourceGroups/elsewhere"
	pvAcct   = pvRG + "/providers/Microsoft.CognitiveServices/accounts/acct"
	pvSearch = pvOther + "/providers/Microsoft.Search/searchServices/searchsvc"
	pvStore  = pvRG + "/providers/Microsoft.Storage/storageAccounts/storageacct"
	pvWS     = pvRG + "/providers/Microsoft.OperationalInsights/workspaces/law1"
)

var pvSentinels = []string{
	"PROMPT_SENTINEL_TEXT", "COMPLETION_SENTINEL_TEXT", "DOCUMENT_SENTINEL_TEXT", "SECRET_SENTINEL_VALUE", "TOKEN_SENTINEL_VALUE", "VECTOR_SENTINEL_TEXT",
}

type pvRT struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]string
}

func (p *pvRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if (req.URL.Host == "management.test" || req.URL.Host == "la.test") && !azure.IsAllowed(req.Method, req.URL.Path) {
		p.t.Errorf("request outside the allow-list: %s %s%s", req.Method, req.URL.Host, req.URL.Path)
	}
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
	}
	key := req.Method + " " + req.URL.Host + req.URL.Path
	p.mu.Lock()
	body, ok := p.routes[key]
	p.mu.Unlock()
	status := 200
	if !ok {
		status, body = 404, `{"error":{"code":"ResourceNotFound"}}`
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func pvRoutes() map[string]string {
	m := "GET management.test"
	return map[string]string{
		m + pvAcct + "/projects/proj": `{"id":"` + pvAcct + `/projects/proj","name":"proj","identity":{"type":"SystemAssigned","principalId":"` + pvPrin + `"},"properties":{"provisioningState":"Succeeded"}}`,
		m + pvAcct:                    `{"id":"` + pvAcct + `","properties":{"publicNetworkAccess":"Disabled","customSubDomainName":"acct"}}`,
		m + pvAcct + "/projects/proj/capabilityHosts": `{"value":[{"id":"h1","name":"default","properties":{"provisioningState":"Succeeded","storageConnections":["storage-conn"],"vectorStoreConnections":["search-conn"]}}]}`,
		m + pvAcct + "/projects/proj/connections": `{"value":[
		  {"id":"c1","name":"search-conn","properties":{"authType":"AAD","category":"CognitiveSearch","target":"https://searchsvc.search.windows.net","credentials":{"key":"SECRET_SENTINEL_VALUE"},"metadata":{"ResourceId":"` + pvSearch + `","index":"knowledge","indexer":"load-knowledge","note":"DOCUMENT_SENTINEL_TEXT"}}},
		  {"id":"c2","name":"storage-conn","properties":{"authType":"AAD","category":"AzureStorageAccount","metadata":{"ResourceId":"` + pvStore + `","note":"PROMPT_SENTINEL_TEXT"}}}]}`,
		m + pvAcct + "/connections":                                         `{"value":[]}`,
		m + pvAcct + "/deployments":                                         `{"value":[{"id":"d1","name":"gpt-main","properties":{"provisioningState":"Succeeded"}}]}`,
		m + pvSearch + "/providers/Microsoft.Authorization/roleAssignments": `{"value":[]}`,
		m + pvStore + "/providers/Microsoft.Authorization/roleAssignments":  `{"value":[]}`,
		m + pvAcct + "/providers/Microsoft.Insights/metrics": `{"timespan":"x","value":[{"id":"m","type":"t","name":{"value":"m"},"unit":"Count","errorCode":"Success","timeseries":[
		  {"metadatavalues":[{"name":{"value":"ModelDeploymentName"},"value":"gpt-main"},{"name":{"value":"StatusCode"},"value":"429"}],"data":[{"timeStamp":"a","total":9,"maximum":100}]}]}]}`,
		m + pvAcct + "/providers/Microsoft.Insights/diagnosticSettings": `{"value":[{"id":"d","name":"to-law","systemData":{"lastModifiedAt":"2026-01-01T00:00:00Z"},"properties":{"workspaceId":"` + pvWS + `","logs":[{"category":"Audit","enabled":true}]}}]}`,
		m + pvWS: `{"id":"` + pvWS + `","properties":{"customerId":"` + pvLA + `"}}`,
		// The query returns no Audit rows, plus a content column a real template never selects.
		"POST la.test/v1/workspaces/" + pvLA + "/query":                             `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Category"},{"name":"count_"},{"name":"Message"}],"rows":[]}],"extra":"COMPLETION_SENTINEL_TEXT"}`,
		"GET acct.services.ai.azure.com/api/projects/proj/agents":                   `{"data":[{"object":"agent","id":"a1","name":"support-agent","state":"enabled","versions":{"latest":{"version":"2","status":"failed","metadata":{"m":"COMPLETION_SENTINEL_TEXT"},"definition":{"instructions":"PROMPT_SENTINEL_TEXT"}}}}],"last_id":"a1","has_more":false}`,
		"GET acct.services.ai.azure.com/api/projects/proj/connections":              `{"value":[{"name":"search-conn","type":"CognitiveSearch","metadata":{"n":"DOCUMENT_SENTINEL_TEXT"}}]}`,
		"GET searchsvc.search.windows.net/indexes('knowledge')":                     `{"name":"knowledge","fields":[{"name":"v","type":"Collection(Edm.Single)","dimensions":8,"vectorSearchProfile":"ghost","sample":"VECTOR_SENTINEL_TEXT"}],"vectorSearch":{"profiles":[{"name":"p"}]}}`,
		"GET searchsvc.search.windows.net/indexes('knowledge')/search.stats":        `{"documentCount":4,"storageSize":1,"vectorIndexSize":1}`,
		"GET searchsvc.search.windows.net/indexers('load-knowledge')/search.status": `{"name":"load-knowledge","status":"error","lastResult":{"status":"transientFailure","errorMessage":"DOCUMENT_SENTINEL_TEXT","errors":[{"errorMessage":"VECTOR_SENTINEL_TEXT"}],"warnings":[],"itemsProcessed":0,"itemsFailed":2},"executionHistory":[],"limits":{}}`,
	}
}

type tokenSentinelCred struct{}

func (tokenSentinelCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "TOKEN_SENTINEL_VALUE", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

type publicResolver struct{}

func (publicResolver) LookupHost(context.Context, string) ([]string, error) {
	return []string{"20.1.2.3"}, nil
}

func TestRuntimeCommandNeverEmitsContentSentinels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "azure.yaml"), []byte("name: demo\nservices:\n  support-agent:\n    host: azure.ai.agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, format := range Formats {
		t.Run(format, func(t *testing.T) {
			rtp := &pvRT{t: t, routes: pvRoutes()}
			hc := &http.Client{Transport: rtp}
			var stderr bytes.Buffer
			svc := NewServices(&stderr, Options{
				Getenv:     func(string) string { return "" },
				AzdVersion: func(context.Context) string { return "1.35.0" },
				RuntimeClient: func() (azure.Client, error) {
					a, err := azure.New(azure.Options{Credential: tokenSentinelCred{}, Endpoint: "https://management.test", LogAnalyticsEndpoint: "https://la.test", HTTPClient: hc,
						Now: func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) error { return nil }})
					if err != nil {
						return azure.Client{}, err
					}
					return a.Client(), nil
				},
				RuntimeHTTP: hc, RuntimeCredential: tokenSentinelCred{}, RuntimeResolver: publicResolver{},
			})
			var out bytes.Buffer
			code, err := Runtime(context.Background(), svc, RuntimeRequest{
				DoctorRequest: DoctorRequest{Dir: dir, Format: format},
				Target:        RuntimeTarget{SubscriptionID: pvSub, ResourceGroup: "rg", Account: "acct", Project: "proj"},
				Vantage:       runtime.VantageVNet, Timeout: 5 * time.Second,
			}, &out)
			if err != nil {
				t.Fatalf("runtime: %v\n%s", err, stderr.String())
			}
			if code != ExitFindings {
				t.Fatalf("exit = %d, want findings (%d)\n%s", code, ExitFindings, out.String())
			}
			text := out.String() + stderr.String()
			for _, id := range []string{"FND-RUN-001", "FND-RUN-003", "FND-RUN-004", "FND-RUN-005", "FND-RUN-006", "FND-RUN-007"} {
				if !strings.Contains(text, id) {
					t.Errorf("%s is absent from the %s output: the fixture no longer exercises it", id, format)
				}
			}
			for _, s := range pvSentinels {
				if strings.Contains(text, s) {
					t.Errorf("sentinel %s leaked into the %s output", s, format)
				}
			}
			if !strings.Contains(text, "asserted with --vantage vnet") && format != "sarif" {
				// RUN-002 passes the account (public network access Disabled) through a
				// public answer: its finding states the asserted vantage.
				t.Errorf("the vantage assumption is not stated in the %s output", format)
			}
		})
	}
}
