package validate

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/azurenames"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
)

// Declared runs every rule that needs the configuration exactly as authored.
func Declared(cfg *config.XFoundry) []diag.Diagnostic {
	rules := []func(*config.XFoundry) []diag.Diagnostic{
		projectsAndHub, uniqueNames, projectReferences, modelSets, secrets, existingVsCreated,
		explicitNames, locations, securityRules, ipRules, insecureEndpoints, searchSizing,
		gatewayNetwork, cronRules, networkAddressing, agentServiceRules, identityRules,
	}
	var out []diag.Diagnostic
	for _, rule := range rules {
		out = append(out, rule(cfg)...)
	}
	return diag.Dedupe(out)
}

// ---------------------------------------------------------------- rules 1-5 and 16

func projectsAndHub(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, name := range duplicates(cfg.Projects) {
		out = append(out, diag.Err("XF001", path("projects"), "project name '%s' is used more than once", name))
	}
	if cfg.Hub != nil {
		for _, p := range cfg.Projects {
			if p.Name == cfg.Hub.Name {
				out = append(out, diag.Err("XF001", path("hub", "name"), "hub name '%s' collides with a project name", cfg.Hub.Name))
			}
		}
	}
	switch {
	case cfg.Hub != nil && cfg.Topology.Mode != "hub-spoke":
		out = append(out, diag.Err("XF003", path("hub"), "'hub' is only valid when topology.mode is 'hub-spoke'"))
	case cfg.Hub == nil && cfg.Topology.Mode == "hub-spoke":
		out = append(out, diag.Err("XF003", path("hub"), "topology.mode 'hub-spoke' requires a 'hub' section"))
	}
	// Rule 4: only an explicit inheritHub: true is a contradiction; the default resolves to false.
	if cfg.Hub == nil {
		for _, p := range cfg.Projects {
			if p.Has("inheritHub") && p.InheritHub {
				out = append(out, diag.Err("XF004", path("projects["+p.Name+"]", "inheritHub"),
					"project '%s' sets inheritHub: true but there is no hub", p.Name))
			}
		}
	}
	return out
}

func rootKBs(cfg *config.XFoundry) (list []config.KnowledgeBase, owner []string) {
	if cfg.IQ == nil {
		return nil, nil
	}
	for _, kb := range cfg.IQ.KnowledgeBases {
		list = append(list, kb)
		project := kb.Project
		if project == "" {
			project = cfg.IQ.Project
		}
		owner = append(owner, project)
	}
	return list, owner
}

