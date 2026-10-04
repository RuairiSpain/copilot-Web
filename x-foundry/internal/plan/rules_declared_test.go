package plan_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
)

const arm = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers"

type rule struct {
	name, code, path, message string
	yaml                      []string
}

func runRules(t *testing.T, cases []rule) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { ExpectCase(t, c.code, c.path, c.message, c.yaml...) })
	}
}

func y(parts ...string) []string { return parts }

func TestMinimalIsValidWithNoDiagnostics(t *testing.T) {
	OK(t, Run(t))
}

func TestRule1Projects(t *testing.T) {
	runRules(t, []rule{
		{"duplicate projects", "XF001", "projects", "used more than once", y(`projects: [{name: aa}, {name: aa}]`)},
		{"hub name collides", "XF001", "hub.name", "collides", y("topology: {mode: hub-spoke}\nhub: {name: shared}\nprojects: [{name: shared}]")},
	})
}

func TestRule2DuplicateNames(t *testing.T) {
	runRules(t, []rule{
		{"knowledge bases", "XF002", "knowledgeBases", "knowledge base", y(`iq: {knowledgeBases: [{name: one, sources: [{name: s1, type: web, url: "https://x.example"}]}, {name: one, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`)},
		{"project knowledge bases", "XF002", "projects[fin]", "knowledge base", y(`projects: [{name: fin, iq: {knowledgeBases: [{name: one, sources: [{name: s1, type: web, url: "https://x.example"}]}, {name: one, sources: [{name: s1, type: web, url: "https://x.example"}]}]}}]`)},
		{"sources", "XF002", "sources", "knowledge source", y(`iq: {knowledgeBases: [{name: one, sources: [{name: s1, type: web, url: "https://x.example"}, {name: s1, type: web, url: "https://x.example"}]}]}`)},
		{"endpoints", "XF002", "endpoints", "gateway endpoint", y(`gateway: {enabled: true, endpoints: [{name: ep1, path: /a, target: x, targetType: agent}, {name: ep1, path: /b, target: x, targetType: agent}]}`)},
		{"containers", "XF002", "containers", "storage container", y(`storage: {containers: [{name: abc}, {name: abc}]}`)},
	})
}

func TestRule2SameNameInDifferentScopesIsAnOverride(t *testing.T) {
	kb := func(src string) string {
		return fmt.Sprintf(`iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: %q}]}]}`, src)
	}
	a := RunHub(t,
		"hub: {name: shared, "+strings.TrimPrefix(kb("https://a.example"), "")+"}",
		"projects: [{name: fin, "+kb("https://b.example")+"}]")
	MustOK(t, a)
}

func TestRule3And4(t *testing.T) {
	// The schema rejects a hub in standalone mode, and a missing hub in hub-spoke mode.
	if !Codes(Run(t, `hub: {name: shared}`), "")["XF102"] {
		t.Fatal("expected a schema error for hub in standalone mode")
	}
	if !Codes(Run(t, `topology: {mode: hub-spoke}`), "")["XF102"] {
		t.Fatal("expected a schema error for hub-spoke without a hub")
	}
	ExpectCase(t, "XF004", "inheritHub", "no hub", `projects: [{name: fin, inheritHub: true}]`)
	MustOK(t, Run(t, `projects: [{name: fin, inheritHub: false}]`))
}

