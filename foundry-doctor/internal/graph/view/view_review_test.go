package view

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	testScope = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg"
	testProv  = testScope + "/providers"
)

func nodeByLabel(t *testing.T, g Graph, label string) Node {
	t.Helper()
	var found []Node
	for _, n := range g.Nodes {
		if n.Label == label {
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one node %q, got %d: %+v", label, len(found), g.Nodes)
	}
	return found[0]
}

func hasEdge(g Graph, from, to, kind string) (Edge, bool) {
	for _, e := range g.Edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return e, true
		}
	}
	return Edge{}, false
}

// A resource declared in source and deployed keeps the deployed ARM ID, so a
// runtime connection target and a live finding that carry that ID resolve to
// the correlated node (not an external placeholder, not unmatched).
func TestCombinedCorrelatedNodeKeepsARMID(t *testing.T) {
	storeID := testProv + "/Microsoft.Storage/storageAccounts/store"
	g, err := Build(Input{
		Source: SourceInput{Resources: []coregraph.Resource{{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "store"}}},
		Inventory: []azure.Resource{
			{ID: storeID, Name: "store", Type: "Microsoft.Storage/storageAccounts", Properties: map[string]string{"properties.provisioningState": "Succeeded"}},
		},
		Runtime: RuntimeInput{Connections: []azure.FoundryConnection{{
			ID: testProv + "/Microsoft.CognitiveServices/accounts/acct/projects/proj/connections/c1", Name: "c1",
			Target: azure.ConnectionTarget{ResourceID: storeID, Endpoint: "https://store.blob.core.windows.net/"},
		}}},
		Findings: []sdk.Finding{{RuleID: "FND-LIVE", Severity: sdk.SeverityError, Resource: sdk.ResourceRef{ID: storeID}}},
	}, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	store := nodeByLabel(t, g, "store")
	if store.Origin != OriginBoth || store.Resource != storeID {
		t.Fatalf("correlated node must be both with the ARM ID: %+v", store)
	}
	if store.Findings != 1 || !store.Unhealthy || g.Summary.UnmatchedFindings != 0 {
		t.Fatalf("live finding not matched by ARM ID: %+v %+v", store, g.Summary)
	}
	conn := nodeByLabel(t, g, "c1")
	e, ok := hasEdge(g, conn.ID, store.ID, "targets")
	if !ok || e.External {
		t.Fatalf("connection must target the correlated node: %+v", g.Edges)
	}
	for _, n := range g.Nodes {
		if n.External {
			t.Fatalf("in-scope target drawn as external: %+v", g.Nodes)
		}
	}
}

// Template pointers never appear as resource IDs.
func TestSourceNodesHaveNoTemplatePointerResource(t *testing.T) {
	g, err := Build(Input{Source: SourceInput{Resources: []coregraph.Resource{{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "store"}}}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	if n := nodeByLabel(t, g, "store"); n.Resource != "" {
		t.Fatalf("resource must be an ARM ID or empty, got %q", n.Resource)
	}
}

// An account declared in source and deployed is one node that carries the
// runtime edges.
func TestCombinedAccountDeclaredInSourceIsNotDuplicated(t *testing.T) {
	acctID := testProv + "/Microsoft.CognitiveServices/accounts/acct"
	acct := azure.Resource{ID: acctID, Name: "acct", Type: "Microsoft.CognitiveServices/accounts"}
	g, err := Build(Input{
		Source:    SourceInput{Resources: []coregraph.Resource{{ID: "/resources/0", Type: "Microsoft.CognitiveServices/accounts", Name: "acct"}}},
		Inventory: []azure.Resource{acct},
		Runtime: RuntimeInput{
			Account:     &acct,
			Project:     &azure.FoundryProject{ID: acctID + "/projects/proj", Name: "proj", ProvisioningState: "Succeeded"},
			Deployments: []azure.AccountDeployment{{ID: acctID + "/deployments/dep", Name: "dep", ProvisioningState: "Succeeded"}},
			Connections: []azure.FoundryConnection{{ID: acctID + "/connections/shared", Name: "shared"}},
		},
	}, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	var accounts []Node
	for _, n := range g.Nodes {
		if n.Kind == "foundry-account" {
			accounts = append(accounts, n)
		}
	}
	if len(accounts) != 1 || accounts[0].Origin != OriginBoth || accounts[0].Resource != acctID {
		t.Fatalf("want one both account node with the ARM ID, got %+v", accounts)
	}
	a := accounts[0].ID
	for _, want := range []struct{ to, kind string }{
		{nodeByLabel(t, g, "proj").ID, "contains"},
		{nodeByLabel(t, g, "dep").ID, "deploys"},
		{nodeByLabel(t, g, "shared").ID, "uses"},
	} {
		if _, ok := hasEdge(g, a, want.to, want.kind); !ok {
			t.Errorf("missing %s edge from the account: %+v", want.kind, g.Edges)
		}
	}
}

// Types the generic resource list cannot observe are unobserved, not local.
func TestCombinedUnobservedTypes(t *testing.T) {
	in := Input{
		Source: SourceInput{
			Document: doc(t, "name: s\nservices:\n  agent:\n    host: azure.ai.agent\n"),
			Services: []coregraph.Service{{Name: "agent", Host: "azure.ai.agent"}},
			Resources: []coregraph.Resource{
				{ID: "/resources/0", Type: "Microsoft.Authorization/roleAssignments", Name: "ra"},
				{ID: "/resources/1", Type: "Microsoft.Network/virtualNetworks/subnets", Name: "vnet/snet"},
				{ID: "/resources/2", Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/dep"},
				{ID: "/resources/3", Type: "Microsoft.Storage/storageAccounts", Name: "notdeployed"},
				{ID: "/resources/4", Type: "Microsoft.Storage/storageAccounts", Name: "deployed"},
				{ID: "/resources/5", Type: "Microsoft.Sql/servers/databases", Name: "sql/db"},
			},
		},
		Inventory: []azure.Resource{
			{ID: testProv + "/Microsoft.Storage/storageAccounts/deployed", Name: "deployed", Type: "Microsoft.Storage/storageAccounts"},
			{ID: testProv + "/Microsoft.Sql/servers/sql/databases/other", Name: "sql/other", Type: "Microsoft.Sql/servers/databases"},
		},
	}
	g, err := Build(in, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	for label, want := range map[string]Origin{
		"ra": OriginUnobserved, "vnet/snet": OriginUnobserved, "acct/dep": OriginUnobserved, "agent": OriginUnobserved,
		"notdeployed": OriginLocal, "deployed": OriginBoth, "sql/db": OriginLocal,
	} {
		if got := nodeByLabel(t, g, label).Origin; got != want {
			t.Errorf("%s: origin %s, want %s", label, got, want)
		}
	}
	in.Runtime.ObservedTypes = []string{"Microsoft.CognitiveServices/accounts/deployments"}
	g, err = Build(in, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	if got := nodeByLabel(t, g, "acct/dep").Origin; got != OriginLocal {
		t.Errorf("observed runtime type must stay local, got %s", got)
	}
	src, err := Build(in, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range src.Nodes {
		if n.Origin == OriginUnobserved {
			t.Fatalf("source mode never marks unobserved: %+v", n)
		}
	}
}

func TestIdentityEdgesFromInventory(t *testing.T) {
	uaID := testProv + "/Microsoft.ManagedIdentity/userAssignedIdentities/id-app"
	absentUA := testProv + "/Microsoft.ManagedIdentity/userAssignedIdentities/id-gone"
	otherUA := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shared/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-shared"
	g, err := Build(Input{Inventory: []azure.Resource{
		{ID: testProv + "/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts", UserAssignedIdentities: []string{uaID}},
		{ID: uaID, Name: "id-app", Type: "Microsoft.ManagedIdentity/userAssignedIdentities"},
		{ID: testProv + "/Microsoft.Storage/storageAccounts/st", Name: "st", Type: "Microsoft.Storage/storageAccounts", UserAssignedIdentities: []string{absentUA, otherUA}},
	}}, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := hasEdge(g, nodeByLabel(t, g, "acct").ID, nodeByLabel(t, g, "id-app").ID, "usesIdentity"); !ok || e.Missing || e.External {
		t.Fatalf("missing account identity edge: %+v", g.Edges)
	}
	st := nodeByLabel(t, g, "st").ID
	var missing, external bool
	for _, e := range g.Edges {
		if e.From == st && e.Kind == "usesIdentity" {
			missing = missing || e.Missing
			external = external || e.External
		}
	}
	if !missing || !external {
		t.Fatalf("want an in-scope missing and an out-of-scope external identity edge: %+v %+v", g.Nodes, g.Edges)
	}
}

// Project and account connections with the same name are distinct nodes;
// capability hosts resolve the project connection first.
func TestConnectionScopesAndSameName(t *testing.T) {
	acctID := testProv + "/Microsoft.CognitiveServices/accounts/acct"
	acct := azure.Resource{ID: acctID, Name: "acct", Type: "Microsoft.CognitiveServices/accounts"}
	g, err := Build(Input{
		Inventory: []azure.Resource{acct},
		Runtime: RuntimeInput{
			Account: &acct,
			Project: &azure.FoundryProject{ID: acctID + "/projects/proj", Name: "proj"},
			Connections: []azure.FoundryConnection{
				{ID: acctID + "/connections/x", Name: "x", Type: "Microsoft.CognitiveServices/accounts/connections"},
				{ID: acctID + "/projects/proj/connections/x", Name: "x", Type: "Microsoft.CognitiveServices/accounts/projects/connections"},
			},
			Hosts: []azure.CapabilityHost{{ID: acctID + "/projects/proj/capabilityHosts/h", Name: "h", AIServiceConnections: []string{"x"}}},
		},
	}, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	var conns []Node
	for _, n := range g.Nodes {
		if n.Kind == "connection" {
			conns = append(conns, n)
		}
	}
	if len(conns) != 2 {
		t.Fatalf("want two connection nodes, got %+v", conns)
	}
	var projConn, acctConn string
	for _, c := range conns {
		if c.Resource == acctID+"/projects/proj/connections/x" {
			projConn = c.ID
		} else {
			acctConn = c.ID
		}
	}
	account, project, host := nodeByLabel(t, g, "acct").ID, nodeByLabel(t, g, "proj").ID, nodeByLabel(t, g, "h").ID
	if _, ok := hasEdge(g, project, projConn, "uses"); !ok {
		t.Errorf("project connection must hang off the project: %+v", g.Edges)
	}
	if _, ok := hasEdge(g, account, acctConn, "uses"); !ok {
		t.Errorf("account connection must hang off the account: %+v", g.Edges)
	}
	if _, ok := hasEdge(g, host, projConn, "uses"); !ok {
		t.Errorf("capability host must use the project connection: %+v", g.Edges)
	}
	if _, ok := hasEdge(g, host, acctConn, "uses"); ok {
		t.Errorf("capability host must not use the same-name account connection: %+v", g.Edges)
	}
}

func TestInScopeUnresolvedReferenceIsMissingNotExternal(t *testing.T) {
	inScope := testProv + "/Microsoft.Storage/storageAccounts/gone"
	outScope := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/other/providers/Microsoft.Storage/storageAccounts/shared"
	d := doc(t, "name: s\nservices:\n  a:\n    host: azure.ai.agent\n    uses: ["+inScope+", "+outScope+"]\n")
	in := Input{Source: SourceInput{Document: d, Services: []coregraph.Service{{Name: "a", Host: "azure.ai.agent", Uses: []string{inScope, outScope}}}}}
	g, err := Build(in, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	var missing, external int
	for _, n := range g.Nodes {
		if n.Missing {
			missing++
		}
		if n.External {
			external++
		}
	}
	if missing != 1 || external != 1 {
		t.Fatalf("want one missing (in scope) and one external node, got %d/%d: %+v", missing, external, g.Nodes)
	}
}

func TestFindingARMIDFallsBackToTypeAndName(t *testing.T) {
	g, err := Build(Input{
		Source:   SourceInput{Resources: []coregraph.Resource{{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "store"}}},
		Findings: []sdk.Finding{{RuleID: "FND-X", Severity: sdk.SeverityWarning, Resource: sdk.ResourceRef{ID: testProv + "/Microsoft.Storage/storageAccounts/store"}}},
	}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	if n := nodeByLabel(t, g, "store"); n.Findings != 1 || g.Summary.UnmatchedFindings != 0 {
		t.Fatalf("finding not matched: %+v %+v", n, g.Summary)
	}
}

func TestDisconnectedNodesAreKept(t *testing.T) {
	g, err := Build(Input{Source: SourceInput{Resources: []coregraph.Resource{
		{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "a"},
		{ID: "/resources/1", Type: "Microsoft.Search/searchServices", Name: "b"},
		{ID: "/resources/2", Type: "Microsoft.Insights/components", Name: "c"},
	}}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 3 || len(g.Edges) != 0 || g.Summary.NodeCount != 3 || g.Summary.EdgeCount != 0 {
		t.Fatalf("disconnected nodes must all be kept without edges: %+v", g)
	}
}

func TestCycleIsProjected(t *testing.T) {
	g, err := Build(Input{Source: SourceInput{Resources: []coregraph.Resource{
		{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "a", SymbolicName: "a", DependsOn: []string{"b"}},
		{ID: "/resources/1", Type: "Microsoft.Storage/storageAccounts", Name: "b", SymbolicName: "b", DependsOn: []string{"a"}},
	}}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	a, b := nodeByLabel(t, g, "a").ID, nodeByLabel(t, g, "b").ID
	if _, ok := hasEdge(g, a, b, "dependsOn"); !ok {
		t.Fatalf("missing a->b: %+v", g.Edges)
	}
	if _, ok := hasEdge(g, b, a, "dependsOn"); !ok {
		t.Fatalf("missing b->a: %+v", g.Edges)
	}
}

func TestCollapseKeepsFoundryNodesFirst(t *testing.T) {
	var res []coregraph.Resource
	for i := range 5 {
		res = append(res, coregraph.Resource{ID: fmt.Sprintf("/resources/%d", i), Type: "Microsoft.Storage/storageAccounts", Name: fmt.Sprintf("st%d", i)})
	}
	g, err := Build(Input{Source: SourceInput{
		Document:  doc(t, "name: s\nservices:\n  proj:\n    host: azure.ai.project\n  agent:\n    host: azure.ai.agent\n"),
		Services:  []coregraph.Service{{Name: "proj", Host: "azure.ai.project"}, {Name: "agent", Host: "azure.ai.agent"}},
		Resources: res,
	}}, Options{Mode: ModeSource, CollapseAfter: 2})
	if err != nil {
		t.Fatal(err)
	}
	if nodeByLabel(t, g, "proj").Collapsed || nodeByLabel(t, g, "agent").Collapsed {
		t.Fatalf("Foundry nodes must be kept: %+v", g.Nodes)
	}
	if len(g.Nodes) != 3 || g.Summary.CollapsedNodes != 5 {
		t.Fatalf("want proj, agent and one storage bucket: %+v %+v", g.Nodes, g.Summary)
	}
}

func largeInput(n int) Input {
	res := make([]coregraph.Resource, 0, n)
	var inv []azure.Resource
	for i := range n {
		name := fmt.Sprintf("st%05d", i)
		r := coregraph.Resource{ID: fmt.Sprintf("/resources/%d", i), Type: "Microsoft.Storage/storageAccounts", Name: name, SymbolicName: name}
		if i > 0 {
			r.DependsOn = []string{fmt.Sprintf("st%05d", i-1)}
		}
		res = append(res, r)
		if i%2 == 0 {
			inv = append(inv, azure.Resource{ID: testProv + "/Microsoft.Storage/storageAccounts/" + name, Name: name, Type: "Microsoft.Storage/storageAccounts"})
		}
	}
	return Input{Source: SourceInput{Resources: res}, Inventory: inv}
}

// TestLargeGraph is the PRD large-graph performance test: 3000 template
// resources in a dependency chain and 1500 deployed resources build and
// collapse deterministically within a generous bound.
func TestLargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("large-graph performance test skipped in -short mode")
	}
	const n = 3000
	in := largeInput(n)
	start := time.Now()
	g, err := Build(in, Options{Mode: ModeCombined, Scope: testScope, CollapseAfter: 80})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("building a %d-node graph took %s", n, elapsed)
	}
	if len(g.Nodes) != 82 || g.Summary.CollapsedNodes != n-80 {
		t.Fatalf("want 80 kept nodes plus a both and a local bucket, got %d nodes, summary %+v", len(g.Nodes), g.Summary)
	}
	again, err := Build(largeInput(n), Options{Mode: ModeCombined, Scope: testScope, CollapseAfter: 80})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, again) {
		t.Fatal("large graph output is not deterministic")
	}
}

func BenchmarkBuildLargeGraph(b *testing.B) {
	in := largeInput(3000)
	for b.Loop() {
		if _, err := Build(in, Options{Mode: ModeCombined, Scope: testScope, CollapseAfter: 80}); err != nil {
			b.Fatal(err)
		}
	}
}

// An in-scope ARM ID of a type the generic resource list cannot observe
// (child or extension resource) is unobserved, not a broken reference; an
// in-scope ID of an observable top-level type the inventory did not return is
// missing.
func TestCombinedInScopeChildAndExtensionRefsAreUnobserved(t *testing.T) {
	subnet := testProv + "/Microsoft.Network/virtualNetworks/vnet/subnets/snet"
	role := testProv + "/Microsoft.Authorization/roleAssignments/ra1"
	storage := testProv + "/Microsoft.Storage/storageAccounts/gone"
	uses := []string{subnet, role, storage}
	d := doc(t, "name: s\nservices:\n  a:\n    host: azure.ai.agent\n    uses: ["+subnet+", "+role+", "+storage+"]\n")
	g, err := Build(Input{
		Source:    SourceInput{Document: d, Services: []coregraph.Service{{Name: "a", Host: "azure.ai.agent", Uses: uses}}},
		Inventory: []azure.Resource{{ID: testProv + "/Microsoft.Storage/storageAccounts/other", Name: "other", Type: "Microsoft.Storage/storageAccounts"}},
	}, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	agent := nodeByLabel(t, g, "a").ID
	for label, kind := range map[string]string{"snet": "network", "ra1": "role"} {
		n := nodeByLabel(t, g, label)
		if n.Origin != OriginUnobserved || n.Missing || n.External || n.Kind != kind {
			t.Errorf("%s: want unobserved %s node, got %+v", label, kind, n)
		}
		if e, ok := hasEdge(g, agent, n.ID, "uses"); !ok || e.Missing || e.External {
			t.Errorf("%s: want a plain uses edge, got %+v", label, g.Edges)
		}
	}
	var missing int
	for _, n := range g.Nodes {
		if n.Missing {
			missing++
			if n.Origin != OriginMissing {
				t.Errorf("missing node with origin %s", n.Origin)
			}
		}
	}
	if missing != 1 {
		t.Fatalf("want only the observable storage reference missing, got %d: %+v", missing, g.Nodes)
	}
}

// A source reference to a deployed resource resolves to the inventory node.
func TestCombinedSourceRefToDeployedResourceResolves(t *testing.T) {
	storeID := testProv + "/Microsoft.Storage/storageAccounts/store"
	d := doc(t, "name: s\nservices:\n  a:\n    host: azure.ai.agent\n    uses: ["+storeID+"]\n")
	g, err := Build(Input{
		Source:    SourceInput{Document: d, Services: []coregraph.Service{{Name: "a", Host: "azure.ai.agent", Uses: []string{storeID}}}},
		Inventory: []azure.Resource{{ID: storeID, Name: "store", Type: "Microsoft.Storage/storageAccounts"}},
	}, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hasEdge(g, nodeByLabel(t, g, "a").ID, nodeByLabel(t, g, "store").ID, "uses"); !ok {
		t.Fatalf("want agent -> deployed store: %+v %+v", g.Nodes, g.Edges)
	}
	for _, n := range g.Nodes {
		if n.Missing || n.External {
			t.Fatalf("a deployed reference must not be missing or external: %+v", g.Nodes)
		}
	}
}

// A reference to a runtime-only type stays unobserved until the runtime
// overlay returns the resource, which then merges into it by ARM ID.
func TestRuntimeResourceMergesIntoUnobservedRef(t *testing.T) {
	depID := testProv + "/Microsoft.CognitiveServices/accounts/acct/deployments/dep"
	d := doc(t, "name: s\nservices:\n  a:\n    host: azure.ai.agent\n    uses: ["+depID+"]\n")
	g, err := Build(Input{
		Source: SourceInput{Document: d, Services: []coregraph.Service{{Name: "a", Host: "azure.ai.agent", Uses: []string{depID}}}},
		Runtime: RuntimeInput{
			Deployments:   []azure.AccountDeployment{{ID: depID, Name: "dep", Type: deploymentType, ProvisioningState: "Succeeded"}},
			ObservedTypes: []string{deploymentType},
		},
	}, Options{Mode: ModeCombined, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	dep := nodeByLabel(t, g, "dep")
	if dep.Origin != OriginAzure || dep.Resource != depID {
		t.Fatalf("want the runtime deployment merged into the reference: %+v", dep)
	}
	if _, ok := hasEdge(g, nodeByLabel(t, g, "a").ID, dep.ID, "uses"); !ok {
		t.Fatalf("missing agent -> deployment edge: %+v", g.Edges)
	}
}

// Deployed mode keeps Azure-side dangling references: a deleted in-scope
// user-assigned identity and a deleted in-scope connection target are
// missing; an in-scope subnet target is unobserved.
func TestDeployedKeepsMissingAndUnobservedReferences(t *testing.T) {
	goneUA := testProv + "/Microsoft.ManagedIdentity/userAssignedIdentities/id-gone"
	goneStore := testProv + "/Microsoft.Storage/storageAccounts/gone"
	subnet := testProv + "/Microsoft.Network/virtualNetworks/vnet/subnets/snet"
	g, err := Build(Input{
		Inventory: []azure.Resource{
			{ID: testProv + "/Microsoft.Storage/storageAccounts/st", Name: "st", Type: "Microsoft.Storage/storageAccounts", UserAssignedIdentities: []string{goneUA}},
		},
		Runtime: RuntimeInput{Connections: []azure.FoundryConnection{
			{ID: testProv + "/Microsoft.CognitiveServices/accounts/acct/connections/c1", Name: "c1",
				Target: azure.ConnectionTarget{ResourceID: goneStore, Endpoint: "https://gone.blob.core.windows.net/"}},
			{ID: testProv + "/Microsoft.CognitiveServices/accounts/acct/connections/c2", Name: "c2",
				Target: azure.ConnectionTarget{ResourceID: subnet}},
		}},
	}, Options{Mode: ModeDeployed, Scope: testScope})
	if err != nil {
		t.Fatal(err)
	}
	st, c1, c2 := nodeByLabel(t, g, "st").ID, nodeByLabel(t, g, "c1").ID, nodeByLabel(t, g, "c2").ID
	missingTargets := map[string]bool{}
	for _, e := range g.Edges {
		if e.Missing {
			missingTargets[e.From+">"+e.Kind] = true
		}
	}
	if !missingTargets[st+">usesIdentity"] || !missingTargets[c1+">targets"] {
		t.Fatalf("want missing identity and connection-target edges in deployed mode: %+v %+v", g.Nodes, g.Edges)
	}
	var missingNodes int
	for _, n := range g.Nodes {
		if n.Origin == OriginMissing {
			missingNodes++
		}
		if n.External {
			t.Fatalf("an in-scope ARM ID target must win over the endpoint URL: %+v", g.Nodes)
		}
	}
	if missingNodes != 2 {
		t.Fatalf("want 2 missing nodes, got %d: %+v", missingNodes, g.Nodes)
	}
	snet := nodeByLabel(t, g, "snet")
	if snet.Origin != OriginUnobserved {
		t.Fatalf("subnet target must be unobserved: %+v", snet)
	}
	if e, ok := hasEdge(g, c2, snet.ID, "targets"); !ok || e.Missing {
		t.Fatalf("want a plain c2 -> subnet edge: %+v", g.Edges)
	}
}