func uniqueNames(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	projectNames := map[string]bool{}
	for _, p := range cfg.Projects {
		projectNames[p.Name] = true
	}
	check := func(label, where string, names []string) {
		counts := map[string]int{}
		for _, n := range names {
			counts[n]++
		}
		var dups []string
		for n, c := range counts {
			if c > 1 {
				dups = append(dups, n)
			}
		}
		sort.Strings(dups)
		for _, d := range dups {
			out = append(out, diag.Err("XF002", where, "duplicate %s name '%s'", label, d))
		}
	}
	unassigned := func(project string) bool { return !projectNames[project] }

	kbList, kbOwner := rootKBs(cfg)
	var rootOnly []string
	for i, kb := range kbList {
		if unassigned(kbOwner[i]) {
			rootOnly = append(rootOnly, kb.Name)
		}
	}
	check("knowledge base", path("iq", "knowledgeBases"), rootOnly)
	if h := cfg.Hub; h != nil {
		hp := root + ".hub"
		if h.IQ != nil {
			check("knowledge base", hp+".iq.knowledgeBases", names(h.IQ.KnowledgeBases))
		}
	}
	for _, p := range cfg.Projects {
		pp := fmt.Sprintf("%s.projects[%s]", root, p.Name)
		var kbs []string
		if p.IQ != nil {
			kbs = names(p.IQ.KnowledgeBases)
		}
		for i, kb := range kbList {
			if kbOwner[i] == p.Name {
				kbs = append(kbs, kb.Name)
			}
		}
		check("knowledge base", pp+".iq.knowledgeBases", kbs)
	}
	for _, h := range scopes(cfg) {
		for _, kb := range h.kbs() {
			kp := fmt.Sprintf("%s.iq.knowledgeBases[%s]", h.path, kb.Name)
			check("knowledge source", kp+".sources", names(kb.Sources))
			check("index field", kp+".index.fields", names(kb.Index.Fields))
			check("route", kp+".routing.routes", names(kb.Routing.Routes))
			check("scoring profile", kp+".index.scoringProfiles", names(kb.Index.ScoringProfiles))
		}
	}
	if g := cfg.Gateway; g != nil {
		gp := path("gateway")
		check("gateway endpoint", gp+".endpoints", names(g.Endpoints))
		paths := map[string]int{}
		for _, e := range g.Endpoints {
			paths[e.Path]++
		}
		var dup []string
		for p, n := range paths {
			if n > 1 {
				dup = append(dup, p)
			}
		}
		sort.Strings(dup)
		for _, p := range dup {
			out = append(out, diag.Err("XF013", gp+".endpoints", "gateway endpoint path '%s' is used more than once", p))
		}
		check("quota profile", gp+".quotas.profiles", names(g.Quotas.Profiles))
		check("limit profile", gp+".limits.profiles", names(g.Limits.Profiles))
		check("backend", gp+".routing.backends", names(g.Routing.Backends))
	}
	if cfg.Storage != nil {
		check("storage container", path("storage", "containers"), names(cfg.Storage.Containers))
	}
	if cfg.ManagedIdentity != nil {
		check("federated credential", path("managedIdentity", "federatedCredentials"), names(cfg.ManagedIdentity.FederatedCredentials))
	}
	return out
}

func names[T named](items []T) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.GetName()
	}
	return out
}

func projectReferences(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	known := map[string]bool{}
	for _, p := range cfg.Projects {
		known[p.Name] = true
	}
	check := func(ref, where, owner string) {
		switch {
		case ref == "":
		case !known[ref]:
			out = append(out, diag.Err("XF005", where, "unknown project '%s'", ref))
		case owner != "" && ref != owner:
			out = append(out, diag.Err("XF005", where, "declared inside project '%s' but assigned to project '%s'", owner, ref))
		}
	}
	for _, h := range scopes(cfg) {
		owner := h.project
		if h.iq != nil {
			check(h.iq.Project, h.path+".iq.project", owner)
			for _, kb := range h.iq.KnowledgeBases {
				base := fmt.Sprintf("%s.iq.knowledgeBases[%s]", h.path, kb.Name)
				check(kb.Project, base+".project", owner)
				for _, r := range kb.Routing.Routes {
					check(r.When.Project, fmt.Sprintf("%s.routing.routes[%s].when.project", base, r.Name), "")
				}
			}
		}
	}
	if cfg.Gateway != nil {
		for _, e := range cfg.Gateway.Endpoints {
			check(e.Project, path("gateway", "endpoints["+e.Name+"]", "project"), "")
		}
	}
	return out
}

func overlap(label string, allowed, denied []string, where string) []diag.Diagnostic {
	var both []string
	for _, a := range allowed {
		if contains(denied, a) {
			both = append(both, a)
		}
	}
	if len(both) == 0 {
		return nil
	}
	sort.Strings(both)
	return []diag.Diagnostic{diag.Err("XF016", where, "%s lists %s as both allowed and denied", label, strings.Join(both, ", "))}
}

