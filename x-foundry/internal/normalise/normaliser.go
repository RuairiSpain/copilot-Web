package normalise

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/ids"
)

type zoneInfo struct {
	group string
	zones []string
}

var privateLinkZones = map[string]zoneInfo{
	"foundry":     {"account", []string{"privatelink.cognitiveservices.azure.com", "privatelink.openai.azure.com", "privatelink.services.ai.azure.com"}},
	"search":      {"searchService", []string{"privatelink.search.windows.net"}},
	"storage":     {"blob", []string{"privatelink.blob.core.windows.net"}},
	"storage-dfs": {"dfs", []string{"privatelink.dfs.core.windows.net"}},
	"key-vault":   {"vault", []string{"privatelink.vaultcore.azure.net"}},
	"redis":       {"redisCache", []string{"privatelink.redis.cache.windows.net"}},
	"cosmos":      {"Sql", []string{"privatelink.documents.azure.com"}},
	"events":      {"namespace", []string{"privatelink.servicebus.windows.net"}},
	"registry":    {"registry", []string{"privatelink.azurecr.io"}},
}

// Result is the output of Normalise.
type Result struct {
	Config      *Config
	Diagnostics []diag.Diagnostic
}

// Normalise returns the normalised configuration for a declared (validated) config. It
// never mutates cfg and never fails; problems that only surface while resolving (for
// example Foundry IQ with Search disabled) are returned as diagnostics.
func Normalise(cfg *config.XFoundry) Result {
	n := &normaliser{cfg: config.Clone(cfg), byID: map[string]*ScopeResources{}}
	return n.run()
}

type normaliser struct {
	cfg         *config.XFoundry
	hub         *config.Hub
	diags       []diag.Diagnostic
	implicit    []Implicit
	scopes      []*ScopeResources
	byID        map[string]*ScopeResources
	baseTags    config.Tags
	hubSearch   string
	storage     *config.Storage
	standard    bool
	projectName []string
}

func (n *normaliser) addImplicit(kind, name, scope, reason string) {
	n.implicit = append(n.implicit, Implicit{kind, name, scope, reason})
}

func (n *normaliser) addScope(s *ScopeResources) *ScopeResources {
	n.scopes = append(n.scopes, s)
	n.byID[s.Scope] = s
	return s
}

func (n *normaliser) project(name string) *config.Project {
	for i := range n.cfg.Projects {
		if n.cfg.Projects[i].Name == name {
			return &n.cfg.Projects[i]
		}
	}
	return nil
}

func (n *normaliser) inherits(projectName string) bool {
	p := n.project(projectName)
	return n.hub != nil && p != nil && p.InheritHub
}

// ancestors are the scopes whose model deployments are visible from scope (broadest first).
func (n *normaliser) ancestors(scope string) []string {
	switch scope {
	case ids.RootScope:
		return []string{ids.RootScope}
	case ids.HubScope:
		return []string{ids.RootScope, ids.HubScope}
	}
	chain := []string{ids.RootScope}
	if n.inherits(ids.ProjectName(scope)) && n.hub.Inheritance.Models {
		chain = append(chain, ids.HubScope)
	}
	return append(chain, scope)
}

func (n *normaliser) deploymentsVisible(scope string) []config.ModelDeployment {
	var layers []layer[config.ModelDeployment]
	for _, s := range n.ancestors(scope) {
		layers = append(layers, layer[config.ModelDeployment]{s, n.byID[s].Models.Deployments})
	}
	merged, _ := mergeNamed(layers)
	return merged
}

// ------------------------------------------------------------------------ assembly

