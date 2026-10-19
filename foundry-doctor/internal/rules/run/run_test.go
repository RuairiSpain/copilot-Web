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
	netprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/network"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/rbac"
	searchprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/search"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	subID       = "11111111-1111-1111-1111-111111111111"
	principalID = "22222222-2222-2222-2222-222222222222"
	uaPrincipal = "33333333-3333-3333-3333-333333333333"
	rgScope     = "/subscriptions/" + subID + "/resourceGroups/rg"
	searchID    = rgScope + "/providers/Microsoft.Search/searchServices/searchsvc"
	storageID   = rgScope + "/providers/Microsoft.Storage/storageAccounts/storageacct"
	cosmosID    = rgScope + "/providers/Microsoft.DocumentDB/databaseAccounts/cosmosacct"
	roleBase    = "/subscriptions/" + subID + "/providers/Microsoft.Authorization/roleDefinitions/"
	cosmosRole  = cosmosID + "/sqlRoleDefinitions/" + rbac.CosmosDataContributorID
)

var roleGUID = map[string]string{
	rbac.SearchServiceContributor:   "7ca78c08-252a-4471-8644-bb5ff32d4ba0",
	rbac.SearchIndexDataContributor: "8ebe5a00-799e-43f5-93ac-243d3dce84a7",
	rbac.StorageBlobDataContributor: "ba92f5b4-2d11-453d-a403-e96b0029c9fe",
	rbac.StorageBlobDataOwner:       "b7e6dc6d-f1e8-4753-8033-0f276bb0955b",
	rbac.StorageAccountContributor:  "17d1049b-9a84-46fb-8f53-869881c3d3ab",
	rbac.CosmosDBOperator:           "230815da-be43-4aae-9cb4-875f7bd000aa",
}

func asg(scope string, keys ...string) azure.RoleAssignmentSet {
	var s azure.RoleAssignmentSet
	for _, k := range keys {
		s.Covering = append(s.Covering, azure.RoleAssignment{RoleDefinitionID: roleBase + roleGUID[k], Scope: scope, PrincipalID: principalID})
	}
	return s
}

func allGranted() fakeRBAC {
	return fakeRBAC{
		sets: map[string]azure.RoleAssignmentSet{
			searchID:  asg(searchID, rbac.SearchServiceContributor, rbac.SearchIndexDataContributor),
			storageID: asg(storageID, rbac.StorageBlobDataContributor, rbac.StorageBlobDataOwner, rbac.StorageAccountContributor),
			cosmosID:  asg(cosmosID, rbac.CosmosDBOperator),
		},
		sql: map[string][]azure.CosmosSQLRoleAssignment{cosmosID: {{RoleDefinitionID: cosmosRole, Scope: cosmosID, PrincipalID: principalID}}},
	}
}

func joinFindings(fs []sdk.Finding) string {
	var parts []string
	for _, f := range fs {
		parts = append(parts, f.Evidence)
	}
	return strings.Join(parts, "\n")
}

func TestRun001MissingRolesCarryIdentityTargetAndRoleNames(t *testing.T) {
	d := baseDeps()
	d.RBAC = fakeRBAC{sets: map[string]azure.RoleAssignmentSet{}, sql: map[string][]azure.CosmosSQLRoleAssignment{}}
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	all := joinFindings(res.Findings)
	for _, want := range []string{
		principalID, searchID, storageID, cosmosID,
		"Search Service Contributor", "Search Index Data Contributor", "Storage Blob Data Contributor", "Storage Blob Data Owner",
		"Storage Account Contributor", "Cosmos DB Operator", "Cosmos DB Built-in Data Contributor",
		"group assignments and deny assignments are not evaluated",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("findings lack %q:\n%s", want, all)
		}
	}
	if len(res.Findings) != 7 {
		t.Errorf("findings = %d, want 7:\n%s", len(res.Findings), all)
	}
	for _, f := range res.Findings {
		if f.Confidence != sdk.ConfidenceMedium {
			t.Errorf("missing-role confidence = %q, want capped at medium (likely)", f.Confidence)
		}
		if f.Location.File == "azure.yaml" {
			t.Errorf("a %s finding got the unrelated azure.yaml agent location: %+v", f.Resource.Type, f.Location)
		}
	}
}

func TestRun001GrantedIsPassAndUserAssignedNeedsNoSystemOnlyRoles(t *testing.T) {
	d := baseDeps()
	d.RBAC = allGranted()
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil || res.Skipped != nil || len(res.Findings) != 0 {
		t.Fatalf("all roles granted must pass: %+v err=%v", res, err)
	}

	// User-assigned only: no Storage Account Contributor / Cosmos DB Operator requirement,
	// and the user-assigned principal is the identity that is checked.
	d = baseDeps()
	p := project()
	p.PrincipalID, p.IdentityType, p.UserAssignedPrincipalIDs = "", "UserAssigned", []string{uaPrincipal}
	d.FoundryMgmt = withProject(d.FoundryMgmt.(fakeMgmt), p)
	d.RBAC = fakeRBAC{
		sets: map[string]azure.RoleAssignmentSet{
			searchID:  asg(searchID, rbac.SearchServiceContributor, rbac.SearchIndexDataContributor),
			storageID: asg(storageID, rbac.StorageBlobDataContributor, rbac.StorageBlobDataOwner),
		},
		sql:  map[string][]azure.CosmosSQLRoleAssignment{cosmosID: {{RoleDefinitionID: cosmosRole, Scope: cosmosID + "/dbs/enterprise_memory", PrincipalID: uaPrincipal}}},
		only: uaPrincipal,
	}
	res, err = evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil || res.Skipped != nil || len(res.Findings) != 0 {
		t.Fatalf("user-assigned project with its roles must pass: %+v err=%v", res, err)
	}
}

