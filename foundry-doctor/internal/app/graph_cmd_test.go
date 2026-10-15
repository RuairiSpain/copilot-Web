package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	graphview "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

type fakeInventory struct {
	list azure.ResourceList
	err  error
}

func (f fakeInventory) ListResources(context.Context, azure.InventoryQuery) (azure.ResourceList, error) {
	return f.list, f.err
}
func (fakeInventory) ResourceGroup(context.Context, string, string) (azure.ResourceGroupInfo, error) {
	return azure.ResourceGroupInfo{}, nil
}
func (fakeInventory) ListLocks(context.Context, string) ([]azure.Lock, error) { return nil, nil }
func (fakeInventory) GetSubnet(context.Context, string) (azure.Subnet, error) {
	return azure.Subnet{}, nil
}

type fakeFoundry struct {
	projectHostsCalled bool
	accountHostsCalled bool
	projectConnsCalled bool
	accountConnsCalled bool
	deploymentsCalled  bool
	projectErr         error
	projectHostsErr    error
	projectConnsErr    error
	accountHostsErr    error
	accountConnsErr    error
	deploymentsErr     error
}

func (f *fakeFoundry) GetProject(context.Context, string, string, string, string) (azure.FoundryProject, error) {
	if f.projectErr != nil {
		return azure.FoundryProject{}, f.projectErr
	}
	return azure.FoundryProject{Name: "proj", ProvisioningState: "Succeeded"}, nil
}
func (f *fakeFoundry) ListProjectCapabilityHosts(context.Context, string, string, string, string) ([]azure.CapabilityHost, error) {
	f.projectHostsCalled = true
	return []azure.CapabilityHost{{Name: "host1", ProvisioningState: "Failed", AIServiceConnections: []string{"conn1"}}}, f.projectHostsErr
}
func (f *fakeFoundry) ListAccountCapabilityHosts(context.Context, string, string, string) ([]azure.CapabilityHost, error) {
	f.accountHostsCalled = true
	return []azure.CapabilityHost{{Name: "host1", ProvisioningState: "Failed", AIServiceConnections: []string{"conn1"}}}, f.accountHostsErr
}
func (f *fakeFoundry) ListProjectConnections(context.Context, string, string, string, string) ([]azure.FoundryConnection, error) {
	f.projectConnsCalled = true
	return []azure.FoundryConnection{{Name: "conn1", Error: "failed"}}, f.projectConnsErr
}
func (f *fakeFoundry) ListAccountConnections(context.Context, string, string, string) ([]azure.FoundryConnection, error) {
	f.accountConnsCalled = true
	return []azure.FoundryConnection{{Name: "conn1", Error: "failed"}}, f.accountConnsErr
}
func (f *fakeFoundry) ListAccountDeployments(context.Context, string, string, string) ([]azure.AccountDeployment, error) {
	f.deploymentsCalled = true
	return []azure.AccountDeployment{{Name: "dep1", ProvisioningState: "Failed"}}, f.deploymentsErr
}

func TestGraphSourceOffline(t *testing.T) {
	svc, _ := newSvc(&fakeEngine{})
	var out bytes.Buffer
	code, err := Graph(context.Background(), svc, GraphRequest{Format: "json"}, &out)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if out.Len() == 0 {
		t.Fatal("expected graph output")
	}
}