func modelSets(cfg *config.XFoundry) []diag.Diagnostic {
	out := overlap("models", cfg.Models.Allowed, cfg.Models.Denied, path("models"))
	if cfg.Hub != nil {
		out = append(out, overlap("hub models", cfg.Hub.Models.Allowed, cfg.Hub.Models.Denied, path("hub", "models"))...)
	}
	for _, p := range cfg.Projects {
		out = append(out, overlap(fmt.Sprintf("project '%s' models", p.Name), p.Models.Allowed, p.Models.Denied, path("projects["+p.Name+"]", "models"))...)
	}
	if cfg.Gateway != nil {
		m := cfg.Gateway.Models
		out = append(out, overlap("gateway models", m.Allowed, m.Denied, path("gateway", "models"))...)
	}
	if cfg.Governance != nil {
		mp := cfg.Governance.ModelPolicy
		out = append(out, overlap("modelPolicy", mp.AllowedModels, mp.DeniedModels, path("governance", "modelPolicy"))...)
	}
	return out
}

// ------------------------------------------------------------------------ rule 17

func secrets(cfg *config.XFoundry) []diag.Diagnostic {
	b, _ := json.Marshal(cfg)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	return diag.Dedupe(findRawSecrets(doc))
}

// ------------------------------------------------------------------------ rule 22

type existingKind struct {
	armType    string
	createOnly []string
}

var existingKinds = map[string]existingKind{
	"search":          {"Microsoft.Search/searchServices", []string{"sku", "replicas", "partitions", "semanticRanking", "localAuthentication", "publicNetworkAccess", "managedIdentity", "tags"}},
	"storage":         {"Microsoft.Storage/storageAccounts", []string{"sku", "hierarchicalNamespace", "publicNetworkAccess", "localAuthentication", "retentionDays", "tags"}},
	"keyVault":        {"Microsoft.KeyVault/vaults", []string{"sku", "rbacAuthorisation", "softDeleteDays", "purgeProtection", "publicNetworkAccess", "tags"}},
	"cosmos":          {"Microsoft.DocumentDB/databaseAccounts", []string{"capacityMode", "throughput", "zoneRedundant", "continuousBackup", "publicNetworkAccess", "localAuthentication", "tags"}},
	"managedIdentity": {"Microsoft.ManagedIdentity/userAssignedIdentities", []string{"tags"}},
}

