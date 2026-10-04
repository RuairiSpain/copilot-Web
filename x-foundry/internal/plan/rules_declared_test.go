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
	tool := `{name: t1, type: function, reference: f}`
	runRules(t, []rule{
		{"project agents", "XF002", "projects[fin].agents", "agent name 'bot'", y(`projects: [{name: fin, agents: [{name: bot}, {name: bot}]}]`)},
		{"root agents", "XF002", "x-foundry.agents", "", y(`agents: [{name: bot}, {name: bot}]`)},
		{"root agent assigned to project", "XF002", "projects[fin]", "", y(`agents: [{name: bot, project: fin}]`, `projects: [{name: fin, agents: [{name: bot}]}]`)},
		{"deployments", "XF002", "deployments", "model deployment", y(`models: {deployments: [{name: m1, model: gpt-5}, {name: m1, model: gpt-5}]}`)},
		{"mcps", "XF002", "mcps", "MCP", y(`mcps: [{name: graph, endpoint: "https://a.example"}, {name: graph, endpoint: "https://b.example"}]`)},
		{"connectors", "XF002", "connectors", "connector", y(`connectors: [{name: sp, type: sharepoint}, {name: sp, type: graph}]`)},
		{"toolboxes", "XF002", "toolboxes", "toolbox", y(fmt.Sprintf(`toolboxes: [{name: tb, tools: [%s]}, {name: tb, tools: [%s]}]`, tool, tool))},
		{"tools", "XF002", "tools", "tool name", y(fmt.Sprintf(`toolboxes: [{name: tb, tools: [%s, %s]}]`, tool, tool))},
		{"knowledge bases", "XF002", "knowledgeBases", "knowledge base", y(`iq: {knowledgeBases: [{name: one, sources: [{name: s1, type: web, url: "https://x.example"}]}, {name: one, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`)},
		{"project knowledge bases", "XF002", "projects[fin]", "knowledge base", y(`projects: [{name: fin, iq: {knowledgeBases: [{name: one, sources: [{name: s1, type: web, url: "https://x.example"}]}, {name: one, sources: [{name: s1, type: web, url: "https://x.example"}]}]}}]`)},
		{"sources", "XF002", "sources", "knowledge source", y(`iq: {knowledgeBases: [{name: one, sources: [{name: s1, type: web, url: "https://x.example"}, {name: s1, type: web, url: "https://x.example"}]}]}`)},
		{"endpoints", "XF002", "endpoints", "gateway endpoint", y(`gateway: {enabled: true, endpoints: [{name: ep1, path: /a, target: x, targetType: agent}, {name: ep1, path: /b, target: x, targetType: agent}]}`)},
		{"events", "XF002", "entities", "event entity", y(`events: {enabled: true, entities: [{name: q1, type: queue}, {name: q1, type: queue}]}`)},
		{"containers", "XF002", "containers", "storage container", y(`storage: {containers: [{name: abc}, {name: abc}]}`)},
		{"scale rules", "XF002", "scale.rules", "scale rule", y(`runtime: {enabled: true, image: "i:1", scale: {rules: [{name: r1, type: http}, {name: r1, type: http}]}}`)},
		{"datasets", "XF002", "datasets", "dataset", y(`evaluation: {datasets: [{name: d1, path: p}, {name: d1, path: p}]}`)},
		{"hub mcps", "XF002", "hub.mcps", "MCP", y("topology: {mode: hub-spoke}\nhub: {name: shared, mcps: [{name: graph, endpoint: \"https://a.example\"}, {name: graph, endpoint: \"https://a.example\"}]}")},
	})
}

