package validate

import (
	"fmt"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
)

// Well-Architected pillars.
const (
	Reliability           = "Reliability"
	Security              = "Security"
	OperationalExcellence = "Operational Excellence"
	CostOptimization      = "Cost Optimization"
)

type finding struct{ path, text string }

type recommendation struct {
	code   string
	pillar string
	test   bool // also recommended in the test environment (prod always)
	check  func(n *normalise.Config, d *config.XFoundry) []finding
}

// Recommendations come from the Azure Architecture Center's "Baseline Microsoft Foundry chat
// reference architecture" and its "basic" counterpart (see docs/waf-profiles.md). They are
// warnings: they never block a deployment. dev has none, test a security/operations/cost
// subset, prod all of them.
var recommendations = []recommendation{
	{"XF301", Reliability, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		var out []finding
		for _, s := range n.Scopes {
			sr := s.Search
			if sr == nil || !sr.Enabled || sr.ExistingResourceID != "" {
				continue
			}
			where := scopePath(s.Scope) + ".search"
			if sr.SKU == "free" || sr.SKU == "basic" {
				out = append(out, finding{where + ".sku", "the " + sr.SKU + " tier has no availability-zone support; use standard or higher"})
			}
			if sr.Replicas < 3 {
				out = append(out, finding{where + ".replicas", fmt.Sprintf("use at least three replicas so Azure AI Search spreads across availability zones (now %d)", sr.Replicas)})
			}
		}
		return out
	}},
	{"XF302", Reliability, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		if s := n.Storage; s != nil && s.Enabled && s.ExistingResourceID == "" && (s.SKU == "Standard_LRS" || s.SKU == "Premium_LRS") {
			return []finding{{path("storage", "sku"), "use zone-redundant storage (Standard_ZRS or Standard_GZRS), not " + s.SKU}}
		}
		return nil
	}},
	{"XF303", Reliability, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		c := n.Cosmos
		if c == nil || !c.Enabled || c.ExistingResourceID != "" {
			return nil
		}
		var out []finding
		if !c.ZoneRedundant {
			out = append(out, finding{path("cosmos", "zoneRedundant"), "use zone redundancy for the Cosmos DB account that holds agent state"})
		}
		if !c.ContinuousBackup {
			out = append(out, finding{path("cosmos", "continuousBackup"), "turn on continuous backup (7-day point-in-time restore) for agent state"})
		}
		return out
	}},
	{"XF304", Reliability, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		if g := n.Governance; g == nil || !g.Enabled || !g.ResourceLocks {
			return []finding{{path("governance", "resourceLocks"), "add delete locks to Search, Cosmos DB and Storage to prevent accidental loss (governance.resourceLocks)"}}
		}
		return nil
	}},
	{"XF310", Security, true, func(n *normalise.Config, _ *config.XFoundry) []finding {
		if n.Network.Mode != "private" {
			return []finding{{path("security", "network", "mode"), "block public access: use private mode so every PaaS service is reached through private endpoints (now " + n.Network.Mode + ")"}}
		}
		return nil
	}},
	{"XF311", Security, true, func(n *normalise.Config, _ *config.XFoundry) []finding {
		var out []finding
		if n.LocalAuthentication {
			out = append(out, finding{path("security", "localAuthentication"), "disable local (key-based) authentication and use Microsoft Entra ID"})
		}
		if s := n.Storage; s != nil && s.LocalAuthentication {
			out = append(out, finding{path("storage", "localAuthentication"), "disable key-based access on the storage account"})
		}
		if c := n.Cosmos; c != nil && c.LocalAuthentication {
			out = append(out, finding{path("cosmos", "localAuthentication"), "disable key-based access on Cosmos DB"})
		}
		for _, s := range n.Scopes {
			if s.Search != nil && s.Search.Enabled && s.Search.LocalAuthentication {
				out = append(out, finding{scopePath(s.Scope) + ".search.localAuthentication", "disable API-key access on Azure AI Search"})
			}
		}
		return out
	}},
	{"XF312", Security, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		var out []finding
		if !n.PurgeProtection {
			out = append(out, finding{path("security", "purgeProtection"), "enable purge protection"})
		}
		if k := n.KeyVault; k != nil && k.Enabled && !k.PurgeProtection {
			out = append(out, finding{path("keyVault", "purgeProtection"), "enable purge protection on Key Vault"})
		}
		return out
	}},
	{"XF313", Security, false, func(n *normalise.Config, d *config.XFoundry) []finding {
		if n.Network.Mode == "private" && d.Security.Network.Egress == "azure-default" {
			return []finding{{path("security", "network", "egress"), "force outbound traffic through a firewall (egress restricted or private-only) instead of default Azure egress"}}
		}
		return nil
	}},
	{"XF314", Security, true, func(n *normalise.Config, _ *config.XFoundry) []finding {
		implicit := map[string]bool{}
		for _, i := range n.Implicit {
			if i.Kind == "model-deployment" {
				implicit[i.Scope+"/"+i.Name] = true
			}
		}
		var out []finding
		for _, s := range n.Scopes {
			for _, d := range s.Models.Deployments {
				if d.RaiPolicy == "" && !strings.HasPrefix(d.Model, "text-embedding") {
					hint := "bind a content-filter policy (raiPolicy)"
					if implicit[s.Scope+"/"+d.Name] {
						hint = "declare this deployment so you can bind a content-filter policy (raiPolicy)"
					}
					out = append(out, finding{fmt.Sprintf("%s.models.deployments[%s]", scopePath(s.Scope), d.Name), hint})
				}
			}
		}
		return out
	}},
	{"XF315", Security, true, func(n *normalise.Config, _ *config.XFoundry) []finding {
		var out []finding
		for _, s := range n.Scopes {
			for _, m := range s.Mcps {
				if len(m.AllowedTools) == 0 {
					out = append(out, finding{fmt.Sprintf("%s.mcps[%s].allowedTools", scopePath(s.Scope), m.Name), "restrict the tools an agent may call on this MCP server"})
				}
			}
		}
		return out
	}},
	{"XF316", Security, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		if g := n.Governance; g == nil || !g.Enabled || len(g.PolicyAssignments) == 0 {
			return []finding{{path("governance", "policyAssignments"), "assign Azure Policy to require no local authentication, explicit network rules, encryption and allowed regions"}}
		}
		return nil
	}},
	{"XF317", Security, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		var have []string
		if g := n.Governance; g != nil && g.Enabled {
			have = g.DefenderPlans
		}
		var missing []string
		for _, plan := range []string{"servers", "appService", "cosmosDb", "ai"} {
			if !contains(have, plan) {
				missing = append(missing, plan)
			}
		}
		if len(missing) > 0 {
			return []finding{{path("governance", "defenderPlans"), "enable Microsoft Defender for Cloud plans: " + strings.Join(missing, ", ")}}
		}
		return nil
	}},
	{"XF319", Security, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		if m := n.ManagedIdentity; m != nil && !m.Enabled {
			return []finding{{path("managedIdentity", "enabled"), "use managed identities for every component rather than disabling them"}}
		}
		return nil
	}},
	{"XF320", OperationalExcellence, true, func(n *normalise.Config, _ *config.XFoundry) []finding {
		if o := n.Observability; o == nil || !o.Enabled || !o.LogAnalytics {
			return []finding{{path("observability"), "send every service's logs to a Log Analytics workspace (observability with logAnalytics)"}}
		}
		return nil
	}},
	{"XF321", OperationalExcellence, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		if o := n.Observability; o != nil && o.Enabled && !o.Alerts {
			return []finding{{path("observability", "alerts"), "keep alerts on so failures and throttling are noticed"}}
		}
		return nil
	}},
	{"XF322", OperationalExcellence, true, func(n *normalise.Config, _ *config.XFoundry) []finding {
		var out []finding
		for _, s := range n.Scopes {
			for _, d := range s.Models.Deployments {
				if d.VersionUpgradeOption != "NoAutoUpgrade" {
					out = append(out, finding{fmt.Sprintf("%s.models.deployments[%s].versionUpgradeOption", scopePath(s.Scope), d.Name),
						"pin the model version (NoAutoUpgrade) so updates go through change control"})
				}
			}
		}
		return out
	}},
	{"XF323", OperationalExcellence, true, func(n *normalise.Config, _ *config.XFoundry) []finding {
		for _, p := range n.Projects {
			if p.Evaluation != nil && p.Evaluation.Enabled {
				return nil
			}
		}
		return []finding{{path("evaluation", "enabled"), "run evaluations (a test suite of realistic questions) before promoting agents"}}
	}},
	{"XF324", OperationalExcellence, false, func(n *normalise.Config, _ *config.XFoundry) []finding {
		if n.Network.AgentSubnet == "create" && n.Network.AgentSubnetPrefixLength > 24 {
			return []finding{{path("security", "network", "agentSubnetPrefixLength"),
				fmt.Sprintf("size the agent subnet for peak concurrent sessions; /24 is recommended (now /%d)", n.Network.AgentSubnetPrefixLength)}}
		}
		return nil
	}},
	{"XF330", CostOptimization, true, func(n *normalise.Config, _ *config.XFoundry) []finding {
		g := n.Governance
		if g == nil || !g.Enabled || (g.Budgets.MonthlyAmount == 0 && g.Budgets.MonthlyTokens == 0) {
			return []finding{{path("governance", "budgets"), "set a budget and alerts (monthlyAmount or monthlyTokens)"}}
		}
		return nil
	}},
}

// Profile returns the Well-Architected recommendations for the configured environment as
// warnings. dev returns none.
func Profile(n *normalise.Config, d *config.XFoundry) []diag.Diagnostic {
	if n.Environment != "test" && n.Environment != "prod" {
		return nil
	}
	var out []diag.Diagnostic
	for _, r := range recommendations {
		if n.Environment == "test" && !r.test {
			continue
		}
		for _, f := range r.check(n, d) {
			w := diag.Warn(r.code, f.path, "[%s] %s", r.pillar, f.text)
			w.Pillar = r.pillar
			out = append(out, w)
		}
	}
	return out
}
