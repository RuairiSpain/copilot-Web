package plan_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/parser"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
)

func cfgOf(t *testing.T, overrides ...string) *normalise.Config {
	t.Helper()
	return MustPlan(t, overrides...).Config
}

func hubCfg(t *testing.T, overrides ...string) *normalise.Config {
	t.Helper()
	return MustOK(t, RunHub(t, overrides...)).Config
}

func implicit(c *normalise.Config) map[string]bool {
	out := map[string]bool{}
	for _, i := range c.Implicit {
		out[i.Kind+":"+i.Name] = true
	}
	return out
}

func names[T interface{ GetName() string }](items []T) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.GetName()
	}
	return out
}

func same(t *testing.T, got, want []string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDefaultsTagsAndEnvironment(t *testing.T) {
	c := cfgOf(t, `defaults: {environment: prod, location: westeurope, tags: {team: x}}`, `tags: {owner: me}`, `search: {replicas: 2}`)
	if c.Environment != "prod" {
		t.Fatal(c.Environment)
	}
	for k, v := range map[string]string{"environment": "prod", "managed-by": "x-foundry", "team": "x", "owner": "me"} {
		if c.Tags[k] != v {
			t.Fatalf("tags = %v", c.Tags)
		}
	}
	p := c.Project("finance")
	if p.Tags["project"] != "finance" || p.Location != "westeurope" || p.DisplayName != "finance" {
		t.Fatalf("project = %+v", p)
	}
}

func TestPrincipalsAreResolvedDeduplicatedAndMerged(t *testing.T) {
	guid := "11111111-2222-3333-4444-555555555555"
	c := cfgOf(t,
		fmt.Sprintf(`security: {roles: {admins: [Admins, admins, %s, {type: user, name: ann}], developers: [Devs]}}`, guid),
		fmt.Sprintf(`projects: [{name: fin, roles: {developers: [Devs, FinDevs], consumers: [{type: servicePrincipal, id: %s}]}}]`, guid))
	var admins []string
	for _, a := range c.Roles.Admins {
		admins = append(admins, a.Name+a.ID)
	}
	same(t, admins, []string{"Admins", guid, "ann"})
	if c.Roles.Admins[1].Name != "" || c.Roles.Admins[2].Type != "user" {
		t.Fatalf("admins = %+v", c.Roles.Admins)
	}
	p := c.Project("fin")
	var devs []string
	for _, d := range p.Roles.Developers {
		devs = append(devs, d.Name)
	}
	same(t, devs, []string{"Devs", "FinDevs"})
	if p.Roles.Consumers[0].Type != "servicePrincipal" || len(p.Roles.Admins) != 3 {
		t.Fatalf("roles = %+v", p.Roles)
	}
}

func TestMinimalPrivateDefaultsUseTheStandardAgentSetup(t *testing.T) {
	c := cfgOf(t)
	if c.AgentSetup != "standard" || c.FoundryIdentity != "systemAssigned" {
		t.Fatalf("setup = %s identity = %s", c.AgentSetup, c.FoundryIdentity)
	}
	got := implicit(c)
	for _, want := range []string{"managed-identity:identity", "network:vnet", "storage:storage", "storage-purpose:documents", "search:search", "cosmos:cosmos"} {
		if !got[want] {
			t.Fatalf("missing implicit %s in %v", want, got)
		}
	}
	if c.Cosmos == nil || c.Cosmos.Throughput != 3000 || c.Cosmos.CapacityMode != "provisioned" {
		t.Fatalf("cosmos = %+v", c.Cosmos)
	}
	if c.Network.AgentSubnet != "create" || c.Network.AgentSubnetPrefixLength != 24 || c.Network.AddressSpace != "10.20.0.0/16" {
		t.Fatalf("network = %+v", c.Network)
	}
	if c.Project("finance").SearchScope != "root" {
		t.Fatal("the standard setup needs a resolved Search")
	}
}

func TestBasicPublicMinimalCreatesAlmostNothing(t *testing.T) {
	c := cfgOf(t, Public)
	if c.AgentSetup != "basic" {
		t.Fatal(c.AgentSetup)
	}
	if c.Storage != nil || c.Redis != nil || c.Cosmos != nil || c.KeyVault != nil || c.Events != nil || c.Gateway != nil || c.Observability != nil {
		t.Fatalf("unexpected components: %+v", c)
	}
	if got := implicit(c); len(got) != 1 || !got["managed-identity:identity"] {
		t.Fatalf("implicit = %v", got)
	}
	if c.Project("finance").SearchScope != "" {
		t.Fatal("no Search without IQ")
	}
}

func TestAgentSetupResolution(t *testing.T) {
	cases := []struct {
		name string
		yaml []string
		want string
	}{
		{"private auto", y(), "standard"},
		{"public auto", y(Public), "basic"},
		{"public with cosmos", y(Public, `cosmos: {}`), "standard"},
		{"explicit standard on public", y(Public, `agentService: {setup: standard}`), "standard"},
		{"explicit basic on private", y(`agentService: {setup: basic}`), "basic"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cfgOf(t, c.yaml...).AgentSetup; got != c.want {
				t.Fatalf("setup = %s, want %s", got, c.want)
			}
		})
	}
	c := cfgOf(t, Public, `agentService: {setup: standard}`)
	if c.Network.AgentSubnet != "" || c.Cosmos == nil || c.Storage == nil {
		t.Fatalf("standard on a public network creates the dependencies but injects no subnet: %+v", c.Network)
	}
}

