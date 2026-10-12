package view

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func doc(t *testing.T, s string) *azureyaml.Document {
	t.Helper()
	d, err := azureyaml.Parse([]byte(s), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBuildDeterministicAcrossOrderings(t *testing.T) {
	yaml := "name: sample\nservices:\n  proj:\n    host: azure.ai.project\n    deployments:\n      - name: gpt-4o-mini\n        model:\n          format: OpenAI\n          name: gpt-4o-mini\n  agent:\n    host: azure.ai.agent\n    uses: [proj, store, missing]\n"
	first, err := Build(Input{
		Source: SourceInput{
			Document:  doc(t, yaml),
			Services:  []coregraph.Service{{Name: "proj", Host: "azure.ai.project"}, {Name: "agent", Host: "azure.ai.agent", Uses: []string{"proj", "store", "missing"}}},
			Resources: []coregraph.Resource{{ID: "/resources/1", Type: "Microsoft.Storage/storageAccounts", Name: "store"}},
		},
	}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(Input{
		Source: SourceInput{
			Document:  doc(t, yaml),
			Services:  []coregraph.Service{{Name: "agent", Host: "azure.ai.agent", Uses: []string{"proj", "store", "missing"}}, {Name: "proj", Host: "azure.ai.project"}},
			Resources: []coregraph.Resource{{ID: "/resources/1", Type: "Microsoft.Storage/storageAccounts", Name: "store"}},
		},
	}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		a, _ := json.MarshalIndent(first, "", "  ")
		b, _ := json.MarshalIndent(second, "", "  ")
		t.Fatalf("non-deterministic build\n%s\n!=\n%s", a, b)
	}
}

func TestBuildCombinedOverlayAndCollapse(t *testing.T) {
	g, err := Build(Input{
		Source: SourceInput{
			Document: doc(t, "name: s\nservices:\n  proj:\n    host: azure.ai.project\n"),
			Services: []coregraph.Service{{Name: "proj", Host: "azure.ai.project"}},
			Resources: []coregraph.Resource{
				{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "st"},
				{ID: "/resources/1", Type: "Microsoft.Network/virtualNetworks", Name: "vnet"},
				{ID: "/resources/2", Type: "Microsoft.ManagedIdentity/userAssignedIdentities", Name: "uai"},
			},
		},
		Inventory: []azure.Resource{
			{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/st", Name: "st", Type: "Microsoft.Storage/storageAccounts", Properties: map[string]string{"properties.provisioningState": "Succeeded"}},
			{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Insights/components/appi", Name: "appi", Type: "Microsoft.Insights/components", Properties: map[string]string{"properties.provisioningState": "Failed"}},
		},
		Findings: []sdk.Finding{{RuleID: "FND-X", Severity: sdk.SeverityError, Resource: sdk.ResourceRef{Type: "Microsoft.Storage/storageAccounts", Name: "st"}, Fingerprint: "a"}},
	}, Options{Mode: ModeCombined, CollapseAfter: 4, MaxEdges: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) == 0 {
		t.Fatalf("empty graph: %+v", g)
	}
	if g.Summary.CollapsedNodes == 0 {
		t.Fatalf("expected collapsed nodes: %+v", g.Summary)
	}
	if g.Summary.UnmatchedFindings != 0 {
		t.Fatalf("expected all findings matched: %+v", g.Summary)
	}
	var sawBoth, sawAzure, sawUnhealthy bool
	for _, n := range g.Nodes {
		sawBoth = sawBoth || n.Origin == OriginBoth
		sawAzure = sawAzure || n.Kind == "monitoring"
		sawUnhealthy = sawUnhealthy || n.Unhealthy
	}
	if !sawBoth || !sawAzure || !sawUnhealthy {
		t.Fatalf("origins/health missing: %+v", g.Nodes)
	}
}

func TestBuildDeployedSkipsSourceOnlyNodes(t *testing.T) {
	g, err := Build(Input{
		Source: SourceInput{
			Document:  doc(t, "name: s\nservices:\n  proj:\n    host: azure.ai.project\n"),
			Services:  []coregraph.Service{{Name: "proj", Host: "azure.ai.project"}},
			Resources: []coregraph.Resource{{ID: "/resources/1", Type: "Microsoft.Storage/storageAccounts", Name: "store"}},
		},
		Inventory: []azure.Resource{
			{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/store", Name: "store", Type: "Microsoft.Storage/storageAccounts", Properties: map[string]string{"properties.provisioningState": "Succeeded"}},
		},
	}, Options{Mode: ModeDeployed})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Origin == OriginLocal {
			t.Fatalf("deployed graph retained source-only node: %+v", g.Nodes)
		}
	}
}

func TestMissingAndExternalReferences(t *testing.T) {
	d := doc(t, "name: s\nservices:\n  a:\n    host: azure.ai.agent\n    uses: [ghost, /subscriptions/x/resourceGroups/y/providers/Microsoft.Storage/storageAccounts/st]\n")
	g, err := Build(Input{Source: SourceInput{
		Document: d,
		Services: []coregraph.Service{{Name: "a", Host: "azure.ai.agent", Uses: []string{"ghost", "/subscriptions/x/resourceGroups/y/providers/Microsoft.Storage/storageAccounts/st"}}},
	}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	var missing, external bool
	for _, n := range g.Nodes {
		missing = missing || n.Missing
		external = external || n.External
	}
	if !missing || !external {
		t.Fatalf("expected missing and external nodes: %+v", g.Nodes)
	}
}

func TestSourceConnectionTargetExternalEdge(t *testing.T) {
	d := doc(t, "name: s\nservices:\n  conn:\n    host: azure.ai.connection\n    target: https://example.openai.azure.com/\n")
	g, err := Build(Input{Source: SourceInput{
		Document: d,
		Services: []coregraph.Service{{Name: "conn", Host: "azure.ai.connection"}},
	}}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	nodeKinds := map[string]string{}
	for _, n := range g.Nodes {
		nodeKinds[n.ID] = n.Kind
	}
	for _, e := range g.Edges {
		if e.Kind == "targets" && nodeKinds[e.From] == "connection" && e.External {
			for _, n := range g.Nodes {
				if n.ID == e.To && n.Name != "" {
					t.Fatalf("expected placeholder target name to be scrubbed: %+v", n)
				}
			}
			return
		}
	}
	t.Fatalf("expected source connection target edge: %+v %+v", g.Nodes, g.Edges)
}

func TestCombinedSourceConnectionTargetCorrelatesInventoryResource(t *testing.T) {
	targetID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/store"
	d := doc(t, "name: s\nservices:\n  conn:\n    host: azure.ai.connection\n    target: "+targetID+"\n")
	g, err := Build(Input{
		Source: SourceInput{
			Document: d,
			Services: []coregraph.Service{{Name: "conn", Host: "azure.ai.connection"}},
		},
		Inventory: []azure.Resource{
			{ID: targetID, Name: "store", Type: "Microsoft.Storage/storageAccounts"},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	nodeKinds := map[string]string{}
	for _, n := range g.Nodes {
		nodeKinds[n.ID] = n.Kind
	}
	for _, e := range g.Edges {
		if e.Kind == "targets" && nodeKinds[e.From] == "connection" && nodeKinds[e.To] == "storage" && !e.External {
			return
		}
	}
	t.Fatalf("expected combined target correlation: %+v %+v", g.Nodes, g.Edges)
}

func TestSourceConnectionTargetCorrelatesLocalResourceByARMID(t *testing.T) {
	targetID := "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/store"
	d := doc(t, "name: s\nservices:\n  conn:\n    host: azure.ai.connection\n    target: "+targetID+"\n")
	g, err := Build(Input{
		Source: SourceInput{
			Document:  d,
			Services:  []coregraph.Service{{Name: "conn", Host: "azure.ai.connection"}},
			Resources: []coregraph.Resource{{ID: "/resources/1", Type: "Microsoft.Storage/storageAccounts", Name: "store"}},
		},
	}, Options{Mode: ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	nodeKinds := map[string]string{}
	for _, n := range g.Nodes {
		nodeKinds[n.ID] = n.Kind
	}
	for _, e := range g.Edges {
		if e.Kind == "targets" && nodeKinds[e.From] == "connection" && nodeKinds[e.To] == "storage" && !e.External {
			return
		}
	}
	t.Fatalf("expected source ARM target correlation: %+v %+v", g.Nodes, g.Edges)
}

func TestBuildCombinedMarksMatchedProjectAndDeploymentAsBoth(t *testing.T) {
	yaml := "name: sample\nservices:\n  proj:\n    host: azure.ai.project\n    deployments:\n      - name: dep1\n  agent:\n    host: azure.ai.agent\n    deployments:\n      - name: dep2\n"
	g, err := Build(Input{
		Source: SourceInput{
			Document: doc(t, yaml),
			Services: []coregraph.Service{
				{Name: "proj", Host: "azure.ai.project"},
				{Name: "agent", Host: "azure.ai.agent"},
			},
		},
		Runtime: RuntimeInput{
			Project:     &azure.FoundryProject{Name: "proj", ProvisioningState: "Succeeded"},
			Deployments: []azure.AccountDeployment{{Name: "dep1", ProvisioningState: "Succeeded"}, {Name: "dep2", ProvisioningState: "Succeeded"}},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	var sawProjectBoth, sawDep1Both, sawDep2Both bool
	for _, n := range g.Nodes {
		if n.Kind == "project" && n.Label == "proj" && n.Origin == OriginBoth {
			sawProjectBoth = true
		}
		if n.Kind == "model" && n.Label == "dep1" && n.Origin == OriginBoth {
			sawDep1Both = true
		}
		if n.Kind == "model" && n.Label == "dep2" && n.Origin == OriginBoth {
			sawDep2Both = true
		}
	}
	if !sawProjectBoth || !sawDep1Both || !sawDep2Both {
		t.Fatalf("expected matched nodes to render as both: %+v", g.Nodes)
	}
}

func TestBuildCombinedReusesInventoryAccountNode(t *testing.T) {
	g, err := Build(Input{
		Inventory: []azure.Resource{
			{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts"},
		},
		Runtime: RuntimeInput{
			Account: &azure.Resource{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts"},
			Project: &azure.FoundryProject{Name: "proj", ProvisioningState: "Succeeded"},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, n := range g.Nodes {
		if n.Kind == "foundry-account" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one account node, got %d: %+v", count, g.Nodes)
	}
}

func TestRuntimeConnectionTargetsInventoryResource(t *testing.T) {
	g, err := Build(Input{
		Inventory: []azure.Resource{
			{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/store", Name: "store", Type: "Microsoft.Storage/storageAccounts"},
		},
		Runtime: RuntimeInput{
			Connections: []azure.FoundryConnection{{
				ID:   "/subscriptions/s/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct/projects/proj/connections/conn1",
				Name: "conn1",
				Target: azure.ConnectionTarget{
					ResourceID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/store",
				},
			}},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	nodeKinds := map[string]string{}
	for _, n := range g.Nodes {
		nodeKinds[n.ID] = n.Kind
	}
	for _, e := range g.Edges {
		if e.Kind == "targets" && nodeKinds[e.From] == "connection" && nodeKinds[e.To] == "storage" {
			return
		}
	}
	t.Fatalf("expected connection target edge: %+v %+v", g.Nodes, g.Edges)
}

func TestRuntimeConnectionTargetsExternalEndpoint(t *testing.T) {
	g, err := Build(Input{
		Runtime: RuntimeInput{
			Connections: []azure.FoundryConnection{{
				Name: "conn1",
				Target: azure.ConnectionTarget{
					Endpoint: "https://example.openai.azure.com/",
				},
			}},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range g.Edges {
		if e.Kind == "targets" && e.External {
			return
		}
	}
	t.Fatalf("expected external target edge: %+v %+v", g.Nodes, g.Edges)
}

func TestProjectScopedRuntimeDoesNotAttachAccountDeploymentsToProject(t *testing.T) {
	g, err := Build(Input{
		Runtime: RuntimeInput{
			Account:     &azure.Resource{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts"},
			Project:     &azure.FoundryProject{Name: "proj", ProvisioningState: "Succeeded"},
			Deployments: []azure.AccountDeployment{{Name: "dep1", ProvisioningState: "Succeeded"}},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	nodeKinds := map[string]string{}
	for _, n := range g.Nodes {
		nodeKinds[n.ID] = n.Kind
	}
	for _, e := range g.Edges {
		if e.Kind == "deploys" && nodeKinds[e.From] == "project" && nodeKinds[e.To] == "model" {
			t.Fatalf("unexpected project->deployment edge: %+v", g.Edges)
		}
	}
}

func TestRedactIDs(t *testing.T) {
	g, err := Build(Input{
		Source: SourceInput{
			Document: doc(t, "name: s\nservices:\n  a:\n    host: azure.ai.agent\n"),
			Services: []coregraph.Service{{Name: "a", Host: "azure.ai.agent"}},
		},
		Inventory: []azure.Resource{
			{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/a", Name: "a", Type: "Microsoft.Storage/storageAccounts", Properties: map[string]string{"properties.provisioningState": "Failed"}},
		},
		Findings: []sdk.Finding{{RuleID: "FND-X", Severity: sdk.SeverityWarning, Resource: sdk.ResourceRef{Type: "nope", Name: "none"}, Fingerprint: "a"}},
	}, Options{Mode: ModeCombined, RedactIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	if g.Summary.UnmatchedFindings != 1 {
		t.Fatalf("expected unmatched findings count: %+v", g.Summary)
	}
	for _, n := range g.Nodes {
		if n.Name != "" || n.Resource != "" || n.Health != "" || n.Source.File != "" || n.Label == "a" {
			t.Fatalf("expected redacted node: %+v", g.Nodes)
		}
	}
}

func TestRuntimeConnectionHealthSanitized(t *testing.T) {
	g, err := Build(Input{
		Runtime: RuntimeInput{
			Account:     &azure.Resource{ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts"},
			Connections: []azure.FoundryConnection{{Name: "conn1", Error: "dial tcp secret-host:443: unauthorized"}},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Kind == "connection" {
			if n.Health != "error reported" {
				t.Fatalf("expected sanitized health, got %+v", n)
			}
			if n.Label == "secret-host" || n.Name == "secret-host" {
				t.Fatalf("unexpected secret leakage: %+v", n)
			}
		}
	}
}

func TestDeploymentStateMarksPausedDeploymentUnhealthy(t *testing.T) {
	g, err := Build(Input{
		Runtime: RuntimeInput{
			Deployments: []azure.AccountDeployment{{Name: "dep1", ProvisioningState: "Succeeded", DeploymentState: "Paused"}},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Kind == "model" && n.Label == "dep1" {
			if !n.Unhealthy || n.Health != "paused" {
				t.Fatalf("expected paused deployment to be unhealthy: %+v", n)
			}
			return
		}
	}
	t.Fatalf("deployment node not found: %+v", g.Nodes)
}

func TestBuildCombinedDoesNotCorrelateDuplicateAzureNames(t *testing.T) {
	g, err := Build(Input{
		Source: SourceInput{
			Resources: []coregraph.Resource{{ID: "/resources/1", Type: "Microsoft.Storage/storageAccounts", Name: "store"}},
		},
		Inventory: []azure.Resource{
			{ID: "/subscriptions/s/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/store", Name: "store", Type: "Microsoft.Storage/storageAccounts"},
			{ID: "/subscriptions/s/resourceGroups/rg2/providers/Microsoft.Storage/storageAccounts/store", Name: "store", Type: "Microsoft.Storage/storageAccounts"},
		},
	}, Options{Mode: ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Kind == "storage" && n.Origin == OriginBoth {
			t.Fatalf("duplicate Azure names should not correlate: %+v", g.Nodes)
		}
	}
}