func TestRun001UncertainInsteadOfFail(t *testing.T) {
	custom := roleBase + "99999999-9999-9999-9999-999999999999"
	cases := []struct {
		name string
		rb   fakeRBAC
		want string
	}{
		{"custom role on search", func() fakeRBAC {
			r := allGranted()
			r.sets[searchID] = azure.RoleAssignmentSet{Covering: []azure.RoleAssignment{{RoleDefinitionID: custom, Scope: searchID}}}
			return r
		}(), "custom or unrecognised"},
		{"conditional assignment", func() fakeRBAC {
			r := allGranted()
			r.sets[searchID] = azure.RoleAssignmentSet{Covering: []azure.RoleAssignment{
				{RoleDefinitionID: roleBase + roleGUID[rbac.SearchServiceContributor], Scope: searchID, Condition: "cond"},
				{RoleDefinitionID: roleBase + roleGUID[rbac.SearchIndexDataContributor], Scope: searchID, Condition: "cond"},
			}}
			return r
		}(), "conditional"},
		{"container-scoped blob owner", func() fakeRBAC {
			r := allGranted()
			r.sets[storageID] = azure.RoleAssignmentSet{
				Covering: asg(storageID, rbac.StorageBlobDataContributor, rbac.StorageAccountContributor).Covering,
				Below:    []azure.RoleAssignment{{RoleDefinitionID: roleBase + roleGUID[rbac.StorageBlobDataOwner], Scope: storageID + "/blobServices/default/containers/c"}},
			}
			return r
		}(), "below the target"},
		{"cosmos custom sql role", func() fakeRBAC {
			r := allGranted()
			r.sql[cosmosID] = []azure.CosmosSQLRoleAssignment{{RoleDefinitionID: cosmosID + "/sqlRoleDefinitions/11111111-0000-0000-0000-000000000000", Scope: cosmosID}}
			return r
		}(), "custom roles"},
		{"cosmos container scope", func() fakeRBAC {
			r := allGranted()
			r.sql[cosmosID] = []azure.CosmosSQLRoleAssignment{{RoleDefinitionID: cosmosRole, Scope: cosmosID + "/dbs/enterprise_memory/colls/c"}}
			return r
		}(), "container scope"},
		{"truncated listing", func() fakeRBAC {
			r := allGranted()
			s := r.sets[searchID]
			s.Covering, s.Truncated = nil, true
			r.sets[searchID] = s
			return r
		}(), "truncated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := baseDeps()
			d.RBAC = c.rb
			res, err := evalRule(t, "FND-RUN-001", d, baseInput())
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, c.want) {
				t.Fatalf("want uncertain containing %q, got %+v", c.want, res)
			}
		})
	}
}

func TestRun001SkipsAndInputs(t *testing.T) {
	d := baseDeps()
	d.RBAC = fakeRBAC{assignErr: &azure.UnavailableError{Capability: "roleAssignments", Permission: "Microsoft.Authorization/roleAssignments/read"}}
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "roleAssignments") || !strings.HasPrefix(res.Skipped.Reason, "input-unavailable") {
		t.Fatalf("permission skip: %+v err=%v", res, err)
	}

	d = baseDeps()
	d.FoundryMgmt = nil
	if res, err = evalRule(t, "FND-RUN-001", d, baseInput()); err != nil || res.Skipped == nil {
		t.Fatalf("nil mgmt: %+v err=%v", res, err)
	}

	d = baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	m.projectConnections = []azure.FoundryConnection{{Name: "sas-conn", AuthType: "SAS", Target: azure.ConnectionTarget{ResourceID: storageID}}}
	m.hosts = []azure.CapabilityHost{{ID: "h", Name: "default", ProvisioningState: "Succeeded", StorageConnections: []string{"sas-conn"}}}
	d.FoundryMgmt = m
	if res, err = evalRule(t, "FND-RUN-001", d, baseInput()); err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "cannot be proven") {
		t.Fatalf("auth skip: %+v err=%v", res, err)
	}

	d = baseDeps()
	p := project()
	p.PrincipalID, p.UserAssignedPrincipalIDs = "", nil
	d.FoundryMgmt = withProject(d.FoundryMgmt.(fakeMgmt), p)
	if res, err = evalRule(t, "FND-RUN-001", d, baseInput()); err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "no managed identity principal") {
		t.Fatalf("no principal: %+v err=%v", res, err)
	}

	d = baseDeps()
	m = d.FoundryMgmt.(fakeMgmt)
	m.projectConnections = []azure.FoundryConnection{{Name: "search-conn", Target: azure.ConnectionTarget{ResourceID: searchID + "/../../x"}}}
	m.hosts = []azure.CapabilityHost{{ID: "h", Name: "default", ProvisioningState: "Succeeded", VectorStoreConnections: []string{"search-conn"}}}
	d.FoundryMgmt = m
	d.RBAC = fakeRBAC{}
	res, err = evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "malformed") || res.Findings != nil {
		t.Fatalf("hostile resource id must be rejected before any request: %+v err=%v", res, err)
	}
}

func TestRun002ProofVantageAndDNSOutcomes(t *testing.T) {
	disabled := azure.AccountNetwork{PublicNetworkAccess: "Disabled", CustomSubDomainName: "acct"}
	approvedPE := azure.AccountNetwork{PublicNetworkAccess: "Enabled", CustomSubDomainName: "acct", PrivateEndpointConnections: 1, ApprovedPrivateEndpointConnections: 1}
	acctHost := func(suffix string) string { return "acct" + suffix }
	cases := []struct {
		name     string
		net      azure.AccountNetwork
		netErr   error
		vantage  rt.Vantage
		resolver fakeResolver
		wantFind string // substring of the finding evidence; empty means none expected
		wantSkip string // substring of the skip reason; empty means no skip
	}{
		{"public IP on a private-endpoint account fails", disabled, nil, rt.VantageVNet,
			fakeResolver{answers: map[string][]string{acctHost(".cognitiveservices.azure.com"): {"52.1.1.1"}}}, "asserted with --vantage vnet", ""},
		{"private answers pass", disabled, nil, rt.VantageVNet, fakeResolver{}, "", ""},
		{"NXDOMAIN with a private endpoint fails", approvedPE, nil, rt.VantageVNet,
			fakeResolver{errs: map[string]error{acctHost(".openai.azure.com"): netprobe.ErrNXDomain}}, "NXDOMAIN", ""},
		{"timeout is skipped, never a failure", disabled, nil, rt.VantageVNet, fakeResolver{err: context.DeadlineExceeded}, "", "timeout"},
		{"public network access enabled and no endpoint: the skip is reachable", azure.AccountNetwork{PublicNetworkAccess: "Enabled", CustomSubDomainName: "acct"}, nil, rt.VantageVNet, fakeResolver{}, "", "private-endpoint backing was not proven for any dependency"},
		{"a Rejected private endpoint connection is not proof", azure.AccountNetwork{PublicNetworkAccess: "Enabled", CustomSubDomainName: "acct", PrivateEndpointConnections: 1}, nil, rt.VantageVNet,
			fakeResolver{answers: map[string][]string{acctHost(".cognitiveservices.azure.com"): {"52.1.1.1"}}}, "", "none is Approved"},
		{"Pending and Rejected alongside an Approved one still prove it", azure.AccountNetwork{PublicNetworkAccess: "Enabled", CustomSubDomainName: "acct", PrivateEndpointConnections: 3, ApprovedPrivateEndpointConnections: 1}, nil, rt.VantageVNet,
			fakeResolver{answers: map[string][]string{acctHost(".cognitiveservices.azure.com"): {"52.1.1.1"}}}, "approved private endpoint", ""},
		{"CGNAT answer on a private-endpoint account is uncertain, not a failure", disabled, nil, rt.VantageVNet,
			fakeResolver{answers: map[string][]string{acctHost(".cognitiveservices.azure.com"): {"100.64.1.1"}}}, "", "neither a private nor a clearly public"},
		{"private endpoint account without customSubDomainName is uncertain", azure.AccountNetwork{PublicNetworkAccess: "Disabled"}, nil, rt.VantageVNet, fakeResolver{}, "", "no customSubDomainName"},
		{"publicNetworkAccess not reported is uncertain", azure.AccountNetwork{CustomSubDomainName: "acct"}, nil, rt.VantageVNet, fakeResolver{}, "", "does not report publicNetworkAccess"},
		{"vantage none skips with the VNet explanation", disabled, nil, rt.VantageNone, fakeResolver{}, "", "inside the VNet"},
		{"vantage local skips", disabled, nil, rt.VantageLocal, fakeResolver{}, "", "--vantage vnet"},
		{"unreadable account network is not proof", azure.AccountNetwork{}, &azure.UnavailableError{Capability: "accounts/read", Permission: "Microsoft.CognitiveServices/accounts/read"}, rt.VantageVNet, fakeResolver{}, "", "could not be read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := baseDeps()
			m := d.FoundryMgmt.(fakeMgmt)
			m.net, m.netErr = c.net, c.netErr
			m.hosts, m.projectConnections = nil, nil // connection-level proof is tested separately
			d.FoundryMgmt = m
			d.Options.Vantage = c.vantage
			d.Resolver = c.resolver
			res, err := evalRule(t, "FND-RUN-002", d, baseInput())
			if err != nil {
				t.Fatal(err)
			}
			if c.wantFind != "" {
				if len(res.Findings) == 0 || !strings.Contains(joinFindings(res.Findings), c.wantFind) {
					t.Fatalf("want a finding containing %q, got %+v", c.wantFind, res)
				}
				return
			}
			if len(res.Findings) != 0 {
				t.Fatalf("unexpected findings: %+v", res.Findings)
			}
			if (c.wantSkip == "") != (res.Skipped == nil) || (c.wantSkip != "" && !strings.Contains(res.Skipped.Reason, c.wantSkip)) {
				t.Fatalf("skip = %+v, want %q", res.Skipped, c.wantSkip)
			}
		})
	}
}

