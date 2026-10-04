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

// Built-in role definition GUIDs (the foundry-samples infrastructure templates and the Azure
// built-in roles reference).
const (
	roleReader           = "acdd72a7-3385-48ef-bd42-f606fba81ae7"
	roleMonitoringReader = "43d0d8ad-25c7-4714-9337-8ba259a9fe05"
)

// roleNames are the display names of the built-in roles the generator assigns.
var roleNames = map[string]string{
	roleReader:           "Reader",
	roleMonitoringReader: "Monitoring Reader",
}

// ref is how generated code refers to a resource, whether it is created or already exists.
type ref struct {
	id, name string // Bicep expressions
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
	outputs  [][2]string // name, Bicep expression

	private, standard bool
	ipRules           []string
	locks             bool
	workspace         string // Bicep expression for the workspace id, "" when there is none
	peSubnetID        string
	vnetID            string
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

// Subnet names in the generated VNet; azd refers to them by name.
const (
	agentSubnetName = "agent-subnet"
	peSubnetName    = "pe-subnet"
)

// output records a value for azd to pick up from the deployment outputs.
func (g *gen) output(name, expr string) {
	for _, o := range g.outputs {
		if o[0] == name {
			return
		}
	}
	g.outputs = append(g.outputs, [2]string{name, expr})
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

// existingRef refers to a resource that already exists by its ARM ID; nothing is declared for it.
func (g *gen) existingRef(resourceID string) ref {
	return ref{id: str(resourceID), name: str(parseARMID(resourceID).name)}
}

func (g *gen) createdRef(sym string) ref {
	return ref{id: sym + ".outputs.id", name: sym + ".outputs.name"}
}

// ------------------------------------------------------------------------------ emit

func (g *gen) emit() error {
	g.workspace = ""
	g.networkRefs()
	names := g.names()
	for _, node := range g.p.Nodes {
		switch node.Kind {
		case "resource-group":
		case "identity":
			g.emitIdentity(node, names)
		case "observability":
			g.emitObservability(node, names)
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
		case "private-endpoint":
			g.emitPrivateEndpoint(node)
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
type nameSet struct{ storage, keyVault, search, cosmos, vnet, identity, workspace, appInsights string }

func (g *gen) names() nameSet {
	return nameSet{
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

func (g *gen) emitObservability(node plan.Node, nm nameSet) {
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
	g.output("LOG_ANALYTICS_WORKSPACE_ID", g.workspace)
	if o.ApplicationInsights {
		g.output("APPLICATIONINSIGHTS_RESOURCE_ID", sym+".outputs.appInsightsId")
	}
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
		{"agentSubnetName", str(agentSubnetName)}, {"agentSubnetPrefix", str(agent)},
		{"peSubnetName", str(peSubnetName)}, {"peSubnetPrefix", str(pe)},
	}, g.depsOf(node))
	g.vnetID = sym + ".outputs.id"
	g.peSubnetID = sym + ".outputs.peSubnetId"
	g.output("VNET_RESOURCE_ID", g.vnetID)
	g.output("PE_SUBNET_NAME", str(peSubnetName))
	if withAgent {
		g.output("AGENT_SUBNET_NAME", str(agentSubnetName))
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
	g.output("VNET_RESOURCE_ID", g.vnetID)
	g.output("PE_SUBNET_NAME", str(parseARMID(net.PrivateEndpointSubnetResourceID).name))
	if g.standard && net.AgentSubnetResourceID != "" {
		g.output("AGENT_SUBNET_NAME", str(parseARMID(net.AgentSubnetResourceID).name))
	}
}

func (g *gen) emitPrivateDNS(node plan.Node) {
	g.networkRefs()
	sym := g.newSym(node.ID)
	g.module(sym, "private-dns", "", "", []param{
		{"zoneNames", zoneNames(g.n.Network.PrivateDNSZones)}, {"vnetId", g.vnetID}, {"tags", g.tags(node.ID)},
	}, g.depsOf(node))
	// azd creates the Foundry resource's private endpoint; pointing it at these zones
	// (azure.ai.project network.dns) keeps all DNS in one place.
	g.output("PRIVATE_DNS_RESOURCE_GROUP", "resourceGroup().name")
	g.output("PRIVATE_DNS_SUBSCRIPTION_ID", "subscription().subscriptionId")
}

func (g *gen) emitStorage(node plan.Node, nm nameSet) {
	s := g.n.Storage
	if node.Existing {
		g.refs[node.ID] = g.existingRef(s.ExistingResourceID)
		g.output("STORAGE_ACCOUNT_RESOURCE_ID", g.refs[node.ID].id)
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
	g.output("STORAGE_ACCOUNT_RESOURCE_ID", g.refs[node.ID].id)
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
		g.refs[node.ID] = g.existingRef(k.ExistingResourceID)
		g.output("KEY_VAULT_RESOURCE_ID", g.refs[node.ID].id)
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
	g.output("KEY_VAULT_RESOURCE_ID", g.refs[node.ID].id)
}

func (g *gen) emitCosmos(node plan.Node, nm nameSet) {
	c := g.n.Cosmos
	if node.Existing {
		g.refs[node.ID] = g.existingRef(c.ExistingResourceID)
		g.output("COSMOS_DB_RESOURCE_ID", g.refs[node.ID].id)
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
	g.output("COSMOS_DB_RESOURCE_ID", g.refs[node.ID].id)
}

func (g *gen) emitSearch(node plan.Node, nm nameSet) {
	sc := g.n.Scope(node.Scope)
	if sc == nil || sc.Search == nil {
		return
	}
	s := sc.Search
	if node.Existing {
		g.refs[node.ID] = g.existingRef(s.ExistingResourceID)
		g.output(searchOutput(node.Scope), g.refs[node.ID].id)
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
	g.output(searchOutput(node.Scope), g.refs[node.ID].id)
}

// searchOutput names the deployment output for the Search service of a scope.
func searchOutput(scope string) string {
	switch scope {
	case ids.RootScope:
		return "AI_SEARCH_RESOURCE_ID"
	case ids.HubScope:
		return "AI_SEARCH_HUB_RESOURCE_ID"
	}
	return "AI_SEARCH_" + strings.ToUpper(strings.ReplaceAll(ids.Slug(ids.ProjectName(scope)), "-", "_")) + "_RESOURCE_ID"
}

func (g *gen) emitPrivateEndpoint(node plan.Node) {
	component := strings.TrimPrefix(node.ID, "private-endpoint:")
	group := component[strings.LastIndex(component, ":")+1:]
	component = component[:strings.LastIndex(component, ":")]
	target, ok := g.refs[component]
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
	g.module(sym, "private-endpoint", "", "", []param{
		{"name", fmt.Sprintf("'pe-${%s}-%s'", target.name, group)}, {"location", "location"}, {"tags", g.tags(node.ID)},
		{"subnetId", g.peSubnetID}, {"targetId", target.id}, {"groupId", str(group)}, {"zoneIds", zoneIDs},
	}, g.depsOf(node))
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

func armPrincipalType(t string) string {
	switch t {
	case "user":
		return "User"
	case "group":
		return "Group"
	}
	return "ServicePrincipal"
}

// emitPrincipals assigns the read roles of the operators on the resource group. The Foundry
// roles (account owner, project manager, user) apply to the Foundry resource and its projects,
// which azd creates, so they are not generated. A principal that is only a display name cannot
// be assigned in Bicep, so it is reported instead.
func (g *gen) emitPrincipals() {
	r := g.n.Roles
	if len(r.Admins)+len(r.Developers) > 0 {
		g.deferred["foundry role assignments"]++
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
	add(r.Operators)
	for _, p := range g.n.Projects {
		add(p.Roles.Operators)
	}
	for _, p := range operators {
		if p.ID == "" {
			g.warn("XF201", "x-foundry.security.roles", "principal '%s' has no object ID, so no role assignment is generated; use its object ID (id) to assign Reader and Monitoring Reader", p.Name)
			continue
		}
		for _, role := range []string{roleReader, roleMonitoringReader} {
			g.role("ra-resource-group", roleLabel(role, "operator "+p.Key()), []param{
				{"principalId", str(p.ID)}, {"principalType", str(armPrincipalType(p.Type))}, {"roleId", str(role)},
			}, "", nil)
		}
	}
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
	case "knowledge-base":
		return "knowledge-base (data plane on the Search service, Phase 4)"
	case "gateway":
		return "gateway (API Management, Phase 5)"
	case "alerts":
		return "alerts (Phase 5)"
	case "governance (policy assignments, Defender plans, budgets)":
		return kind + " (Phase 5)"
	case "foundry role assignments":
		return "role assignments for admins and developers on the Foundry resource and projects (they need the resources azd creates)"
	}
	return kind + " (later phase)"
}
