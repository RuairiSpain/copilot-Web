package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	rt "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	foundrydp "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/foundry"
	searchprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/search"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestRun001MissingRolesAndSkips(t *testing.T) {
	d := baseDeps()
	d.RBAC = fakeRBAC{
		assignments: map[string][]azure.RoleAssignment{
			searchID:  {{RoleDefinitionID: "/providers/Microsoft.Authorization/roleDefinitions/" + "8ebe5a00-799e-43f5-93ac-243d3dce84a7"}},
			storageID: {{RoleDefinitionID: "/providers/Microsoft.Authorization/roleDefinitions/" + "17d1049b-9a84-46fb-8f53-869881c3d3ab"}},
		},
		sql: map[string][]azure.CosmosSQLRoleAssignment{},
	}
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) < 2 {
		t.Fatalf("want findings, got %+v", res)
	}
}

func TestRun001PermissionUnavailableSkips(t *testing.T) {
	d := baseDeps()
	d.RBAC = fakeRBAC{assignErr: &azure.UnavailableError{Capability: "roleAssignments", Permission: "Microsoft.Authorization/roleAssignments/read"}}
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "roleAssignments") {
		t.Fatalf("expected skipped result, got %+v", res)
	}
}

func TestRun002VantageAndResolver(t *testing.T) {
	d := baseDeps()
	d.Options.Vantage = rt.VantageVNet
	d.Resolver = fakeResolver{answers: map[string][]string{
		d.Target.Account + ".cognitiveservices.azure.com": {"52.1.1.1"},
	}}
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) == 0 {
		t.Fatalf("expected finding, got %+v", res)
	}
	d.Options.Vantage = rt.VantageNone
	res, err = evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "inside the VNet") {
		t.Fatalf("expected skipped result, got %+v", res)
	}
}

func TestRun003ConnectionErrorAndMissing(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{
		project: project(),
		hosts: []azure.CapabilityHost{{
			ID: "host1", Name: "default", ProvisioningState: "Failed",
			StorageConnections: []string{"missing-storage"}, VectorStoreConnections: []string{"search-conn"},
		}},
		projectConnections: []azure.FoundryConnection{{
			ID: "conn1", Name: "search-conn", Target: azure.ConnectionTarget{ResourceID: searchID}, Error: "token=" + "abcdefghijklmno",
		}},
		accountConnections: []azure.FoundryConnection{},
	}
	res, err := evalRule(t, "FND-RUN-003", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) < 2 {
		t.Fatalf("expected findings, got %+v", res)
	}
	if strings.Contains(res.Findings[0].Evidence, "abcdefghijklmno") {
		t.Fatalf("secret leaked: %+v", res.Findings)
	}
}

func TestRun004MetricsAndProvisioning(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{
		project:            project(),
		hosts:              []azure.CapabilityHost{{ID: "host1", Name: "default", ProvisioningState: "Succeeded", VectorStoreConnections: []string{"search-conn"}}},
		projectConnections: []azure.FoundryConnection{{Name: "search-conn", Target: azure.ConnectionTarget{ResourceID: searchID}}},
		deployments:        []azure.AccountDeployment{{Name: "dep1", ProvisioningState: "Failed"}},
	}
	d.Monitor = fakeMonitor{
		metrics: func(_ context.Context, _ string, metricName, _, _, _ string) ([]azure.MetricTotal, error) {
			switch metricName {
			case "AzureOpenAIRequests":
				return []azure.MetricTotal{{Series: "dep1", Total: 2}}, nil
			default:
				return []azure.MetricTotal{{Series: "dep1", Total: 100}}, nil
			}
		},
	}
	res, err := evalRule(t, "FND-RUN-004", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) < 3 {
		t.Fatalf("expected deployment + metric findings, got %+v", res)
	}
}