func TestGraphCombinedUsesAzureReadOnlyInventory(t *testing.T) {
	old := graphClient
	ff := &fakeFoundry{}
	graphClient = func() (azure.Client, error) {
		return azure.Client{
			Inventory: fakeInventory{list: azure.ResourceList{Resources: []azure.Resource{
				{ID: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts", Properties: map[string]string{"properties.provisioningState": "Succeeded"}},
				{ID: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/st", Name: "st", Type: "Microsoft.Storage/storageAccounts", Properties: map[string]string{"properties.provisioningState": "Failed"}},
			}}},
			Foundry: ff,
		}, nil
	}
	defer func() { graphClient = old }()

	svc, _ := newSvc(&fakeEngine{})
	code, err := Graph(context.Background(), svc, GraphRequest{
		Mode:   graphview.ModeCombined,
		Format: "markdown",
		Target: GraphTarget{SubscriptionID: "sub", ResourceGroup: "rg", Account: "acct", Project: "proj"},
	}, io.Discard)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !ff.projectConnsCalled || !ff.projectHostsCalled || !ff.deploymentsCalled {
		t.Fatalf("expected project-scoped runtime overlay, got %+v", ff)
	}
	if !ff.accountConnsCalled || ff.accountHostsCalled {
		t.Fatalf("expected merged project+account connections and project hosts, got %+v", ff)
	}
}

func TestGraphDeployedSkipsLocalSourceLoading(t *testing.T) {
	old := graphClient
	graphClient = func() (azure.Client, error) {
		return azure.Client{
			Inventory: fakeInventory{list: azure.ResourceList{Resources: []azure.Resource{
				{ID: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/st", Name: "st", Type: "Microsoft.Storage/storageAccounts"},
			}}},
			Foundry: &fakeFoundry{},
		}, nil
	}
	defer func() { graphClient = old }()

	svc, _ := newSvc(&fakeEngine{})
	svc.Project = fakeProject{err: errors.New("should not load source")}
	code, err := Graph(context.Background(), svc, GraphRequest{
		Mode:   graphview.ModeDeployed,
		Format: "json",
		Target: GraphTarget{SubscriptionID: "sub"},
	}, io.Discard)
	if err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestGraphCombinedReturnsInternalOnGenericProjectOverlayFailure(t *testing.T) {
	old := graphClient
	ff := &fakeFoundry{projectConnsErr: errors.New("boom")}
	graphClient = func() (azure.Client, error) {
		return azure.Client{
			Inventory: fakeInventory{list: azure.ResourceList{Resources: []azure.Resource{
				{ID: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts", Properties: map[string]string{"properties.provisioningState": "Succeeded"}},
			}}},
			Foundry: ff,
		}, nil
	}
	defer func() { graphClient = old }()

	svc, _ := newSvc(&fakeEngine{})
	code, err := Graph(context.Background(), svc, GraphRequest{
		Mode:   graphview.ModeCombined,
		Format: "json",
		Target: GraphTarget{SubscriptionID: "sub", ResourceGroup: "rg", Account: "acct", Project: "proj"},
	}, io.Discard)
	if err == nil || code != ExitInternal {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !ff.projectConnsCalled {
		t.Fatalf("expected project overlay call, got %+v", ff)
	}
}

func TestGraphFindingsReportReadFailureIsUnavailable(t *testing.T) {
	svc, _ := newSvc(&fakeEngine{})
	code, err := Graph(context.Background(), svc, GraphRequest{
		Format:         "json",
		FindingsReport: []string{"missing-report.json"},
	}, io.Discard)
	if err == nil || code != ExitUnavailable {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestGraphValidateProjectFlags(t *testing.T) {
	cases := []GraphRequest{
		{Mode: graphview.ModeCombined, Format: "json", Target: GraphTarget{SubscriptionID: "sub", Project: "proj"}},
		{Mode: graphview.ModeCombined, Format: "json", Target: GraphTarget{SubscriptionID: "sub", Account: "acct"}},
		{Mode: graphview.ModeCombined, Format: "json", Target: GraphTarget{SubscriptionID: "sub", ResourceGroup: "bad/rg", Account: "acct"}},
	}
	for _, tc := range cases {
		if err := tc.Validate(); err == nil {
			t.Fatalf("expected validation error for %+v", tc.Target)
		}
	}
}

func TestGraphCombinedReturnsUnavailableOnTruncatedInventory(t *testing.T) {
	old := graphClient
	graphClient = func() (azure.Client, error) {
		return azure.Client{
			Inventory: fakeInventory{list: azure.ResourceList{
				Resources: []azure.Resource{{ID: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts"}},
				Truncated: true,
			}},
			Foundry: &fakeFoundry{},
		}, nil
	}
	defer func() { graphClient = old }()

	svc, _ := newSvc(&fakeEngine{})
	code, err := Graph(context.Background(), svc, GraphRequest{
		Mode:   graphview.ModeCombined,
		Format: "json",
		Target: GraphTarget{SubscriptionID: "sub", ResourceGroup: "rg", Account: "acct"},
	}, io.Discard)
	if err == nil || code != ExitUnavailable {
		t.Fatalf("code=%d err=%v", code, err)
	}
}
