package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
)

// modelChain returns the scopes (broadest first) whose models apply to scope.
func modelChain(norm *normalise.Config, scope string) []string {
	switch scope {
	case "root":
		return []string{"root"}
	case "hub":
		return []string{"root", "hub"}
	}
	chain := []string{"root"}
	if p := norm.Project(strings.TrimPrefix(scope, "project:")); p != nil && p.InheritsHub && norm.Hub != nil && norm.Hub.Inheritance.Models {
		chain = append(chain, "hub")
	}
	return append(chain, scope)
}

// scopeModels merges model governance (default, allowed, denied) along a scope's chain.
func scopeModels(norm *normalise.Config, scope string) config.ModelConfiguration {
	var out config.ModelConfiguration
	for _, s := range modelChain(norm, scope) {
		m := norm.Scope(s).Models
		if m.Default != "" {
			out.Default = m.Default
		}
		if len(m.Allowed) > 0 {
			out.Allowed = m.Allowed
		}
		for _, d := range m.Denied {
			if !contains(out.Denied, d) {
				out.Denied = append(out.Denied, d)
			}
		}
	}
	return out
}

func scopeDeployments(norm *normalise.Config, scope string) []config.ModelDeployment {
	index := map[string]int{}
	var merged []config.ModelDeployment
	for _, s := range modelChain(norm, scope) {
		for _, d := range norm.Scope(s).Models.Deployments {
			if i, ok := index[d.Name]; ok {
				merged[i] = d
			} else {
				index[d.Name] = len(merged)
				merged = append(merged, d)
			}
		}
	}
	return merged
}

func modelKnown(m config.ModelConfiguration, name string) bool {
	if contains(m.Allowed, name) {
		return true
	}
	for _, d := range m.Deployments {
		if d.Name == name || d.Model == name {
			return true
		}
	}
	return false
}

func servedModel(m config.ModelConfiguration, name string) string {
	for _, d := range m.Deployments {
		if d.Name == name {
			return d.Model
		}
	}
	return name
}