func TestRule2SameNameInDifferentScopesIsAnOverride(t *testing.T) {
	a := RunHub(t,
		`hub: {name: shared, toolboxes: [{name: tb, tools: [{name: t1, type: function, reference: g}]}]}`,
		`projects: [{name: fin, toolboxes: [{name: tb, tools: [{name: t1, type: function, reference: f}]}]}]`)
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
		"agent":      `agents: [{name: bot, project: nope}]`,
		"toolbox":    `toolboxes: [{name: tb, project: nope, tools: [{name: t1, type: function, reference: f}]}]`,
		"iq":         `iq: {project: nope, knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`,
		"kb":         `iq: {knowledgeBases: [{name: kb1, project: nope, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`,
		"runtime":    `runtime: {enabled: true, image: "i:1", project: nope}`,
		"evaluation": `evaluation: {project: nope}`,
		"endpoint":   `gateway: {enabled: true, endpoints: [{name: ep1, path: /a, target: x, targetType: agent, project: nope}]}`,
		"route":      `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}], routing: {routes: [{name: r1, when: {project: nope}, knowledgeBase: kb1}]}}]}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) { ExpectCase(t, "XF005", "", "unknown project 'nope'", doc) })
	}
	ExpectCase(t, "XF005", "", "declared inside project 'bb'", `projects: [{name: aa}, {name: bb, agents: [{name: bot, project: aa}]}]`)
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
			ExpectCase(t, "XF017", "description", "appears to contain", fmt.Sprintf(`projects: [{name: fin, description: %q}]`, value))
		})
	}
	a := Run(t, `runtime: {enabled: true, image: "i:1", environment: {DB_PASSWORD: hunter2, MAX_TOKENS: 4096, LOG_LEVEL: info}}`)
	var paths []string
	for _, d := range a.Diagnostics {
		if d.Code == "XF017" {
			paths = append(paths, d.Path)
		}
	}
	if len(paths) != 1 || paths[0] != "x-foundry.runtime.environment.DB_PASSWORD" {
		t.Fatalf("paths = %v", paths)
	}
	ExpectCase(t, "XF017", "headers.x-api-key", "", `mcps: [{name: graph, endpoint: "https://a.example", headers: {x-api-key: abc, accept: json}}]`)
	for _, ref := range []string{
		"@Microsoft.KeyVault(SecretUri=https://kv.vault.azure.net/secrets/key/)",
		"keyvault:search-key",
		"https://my-vault.vault.azure.net/secrets/search-key",
		"https://my-vault.vault.azure.net/secrets/search-key/0123456789abcdef0123456789abcdef",
	} {
		a := Run(t, fmt.Sprintf(`runtime: {enabled: true, image: "i:1", environment: {SEARCH_API_KEY: %q}}`, ref))
		if Codes(a, "")["XF017"] {
			t.Fatalf("reference %q rejected:\n%s", ref, Lines(a.Diagnostics))
		}
	}
	ExpectCase(t, "XF017", "secrets.db", "", `runtime: {enabled: true, image: "i:1", secrets: {db: "not a name!"}}`)
	ExpectCase(t, "XF017", "secretRef", "", `mcps: [{name: graph, endpoint: "https://a.example", authentication: {mode: apiKey, secretRef: "bad value"}}]`)
	MustOK(t, Run(t, `runtime: {enabled: true, image: "i:1", secrets: {db: db-password}}`))
	ExpectCase(t, "XF119", "", "", `connectors: [{name: svc, type: api, authentication: {mode: apiKey}}]`)
}

func TestRule22ExistingResources(t *testing.T) {
	existing := func(typ string) string { return fmt.Sprintf("%s/%s/x1", arm, typ) }
	for _, c := range []struct{ component, typ, extra, key string }{
		{"search", "Microsoft.Search/searchServices", "sku: basic", "sku"},
		{"search", "Microsoft.Search/searchServices", "replicas: 2", "replicas"},
		{"storage", "Microsoft.Storage/storageAccounts", "sku: Standard_LRS", "sku"},
		{"redis", "Microsoft.Cache/redis", "capacity: 2", "capacity"},
		{"keyVault", "Microsoft.KeyVault/vaults", "sku: premium", "sku"},
		{"cosmos", "Microsoft.DocumentDB/databaseAccounts", "throughput: 4000", "throughput"},
		{"managedIdentity", "Microsoft.ManagedIdentity/userAssignedIdentities", "tags: {a: b}", "tags"},
	} {
		t.Run(c.component+"/"+c.key, func(t *testing.T) {
			ExpectCase(t, "XF022", c.key, "", fmt.Sprintf("%s: {existingResourceId: %q, %s}", c.component, existing(c.typ), c.extra))
		})
	}
	for component, typ := range map[string]string{
		"search": "Microsoft.Search/searchServices", "storage": "Microsoft.Storage/storageAccounts",
		"redis": "Microsoft.Cache/redisEnterprise", "keyVault": "Microsoft.KeyVault/vaults",
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

func TestRule22Registry(t *testing.T) {
	runRules(t, []rule{
		{"managed with server", "XF022", "", "managed registry cannot set", y(`runtime: {enabled: true, image: "i:1", registry: {mode: managed, server: x.azurecr.io}}`)},
		{"existing needs id", "XF022", "", "requires resourceId", y(`runtime: {enabled: true, image: "i:1", registry: {mode: existing}}`)},
		{"existing wrong type", "XF022", "", "not a container registry", y(fmt.Sprintf(`runtime: {enabled: true, image: "i:1", registry: {mode: existing, resourceId: "%s/Microsoft.Storage/storageAccounts/x"}}`, arm))},
		{"external needs server", "XF022", "", "requires server", y(`runtime: {enabled: true, image: "i:1", registry: {mode: external}}`)},
		{"external name", "XF022", "", "applies only to a managed registry", y(`runtime: {enabled: true, image: "i:1", registry: {mode: external, server: ghcr.io, name: acr12345}}`)},
		{"external sku", "XF022", "registry.sku", "", y(`runtime: {enabled: true, image: "i:1", registry: {mode: external, server: ghcr.io, sku: Premium}}`)},
	})
	MustOK(t, Run(t, fmt.Sprintf(`runtime: {enabled: true, image: "i:1", registry: {mode: existing, resourceId: "%s/Microsoft.ContainerRegistry/registries/acr1"}}`, arm)))
}

func TestRule23Regions(t *testing.T) {
	runRules(t, []rule{
		{"defaults", "XF023", "defaults.location", "", y(`defaults: {location: atlantis}`)},
		{"project", "XF023", "projects[fin].location", "", y(`projects: [{name: fin, location: moon}]`)},
		{"deployment", "XF023", "deployments[m1]", "", y(`models: {deployments: [{name: m1, model: gpt-5, location: nowhere}]}`)},
		{"residency", "XF023", "", "outside governance.dataResidency", y(`defaults: {location: eastus}`, `governance: {dataResidency: [westeurope, northeurope]}`)},
		{"residency region", "XF023", "dataResidency", "", y(`governance: {dataResidency: [atlantis]}`)},
	})
	MustOK(t, Run(t, `defaults: {location: "West Europe"}`))
	MustOK(t, Run(t, `defaults: {location: westeurope}`, `governance: {dataResidency: [westeurope]}`))
	// Region mismatch: warning outside private mode, error in private mode.
	a := Run(t, Public, `defaults: {location: westeurope}`, `projects: [{name: fin, location: eastus}]`)
	if !a.OK() || !Codes(a, diag.Warning)["XF120"] {
		t.Fatalf("expected an XF120 warning:\n%s", Lines(a.Diagnostics))
	}
	ExpectCase(t, "XF120", "projects[fin].location", "", Private, `defaults: {location: westeurope}`, `projects: [{name: fin, location: eastus}]`)
	hub := Analyse(t, HubDoc(t, Public, `defaults: {location: westeurope}`, `projects: [{name: fin, location: eastus}]`))
	if !hub.OK() || !Codes(hub, diag.Warning)["XF120"] {
		t.Fatalf("expected warnings only:\n%s", Lines(hub.Diagnostics))
	}
	privateHub := RunHub(t, Private, `defaults: {location: westeurope}`, `projects: [{name: fin, location: eastus}]`)
	if privateHub.OK() {
		t.Fatal("a cross-region spoke must fail in private mode")
	}
	if w := hub.Plan.Warnings; len(w) == 0 || w[0].Code != "XF120" {
		t.Fatalf("warnings = %v", w)
	}
}

func TestRule24ExplicitNames(t *testing.T) {
	runRules(t, []rule{
		{"storage case", "XF024", "storage.name", "", y(`storage: {name: My-Storage}`)},
		{"storage short", "XF024", "storage.name", "", y(`storage: {name: ab}`)},
		{"key vault hyphens", "XF024", "keyVault.name", "", y(`keyVault: {name: kv--double}`)},
		{"search", "XF024", "search.name", "", y(`search: {name: Search1}`)},
		{"redis", "XF024", "redis.name", "", y(`redis: {name: r--x}`)},
		{"cosmos", "XF024", "cosmos.name", "", y(`cosmos: {name: Cosmos1}`)},
		{"apim", "XF024", "gateway.name", "", y(`gateway: {enabled: true, name: 1apim}`)},
		{"service bus", "XF024", "events.namespace", "", y(`events: {enabled: true, namespace: 1bus-name}`)},
		{"identity", "XF024", "managedIdentity.name", "", y(`managedIdentity: {name: ab}`)},
		{"container app", "XF024", "runtime.name", "", y(`runtime: {enabled: true, image: "i:1", name: Upper}`)},
		{"registry", "XF024", "registry.name", "", y(`runtime: {enabled: true, image: "i:1", registry: {mode: managed, name: reg-1}}`)},
		{"resource group", "XF024", "defaults.resourceGroup", "", y(`defaults: {resourceGroup: "rg."}`)},
		{"project resource group", "XF024", "projects[fin].resourceGroup", "", y(`projects: [{name: fin, resourceGroup: "bad!"}]`)},
		{"source container", "XF024", "container", "", y(`iq: {knowledgeBases: [{name: kb1, sources: [{name: files, type: blob, container: Policies}]}]}`)},
		{"storage container", "XF024", "containers[a--b]", "", y(`storage: {containers: [{name: a--b}]}`)},
	})
	MustOK(t, Run(t, `storage: {name: stfinance01}`, `keyVault: {name: kv-finance}`, `search: {name: srch-finance}`,
		`redis: {name: redis-finance}`, `managedIdentity: {name: id-finance}`, `defaults: {resourceGroup: rg-finance}`,
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
	ExpectCase(t, "XF106", "redis.publicNetworkAccess", "", Public, `redis: {enabled: true, publicNetworkAccess: true}`)
	MustOK(t, Run(t, `security: {network: {mode: public}, roles: {admins: [a]}, publicNetworkAccess: true}`, `redis: {enabled: true, publicNetworkAccess: true}`))
	ExpectCase(t, "XF106", "search.localAuthentication", "", `search: {localAuthentication: true}`)
	MustOK(t, Run(t, `security: {roles: {admins: [a]}, localAuthentication: true}`, `search: {localAuthentication: true}`))
	ExpectCase(t, "XF021", "managedIdentity.enabled", "", Private, `managedIdentity: {enabled: false}`)
	ExpectCase(t, "XF021", "ingress.external", "", Private, `runtime: {enabled: true, image: "i:1", ingress: {external: true}}`)
	a = Run(t, `security: {network: {mode: private, privateDns: false}, roles: {admins: [a]}}`)
	if !a.OK() || !Codes(a, diag.Warning)["XF021"] {
		t.Fatalf("privateDns false should warn:\n%s", Lines(a.Diagnostics))
	}
	ExpectCase(t, "XF106", "redis.sku", "", `redis: {enabled: true, service: azure-cache-for-redis, sku: balanced}`)
	ExpectCase(t, "XF106", "redis.sku", "", `redis: {enabled: true, sku: premium}`)
	MustOK(t, Run(t, fmt.Sprintf(`redis: {existingResourceId: "%s/Microsoft.Cache/redis/r1"}`, arm)))
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
		"http":      `mcps: [{name: graph, endpoint: "http://graph.example/mcp"}]`,
		"metadata":  `mcps: [{name: graph, endpoint: "https://169.254.169.254/metadata"}]`,
		"localhost": `mcps: [{name: graph, endpoint: "https://localhost/mcp"}]`,
		"ipv6":      `mcps: [{name: graph, endpoint: "https://[::1]/mcp"}]`,
		"connector": `connectors: [{name: svc, type: api, endpoint: "http://svc.example"}]`,
		"web":       `iq: {knowledgeBases: [{name: kb1, sources: [{name: web1, type: web, url: "http://x.example"}]}]}`,
		"issuer":    `managedIdentity: {federatedCredentials: [{name: gh, issuer: "http://issuer.example", subject: s}]}`,
	} {
		t.Run(name, func(t *testing.T) { ExpectCase(t, "XF122", "", "", doc) })
	}
	MustOK(t, Run(t, `mcps: [{name: graph, endpoint: "https://graph.contoso.com/mcp"}]`))

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

	ExpectCase(t, "XF125", "redis.service", "", `redis: {enabled: true, service: azure-cache-for-redis, sku: standard}`)
	if Codes(Run(t, `redis: {enabled: false, service: azure-cache-for-redis}`), "")["XF125"] {
		t.Fatal("a disabled Redis must not error")
	}
}

func TestRuntimeRules(t *testing.T) {
	ExpectCase(t, "XF108", "scale", "", `runtime: {enabled: true, image: "i:1", scale: {minReplicas: 5, maxReplicas: 2}}`)
	ExpectCase(t, "XF108", "registry.authentication", "", `runtime: {enabled: true, image: "i:1", registry: {mode: managed, authentication: credentials}}`)
	odd := Run(t, `runtime: {enabled: true, image: "i:1", resources: {cpu: 1, memory: 3Gi}}`)
	if !odd.OK() || !Codes(odd, diag.Warning)["XF108"] {
		t.Fatalf("odd CPU/memory should warn:\n%s", Lines(odd.Diagnostics))
	}
	if Codes(Run(t, `runtime: {enabled: false, scale: {minReplicas: 5, maxReplicas: 2}}`), "")["XF108"] {
		t.Fatal("a disabled runtime is not validated")
	}
	for _, image := range []string{"ghcr.io/x/y", "ghcr.io/x/y:latest", "ghcr.io/x/y:"} {
		a := Run(t, fmt.Sprintf(`runtime: {enabled: true, image: %q}`, image))
		if !a.OK() || !Codes(a, diag.Warning)["XF132"] {
			t.Fatalf("%s should warn about a floating tag:\n%s", image, Lines(a.Diagnostics))
		}
	}
	for _, image := range []string{"ghcr.io/x/y:1.2", "localhost:5000/y:1", "ghcr.io/x/y@sha256:abc"} {
		OK(t, Run(t, fmt.Sprintf(`runtime: {enabled: true, image: %q}`, image)))
	}
}

func TestEventRules(t *testing.T) {
	for name, events := range map[string]string{
		"queue on grid": `{enabled: true, provider: eventGrid, entities: [{name: q1, type: queue}]}`,
		"grid sub":      `{enabled: true, entities: [{name: es, type: eventSubscription, parent: t1}]}`,
		"sub no parent": `{enabled: true, entities: [{name: sub1, type: subscription}]}`,
		"sub of queue":  `{enabled: true, entities: [{name: q1, type: queue}, {name: sub1, type: subscription, parent: q1}]}`,
		"queue parent":  `{enabled: true, entities: [{name: q1, type: queue, parent: x}]}`,
		"event hubs":    `{enabled: true, provider: eventHubs, entities: [{name: t1, type: topic}]}`,
	} {
		t.Run(name, func(t *testing.T) { ExpectCase(t, "XF109", "", "", "events: "+events) })
	}
	MustOK(t, Run(t, `events: {enabled: true, entities: [{name: t1, type: topic}, {name: sub1, type: subscription, parent: t1}]}`))
}

func TestCronAndAgentRules(t *testing.T) {
	ExpectCase(t, "XF118", "refresh.schedule", "", `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}], refresh: {schedule: daily}}]}`)
	ExpectCase(t, "XF118", "evaluation.schedule", "", `evaluation: {enabled: true, schedule: "every day"}`)
	ExpectCase(t, "XF112", "", "", `agents: [{name: bot, kind: hosted}]`)
	MustOK(t, Run(t, `agents: [{name: bot, kind: hosted, source: ./bot}]`, `models: {default: gpt-5, allowed: [gpt-5]}`))
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

func TestServiceTierRequirements(t *testing.T) {
	ExpectCase(t, "XF129", "events.sku", "Premium", Private, `events: {enabled: true, sku: Standard}`)
	ExpectCase(t, "XF129", "events.capacity", "Premium", Public, `events: {enabled: true, capacity: 2}`)
	ExpectCase(t, "XF129", "zoneRedundant", "", Public, `events: {enabled: true, zoneRedundant: true}`)
	ExpectCase(t, "XF129", "entities", "queues only", Public, `events: {enabled: true, sku: Basic, entities: [{name: t1, type: topic}]}`)
	ExpectCase(t, "XF129", "events.sku", "serviceBus", `events: {enabled: true, provider: eventGrid, sku: Premium}`)
	MustOK(t, Run(t, `events: {enabled: true, sku: Premium, capacity: 2, zoneRedundant: true}`))
	MustOK(t, Run(t, `events: {enabled: true}`)) // Premium is chosen automatically in private mode
	MustOK(t, Run(t, Public, `events: {enabled: true, sku: Basic, entities: [{name: q1, type: queue}]}`))
	ExpectCase(t, "XF129", "registry.sku", "Premium", Private, `runtime: {enabled: true, source: ./app, registry: {mode: managed, sku: Standard}}`)
	MustOK(t, Run(t, `runtime: {enabled: true, source: ./app, registry: {mode: managed, sku: Premium}}`))
	MustOK(t, Run(t, Public, `runtime: {enabled: true, source: ./app, registry: {mode: managed, sku: Basic}}`))
}

func TestIdentityTypeRules(t *testing.T) {
	ExpectCase(t, "XF130", "managedIdentity", "no user-assigned identity", `managedIdentity: {type: systemAssigned, name: id-finance}`)
	ExpectCase(t, "XF130", "federatedCredentials", "", `managedIdentity: {type: systemAssigned, federatedCredentials: [{name: gh, issuer: "https://token.example", subject: s}]}`)
	a := Run(t, `managedIdentity: {type: systemAssigned}`, `runtime: {enabled: true, source: ./app}`)
	if !a.OK() || !Codes(a, diag.Warning)["XF130"] {
		t.Fatalf("system-assigned with a managed registry should warn:\n%s", Lines(a.Diagnostics))
	}
	OK(t, Run(t, `managedIdentity: {type: systemAssigned}`))
	OK(t, Run(t, `managedIdentity: {type: systemAssignedAndUserAssigned, name: id-finance}`, `runtime: {enabled: true, source: ./app}`))
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