func TestIQCreatesSearchAndShowsTheResolvedDependency(t *testing.T) {
	c := cfgOf(t, Public, models, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`)
	if !implicit(c)["search:search"] {
		t.Fatal("implicit search")
	}
	if s := c.Scope("root").Search; s == nil || !s.Enabled {
		t.Fatal("root search must be enabled")
	}
	if c.Project("finance").SearchScope != "root" {
		t.Fatal("resolved Search scope")
	}
	explicit := cfgOf(t, Public, `search: {sku: basic}`, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`)
	if implicit(explicit)["search:search"] || explicit.Scope("root").Search.SKU != "basic" {
		t.Fatal("an explicit Search is reused")
	}
	declared := cfgOf(t, Public, `search: {}`)
	if declared.Project("finance").SearchScope != "root" {
		t.Fatal("declared search resolves without IQ")
	}
}

func TestBlobSourcesCreateStorageWithContainers(t *testing.T) {
	c := cfgOf(t, Public, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`)
	same(t, c.Storage.Purposes, []string{"knowledge"})
	var containers []string
	for _, ct := range c.Storage.Containers {
		containers = append(containers, ct.Name)
	}
	same(t, containers, []string{"knowledge", "policies"})
	if !implicit(c)["storage:storage"] {
		t.Fatal("implicit storage")
	}
}

func TestDeclaredStorageGainsRequiredPurposes(t *testing.T) {
	c := cfgOf(t, Public, `storage: {purposes: [documents]}`,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`,
		`evaluation: {enabled: true, datasets: [{name: golden, path: g.jsonl}]}`)
	same(t, c.Storage.Purposes, []string{"documents", "knowledge", "evaluations"})
	if !implicit(c)["storage-purpose:knowledge"] || implicit(c)["storage:storage"] {
		t.Fatalf("implicit = %v", implicit(c))
	}
	have := map[string]bool{}
	for _, ct := range c.Storage.Containers {
		have[ct.Name] = true
	}
	for _, want := range []string{"documents", "knowledge", "evaluations", "policies"} {
		if !have[want] {
			t.Fatalf("containers = %v", have)
		}
	}
}