func projectRules(norm *normalise.Config, p *normalise.EffectiveProject) []diag.Diagnostic {
	var out []diag.Diagnostic
	base := fmt.Sprintf("%s.projects[%s]", root, p.Name)
	models := p.Models
	toolboxes, mcps, kbs := nameSet(p.Toolboxes), nameSet(p.Mcps), nameSet(p.KnowledgeBases)
	connectors, agents := nameSet(p.Connectors), nameSet(p.Agents)
	origin := func(kind, name string) string {
		if o, ok := p.Origins[kind+":"+name]; ok {
			return o
		}
		return "project:" + p.Name
	}

	// Rule 6
	if d := models.Default; d != "" {
		switch served := servedModel(models, d); {
		case !modelKnown(models, d):
			out = append(out, diag.Err("XF006", base+".models.default", "models.default '%s' is neither a model deployment nor in models.allowed", d))
		case contains(models.Denied, d) || contains(models.Denied, served):
			out = append(out, diag.Err("XF006", base+".models.default", "models.default '%s' is denied", d))
		case len(models.Allowed) > 0 && !contains(models.Allowed, d) && !contains(models.Allowed, served):
			out = append(out, diag.Err("XF006", base+".models.default", "models.default '%s' is not in models.allowed", d))
		}
	}

	for _, a := range p.Agents {
		scope := origin("agent", a.Name)
		where := itemPath(scope, "agents", a.Name, "")
		label := ""
		if scope == "root" {
			label = fmt.Sprintf(" (project '%s')", p.Name)
		}
		model := firstNonEmpty(a.Model, models.Default)
		switch {
		case a.Kind == "prompt" && model == "":
			out = append(out, diag.Err("XF112", where, "prompt agent '%s' has no model and there is no models.default%s", a.Name, label))
		case model != "" && !modelKnown(models, model):
			out = append(out, diag.Err("XF005", where+".model", "agent '%s' references unknown model '%s'%s", a.Name, model, label))
		case model != "" && (contains(models.Denied, model) || contains(models.Denied, servedModel(models, model))):
			out = append(out, diag.Err("XF016", where+".model", "agent '%s' uses denied model '%s'%s", a.Name, model, label))
		}
		for _, ref := range []struct {
			names []string
			known map[string]bool
			kind  string
		}{{a.Toolboxes, toolboxes, "toolbox"}, {a.Mcps, mcps, "MCP"}, {a.KnowledgeBases, kbs, "knowledge base"}} {
			for _, name := range ref.names {
				if !ref.known[name] {
					out = append(out, diag.Err("XF005", where, "agent '%s' references unknown %s '%s'%s", a.Name, ref.kind, name, label))
				}
			}
		}
	}

	for _, t := range p.Toolboxes {
		scope := origin("toolbox", t.Name)
		for _, tool := range t.Tools {
			var known map[string]bool
			kind := ""
			switch tool.Type {
			case "mcp":
				known, kind = mcps, "MCP"
			case "knowledgeBase":
				known, kind = kbs, "knowledge base"
			default:
				continue
			}
			if !known[tool.Reference] {
				out = append(out, diag.Err("XF005", itemPath(scope, "toolboxes", t.Name, "tools["+tool.Name+"]"),
					"tool '%s' references unknown %s '%s' (project '%s')", tool.Name, kind, tool.Reference, p.Name))
			}
		}
	}

	for _, kb := range p.KnowledgeBases {
		scope := origin("knowledgeBase", kb.Name)
		for _, s := range kb.Sources {
			if s.Connection != "" && !connectors[s.Connection] {
				out = append(out, diag.Err("XF005", itemPath(scope, "knowledgeBases", kb.Name, "sources["+s.Name+"].connection"),
					"source '%s' references unknown connector '%s' (project '%s')", s.Name, s.Connection, p.Name))
			}
		}
		for _, r := range kb.Routing.Routes {
			where := itemPath(scope, "knowledgeBases", kb.Name, "routing.routes["+r.Name+"]")
			if !kbs[r.KnowledgeBase] {
				out = append(out, diag.Err("XF005", where+".knowledgeBase", "route '%s' targets unknown knowledge base '%s' (project '%s')", r.Name, r.KnowledgeBase, p.Name))
			}
			if r.When.Agent != "" && !agents[r.When.Agent] {
				out = append(out, diag.Err("XF005", where+".when.agent", "route '%s' matches unknown agent '%s' (project '%s')", r.Name, r.When.Agent, p.Name))
			}
		}
		if kb.Retrieval.SemanticRanking && p.SearchScope != "" {
			if s := norm.Scope(p.SearchScope).Search; s != nil && !s.SemanticRanking {
				out = append(out, diag.Err("XF117", itemPath(scope, "knowledgeBases", kb.Name, "retrieval.semanticRanking"),
					"retrieval.semanticRanking needs semanticRanking on the Search service in scope '%s'", p.SearchScope))
			}
		}
	}
	return out
}

