package plan_test

import (
	"fmt"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/graph"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
)

func deps(t *testing.T, p *plan.Plan, id string) map[string]bool {
	t.Helper()
	n, ok := p.Node(id)
	if !ok {
		t.Fatalf("no node %s in %v", id, p.Order())
	}
	out := map[string]bool{}
	for _, d := range n.DependsOn {
		out[d] = true
	}
	return out
}

func wantDeps(t *testing.T, p *plan.Plan, id string, want ...string) {
	t.Helper()
	got := deps(t, p, id)
	for _, w := range want {
		if !got[w] {
			t.Fatalf("%s should depend on %s; depends on %v", id, w, got)
		}
	}
}

func exactDeps(t *testing.T, p *plan.Plan, id string, want ...string) {
	t.Helper()
	got := deps(t, p, id)
	if len(got) != len(want) {
		t.Fatalf("%s depends on %v, want %v", id, got, want)
	}
	wantDeps(t, p, id, want...)
}

func index(p *plan.Plan, id string) int {
	for i, n := range p.Nodes {
		if n.ID == id {
			return i
		}
	}
	return -1
}

func TestEveryNodeKindHasAStage(t *testing.T) {
	p := MustOK(t, mustEnterprise(t))
	for _, n := range p.Nodes {
		if _, ok := graph.Stages[n.Kind]; !ok {
			t.Fatalf("kind %s has no stage", n.Kind)
		}
	}
}

func TestStageOrderFollowsTheSpecification(t *testing.T) {
	p := MustOK(t, mustEnterprise(t))
	for _, pair := range [][2]string{
		{"resource-group", "identity"}, {"identity", "network"}, {"network", "storage"},
		{"storage", "search:hub"}, {"search:hub", "foundry"}, {"foundry", "foundry-project:hub"},
		{"foundry-project:hub", "model-deployment:hub:gpt-5"}, {"gateway", "alerts"}, {"alerts", "governance"},
	} {
		if index(p, pair[0]) < 0 || index(p, pair[0]) > index(p, pair[1]) {
			t.Fatalf("%s must come before %s in %v", pair[0], pair[1], p.Order())
		}
	}
}

func TestStandardSetupDependencies(t *testing.T) {
	p := MustPlan(t, Private, `projects: [{name: fin, agents: [{name: bot, model: gpt-5}]}]`, `models: {allowed: [gpt-5]}`)
	exactDeps(t, p, "capability-host:project:fin:agents",
		"foundry-project:project:fin", "storage", "cosmos", "network", "search:root",
		"private-endpoint:storage:blob", "private-endpoint:cosmos:Sql", "private-endpoint:search:root:searchService")
	wantDeps(t, p, "agent:project:fin:bot", "capability-host:project:fin:agents", "model-deployment:root:gpt-5", "foundry-project:project:fin")
	wantDeps(t, p, "cosmos", "resource-group", "identity")
	wantDeps(t, p, "private-endpoint:cosmos:Sql", "network", "private-dns", "cosmos")
	basic := MustPlan(t, Public, `projects: [{name: fin, agents: [{name: bot, model: gpt-5}]}]`, `models: {allowed: [gpt-5]}`)
	for _, id := range basic.Order() {
		if id == "cosmos" || id == "capability-host:project:fin:agents" {
			t.Fatalf("basic setup must not create %s", id)
		}
	}
}

func TestIdentityNodeFollowsTheIdentityType(t *testing.T) {
	system := MustPlan(t, `managedIdentity: {type: systemAssigned}`)
	if index(system, "identity") >= 0 {
		t.Fatal("no user-assigned identity node for systemAssigned")
	}
	both := MustPlan(t, `managedIdentity: {type: systemAssignedAndUserAssigned}`)
	if index(both, "identity") < 0 {
		t.Fatal("user-assigned identity node expected")
	}
	if index(MustPlan(t, `managedIdentity: {enabled: false}`, Public), "identity") >= 0 {
		t.Fatal("disabled identity")
	}
}