func TestConnectorBackedSourcesDoNotNeedStorage(t *testing.T) {
	c := cfgOf(t, Public, `connectors: [{name: sp, type: sharepoint}]`,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: site, type: sharepoint, site: hr, connection: sp}]}]}`)
	if c.Storage != nil {
		t.Fatal("no storage needed")
	}
}

func TestRuntimeImpliesObservabilityAndRegistry(t *testing.T) {
	c := cfgOf(t, `runtime: {enabled: true, source: ./app}`)
	got := implicit(c)
	if !got["observability:observability"] || !got["container-registry:registry"] {
		t.Fatalf("implicit = %v", got)
	}
	if c.RegistrySKU != "Premium" {
		t.Fatalf("private mode needs a Premium registry, got %s", c.RegistrySKU)
	}
	registry := false
	for _, pe := range c.Network.PrivateEndpoints {
		registry = registry || pe.Component == "registry"
	}
	if !registry {
		t.Fatal("registry private endpoint")
	}
	image := cfgOf(t, `runtime: {enabled: true, image: "ghcr.io/x/y:1"}`)
	if implicit(image)["container-registry:registry"] || image.RegistrySKU != "" {
		t.Fatal("no registry for a prebuilt image")
	}
	if got := cfgOf(t, Public, `runtime: {enabled: true, source: ./app}`).RegistrySKU; got != "Standard" {
		t.Fatalf("public registry = %s", got)
	}
	if got := cfgOf(t, Public, `runtime: {enabled: true, source: ./app, registry: {sku: Basic}}`).RegistrySKU; got != "Basic" {
		t.Fatalf("explicit registry = %s", got)
	}
	two := cfgOf(t, Public, `runtime: {enabled: true, source: ./app, registry: {sku: Basic}}`,
		`projects: [{name: finance, runtime: {enabled: true, source: ./b, registry: {sku: Premium}}}]`)
	if two.RegistrySKU != "Premium" {
		t.Fatalf("the highest requested tier wins, got %s", two.RegistrySKU)
	}
}

func TestSecretReferencesImplyKeyVault(t *testing.T) {
	for name, doc := range map[string]string{
		"mcp":       `mcps: [{name: graph, endpoint: "https://a.example", authentication: {mode: apiKey, secretRef: graph-key}}]`,
		"runtime":   `runtime: {enabled: true, image: "i:1", secrets: {db: db-password}}`,
		"connector": `connectors: [{name: svc, type: api, authentication: {mode: apiKey, secretRef: svc-key}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			if c := cfgOf(t, Public, doc); c.KeyVault == nil || !implicit(c)["key-vault:key-vault"] {
				t.Fatal("expected an implicit Key Vault")
			}
		})
	}
	c := cfgOf(t, Public, `keyVault: {sku: premium}`, `runtime: {enabled: true, image: "i:1", secrets: {db: db-password}}`)
	if c.KeyVault.SKU != "premium" || implicit(c)["key-vault:key-vault"] {
		t.Fatal("an explicit Key Vault is not duplicated")
	}
}

func TestImplicitModelDeployments(t *testing.T) {
	c := cfgOf(t, Public, `models: {default: gpt-5, allowed: [gpt-5, gpt-4.1]}`)
	same(t, names(c.Scope("root").Models.Deployments), []string{"gpt-5", "gpt-4-1"})
	if c.Project("finance").Models.Default != "gpt-5" || !implicit(c)["model-deployment:gpt-5"] {
		t.Fatal("implicit deployments")
	}
}

func TestEmbeddingDeploymentIsReusedOrCreated(t *testing.T) {
	kb := `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`
	reuse := cfgOf(t, Public, `models: {deployments: [{name: emb, model: text-embedding-3-large}]}`, kb)
	if got := reuse.Scope("root").KnowledgeBases[0].Index.Vector.Deployment; got != "emb" || len(reuse.Scope("root").Models.Deployments) != 1 {
		t.Fatalf("deployment = %s", got)
	}
	created := cfgOf(t, Public, kb)
	same(t, names(created.Scope("root").Models.Deployments), []string{"text-embedding-3-large"})
	none := cfgOf(t, Public, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}], index: {vector: {enabled: false}}, retrieval: {mode: keyword}}]}`)
	if len(none.Scope("root").Models.Deployments) != 0 {
		t.Fatal("no embedding deployment without vectors")
	}
}

func TestIndexIsMaterialised(t *testing.T) {
	c := cfgOf(t, Public, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`)
	idx := c.Scope("root").KnowledgeBases[0].Index
	if idx.Name != "policies" {
		t.Fatal(idx.Name)
	}
	fields := map[string]config.IndexField{}
	for _, f := range idx.Fields {
		fields[f.Name] = f
	}
	if len(fields) != 4 || !fields["id"].Key || !fields["content"].Searchable {
		t.Fatalf("fields = %v", fields)
	}
	if v := fields["contentVector"]; v.Dimensions != 3072 || v.VectorProfile != "default-vector-profile" || v.Retrievable {
		t.Fatalf("vector = %+v", v)
	}
	if idx.Semantic.TitleField != "title" || len(idx.Semantic.ContentFields) != 1 || idx.Semantic.ContentFields[0] != "content" {
		t.Fatalf("semantic = %+v", idx.Semantic)
	}
}

