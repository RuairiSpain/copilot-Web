// Package bicep generates Bicep infrastructure from a validated plan.
//
// The output is a small project: main.bicep (subscription scope, creates the resource group),
// resources.bicep (resource-group scope, wires the modules together in plan order) and the
// static modules it uses. Everything the generator writes is deterministic.
//
// Phase 2 generates the resources that Azure Resource Manager deploys: networking, private DNS
// and endpoints, Storage, Key Vault, Cosmos DB, AI Search, the Foundry resource, projects, the
// agent capability host, model deployments, observability and role assignments. Data-plane
// items (agents, toolboxes, MCPs, knowledge bases, evaluations) and the gateway and governance
// resources belong to later phases and are listed in the generated README.
package bicep

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/netip"
	"sort"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/ids"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
)

// File is one generated file; Path is relative to the output directory.
type File struct {
	Path    string
	Content []byte
}

// Output is the result of Generate.
type Output struct {
	Files       []File
	Diagnostics []diag.Diagnostic // XF2xx warnings and errors found while generating
	Deferred    []string          // what the plan contains that this phase does not generate
}

// Built-in role definition GUIDs. Foundry roles: MicrosoftDocs/azure-docs, "Azure built-in roles
// for AI + machine learning". Other roles: the foundry-samples infrastructure templates.
const (
	roleFoundryAccountOwner    = "e47c6f54-e4a2-4754-9501-8e0985b135e1"
	roleFoundryProjectManager  = "eadc314b-1a2d-4efa-be10-5d325db5065e"
	roleFoundryUser            = "53ca6127-db72-4b80-b1b0-d745d6d5456d"
	roleStorageBlobContributor = "ba92f5b4-2d11-453d-a403-e96b0029c9fe"
	roleCosmosDBOperator       = "230815da-be43-4aae-9cb4-875f7bd000aa"
	roleSearchIndexContributor = "8ebe5a00-799e-43f5-93ac-243d3dce84a7"
	roleSearchServiceContrib   = "7ca78c08-252a-4471-8644-bb5ff32d4ba0"
	roleReader                 = "acdd72a7-3385-48ef-bd42-f606fba81ae7"
	roleMonitoringReader       = "43d0d8ad-25c7-4714-9337-8ba259a9fe05"
)

// roleNames are the display names of the built-in roles the generator assigns.
var roleNames = map[string]string{
	roleFoundryAccountOwner:    "Foundry Account Owner",
	roleFoundryProjectManager:  "Foundry Project Manager",
	roleFoundryUser:            "Foundry User",
	roleStorageBlobContributor: "Storage Blob Data Contributor",
	roleCosmosDBOperator:       "Cosmos DB Operator",
	roleSearchIndexContributor: "Search Index Data Contributor",
	roleSearchServiceContrib:   "Search Service Contributor",
	roleReader:                 "Reader",
	roleMonitoringReader:       "Monitoring Reader",
}

// ref is how generated code refers to a resource, whether it is created or already exists.
type ref struct {
	id, name, location string // Bicep expressions
	scope              string // Bicep scope expression, "" for the deployment's resource group
	armType            string // resource type, for the existing-resource declaration
	existing           bool
}

type gen struct {
	p        *plan.Plan
	n        *normalise.Config
	body     strings.Builder
	used     map[string]bool // Bicep symbols in use
	mods     map[string]bool // module files referenced
	syms     map[string]string
	refs     map[string]ref
	diags    []diag.Diagnostic
	deferred map[string]int
	raCount  int

	private, standard bool
	ipRules           []string
	locks             bool
	workspace         string // Bicep expression for the workspace id, "" when there is none
	agentSubnetID     string
	peSubnetID        string
	vnetID            string
	peModules         []string
	account           string // module symbol of the Foundry account
	chain             string // the last module that changed the Foundry account's children
	deploymentsModule string
}