func TestRun002ConnectionLevelProof(t *testing.T) {
	d := baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	m.net = azure.AccountNetwork{PublicNetworkAccess: "Enabled"}
	m.projectConnections = []azure.FoundryConnection{
		{Name: "search-conn", PERequirement: "Required", PEStatus: "Active", Target: azure.ConnectionTarget{ResourceID: searchID}},
		{Name: "storage-conn", PERequirement: "NotApplicable", PEStatus: "NotApplicable", Target: azure.ConnectionTarget{ResourceID: storageID}},
		{Name: "cosmos-conn", Target: azure.ConnectionTarget{ResourceID: cosmosID}},
	}
	d.FoundryMgmt = m
	d.Options.Vantage = rt.VantageVNet
	d.Resolver = fakeResolver{answers: map[string][]string{
		"searchsvc.search.windows.net":      {"20.1.1.1"},
		"storageacct.blob.core.windows.net": {"20.2.2.2"}, // not proven private: must not be probed or failed
	}}
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 || res.Findings[0].Resource.Type != "Microsoft.Search/searchServices" || !strings.Contains(res.Findings[0].Evidence, "peStatus Active") {
		t.Fatalf("want exactly the proven search finding, got %+v", res)
	}
	if res.Findings[0].Location.File == "azure.yaml" {
		t.Errorf("search finding got the unrelated agent location")
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
	if len(res.Findings) < 3 {
		t.Fatalf("expected findings, got %+v", res)
	}
	if strings.Contains(joinFindings(res.Findings), "abcdefghijklmno") {
		t.Fatalf("secret leaked: %+v", res.Findings)
	}
}

func TestRun003ScopePolicy(t *testing.T) {
	other := "/subscriptions/" + subID + "/resourceGroups/other-rg/providers/Microsoft.Search/searchServices/foreign"
	mk := func(rid, category string) Deps {
		d := baseDeps()
		m := d.FoundryMgmt.(fakeMgmt)
		m.hosts = []azure.CapabilityHost{{ID: "h", Name: "default", ProvisioningState: "Succeeded", VectorStoreConnections: []string{"search-conn"}}}
		m.projectConnections = []azure.FoundryConnection{{ID: "c1", Name: "search-conn", Category: category, Target: azure.ConnectionTarget{ResourceID: rid}}}
		d.FoundryMgmt = m
		return d
	}
	t.Run("positive: outside the resource group and not approved", func(t *testing.T) {
		res, err := evalRule(t, "FND-RUN-003", mk(other, "CognitiveSearch"), baseInput())
		if err != nil || len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, other) || !strings.Contains(res.Findings[0].Evidence, "--approved-scope") {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})
	t.Run("inside the resource group is clean", func(t *testing.T) {
		res, err := evalRule(t, "FND-RUN-003", mk(searchID, "CognitiveSearch"), baseInput())
		if err != nil || res.Skipped != nil || len(res.Findings) != 0 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})
	t.Run("approved scope covers it on a path boundary", func(t *testing.T) {
		d := mk(other, "CognitiveSearch")
		d.ApprovedScopes = []string{"/subscriptions/" + subID + "/resourceGroups/other-rg"}
		if res, err := evalRule(t, "FND-RUN-003", d, baseInput()); err != nil || res.Skipped != nil || len(res.Findings) != 0 {
			t.Fatalf("approved: res=%+v err=%v", res, err)
		}
		d.ApprovedScopes = []string{"/subscriptions/" + subID + "/resourceGroups/other"} // name prefix is not a boundary
		if res, err := evalRule(t, "FND-RUN-003", d, baseInput()); err != nil || len(res.Findings) != 1 {
			t.Fatalf("prefix scope must not approve: res=%+v err=%v", res, err)
		}
	})
	t.Run("uncertain: unresolved expression", func(t *testing.T) {
		res, err := evalRule(t, "FND-RUN-003", mk("[parameters('searchId')]", "CognitiveSearch"), baseInput())
		if err != nil || len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, "unresolved expression") {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})
	t.Run("uncertain: resource-backed connection without a resource ID", func(t *testing.T) {
		res, err := evalRule(t, "FND-RUN-003", mk("", "CognitiveSearch"), baseInput())
		if err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if res, err = evalRule(t, "FND-RUN-003", mk("", "ApiKey"), baseInput()); err != nil || res.Skipped != nil || len(res.Findings) != 0 {
			t.Fatalf("a URL-only connection is not subject to the policy: res=%+v err=%v", res, err)
		}
	})
}

func TestRun003SkipBranches(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = nil
	res, err := evalRule(t, "FND-RUN-003", d, baseInput())
	if err != nil || res.Skipped == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	d = baseDeps()
	d.FoundryMgmt = fakeMgmt{project: project()}
	if res, err = evalRule(t, "FND-RUN-003", d, baseInput()); err != nil || res.Skipped == nil {
		t.Fatalf("no capability host: res=%+v err=%v", res, err)
	}
}

func mseries(v float64, kv ...string) azure.MetricSeries {
	s := azure.MetricSeries{Dimensions: map[string]string{}, Value: v, Points: 1}
	for i := 0; i+1 < len(kv); i += 2 {
		s.Dimensions[kv[i]] = kv[i+1]
	}
	return s
}

// monitorFor answers the three RUN-004/007 metric reads by their query shape.
func monitorFor(traffic, status, util azure.MetricResult) fakeMonitor {
	return fakeMonitor{metrics: func(q azure.MetricQuery) (azure.MetricResult, error) {
		switch {
		case q.MetricName == "AzureOpenAIProvisionedManagedUtilizationV2":
			return util, nil
		case len(q.SplitDimensions) == 0:
			return traffic, nil
		default:
			return status, nil
		}
	}}
}

func deployDeps(m fakeMonitor, deps ...azure.AccountDeployment) Deps {
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{project: project(), deployments: deps}
	d.Monitor = m
	return d
}

func TestRun004AttributesSeriesToDeployments(t *testing.T) {
	m := monitorFor(azure.MetricResult{}, azure.MetricResult{Series: []azure.MetricSeries{
		mseries(50, "statuscode", "429", "modeldeploymentname", "gpt-main"),
		mseries(450, "statuscode", "200", "modeldeploymentname", "gpt-main"),
		mseries(0, "statuscode", "429", "modeldeploymentname", "quiet"),
		mseries(60, "statuscode", "429", "modeldeploymentname", "gpt-ptu"),
		mseries(540, "statuscode", "200", "modeldeploymentname", "gpt-ptu"),
	}}, azure.MetricResult{Series: []azure.MetricSeries{
		mseries(100, "modeldeploymentname", "gpt-ptu"),
		mseries(60, "modeldeploymentname", "gpt-low"),
	}})
	d := deployDeps(m, azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded"}, azure.AccountDeployment{Name: "broken", ProvisioningState: "Failed"})
	res, err := evalRule(t, "FND-RUN-004", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 4 {
		t.Fatalf("findings = %+v", res.Findings)
	}
	acct := accountID(d.Target)
	byID := map[string]sdk.Finding{}
	for _, f := range res.Findings {
		byID[f.Resource.ID+"|"+f.Key] = f
		if f.Key == "" {
			t.Errorf("a RUN-004 finding has no stable key: %+v", f)
		}
		if f.Resource.Name == "AzureOpenAIRequests" || f.Resource.Name == "AzureOpenAIProvisionedManagedUtilizationV2" || strings.Contains(f.Resource.ID, "AzureOpenAI") {
			t.Errorf("a metric name was used as a deployment: %+v", f.Resource)
		}
	}
	if f, ok := byID[acct+"/deployments/gpt-main|throttling-429"]; !ok || !strings.Contains(f.Evidence, "50 HTTP 429 responses of 500 requests (10.0%)") || !strings.Contains(f.Evidence, "last 60 minutes") || !strings.Contains(f.Evidence, "at least 100 requests") || f.Resource.Name != "gpt-main" {
		t.Errorf("429 finding = %+v", f)
	}
	if f, ok := byID[acct+"/deployments/gpt-ptu|utilization"]; !ok || !strings.Contains(f.Evidence, "100%") {
		t.Errorf("utilisation finding = %+v", f)
	}
	if _, ok := byID[acct+"/deployments/broken|provisioning-state"]; !ok {
		t.Errorf("provisioning finding missing: %+v", byID)
	}
}

func TestRun004UncertainAndQuietCases(t *testing.T) {
	ok := azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded"}
	cases := []struct {
		name     string
		m        fakeMonitor
		wantNote string // empty: clean pass
	}{
		{"numeric codes without 429 mean none occurred", monitorFor(azure.MetricResult{}, azure.MetricResult{Series: []azure.MetricSeries{mseries(40, "statuscode", "200", "modeldeploymentname", "gpt-main")}}, azure.MetricResult{}), ""},
		{"429 series with zero total is a healthy pass", monitorFor(azure.MetricResult{}, azure.MetricResult{Series: []azure.MetricSeries{mseries(0, "statuscode", "429", "modeldeploymentname", "gpt-main")}}, azure.MetricResult{}), ""},
		{"category values cannot identify 429", monitorFor(azure.MetricResult{}, azure.MetricResult{Series: []azure.MetricSeries{mseries(9, "statuscode", "clienterrors", "modeldeploymentname", "gpt-main"), mseries(40, "statuscode", "successful", "modeldeploymentname", "gpt-main")}}, azure.MetricResult{}), "no StatusCode value could be identified as 429 (observed: clienterrors, successful)"},
		{"no time series is not zero", monitorFor(azure.MetricResult{}, azure.MetricResult{}, azure.MetricResult{}), "no time series"},
		{"truncated series", monitorFor(azure.MetricResult{}, azure.MetricResult{Truncated: true, Series: []azure.MetricSeries{mseries(1, "statuscode", "200")}}, azure.MetricResult{}), "maximum number of series"},
		{"metric error code", monitorFor(azure.MetricResult{}, azure.MetricResult{ErrorCode: "BadRequest", Series: []azure.MetricSeries{mseries(1, "statuscode", "200")}}, azure.MetricResult{}), "errorCode BadRequest"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := evalRule(t, "FND-RUN-004", deployDeps(c.m, ok), baseInput())
			if err != nil || len(res.Findings) != 0 {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if c.wantNote == "" {
				if res.Skipped != nil {
					t.Fatalf("want pass, got %+v", res.Skipped)
				}
				return
			}
			if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, c.wantNote) {
				t.Fatalf("skip = %+v, want uncertain containing %q", res.Skipped, c.wantNote)
			}
		})
	}
}

func TestRun004UnattributableAndHostileSeries(t *testing.T) {
	m := monitorFor(azure.MetricResult{}, azure.MetricResult{Series: []azure.MetricSeries{
		mseries(30, "statuscode", "429"),
		mseries(270, "statuscode", "200"),
		mseries(20, "statuscode", "429", "modeldeploymentname", "x/../../y"),
		mseries(180, "statuscode", "200", "modeldeploymentname", "x/../../y"),
	}}, azure.MetricResult{Series: []azure.MetricSeries{mseries(120, "modeldeploymentname", "bad name")}})
	d := deployDeps(m, azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded"})
	res, err := evalRule(t, "FND-RUN-004", d, baseInput())
	if err != nil || len(res.Findings) != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for _, f := range res.Findings {
		if f.Resource.ID != accountID(d.Target) || strings.Contains(f.Evidence, "..") || strings.Contains(f.Evidence, "bad name") {
			t.Errorf("an unvalidated series name reached a finding: %+v", f)
		}
	}
}

func TestRun004SkipBranches(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{deployments: nil}
	if res, err := evalRule(t, "FND-RUN-004", d, baseInput()); err != nil || res.Skipped == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	d = baseDeps()
	d.Target.SubscriptionID = ""
	if res, err := evalRule(t, "FND-RUN-004", d, baseInput()); err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "--subscription") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	d = deployDeps(fakeMonitor{}, azure.AccountDeployment{Name: "d", ProvisioningState: "Creating"})
	if res, err := evalRule(t, "FND-RUN-004", d, baseInput()); err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func agentDeps(r fakeProjectReader) Deps {
	d := baseDeps()
	d.ProjectData = r
	return d
}

func TestRun005AgentStatusesFollowTheSpec(t *testing.T) {
	cases := []struct {
		name     string
		agent    foundrydp.Agent
		wantFind string
		wantNote string
	}{
		{"active", foundrydp.Agent{Name: "support-agent", LatestVersion: "2", LatestStatus: "active"}, "", ""},
		{"absent status means active, never a failure", foundrydp.Agent{Name: "support-agent", LatestVersion: "2"}, "", ""},
		{"creating is uncertain", foundrydp.Agent{Name: "support-agent", LatestVersion: "2", LatestStatus: "creating"}, "", "still creating"},
		{"failed", foundrydp.Agent{Name: "support-agent", LatestVersion: "2", LatestStatus: "failed"}, "status is failed", ""},
		{"deleted", foundrydp.Agent{Name: "support-agent", LatestVersion: "2", LatestStatus: "deleted"}, "status is deleted", ""},
		{"unknown status is uncertain", foundrydp.Agent{Name: "support-agent", LatestVersion: "2", LatestStatus: "Broken"}, "", "unrecognised"},
		{"disabled agent is uncertain", foundrydp.Agent{Name: "support-agent", LatestVersion: "2", State: "disabled"}, "", "disabled"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := agentDeps(fakeProjectReader{
				agents:      foundrydp.AgentList{Agents: []foundrydp.Agent{c.agent}},
				connections: foundrydp.ConnectionList{Connections: []foundrydp.Connection{{Name: "search-conn"}, {Name: "storage-conn"}, {Name: "cosmos-conn"}}},
			})
			res, err := evalRule(t, "FND-RUN-005", d, baseInput())
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.wantFind != "":
				if len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, c.wantFind) || res.Findings[0].Location.File != "azure.yaml" {
					t.Fatalf("res=%+v", res)
				}
			case c.wantNote != "":
				if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, c.wantNote) {
					t.Fatalf("res=%+v", res)
				}
			default:
				if res.Skipped != nil || len(res.Findings) != 0 {
					t.Fatalf("want pass: %+v", res)
				}
			}
		})
	}
}

