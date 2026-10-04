package graph

import (
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/ids"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
)

// Stages follow the deployment order (resource group, identity, networking, storage, Search,
// then knowledge bases and the gateway). They break ties only; dependencies always win. The
// Foundry resource, projects, model deployments, agents, toolboxes, MCP servers and connections
// are azd's (azure.ai.* services) and are not part of this graph. The observability workspace is
// created early because the gateway logs to it (alerts are the late "Monitoring" step).
var Stages = map[string]int{
	"resource-group": 0, "identity": 10, "observability": 10, "network": 20, "private-dns": 22,
	"storage": 30, "key-vault": 32, "cosmos": 41, "search": 50, "private-endpoint": 60,
	"knowledge-base": 110, "gateway": 160, "alerts": 170, "governance": 180,
}

type builder struct {
	norm                                  *normalise.Config
	g                                     *Graph
	rg, identity, workspace, network, dns string
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
	for _, pe := range n.Network.PrivateEndpoints {
		id := b.node(ids.PrivateEndpointNode(pe.Component, pe.Group), "private-endpoint", "", false)
		b.link(id, b.rg, b.network, b.dns, pe.Component)
	}
}

func (b *builder) projects() {
	for _, p := range b.norm.Projects {
		b.knowledgeBases(p)
	}
}

// knowledgeBases adds the Foundry IQ knowledge bases a project sees (its own and the inherited
// ones). They are data-plane items on the Search service and are deployed by a later phase.
func (b *builder) knowledgeBases(p *normalise.EffectiveProject) {
	for _, kb := range p.KnowledgeBases {
		h := host(p, p.Origins["knowledgeBase:"+kb.Name])
		id := b.node(ids.ItemNode("knowledge-base", h, kb.Name), "knowledge-base", h, false)
		searchScope := p.SearchScope
		if h == ids.HubScope && b.norm.Hub != nil {
			searchScope = b.norm.Hub.SearchScope
		}
		if searchScope != "" {
			b.link(id, ids.SearchNode(searchScope))
		}
		for _, s := range kb.Sources {
			if (s.Type == "blob" || s.Type == "adls") && s.Connection == "" {
				b.link(id, ids.Storage)
			}
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
			case "gateway", "search":
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