func (n *normaliser) assemble() {
	cfg := n.cfg
	root := n.addScope(&ScopeResources{Scope: ids.RootScope, Models: cfg.Models, Search: cfg.Search})
	if h := n.hub; h != nil {
		hub := &ScopeResources{Scope: ids.HubScope, Models: h.Models, Toolboxes: h.Toolboxes, Mcps: h.Mcps, Search: h.Search}
		if h.IQ != nil && h.IQ.Enabled {
			hub.KnowledgeBases = append(hub.KnowledgeBases, h.IQ.KnowledgeBases...)
		}
		n.addScope(hub)
	}
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		s := &ScopeResources{
			Scope: ids.ProjectScope(p.Name), Models: p.Models, Agents: p.Agents, Toolboxes: p.Toolboxes,
			Mcps: p.Mcps, Connectors: p.Connectors, Search: p.Search, Runtime: p.Runtime, Evaluation: p.Evaluation,
		}
		if p.IQ != nil && p.IQ.Enabled {
			s.KnowledgeBases = append(s.KnowledgeBases, p.IQ.KnowledgeBases...)
		}
		n.addScope(s)
	}
	target := func(project string) *ScopeResources {
		if s, ok := n.byID[ids.ProjectScope(project)]; ok && project != "" {
			return s
		}
		return root
	}
	for _, a := range cfg.Agents {
		t := target(a.Project)
		t.Agents = append(t.Agents, a)
	}
	for _, x := range cfg.Toolboxes {
		t := target(x.Project)
		t.Toolboxes = append(t.Toolboxes, x)
	}
	for _, x := range cfg.Mcps {
		t := target(x.Project)
		t.Mcps = append(t.Mcps, x)
	}
	for _, x := range cfg.Connectors {
		t := target(x.Project)
		t.Connectors = append(t.Connectors, x)
	}
	if cfg.IQ != nil && cfg.IQ.Enabled {
		for _, kb := range cfg.IQ.KnowledgeBases {
			project := kb.Project
			if project == "" {
				project = cfg.IQ.Project
			}
			t := target(project)
			t.KnowledgeBases = append(t.KnowledgeBases, kb)
		}
	}
	if cfg.Runtime != nil {
		if t := target(cfg.Runtime.Project); t.Runtime == nil {
			t.Runtime = cfg.Runtime
		}
	}
	if cfg.Evaluation != nil {
		if t := target(cfg.Evaluation.Project); t.Evaluation == nil {
			t.Evaluation = cfg.Evaluation
		}
	}
}

// ---------------------------------------------------------------------------- models

// defaultSKU: global deployments may process data in any region, so data residency
// forces DataZone.
func (n *normaliser) defaultSKU() string {
	if g := n.cfg.Governance; g != nil && g.Enabled && len(g.DataResidency) > 0 {
		return "DataZoneStandard"
	}
	return "GlobalStandard"
}

func (n *normaliser) newDeployment(name, model string) config.ModelDeployment {
	d := config.New[config.ModelDeployment]()
	d.Name, d.Model, d.SKU = name, model, n.defaultSKU()
	return *d
}

// implicitDeployments deploys models named in allowed, or used for embeddings, when
// nothing else does.
func (n *normaliser) implicitDeployments() {
	for _, s := range n.scopes {
		visible := map[string]bool{}
		for _, d := range n.deploymentsVisible(s.Scope) {
			visible[d.Name], visible[d.Model] = true, true
		}
		for _, model := range s.Models.Allowed {
			if visible[model] {
				continue
			}
			d := n.newDeployment(ids.Slug(model), model)
			s.Models.Deployments = append(s.Models.Deployments, d)
			visible[d.Name], visible[model] = true, true
			n.addImplicit("model-deployment", d.Name, s.Scope, fmt.Sprintf("'%s' is in models.allowed", model))
		}
		for i := range s.KnowledgeBases {
			kb := &s.KnowledgeBases[i]
			vector := &kb.Index.Vector
			if !vector.Enabled || vector.Deployment != "" {
				continue
			}
			var match *config.ModelDeployment
			for _, d := range n.deploymentsVisible(s.Scope) {
				if d.Model == vector.Model {
					d := d
					match = &d
					break
				}
			}
			if match == nil {
				d := n.newDeployment(ids.Slug(vector.Model), vector.Model)
				s.Models.Deployments = append(s.Models.Deployments, d)
				match = &d
				n.addImplicit("model-deployment", d.Name, s.Scope, fmt.Sprintf("embedding model for knowledge base '%s'", kb.Name))
			}
			vector.Deployment = match.Name
		}
	}
}

// ----------------------------------------------------------------------------- index