func TestRun005MissingAgentAndConnectionsAndCaps(t *testing.T) {
	conns := foundrydp.ConnectionList{Connections: []foundrydp.Connection{{Name: "search-conn"}, {Name: "storage-conn"}, {Name: "cosmos-conn"}}}
	res, err := evalRule(t, "FND-RUN-005", agentDeps(fakeProjectReader{connections: conns}), baseInput())
	if err != nil || len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, "is not listed") {
		t.Fatalf("missing agent: %+v err=%v", res, err)
	}
	res, err = evalRule(t, "FND-RUN-005", agentDeps(fakeProjectReader{agents: foundrydp.AgentList{Truncated: true}, connections: conns}), baseInput())
	if err != nil || len(res.Findings) != 0 || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "capped") {
		t.Fatalf("capped agent listing must be uncertain: %+v err=%v", res, err)
	}
	ok := foundrydp.AgentList{Agents: []foundrydp.Agent{{Name: "support-agent", LatestStatus: "active"}}}
	res, err = evalRule(t, "FND-RUN-005", agentDeps(fakeProjectReader{agents: ok, connections: foundrydp.ConnectionList{Connections: []foundrydp.Connection{{Name: "search-conn"}}}}), baseInput())
	if err != nil || len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) ||
		!strings.Contains(res.Skipped.Reason, "storage-conn") || !strings.Contains(res.Skipped.Reason, "cosmos-conn") || !strings.Contains(res.Skipped.Reason, "account-level connection may not appear") {
		t.Fatalf("a bound connection missing from the project listing is uncertain, never a failure: %+v err=%v", res, err)
	}
	res, err = evalRule(t, "FND-RUN-005", agentDeps(fakeProjectReader{agents: ok, connections: foundrydp.ConnectionList{Truncated: true}}), baseInput())
	if err != nil || len(res.Findings) != 0 || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "capped") {
		t.Fatalf("capped connection listing must be uncertain: %+v err=%v", res, err)
	}
}

