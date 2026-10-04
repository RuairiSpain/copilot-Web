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
		{"storage", "search:hub"}, {"search:hub", "private-endpoint:search:hub:searchService"},
		{"search:hub", "knowledge-base:hub:policies"}, {"gateway", "alerts"}, {"alerts", "governance"},
	} {
		if index(p, pair[0]) < 0 || index(p, pair[0]) > index(p, pair[1]) {
			t.Fatalf("%s must come before %s in %v", pair[0], pair[1], p.Order())
		}
	}
}

func TestStandardSetupDependencies(t *testing.T) {
	p := MustPlan(t, Private, `projects: [{name: fin}]`)
	wantDeps(t, p, "cosmos", "resource-group", "identity")
	wantDeps(t, p, "private-endpoint:cosmos:Sql", "network", "private-dns", "cosmos")
	wantDeps(t, p, "private-endpoint:storage:blob", "network", "private-dns", "storage")
	wantDeps(t, p, "private-endpoint:search:root:searchService", "network", "private-dns", "search:root")
	basic := MustPlan(t, Public, `projects: [{name: fin}]`)
	for _, id := range basic.Order() {
		if id == "cosmos" || id == "network" {
			t.Fatalf("basic public setup must not create %s", id)
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

func TestKnowledgeBaseDependencies(t *testing.T) {
	p := MustPlan(t, Public,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: site, type: sharepoint, site: hr, connection: sp}, {name: files, type: blob, container: policies}]}]}`,
		`projects: [{name: fin}]`)
	exactDeps(t, p, "knowledge-base:project:fin:policies", "search:root", "storage")
}

func TestHubKnowledgeBasesAreSharedAndRootOnesAreInstantiatedPerProject(t *testing.T) {
	kb := `knowledgeBases: [{name: %s, sources: [{name: files, type: blob, container: policies}]}]`
	p := MustOK(t, RunHub(t, Public, `iq: {`+fmt.Sprintf(kb, "shared")+`}`, `projects: [{name: aa}, {name: bb}]`,
		`hub: {name: hub1, search: {}, inheritance: {iq: true, search: true}, iq: {`+fmt.Sprintf(kb, "central")+`}}`))
	for _, id := range []string{"knowledge-base:project:aa:shared", "knowledge-base:project:bb:shared", "knowledge-base:hub:central"} {
		if index(p, id) < 0 {
			t.Fatalf("missing %s in %v", id, p.Order())
		}
	}
	if index(p, "knowledge-base:project:aa:central") >= 0 {
		t.Fatal("a hub knowledge base is not instantiated per project")
	}
	wantDeps(t, p, "knowledge-base:hub:central", "search:hub")
}

func TestGatewayDependsOnSearchAndKnowledgeBaseTargets(t *testing.T) {
	p := MustPlan(t, Public, models, `observability: {}`, `search: {name: srch-main}`,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`,
		`projects: [{name: fin}]`,
		`gateway: {enabled: true, endpoints: [
			{name: ep1, path: /a, target: bot, targetType: agent},
			{name: ep2, path: /m, target: gpt-5, targetType: model},
			{name: ep3, path: /s, target: srch-main, targetType: search},
			{name: ep4, path: /k, target: policies, targetType: knowledgeBase}]}`)
	wantDeps(t, p, "gateway", "search:root", "knowledge-base:project:fin:policies", "observability")
}

func TestAlertsAndGovernance(t *testing.T) {
	p := MustPlan(t, Public, `observability: {alerts: true}`, `search: {}`, `governance: {}`, `gateway: {enabled: true}`)
	wantDeps(t, p, "alerts", "observability", "search:root", "gateway")
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

func mustEnterprise(t *testing.T) plan.Analysis {
	t.Helper()
	a, err := plan.AnalyseFile("../../examples/enterprise.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return a
}
