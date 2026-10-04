package graph

import (
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/ids"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
)

// Stages follow the deployment order in the specification (resource group, identity,
// networking, storage, Search, Foundry resource, projects, models, ...). They break
// ties only; dependencies always win. Deliberate deviations: MCPs, connectors and
// knowledge bases come before toolboxes and agents because those reference them; the
// observability workspace is created early because the gateway logs to it
// (alerts are the late "Monitoring" step).
var Stages = map[string]int{
	"resource-group": 0, "identity": 10, "observability": 10, "network": 20, "private-dns": 22,
	"storage": 30, "key-vault": 32, "cosmos": 41,
	"search": 50, "private-endpoint": 60, "foundry-account": 70, "foundry-project": 80,
	"capability-host": 82, "model-deployment": 90, "connector": 95, "mcp": 100,
	"knowledge-base": 110, "toolbox": 120, "agent": 130, "evaluation": 150, "gateway": 160,
	"alerts": 170, "governance": 180,
}

type builder struct {
	norm                                           *normalise.Config
	g                                              *Graph
	rg, identity, workspace, network, dns, foundry string
}

func (b *builder) node(id, kind, scope string, existing bool) string {
	b.g.AddNode(Node{ID: id, Kind: kind, Stage: Stages[kind], Scope: scope, Existing: existing})
	return id
}

// link records dependencies, skipping empty ids and nodes that are not part of the plan.
func (b *builder) link(id string, deps ...string) {
	for _, d := range deps {
		if d != "" && b.g.Has(d) {
			_ = b.g.AddDependency(id, d)
		}
	}
}

func host(project *normalise.EffectiveProject, origin string) string {
	if origin == ids.HubScope {
		return ids.HubScope
	}
	return ids.ProjectScope(project.Name)
}

// deploymentNode is the node of deployment name as seen from a scope (project, hub, root).
func (b *builder) deploymentNode(scope string, inheritsHub bool, name string) string {
	chain := []string{scope}
	if inheritsHub && b.norm.Scope(ids.HubScope) != nil {
		chain = append(chain, ids.HubScope)
	}
	chain = append(chain, ids.RootScope)
	for _, s := range chain {
		for _, d := range b.norm.Scope(s).Models.Deployments {
			if d.Name == name || d.Model == name {
				return ids.ItemNode("model-deployment", s, d.Name)
			}
		}
	}
	return ""
}

func userAssigned(m *config.ManagedIdentity) bool {
	return m != nil && m.Enabled && m.Type != "systemAssigned"
}

func (b *builder) foundations() {
	n := b.norm
	b.rg = b.node(ids.ResourceGroup, "resource-group", "", false)
	if userAssigned(n.ManagedIdentity) {
		b.identity = b.node(ids.Identity, "identity", "", n.ManagedIdentity.ExistingResourceID != "")
		b.link(b.identity, b.rg)
	}
	if o := n.Observability; o != nil && o.Enabled {
		b.workspace = b.node(ids.Workspace, "observability", "", false)
		b.link(b.workspace, b.rg)
	}
	if n.Network.VNet == "create" {
		b.network = b.node(ids.Network, "network", "", false)
		b.link(b.network, b.rg)
	}
	if n.Network.PrivateDNS {
		b.dns = b.node(ids.PrivateDNS, "private-dns", "", false)
		b.link(b.dns, b.rg, b.network)
	}
	if s := n.Storage; s != nil && s.Enabled {
		b.node(ids.Storage, "storage", "", s.ExistingResourceID != "")
		b.link(ids.Storage, b.rg, b.identity)
	}
	if k := n.KeyVault; k != nil && k.Enabled {
		b.node(ids.KeyVault, "key-vault", "", k.ExistingResourceID != "")
		b.link(ids.KeyVault, b.rg, b.identity)
	}
	if c := n.Cosmos; c != nil && c.Enabled {
		b.node(ids.Cosmos, "cosmos", "", c.ExistingResourceID != "")
		b.link(ids.Cosmos, b.rg, b.identity)
	}
	for _, s := range n.Scopes {
		if s.Search != nil && s.Search.Enabled {
			id := b.node(ids.SearchNode(s.Scope), "search", s.Scope, s.Search.ExistingResourceID != "")
			b.link(id, b.rg, b.identity)
		}
	}
	b.foundry = b.node(ids.Foundry, "foundry-account", "", false)
	b.link(b.foundry, b.rg, b.identity, b.workspace, b.network)
	for _, pe := range n.Network.PrivateEndpoints {
		id := b.node(ids.PrivateEndpointNode(pe.Component, pe.Group), "private-endpoint", "", false)
		b.link(id, b.rg, b.network, b.dns, pe.Component)
	}
}