func TestRun005SkipsAndTypedErrors(t *testing.T) {
	res, err := evalRule(t, "FND-RUN-005", agentDeps(fakeProjectReader{pingErr: &azure.UnavailableError{Capability: "project endpoint"}}), baseInput())
	if err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, "input-unavailable") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	d := baseDeps()
	d.ProjectData = nil
	if res, err = evalRule(t, "FND-RUN-005", d, baseInput()); err != nil || res.Skipped == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func goodIndex() searchprobe.Index {
	return searchprobe.Index{Name: "knowledge", VectorProfiles: []string{"p"}, Fields: []searchprobe.Field{{Name: "v", Type: "Collection(Edm.Single)", Dimensions: 1536, VectorSearchProfile: "p"}}}
}

func searchDeps(s fakeSearch, hint azure.ConnectionTarget) Deps {
	d := baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	hint.ResourceID = searchID
	m.projectConnections = []azure.FoundryConnection{{Name: "search-conn", Target: hint}}
	m.hosts = []azure.CapabilityHost{{ID: "h", Name: "default", ProvisioningState: "Succeeded", VectorStoreConnections: []string{"search-conn"}}}
	d.FoundryMgmt = m
	d.SearchData = s
	return d
}

var hint = azure.ConnectionTarget{IndexNames: []string{"knowledge"}, IndexerNames: []string{"load-knowledge"}}

func TestRun006IndexerStatesFollowTheSpecEnums(t *testing.T) {
	cases := []struct {
		name     string
		docs     int64
		status   searchprobe.IndexerStatus
		wantFind string
		wantNote string
	}{
		{"healthy", 5, searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 500}, "", ""},
		{"status error is a finding", 5, searchprobe.IndexerStatus{Status: "error", LastResultStatus: "transientFailure", HasSuccess: true, LastSuccessMinutes: 500, ItemsFailed: 3}, "in error state (itemsFailed=3)", "transient failure"},
		{"transient failure alone is a warning-class note", 5, searchprobe.IndexerStatus{Status: "running", LastResultStatus: "transientFailure", HasSuccess: true, LastSuccessMinutes: 500}, "", "transient failure"},
		{"reset", 5, searchprobe.IndexerStatus{Status: "running", LastResultStatus: "reset", HasSuccess: true, LastSuccessMinutes: 500}, "", "was reset"},
		{"zero docs after an old success is a failure", 0, searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 500}, "documentCount 0", ""},
		{"zero docs right after a success is uncertain", 0, searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 59}, "", "statistics lag"},
		{"zero docs and a success exactly at the window is a failure", 0, searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 60}, "documentCount 0", ""},
		{"a success zero minutes ago is recent, not ambiguous", 0, searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 0}, "", "statistics lag"},
		{"zero docs while in progress is uncertain", 0, searchprobe.IndexerStatus{Status: "running", LastResultStatus: "inProgress", HasSuccess: true, LastSuccessMinutes: 500}, "", "in progress"},
		{"never succeeded with zero docs", 0, searchprobe.IndexerStatus{Status: "unknown"}, "no indexer has a successful run", ""},
		{"never succeeded with documents is a note", 5, searchprobe.IndexerStatus{Status: "unknown"}, "", "no successful run in its visible"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := searchDeps(fakeSearch{index: goodIndex(), stats: searchprobe.Stats{DocumentCount: c.docs}, status: c.status}, hint)
			res, err := evalRule(t, "FND-RUN-006", d, baseInput())
			if err != nil {
				t.Fatal(err)
			}
			if c.wantFind != "" {
				if !strings.Contains(joinFindings(res.Findings), c.wantFind) {
					t.Fatalf("want a finding containing %q, got %+v", c.wantFind, res)
				}
				return
			}
			if len(res.Findings) != 0 {
				t.Fatalf("unexpected findings %+v", res.Findings)
			}
			if c.wantNote == "" {
				if res.Skipped != nil {
					t.Fatalf("want pass, got %+v", res.Skipped)
				}
				return
			}
			if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, c.wantNote) {
				t.Fatalf("skip = %+v, want uncertain containing %q", res.Skipped, c.wantNote)
			}
		})
	}
}