func materialiseIndex(kb *config.KnowledgeBase) {
	index := &kb.Index
	if index.Name == "" {
		index.Name = kb.Name
	}
	have := map[string]bool{}
	for _, f := range index.Fields {
		have[f.Name] = true
	}
	add := func(f config.IndexField) {
		if !have[f.Name] {
			index.Fields = append(index.Fields, f)
			have[f.Name] = true
		}
	}
	if index.KeyField == "id" {
		add(config.IndexField{Name: "id", Type: "Edm.String", Key: true, Retrievable: true})
	}
	if index.ContentField == "content" {
		add(config.IndexField{Name: "content", Type: "Edm.String", Searchable: true, Retrievable: true})
	}
	if index.TitleField == "title" {
		add(config.IndexField{Name: "title", Type: "Edm.String", Searchable: true, Retrievable: true})
	}
	if index.Vector.Enabled && index.VectorField == "contentVector" {
		add(config.IndexField{
			Name: "contentVector", Type: "Collection(Edm.Single)", Searchable: true,
			Dimensions: index.Vector.Dimensions, VectorProfile: index.Vector.Profile,
		})
	}
	if !index.Semantic.Has("titleField") {
		index.Semantic.TitleField = index.TitleField
	}
	if !index.Semantic.Has("contentFields") {
		index.Semantic.ContentFields = []string{index.ContentField}
	}
}

// ---------------------------------------------------------------------------- search

func (n *normaliser) searchChain(scope string) []string {
	switch scope {
	case ids.RootScope:
		return []string{ids.RootScope}
	case ids.HubScope:
		return []string{ids.HubScope, ids.RootScope}
	}
	chain := []string{scope}
	if n.inherits(ids.ProjectName(scope)) && n.hub.Inheritance.Search {
		chain = append(chain, ids.HubScope)
	}
	return append(chain, ids.RootScope)
}

func (n *normaliser) findSearch(scope string) *ScopeResources {
	for _, c := range n.searchChain(scope) {
		if s := n.byID[c]; s.Search != nil {
			return s
		}
	}
	return nil
}

func (n *normaliser) resolveSearch(effective []*EffectiveProject) {
	type consumer struct {
		scope   string
		project *EffectiveProject
	}
	var consumers []consumer
	for _, p := range effective {
		consumers = append(consumers, consumer{ids.ProjectScope(p.Name), p})
	}
	if n.hub != nil {
		consumers = append(consumers, consumer{ids.HubScope, nil})
	}
	needs := func(c consumer) bool {
		if c.project != nil {
			return len(c.project.KnowledgeBases) > 0 || n.standard
		}
		return len(n.byID[c.scope].KnowledgeBases) > 0
	}
	// Pass 1: derive a root Search when Foundry IQ or the standard agent setup needs one.
	for _, c := range consumers {
		if !needs(c) {
			continue
		}
		found := n.findSearch(c.scope)
		switch {
		case found == nil:
			root := n.byID[ids.RootScope]
			root.Search = config.New[config.Search]()
			reason := "Foundry IQ requires Azure AI Search"
			if c.project != nil && len(c.project.KnowledgeBases) == 0 {
				reason = "the standard agent setup requires Azure AI Search"
			}
			n.addImplicit("search", "search", ids.RootScope, reason)
		case !found.Search.Enabled:
			label := c.scope
			if c.project != nil {
				label = "project '" + c.project.Name + "'"
			}
			n.diags = append(n.diags, diag.Err("XF020", "x-foundry.search.enabled",
				"Foundry IQ or the standard agent setup for %s requires Azure AI Search, but search is disabled in scope '%s'", label, found.Scope))
		}
	}
	// Pass 2: record the resolved Search scope for every consumer.
	for _, c := range consumers {
		resolved := ""
		if found := n.findSearch(c.scope); found != nil && found.Search != nil && found.Search.Enabled {
			resolved = found.Scope
		}
		if c.project != nil {
			c.project.SearchScope = resolved
		} else {
			n.hubSearch = resolved
		}
	}
}

// -------------------------------------------------------------------------- effective

func mergeRoles(sets ...config.SecurityRoles) Roles {
	var out Roles
	targets := []*[]config.Principal{&out.Admins, &out.Developers, &out.Consumers, &out.Operators}
	for _, set := range sets {
		sources := [][]config.Principal{set.Admins, set.Developers, set.Consumers, set.Operators}
		for i, src := range sources {
			for _, p := range src {
				dup := false
				for _, e := range *targets[i] {
					if e.Key() == p.Key() {
						dup = true
					}
				}
				if !dup {
					*targets[i] = append(*targets[i], p)
				}
			}
		}
	}
	return out
}

