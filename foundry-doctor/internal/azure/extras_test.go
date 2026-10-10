package azure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const (
	testSubnet = testRG + "/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/agents"
)

func TestRegionAvailability(t *testing.T) {
	ctx := context.Background()
	prov := testScope + "/providers/Microsoft.CognitiveServices"
	a, _ := newAdapter(t, newFake(t).onFile("GET", testScope+"/locations", "locations.json").onFile("GET", prov, "provider_types.json"))
	locs, err := a.ListLocations(ctx, testSub)
	if err != nil || len(locs) != 2 || locs[0].Name != "eastus2" || locs[1].DisplayName != "Sweden Central" {
		t.Fatalf("locs = %+v, %v", locs, err)
	}
	rts, err := a.ProviderResourceTypes(ctx, testSub, "Microsoft.CognitiveServices")
	if err != nil || len(rts) != 2 || rts[0].ResourceType != "accounts" || len(rts[0].Locations) != 2 {
		t.Fatalf("rts = %+v, %v", rts, err)
	}
	// display-name form from the provider matches the name form of the subscription location.
	if !LocationOffered(rts[0].Locations, locs[1]) || !LocationOffered([]string{"swedencentral"}, locs[1]) {
		t.Error("normalisation failed to match name/display name")
	}
	if LocationOffered(rts[0].Locations, LocationInfo{Name: "westeurope", DisplayName: "West Europe"}) {
		t.Error("unexpected match")
	}
	if LocationOffered(nil, locs[0]) {
		t.Error("empty offered list must not match")
	}
	t.Run("missing input", func(t *testing.T) {
		if _, err := a.ListLocations(ctx, "bad"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
		if _, err := a.ProviderResourceTypes(ctx, testSub, "a/b"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("restricted", func(t *testing.T) {
		r, _ := newAdapter(t, newFake(t).on403("GET", testScope+"/locations"))
		_, err := r.ListLocations(ctx, testSub)
		if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.Resources/subscriptions/locations/read" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestFoundryRegionSupport(t *testing.T) {
	if FoundryFeatureDataDate == "" || !strings.HasPrefix(FoundryFeatureDataSource, "https://learn.microsoft.com/") {
		t.Fatal("matrix must carry its source and date")
	}
	if n := len(FoundryRegions()); n != 30 {
		t.Errorf("documented regions = %d, want 30", n)
	}
	pl, ok := FoundryRegionSupport("Poland Central")
	if !ok || pl.VoiceAgents || pl.ToolSupported("Computer Use") || !pl.ToolSupported("file search") {
		t.Errorf("poland = %+v", pl)
	}
	it, _ := FoundryRegionSupport("italynorth")
	if it.ToolSupported("File Search") || !it.ToolSupported("Function") {
		t.Errorf("italy = %+v", it)
	}
	if sc, _ := FoundryRegionSupport("sweden central"); len(sc.UnsupportedTools) != 0 || !sc.VoiceAgents {
		t.Errorf("sweden = %+v", sc)
	}
	if _, ok := FoundryRegionSupport("mars"); ok {
		t.Error("unknown region must report not documented")
	}
	if !BingGroundingDocumented("West US 2") || BingGroundingDocumented("japanwest") {
		t.Error("bing grounding list wrong")
	}
	for _, r := range FoundryRegions() {
		f, _ := FoundryRegionSupport(r)
		for _, u := range f.UnsupportedTools {
			found := false
			for _, tl := range FoundryTools {
				found = found || tl == u
			}
			if !found {
				t.Errorf("%s: unknown tool %q", r, u)
			}
		}
	}
}

func TestCountDeployments(t *testing.T) {
	ctx := context.Background()
	path := testRG + "/providers/Microsoft.Resources/deployments"
	mk := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"name":"d%d"}`, i)
		}
		return `{"value":[` + strings.Join(parts, ",") + `]}`
	}
	t.Run("small", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).onFile("GET", path, "deployments.json"))
		got, err := a.CountDeployments(ctx, testSub, "rg-demo")
		if err != nil || got != (DeploymentCount{Count: 3}) {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("over limit is truncated", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on("GET", path, reply{status: 200, body: mk(DeploymentHistoryLimit + 50)}))
		got, err := a.CountDeployments(ctx, testSub, "rg-demo")
		if err != nil || !got.Truncated || got.Count != DeploymentHistoryLimit+1 {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("exactly at limit", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on("GET", path, reply{status: 200, body: mk(DeploymentHistoryLimit)}))
		got, err := a.CountDeployments(ctx, testSub, "rg-demo")
		if err != nil || got.Truncated || got.Count != DeploymentHistoryLimit {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("missing input and restricted", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on403("GET", path))
		if _, err := a.CountDeployments(ctx, testSub, "a/b"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
		_, err := a.CountDeployments(ctx, testSub, "rg-demo")
		if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.Resources/deployments/read" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestSubnetLinks(t *testing.T) {
	ctx := context.Background()
	f := newFake(t).onFile("GET", testSubnet+"/ServiceAssociationLinks", "servicelinks.json").onFile("GET", testSubnet+"/ResourceNavigationLinks", "navlinks.json")
	a, _ := newAdapter(t, f)
	got, err := a.SubnetLinks(ctx, testSubnet)
	if err != nil || len(got.ServiceAssociationLinks) != 2 || got.ServiceAssociationLinks[0].Name != "a-link" ||
		got.ServiceAssociationLinks[1].LinkedResourceType != "Microsoft.App/environments" || len(got.ResourceNavigationLinks) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := a.SubnetLinks(ctx, testRG); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("non-subnet id err = %v", err)
	}
	r, _ := newAdapter(t, newFake(t).on403("GET", testSubnet+"/ServiceAssociationLinks"))
	if _, err := r.SubnetLinks(ctx, testSubnet); !errors.Is(err, ErrUnavailable) {
		t.Errorf("restricted err = %v", err)
	}
}

func TestPermissionEvidence(t *testing.T) {
	ctx := context.Background()
	path := testRG + "/providers/Microsoft.Authorization/permissions"
	reg := RegisterAction("Microsoft.CognitiveServices")
	if reg != "Microsoft.CognitiveServices/register/action" {
		t.Fatalf("RegisterAction = %s", reg)
	}
	req := PermissionRequest{Scope: testRG, Actions: []string{reg, "Microsoft.Authorization/roleAssignments/write", "Microsoft.Resources/deployments/read"}}
	t.Run("contributor", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).onFile("GET", path, "permissions_contributor.json"))
		got, err := a.CheckReportedActions(ctx, req)
		if err != nil || len(got) != 3 {
			t.Fatalf("got %+v, %v", got, err)
		}
		if got[0].Decision != DecisionAllowed || got[0].Reason != ReasonReportedNotProof {
			t.Errorf("register: %+v", got[0])
		}
		if got[1].Decision != DecisionDenied || got[1].Reason != ReasonNoGrantingRole {
			t.Errorf("role assignment write must be excluded by notActions: %+v", got[1])
		}
		if got[2].Decision != DecisionAllowed {
			t.Errorf("read: %+v", got[2])
		}
	})
	t.Run("reader", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).onFile("GET", path, "permissions_reader.json"))
		got, err := a.CheckReportedActions(ctx, req)
		if err != nil || got[0].Decision != DecisionDenied || got[2].Decision != DecisionAllowed {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("subscription scope unavailable", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t))
		_, err := a.ReportedPermissions(ctx, testScope)
		u, ok := AsUnavailable(err)
		if !ok || !strings.Contains(u.Reason, "no subscription-scope variant") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("missing input and none", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t))
		if _, err := a.ReportedPermissions(ctx, ""); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
		if got, err := a.CheckReportedActions(ctx, PermissionRequest{Scope: testRG}); err != nil || len(got) != 0 {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("restricted", func(t *testing.T) {
		a, _ := newAdapter(t, newFake(t).on403("GET", path))
		_, err := a.CheckReportedActions(ctx, req)
		if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.Authorization/permissions/read" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestClientWiringExtras(t *testing.T) {
	a, _ := newAdapter(t, newFake(t))
	c := a.Client()
	if c.Regions == nil || c.Deployments == nil || c.SubnetLinks == nil || c.Evidence == nil {
		t.Errorf("additive Client fields not wired: %+v", c)
	}
}