func TestRun005AndRun006PrivacyFixtures(t *testing.T) {
	foundrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/agents/support-agent/versions/v2"):
			fmt.Fprint(w, mustRead(t, filepath.Join("..", "..", "runtime", "foundry", "testdata", "agent_version.json")))
		case strings.HasPrefix(r.URL.Path, "/agents"):
			fmt.Fprint(w, mustRead(t, filepath.Join("..", "..", "runtime", "foundry", "testdata", "agents.json")))
		case strings.HasPrefix(r.URL.Path, "/connections"):
			fmt.Fprint(w, mustRead(t, filepath.Join("..", "..", "runtime", "foundry", "testdata", "connections.json")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer foundrySrv.Close()

	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexes('knowledge')/search.stats"):
			fmt.Fprint(w, mustRead(t, filepath.Join("..", "..", "runtime", "search", "testdata", "stats.json")))
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexes('knowledge')"):
			fmt.Fprint(w, mustRead(t, filepath.Join("..", "..", "runtime", "search", "testdata", "index.json")))
		case strings.HasPrefix(r.URL.EscapedPath(), "/indexers('load-knowledge')/search.status"):
			fmt.Fprint(w, mustRead(t, filepath.Join("..", "..", "runtime", "search", "testdata", "indexer_status.json")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer searchSrv.Close()

	d := baseDeps()
	d.ProjectData = fixtureProjectReader{endpoint: foundrySrv.URL, client: foundrydp.HTTPClient{HTTP: foundrySrv.Client(), Credential: staticCred{}}}
	d.SearchData = fixtureSearchClient{serviceURL: searchSrv.URL, client: searchprobe.HTTPClient{HTTP: rewriteClient(searchSrv), Credential: staticCred{}}}
	d.FoundryMgmt = fakeMgmt{
		project: project(),
		hosts: []azure.CapabilityHost{{
			ID: "host1", Name: "default", ProvisioningState: "Succeeded",
			VectorStoreConnections: []string{"search-conn"},
		}},
		projectConnections: []azure.FoundryConnection{{
			ID: "conn1", Name: "search-conn", Category: "AzureAISearch",
			Target: azure.ConnectionTarget{ResourceID: searchID, IndexNames: []string{"knowledge"}, IndexerNames: []string{"load-knowledge"}},
		}},
	}
	in := baseInput()
	res5, err := evalRule(t, "FND-RUN-005", d, in)
	if err != nil {
		t.Fatal(err)
	}
	res6, err := evalRule(t, "FND-RUN-006", d, in)
	if err != nil {
		t.Fatal(err)
	}
	rendered := renderAll(t, append(tagFindings("FND-RUN-005", sdk.SeverityWarning, res5.Findings), tagFindings("FND-RUN-006", sdk.SeverityError, res6.Findings)...), nil)
	for _, s := range []string{"PROMPT_SENTINEL_TEXT", "DOCUMENT_SENTINEL_TEXT", "COMPLETION_SENTINEL_TEXT"} {
		if strings.Contains(rendered, s) {
			t.Fatalf("sentinel leaked in rendered outputs: %q", s)
		}
	}
}

func TestRun006ZeroDocsAndIndexerStates(t *testing.T) {
	d := baseDeps()
	d.SearchData = fakeSearch{
		index:  searchprobe.Index{Name: "knowledge", Fields: []searchprobe.Field{{Name: "v", Type: "Collection(Edm.Single)", Dimensions: 10, VectorSearchProfile: "p"}}},
		stats:  searchprobe.Stats{DocumentCount: 0},
		status: searchprobe.IndexerStatus{Status: "unknown", LastResultStatus: "inProgress"},
	}
	res, err := evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, rt.UncertainPrefix) {
		t.Fatalf("expected uncertain skip, got %+v", res)
	}
}

func TestRun007LogsArrival(t *testing.T) {
	d := baseDeps()
	d.Monitor = fakeMonitor{
		metrics: func(_ context.Context, _ string, metricName, _, _, _ string) ([]azure.MetricTotal, error) {
			if metricName == "AzureOpenAIRequests" {
				return []azure.MetricTotal{{Series: "all", Total: 1}}, nil
			}
			return nil, nil
		},
		diags: []azure.RuntimeDiagnosticSetting{{Name: "to-law", WorkspaceID: "/workspaces/law", Categories: []string{"Audit"}, AgeHours: 3}},
		logs:  azure.RuntimeLogsResult{Table: "AzureDiagnostics", Counts: map[string]int64{"Audit": 0}},
	}
	res, err := evalRule(t, "FND-RUN-007", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("expected finding, got %+v", res)
	}
	d.Monitor = fakeMonitor{
		metrics: func(_ context.Context, _ string, metricName, _, _, _ string) ([]azure.MetricTotal, error) {
			if metricName == "AzureOpenAIRequests" {
				return []azure.MetricTotal{{Series: "all", Total: 1}}, nil
			}
			return nil, nil
		},
		diags: []azure.RuntimeDiagnosticSetting{{Name: "to-law", WorkspaceID: "/workspaces/law", Categories: []string{"Audit"}, AgeHours: 1}},
		logs:  azure.RuntimeLogsResult{Table: "AzureDiagnostics", Counts: map[string]int64{"Audit": 0}},
	}
	res, err = evalRule(t, "FND-RUN-007", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "created recently") {
		t.Fatalf("expected recent-setting uncertainty, got %+v", res)
	}
}

func TestRuleHelpersAndEdgeCases(t *testing.T) {
	if got := needTarget(Target{}, true); !strings.Contains(got, "--subscription") {
		t.Fatalf("needTarget = %q", got)
	}
	if got := azureReason(errors.New("boom")); !strings.Contains(got, "boom") {
		t.Fatalf("azureReason = %q", got)
	}
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{projectErr: errors.New("boom")}
	if _, err := evalRule(t, "FND-RUN-003", d, baseInput()); err != nil {
		t.Fatal(err)
	}
	if lastIDSegment("abc") != "abc" {
		t.Fatal("unexpected last segment")
	}
}

func TestRun005SkipAndStatusBranches(t *testing.T) {
	d := baseDeps()
	d.ProjectData = fakeProjectReader{pingErr: &azure.UnavailableError{Capability: "project endpoint"}}
	res, err := evalRule(t, "FND-RUN-005", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil {
		t.Fatalf("expected skip, got %+v", res)
	}

	d = baseDeps()
	d.ProjectData = fakeProjectReader{
		agents:      []foundrydp.Agent{{Name: "support-agent", LatestVersion: "v2"}},
		versions:    map[string]foundrydp.AgentVersion{"support-agent": {Name: "support-agent", Version: "v2", Status: "Broken"}},
		connections: []foundrydp.Connection{},
	}
	res, err = evalRule(t, "FND-RUN-005", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) == 0 {
		t.Fatalf("expected finding, got %+v", res)
	}
}

func TestRun006SkipAndMissingIndex(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{
		project:            project(),
		hosts:              []azure.CapabilityHost{{ID: "host1", Name: "default", ProvisioningState: "Succeeded"}},
		projectConnections: []azure.FoundryConnection{{Name: "blob", Target: azure.ConnectionTarget{ResourceID: storageID}}},
	}
	res, err := evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil {
		t.Fatalf("expected search skip, got %+v", res)
	}

	d = baseDeps()
	d.SearchData = fakeSearch{indexErr: errors.New("http 404")}
	res, err = evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) == 0 {
		t.Fatalf("expected missing index finding, got %+v", res)
	}
}

func TestRun007UncertainModes(t *testing.T) {
	d := baseDeps()
	d.Monitor = fakeMonitor{diags: []azure.RuntimeDiagnosticSetting{{Name: "diag", WorkspaceID: "law", Categories: []string{"Audit"}, AgeHours: 3}}, logs: azure.RuntimeLogsResult{Table: "CustomTable", Counts: map[string]int64{}}}
	d.Monitor = fakeMonitor{
		metrics: func(_ context.Context, _ string, metricName, _, _, _ string) ([]azure.MetricTotal, error) {
			if metricName == "AzureOpenAIRequests" {
				return []azure.MetricTotal{{Series: "all", Total: 1}}, nil
			}
			return nil, nil
		},
		diags: []azure.RuntimeDiagnosticSetting{{Name: "diag", WorkspaceID: "law", Categories: []string{"Audit"}, AgeHours: 3}},
		logs:  azure.RuntimeLogsResult{Table: "CustomTable", Counts: map[string]int64{}},
	}
	res, err := evalRule(t, "FND-RUN-007", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, rt.UncertainPrefix) {
		t.Fatalf("expected uncertain skip, got %+v", res)
	}
}

func TestRuleEvaluateNilInputAndTargetValidation(t *testing.T) {
	rules := Register(baseDeps())
	res, err := rules[0].Evaluate(context.Background(), nil)
	if err != nil || res.Skipped == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	d := baseDeps()
	d.Target.SubscriptionID = ""
	res, err = evalRule(t, "FND-RUN-004", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "--subscription") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRun003AndRun004SkipBranches(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = nil
	res, err := evalRule(t, "FND-RUN-003", d, baseInput())
	if err != nil || res.Skipped == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	d = baseDeps()
	d.FoundryMgmt = fakeMgmt{deployments: nil}
	res, err = evalRule(t, "FND-RUN-004", d, baseInput())
	if err != nil || res.Skipped == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRun005AdditionalBranches(t *testing.T) {
	d := baseDeps()
	d.ProjectData = fakeProjectReader{
		agents:      []foundrydp.Agent{{Name: "support-agent", LatestVersion: "v1"}},
		versions:    map[string]foundrydp.AgentVersion{"support-agent": {Name: "support-agent", Version: "v1", Status: "Creating"}},
		connections: []foundrydp.Connection{{Name: "search-conn"}, {Name: "storage-conn"}, {Name: "cosmos-conn"}},
	}
	res, err := evalRule(t, "FND-RUN-005", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, "still creating") {
		t.Fatalf("expected uncertain creating status, got %+v", res)
	}

	d = baseDeps()
	d.ProjectData = fakeProjectReader{
		agents:      []foundrydp.Agent{{Name: "support-agent", LatestVersion: "v1"}},
		versions:    map[string]foundrydp.AgentVersion{},
		versionErr:  map[string]error{"support-agent": context.DeadlineExceeded},
		connections: []foundrydp.Connection{{Name: "search-conn"}, {Name: "storage-conn"}, {Name: "cosmos-conn"}},
	}
	res, err = evalRule(t, "FND-RUN-005", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "timed out") {
		t.Fatalf("expected timeout uncertainty, got %+v", res)
	}
}

func TestRun006AdditionalBranches(t *testing.T) {
	d := baseDeps()
	d.SearchData = fakeSearch{
		index:  searchprobe.Index{Name: "knowledge", Fields: []searchprobe.Field{{Name: "v", Type: "Collection(Edm.Single)", Dimensions: 1536, VectorSearchProfile: "p"}}},
		stats:  searchprobe.Stats{DocumentCount: 0},
		status: searchprobe.IndexerStatus{Status: "error", LastResultStatus: "failed"},
	}
	res, err := evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range res.Findings {
		if strings.Contains(f.Evidence, "error state") {
			found = true
			break
		}
	}
	if len(res.Findings) == 0 || !found {
		t.Fatalf("expected indexer error finding, got %+v", res)
	}
}

func TestEnvHelpersAndCaches(t *testing.T) {
	e := &env{d: baseDeps()}
	ctx := context.Background()
	if _, err := e.projectInfo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.projectInfo(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := e.projectConnections(ctx); err != nil || len(got) == 0 {
		t.Fatalf("projectConnections got=%+v err=%v", got, err)
	}
	e.d.FoundryMgmt = fakeMgmt{accountConnections: []azure.FoundryConnection{{Name: "extra"}}}
	if got, err := e.accountConnections(ctx); err != nil || len(got) != 1 {
		t.Fatalf("accountConnections got=%+v err=%v", got, err)
	}
	e.d.FoundryMgmt = fakeMgmt{deployments: []azure.AccountDeployment{{Name: "dep1"}}}
	if got, err := e.deployments(ctx); err != nil || len(got) != 1 {
		t.Fatalf("deployments got=%+v err=%v", got, err)
	}
}

func TestNeedTargetAzureReasonBoundConnectionsAndAgentNames(t *testing.T) {
	if got := needTarget(Target{SubscriptionID: "s"}, true); !strings.Contains(got, "--resource-group") {
		t.Fatalf("needTarget rg = %q", got)
	}
	if got := needTarget(Target{SubscriptionID: "s", ResourceGroup: "rg"}, true); !strings.Contains(got, "--account") {
		t.Fatalf("needTarget account = %q", got)
	}
	if got := needTarget(Target{SubscriptionID: "s", ResourceGroup: "rg", Account: "acct"}, true); !strings.Contains(got, "--project") {
		t.Fatalf("needTarget project = %q", got)
	}
	msg := azureReason(&azure.UnavailableError{Capability: "x", Permission: "perm", Reason: "why"})
	if !strings.Contains(msg, "missing permission perm") {
		t.Fatalf("azureReason unavailable = %q", msg)
	}

	e := &env{d: baseDeps()}
	conns, names, err := e.boundConnections(context.Background())
	if err != nil || len(conns) == 0 || len(names) < 3 {
		t.Fatalf("boundConnections conns=%+v names=%v err=%v", conns, names, err)
	}
	if got := e.agentNames(&sdk.Input{AzureYAML: nil}); got != nil {
		t.Fatalf("expected nil agent names, got %v", got)
	}
}

func TestRun001AndRun007AdditionalSkips(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = nil
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil || res.Skipped == nil {
		t.Fatalf("run001 res=%+v err=%v", res, err)
	}

	d = baseDeps()
	d.FoundryMgmt = fakeMgmt{
		project: project(),
		hosts: []azure.CapabilityHost{{
			ID: "host1", Name: "default", ProvisioningState: "Succeeded",
			StorageConnections: []string{"sas-conn"},
		}},
		projectConnections: []azure.FoundryConnection{{Name: "sas-conn", AuthType: "SAS", Target: azure.ConnectionTarget{ResourceID: storageID}}},
	}
	res, err = evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "cannot be proven") {
		t.Fatalf("run001 auth skip res=%+v err=%v", res, err)
	}

	d = baseDeps()
	d.Monitor = fakeMonitor{diags: []azure.RuntimeDiagnosticSetting{{Name: "diag", WorkspaceID: "law", Categories: []string{"Audit"}, AgeHours: 3}}}
	res, err = evalRule(t, "FND-RUN-007", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "no traffic") {
		t.Fatalf("run007 res=%+v err=%v", res, err)
	}
}

func TestRun005NotFoundBranch(t *testing.T) {
	d := baseDeps()
	d.ProjectData = fakeProjectReader{
		agents:      []foundrydp.Agent{{Name: "support-agent", LatestVersion: "v1"}},
		versions:    map[string]foundrydp.AgentVersion{},
		versionErr:  map[string]error{"support-agent": errors.New("http 404")},
		connections: []foundrydp.Connection{{Name: "search-conn"}, {Name: "storage-conn"}, {Name: "cosmos-conn"}},
	}
	res, err := evalRule(t, "FND-RUN-005", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) == 0 || !strings.Contains(res.Findings[0].Evidence, "was not found") {
		t.Fatalf("expected not-found finding, got %+v", res)
	}
}

func TestEnvErrorBranchesAndRunSkips(t *testing.T) {
	ctx := context.Background()
	e := &env{d: baseDeps()}
	e.d.FoundryMgmt = fakeMgmt{projectErr: errors.New("boom")}
	if _, err := e.projectInfo(ctx); err == nil {
		t.Fatal("expected projectInfo error")
	}
	e = &env{d: baseDeps()}
	e.d.FoundryMgmt = fakeMgmt{projectConnsErr: errors.New("boom")}
	if _, err := e.projectConnections(ctx); err == nil {
		t.Fatal("expected projectConnections error")
	}
	e = &env{d: baseDeps()}
	e.d.FoundryMgmt = fakeMgmt{accountConnsErr: errors.New("boom")}
	if _, err := e.accountConnections(ctx); err == nil {
		t.Fatal("expected accountConnections error")
	}
	e = &env{d: baseDeps()}
	e.d.FoundryMgmt = fakeMgmt{deploymentsErr: errors.New("boom")}
	if _, err := e.deployments(ctx); err == nil {
		t.Fatal("expected deployments error")
	}

	d := baseDeps()
	d.Resolver = nil
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil || res.Skipped == nil {
		t.Fatalf("run002 res=%+v err=%v", res, err)
	}

	d = baseDeps()
	d.FoundryMgmt = fakeMgmt{project: project()}
	res, err = evalRule(t, "FND-RUN-003", d, baseInput())
	if err != nil || res.Skipped == nil {
		t.Fatalf("run003 res=%+v err=%v", res, err)
	}
}

func TestRun004AndRun007MoreBranches(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{deployments: []azure.AccountDeployment{{Name: "dep1", ProvisioningState: "Succeeded"}}}
	d.Monitor = fakeMonitor{}
	res, err := evalRule(t, "FND-RUN-004", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, rt.UncertainPrefix) {
		t.Fatalf("run004 res=%+v err=%v", res, err)
	}

	d = baseDeps()
	d.Monitor = fakeMonitor{diags: nil}
	res, err = evalRule(t, "FND-RUN-007", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "no diagnostic setting") {
		t.Fatalf("run007 missing diag res=%+v err=%v", res, err)
	}

	d = baseDeps()
	d.Monitor = fakeMonitor{
		metrics: func(_ context.Context, _ string, metricName, _, _, _ string) ([]azure.MetricTotal, error) {
			if metricName == "AzureOpenAIRequests" {
				return []azure.MetricTotal{{Series: "all", Total: 2}}, nil
			}
			return nil, nil
		},
		diags: []azure.RuntimeDiagnosticSetting{{Name: "diag", WorkspaceID: "law", Categories: []string{"Audit"}, AgeHours: 3}},
		logs:  azure.RuntimeLogsResult{Table: "AzureDiagnostics", Counts: map[string]int64{"Audit": 2}},
	}
	res, err = evalRule(t, "FND-RUN-007", d, baseInput())
	if err != nil || res.Skipped != nil || len(res.Findings) != 0 {
		t.Fatalf("run007 expected pass, got res=%+v err=%v", res, err)
	}
}

type fakeMgmt struct {
	project            azure.FoundryProject
	projectErr         error
	hosts              []azure.CapabilityHost
	hostsErr           error
	projectConnections []azure.FoundryConnection
	projectConnsErr    error
	accountConnections []azure.FoundryConnection
	accountConnsErr    error
	deployments        []azure.AccountDeployment
	deploymentsErr     error
}

func (f fakeMgmt) GetProject(context.Context, string, string, string, string) (azure.FoundryProject, error) {
	return f.project, f.projectErr
}
func (f fakeMgmt) ListProjectCapabilityHosts(context.Context, string, string, string, string) ([]azure.CapabilityHost, error) {
	return f.hosts, f.hostsErr
}
func (f fakeMgmt) ListAccountCapabilityHosts(context.Context, string, string, string) ([]azure.CapabilityHost, error) {
	return f.hosts, f.hostsErr
}
func (f fakeMgmt) ListProjectConnections(context.Context, string, string, string, string) ([]azure.FoundryConnection, error) {
	return f.projectConnections, f.projectConnsErr
}
func (f fakeMgmt) ListAccountConnections(context.Context, string, string, string) ([]azure.FoundryConnection, error) {
	return f.accountConnections, f.accountConnsErr
}
func (f fakeMgmt) ListAccountDeployments(context.Context, string, string, string) ([]azure.AccountDeployment, error) {
	return f.deployments, f.deploymentsErr
}

type fakeRBAC struct {
	assignments map[string][]azure.RoleAssignment
	assignErr   error
	sql         map[string][]azure.CosmosSQLRoleAssignment
	sqlErr      error
}

func (f fakeRBAC) ListRoleAssignments(context.Context, string, string) ([]azure.RoleAssignment, error) {
	if f.assignErr != nil {
		return nil, f.assignErr
	}
	out := []azure.RoleAssignment{}
	for _, roles := range f.assignments {
		out = append(out, roles...)
	}
	return out, nil
}
func (f fakeRBAC) ListCosmosSQLRoleAssignments(_ context.Context, accountID, _ string) ([]azure.CosmosSQLRoleAssignment, error) {
	if f.sqlErr != nil {
		return nil, f.sqlErr
	}
	return append([]azure.CosmosSQLRoleAssignment(nil), f.sql[accountID]...), nil
}

type fakeMonitor struct {
	metrics func(context.Context, string, string, string, string, string) ([]azure.MetricTotal, error)
	diags   []azure.RuntimeDiagnosticSetting
	diagErr error
	logs    azure.RuntimeLogsResult
	logErr  error
}

func (f fakeMonitor) QueryAccountMetrics(ctx context.Context, accountID, metricName, filterDimension, filterValue, seriesDimension string) ([]azure.MetricTotal, error) {
	if f.metrics == nil {
		return nil, nil
	}
	return f.metrics(ctx, accountID, metricName, filterDimension, filterValue, seriesDimension)
}
func (f fakeMonitor) ListDiagnosticSettings(context.Context, string) ([]azure.RuntimeDiagnosticSetting, error) {
	return f.diags, f.diagErr
}
func (f fakeMonitor) QueryDiagnosticCounts(context.Context, string, string, int) (azure.RuntimeLogsResult, error) {
	return f.logs, f.logErr
}

type fakeProjectReader struct {
	pingErr     error
	agents      []foundrydp.Agent
	agentsErr   error
	versions    map[string]foundrydp.AgentVersion
	versionErr  map[string]error
	connections []foundrydp.Connection
	connectErr  error
}

func (f fakeProjectReader) Ping(context.Context, string) error { return f.pingErr }
func (f fakeProjectReader) ListAgents(context.Context, string) ([]foundrydp.Agent, error) {
	return f.agents, f.agentsErr
}
func (f fakeProjectReader) GetAgentVersion(_ context.Context, _ string, name, _ string) (foundrydp.AgentVersion, error) {
	if err := f.versionErr[name]; err != nil {
		return foundrydp.AgentVersion{}, err
	}
	return f.versions[name], nil
}
func (f fakeProjectReader) ListConnections(context.Context, string) ([]foundrydp.Connection, error) {
	return f.connections, f.connectErr
}

type fixtureProjectReader struct {
	endpoint string
	client   foundrydp.HTTPClient
}

func (f fixtureProjectReader) Ping(ctx context.Context, _ string) error {
	return f.client.Ping(ctx, f.endpoint)
}
func (f fixtureProjectReader) ListAgents(ctx context.Context, _ string) ([]foundrydp.Agent, error) {
	return f.client.ListAgents(ctx, f.endpoint)
}
func (f fixtureProjectReader) GetAgentVersion(ctx context.Context, _ string, name, version string) (foundrydp.AgentVersion, error) {
	return f.client.GetAgentVersion(ctx, f.endpoint, name, version)
}
func (f fixtureProjectReader) ListConnections(ctx context.Context, _ string) ([]foundrydp.Connection, error) {
	return f.client.ListConnections(ctx, f.endpoint)
}

type fakeSearch struct {
	index     searchprobe.Index
	indexErr  error
	stats     searchprobe.Stats
	statsErr  error
	status    searchprobe.IndexerStatus
	statusErr error
}

func (f fakeSearch) GetIndex(context.Context, string, string) (searchprobe.Index, error) {
	return f.index, f.indexErr
}
func (f fakeSearch) GetIndexStatistics(context.Context, string, string) (searchprobe.Stats, error) {
	return f.stats, f.statsErr
}
func (f fakeSearch) GetIndexerStatus(context.Context, string, string) (searchprobe.IndexerStatus, error) {
	return f.status, f.statusErr
}

type fixtureSearchClient struct {
	serviceURL string
	client     searchprobe.HTTPClient
}

func (f fixtureSearchClient) GetIndex(ctx context.Context, _ string, index string) (searchprobe.Index, error) {
	return f.client.GetIndex(ctx, "svc", index)
}
func (f fixtureSearchClient) GetIndexStatistics(ctx context.Context, _ string, index string) (searchprobe.Stats, error) {
	return f.client.GetIndexStatistics(ctx, "svc", index)
}
func (f fixtureSearchClient) GetIndexerStatus(ctx context.Context, _ string, indexer string) (searchprobe.IndexerStatus, error) {
	return f.client.GetIndexerStatus(ctx, "svc", indexer)
}

type fakeResolver struct {
	answers map[string][]string
	err     error
}

func (f fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if x, ok := f.answers[host]; ok {
		return x, nil
	}
	return []string{"10.0.0.4"}, nil
}

type fakeAzureYAML struct{}

func (fakeAzureYAML) Path() string           { return "azure.yaml" }
func (fakeAzureYAML) ServiceNames() []string { return []string{"support"} }
func (fakeAzureYAML) Lookup(path ...string) (string, sdk.Location, bool) {
	if len(path) == 3 && path[0] == "services" && path[1] == "support" && path[2] == "host" {
		return "azure.ai.agent", sdk.Location{File: "azure.yaml", Line: 3}, true
	}
	if len(path) == 3 && path[0] == "services" && path[1] == "support" && path[2] == "name" {
		return "support-agent", sdk.Location{File: "azure.yaml", Line: 4}, true
	}
	return "", sdk.Location{}, false
}

type fakeARM struct{ resources []sdk.ARMResource }

func (f fakeARM) Resources() []sdk.ARMResource { return f.resources }

type staticCred struct{}

func (staticCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "token"}, nil
}

func baseDeps() Deps {
	return Deps{
		FoundryMgmt: fakeMgmt{
			project: project(),
			hosts: []azure.CapabilityHost{{
				ID: "host1", Name: "default", ProvisioningState: "Succeeded",
				StorageConnections: []string{"storage-conn"}, ThreadStorageConnections: []string{"cosmos-conn"}, VectorStoreConnections: []string{"search-conn"},
			}},
			projectConnections: []azure.FoundryConnection{
				{Name: "search-conn", Target: azure.ConnectionTarget{ResourceID: searchID, IndexNames: []string{"knowledge"}, IndexerNames: []string{"load-knowledge"}}},
				{Name: "storage-conn", Target: azure.ConnectionTarget{ResourceID: storageID}},
				{Name: "cosmos-conn", Target: azure.ConnectionTarget{ResourceID: cosmosID}},
			},
		},
		RBAC:    fakeRBAC{assignments: map[string][]azure.RoleAssignment{}, sql: map[string][]azure.CosmosSQLRoleAssignment{}},
		Monitor: fakeMonitor{},
		ProjectData: fakeProjectReader{
			agents:      []foundrydp.Agent{{Name: "support-agent", LatestVersion: "v1"}},
			versions:    map[string]foundrydp.AgentVersion{"support-agent": {Name: "support-agent", Version: "v1", Status: "Active"}},
			connections: []foundrydp.Connection{{Name: "search-conn", Category: "AzureAISearch"}},
		},
		SearchData: fakeSearch{
			index:  searchprobe.Index{Name: "knowledge", Fields: []searchprobe.Field{{Name: "v", Type: "Collection(Edm.Single)", Dimensions: 1536, VectorSearchProfile: "p"}}},
			stats:  searchprobe.Stats{DocumentCount: 1},
			status: searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success"},
		},
		Resolver: fakeResolver{answers: map[string][]string{}},
		Target:   Target{SubscriptionID: subID, ResourceGroup: "rg", Account: "acct", Project: "proj"},
		Options:  rt.Options{Timeout: time.Second, Vantage: rt.VantageLocal},
	}
}

func baseInput() *sdk.Input {
	return &sdk.Input{
		AzureYAML: fakeAzureYAML{},
		ARM: fakeARM{resources: []sdk.ARMResource{
			{Type: "Microsoft.Search/searchServices", Name: "searchsvc", Location: sdk.Location{File: "main.bicep", Line: 10}},
			{Type: "Microsoft.CognitiveServices/accounts/projects", Name: "proj", Location: sdk.Location{File: "main.bicep", Line: 20}},
		}},
	}
}

func evalRule(t *testing.T, id string, d Deps, in *sdk.Input) (sdk.Result, error) {
	t.Helper()
	for _, r := range Register(d) {
		if r.ID() == id {
			return r.Evaluate(context.Background(), in)
		}
	}
	t.Fatalf("rule %s not found", id)
	return sdk.Result{}, nil
}

func renderAll(t *testing.T, fs []sdk.Finding, skips []sdk.Skip) string {
	t.Helper()
	var b bytes.Buffer
	for _, f := range []report.Format{report.FormatConsole, report.FormatJSON, report.FormatMarkdown, report.FormatSARIF} {
		if err := report.Write(&b, f, fs, report.Run{ToolVersion: "test", Profile: "prod", Skipped: skips, ExitCode: 1}); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

func tagFindings(ruleID string, sev sdk.Severity, fs []sdk.Finding) []sdk.Finding {
	out := make([]sdk.Finding, len(fs))
	for i, f := range fs {
		f.RuleID = ruleID
		f.Severity = sev
		f.RuleVersion = 1
		out[i] = f
	}
	return out
}

func rewriteClient(srv *httptest.Server) *http.Client {
	hc := srv.Client()
	base := hc.Transport
	hc.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return base.RoundTrip(r)
	})
	return hc
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

func project() azure.FoundryProject {
	return azure.FoundryProject{
		ID:                accountID(Target{SubscriptionID: subID, ResourceGroup: "rg", Account: "acct"}) + "/projects/proj",
		Name:              "proj",
		AccountID:         accountID(Target{SubscriptionID: subID, ResourceGroup: "rg", Account: "acct"}),
		PrincipalID:       principalID,
		IdentityType:      "SystemAssigned",
		ProvisioningState: "Succeeded",
	}
}

const (
	subID       = "11111111-1111-1111-1111-111111111111"
	principalID = "22222222-2222-2222-2222-222222222222"
	searchID    = "/subscriptions/" + subID + "/resourceGroups/rg/providers/Microsoft.Search/searchServices/searchsvc"
	storageID   = "/subscriptions/" + subID + "/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/storageacct"
	cosmosID    = "/subscriptions/" + subID + "/resourceGroups/rg/providers/Microsoft.DocumentDB/databaseAccounts/cosmosacct"
)