func TestRun006IndexSchemaAndProfiles(t *testing.T) {
	idx := goodIndex()
	idx.VectorProfiles = []string{"other"}
	d := searchDeps(fakeSearch{index: idx, stats: searchprobe.Stats{DocumentCount: 5}, status: searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 500}}, hint)
	res, err := evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil || len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, "not defined in vectorSearch.profiles") || !strings.Contains(res.Findings[0].Evidence, "p") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	d = searchDeps(fakeSearch{index: searchprobe.Index{Name: "knowledge"}, stats: searchprobe.Stats{DocumentCount: 5}}, azure.ConnectionTarget{IndexNames: []string{"knowledge"}})
	if res, err = evalRule(t, "FND-RUN-006", d, baseInput()); err != nil || len(res.Findings) != 0 || res.Skipped == nil ||
		!strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, "keyword-only index is valid") {
		t.Fatalf("a keyword-only index is uncertain, never a failure: res=%+v err=%v", res, err)
	}
}

func TestRun006InputSourceIsHonest(t *testing.T) {
	// No index hint on the Search connection: the rule says the index is unknown.
	d := searchDeps(fakeSearch{}, azure.ConnectionTarget{})
	res, err := evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "unverified optional inputs") || strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// No Search connection at all.
	d = baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	m.projectConnections = []azure.FoundryConnection{{Name: "blob", Target: azure.ConnectionTarget{ResourceID: storageID}}}
	m.hosts = []azure.CapabilityHost{{ID: "h", Name: "default", ProvisioningState: "Succeeded", StorageConnections: []string{"blob"}}}
	d.FoundryMgmt = m
	if res, err = evalRule(t, "FND-RUN-006", d, baseInput()); err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "no Search connection") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// Hostile hints never reach the client.
	d = searchDeps(fakeSearch{indexErr: errors.New("must not be called")}, azure.ConnectionTarget{IndexNames: []string{"x')/docs"}, IndexerNames: []string{"a/b"}})
	if res, err = evalRule(t, "FND-RUN-006", d, baseInput()); err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "not a valid index name") {
		t.Fatalf("hostile hint: res=%+v err=%v", res, err)
	}
	// Missing index is a typed 404.
	d = searchDeps(fakeSearch{indexErr: &rt.HTTPError{Status: 404}}, hint)
	if res, err = evalRule(t, "FND-RUN-006", d, baseInput()); err != nil || len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, "does not exist") {
		t.Fatalf("404: res=%+v err=%v", res, err)
	}
	// A string that merely contains "404" is not a not-found.
	d = searchDeps(fakeSearch{indexErr: errors.New("request id 40412 failed")}, hint)
	if res, err = evalRule(t, "FND-RUN-006", d, baseInput()); err != nil || len(res.Findings) != 0 || res.Skipped == nil {
		t.Fatalf("text match must not be a 404: res=%+v err=%v", res, err)
	}
}

func logsDeps(settings []azure.RuntimeDiagnosticSetting, traffic azure.MetricResult, logs azure.RuntimeLogsResult) Deps {
	d := baseDeps()
	d.Monitor = fakeMonitor{
		metrics: func(q azure.MetricQuery) (azure.MetricResult, error) { return traffic, nil },
		diags:   settings,
		logs:    logs,
	}
	return d
}

func TestRun007Outcomes(t *testing.T) {
	old := azure.RuntimeDiagnosticSetting{Name: "to-law", WorkspaceID: "/w", Categories: []string{"Audit"}, AgeKnown: true, AgeMinutes: 600}
	traffic := azure.MetricResult{Series: []azure.MetricSeries{mseries(5)}}
	cases := []struct {
		name     string
		settings []azure.RuntimeDiagnosticSetting
		traffic  azure.MetricResult
		logs     azure.RuntimeLogsResult
		wantFind bool
		wantSkip string
	}{
		{"logs not arriving", []azure.RuntimeDiagnosticSetting{old}, traffic, azure.RuntimeLogsResult{Counts: map[string]int64{}}, true, ""},
		{"logs arriving", []azure.RuntimeDiagnosticSetting{old}, traffic, azure.RuntimeLogsResult{Counts: map[string]int64{"Audit": 2}}, false, ""},
		{"recent setting is uncertain", []azure.RuntimeDiagnosticSetting{func() azure.RuntimeDiagnosticSetting { s := old; s.AgeMinutes = 60; return s }()}, traffic, azure.RuntimeLogsResult{}, false, "created recently"},
		{"unknown age is uncertain", []azure.RuntimeDiagnosticSetting{func() azure.RuntimeDiagnosticSetting { s := old; s.AgeKnown, s.AgeMinutes = false, 0; return s }()}, traffic, azure.RuntimeLogsResult{}, false, "age is not reported"},
		{"resource-specific tables are uncertain, never logs-missing", []azure.RuntimeDiagnosticSetting{func() azure.RuntimeDiagnosticSetting { s := old; s.LogAnalyticsDestinationType = "Dedicated"; return s }()}, traffic, azure.RuntimeLogsResult{}, false, "resource-specific tables"},
		{"allLogs group counts", []azure.RuntimeDiagnosticSetting{{Name: "g", WorkspaceID: "/w", CategoryGroups: []string{"allLogs"}, AgeKnown: true, AgeMinutes: 600}}, traffic, azure.RuntimeLogsResult{Counts: map[string]int64{"x": 1}}, false, ""},
		{"no time series is not zero traffic", []azure.RuntimeDiagnosticSetting{old}, azure.MetricResult{}, azure.RuntimeLogsResult{}, false, "no time series"},
		{"zero traffic", []azure.RuntimeDiagnosticSetting{old}, azure.MetricResult{Series: []azure.MetricSeries{mseries(0)}}, azure.RuntimeLogsResult{}, false, "no traffic"},
		{"no qualifying setting", nil, traffic, azure.RuntimeLogsResult{}, false, "no diagnostic setting"},
		{"setting without workspace", []azure.RuntimeDiagnosticSetting{{Name: "x", Categories: []string{"Audit"}}}, traffic, azure.RuntimeLogsResult{}, false, "no diagnostic setting"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := evalRule(t, "FND-RUN-007", logsDeps(c.settings, c.traffic, c.logs), baseInput())
			if err != nil {
				t.Fatal(err)
			}
			if c.wantFind != (len(res.Findings) == 1) {
				t.Fatalf("findings = %+v", res.Findings)
			}
			if c.wantSkip == "" && !c.wantFind && res.Skipped != nil {
				t.Fatalf("want pass, got %+v", res.Skipped)
			}
			if c.wantSkip != "" && (res.Skipped == nil || !strings.Contains(res.Skipped.Reason, c.wantSkip)) {
				t.Fatalf("skip = %+v, want %q", res.Skipped, c.wantSkip)
			}
		})
	}
}