func TestKnowledgeBaseAndAgentDependencies(t *testing.T) {
	p := MustPlan(t, Public, models,
		`connectors: [{name: sp, type: sharepoint, authentication: {mode: apiKey, secretRef: sp-key}}]`,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: site, type: sharepoint, site: hr, connection: sp}, {name: files, type: blob, container: policies}]}]}`,
		`projects: [{name: fin, agents: [{name: bot, knowledgeBases: [policies]}]}]`)
	kb := "knowledge-base:project:fin:policies"
	exactDeps(t, p, kb, "foundry-project:project:fin", "search:root", "storage", "connector:project:fin:sp", "model-deployment:root:text-embedding-3-large")
	exactDeps(t, p, "connector:project:fin:sp", "foundry-project:project:fin", "key-vault")
	exactDeps(t, p, "agent:project:fin:bot", "foundry-project:project:fin", kb, "model-deployment:root:gpt-5")
}

func TestHubItemsAreSharedAndRootItemsAreInstantiatedPerProject(t *testing.T) {
	p := MustOK(t, RunHub(t, Public, models, `agents: [{name: shared}]`, `projects: [{name: aa}, {name: bb}]`,
		`hub: {name: hub1, mcps: [{name: graph, endpoint: "https://a.example"}], toolboxes: [{name: tb, tools: [{name: t1, type: mcp, reference: graph}]}]}`))
	for _, id := range []string{"agent:project:aa:shared", "agent:project:bb:shared", "mcp:hub:graph", "toolbox:hub:tb"} {
		if index(p, id) < 0 {
			t.Fatalf("missing %s in %v", id, p.Order())
		}
	}
	if index(p, "mcp:project:aa:graph") >= 0 {
		t.Fatal("hub MCP is not instantiated per project")
	}
	exactDeps(t, p, "toolbox:hub:tb", "foundry-project:hub", "mcp:hub:graph")
}

func TestGatewayDependsOnEveryEndpointTarget(t *testing.T) {
	p := MustPlan(t, Public, models, `observability: {}`, `search: {name: srch-main}`,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`,
		`projects: [{name: fin, agents: [{name: bot, instructions: x}]}]`,
		`gateway: {enabled: true, endpoints: [
			{name: ep1, path: /a, target: bot, targetType: agent},
			{name: ep2, path: /m, target: gpt-5, targetType: model},
			{name: ep3, path: /s, target: srch-main, targetType: search},
			{name: ep4, path: /k, target: policies, targetType: knowledgeBase}]}`)
	wantDeps(t, p, "gateway", "agent:project:fin:bot", "model-deployment:root:gpt-5", "search:root",
		"knowledge-base:project:fin:policies", "observability")
}

func TestAlertsAndGovernance(t *testing.T) {
	p := MustPlan(t, Public, `observability: {alerts: true}`, `search: {}`, `governance: {}`, `gateway: {enabled: true}`)
	wantDeps(t, p, "alerts", "observability", "search:root", "gateway", "foundry")
	wantDeps(t, p, "governance", "resource-group", "gateway", "alerts")
	if p.Order()[len(p.Nodes)-1] != "governance" {
		t.Fatalf("governance should be last: %v", p.Order())
	}
	quiet := MustPlan(t, Public, `observability: {alerts: false}`)
	if index(quiet, "alerts") >= 0 || index(quiet, "governance") >= 0 {
		t.Fatal("no alerts or governance expected")
	}
}

func TestExistingResourcesAreMarked(t *testing.T) {
	p := MustPlan(t, Public,
		fmt.Sprintf(`storage: {existingResourceId: "%s/Microsoft.Storage/storageAccounts/st1"}`, arm),
		fmt.Sprintf(`search: {existingResourceId: "%s/Microsoft.Search/searchServices/s1"}`, arm),
		fmt.Sprintf(`cosmos: {existingResourceId: "%s/Microsoft.DocumentDB/databaseAccounts/c1"}`, arm),
		fmt.Sprintf(`keyVault: {existingResourceId: "%s/Microsoft.KeyVault/vaults/kv1"}`, arm),
		fmt.Sprintf(`managedIdentity: {existingResourceId: "%s/Microsoft.ManagedIdentity/userAssignedIdentities/id1"}`, arm))
	for _, id := range []string{"storage", "search:root", "cosmos", "key-vault", "identity"} {
		if n, _ := p.Node(id); !n.Existing {
			t.Fatalf("%s should be marked existing", id)
		}
	}
	if n, _ := p.Node("foundry"); n.Existing {
		t.Fatal("the Foundry resource is always created")
	}
}

func TestExistingVNetHasNoNetworkNode(t *testing.T) {
	vnet := arm + "/Microsoft.Network/virtualNetworks/v1"
	p := MustPlan(t, fmt.Sprintf(`security: {roles: {admins: [a]}, network: {mode: private, existingVnetResourceId: %q, existingPrivateEndpointSubnetResourceId: %q, existingAgentSubnetResourceId: %q}}`,
		vnet, vnet+"/subnets/pe", vnet+"/subnets/agents"))
	if index(p, "network") >= 0 {
		t.Fatal("no network node for an existing VNet")
	}
	exactDeps(t, p, "private-dns", "resource-group")
}

func TestEvaluationDependsOnStorageAndAgents(t *testing.T) {
	p := MustPlan(t, Public, models, `projects: [{name: fin, agents: [{name: bot}], evaluation: {enabled: true, datasets: [{name: golden, path: g.jsonl}]}}]`)
	exactDeps(t, p, "evaluation:project:fin:evaluation", "foundry-project:project:fin", "storage", "agent:project:fin:bot")
}

func mustEnterprise(t *testing.T) plan.Analysis {
	t.Helper()
	a, err := plan.AnalyseFile("../../examples/enterprise.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return a
}
