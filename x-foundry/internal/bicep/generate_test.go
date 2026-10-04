package bicep_test

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/bicep"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
)

const arm = "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/rg-shared/providers"

func generate(t *testing.T, overrides ...string) *bicep.Output {
	t.Helper()
	out, err := bicep.Generate(MustPlan(t, overrides...))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func generateHub(t *testing.T, overrides ...string) *bicep.Output {
	t.Helper()
	p := MustOK(t, RunHub(t, overrides...))
	out, err := bicep.Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func file(t *testing.T, out *bicep.Output, path string) string {
	t.Helper()
	for _, f := range out.Files {
		if f.Path == path {
			return string(f.Content)
		}
	}
	t.Fatalf("no %s in %v", path, paths(out))
	return ""
}

func paths(out *bicep.Output) []string {
	var p []string
	for _, f := range out.Files {
		p = append(p, f.Path)
	}
	return p
}

func has(out *bicep.Output, path string) bool {
	for _, f := range out.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

func mustContain(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Fatalf("missing %q in:\n%s", p, text)
		}
	}
}

func mustNotContain(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(text, p) {
			t.Fatalf("unexpected %q in:\n%s", p, text)
		}
	}
}

func codes(out *bicep.Output) map[string]bool {
	m := map[string]bool{}
	for _, d := range out.Diagnostics {
		m[d.Code] = true
	}
	return m
}

// bicepCLI returns the Bicep CLI, or skips the test. CI sets XFOUNDRY_REQUIRE_BICEP so a missing
// CLI fails instead of skipping.
func bicepCLI(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("XFOUNDRY_BICEP"); p != "" {
		return p
	}
	p, err := exec.LookPath("bicep")
	if err != nil {
		if os.Getenv("XFOUNDRY_REQUIRE_BICEP") != "" {
			t.Fatal("the Bicep CLI is required: install it or set XFOUNDRY_BICEP")
		}
		t.Skip("the Bicep CLI is not installed")
	}
	return p
}

// build compiles a Bicep file and fails on any error or linter warning.
func build(t *testing.T, cli, file string) {
	t.Helper()
	cmd := exec.Command(cli, "build", file, "--stdout") //nolint:gosec // the CLI path comes from the test environment
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = nil, &stderr
	if err := cmd.Run(); err != nil || strings.Contains(stderr.String(), "Warning") || strings.Contains(stderr.String(), "Error") {
		t.Fatalf("bicep build %s: %v\n%s", file, err, stderr.String())
	}
}