func (b *builder) projects() {
	n := b.norm
	if n.Hub != nil {
		b.node(ids.ProjectNode(ids.HubScope), "foundry-project", ids.HubScope, false)
		b.link(ids.ProjectNode(ids.HubScope), b.foundry)
	}
	for _, p := range n.Projects {
		scope := ids.ProjectScope(p.Name)
		b.node(ids.ProjectNode(scope), "foundry-project", scope, false)
		b.link(ids.ProjectNode(scope), b.foundry)
	}
	for _, s := range n.Scopes {
		for _, d := range s.Models.Deployments {
			id := b.node(ids.ItemNode("model-deployment", s.Scope, d.Name), "model-deployment", s.Scope, false)
			b.link(id, b.foundry)
			if s.Scope != ids.RootScope {
				b.link(id, ids.ProjectNode(s.Scope))
			}
		}
	}
	for _, p := range n.Projects {
		b.projectItems(p)
	}
}

func (b *builder) capabilityHost(p *normalise.EffectiveProject) string {
	scope := ids.ProjectScope(p.Name)
	id := b.node(ids.ItemNode("capability-host", scope, "agents"), "capability-host", scope, false)
	b.link(id, ids.ProjectNode(scope), ids.Storage, ids.Cosmos, b.network)
	if p.SearchScope != "" {
		b.link(id, ids.SearchNode(p.SearchScope))
	}
	for _, pe := range b.norm.Network.PrivateEndpoints {
		if pe.Component != ids.Foundry {
			b.link(id, ids.PrivateEndpointNode(pe.Component, pe.Group))
		}
	}
	return id
}

