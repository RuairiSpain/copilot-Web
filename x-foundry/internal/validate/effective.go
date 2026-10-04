package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
)

func projectRules(norm *normalise.Config, p *normalise.EffectiveProject) []diag.Diagnostic {
	var out []diag.Diagnostic
	base := fmt.Sprintf("%s.projects[%s]", root, p.Name)
	models := p.Models
	kbs := nameSet(p.KnowledgeBases)
	origin := func(kind, name string) string {
		if o, ok := p.Origins[kind+":"+name]; ok {
			return o
		}
		return "project:" + p.Name
	}

	// Rule 6. The default model is a deployment name declared on the azd azure.ai.project service,
	// so only the allowed and denied sets can be checked.
	if d := models.Default; d != "" {
		switch {
		case contains(models.Denied, d):
			out = append(out, diag.Err("XF006", base+".models.default", "models.default '%s' is denied", d))
		case len(models.Allowed) > 0 && !contains(models.Allowed, d):
			out = append(out, diag.Err("XF006", base+".models.default", "models.default '%s' is not in models.allowed", d))
		}
	}

	for _, kb := range p.KnowledgeBases {
		scope := origin("knowledgeBase", kb.Name)
		for _, r := range kb.Routing.Routes {
			where := itemPath(scope, "knowledgeBases", kb.Name, "routing.routes["+r.Name+"]")
			if !kbs[r.KnowledgeBase] {
				out = append(out, diag.Err("XF005", where+".knowledgeBase", "route '%s' targets unknown knowledge base '%s' (project '%s')", r.Name, r.KnowledgeBase, p.Name))
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

// modelPolicy implements rule 113 for the gateway: the models it allows must be in the
// governance model policy. Deployments themselves are declared on the azd azure.ai.project
// service, so their SKUs and regions are not checked here.
func modelPolicy(norm *normalise.Config) []diag.Diagnostic {
	var out []diag.Diagnostic
	g := norm.Governance
	if g == nil || !g.Enabled || len(g.ModelPolicy.AllowedModels) == 0 || norm.Gateway == nil || !norm.Gateway.Enabled {
		return nil
	}
	for _, m := range norm.Gateway.Models.Allowed {
		if !contains(g.ModelPolicy.AllowedModels, m) {
			out = append(out, diag.Err("XF113", root+".gateway.models.allowed", "gateway allows model '%s' which is not in governance.modelPolicy.allowedModels", m))
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
		for i := range s.KnowledgeBases {
			kb := &s.KnowledgeBases[i]
			out = append(out, validateKnowledgeBase(kb, s.Scope)...)
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
	out = append(out, Profile(norm, declared)...)
	return diag.Dedupe(out)
}