func TestEngineErrorsAreNotHiddenAsUnavailable(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantPrefix string
		wantText   string
	}{
		{"allow-list refusal", fmt.Errorf("wrap: %w: GET /x", azure.ErrNotAllowed), "uncertain: internal error", "allow-list"},
		{"decode failure", fmt.Errorf("%w: /x: bad", azure.ErrDecode), "uncertain: internal error", "expected shape"},
		{"rejected input", fmt.Errorf("%w: scope", azure.ErrInvalidInput), "uncertain: internal error", "rejected"},
		{"permission", &azure.UnavailableError{Capability: "cap", Permission: "Microsoft.X/y/read"}, "input-unavailable", "Microsoft.X/y/read"},
		{"network", errors.New("transport failure"), "input-unavailable", "transport failure"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := baseDeps()
			d.FoundryMgmt = fakeMgmt{projectErr: c.err}
			res, err := evalRule(t, "FND-RUN-003", d, baseInput())
			if err != nil || res.Skipped == nil {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if !strings.HasPrefix(res.Skipped.Reason, c.wantPrefix) || !strings.Contains(res.Skipped.Reason, c.wantText) {
				t.Fatalf("reason = %q", res.Skipped.Reason)
			}
		})
	}
	// An internal error must also not look like a by-design skip to --strict.
	if got := azureReason(azure.ErrNotAllowed); strings.HasPrefix(got, "input-unavailable") {
		t.Errorf("internal error is disguised as unavailability: %q", got)
	}
}

func TestEvaluateContextHandling(t *testing.T) {
	d := baseDeps()
	d.FoundryMgmt = fakeMgmt{projectErr: context.DeadlineExceeded}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, r := range Register(d) {
		if r.ID() != "FND-RUN-003" {
			continue
		}
		if _, err := r.Evaluate(ctx, baseInput()); !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled run err = %v", err)
		}
		dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer dcancel()
		<-dctx.Done()
		res, err := r.Evaluate(dctx, baseInput())
		if err != nil || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "--timeout") {
			t.Errorf("deadline: res=%+v err=%v", res, err)
		}
	}
}

func TestRuleEvaluateNilInput(t *testing.T) {
	rules := Register(baseDeps())
	res, err := rules[0].Evaluate(context.Background(), nil)
	if err != nil || res.Skipped == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(RuleIDs()) != 8 || len(rules) != 8 {
		t.Fatal("eight runtime rules expected")
	}
}

func TestNeedTargetAndReasonHelpers(t *testing.T) {
	for _, c := range []struct {
		t    Target
		want string
	}{
		{Target{}, "--subscription"},
		{Target{SubscriptionID: "s"}, "--resource-group"},
		{Target{SubscriptionID: "s", ResourceGroup: "rg"}, "--account"},
		{Target{SubscriptionID: "s", ResourceGroup: "rg", Account: "a"}, "--project"},
	} {
		if got := needTarget(c.t, true); !strings.Contains(got, c.want) {
			t.Errorf("needTarget(%+v) = %q", c.t, got)
		}
	}
	if got := needTarget(Target{SubscriptionID: "s", ResourceGroup: "rg", Account: "a"}, false); got != "" {
		t.Errorf("project not needed: %q", got)
	}
	if msg := azureReason(&azure.UnavailableError{Capability: "x", Permission: "perm", Reason: "why"}); !strings.Contains(msg, "missing permission perm") {
		t.Errorf("azureReason = %q", msg)
	}
	if lastIDSegment("abc") != "abc" || lastIDSegment("/a/b/") != "b" {
		t.Error("lastIDSegment")
	}
	if validResourceID(searchID+"/../x") || validResourceID("") || !validResourceID(searchID) || !looksLikeExpression("[parameters('x')]") || looksLikeExpression(searchID) {
		t.Error("resource id helpers")
	}
}