func (n *normaliser) effectiveProject(name string) *EffectiveProject {
	cfg := n.cfg
	p := n.project(name)
	scopeID := ids.ProjectScope(name)
	own, root := n.byID[scopeID], n.byID[ids.RootScope]
	inherits := n.inherits(name)
	hub := n.byID[ids.HubScope]
	useHub := func(flag bool) bool { return inherits && hub != nil && flag }
	inh := config.Inheritance{}
	if n.hub != nil {
		inh = n.hub.Inheritance
	}

	modelLayers := []modelLayer{{ids.RootScope, root.Models}}
	if useHub(inh.Models) {
		modelLayers = append(modelLayers, modelLayer{ids.HubScope, hub.Models})
	}
	modelLayers = append(modelLayers, modelLayer{scopeID, own.Models})

	origins := map[string]string{}
	record := func(kind string, o map[string]string) {
		for k, v := range o {
			origins[kind+":"+k] = v
		}
	}
	eff := &EffectiveProject{Name: name, InheritsHub: inherits, Models: mergeModels(modelLayers)}

	var o map[string]string
	eff.Agents, o = mergeNamed([]layer[config.Agent]{{ids.RootScope, root.Agents}, {scopeID, own.Agents}})
	record("agent", o)
	tb := []layer[config.Toolbox]{{ids.RootScope, root.Toolboxes}}
	mc := []layer[config.Mcp]{{ids.RootScope, root.Mcps}}
	kb := []layer[config.KnowledgeBase]{{ids.RootScope, root.KnowledgeBases}}
	if useHub(inh.Toolboxes) {
		tb = append(tb, layer[config.Toolbox]{ids.HubScope, hub.Toolboxes})
	}
	if useHub(inh.Mcps) {
		mc = append(mc, layer[config.Mcp]{ids.HubScope, hub.Mcps})
	}
	if useHub(inh.IQ) {
		kb = append(kb, layer[config.KnowledgeBase]{ids.HubScope, hub.KnowledgeBases})
	}
	eff.Toolboxes, o = mergeNamed(append(tb, layer[config.Toolbox]{scopeID, own.Toolboxes}))
	record("toolbox", o)
	eff.Mcps, o = mergeNamed(append(mc, layer[config.Mcp]{scopeID, own.Mcps}))
	record("mcp", o)
	eff.Connectors, o = mergeNamed([]layer[config.Connector]{{ids.RootScope, root.Connectors}, {scopeID, own.Connectors}})
	record("connector", o)
	eff.KnowledgeBases, o = mergeNamed(append(kb, layer[config.KnowledgeBase]{scopeID, own.KnowledgeBases}))
	record("knowledgeBase", o)
	eff.Origins = origins

	switch {
	case own.Runtime != nil:
		eff.Runtime, eff.RuntimeScope = own.Runtime, scopeID
	case root.Runtime != nil:
		eff.Runtime, eff.RuntimeScope = root.Runtime, ids.RootScope
	}
	eff.Evaluation = own.Evaluation
	if eff.Evaluation == nil {
		eff.Evaluation = root.Evaluation
	}
	if p.Gateway != nil {
		g := config.Clone(*p.Gateway)
		if g.Path == "" {
			g.Path = "/" + name
		}
		eff.Gateway = &g
	}
	eff.DisplayName = firstNonEmpty(p.DisplayName, name)
	eff.Description = p.Description
	eff.Location = firstNonEmpty(p.Location, cfg.Defaults.Location)
	eff.ResourceGroup = firstNonEmpty(p.ResourceGroup, cfg.Defaults.ResourceGroup)
	eff.Roles = mergeRoles(cfg.Security.Roles, p.Roles)
	eff.Tags = config.Tags{}
	for k, v := range n.baseTags {
		eff.Tags[k] = v
	}
	for k, v := range p.Tags {
		eff.Tags[k] = v
	}
	eff.Tags["project"] = name
	return eff
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ------------------------------------------------------------------------ components

func (n *normaliser) resolveStorage() *config.Storage {
	storage := n.cfg.Storage
	var purposes []string
	reasons := map[string]string{}
	var containers []string
	need := func(purpose, reason string) {
		if _, ok := reasons[purpose]; !ok {
			purposes = append(purposes, purpose)
			reasons[purpose] = reason
		}
	}
	if n.standard {
		need("documents", "the standard agent setup stores agent files")
	}
	adls := false
	for _, s := range n.scopes {
		for _, kb := range s.KnowledgeBases {
			for _, src := range kb.Sources {
				if (src.Type == "blob" || src.Type == "adls") && src.Connection == "" {
					need("knowledge", fmt.Sprintf("knowledge base '%s' has a %s source", kb.Name, src.Type))
					containers = append(containers, src.Container)
				}
				adls = adls || src.Type == "adls"
			}
		}
		if ev := s.Evaluation; ev != nil && ev.Enabled && len(ev.Datasets) > 0 {
			need("evaluations", "evaluation datasets are enabled")
		}
	}
	if len(purposes) == 0 {
		return storage
	}
	var why []string
	for _, p := range purposes {
		why = append(why, reasons[p])
	}
	switch {
	case storage == nil:
		storage = config.New[config.Storage]()
		storage.Purposes = nil
		n.addImplicit("storage", "storage", ids.RootScope, strings.Join(why, "; "))
	case !storage.Enabled:
		n.diags = append(n.diags, diag.Err("XF107", "x-foundry.storage.enabled", "storage is disabled but required: %s", strings.Join(why, "; ")))
		return storage
	}
	for _, p := range purposes {
		if !has(storage.Purposes, p) {
			storage.Purposes = append(storage.Purposes, p)
			n.addImplicit("storage-purpose", p, ids.RootScope, reasons[p])
		}
	}
	if adls && !storage.Has("hierarchicalNamespace") && storage.ExistingResourceID == "" {
		storage.HierarchicalNamespace = true
	}
	haveName := map[string]bool{}
	for _, c := range storage.Containers {
		haveName[c.Name] = true
	}
	for _, p := range storage.Purposes {
		found := false
		for _, c := range storage.Containers {
			found = found || c.Purpose == p
		}
		if !found {
			c := config.New[config.StorageContainer]()
			c.Name, c.Purpose = p, p
			storage.Containers = append(storage.Containers, *c)
			haveName[p] = true
		}
	}
	for _, name := range containers {
		if name != "" && !haveName[name] {
			c := config.New[config.StorageContainer]()
			c.Name, c.Purpose = name, "knowledge"
			storage.Containers = append(storage.Containers, *c)
			haveName[name] = true
		}
	}
	return storage
}

// resolveCosmos returns the Cosmos DB account the standard agent setup needs.
func (n *normaliser) resolveCosmos() *config.Cosmos {
	cosmos := n.cfg.Cosmos
	if n.standard && cosmos == nil {
		cosmos = config.New[config.Cosmos]()
		n.addImplicit("cosmos", "cosmos", ids.RootScope, "the standard agent setup stores threads and agent state in Cosmos DB")
	}
	return cosmos
}

func (n *normaliser) resolveGateway() *config.Gateway {
	g := n.cfg.Gateway
	if g == nil || !g.Enabled {
		return g
	}
	if g.Authentication == nil {
		prefix := firstNonEmpty(n.cfg.Defaults.NamingPrefix, "x-foundry")
		a := config.New[config.GatewayAuthentication]()
		a.Audiences = []string{"api://" + prefix + "-gateway"}
		g.Authentication = a
		n.addImplicit("gateway-authentication", "entra", ids.RootScope, "gateway requires Entra authentication")
	}
	tracking := &g.TokenTracking
	if g.Chargeback.Enabled {
		for _, dim := range g.Chargeback.Dimensions {
			if !has(tracking.Dimensions, dim) {
				tracking.Dimensions = append(tracking.Dimensions, dim)
			}
		}
	}
	if has(tracking.Dimensions, "department") && tracking.DepartmentClaim == "" {
		tracking.DepartmentClaim = "department"
	}
	if !g.Models.Has("default") && !g.Models.Has("allowed") && !g.Models.Has("denied") {
		m := n.cfg.Models
		g.Models.Default = m.Default
		g.Models.Allowed = append([]string(nil), m.Allowed...)
		g.Models.Denied = append([]string(nil), m.Denied...)
	}
	return g
}

func (n *normaliser) runtimes() []*config.Runtime {
	var found []*config.Runtime
	for _, s := range n.scopes {
		if s.Runtime != nil {
			found = append(found, s.Runtime)
		}
		for i := range s.Agents {
			if s.Agents[i].Runtime != nil {
				found = append(found, s.Agents[i].Runtime)
			}
		}
	}
	for _, r := range found {
		if !r.Resources.Has("memory") {
			r.Resources.Memory = fmt.Sprintf("%gGi", r.Resources.CPU*2)
		}
	}
	return found
}

var skuRank = map[string]int{"Basic": 1, "Standard": 2, "Premium": 3}

// registrySKU is the tier of the managed registry: the highest explicit request, else
// Premium in private mode (required for private endpoints) and Standard otherwise.
func (n *normaliser) registrySKU(runtimes []*config.Runtime) string {
	best := ""
	for _, r := range runtimes {
		if r.Enabled && r.Registry.Mode == "managed" && skuRank[r.Registry.SKU] > skuRank[best] {
			best = r.Registry.SKU
		}
	}
	switch {
	case best != "":
		return best
	case n.cfg.Security.Network.Mode == "private":
		return "Premium"
	}
	return "Standard"
}

type componentSet struct {
	storage, keyVault, redis, cosmos, events, registry bool
}

func (n *normaliser) resolveNetwork(c componentSet, searchScopes []string) Network {
	net := n.cfg.Security.Network
	out := Network{Mode: net.Mode, AllowedIPs: net.AllowedIPs, ExistingVnetResourceID: net.ExistingVnetResourceID}
	switch {
	case net.ExistingVnetResourceID != "":
		out.VNet = "existing"
	case net.Mode == "private":
		out.VNet = "create"
		out.AddressSpace = firstNonEmpty(net.AddressSpace, "10.20.0.0/16")
		n.addImplicit("network", "vnet", ids.RootScope, "private network mode")
	}
	if net.Mode != "private" {
		return out
	}
	out.PrivateDNS = net.PrivateDNS
	out.PrivateEndpointSubnetResourceID = net.ExistingPrivateEndpointSubnetResourceID
	if n.standard {
		if net.ExistingVnetResourceID != "" {
			out.AgentSubnet, out.AgentSubnetResourceID = "existing", net.ExistingAgentSubnetResourceID
		} else {
			out.AgentSubnet, out.AgentSubnetPrefixLength = "create", net.AgentSubnetPrefixLength
		}
	}
	type endpoint struct{ component, zoneKey string }
	endpoints := []endpoint{{ids.Foundry, "foundry"}}
	for _, s := range searchScopes {
		endpoints = append(endpoints, endpoint{ids.SearchNode(s), "search"})
	}
	if c.storage {
		endpoints = append(endpoints, endpoint{ids.Storage, "storage"})
		if n.storage != nil && n.storage.HierarchicalNamespace {
			endpoints = append(endpoints, endpoint{ids.Storage, "storage-dfs"})
		}
	}
	for _, e := range []struct {
		on        bool
		component string
		zoneKey   string
	}{
		{c.keyVault, ids.KeyVault, "key-vault"}, {c.redis, ids.Redis, "redis"}, {c.cosmos, ids.Cosmos, "cosmos"},
		{c.events, ids.Events, "events"}, {c.registry, ids.Registry, "registry"},
	} {
		if e.on {
			endpoints = append(endpoints, endpoint{e.component, e.zoneKey})
		}
	}
	var zones []string
	for _, e := range endpoints {
		info := privateLinkZones[e.zoneKey]
		out.PrivateEndpoints = append(out.PrivateEndpoints, PrivateEndpoint{e.component, info.group})
		for _, z := range info.zones {
			if !has(zones, z) {
				zones = append(zones, z)
			}
		}
	}
	if net.PrivateDNS {
		out.PrivateDNSZones = zones
	}
	return out
}

// ------------------------------------------------------------------------------ run

func (n *normaliser) run() Result {
	cfg := n.cfg
	n.hub = cfg.Hub
	n.standard = cfg.ResolvedAgentSetup() == "standard"
	for _, p := range cfg.Projects {
		n.projectName = append(n.projectName, p.Name)
	}
	n.baseTags = config.Tags{"environment": cfg.Defaults.Environment, "managed-by": "x-foundry"}
	for _, m := range []config.Tags{cfg.Defaults.Tags, cfg.Tags} {
		for k, v := range m {
			n.baseTags[k] = v
		}
	}
	n.assemble()
	n.implicitDeployments()
	for _, s := range n.scopes {
		for i := range s.KnowledgeBases {
			materialiseIndex(&s.KnowledgeBases[i])
		}
	}
	var effective []*EffectiveProject
	for _, name := range n.projectName {
		effective = append(effective, n.effectiveProject(name))
	}
	n.resolveSearch(effective)
	runtimes := n.runtimes()
	n.storage = n.resolveStorage()
	cosmos := n.resolveCosmos()
	gateway := n.resolveGateway()

	identity := cfg.ManagedIdentity
	if identity == nil {
		identity = config.New[config.ManagedIdentity]()
		n.addImplicit("managed-identity", "identity", ids.RootScope, "keyless access and RBAC")
	}
	usesSecrets := false
	for _, s := range n.scopes {
		for _, m := range s.Mcps {
			usesSecrets = usesSecrets || (m.Authentication != nil && m.Authentication.SecretRef != "")
		}
		for _, c := range s.Connectors {
			usesSecrets = usesSecrets || (c.Authentication != nil && c.Authentication.SecretRef != "")
		}
	}
	for _, r := range runtimes {
		usesSecrets = usesSecrets || len(r.Secrets) > 0
	}
	keyVault := cfg.KeyVault
	if keyVault == nil && usesSecrets {
		keyVault = config.New[config.KeyVault]()
		n.addImplicit("key-vault", "key-vault", ids.RootScope, "secret references are used")
	}
	observability := cfg.Observability
	anyRuntime := false
	for _, r := range runtimes {
		anyRuntime = anyRuntime || r.Enabled
	}
	if observability == nil && ((gateway != nil && gateway.Enabled) || anyRuntime) {
		observability = config.New[config.Observability]()
		n.addImplicit("observability", "observability", ids.RootScope, "gateway or runtime telemetry")
	}

	events := cfg.Events
	if events != nil && events.Enabled && events.Provider == "serviceBus" && events.SKU == "" {
		events.SKU = map[bool]string{true: "Premium", false: "Standard"}[cfg.Security.Network.Mode == "private"]
	}

	scopeSet := map[string]bool{}
	for _, p := range effective {
		if p.SearchScope != "" {
			scopeSet[p.SearchScope] = true
		}
	}
	if n.hubSearch != "" {
		scopeSet[n.hubSearch] = true
	}
	var searchScopes []string
	for s := range scopeSet {
		searchScopes = append(searchScopes, s)
	}
	sort.Strings(searchScopes)

	registryNeeded := false
	for _, r := range runtimes {
		registryNeeded = registryNeeded || (r.Enabled && r.Source != "" && r.Registry.Mode == "managed")
	}
	registrySKU := ""
	if registryNeeded {
		n.addImplicit("container-registry", "registry", ids.RootScope, "runtime image is built from source")
		registrySKU = n.registrySKU(runtimes)
	}
	network := n.resolveNetwork(componentSet{
		storage:  n.storage != nil && n.storage.Enabled,
		keyVault: keyVault != nil && keyVault.Enabled,
		redis:    cfg.Redis != nil && cfg.Redis.Enabled,
		cosmos:   cosmos != nil && cosmos.Enabled,
		events:   cfg.Events != nil && cfg.Events.Enabled,
		registry: registryNeeded,
	}, searchScopes)

	var hubView *HubView
	if h := n.hub; h != nil {
		tags := config.Tags{}
		for k, v := range n.baseTags {
			tags[k] = v
		}
		for k, v := range h.Tags {
			tags[k] = v
		}
		hubView = &HubView{
			Name: h.Name, Location: firstNonEmpty(h.Location, cfg.Defaults.Location),
			ResourceGroup: firstNonEmpty(h.ResourceGroup, cfg.Defaults.ResourceGroup),
			Inheritance:   h.Inheritance, SearchScope: n.hubSearch, Tags: tags,
		}
	}
	out := &Config{
		SchemaVersion: cfg.SchemaVersion, TopologyMode: cfg.Topology.Mode, NamingPrefix: cfg.Defaults.NamingPrefix,
		Environment: cfg.Defaults.Environment, Location: cfg.Defaults.Location, ResourceGroup: cfg.Defaults.ResourceGroup,
		Tags: n.baseTags, Roles: mergeRoles(cfg.Security.Roles), Network: network,
		LocalAuthentication: cfg.Security.LocalAuthentication, PurgeProtection: cfg.Security.PurgeProtection,
		AgentSetup: cfg.ResolvedAgentSetup(), FoundryIdentity: "systemAssigned",
		Hub: hubView, Scopes: n.scopes, Projects: effective, Gateway: gateway, Storage: n.storage,
		Redis: cfg.Redis, KeyVault: keyVault, Cosmos: cosmos, ManagedIdentity: identity,
		Observability: observability, Events: events, RegistrySKU: registrySKU, Governance: cfg.Governance,
		Implicit: n.implicit,
	}
	return Result{Config: out, Diagnostics: n.diags}
}
