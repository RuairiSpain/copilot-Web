package validate

import (
	"fmt"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
)

const gw = root + ".gateway"

func searchNames(norm *normalise.Config) map[string]bool {
	names := map[string]bool{}
	for _, s := range norm.Scopes {
		if s.Search != nil && s.Search.Enabled {
			names[firstNonEmpty(s.Search.Name, "search")] = true
			if id := s.Search.ExistingResourceID; id != "" {
				names[id[strings.LastIndex(id, "/")+1:]] = true
			}
		}
	}
	return names
}

// modelNames are the models the configuration names: allowed or default in any scope. The
// deployments themselves are declared on the azd azure.ai.project service.
func modelNames(norm *normalise.Config) map[string]bool {
	names := map[string]bool{}
	for _, s := range norm.Scopes {
		if s.Models.Default != "" {
			names[s.Models.Default] = true
		}
		for _, m := range s.Models.Allowed {
			names[m] = true
		}
	}
	return names
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func validateGateway(norm *normalise.Config) []diag.Diagnostic {
	g := norm.Gateway
	var out []diag.Diagnostic
	var registered []*normalise.EffectiveProject
	for _, p := range norm.Projects {
		if p.Gateway != nil && p.Gateway.Enabled {
			registered = append(registered, p)
		}
	}
	if g == nil || !g.Enabled {
		for _, p := range registered {
			out = append(out, diag.Err("XF111", fmt.Sprintf("%s.projects[%s].gateway", root, p.Name),
				"project '%s' registers with the gateway, but gateway.enabled is not true", p.Name))
		}
		if g != nil && len(g.Endpoints) > 0 {
			out = append(out, diag.Warn("XF111", gw+".endpoints", "gateway endpoints are ignored because gateway.enabled is false"))
		}
		return out
	}

	// Rule 13: endpoint paths (project registrations share the namespace).
	claimed := map[string]string{}
	for _, e := range g.Endpoints {
		claimed[e.Path] = "endpoint '" + e.Name + "'"
	}
	for _, p := range registered {
		pth := p.Gateway.Path
		if owner, ok := claimed[pth]; ok {
			out = append(out, diag.Err("XF013", fmt.Sprintf("%s.projects[%s].gateway.path", root, p.Name),
				"project '%s' gateway path '%s' is already used by %s", p.Name, pth, owner))
		} else {
			claimed[pth] = "project '" + p.Name + "'"
		}
	}

	// Rule 14: targets resolve.
	searches, models := searchNames(norm), modelNames(norm)
	for _, m := range g.Models.Allowed {
		models[m] = true
	}
	for _, e := range g.Endpoints {
		where := fmt.Sprintf("%s.endpoints[%s].target", gw, e.Name)
		switch e.TargetType {
		// Agent targets are azd azure.ai.agent services, which x-foundry cannot see.
		case "model":
			if !models[e.Target] {
				out = append(out, diag.Err("XF014", where, "endpoint target model '%s' is not in any models.allowed or models.default", e.Target))
			}
		case "search":
			if !searches[e.Target] {
				out = append(out, diag.Err("XF014", where, "endpoint target search '%s' does not exist or is not enabled", e.Target))
			}
		case "knowledgeBase":
			found := false
			for _, p := range norm.Projects {
				if e.Project != "" && e.Project != p.Name {
					continue
				}
				for _, kb := range p.KnowledgeBases {
					found = found || kb.Name == e.Target
				}
			}
			if !found {
				out = append(out, diag.Err("XF014", where, "endpoint target knowledge base '%s' does not exist", e.Target))
			}
		}
	}

	// Rule 15: profile references.
	quotas, limits, backends := nameSet(g.Quotas.Profiles), nameSet(g.Limits.Profiles), nameSet(g.Routing.Backends)
	for _, e := range g.Endpoints {
		where := fmt.Sprintf("%s.endpoints[%s]", gw, e.Name)
		for _, ref := range []struct {
			value string
			known map[string]bool
			label string
			field string
		}{
			{e.QuotaProfile, quotas, "quota", "quotaProfile"},
			{e.LimitProfile, limits, "limit", "limitProfile"},
			{e.RoutingProfile, backends, "routing (backend)", "routingProfile"},
		} {
			if ref.value != "" && !ref.known[ref.value] {
				out = append(out, diag.Err("XF015", where+"."+ref.field, "%s profile '%s' is not defined in the gateway", ref.label, ref.value))
			}
		}
	}
	for _, p := range registered {
		if ref := p.Gateway.QuotaProfile; ref != "" && !quotas[ref] {
			out = append(out, diag.Err("XF015", fmt.Sprintf("%s.projects[%s].gateway.quotaProfile", root, p.Name),
				"quota profile '%s' is not defined in the gateway", ref))
		}
	}

	// Tracking and the telemetry sink.
	tracking := g.TokenTracking
	for _, e := range g.Endpoints {
		if e.TokenTracking && !tracking.Enabled && e.Has("tokenTracking") {
			out = append(out, diag.Err("XF111", fmt.Sprintf("%s.endpoints[%s].tokenTracking", gw, e.Name),
				"endpoint enables token tracking but gateway tokenTracking is disabled"))
		}
	}
	if obs := norm.Observability; tracking.Enabled && (obs == nil || !obs.Enabled || !obs.LogAnalytics) {
		out = append(out, diag.Err("XF111", root+".observability", "token tracking needs observability with logAnalytics enabled"))
	}
	m := g.Models
	if len(m.Allowed) > 0 && m.Default != "" && !contains(m.Allowed, m.Default) {
		out = append(out, diag.Err("XF006", gw+".models.default", "gateway default model '%s' is not in gateway.models.allowed", m.Default))
	}
	if m.Default != "" && contains(m.Denied, m.Default) {
		out = append(out, diag.Err("XF006", gw+".models.default", "gateway default model '%s' is denied", m.Default))
	}
	if norm.Network.Mode == "private" && !g.Security.InternalOnly {
		out = append(out, diag.Err("XF021", gw+".security.internalOnly", "gateway.security.internalOnly cannot be false when network mode is 'private'"))
	}
	return out
}