func TestEnvCachesAndErrors(t *testing.T) {
	ctx := context.Background()
	e := &env{d: baseDeps()}
	for i := 0; i < 2; i++ {
		if _, err := e.projectInfo(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := e.projectConnections(ctx); err != nil || len(got) == 0 {
		t.Fatalf("projectConnections got=%+v err=%v", got, err)
	}
	for name, mk := range map[string]fakeMgmt{
		"project": {projectErr: errors.New("boom")}, "projectConns": {projectConnsErr: errors.New("boom")},
		"accountConns": {accountConnsErr: errors.New("boom")}, "deployments": {deploymentsErr: errors.New("boom")},
	} {
		e := &env{d: baseDeps()}
		e.d.FoundryMgmt = mk
		var err error
		switch name {
		case "project":
			_, err = e.projectInfo(ctx)
		case "projectConns":
			_, err = e.projectConnections(ctx)
		case "accountConns":
			_, err = e.accountConnections(ctx)
		default:
			_, err = e.deployments(ctx)
		}
		if err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	conns, names, err := (&env{d: baseDeps()}).boundConnections(ctx)
	if err != nil || len(conns) == 0 || len(names) < 3 {
		t.Fatalf("boundConnections conns=%+v names=%v err=%v", conns, names, err)
	}
	if got := e.agentRefs(&sdk.Input{AzureYAML: nil}); got != nil {
		t.Fatalf("expected nil agent names, got %v", got)
	}
}

// ---- privacy: no content sentinel may reach any rendered output ------------------

func TestRun005AndRun006PrivacyFixtures(t *testing.T) {
	foundrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dir := filepath.Join("..", "..", "runtime", "foundry", "testdata")
		switch {
		case strings.HasSuffix(r.URL.Path, "/agents") && r.URL.Query().Get("after") != "":
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "agents_page2.json")))
		case strings.HasSuffix(r.URL.Path, "/agents"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "agents.json")))
		case strings.HasSuffix(r.URL.Path, "/connections") && r.URL.Query().Get("skiptoken") != "":
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "connections_page2.json")))
		case strings.HasSuffix(r.URL.Path, "/connections"):
			fmt.Fprint(w, mustRead(t, filepath.Join(dir, "connections.json")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer foundrySrv.Close()
	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dir := filepath.Join("..", "..", "runtime", "search", "testdata")
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
	defer searchSrv.Close()

	d := baseDeps()
	d.ProjectData = foundrydp.HTTPClient{HTTP: rewriteClient(foundrySrv), Credential: staticCred{}}
	d.SearchData = searchprobe.HTTPClient{HTTP: rewriteClient(searchSrv), Credential: staticCred{}, Now: func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }}
	m := d.FoundryMgmt.(fakeMgmt)
	m.projectConnections = []azure.FoundryConnection{{ID: "conn1", Name: "search-conn", Category: "CognitiveSearch", Target: azure.ConnectionTarget{ResourceID: searchID, IndexNames: []string{"knowledge"}, IndexerNames: []string{"load-knowledge"}}}}
	m.hosts = []azure.CapabilityHost{{ID: "host1", Name: "default", ProvisioningState: "Succeeded", VectorStoreConnections: []string{"search-conn"}}}
	d.FoundryMgmt = m
	in := baseInput()
	res5, err := evalRule(t, "FND-RUN-005", d, in)
	if err != nil {
		t.Fatal(err)
	}
	res6, err := evalRule(t, "FND-RUN-006", d, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joinFindings(res5.Findings), "status is failed") {
		t.Fatalf("the spec-shaped fixture must produce the failed-status finding: %+v", res5)
	}
	if !strings.Contains(joinFindings(res6.Findings), "undefined-profile") {
		t.Fatalf("the spec-shaped fixture must produce the unresolved-profile finding: %+v", res6)
	}
	rendered := renderAll(t, append(tagFindings("FND-RUN-005", sdk.SeverityWarning, res5.Findings), tagFindings("FND-RUN-006", sdk.SeverityError, res6.Findings)...), nil)
	for _, s := range []string{"PROMPT_SENTINEL_TEXT", "DOCUMENT_SENTINEL_TEXT", "COMPLETION_SENTINEL_TEXT"} {
		if strings.Contains(rendered, s) {
			t.Fatalf("sentinel leaked in rendered outputs: %q", s)
		}
	}
}

// ---- fakes -------------------------------------------------------------------------

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
	net                azure.AccountNetwork
	netErr             error
}

func withProject(m fakeMgmt, p azure.FoundryProject) fakeMgmt { m.project = p; return m }

func (f fakeMgmt) GetAccountNetwork(context.Context, string, string, string) (azure.AccountNetwork, error) {
	return f.net, f.netErr
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
	sets      map[string]azure.RoleAssignmentSet
	assignErr error
	sql       map[string][]azure.CosmosSQLRoleAssignment
	sqlErr    error
	// only, when set, restricts the data to one principal: any other principal has nothing.
	only string
}

func (f fakeRBAC) ListRoleAssignments(_ context.Context, scope, principal string) (azure.RoleAssignmentSet, error) {
	if f.assignErr != nil {
		return azure.RoleAssignmentSet{}, f.assignErr
	}
	if f.only != "" && f.only != principal {
		return azure.RoleAssignmentSet{}, nil
	}
	return f.sets[scope], nil
}
func (f fakeRBAC) ListCosmosSQLRoleAssignments(_ context.Context, accountID, principal string) ([]azure.CosmosSQLRoleAssignment, error) {
	if f.sqlErr != nil {
		return nil, f.sqlErr
	}
	if f.only != "" && f.only != principal {
		return nil, nil
	}
	return append([]azure.CosmosSQLRoleAssignment(nil), f.sql[accountID]...), nil
}

type fakeMonitor struct {
	metrics func(azure.MetricQuery) (azure.MetricResult, error)
	diags   []azure.RuntimeDiagnosticSetting
	diagErr error
	logs    azure.RuntimeLogsResult
	logErr  error
}

func (f fakeMonitor) QueryAccountMetrics(_ context.Context, _ string, q azure.MetricQuery) (azure.MetricResult, error) {
	if f.metrics == nil {
		return azure.MetricResult{}, nil
	}
	return f.metrics(q)
}
func (f fakeMonitor) ListDiagnosticSettings(context.Context, string) ([]azure.RuntimeDiagnosticSetting, error) {
	return f.diags, f.diagErr
}
func (f fakeMonitor) QueryDiagnosticCounts(context.Context, string, string, int) (azure.RuntimeLogsResult, error) {
	return f.logs, f.logErr
}

type fakeProjectReader struct {
	pingErr     error
	agents      foundrydp.AgentList
	agentsErr   error
	connections foundrydp.ConnectionList
	connectErr  error
}

func (f fakeProjectReader) Ping(context.Context, string) error { return f.pingErr }
func (f fakeProjectReader) ListAgents(context.Context, string) (foundrydp.AgentList, error) {
	return f.agents, f.agentsErr
}
func (f fakeProjectReader) ListConnections(context.Context, string) (foundrydp.ConnectionList, error) {
	return f.connections, f.connectErr
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

type fakeResolver struct {
	answers map[string][]string
	errs    map[string]error
	err     error
}

func (f fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if e, ok := f.errs[host]; ok {
		return nil, e
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
			net:     azure.AccountNetwork{CustomSubDomainName: "acct"},
			project: project(),
			hosts: []azure.CapabilityHost{{
				ID: "host1", Name: "default", ProvisioningState: "Succeeded",
				StorageConnections: []string{"storage-conn"}, ThreadStorageConnections: []string{"cosmos-conn"}, VectorStoreConnections: []string{"search-conn"},
			}},
			projectConnections: []azure.FoundryConnection{
				{Name: "search-conn", Category: "CognitiveSearch", Target: azure.ConnectionTarget{ResourceID: searchID, IndexNames: []string{"knowledge"}, IndexerNames: []string{"load-knowledge"}}},
				{Name: "storage-conn", Category: "AzureStorageAccount", Target: azure.ConnectionTarget{ResourceID: storageID}},
				{Name: "cosmos-conn", Category: "CosmosDB", Target: azure.ConnectionTarget{ResourceID: cosmosID}},
			},
		},
		RBAC:    fakeRBAC{},
		Monitor: fakeMonitor{},
		ProjectData: fakeProjectReader{
			agents:      foundrydp.AgentList{Agents: []foundrydp.Agent{{Name: "support-agent", LatestVersion: "1", LatestStatus: "active"}}},
			connections: foundrydp.ConnectionList{Connections: []foundrydp.Connection{{Name: "search-conn", Type: "CognitiveSearch"}, {Name: "storage-conn"}, {Name: "cosmos-conn"}}},
		},
		SearchData: fakeSearch{
			index:  goodIndex(),
			stats:  searchprobe.Stats{DocumentCount: 1},
			status: searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 500},
		},
		Resolver: fakeResolver{},
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
