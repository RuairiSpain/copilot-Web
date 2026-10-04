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
	if p.Tags["project"] != "finance" {
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

func TestPrivateModeUsesTheStandardAgentSetup(t *testing.T) {
	c := cfgOf(t, Private)
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
	if c.Storage != nil || c.Cosmos != nil || c.KeyVault != nil || c.Gateway != nil || c.Observability != nil {
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
		{"default is public", y(), "basic"},
		{"private auto", y(Private), "standard"},
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
	c := cfgOf(t, Public,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`,
		`storage: {purposes: [documents, evaluations]}`)
	same(t, c.Storage.Purposes, []string{"documents", "evaluations", "knowledge"})
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
	c := cfgOf(t, Public,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: site, type: sharepoint, site: hr, connection: sp}]}]}`)
	if c.Storage != nil {
		t.Fatal("no storage needed")
	}
}

func TestEmbeddingDeploymentDefaultsToTheModelName(t *testing.T) {
	kb := `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`
	if got := cfgOf(t, Public, kb).Scope("root").KnowledgeBases[0].Index.Vector.Deployment; got != "text-embedding-3-large" {
		t.Fatalf("deployment = %s", got)
	}
	named := cfgOf(t, Public, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}], index: {vector: {deployment: emb}}}]}`)
	if got := named.Scope("root").KnowledgeBases[0].Index.Vector.Deployment; got != "emb" {
		t.Fatalf("an explicit deployment name wins, got %s", got)
	}
	none := cfgOf(t, Public, `iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}], index: {vector: {enabled: false}}, retrieval: {mode: keyword}}]}`)
	if got := none.Scope("root").KnowledgeBases[0].Index.Vector.Deployment; got != "" {
		t.Fatalf("no embedding deployment without vectors, got %s", got)
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

func TestPrivateModeDerivesPrivateConnectivity(t *testing.T) {
	c := cfgOf(t, Private, `storage: {hierarchicalNamespace: true}`, `keyVault: {}`, `search: {replicas: 2}`, `observability: {}`)
	n := c.Network
	if n.Mode != "private" || n.VNet != "create" || !n.PrivateDNS {
		t.Fatalf("network = %+v", n)
	}
	got := map[string]bool{}
	for _, pe := range n.PrivateEndpoints {
		got[pe.Component+"/"+pe.Group] = true
	}
	for _, want := range []string{
		"search:root/searchService", "storage/blob", "storage/dfs", "key-vault/vault", "cosmos/Sql",
	} {
		if !got[want] {
			t.Fatalf("missing private endpoint %s in %v", want, got)
		}
	}
	if got["foundry/account"] {
		t.Fatal("the Foundry resource's private endpoint is created by azd")
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

func TestSpokesInheritHubKnowledgeBasesAndOverrideByName(t *testing.T) {
	src := `sources: [{name: files, type: blob, container: policies}]`
	c := hubCfg(t, Public,
		`hub: {name: shared, inheritance: {iq: true}, iq: {knowledgeBases: [{name: policies, description: hub, `+src+`}, {name: shared-kb, `+src+`}]}}`,
		`projects: [{name: aa, iq: {knowledgeBases: [{name: policies, description: spoke, `+src+`}]}}, {name: bb}, {name: cc, inheritHub: false}]`)
	aa, bb, cc := c.Project("aa"), c.Project("bb"), c.Project("cc")
	same(t, names(aa.KnowledgeBases), []string{"policies", "shared-kb"})
	if aa.KnowledgeBases[0].Description != "spoke" || aa.Origins["knowledgeBase:policies"] != "project:aa" || bb.Origins["knowledgeBase:policies"] != "hub" {
		t.Fatal("override by name")
	}
	if bb.KnowledgeBases[0].Description != "hub" {
		t.Fatal("inherited")
	}
	if len(cc.KnowledgeBases) != 0 || cc.InheritsHub {
		t.Fatal("inheritHub false")
	}
}

func TestInheritanceFlagsSwitchOffResourceKinds(t *testing.T) {
	c := hubCfg(t, `hub: {name: shared, inheritance: {models: false}, models: {default: gpt-5, allowed: [gpt-5]}}`, Public)
	if p := c.Project("finance"); p.Models.Default != "" {
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
}

func TestRootItemsAssignedToAProjectMoveIntoIt(t *testing.T) {
	c := cfgOf(t, Public, models, `projects: [{name: aa}, {name: bb}]`,
		`iq: {project: aa, knowledgeBases: [{name: kb-aa, sources: [{name: s1, type: web, url: "https://x.example"}]}, {name: kb-bb, project: bb, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`)
	aa, bb := c.Project("aa"), c.Project("bb")
	same(t, names(aa.KnowledgeBases), []string{"kb-aa"})
	same(t, names(bb.KnowledgeBases), []string{"kb-bb"})
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

func TestNetworkModeDefaultsToPublicUnlessAVNetIsSpecified(t *testing.T) {
	vnet := arm + "/Microsoft.Network/virtualNetworks/v1"
	subnet := vnet + "/subnets/pe"
	cases := []struct {
		name, network, mode, source string
	}{
		{"nothing specified", ``, "public", "default"},
		{"empty network block", `security: {network: {}, roles: {admins: [a]}}`, "public", "default"},
		{"explicit public", `security: {network: {mode: public}, roles: {admins: [a]}}`, "public", "explicit"},
		{"explicit private", `security: {network: {mode: private}, roles: {admins: [a]}}`, "private", "explicit"},
		{"address space", `security: {network: {addressSpace: 10.30.0.0/16}, roles: {admins: [a]}}`, "private", "vnet-settings"},
		{"agent subnet size", `security: {network: {agentSubnetPrefixLength: 25}, roles: {admins: [a]}}`, "private", "vnet-settings"},
		{"existing vnet", fmt.Sprintf(`security: {network: {existingVnetResourceId: %q, existingPrivateEndpointSubnetResourceId: %q, existingAgentSubnetResourceId: %q}, roles: {admins: [a]}}`, vnet, subnet, vnet+"/subnets/agents"), "private", "vnet-settings"},
		{"restricted", `security: {network: {mode: restricted, allowedIps: [203.0.113.0/24]}, roles: {admins: [a]}}`, "restricted", "explicit"},
		{"only allowed IPs", `security: {network: {allowedIps: [203.0.113.0/24]}, roles: {admins: [a]}}`, "public", "default"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var yaml []string
			if c.network != "" {
				yaml = y(c.network)
			}
			n := cfgOf(t, yaml...).Network
			if n.Mode != c.mode || n.ModeSource != c.source {
				t.Fatalf("mode = %s (%s), want %s (%s)", n.Mode, n.ModeSource, c.mode, c.source)
			}
		})
	}
}

func TestDefaultPublicDeploymentCreatesNoVNetAndUsesBasicSetup(t *testing.T) {
	c := cfgOf(t)
	if c.Network.VNet != "" || c.Network.AddressSpace != "" || len(c.Network.PrivateEndpoints) != 0 || c.AgentSetup != "basic" {
		t.Fatalf("network = %+v setup = %s", c.Network, c.AgentSetup)
	}
	for _, id := range MustPlan(t).Order() {
		if id == "network" || id == "private-dns" || id == "cosmos" {
			t.Fatalf("a default deployment must not create %s", id)
		}
	}
	local := cfgOf(t)
	if local.LocalAuthentication {
		t.Fatal("public mode is still Entra ID only by default")
	}
}