func TestSemanticFieldsFollowCustomIndexFields(t *testing.T) {
	c := cfgOf(t, Public, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}], retrieval: {mode: keyword}, index: {keyField: docId, contentField: body, vector: {enabled: false}, fields: [{name: docId, type: Edm.String, key: true}, {name: body, type: Edm.String, searchable: true}]}}]}`)
	idx := c.Scope("root").KnowledgeBases[0].Index
	if idx.Semantic.ContentFields[0] != "body" {
		t.Fatalf("semantic = %+v", idx.Semantic)
	}
	for _, f := range idx.Fields {
		if f.Name == "id" {
			t.Fatal("the default id field must not be added when keyField is custom")
		}
	}
}

func TestRedisDefaults(t *testing.T) {
	if got := cfgOf(t, `redis: {enabled: true}`).Redis.SKU; got != "balanced" {
		t.Fatal(got)
	}
}

func TestEventsTierFollowsNetworkMode(t *testing.T) {
	if got := cfgOf(t, `events: {enabled: true}`).Events.SKU; got != "Premium" {
		t.Fatalf("private = %s", got)
	}
	if got := cfgOf(t, Public, `events: {enabled: true}`).Events.SKU; got != "Standard" {
		t.Fatalf("public = %s", got)
	}
	if got := cfgOf(t, Public, `events: {enabled: true, sku: Basic}`).Events.SKU; got != "Basic" {
		t.Fatalf("explicit = %s", got)
	}
}

func TestContainerMemoryFollowsCPUUnlessSet(t *testing.T) {
	c := cfgOf(t, `runtime: {enabled: true, image: "i:1", resources: {cpu: 2}}`)
	if got := c.Scope("root").Runtime.Resources.Memory; got != "4Gi" {
		t.Fatal(got)
	}
	agent := cfgOf(t, models, `agents: [{name: bot, kind: hosted, runtime: {enabled: true, image: "i:1", resources: {cpu: 0.5}}}]`)
	if got := agent.Scope("root").Agents[0].Runtime.Resources.Memory; got != "1Gi" {
		t.Fatal(got)
	}
}

func TestPrivateModeDerivesPrivateConnectivity(t *testing.T) {
	c := cfgOf(t, `storage: {hierarchicalNamespace: true}`, `keyVault: {}`, `redis: {enabled: true}`, `events: {enabled: true}`, `search: {replicas: 2}`, `observability: {}`)
	n := c.Network
	if n.Mode != "private" || n.VNet != "create" || !n.PrivateDNS {
		t.Fatalf("network = %+v", n)
	}
	got := map[string]bool{}
	for _, pe := range n.PrivateEndpoints {
		got[pe.Component+"/"+pe.Group] = true
	}
	for _, want := range []string{
		"foundry/account", "search:root/searchService", "storage/blob", "storage/dfs", "key-vault/vault",
		"redis/redisCache", "events/namespace", "cosmos/Sql",
	} {
		if !got[want] {
			t.Fatalf("missing private endpoint %s in %v", want, got)
		}
	}
	zones := map[string]bool{}
	for _, z := range n.PrivateDNSZones {
		zones[z] = true
	}
	for _, z := range []string{"privatelink.search.windows.net", "privatelink.dfs.core.windows.net", "privatelink.documents.azure.com", "privatelink.services.ai.azure.com"} {
		if !zones[z] {
			t.Fatalf("missing zone %s in %v", z, n.PrivateDNSZones)
		}
	}
}

func TestPrivateModeWithExistingNetwork(t *testing.T) {
	vnet := arm + "/Microsoft.Network/virtualNetworks/vnet1"
	c := cfgOf(t, fmt.Sprintf(`security: {roles: {admins: [a]}, network: {mode: private, existingVnetResourceId: %q, existingPrivateEndpointSubnetResourceId: %q, existingAgentSubnetResourceId: %q}}`,
		vnet, vnet+"/subnets/pe", vnet+"/subnets/agents"))
	n := c.Network
	if n.VNet != "existing" || n.ExistingVnetResourceID != vnet || n.AgentSubnet != "existing" ||
		n.AgentSubnetResourceID != vnet+"/subnets/agents" || n.PrivateEndpointSubnetResourceID != vnet+"/subnets/pe" || n.AddressSpace != "" {
		t.Fatalf("network = %+v", n)
	}
	custom := cfgOf(t, `security: {roles: {admins: [a]}, network: {mode: private, addressSpace: 192.168.0.0/20, agentSubnetPrefixLength: 26}}`)
	if custom.Network.AddressSpace != "192.168.0.0/20" || custom.Network.AgentSubnetPrefixLength != 26 {
		t.Fatalf("network = %+v", custom.Network)
	}
	nodns := cfgOf(t, `security: {roles: {admins: [a]}, network: {mode: private, privateDns: false}}`)
	if len(nodns.Network.PrivateDNSZones) != 0 || len(nodns.Network.PrivateEndpoints) == 0 {
		t.Fatalf("network = %+v", nodns.Network)
	}
}

func TestNonPrivateModesHaveNoPrivateEndpoints(t *testing.T) {
	pub := cfgOf(t, Public)
	if pub.Network.VNet != "" || len(pub.Network.PrivateEndpoints) != 0 {
		t.Fatalf("public network = %+v", pub.Network)
	}
	restricted := cfgOf(t, `security: {network: {mode: restricted, allowedIps: [203.0.113.0/24]}, roles: {admins: [a]}}`)
	if len(restricted.Network.AllowedIPs) != 1 || len(restricted.Network.PrivateEndpoints) != 0 {
		t.Fatalf("restricted network = %+v", restricted.Network)
	}
}

// Hub and inheritance -------------------------------------------------------------------------

func TestSpokesInheritHubResourcesAndOverrideByName(t *testing.T) {
	tool := func(ref string) string { return fmt.Sprintf(`tools: [{name: t1, type: function, reference: %s}]`, ref) }
	c := hubCfg(t,
		fmt.Sprintf(`hub: {name: shared, toolboxes: [{name: tb, %s}], mcps: [{name: graph, endpoint: "https://hub.example"}]}`, tool("hub")),
		fmt.Sprintf(`projects: [{name: aa, toolboxes: [{name: tb, %s}, {name: extra, %s}]}, {name: bb}, {name: cc, inheritHub: false}]`, tool("spoke"), tool("x")),
		Public)
	aa, bb, cc := c.Project("aa"), c.Project("bb"), c.Project("cc")
	same(t, names(aa.Toolboxes), []string{"tb", "extra"})
	if aa.Toolboxes[0].Tools[0].Reference != "spoke" || aa.Origins["toolbox:tb"] != "project:aa" || bb.Origins["toolbox:tb"] != "hub" {
		t.Fatal("override by name")
	}
	if bb.Toolboxes[0].Tools[0].Reference != "hub" {
		t.Fatal("inherited")
	}
	same(t, names(bb.Mcps), []string{"graph"})
	if len(cc.Toolboxes) != 0 || len(cc.Mcps) != 0 || cc.InheritsHub {
		t.Fatal("inheritHub false")
	}
}

func TestInheritanceFlagsSwitchOffResourceKinds(t *testing.T) {
	c := hubCfg(t, `hub: {name: shared, inheritance: {toolboxes: false, mcps: false, models: false}, toolboxes: [{name: tb, tools: [{name: t1, type: function, reference: f}]}], mcps: [{name: graph, endpoint: "https://hub.example"}], models: {default: gpt-5, allowed: [gpt-5]}}`, Public)
	p := c.Project("finance")
	if len(p.Toolboxes) != 0 || len(p.Mcps) != 0 || p.Models.Default != "" {
		t.Fatalf("project = %+v", p)
	}
}

func TestHubIQIsNotInheritedByDefaultButCanBe(t *testing.T) {
	hubIQ := `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`
	c := hubCfg(t, Public, `hub: {name: shared, `+hubIQ+`}`)
	if len(c.Project("finance").KnowledgeBases) != 0 {
		t.Fatal("hub IQ is private to the hub by default")
	}
	c = hubCfg(t, Public, `hub: {name: shared, inheritance: {iq: true}, `+hubIQ+`}`)
	p := c.Project("finance")
	same(t, names(p.KnowledgeBases), []string{"policies"})
	if p.Origins["knowledgeBase:policies"] != "hub" || p.SearchScope != "root" || c.Hub.SearchScope != "root" {
		t.Fatalf("project = %+v hub = %+v", p, c.Hub)
	}
}

func TestSearchResolutionPrecedence(t *testing.T) {
	c := hubCfg(t, Public, `hub: {name: shared, search: {sku: standard2}}`, `search: {sku: basic}`,
		`projects: [{name: aa}, {name: bb, search: {sku: basic}}, {name: cc, inheritHub: false}]`)
	for project, want := range map[string]string{"aa": "hub", "bb": "project:bb", "cc": "root"} {
		if got := c.Project(project).SearchScope; got != want {
			t.Fatalf("%s resolves %s, want %s", project, got, want)
		}
	}
	if c.Hub.SearchScope != "hub" {
		t.Fatal(c.Hub.SearchScope)
	}
	noInherit := hubCfg(t, Public, `hub: {name: shared, search: {}, inheritance: {search: false}}`, `search: {sku: basic}`)
	if got := noInherit.Project("finance").SearchScope; got != "root" {
		t.Fatal(got)
	}
}

func TestModelsMergeAcrossLevels(t *testing.T) {
	c := hubCfg(t, Public, `hub: {name: shared, models: {default: gpt-5, allowed: [gpt-5, gpt-5-mini], denied: [gpt-3]}}`, `models: {denied: [gpt-2]}`,
		`projects: [{name: fin, models: {default: gpt-5-mini, allowed: [gpt-5-mini], denied: [gpt-1]}}]`)
	m := c.Project("fin").Models
	if m.Default != "gpt-5-mini" {
		t.Fatal(m.Default)
	}
	same(t, m.Allowed, []string{"gpt-5-mini"})
	same(t, m.Denied, []string{"gpt-2", "gpt-3", "gpt-1"})
	var served []string
	for _, d := range m.Deployments {
		served = append(served, d.Model)
	}
	same(t, served, []string{"gpt-5", "gpt-5-mini"})
}

func TestProjectDeploymentOverridesInherited(t *testing.T) {
	c := hubCfg(t, Public, `hub: {name: shared, models: {deployments: [{name: chat, model: gpt-5, capacity: 50}]}}`,
		`projects: [{name: fin, models: {deployments: [{name: chat, model: gpt-5, capacity: 5}]}}]`)
	d := c.Project("fin").Models.Deployments
	if len(d) != 1 || d[0].Capacity != 5 {
		t.Fatalf("deployments = %+v", d)
	}
}

func TestRootItemsAssignedToAProjectMoveIntoIt(t *testing.T) {
	c := cfgOf(t, Public, models, `projects: [{name: aa}, {name: bb}]`,
		`agents: [{name: shared}, {name: only-bb, project: bb}]`,
		`iq: {project: aa, knowledgeBases: [{name: kb-aa, sources: [{name: s1, type: web, url: "https://x.example"}]}, {name: kb-bb, project: bb, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`,
		`runtime: {enabled: true, image: "i:1", project: bb}`, `evaluation: {enabled: true, project: aa}`)
	aa, bb := c.Project("aa"), c.Project("bb")
	same(t, names(aa.Agents), []string{"shared"})
	same(t, names(bb.Agents), []string{"shared", "only-bb"})
	same(t, names(aa.KnowledgeBases), []string{"kb-aa"})
	same(t, names(bb.KnowledgeBases), []string{"kb-bb"})
	if aa.Runtime != nil || bb.RuntimeScope != "project:bb" || !aa.Evaluation.Enabled || bb.Evaluation != nil {
		t.Fatal("runtime/evaluation assignment")
	}
}

func TestRootRuntimeAppliesToEveryProjectAndProjectRuntimeWins(t *testing.T) {
	c := cfgOf(t, Public, `projects: [{name: aa}, {name: bb, runtime: {enabled: true, image: "mine:1"}}]`, `runtime: {enabled: true, image: "shared:1"}`)
	if a := c.Project("aa"); a.Runtime.Image != "shared:1" || a.RuntimeScope != "root" {
		t.Fatalf("aa = %+v", a.Runtime)
	}
	if b := c.Project("bb"); b.Runtime.Image != "mine:1" || b.RuntimeScope != "project:bb" {
		t.Fatalf("bb = %+v", b.Runtime)
	}
}

func TestDisabledIQContributesNothing(t *testing.T) {
	c := cfgOf(t, Public, `iq: {enabled: false, knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`)
	if len(c.Project("finance").KnowledgeBases) != 0 || c.Scope("root").Search != nil {
		t.Fatal("disabled IQ")
	}
}

func TestProjectGatewayPathDefaultsToProjectName(t *testing.T) {
	c := cfgOf(t, models, `observability: {}`, `gateway: {enabled: true}`, `projects: [{name: fin, gateway: {}}]`)
	if got := c.Project("fin").Gateway.Path; got != "/fin" {
		t.Fatal(got)
	}
}

func TestManagedIdentityTypeAndGraphIdentity(t *testing.T) {
	c := cfgOf(t, `managedIdentity: {type: systemAssigned}`)
	if c.ManagedIdentity.Type != "systemAssigned" || implicit(c)["managed-identity:identity"] {
		t.Fatal("explicit identity config")
	}
}

func TestNormaliseDoesNotMutateTheParsedConfig(t *testing.T) {
	parsed, err := parser.ParseMapping(Doc(t, models, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`), "<test>")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(parsed.Config)
	normalise.Normalise(parsed.Config)
	if after, _ := json.Marshal(parsed.Config); string(after) != string(before) {
		t.Fatal("Normalise mutated its input")
	}
}