// Generate renders the Bicep project for a plan.
func Generate(p *plan.Plan) (*Output, error) {
	g := &gen{
		p: p, n: p.Config, used: map[string]bool{}, mods: map[string]bool{}, syms: map[string]string{},
		refs: map[string]ref{}, deferred: map[string]int{},
	}
	n := g.n
	g.private = n.Network.Mode == "private"
	g.standard = n.AgentSetup == "standard"
	if n.Network.Mode == "restricted" {
		g.ipRules = n.Network.AllowedIPs
		if n.Network.ExistingVnetResourceID != "" {
			g.warn("XF205", "x-foundry.security.network", "restricted mode with an existing VNet: no virtual network rules are generated; only allowedIps are applied")
		}
	}
	if m := n.Governance; m != nil && m.Enabled && m.ResourceLocks {
		g.locks = true
	}
	g.checkScopes()
	if err := g.emit(); err != nil {
		return nil, err
	}
	if diag.HasErrors(g.diags) {
		return &Output{Diagnostics: diag.Dedupe(g.diags)}, nil
	}
	out := &Output{Diagnostics: diag.Dedupe(g.diags)}
	out.Files = append(out.Files,
		File{"main.bicep", []byte(g.mainBicep())},
		File{"resources.bicep", []byte(g.resourcesBicep())},
		File{"main.parameters.json", []byte(g.parametersJSON())},
	)
	modules := make([]string, 0, len(g.mods))
	for m := range g.mods {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	for _, m := range modules {
		b, err := fs.ReadFile(modulesFS, "modules/"+m+".bicep")
		if err != nil {
			return nil, err
		}
		out.Files = append(out.Files, File{"modules/" + m + ".bicep", b})
	}
	out.Deferred = g.deferredList()
	out.Files = append(out.Files, File{"README.md", []byte(g.readme(out))})
	return out, nil
}

func (g *gen) warn(code, path, format string, args ...any) {
	g.diags = append(g.diags, diag.Warn(code, path, format, args...))
}

func (g *gen) fail(code, path, format string, args ...any) {
	g.diags = append(g.diags, diag.Err(code, path, format, args...))
}

// ------------------------------------------------------------------------------ naming

func (g *gen) prefix() string {
	p := strings.ToLower(g.n.NamingPrefix)
	if p == "" {
		return "xf"
	}
	return p
}

func (g *gen) location() string {
	return strings.ToLower(strings.ReplaceAll(g.n.Location, " ", ""))
}

func (g *gen) resourceGroupName() string {
	if g.n.ResourceGroup != "" {
		return str(g.n.ResourceGroup)
	}
	return "'rg-${environmentName}'"
}

// checkScopes reports settings Phase 2 cannot honour.
func (g *gen) checkScopes() {
	if h := g.n.Hub; h != nil && h.ResourceGroup != "" && h.ResourceGroup != g.n.ResourceGroup {
		g.warn("XF204", "x-foundry.hub.resourceGroup", "resourceGroup '%s' is ignored: the Foundry resource, its projects and the shared resources are deployed to one resource group", h.ResourceGroup)
	}
	for _, p := range g.n.Projects {
		if p.ResourceGroup != "" && p.ResourceGroup != g.n.ResourceGroup {
			g.warn("XF204", "x-foundry.projects["+p.Name+"].resourceGroup", "resourceGroup '%s' is ignored: projects live in the Foundry resource's resource group", p.ResourceGroup)
		}
	}
}

// tags renders the tag expression for a resource: shared tags, the resource's own, and its node id.
func (g *gen) tags(id string, own ...config.Tags) string {
	merged := map[string]string{}
	for _, t := range own {
		for k, v := range t {
			merged[k] = v
		}
	}
	merged["x-foundry-id"] = id
	return "union(tags, " + oneLine(merged) + ")"
}

func oneLine(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = propName(k) + ": " + str(m[k])
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// ------------------------------------------------------------------------------ modules

type param = [2]string

func (g *gen) deployName(sym string) string {
	name := "xf-" + sym
	if len(name) <= 60 {
		return str(name)
	}
	sum := sha256.Sum256([]byte(sym))
	return str(name[:50] + "-" + hex.EncodeToString(sum[:])[:8])
}

// module writes a module call. deps are module symbols; those already implied by an output
// reference in the parameters are dropped so the Bicep linter stays quiet.
func (g *gen) module(sym, file, condition, scope string, params []param, deps []string) {
	g.mods[file] = true
	var b strings.Builder
	fmt.Fprintf(&b, "module %s 'modules/%s.bicep' = ", sym, file)
	if condition != "" {
		fmt.Fprintf(&b, "if (%s) ", condition)
	}
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  name: %s\n", g.deployName(sym))
	if scope != "" {
		fmt.Fprintf(&b, "  scope: %s\n", scope)
	}
	b.WriteString("  params: {\n")
	var text strings.Builder
	for _, kv := range params {
		fmt.Fprintf(&b, "    %s: %s\n", kv[0], kv[1])
		text.WriteString(kv[1])
	}
	b.WriteString("  }\n")
	var needed []string
	seen := map[string]bool{}
	for _, d := range deps {
		if d == "" || seen[d] || strings.Contains(text.String(), d+".outputs") {
			continue
		}
		seen[d] = true
		needed = append(needed, d)
	}
	sort.Strings(needed)
	if len(needed) > 0 {
		b.WriteString("  dependsOn: [\n")
		for _, d := range needed {
			fmt.Fprintf(&b, "    %s\n", d)
		}
		b.WriteString("  ]\n")
	}
	b.WriteString("}\n\n")
	g.body.WriteString(b.String())
}

// depsOf returns the module symbols the plan says a node depends on.
func (g *gen) depsOf(node plan.Node) []string {
	var out []string
	for _, d := range node.DependsOn {
		if s, ok := g.syms[d]; ok {
			out = append(out, s)
		}
	}
	return out
}

func (g *gen) newSym(id string) string {
	s := symbol(id, g.used)
	g.syms[id] = s
	return s
}

// ------------------------------------------------------------------------------ references

type armID struct{ sub, group, name string }

func parseARMID(id string) armID {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	var a armID
	for i := 0; i+1 < len(parts); i++ {
		switch strings.ToLower(parts[i]) {
		case "subscriptions":
			a.sub = parts[i+1]
		case "resourcegroups":
			a.group = parts[i+1]
		}
	}
	a.name = parts[len(parts)-1]
	return a
}

// existingRef declares an existing resource so its location can be read.
func (g *gen) existingRef(id, armType, apiVersion, resourceID string) ref {
	a := parseARMID(resourceID)
	sym := symbol(id+"_existing", g.used)
	scope := fmt.Sprintf("resourceGroup(%s, %s)", str(a.sub), str(a.group))
	fmt.Fprintf(&g.body, "resource %s '%s@%s' existing = {\n  name: %s\n  scope: %s\n}\n\n", sym, armType, apiVersion, str(a.name), scope)
	return ref{id: str(resourceID), name: str(a.name), location: sym + ".location", scope: scope, armType: armType, existing: true}
}

func (g *gen) createdRef(sym string) ref {
	return ref{id: sym + ".outputs.id", name: sym + ".outputs.name", location: sym + ".outputs.location"}
}

// ------------------------------------------------------------------------------ emit

func (g *gen) emit() error {
	g.workspace = ""
	g.networkRefs()
	var obsSym string
	names := g.names()
	for _, node := range g.p.Nodes {
		switch node.Kind {
		case "resource-group":
		case "identity":
			g.emitIdentity(node, names)
		case "observability":
			obsSym = g.emitObservability(node, names)
		case "network":
			g.emitNetwork(node, names)
		case "private-dns":
			g.emitPrivateDNS(node)
		case "storage":
			g.emitStorage(node, names)
		case "key-vault":
			g.emitKeyVault(node, names)
		case "cosmos":
			g.emitCosmos(node, names)
		case "search":
			g.emitSearch(node, names)
		case "foundry-account":
			g.emitAccount(node, names, obsSym)
		case "private-endpoint":
			g.emitPrivateEndpoint(node)
		case "model-deployment":
			g.emitDeployments(node)
		case "foundry-project":
			g.emitProject(node)
		case "capability-host":
			g.emitCapabilityHost(node)
		case "governance":
			g.noteGovernance()
		default:
			g.deferred[node.Kind]++
		}
	}
	g.emitPrincipals()
	return nil
}

// nameSet holds the Bicep expressions for generated resource names.
type nameSet struct{ foundry, storage, keyVault, search, cosmos, vnet, identity, workspace, appInsights string }

func (g *gen) names() nameSet {
	return nameSet{
		foundry:     "'${base}-${token}'",
		storage:     "'st${take(baseCompact, 9)}${take(token, 12)}'",
		keyVault:    "'kv-${take(baseCompact, 6)}-${take(token, 12)}'",
		search:      "'srch-${base}-${token}'",
		cosmos:      "'cosmos-${base}-${token}'",
		vnet:        "'vnet-${base}-${take(token, 6)}'",
		identity:    "'id-${base}-${take(token, 6)}'",
		workspace:   "'log-${base}-${take(token, 6)}'",
		appInsights: "'appi-${base}-${take(token, 6)}'",
	}
}

func pick(explicit, generated string) string {
	if explicit != "" {
		return str(explicit)
	}
	return generated
}

func (g *gen) emitIdentity(node plan.Node, nm nameSet) {
	m := g.n.ManagedIdentity
	if node.Existing {
		return
	}
	sym := g.newSym(node.ID)
	g.module(sym, "identity", "", "", []param{
		{"name", pick(m.Name, nm.identity)}, {"location", "location"}, {"tags", g.tags(node.ID, m.Tags)},
	}, g.depsOf(node))
}

func (g *gen) emitObservability(node plan.Node, nm nameSet) string {
	o := g.n.Observability
	sym := g.newSym(node.ID)
	appInsights := "''"
	if o.ApplicationInsights {
		appInsights = nm.appInsights
	}
	g.module(sym, "observability", "", "", []param{
		{"workspaceName", nm.workspace}, {"appInsightsName", appInsights}, {"location", "location"},
		{"tags", g.tags(node.ID, o.Tags)}, {"retentionDays", fmt.Sprint(o.RetentionDays)},
	}, g.depsOf(node))
	g.workspace = sym + ".outputs.workspaceId"
	return sym
}

// subnets splits the address space: the agent subnet first, then a /24 (or smaller) for private
// endpoints. It returns an error message when they do not fit.
func subnets(space string, agentLen int, withAgent bool) (agent, pe, problem string) {
	prefix, err := netip.ParsePrefix(space)
	if err != nil || !prefix.Addr().Is4() {
		return "", "", fmt.Sprintf("addressSpace '%s' must be an IPv4 CIDR range", space)
	}
	prefix = prefix.Masked()
	bits := prefix.Bits()
	base := ipv4(prefix.Addr())
	spaceEnd := base + (uint64(1) << (32 - bits))
	next := base
	if withAgent {
		agent = netip.PrefixFrom(prefix.Addr(), agentLen).String()
		next = base + (uint64(1) << (32 - agentLen))
	}
	peLen := 24
	if peLen < bits+1 {
		peLen = bits + 1
	}
	if peLen > 28 {
		peLen = 28
	}
	size := uint64(1) << (32 - peLen)
	start := (next + size - 1) / size * size
	if start+size > spaceEnd {
		return "", "", fmt.Sprintf("addressSpace %s has no room for a /%d private endpoint subnet after the agent subnet", space, peLen)
	}
	pe = netip.PrefixFrom(fromIPv4(start), peLen).String()
	return agent, pe, ""
}

func ipv4(a netip.Addr) uint64 {
	b := a.As4()
	return uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
}

func fromIPv4(v uint64) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func (g *gen) emitNetwork(node plan.Node, nm nameSet) {
	net := g.n.Network
	withAgent := g.standard && net.AgentSubnet == "create"
	agent, pe, problem := subnets(net.AddressSpace, net.AgentSubnetPrefixLength, withAgent)
	if problem != "" {
		g.fail("XF206", "x-foundry.security.network.addressSpace", "%s", problem)
		return
	}
	sym := g.newSym(node.ID)
	g.module(sym, "network", "", "", []param{
		{"name", nm.vnet}, {"location", "location"}, {"tags", g.tags(node.ID)}, {"addressSpace", str(net.AddressSpace)},
		{"agentSubnetPrefix", str(agent)}, {"peSubnetPrefix", str(pe)},
	}, g.depsOf(node))
	g.vnetID = sym + ".outputs.id"
	g.peSubnetID = sym + ".outputs.peSubnetId"
	if withAgent {
		g.agentSubnetID = sym + ".outputs.agentSubnetId"
	}
}

// networkRefs fills the subnet references for an existing VNet.
func (g *gen) networkRefs() {
	net := g.n.Network
	if net.VNet != "existing" {
		return
	}
	g.vnetID = str(net.ExistingVnetResourceID)
	g.peSubnetID = str(net.PrivateEndpointSubnetResourceID)
	if g.standard && net.AgentSubnetResourceID != "" {
		g.agentSubnetID = str(net.AgentSubnetResourceID)
	}
}

func (g *gen) emitPrivateDNS(node plan.Node) {
	g.networkRefs()
	sym := g.newSym(node.ID)
	g.module(sym, "private-dns", "", "", []param{
		{"zoneNames", zoneNames(g.n.Network.PrivateDNSZones)}, {"vnetId", g.vnetID}, {"tags", g.tags(node.ID)},
	}, g.depsOf(node))
}

func (g *gen) emitStorage(node plan.Node, nm nameSet) {
	s := g.n.Storage
	if node.Existing {
		g.refs[node.ID] = g.existingRef(node.ID, "Microsoft.Storage/storageAccounts", "2023-05-01", s.ExistingResourceID)
		return
	}
	g.networkRefs()
	sym := g.newSym(node.ID)
	containers := g.containers(s)
	g.module(sym, "storage", "", "", []param{
		{"name", pick(s.Name, nm.storage)}, {"location", "location"}, {"tags", g.tags(node.ID, s.Tags)},
		{"sku", str(s.SKU)}, {"hierarchicalNamespace", boolean(s.HierarchicalNamespace)},
		{"publicNetworkAccess", boolean(!g.private)}, {"ipRules", strs(g.ipRules)},
		{"allowSharedKeyAccess", boolean(s.LocalAuthentication)}, {"retentionDays", fmt.Sprint(s.RetentionDays)},
		{"containers", strs(containers)}, {"deleteLock", boolean(g.locks)}, {"workspaceId", g.workspaceExpr()},
	}, g.depsOf(node))
	g.refs[node.ID] = g.createdRef(sym)
}

func (g *gen) workspaceExpr() string {
	if g.workspace == "" {
		return "''"
	}
	return g.workspace
}

// containers lists the blob containers to create: one per declared purpose, the declared
// containers, and the containers named by blob and ADLS knowledge sources.
func (g *gen) containers(s *config.Storage) []string {
	set := map[string]bool{}
	for _, p := range s.Purposes {
		set[p] = true
	}
	for _, c := range s.Containers {
		set[c.Name] = true
		if c.PublicAccess != "" && c.PublicAccess != "none" {
			g.warn("XF207", "x-foundry.storage.containers["+c.Name+"].publicAccess", "container '%s' is created private: anonymous blob access is disabled on the storage account", c.Name)
		}
	}
	for _, sc := range g.n.Scopes {
		for _, kb := range sc.KnowledgeBases {
			for _, src := range kb.Sources {
				if (src.Type == "blob" || src.Type == "adls") && src.Connection == "" && src.Container != "" {
					set[src.Container] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func (g *gen) emitKeyVault(node plan.Node, nm nameSet) {
	k := g.n.KeyVault
	if node.Existing {
		g.refs[node.ID] = g.existingRef(node.ID, "Microsoft.KeyVault/vaults", "2024-11-01", k.ExistingResourceID)
		return
	}
	sym := g.newSym(node.ID)
	purge := g.n.PurgeProtection
	if k.Has("purgeProtection") {
		purge = k.PurgeProtection
	}
	g.module(sym, "keyvault", "", "", []param{
		{"name", pick(k.Name, nm.keyVault)}, {"location", "location"}, {"tags", g.tags(node.ID, k.Tags)},
		{"sku", str(k.SKU)}, {"softDeleteDays", fmt.Sprint(k.SoftDeleteDays)}, {"purgeProtection", boolean(purge)},
		{"publicNetworkAccess", boolean(!g.private)}, {"ipRules", strs(g.ipRules)}, {"workspaceId", g.workspaceExpr()},
	}, g.depsOf(node))
	g.refs[node.ID] = g.createdRef(sym)
}

func (g *gen) emitCosmos(node plan.Node, nm nameSet) {
	c := g.n.Cosmos
	if node.Existing {
		g.refs[node.ID] = g.existingRef(node.ID, "Microsoft.DocumentDB/databaseAccounts", "2024-11-15", c.ExistingResourceID)
		return
	}
	sym := g.newSym(node.ID)
	g.module(sym, "cosmos", "", "", []param{
		{"name", pick(c.Name, nm.cosmos)}, {"location", "location"}, {"tags", g.tags(node.ID, c.Tags)},
		{"capacityMode", str(c.CapacityMode)}, {"zoneRedundant", boolean(c.ZoneRedundant)},
		{"continuousBackup", boolean(c.ContinuousBackup)}, {"publicNetworkAccess", boolean(!g.private)},
		{"ipRules", strs(g.ipRules)}, {"localAuthentication", boolean(c.LocalAuthentication)},
		{"deleteLock", boolean(g.locks)}, {"workspaceId", g.workspaceExpr()},
	}, g.depsOf(node))
	g.refs[node.ID] = g.createdRef(sym)
}

func (g *gen) emitSearch(node plan.Node, nm nameSet) {
	sc := g.n.Scope(node.Scope)
	if sc == nil || sc.Search == nil {
		return
	}
	s := sc.Search
	if node.Existing {
		g.refs[node.ID] = g.existingRef(node.ID, "Microsoft.Search/searchServices", "2024-06-01-preview", s.ExistingResourceID)
		return
	}
	sym := g.newSym(node.ID)
	name := pick(s.Name, nm.search)
	if s.Name == "" && node.Scope != ids.RootScope {
		name = "'srch-${base}-" + ids.Slug(strings.TrimPrefix(node.Scope, "project:")) + "-${take(token, 6)}'"
	}
	g.module(sym, "search", "", "", []param{
		{"name", name}, {"location", "location"}, {"tags", g.tags(node.ID, s.Tags)}, {"sku", str(s.SKU)},
		{"replicas", fmt.Sprint(s.Replicas)}, {"partitions", fmt.Sprint(s.Partitions)},
		{"semanticRanking", boolean(s.SemanticRanking)}, {"localAuthentication", boolean(s.LocalAuthentication)},
		{"publicNetworkAccess", boolean(!g.private)}, {"ipRules", strs(g.ipRules)},
		{"systemIdentity", boolean(s.ManagedIdentity)}, {"deleteLock", boolean(g.locks)}, {"workspaceId", g.workspaceExpr()},
	}, g.depsOf(node))
	g.refs[node.ID] = g.createdRef(sym)
}

func (g *gen) emitAccount(node plan.Node, nm nameSet, obsSym string) {
	sym := g.newSym(node.ID)
	g.account = sym
	g.chain = sym
	g.module(sym, "foundry-account", "", "", []param{
		{"name", nm.foundry}, {"location", "location"}, {"tags", g.tags(node.ID)},
		{"publicNetworkAccess", boolean(!g.private)}, {"ipRules", strs(g.ipRules)},
		{"localAuthentication", boolean(g.n.LocalAuthentication)},
		{"agentSubnetId", g.agentSubnetExpr()}, {"workspaceId", g.workspaceExpr()},
	}, g.depsOf(node))
	g.refs[node.ID] = ref{id: sym + ".outputs.id", name: sym + ".outputs.name"}
	if o := g.n.Observability; o != nil && o.Enabled && o.ApplicationInsights && obsSym != "" {
		conn := symbol("foundry_appinsights_connection", g.used)
		g.module(conn, "foundry-appinsights-connection", "", "", []param{
			{"accountName", sym + ".outputs.name"}, {"appInsightsId", obsSym + ".outputs.appInsightsId"}, {"name", "'appinsights'"},
		}, nil)
		g.chain = conn
	}
}

func (g *gen) agentSubnetExpr() string {
	g.networkRefs()
	if g.private && g.standard && g.agentSubnetID != "" {
		return g.agentSubnetID
	}
	return "''"
}

// targetOf resolves a private-endpoint component to a reference.
func (g *gen) targetOf(component string) (ref, bool) {
	if component == ids.Foundry {
		r, ok := g.refs[ids.Foundry]
		return r, ok
	}
	r, ok := g.refs[component]
	return r, ok
}

func (g *gen) emitPrivateEndpoint(node plan.Node) {
	component := strings.TrimPrefix(node.ID, "private-endpoint:")
	group := component[strings.LastIndex(component, ":")+1:]
	component = component[:strings.LastIndex(component, ":")]
	target, ok := g.targetOf(component)
	if !ok {
		return
	}
	var zones []string
	for _, pe := range g.n.Network.PrivateEndpoints {
		if pe.Component == component && pe.Group == group && g.n.Network.PrivateDNS {
			for _, z := range pe.Zones {
				zones = append(zones, fmt.Sprintf("resourceId('Microsoft.Network/privateDnsZones', %s)", zoneName(z)))
			}
		}
	}
	zoneIDs := "[]"
	if len(zones) > 0 {
		zoneIDs = "[\n      " + strings.Join(zones, "\n      ") + "\n    ]"
	}
	sym := g.newSym(node.ID)
	deps := g.depsOf(node)
	g.module(sym, "private-endpoint", "", "", []param{
		{"name", fmt.Sprintf("'pe-${%s}-%s'", target.name, group)}, {"location", "location"}, {"tags", g.tags(node.ID)},
		{"subnetId", g.peSubnetID}, {"targetId", target.id}, {"groupId", str(group)}, {"zoneIds", zoneIDs},
	}, deps)
	g.peModules = append(g.peModules, sym)
}

// deploymentKey compares the settings that matter when the same name is declared twice.
func deploymentKey(d config.ModelDeployment) string {
	return strings.Join([]string{d.Model, d.Format, d.Version, d.SKU, fmt.Sprint(d.Capacity), d.RaiPolicy, d.VersionUpgradeOption}, "|")
}

func (g *gen) emitDeployments(node plan.Node) {
	if g.deploymentsModule != "" {
		return
	}
	seen := map[string]config.ModelDeployment{}
	declared := map[string]string{}
	var order []string
	for _, s := range g.n.Scopes {
		for _, d := range s.Models.Deployments {
			if d.Location != "" && d.Location != g.n.Location {
				g.warn("XF202", "x-foundry.deployments["+d.Name+"].location", "deployment '%s' location '%s' is ignored: deployments always run in the Foundry resource's region", d.Name, d.Location)
			}
			prev, dup := seen[d.Name]
			if dup {
				if deploymentKey(prev) != deploymentKey(d) {
					g.warn("XF203", "x-foundry.deployments["+d.Name+"]", "deployment '%s' is declared in %s and %s with different settings; the %s declaration is used because the Foundry resource has one deployment per name", d.Name, declared[d.Name], s.Scope, declared[d.Name])
				}
				continue
			}
			seen[d.Name] = d
			declared[d.Name] = s.Scope
			order = append(order, d.Name)
		}
	}
	if len(order) == 0 {
		return
	}
	var items []string
	for _, name := range order {
		d := seen[name]
		items = append(items, "      "+object("      ", [][2]string{
			{"name", str(d.Name)}, {"model", str(d.Model)}, {"format", str(d.Format)}, {"version", str(d.Version)},
			{"sku", str(d.SKU)}, {"capacity", fmt.Sprint(d.Capacity)}, {"versionUpgradeOption", str(d.VersionUpgradeOption)},
			{"raiPolicy", str(d.RaiPolicy)},
		}))
	}
	sym := symbol("model_deployments", g.used)
	g.syms[node.ID] = sym
	g.deploymentsModule = sym
	g.module(sym, "foundry-deployments", "", "", []param{
		{"accountName", g.account + ".outputs.name"}, {"deployments", "[\n" + strings.Join(items, "\n") + "\n    ]"},
	}, []string{g.chain})
	g.chain = sym
}

func (g *gen) emitProject(node plan.Node) {
	name := ""
	var tags config.Tags
	display, description := "", ""
	if node.Scope == ids.HubScope {
		if h := g.n.Hub; h != nil {
			name, tags, display = h.Name, h.Tags, h.Name
			description = "Shared hub project for " + h.Name
		}
	} else if p := g.n.Project(ids.ProjectName(node.Scope)); p != nil {
		name, tags, display, description = p.Name, p.Tags, p.DisplayName, p.Description
	}
	if name == "" {
		return
	}
	sym := g.newSym(node.ID)
	params := []param{
		{"accountName", g.account + ".outputs.name"}, {"name", str(name)}, {"location", "location"},
		{"displayName", str(display)}, {"projectDescription", str(description)}, {"tags", g.tags(node.ID, tags)},
	}
	var deps []string
	if g.standard && node.Scope != ids.HubScope {
		conns := g.connections(g.n.Project(ids.ProjectName(node.Scope)))
		if conns != "" {
			params = append(params, param{"connections", conns})
		}
		deps = append(deps, g.componentModules()...)
		deps = append(deps, g.peModules...)
	}
	g.module(sym, "foundry-project", "", "", params, append(deps, g.chain))
	g.chain = sym
}

// componentModules are the modules of the bring-your-own resources.
func (g *gen) componentModules() []string {
	var out []string
	for id, s := range g.syms {
		if id == ids.Storage || id == ids.Cosmos || strings.HasPrefix(id, "search:") {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func (g *gen) searchNode(p *normalise.EffectiveProject) (ref, bool) {
	if p == nil || p.SearchScope == "" {
		return ref{}, false
	}
	r, ok := g.refs[ids.SearchNode(p.SearchScope)]
	return r, ok
}

// connections renders the project connections to Cosmos DB, Storage and Search.
func (g *gen) connections(p *normalise.EffectiveProject) string {
	var items []string
	add := func(name, category, target, resourceID, location string) {
		items = append(items, "      "+object("      ", [][2]string{
			{"name", name}, {"category", str(category)}, {"target", target}, {"resourceId", resourceID}, {"location", location},
		}))
	}
	if c, ok := g.refs[ids.Cosmos]; ok {
		add(c.name, "CosmosDB", "'https://${"+c.name+"}.documents.azure.com:443/'", c.id, c.location)
	}
	if s, ok := g.refs[ids.Storage]; ok {
		add(s.name, "AzureStorageAccount", "'https://${"+s.name+"}.blob.${environment().suffixes.storage}/'", s.id, s.location)
	}
	if s, ok := g.searchNode(p); ok {
		add(s.name, "CognitiveSearch", "'https://${"+s.name+"}.search.windows.net'", s.id, s.location)
	}
	if len(items) == 0 {
		return ""
	}
	return "[\n" + strings.Join(items, "\n") + "\n    ]"
}

// role emits a role assignment module and returns its symbol. The label names the role and
// who gets it; it becomes a comment above the module and, where the resource supports it, the
// description of the role assignment, so the GUIDs are readable.
func (g *gen) role(kind, label string, params []param, scope string, deps []string) string {
	g.raCount++
	sym := symbol(fmt.Sprintf("ra_%03d", g.raCount), g.used)
	g.body.WriteString("// " + label + "\n")
	if kind != "ra-cosmos-sql" {
		params = append(params, param{"assignmentDescription", str(label)})
	}
	g.module(sym, kind, "", scope, params, deps)
	return sym
}

// roleLabel is "<role name> for <who>".
func roleLabel(roleID, who string) string {
	name := roleNames[roleID]
	if name == "" {
		name = roleID
	}
	return name + " for " + who
}

// resourceScope is the scope expression for a role assignment on a resource.
func resourceScope(r ref) string { return r.scope }

func (g *gen) emitCapabilityHost(node plan.Node) {
	p := g.n.Project(ids.ProjectName(node.Scope))
	if p == nil {
		return
	}
	projectSym := g.syms[ids.ProjectNode(node.Scope)]
	if projectSym == "" {
		return
	}
	cosmos, cok := g.refs[ids.Cosmos]
	storage, sok := g.refs[ids.Storage]
	search, hok := g.searchNode(p)
	if !cok || !sok || !hok {
		return
	}
	principal := projectSym + ".outputs.principalId"
	who := "the " + p.Name + " project identity"
	base := []param{{"principalId", principal}, {"principalType", "'ServicePrincipal'"}}
	with := func(extra ...param) []param { return append(append([]param{}, extra...), base...) }
	var before []string
	before = append(before,
		g.role("ra-storage", roleLabel(roleStorageBlobContributor, who), with(param{"storageName", storage.name}, param{"roleId", str(roleStorageBlobContributor)}), resourceScope(storage), nil),
		g.role("ra-cosmos", roleLabel(roleCosmosDBOperator, who), with(param{"cosmosName", cosmos.name}, param{"roleId", str(roleCosmosDBOperator)}), resourceScope(cosmos), nil),
		g.role("ra-search", roleLabel(roleSearchIndexContributor, who), with(param{"searchName", search.name}, param{"roleId", str(roleSearchIndexContributor)}), resourceScope(search), nil),
		g.role("ra-search", roleLabel(roleSearchServiceContrib, who), with(param{"searchName", search.name}, param{"roleId", str(roleSearchServiceContrib)}), resourceScope(search), nil),
	)
	deps := append(append([]string{}, before...), g.peModules...)
	for _, d := range g.depsOf(node) {
		// The private endpoints already depend on the network.
		if d != g.syms[ids.Network] || len(g.peModules) == 0 {
			deps = append(deps, d)
		}
	}
	capSym := g.newSym(node.ID)
	g.module(capSym, "foundry-capability-host", "", "", []param{
		{"accountName", g.account + ".outputs.name"}, {"projectName", projectSym + ".outputs.name"},
		{"threadStorageConnection", cosmos.name}, {"storageConnection", storage.name}, {"vectorStoreConnection", search.name},
	}, append(deps, g.chain))
	g.chain = capSym
	workspace := projectSym + ".outputs.workspaceId"
	g.role("ra-storage-containers", "Storage Blob Data Owner (limited to the project's agent containers) for "+who, []param{{"storageName", storage.name}, {"principalId", principal}, {"workspaceId", workspace}}, resourceScope(storage), []string{capSym})
	g.role("ra-cosmos-sql", "Cosmos DB Built-in Data Contributor (limited to enterprise_memory) for "+who, []param{{"cosmosName", cosmos.name}, {"principalId", principal}, {"workspaceId", workspace}}, resourceScope(cosmos), []string{capSym})
}

// ------------------------------------------------------------------------------ principals

func armPrincipalType(t string) string {
	switch t {
	case "user":
		return "User"
	case "group":
		return "Group"
	}
	return "ServicePrincipal"
}

// emitPrincipals assigns Foundry and monitoring roles to the configured principals. A principal
// that is only a display name cannot be assigned in Bicep, so it is reported instead.
func (g *gen) emitPrincipals() {
	if g.account == "" {
		return
	}
	reported := map[string]bool{}
	usable := func(list []config.Principal, role string) []config.Principal {
		var out []config.Principal
		for _, p := range list {
			if p.ID == "" {
				if !reported[p.Key()] {
					reported[p.Key()] = true
					g.warn("XF201", "x-foundry.security.roles", "principal '%s' has no object ID, so no role assignment is generated; use its object ID (id) to assign %s", p.Name, role)
				}
				continue
			}
			out = append(out, p)
		}
		return out
	}
	account := g.account + ".outputs.name"
	for _, p := range usable(g.n.Roles.Admins, "Foundry Account Owner") {
		g.role("ra-account", roleLabel(roleFoundryAccountOwner, "admin "+p.Key()), []param{{"accountName", account}, {"principalId", str(p.ID)}, {"principalType", str(armPrincipalType(p.Type))}, {"roleId", str(roleFoundryAccountOwner)}}, "", nil)
	}
	for _, p := range g.n.Projects {
		projectSym := g.syms[ids.ProjectNode(ids.ProjectScope(p.Name))]
		if projectSym == "" {
			continue
		}
		pa := []param{{"accountName", account}, {"projectName", projectSym + ".outputs.name"}}
		rootAdmins := map[string]bool{}
		for _, a := range g.n.Roles.Admins {
			rootAdmins[a.Key()] = true
		}
		var projectAdmins []config.Principal
		for _, a := range p.Roles.Admins {
			if !rootAdmins[a.Key()] {
				projectAdmins = append(projectAdmins, a)
			}
		}
		for _, pr := range usable(projectAdmins, "Foundry Project Manager") {
			g.role("ra-project", roleLabel(roleFoundryProjectManager, "admin "+pr.Key()+" of project "+p.Name), append(append([]param{}, pa...), param{"principalId", str(pr.ID)}, param{"principalType", str(armPrincipalType(pr.Type))}, param{"roleId", str(roleFoundryProjectManager)}), "", nil)
		}
		for _, pr := range usable(p.Roles.Developers, "Foundry User") {
			g.role("ra-project", roleLabel(roleFoundryUser, "developer "+pr.Key()+" of project "+p.Name), append(append([]param{}, pa...), param{"principalId", str(pr.ID)}, param{"principalType", str(armPrincipalType(pr.Type))}, param{"roleId", str(roleFoundryUser)}), "", nil)
		}
	}
	seen := map[string]bool{}
	var operators []config.Principal
	add := func(list []config.Principal) {
		for _, p := range list {
			if !seen[p.Key()] {
				seen[p.Key()] = true
				operators = append(operators, p)
			}
		}
	}
	add(g.n.Roles.Operators)
	for _, p := range g.n.Projects {
		add(p.Roles.Operators)
	}
	for _, pr := range usable(operators, "Reader and Monitoring Reader") {
		for _, role := range []string{roleReader, roleMonitoringReader} {
			g.role("ra-resource-group", roleLabel(role, "operator "+pr.Key()), []param{{"principalId", str(pr.ID)}, {"principalType", str(armPrincipalType(pr.Type))}, {"roleId", str(role)}}, "", nil)
		}
	}
	// The identity running the deployment (azd sets principalId) can use the Foundry resource.
	g.mods["ra-account"] = true
	g.raCount++
	sym := symbol(fmt.Sprintf("ra_%03d", g.raCount), g.used)
	label := roleLabel(roleFoundryUser, "the identity running the deployment")
	g.body.WriteString("// " + label + "\n")
	g.module(sym, "ra-account", "!empty(principalId)", "", []param{
		{"accountName", account}, {"principalId", "principalId"}, {"principalType", "principalType"}, {"roleId", str(roleFoundryUser)},
		{"assignmentDescription", str(label)},
	}, nil)
}

// ------------------------------------------------------------------------------ governance

func (g *gen) noteGovernance() {
	gv := g.n.Governance
	if gv == nil {
		return
	}
	if len(gv.PolicyAssignments) > 0 || len(gv.DefenderPlans) > 0 || gv.Budgets.MonthlyAmount > 0 || gv.Budgets.MonthlyTokens > 0 {
		g.deferred["governance (policy assignments, Defender plans, budgets)"]++
	}
}

func (g *gen) deferredList() []string {
	keys := make([]string, 0, len(g.deferred))
	for k := range g.deferred {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s: %d", deferredLabel(k), g.deferred[k]))
	}
	return out
}

func deferredLabel(kind string) string {
	switch kind {
	case "governance (policy assignments, Defender plans, budgets)":
		return kind + " (Phase 5)"
	case "connector", "mcp", "knowledge-base", "toolbox", "agent":
		return kind + " (data plane, Phase 3 and 4)"
	case "evaluation":
		return "evaluation (data plane, Phase 3)"
	case "gateway":
		return "gateway (API Management, Phase 5)"
	case "alerts":
		return "alerts (Phase 5)"
	}
	return kind + " (later phase)"
}