func writeAll(t *testing.T, out *bicep.Output) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range out.Files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, f.Content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func exampleFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../examples/*.yaml")
	if err != nil || len(files) < 6 {
		t.Fatalf("examples: %v %v", files, err)
	}
	return files
}

func TestExamplesGenerateDeterministicBicepThatCompiles(t *testing.T) {
	cli := bicepCLI(t)
	for _, example := range exampleFiles(t) {
		t.Run(filepath.Base(example), func(t *testing.T) {
			p, err := plan.BuildFile(example)
			if err != nil {
				t.Fatal(err)
			}
			first, err := bicep.Generate(p)
			if err != nil {
				t.Fatal(err)
			}
			second, _ := bicep.Generate(p)
			if len(first.Files) != len(second.Files) {
				t.Fatal("different file sets")
			}
			for i := range first.Files {
				if first.Files[i].Path != second.Files[i].Path || !bytes.Equal(first.Files[i].Content, second.Files[i].Content) {
					t.Fatalf("%s differs between runs", first.Files[i].Path)
				}
			}
			for _, d := range first.Diagnostics {
				if d.Severity == diag.Error {
					t.Fatalf("unexpected error: %v", d)
				}
			}
			build(t, cli, filepath.Join(writeAll(t, first), "main.bicep"))
		})
	}
}

func TestStaticModulesCompile(t *testing.T) {
	cli := bicepCLI(t)
	entries, err := filepath.Glob("modules/*.bicep")
	if err != nil || len(entries) < 10 {
		t.Fatalf("modules: %v %v", entries, err)
	}
	for _, m := range entries {
		t.Run(filepath.Base(m), func(t *testing.T) { build(t, cli, m) })
	}
}

func TestEveryModuleIsUsedByOneOfTheScenarios(t *testing.T) {
	// A module that nothing can reach is dead code: this guards against forgetting to wire a module.
	used := map[string]bool{}
	scenarios := []*bicep.Output{
		generate(t, Private, `managedIdentity: {type: userAssigned}`),
		generate(t, Private, `observability: {}`, `projects: [{name: finance, roles: {developers: [{type: user, id: "11111111-1111-1111-1111-111111111111"}], admins: [{type: group, id: "22222222-2222-2222-2222-222222222222"}]}}]`,
			`security: {network: {mode: private}, roles: {admins: [{type: user, id: "33333333-3333-3333-3333-333333333333"}], operators: [{type: group, id: "44444444-4444-4444-4444-444444444444"}]}}`, `keyVault: {}`,
			`models: {default: gpt-5, allowed: [gpt-5]}`),
	}
	for _, o := range scenarios {
		for _, f := range o.Files {
			used[f.Path] = true
		}
	}
	entries, _ := fs.Glob(os.DirFS("."), "modules/*.bicep")
	for _, m := range entries {
		if !used[m] {
			t.Errorf("%s is not produced by any scenario", m)
		}
	}
}

func TestPublicMinimalHasNoNetworkingOrSharedServices(t *testing.T) {
	out := generate(t, Public)
	r := file(t, out, "resources.bicep")
	mustContain(t, r, "module identity 'modules/identity.bicep'")
	mustNotContain(t, r, "network.bicep", "private-dns", "private-endpoint", "storage.bicep", "cosmos.bicep", "search.bicep", "foundry-account")
	if has(out, "modules/network.bicep") || has(out, "modules/private-endpoint.bicep") {
		t.Fatalf("unused modules were copied: %v", paths(out))
	}
	main := file(t, out, "main.bicep")
	mustContain(t, main, "targetScope = 'subscription'", "name: 'rg-${environmentName}'", "param location string\n", "'azd-env-name': environmentName")
	mustContain(t, file(t, out, "main.parameters.json"), "${AZURE_ENV_NAME}")
	mustContain(t, file(t, out, "README.md"), "Network: `public` (explicit)", "x-foundry-id", "does **not** create the Foundry resource")
}

func TestPrivateStandardSetup(t *testing.T) {
	out := generate(t, Private)
	r := file(t, out, "resources.bicep")
	mustContain(t, r,
		"module network 'modules/network.bicep'", "agentSubnetPrefix: '10.20.0.0/24'", "peSubnetPrefix: '10.20.1.0/24'",
		"module private_dns", "module storage 'modules/storage.bicep'", "module cosmos 'modules/cosmos.bicep'", "module search_root",
		"module private_endpoint_storage_blob", "groupId: 'blob'", "groupId: 'Sql'", "groupId: 'searchService'",
		"publicNetworkAccess: false", "'privatelink.blob.${environment().suffixes.storage}'")
	mustNotContain(t, r, "capability", "foundry-account", "connection")
	mustContain(t, r, "output VNET_RESOURCE_ID string = network.outputs.id", "output PE_SUBNET_NAME string = 'pe-subnet'",
		"output AGENT_SUBNET_NAME string = 'agent-subnet'", "output STORAGE_ACCOUNT_RESOURCE_ID", "output COSMOS_DB_RESOURCE_ID", "output AI_SEARCH_RESOURCE_ID")
}

func TestBasicSetupInPrivateModeHasNoAgentSubnetOrCosmos(t *testing.T) {
	out := generate(t, Private, `agentService: {setup: basic}`)
	r := file(t, out, "resources.bicep")
	mustContain(t, r, "module network", "agentSubnetPrefix: ''")
	mustNotContain(t, r, "module storage", "module cosmos", "AGENT_SUBNET_NAME")
}

func TestRestrictedModeAppliesAllowedIPs(t *testing.T) {
	out := generate(t, `security: {network: {mode: restricted, allowedIps: [203.0.113.0/24]}, roles: {admins: [a]}}`, `storage: {}`, `keyVault: {}`, `agentService: {setup: basic}`)
	r := file(t, out, "resources.bicep")
	mustContain(t, r, "ipRules: ['203.0.113.0/24']", "publicNetworkAccess: true")
	mustNotContain(t, r, "private-endpoint")
	vnet := arm + "/Microsoft.Network/virtualNetworks/v1"
	out = generate(t, `security: {network: {mode: restricted, existingVnetResourceId: "`+vnet+`", allowedIps: [203.0.113.0/24]}, roles: {admins: [a]}}`, `agentService: {setup: basic}`)
	if !codes(out)["XF205"] {
		t.Fatalf("expected XF205: %v", out.Diagnostics)
	}
}

func TestExistingResourcesAreReferencedNotCreated(t *testing.T) {
	overrides := []string{Private,
		`storage: {existingResourceId: "` + arm + `/Microsoft.Storage/storageAccounts/stshared"}`,
		`cosmos: {existingResourceId: "` + arm + `/Microsoft.DocumentDB/databaseAccounts/cosshared"}`,
		`search: {existingResourceId: "` + arm + `/Microsoft.Search/searchServices/srchshared"}`,
		`keyVault: {existingResourceId: "` + arm + `/Microsoft.KeyVault/vaults/kvshared"}`}
	out := generate(t, overrides...)
	r := file(t, out, "resources.bicep")
	mustContain(t, r,
		"output STORAGE_ACCOUNT_RESOURCE_ID string = '"+arm+"/Microsoft.Storage/storageAccounts/stshared'",
		"output COSMOS_DB_RESOURCE_ID string = '"+arm+"/Microsoft.DocumentDB/databaseAccounts/cosshared'",
		"output AI_SEARCH_RESOURCE_ID string = '"+arm+"/Microsoft.Search/searchServices/srchshared'",
		"output KEY_VAULT_RESOURCE_ID string = '"+arm+"/Microsoft.KeyVault/vaults/kvshared'")
	mustNotContain(t, r, "module storage ", "module cosmos ", "module search_root ", "module key_vault ", " existing = ")
	// Private endpoints still target the existing resources.
	mustContain(t, r, "targetId: '"+arm+"/Microsoft.Storage/storageAccounts/stshared'", "pe-${'stshared'}-blob")
	if cli := os.Getenv("XFOUNDRY_BICEP"); cli != "" {
		build(t, cli, filepath.Join(writeAll(t, out), "main.bicep"))
	}
}

func TestExistingVNetAndSubnets(t *testing.T) {
	vnet := arm + "/Microsoft.Network/virtualNetworks/v1"
	out := generate(t, `security: {network: {existingVnetResourceId: "`+vnet+`", existingPrivateEndpointSubnetResourceId: "`+vnet+`/subnets/pe", existingAgentSubnetResourceId: "`+vnet+`/subnets/agents"}, roles: {admins: [a]}}`)
	r := file(t, out, "resources.bicep")
	mustContain(t, r, "subnetId: '"+vnet+"/subnets/pe'", "vnetId: '"+vnet+"'", "output AGENT_SUBNET_NAME string = 'agents'", "output PE_SUBNET_NAME string = 'pe'")
	mustNotContain(t, r, "module network ")
}

func TestExplicitNamesAndResourceGroup(t *testing.T) {
	out := generate(t, Private, `defaults: {resourceGroup: rg-finance, namingPrefix: Fin-Ai, location: West Europe}`,
		`storage: {name: stfinance01}`, `keyVault: {name: kv-finance}`, `cosmos: {name: cosmos-finance}`, `search: {name: srch-finance}`, `managedIdentity: {name: id-finance}`)
	r, m := file(t, out, "resources.bicep"), file(t, out, "main.bicep")
	mustContain(t, r, "name: 'stfinance01'", "name: 'kv-finance'", "name: 'cosmos-finance'", "name: 'srch-finance'", "name: 'id-finance'", "var base = 'fin-ai'")
	mustContain(t, m, "name: 'rg-finance'", "param location string = 'westeurope'")
}

func TestHubAndProjectSearchServicesAreSeparate(t *testing.T) {
	out := generateHub(t, Private, `projects: [{name: finance, search: {name: srch-finance}}, {name: hr}]`,
		`hub: {name: shared, search: {name: srch-hub}}`)
	r := file(t, out, "resources.bicep")
	mustContain(t, r, "module search_hub", "name: 'srch-hub'", "module search_project_finance", "name: 'srch-finance'",
		"module private_endpoint_search_hub_searchService", "module private_endpoint_search_project_finance_searchService")
	mustContain(t, r, "output AI_SEARCH_HUB_RESOURCE_ID string = search_hub.outputs.id", "output AI_SEARCH_FINANCE_RESOURCE_ID string = search_project_finance.outputs.id")
}

func TestGovernanceLocksReachTheModules(t *testing.T) {
	locked := file(t, generate(t, Private, `governance: {resourceLocks: true}`), "resources.bicep")
	mustContain(t, locked, "deleteLock: true")
	unlocked := file(t, generate(t, Private), "resources.bicep")
	mustContain(t, unlocked, "deleteLock: false")
	mustNotContain(t, unlocked, "deleteLock: true")
	off := file(t, generate(t, Private, `governance: {resourceLocks: false}`), "resources.bicep")
	mustNotContain(t, off, "deleteLock: true")
	out := generate(t, Private, `governance: {policyAssignments: [require-no-local-auth]}`)
	found := false
	for _, d := range out.Deferred {
		found = found || strings.Contains(d, "governance")
	}
	if !found {
		t.Fatalf("deferred = %v", out.Deferred)
	}
}

func TestRoleAssignments(t *testing.T) {
	admin, dev, ops := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"
	out := generate(t,
		`security: {network: {mode: public}, roles: {admins: [{type: user, id: "`+admin+`"}], developers: ["`+dev+`"], operators: [{type: servicePrincipal, id: "`+ops+`"}, Ops-Group]}}`,
		`projects: [{name: finance}]`)
	r := file(t, out, "resources.bicep")
	mustContain(t, r, "principalId: '"+ops+"'", "principalType: 'ServicePrincipal'",
		"acdd72a7-3385-48ef-bd42-f606fba81ae7", "43d0d8ad-25c7-4714-9337-8ba259a9fe05")
	// Only operators get roles here (on the resource group); admins and developers need the Foundry
	// resources azd creates, so they are listed as not generated. A name without an object ID is reported once.
	mustNotContain(t, r, "principalId: '"+admin+"'", "principalId: '"+dev+"'")
	if !codes(out)["XF201"] || len(out.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v", out.Diagnostics)
	}
	if !strings.Contains(strings.Join(out.Deferred, "\n"), "role assignments for admins and developers") {
		t.Fatalf("deferred = %v", out.Deferred)
	}
}

func TestStorageContainersAndSharedKey(t *testing.T) {
	out := generate(t, `security: {network: {mode: private}, roles: {admins: [a]}, localAuthentication: true}`, `storage: {purposes: [evaluations], containers: [{name: raw-docs}, {name: open, publicAccess: blob}], localAuthentication: true}`,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policy-files}]}]}`)
	r := file(t, out, "resources.bicep")
	mustContain(t, r, "containers: ['documents', 'evaluations', 'knowledge', 'open', 'policy-files', 'raw-docs']", "allowSharedKeyAccess: true")
	if !codes(out)["XF207"] {
		t.Fatalf("diagnostics = %v", out.Diagnostics)
	}
}

func TestKeyVaultPurgeProtectionFollowsTheSetting(t *testing.T) {
	r := file(t, generate(t, Private, `keyVault: {purgeProtection: false}`), "resources.bicep")
	mustContain(t, r, "purgeProtection: false")
	// Without its own setting the vault follows security.purgeProtection; its own setting wins.
	global := `security: {network: {mode: private}, roles: {admins: [a]}, purgeProtection: false}`
	r = file(t, generate(t, global, `keyVault: {}`), "resources.bicep")
	mustContain(t, r, "purgeProtection: false")
	r = file(t, generate(t, global, `keyVault: {purgeProtection: true}`), "resources.bicep")
	mustContain(t, r, "purgeProtection: true")
}

func TestObservabilityWithoutApplicationInsights(t *testing.T) {
	r := file(t, generate(t, Public, `observability: {applicationInsights: false, retentionDays: 30}`), "resources.bicep")
	mustContain(t, r, "appInsightsName: ''", "retentionDays: 30")
	mustNotContain(t, r, "appinsights-connection")
}

func TestLongSymbolsGetShortDeploymentNames(t *testing.T) {
	long := strings.Repeat("a", 30)
	out := generate(t, Public, `projects: [{name: `+long+`-project-with-a-long-name}]`, `search: {name: srch-long}`)
	r := file(t, out, "resources.bicep")
	for _, line := range strings.Split(r, "\n") {
		if strings.HasPrefix(line, "  name: 'xf-") && len(line) > len("  name: ''")+64 {
			t.Fatalf("deployment name too long: %s", line)
		}
	}
}

func TestOutputsCoverTheRunnableFiles(t *testing.T) {
	out := generate(t, Private)
	want := []string{"README.md", "main.bicep", "main.parameters.json", "resources.bicep", "modules/storage.bicep"}
	for _, w := range want {
		if !has(out, w) {
			t.Errorf("missing %s in %v", w, paths(out))
		}
	}
	readme := file(t, out, "README.md")
	mustContain(t, readme, "## Deploy", "az deployment sub create", "## Resources", "`storage`", "Delete locks")
}

func TestDeferredItemsAreListed(t *testing.T) {
	out := generateHub(t, Public, `hub: {name: shared, search: {}, inheritance: {iq: true, search: true}, iq: {knowledgeBases: [{name: kb1, sources: [{name: files, type: blob, container: docs}]}]}}`,
		`projects: [{name: fin}]`, `gateway: {enabled: true}`, `observability: {}`)
	text := strings.Join(out.Deferred, "\n")
	mustContain(t, text, "knowledge-base (data plane on the Search service, Phase 4)", "gateway (API Management, Phase 5)", "alerts (Phase 5)", "role assignments for admins and developers")
	mustContain(t, file(t, out, "README.md"), "## Not generated yet")
}

func TestAnUnusableAddressSpaceFailsGeneration(t *testing.T) {
	p := MustPlan(t, Private)
	p.Config.Network.AddressSpace = "fd00::/48"
	out, err := bicep.Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 0 || !codes(out)["XF206"] || !diag.HasErrors(out.Diagnostics) {
		t.Fatalf("expected XF206 and no files: %v %v", out.Diagnostics, paths(out))
	}
}

func TestRoleAssignmentsAreLabelledWithTheRoleName(t *testing.T) {
	ops := "33333333-3333-3333-3333-333333333333"
	out := generate(t, `security: {network: {mode: private}, roles: {admins: [a], operators: [{type: user, id: "`+ops+`"}]}}`)
	r := file(t, out, "resources.bicep")
	mustContain(t, r,
		"// Reader for operator "+ops+"\n",
		"assignmentDescription: 'Reader for operator "+ops+"'",
		"// Monitoring Reader for operator "+ops+"\n")
}