// modelPolicy implements rules 16, 113 and 126: declared deployments respect allowed and
// denied sets, governance policy and data residency.
func modelPolicy(norm *normalise.Config) []diag.Diagnostic {
	var out []diag.Diagnostic
	var policy *config.ModelPolicy
	var residency []string
	if g := norm.Governance; g != nil && g.Enabled {
		policy, residency = &g.ModelPolicy, g.DataResidency
	}
	for _, s := range norm.Scopes {
		effective := scopeModels(norm, s.Scope)
		for _, d := range s.Models.Deployments {
			where := fmt.Sprintf("%s.models.deployments[%s]", scopePath(s.Scope), d.Name)
			if contains(effective.Denied, d.Model) || contains(effective.Denied, d.Name) {
				out = append(out, diag.Err("XF016", where, "deployment '%s' uses denied model '%s'", d.Name, d.Model))
			}
			embeddingModel := strings.HasPrefix(d.Model, "text-embedding")
			if len(effective.Allowed) > 0 && !embeddingModel && !contains(effective.Allowed, d.Model) && !contains(effective.Allowed, d.Name) {
				out = append(out, diag.Err("XF016", where, "deployment '%s' uses model '%s' which is not in models.allowed", d.Name, d.Model))
			}
			if len(residency) > 0 && strings.HasPrefix(d.SKU, "Global") {
				out = append(out, diag.Err("XF126", where+".sku",
					"deployment '%s' uses SKU '%s', which can process data outside governance.dataResidency; use a DataZone or regional SKU", d.Name, d.SKU))
			}
			if policy == nil {
				continue
			}
			if len(policy.DeniedModels) > 0 && contains(policy.DeniedModels, d.Model) {
				out = append(out, diag.Err("XF113", where, "governance.modelPolicy denies model '%s'", d.Model))
			}
			if len(policy.AllowedModels) > 0 && !contains(policy.AllowedModels, d.Model) {
				out = append(out, diag.Err("XF113", where, "model '%s' is not in governance.modelPolicy.allowedModels", d.Model))
			}
			if len(policy.AllowedSKUs) > 0 && !contains(policy.AllowedSKUs, d.SKU) {
				out = append(out, diag.Err("XF113", where+".sku", "SKU '%s' is not in governance.modelPolicy.allowedSkus", d.SKU))
			}
		}
	}
	if policy != nil && len(policy.AllowedModels) > 0 && norm.Gateway != nil && norm.Gateway.Enabled {
		for _, m := range norm.Gateway.Models.Allowed {
			if !contains(policy.AllowedModels, m) {
				out = append(out, diag.Err("XF113", root+".gateway.models.allowed", "gateway allows model '%s' which is not in governance.modelPolicy.allowedModels", m))
			}
		}
	}
	return out
}

// widening: a spoke may narrow, but not widen, the allowed models it inherits (XF115).
func widening(declared *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, p := range declared.Projects {
		if len(p.Models.Allowed) == 0 {
			continue
		}
		parent := declared.Models.Allowed
		if declared.Hub != nil && p.InheritHub && len(declared.Hub.Models.Allowed) > 0 {
			parent = declared.Hub.Models.Allowed
		}
		var wider []string
		for _, m := range p.Models.Allowed {
			if !contains(parent, m) {
				wider = append(wider, m)
			}
		}
		sort.Strings(wider)
		if len(parent) > 0 && len(wider) > 0 {
			out = append(out, diag.Err("XF115", fmt.Sprintf("%s.projects[%s].models.allowed", root, p.Name),
				"project '%s' allows %s, which its parent scope does not allow; projects may only narrow inherited allowed models", p.Name, strings.Join(wider, ", ")))
		}
	}
	return out
}

func requiredTags(norm *normalise.Config) []diag.Diagnostic {
	g := norm.Governance
	if g == nil || !g.Enabled {
		return nil
	}
	var out []diag.Diagnostic
	for _, p := range norm.Projects {
		for _, tag := range g.RequiredTags {
			if _, ok := p.Tags[tag]; !ok {
				out = append(out, diag.Err("XF104", fmt.Sprintf("%s.projects[%s].tags", root, p.Name),
					"required tag '%s' is missing for project '%s'; add it under tags or defaults.tags", tag, p.Name))
			}
		}
	}
	return out
}

// Effective runs every rule that needs inheritance and implicit resources resolved.
func Effective(norm *normalise.Config, declared *config.XFoundry) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, p := range norm.Projects {
		out = append(out, projectRules(norm, p)...)
	}
	adls := false
	for _, s := range norm.Scopes {
		deployments := scopeDeployments(norm, s.Scope)
		for i := range s.KnowledgeBases {
			kb := &s.KnowledgeBases[i]
			out = append(out, validateKnowledgeBase(kb, s.Scope, deployments)...)
			for _, src := range kb.Sources {
				adls = adls || src.Type == "adls"
			}
		}
	}
	if st := norm.Storage; st != nil && adls && !st.HierarchicalNamespace && st.ExistingResourceID == "" {
		out = append(out, diag.Err("XF107", root+".storage.hierarchicalNamespace", "an adls knowledge source needs storage.hierarchicalNamespace"))
	}
	out = append(out, modelPolicy(norm)...)
	out = append(out, widening(declared)...)
	out = append(out, validateGateway(norm)...)
	out = append(out, requiredTags(norm)...)
	return diag.Dedupe(out)
}