func (b *builder) projectItems(p *normalise.EffectiveProject) {
	pscope := ids.ProjectScope(p.Name)
	pnode := ids.ProjectNode(pscope)
	origin := func(kind, name string) string { return p.Origins[kind+":"+name] }
	target := func(kind, label, name string) string {
		return ids.ItemNode(label, host(p, origin(kind, name)), name)
	}
	secrets := func(a *config.ConnectionAuthentication) string {
		if a != nil && a.SecretRef != "" {
			return ids.KeyVault
		}
		return ""
	}
	capHost := ""
	if b.norm.AgentSetup == "standard" {
		capHost = b.capabilityHost(p)
	}

	for _, c := range p.Connectors {
		h := host(p, origin("connector", c.Name))
		id := b.node(target("connector", "connector", c.Name), "connector", h, false)
		b.link(id, ids.ProjectNode(h), secrets(c.Authentication))
	}
	for _, m := range p.Mcps {
		h := host(p, origin("mcp", m.Name))
		id := b.node(target("mcp", "mcp", m.Name), "mcp", h, false)
		b.link(id, ids.ProjectNode(h), secrets(m.Authentication))
	}
	for _, kb := range p.KnowledgeBases {
		h := host(p, origin("knowledgeBase", kb.Name))
		id := b.node(target("knowledgeBase", "knowledge-base", kb.Name), "knowledge-base", h, false)
		searchScope := p.SearchScope
		if h == ids.HubScope && b.norm.Hub != nil {
			searchScope = b.norm.Hub.SearchScope
		}
		b.link(id, ids.ProjectNode(h))
		if searchScope != "" {
			b.link(id, ids.SearchNode(searchScope))
		}
		if kb.Index.Vector.Enabled && kb.Index.Vector.Deployment != "" {
			b.link(id, b.deploymentNode(h, p.InheritsHub, kb.Index.Vector.Deployment))
		}
		for _, s := range kb.Sources {
			if (s.Type == "blob" || s.Type == "adls") && s.Connection == "" {
				b.link(id, ids.Storage)
			}
			if s.Connection != "" {
				b.link(id, ids.ItemNode("connector", host(p, origin("connector", s.Connection)), s.Connection))
			}
		}
	}
	mcpNames, kbNames := map[string]bool{}, map[string]bool{}
	for _, m := range p.Mcps {
		mcpNames[m.Name] = true
	}
	for _, k := range p.KnowledgeBases {
		kbNames[k.Name] = true
	}
	for _, t := range p.Toolboxes {
		h := host(p, origin("toolbox", t.Name))
		id := b.node(target("toolbox", "toolbox", t.Name), "toolbox", h, false)
		b.link(id, ids.ProjectNode(h))
		for _, tool := range t.Tools {
			switch {
			case tool.Type == "mcp" && mcpNames[tool.Reference]:
				b.link(id, target("mcp", "mcp", tool.Reference))
			case tool.Type == "knowledgeBase" && kbNames[tool.Reference]:
				b.link(id, target("knowledgeBase", "knowledge-base", tool.Reference))
			}
		}
	}
	for _, a := range p.Agents {
		h := host(p, origin("agent", a.Name))
		id := b.node(target("agent", "agent", a.Name), "agent", h, false)
		b.link(id, ids.ProjectNode(h))
		if h != ids.HubScope {
			b.link(id, capHost)
		}
		if model := firstNonEmpty(a.Model, p.Models.Default); model != "" {
			b.link(id, b.deploymentNode(h, p.InheritsHub, model))
		}
		for _, name := range a.Toolboxes {
			b.link(id, target("toolbox", "toolbox", name))
		}
		for _, name := range a.Mcps {
			b.link(id, target("mcp", "mcp", name))
		}
		for _, name := range a.KnowledgeBases {
			b.link(id, target("knowledgeBase", "knowledge-base", name))
		}
	}
	if ev := p.Evaluation; ev != nil && ev.Enabled {
		id := b.node(ids.ItemNode("evaluation", pscope, "evaluation"), "evaluation", pscope, false)
		b.link(id, pnode)
		if len(ev.Datasets) > 0 {
			b.link(id, ids.Storage)
		}
		for _, a := range p.Agents {
			b.link(id, target("agent", "agent", a.Name))
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (b *builder) gateway() {
	n := b.norm
	g := n.Gateway
	if g == nil || !g.Enabled {
		return
	}
	id := b.node(ids.Gateway, "gateway", "", false)
	b.link(id, b.rg, b.identity, b.workspace, b.network)
	for _, e := range g.Endpoints {
		switch e.TargetType {
		case "agent":
			for _, p := range n.Projects {
				if e.Project != "" && e.Project != p.Name {
					continue
				}
				for _, a := range p.Agents {
					if a.Name == e.Target {
						b.link(id, ids.ItemNode("agent", host(p, p.Origins["agent:"+e.Target]), e.Target))
					}
				}
			}
		case "knowledgeBase":
			for _, p := range n.Projects {
				if e.Project != "" && e.Project != p.Name {
					continue
				}
				for _, k := range p.KnowledgeBases {
					if k.Name == e.Target {
						b.link(id, ids.ItemNode("knowledge-base", host(p, p.Origins["knowledgeBase:"+e.Target]), e.Target))
					}
				}
			}
		case "model":
			for _, s := range n.Scopes {
				for _, d := range s.Models.Deployments {
					if d.Name == e.Target || d.Model == e.Target {
						b.link(id, ids.ItemNode("model-deployment", s.Scope, d.Name))
					}
				}
			}
		case "search":
			for _, s := range n.Scopes {
				sr := s.Search
				if sr == nil || !sr.Enabled {
					continue
				}
				existing := sr.ExistingResourceID[strings.LastIndex(sr.ExistingResourceID, "/")+1:]
				if e.Target == firstNonEmpty(sr.Name, "search") || (existing != "" && e.Target == existing) {
					b.link(id, ids.SearchNode(s.Scope))
				}
			}
		}
	}
}

func (b *builder) monitoringAndGovernance() {
	n := b.norm
	if o := n.Observability; o != nil && o.Enabled && o.Alerts {
		alerts := b.node(ids.Alerts, "alerts", "", false)
		b.link(alerts, b.workspace)
		for _, id := range b.g.IDs() {
			switch b.g.Node(id).Kind {
			case "gateway", "search", "foundry-account":
				b.link(alerts, id)
			}
		}
	}
	if gv := n.Governance; gv != nil && gv.Enabled {
		id := b.node(ids.Governance, "governance", "", false)
		b.link(id, b.rg, ids.Gateway, ids.Alerts)
	}
}

// Build constructs the deployment graph from a normalised configuration.
func Build(norm *normalise.Config) *Graph {
	b := &builder{norm: norm, g: New()}
	b.foundations()
	b.projects()
	b.gateway()
	b.monitoringAndGovernance()
	return b.g
}