func armPattern(armType string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)^/subscriptions/[^/]+/resourceGroups/[^/]+/providers/` + armType + `/[^/]+$`)
}

// component is a platform component with the settings shared by several rules.
type component struct {
	kind      string
	path      string
	tracked   config.Tracked
	existing  string
	public    bool
	hasPublic bool
	local     bool
	hasLocal  bool
}

func components(cfg *config.XFoundry) []component {
	var out []component
	for _, h := range scopes(cfg) {
		if s := h.search; s != nil {
			out = append(out, component{"search", h.path + ".search", s.Tracked, s.ExistingResourceID, s.PublicNetworkAccess, true, s.LocalAuthentication, true})
		}
	}
	if s := cfg.Storage; s != nil {
		out = append(out, component{"storage", path("storage"), s.Tracked, s.ExistingResourceID, s.PublicNetworkAccess, true, s.LocalAuthentication, true})
	}
	if k := cfg.KeyVault; k != nil {
		out = append(out, component{"keyVault", path("keyVault"), k.Tracked, k.ExistingResourceID, k.PublicNetworkAccess, true, false, false})
	}
	if c := cfg.Cosmos; c != nil {
		out = append(out, component{"cosmos", path("cosmos"), c.Tracked, c.ExistingResourceID, c.PublicNetworkAccess, true, c.LocalAuthentication, true})
	}
	if m := cfg.ManagedIdentity; m != nil {
		out = append(out, component{"managedIdentity", path("managedIdentity"), m.Tracked, m.ExistingResourceID, false, false, false, false})
	}
	return out
}

func existingVsCreated(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, c := range components(cfg) {
		if c.existing == "" {
			continue
		}
		kind := existingKinds[c.kind]
		if !armPattern(kind.armType).MatchString(c.existing) {
			out = append(out, diag.Err("XF022", c.path+".existingResourceId",
				"existingResourceId is not a %s resource ID", kind.armType))
		}
		for _, key := range kind.createOnly {
			if c.tracked.Has(key) {
				out = append(out, diag.Err("XF022", c.path+"."+key,
					"'%s' configures an extension-created resource and cannot be combined with existingResourceId", key))
			}
		}
	}
	return out
}

// ------------------------------------------------------------------------ rule 24

func explicitNames(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	check := func(kind, name, where string) {
		if name == "" {
			return
		}
		for _, problem := range azurenames.Problems(kind, name) {
			out = append(out, diag.Err("XF024", where, "'%s' is not a valid %s name: %s", name, kind, problem))
		}
	}
	for _, h := range scopes(cfg) {
		if h.search != nil {
			check("search", h.search.Name, h.path+".search.name")
		}
	}
	if s := cfg.Storage; s != nil {
		check("storage", s.Name, path("storage", "name"))
		for _, c := range s.Containers {
			check("storage-container", c.Name, path("storage", "containers["+c.Name+"]", "name"))
		}
	}
	if k := cfg.KeyVault; k != nil {
		check("key-vault", k.Name, path("keyVault", "name"))
	}
	if c := cfg.Cosmos; c != nil {
		check("cosmos", c.Name, path("cosmos", "name"))
	}
	if m := cfg.ManagedIdentity; m != nil {
		check("managed-identity", m.Name, path("managedIdentity", "name"))
	}
	if g := cfg.Gateway; g != nil {
		check("apim", g.Name, path("gateway", "name"))
	}
	check("resource-group", cfg.Defaults.ResourceGroup, path("defaults", "resourceGroup"))
	for _, h := range scopes(cfg) {
		for _, kb := range h.kbs() {
			for _, s := range kb.Sources {
				if s.Type == "blob" || s.Type == "adls" {
					check("storage-container", s.Container, fmt.Sprintf("%s.iq.knowledgeBases[%s].sources[%s].container", h.path, kb.Name, s.Name))
				}
			}
		}
	}
	return out
}

// ------------------------------------------------------------------------ rule 23

func locations(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	type located struct{ location, path string }
	var all []located
	add := func(location, where string) {
		if location != "" {
			all = append(all, located{location, where})
		}
	}
	add(cfg.Defaults.Location, path("defaults", "location"))
	var residency []string
	if cfg.Governance != nil {
		residency = cfg.Governance.DataResidency
	}
	allowed := map[string]bool{}
	for i, r := range residency {
		if !isKnownRegion(r) {
			out = append(out, diag.Err("XF023", fmt.Sprintf("%s[%d]", path("governance", "dataResidency"), i), "'%s' is not a known Azure region", r))
		}
		allowed[canonicalRegion(r)] = true
	}
	for _, l := range all {
		switch {
		case !isKnownRegion(l.location):
			out = append(out, diag.Err("XF023", l.path, "'%s' is not a known Azure region", l.location))
		case len(allowed) > 0 && !allowed[canonicalRegion(l.location)]:
			list := make([]string, 0, len(allowed))
			for r := range allowed {
				list = append(list, r)
			}
			sort.Strings(list)
			out = append(out, diag.Err("XF023", l.path, "'%s' is outside governance.dataResidency (%s)", l.location, strings.Join(list, ", ")))
		}
	}
	return out
}

// ----------------------------------------------------------- security and networking

func securityRules(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	net := cfg.Security.Network
	if len(cfg.Security.Roles.Admins) == 0 {
		out = append(out, diag.Warn("XF114", path("security", "roles", "admins"), "security.roles.admins is empty; nobody will administer the deployment"))
	}
	if net.Mode == "restricted" && len(net.AllowedIPs) == 0 && net.ExistingVnetResourceID == "" {
		out = append(out, diag.Err("XF105", path("security", "network"), "network mode 'restricted' requires allowedIps or existingVnetResourceId"))
	}
	if net.Mode == "private" && cfg.Security.PublicNetworkAccess {
		out = append(out, diag.Err("XF021", path("security", "publicNetworkAccess"), "security.publicNetworkAccess cannot be true when network mode is 'private'"))
	}
	for _, c := range components(cfg) {
		if c.hasPublic && c.tracked.Has("publicNetworkAccess") && c.public {
			if net.Mode == "private" {
				out = append(out, diag.Err("XF021", c.path+".publicNetworkAccess", "publicNetworkAccess cannot be true when network mode is 'private'"))
			} else if !cfg.Security.PublicNetworkAccess {
				out = append(out, diag.Err("XF106", c.path+".publicNetworkAccess", "publicNetworkAccess is true but security.publicNetworkAccess is false; set the global intent first"))
			}
		}
		if c.hasLocal && c.tracked.Has("localAuthentication") && c.local && !cfg.Security.LocalAuthentication {
			out = append(out, diag.Err("XF106", c.path+".localAuthentication", "localAuthentication is true but security.localAuthentication is false; set the global intent first"))
		}
	}
	if m := cfg.ManagedIdentity; m != nil && !m.Enabled && net.Mode == "private" {
		out = append(out, diag.Err("XF021", path("managedIdentity", "enabled"), "private network mode requires a managed identity; do not disable managedIdentity"))
	}
	if net.Mode == "private" && !net.PrivateDNS && net.ExistingVnetResourceID == "" {
		out = append(out, diag.Warn("XF021", path("security", "network", "privateDns"), "privateDns is false: private endpoints will not resolve unless you manage DNS yourself"))
	}
	return out
}

func ipRules(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	check := func(where string, ranges []string, firewall bool) {
		for _, v := range ranges {
			n, ok := parseNet(v)
			switch {
			case !ok:
				out = append(out, diag.Err("XF121", where, "'%s' is not an IP address or CIDR range", v))
			case n.Bits() == 0:
				out = append(out, diag.Err("XF121", where, "'%s' allows the whole internet; list specific ranges", v))
			case firewall && isPrivateRange(v):
				out = append(out, diag.Err("XF121", where,
					"'%s' is a private range; Azure service firewalls accept public addresses only (use private mode or an existing VNet for private traffic)", v))
			}
		}
	}
	net := cfg.Security.Network
	check(path("security", "network", "allowedIps"), net.AllowedIPs, net.Mode == "restricted")
	if cfg.Gateway != nil {
		check(path("gateway", "security", "allowIps"), cfg.Gateway.Security.AllowIPs, false)
	}
	return out
}

func insecureEndpoints(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	check := func(raw, where string) {
		if raw == "" {
			return
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" {
			out = append(out, diag.Err("XF122", where, "'%s' must use https", raw))
		}
		if err != nil {
			return
		}
		host := strings.ToLower(u.Hostname())
		bad := host == "localhost" || strings.HasSuffix(host, ".localhost")
		if a, err := netip.ParseAddr(host); err == nil {
			bad = a.IsLinkLocalUnicast() || a.IsLoopback()
		}
		if bad {
			out = append(out, diag.Err("XF122", where, "'%s' is a loopback or link-local host (for example the instance metadata endpoint)", host))
		}
	}
	for _, h := range scopes(cfg) {
		for _, kb := range h.kbs() {
			for _, s := range kb.Sources {
				if s.Type == "web" {
					check(s.URL, fmt.Sprintf("%s.iq.knowledgeBases[%s].sources[%s].url", h.path, kb.Name, s.Name))
				}
			}
		}
	}
	if m := cfg.ManagedIdentity; m != nil {
		for _, c := range m.FederatedCredentials {
			check(c.Issuer, path("managedIdentity", "federatedCredentials["+c.Name+"]", "issuer"))
		}
	}
	return out
}

var searchLimits = map[string][2]int{ // sku -> max replicas, max partitions
	"free": {1, 1}, "basic": {3, 1}, "standard": {12, 12}, "standard2": {12, 12},
	"standard3": {12, 12}, "storage_optimized_l1": {12, 12}, "storage_optimized_l2": {12, 12},
}

func searchSizing(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	private := cfg.Security.Network.Mode == "private"
	for _, h := range scopes(cfg) {
		s := h.search
		if s == nil || !s.Enabled || s.ExistingResourceID != "" {
			continue
		}
		where := h.path + ".search"
		limits := searchLimits[s.SKU]
		if s.Replicas > limits[0] {
			out = append(out, diag.Err("XF123", where+".replicas", "sku '%s' allows at most %d replica(s)", s.SKU, limits[0]))
		}
		if s.Partitions > limits[1] {
			out = append(out, diag.Err("XF123", where+".partitions", "sku '%s' allows at most %d partition(s)", s.SKU, limits[1]))
		}
		if s.Replicas*s.Partitions > 36 {
			out = append(out, diag.Err("XF123", where, "replicas x partitions cannot exceed 36 search units"))
		}
		if s.SKU == "free" && private {
			out = append(out, diag.Err("XF123", where+".sku", "the free tier does not support private endpoints; use basic or higher in private mode"))
		}
	}
	return out
}

func gatewayNetwork(cfg *config.XFoundry) []diag.Diagnostic {
	g := cfg.Gateway
	if g == nil || !g.Enabled || cfg.Security.Network.Mode != "private" {
		return nil
	}
	where := path("gateway", "sku")
	switch g.SKU {
	case "Consumption":
		return []diag.Diagnostic{diag.Err("XF124", where, "the Consumption tier supports neither private endpoints nor VNet integration; use StandardV2 or higher in private mode")}
	case "BasicV2":
		return []diag.Diagnostic{diag.Warn("XF124", where, "BasicV2 has no outbound VNet integration, so it cannot reach private backends; use StandardV2 or higher")}
	}
	return nil
}

var cron = regexp.MustCompile(`^\S+(?:\s+\S+){4}$`)

func cronRules(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, h := range scopes(cfg) {
		for _, kb := range h.kbs() {
			if !cron.MatchString(kb.Refresh.Schedule) {
				out = append(out, diag.Err("XF118", fmt.Sprintf("%s.iq.knowledgeBases[%s].refresh.schedule", h.path, kb.Name), "'%s' is not a 5-field cron expression", kb.Refresh.Schedule))
			}
		}
	}
	return out
}

// ------------------------------------------------------- network, agent service, identity

func networkAddressing(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	net := cfg.Security.Network
	where := path("security", "network")
	existing := net.ExistingVnetResourceID != ""
	if net.Mode == "public" || (net.Mode == "restricted" && !existing) {
		for _, key := range []string{"addressSpace", "agentSubnetPrefixLength", "existingVnetResourceId", "existingAgentSubnetResourceId", "existingPrivateEndpointSubnetResourceId"} {
			if net.Has(key) {
				out = append(out, diag.Err("XF127", where+"."+key, "%s needs network mode 'private' (a VNet is only created or used in private mode)", key))
			}
		}
		return out
	}
	if !existing {
		for _, kv := range subnetFields(net) {
			key, v := kv[0], kv[1]
			if v != "" {
				out = append(out, diag.Err("XF127", where+"."+key, "%s requires existingVnetResourceId", key))
			}
		}
		if net.AddressSpace != "" {
			space, ok := parseNet(net.AddressSpace)
			switch {
			case !ok || !isPrivateRange(net.AddressSpace):
				out = append(out, diag.Err("XF127", where+".addressSpace", "'%s' must be a private (RFC 1918 or 100.64.0.0/10) range", net.AddressSpace))
			case net.AgentSubnetPrefixLength < space.Bits()+2:
				out = append(out, diag.Err("XF127", where+".agentSubnetPrefixLength",
					"a /%d agent subnet leaves no room for other subnets in %s; use /%d or smaller", net.AgentSubnetPrefixLength, net.AddressSpace, space.Bits()+2))
			}
		}
		return out
	}
	if net.AddressSpace != "" {
		out = append(out, diag.Err("XF127", where+".addressSpace", "addressSpace applies to a generated VNet, not existingVnetResourceId"))
	}
	if net.Has("agentSubnetPrefixLength") {
		out = append(out, diag.Err("XF127", where+".agentSubnetPrefixLength", "agentSubnetPrefixLength applies to a generated VNet, not existingVnetResourceId"))
	}
	for _, kv := range subnetFields(net) {
		key, v := kv[0], kv[1]
		if v != "" && !hasPrefixFold(v, net.ExistingVnetResourceID+"/subnets/") {
			out = append(out, diag.Err("XF127", where+"."+key, "subnet must belong to existingVnetResourceId"))
		}
	}
	if net.Mode == "private" {
		if net.ExistingPrivateEndpointSubnetResourceID == "" {
			out = append(out, diag.Err("XF127", where+".existingPrivateEndpointSubnetResourceId", "private mode with an existing VNet needs a subnet for private endpoints"))
		}
		if cfg.ResolvedAgentSetup() == "standard" && net.ExistingAgentSubnetResourceID == "" {
			out = append(out, diag.Err("XF127", where+".existingAgentSubnetResourceId", "the standard agent setup needs a subnet delegated to Microsoft.App/environments"))
		}
	}
	return out
}

func agentServiceRules(cfg *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	where := path("agentService", "setup")
	cosmosOn := cfg.Cosmos != nil && cfg.Cosmos.Enabled
	if cfg.AgentService.Setup == "basic" {
		if cosmosOn {
			out = append(out, diag.Err("XF128", where, "cosmos is only used by the standard agent setup; remove it or set setup to 'standard'"))
		}
		if cfg.Security.Network.Mode == "private" {
			out = append(out, diag.Warn("XF128", where, "the basic agent setup does not isolate agent traffic in a delegated subnet; use 'standard' for private networking"))
		}
	}
	if cfg.ResolvedAgentSetup() == "standard" {
		if cfg.Storage != nil && !cfg.Storage.Enabled {
			out = append(out, diag.Err("XF128", path("storage", "enabled"), "the standard agent setup needs storage; do not disable it"))
		}
		if cfg.Cosmos != nil && !cfg.Cosmos.Enabled {
			out = append(out, diag.Err("XF128", path("cosmos", "enabled"), "the standard agent setup needs Cosmos DB; do not disable it"))
		}
		if cfg.Search != nil && !cfg.Search.Enabled {
			out = append(out, diag.Err("XF128", path("search", "enabled"), "the standard agent setup needs Azure AI Search; do not disable it"))
		}
	}
	if c := cfg.Cosmos; c != nil && c.CapacityMode == "serverless" && c.Has("throughput") {
		out = append(out, diag.Err("XF131", path("cosmos", "throughput"), "throughput applies to provisioned capacity, not serverless"))
	}
	return out
}

func identityRules(cfg *config.XFoundry) []diag.Diagnostic {
	m := cfg.ManagedIdentity
	if m == nil || m.Type != "systemAssigned" {
		return nil
	}
	var out []diag.Diagnostic
	where := path("managedIdentity")
	if m.Name != "" || m.ExistingResourceID != "" {
		out = append(out, diag.Err("XF130", where, "no user-assigned identity is created with type 'systemAssigned'; remove name/existingResourceId or use a type that includes userAssigned"))
	}
	if len(m.FederatedCredentials) > 0 {
		out = append(out, diag.Err("XF130", where+".federatedCredentials", "federated credentials need a user-assigned identity"))
	}
	return out
}

func subnetFields(net config.NetworkSecurity) [][2]string {
	return [][2]string{
		{"existingAgentSubnetResourceId", net.ExistingAgentSubnetResourceID},
		{"existingPrivateEndpointSubnetResourceId", net.ExistingPrivateEndpointSubnetResourceID},
	}
}