func TestRule5UnknownProjects(t *testing.T) {
	cases := map[string]string{
		"iq":       `iq: {project: nope, knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`,
		"kb":       `iq: {knowledgeBases: [{name: kb1, project: nope, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`,
		"endpoint": `gateway: {enabled: true, endpoints: [{name: ep1, path: /a, target: x, targetType: agent, project: nope}]}`,
		"route":    `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}], routing: {routes: [{name: r1, when: {project: nope}, knowledgeBase: kb1}]}}]}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) { ExpectCase(t, "XF005", "", "unknown project 'nope'", doc) })
	}
}

func TestRule16Overlap(t *testing.T) {
	runRules(t, []rule{
		{"root", "XF016", "x-foundry.models", "both allowed and denied", y(`models: {allowed: [gpt-5], denied: [gpt-5]}`)},
		{"project", "XF016", "projects[aa]", "", y(`projects: [{name: aa, models: {allowed: [a1], denied: [a1]}}]`)},
		{"gateway", "XF016", "gateway.models", "", y(`gateway: {enabled: true, models: {allowed: [gpt-5], denied: [gpt-5]}}`)},
		{"policy", "XF016", "modelPolicy", "", y(`governance: {modelPolicy: {allowedModels: [gpt-5], deniedModels: [gpt-5]}}`)},
		{"hub", "XF016", "hub.models", "", y("topology: {mode: hub-spoke}\nhub: {name: shared, models: {allowed: [gpt-5], denied: [gpt-5]}}")},
	})
}

func TestRule17Secrets(t *testing.T) {
	for _, value := range []string{
		"AccountKey=abc123==;EndpointSuffix=core.windows.net",
		"https://acct.blob.core.windows.net/c?sv=1&sig=AbCdEfGhIjKlMnOpQrStUvWxYz0123456789",
		"-----BEGIN RSA PRIVATE KEY-----",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"sk-abcdefghijklmnopqrstuvwxyz123456",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"Server=db;Password=hunter2",
		"https://user:pass@example.com/path",
	} {
		t.Run(value[:12], func(t *testing.T) {
			ExpectCase(t, "XF017", "description", "appears to contain", fmt.Sprintf(`iq: {knowledgeBases: [{name: kb1, description: %q, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`, value))
		})
	}
}

func TestRule22ExistingResources(t *testing.T) {
	existing := func(typ string) string { return fmt.Sprintf("%s/%s/x1", arm, typ) }
	for _, c := range []struct{ component, typ, extra, key string }{
		{"search", "Microsoft.Search/searchServices", "sku: basic", "sku"},
		{"search", "Microsoft.Search/searchServices", "replicas: 2", "replicas"},
		{"storage", "Microsoft.Storage/storageAccounts", "sku: Standard_LRS", "sku"},
		{"keyVault", "Microsoft.KeyVault/vaults", "sku: premium", "sku"},
		{"cosmos", "Microsoft.DocumentDB/databaseAccounts", "throughput: 4000", "throughput"},
		{"managedIdentity", "Microsoft.ManagedIdentity/userAssignedIdentities", "tags: {a: b}", "tags"},
	} {
		t.Run(c.component+"/"+c.key, func(t *testing.T) {
			ExpectCase(t, "XF022", c.key, "", fmt.Sprintf("%s: {existingResourceId: %q, %s}", c.component, existing(c.typ), c.extra))
		})
	}
	for component, typ := range map[string]string{
		"search": "Microsoft.Search/searchServices", "storage": "Microsoft.Storage/storageAccounts", "keyVault": "Microsoft.KeyVault/vaults",
		"cosmos": "Microsoft.DocumentDB/databaseAccounts", "managedIdentity": "Microsoft.ManagedIdentity/userAssignedIdentities",
	} {
		t.Run("valid "+component, func(t *testing.T) {
			MustOK(t, Run(t, fmt.Sprintf("%s: {existingResourceId: %q}", component, existing(typ))))
		})
	}
	ExpectCase(t, "XF022", "existingResourceId", "", fmt.Sprintf("storage: {existingResourceId: %q}", existing("Microsoft.KeyVault/vaults")))
	if !Codes(Run(t, fmt.Sprintf("storage: {name: stacct, existingResourceId: %q}", existing("Microsoft.Storage/storageAccounts"))), "")["XF102"] {
		t.Fatal("name and existingResourceId must be mutually exclusive")
	}
	ExpectCase(t, "XF022", "projects[fin].search", "", fmt.Sprintf(`projects: [{name: fin, search: {existingResourceId: %q}}]`, existing("Microsoft.Storage/storageAccounts")))
}

func TestRule23Regions(t *testing.T) {
	runRules(t, []rule{
		{"defaults", "XF023", "defaults.location", "", y(`defaults: {location: atlantis}`)},
		{"residency", "XF023", "", "outside governance.dataResidency", y(`defaults: {location: eastus}`, `governance: {dataResidency: [westeurope, northeurope]}`)},
		{"residency region", "XF023", "dataResidency", "", y(`governance: {dataResidency: [atlantis]}`)},
	})
	MustOK(t, Run(t, `defaults: {location: "West Europe"}`))
	MustOK(t, Run(t, `defaults: {location: westeurope}`, `governance: {dataResidency: [westeurope]}`))
}

func TestRule24ExplicitNames(t *testing.T) {
	runRules(t, []rule{
		{"storage case", "XF024", "storage.name", "", y(`storage: {name: My-Storage}`)},
		{"storage short", "XF024", "storage.name", "", y(`storage: {name: ab}`)},
		{"key vault hyphens", "XF024", "keyVault.name", "", y(`keyVault: {name: kv--double}`)},
		{"search", "XF024", "search.name", "", y(`search: {name: Search1}`)},
		{"cosmos", "XF024", "cosmos.name", "", y(`cosmos: {name: Cosmos1}`)},
		{"apim", "XF024", "gateway.name", "", y(`gateway: {enabled: true, name: 1apim}`)},
		{"identity", "XF024", "managedIdentity.name", "", y(`managedIdentity: {name: ab}`)},
		{"resource group", "XF024", "defaults.resourceGroup", "", y(`defaults: {resourceGroup: "rg."}`)},
		{"source container", "XF024", "container", "", y(`iq: {knowledgeBases: [{name: kb1, sources: [{name: files, type: blob, container: Policies}]}]}`)},
		{"storage container", "XF024", "containers[a--b]", "", y(`storage: {containers: [{name: a--b}]}`)},
	})
	MustOK(t, Run(t, `storage: {name: stfinance01}`, `keyVault: {name: kv-finance}`, `search: {name: srch-finance}`,
		`managedIdentity: {name: id-finance}`, `defaults: {resourceGroup: rg-finance}`,
		`cosmos: {name: cosmos-finance}`))
}

func TestSecurityRules(t *testing.T) {
	a := Run(t, `security: {roles: {admins: []}}`)
	if !a.OK() || !Codes(a, diag.Warning)["XF114"] {
		t.Fatalf("empty admins should warn:\n%s", Lines(a.Diagnostics))
	}
	ExpectCase(t, "XF105", "", "", `security: {network: {mode: restricted}, roles: {admins: [a]}}`)
	MustOK(t, Run(t, `security: {network: {mode: restricted, allowedIps: [203.0.113.0/24]}, roles: {admins: [a]}}`))

	ExpectCase(t, "XF021", "security.publicNetworkAccess", "", `security: {network: {mode: private}, roles: {admins: [a]}, publicNetworkAccess: true}`)
	ExpectCase(t, "XF021", "storage.publicNetworkAccess", "", Private, `storage: {publicNetworkAccess: true}`)
	ExpectCase(t, "XF106", "search.localAuthentication", "", `search: {localAuthentication: true}`)
	MustOK(t, Run(t, `security: {roles: {admins: [a]}, localAuthentication: true}`, `search: {localAuthentication: true}`))
	ExpectCase(t, "XF021", "managedIdentity.enabled", "", Private, `managedIdentity: {enabled: false}`)
	a = Run(t, `security: {network: {mode: private, privateDns: false}, roles: {admins: [a]}}`)
	if !a.OK() || !Codes(a, diag.Warning)["XF021"] {
		t.Fatalf("privateDns false should warn:\n%s", Lines(a.Diagnostics))
	}
}

func TestHardeningRules(t *testing.T) {
	restricted := func(ips ...string) string {
		return fmt.Sprintf(`security: {network: {mode: restricted, allowedIps: [%s]}, roles: {admins: [a]}}`, strings.Join(ips, ", "))
	}
	for _, v := range []string{"10.0.0.0/8", "172.20.1.0/24", "192.168.1.5", "100.64.0.0/10", `"fd00::/8"`} {
		ExpectCase(t, "XF121", "allowedIps", "private range", restricted(v))
	}
	for _, v := range []string{"0.0.0.0/0", `"::/0"`} {
		ExpectCase(t, "XF121", "", "whole internet", restricted(v))
		ExpectCase(t, "XF121", "allowIps", "", fmt.Sprintf(`gateway: {enabled: true, security: {allowIps: [%s]}}`, v), `observability: {}`)
	}
	MustOK(t, Run(t, restricted("203.0.113.0/24", "198.51.100.7")))
	MustOK(t, Run(t, `security: {network: {mode: private, allowedIps: [10.0.0.0/8]}, roles: {admins: [a]}}`))

	for name, doc := range map[string]string{
		"web": `iq: {knowledgeBases: [{name: kb1, sources: [{name: web1, type: web, url: "http://x.example"}]}]}`,
	} {
		t.Run(name, func(t *testing.T) { ExpectCase(t, "XF122", "", "", doc) })
	}

	for _, c := range []struct{ search, message string }{
		{`{sku: free, replicas: 2}`, "at most 1 replica"},
		{`{sku: basic, partitions: 2}`, "at most 1 partition"},
		{`{sku: basic, replicas: 4}`, "at most 3 replica"},
		{`{sku: standard, replicas: 12, partitions: 4}`, "36 search units"},
		{`{sku: free}`, "private endpoints"},
	} {
		ExpectCase(t, "XF123", "", c.message, Private, "search: "+c.search)
	}
	MustOK(t, Run(t, `search: {sku: basic, replicas: 3}`))
	MustOK(t, Run(t, `search: {sku: storage_optimized_l1, replicas: 3, partitions: 12}`))
	OK(t, Run(t, `search: {sku: free}`, Public))
	if !Codes(Run(t, `search: {sku: storage_optimised_l1}`), "")["XF102"] {
		t.Fatal("the British spelling is not a valid SKU")
	}

	ExpectCase(t, "XF124", "gateway.sku", "", Private, `gateway: {enabled: true, sku: Consumption}`, `observability: {}`)
	basic := Run(t, Private, `gateway: {enabled: true, sku: BasicV2}`, `observability: {}`)
	if !basic.OK() || !Codes(basic, diag.Warning)["XF124"] {
		t.Fatalf("BasicV2 should warn:\n%s", Lines(basic.Diagnostics))
	}
	OK(t, Run(t, Private, `gateway: {enabled: true, sku: PremiumV2}`, `observability: {}`))
	OK(t, Run(t, `gateway: {enabled: true, sku: Consumption}`, `observability: {}`, Public))
}

func TestCronAndAgentRules(t *testing.T) {
	ExpectCase(t, "XF118", "refresh.schedule", "", `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}], refresh: {schedule: daily}}]}`)
}

func TestNetworkAddressing(t *testing.T) {
	vnet := arm + "/Microsoft.Network/virtualNetworks/v1"
	subnet := func(name string) string { return vnet + "/subnets/" + name }
	net := func(body string) string {
		return fmt.Sprintf(`security: {network: {%s}, roles: {admins: [a]}}`, body)
	}
	ExpectCase(t, "XF127", "addressSpace", "private", net(`addressSpace: 8.8.0.0/16`))
	ExpectCase(t, "XF127", "agentSubnetPrefixLength", "no room", net(`addressSpace: 10.0.0.0/22, agentSubnetPrefixLength: 22`))
	ExpectCase(t, "XF127", "existingAgentSubnetResourceId", "requires existingVnetResourceId", net(fmt.Sprintf(`existingAgentSubnetResourceId: %q`, subnet("agents"))))
	ExpectCase(t, "XF127", "addressSpace", "generated VNet", net(fmt.Sprintf(`existingVnetResourceId: %q, addressSpace: 10.0.0.0/16`, vnet)))
	ExpectCase(t, "XF127", "agentSubnetPrefixLength", "generated VNet", net(fmt.Sprintf(`existingVnetResourceId: %q, agentSubnetPrefixLength: 24`, vnet)))
	ExpectCase(t, "XF127", "existingPrivateEndpointSubnetResourceId", "needs a subnet", net(fmt.Sprintf(`existingVnetResourceId: %q, existingAgentSubnetResourceId: %q`, vnet, subnet("agents"))))
	ExpectCase(t, "XF127", "existingAgentSubnetResourceId", "delegated", net(fmt.Sprintf(`existingVnetResourceId: %q, existingPrivateEndpointSubnetResourceId: %q`, vnet, subnet("pe"))))
	other := arm + "/Microsoft.Network/virtualNetworks/other/subnets/pe"
	ExpectCase(t, "XF127", "existingPrivateEndpointSubnetResourceId", "belong", net(fmt.Sprintf(`existingVnetResourceId: %q, existingPrivateEndpointSubnetResourceId: %q, existingAgentSubnetResourceId: %q`, vnet, other, subnet("agents"))))
	MustOK(t, Run(t, net(fmt.Sprintf(`existingVnetResourceId: %q, existingPrivateEndpointSubnetResourceId: %q, existingAgentSubnetResourceId: %q`, vnet, subnet("pe"), subnet("agents")))))
	MustOK(t, Run(t, net(`addressSpace: 192.168.0.0/20, agentSubnetPrefixLength: 24`)))
	// A basic setup does not need an agent subnet.
	MustOK(t, Run(t, `agentService: {setup: basic}`, net(fmt.Sprintf(`existingVnetResourceId: %q, existingPrivateEndpointSubnetResourceId: %q`, vnet, subnet("pe")))))
}

func TestAgentServiceRules(t *testing.T) {
	ExpectCase(t, "XF128", "agentService.setup", "only used by the standard", `agentService: {setup: basic}`, `cosmos: {}`)
	a := Run(t, Private, `agentService: {setup: basic}`)
	if !a.OK() || !Codes(a, diag.Warning)["XF128"] {
		t.Fatalf("basic in private mode should warn:\n%s", Lines(a.Diagnostics))
	}
	ExpectCase(t, "XF128", "storage.enabled", "", Private, `storage: {enabled: false}`)
	ExpectCase(t, "XF128", "cosmos.enabled", "", Private, `cosmos: {enabled: false}`)
	ExpectCase(t, "XF128", "search.enabled", "", Private, `search: {enabled: false}`)
	// Not standard: nothing to enforce.
	MustOK(t, Run(t, Public, `storage: {enabled: false}`))
	ExpectCase(t, "XF131", "cosmos.throughput", "", `cosmos: {capacityMode: serverless, throughput: 4000}`)
	if !Codes(Run(t, `cosmos: {throughput: 1000}`), "")["XF102"] {
		t.Fatal("Foundry needs at least 3000 RU/s")
	}
	MustOK(t, Run(t, `cosmos: {capacityMode: serverless}`))
}

func TestIdentityTypeRules(t *testing.T) {
	ExpectCase(t, "XF130", "managedIdentity", "no user-assigned identity", `managedIdentity: {type: systemAssigned, name: id-finance}`)
	ExpectCase(t, "XF130", "federatedCredentials", "", `managedIdentity: {type: systemAssigned, federatedCredentials: [{name: gh, issuer: "https://token.example", subject: s}]}`)
	OK(t, Run(t, `managedIdentity: {type: systemAssigned}`))
	OK(t, Run(t, `managedIdentity: {type: systemAssignedAndUserAssigned, name: id-finance}`))
}

func TestVNetSettingsNeedPrivateMode(t *testing.T) {
	vnet := arm + "/Microsoft.Network/virtualNetworks/v1"
	for _, mode := range []string{"public", "restricted"} {
		for _, setting := range []string{
			"addressSpace: 10.30.0.0/16",
			"agentSubnetPrefixLength: 25",
			fmt.Sprintf("existingVnetResourceId: %q", vnet),
		} {
			if mode == "restricted" && strings.HasPrefix(setting, "existingVnet") {
				continue // restricted mode may use an existing VNet
			}
			key := strings.SplitN(setting, ":", 2)[0]
			ExpectCase(t, "XF127", key, "needs network mode 'private'",
				fmt.Sprintf(`security: {network: {mode: %s, allowedIps: [203.0.113.0/24], %s}, roles: {admins: [a]}}`, mode, setting))
		}
	}
}
