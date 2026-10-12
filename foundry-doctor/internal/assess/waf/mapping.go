// Package waf maps catalogue rules to WAF-facing controls.
package waf

import (
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

var (
	applicableGroups = map[string]bool{
		"SEC": true, "NET": true, "IDN": true, "REL": true, "OPS": true, "COST": true,
	}
	questionRules = map[string]struct {
		key, prompt string
	}{
		"FND-IDN-005": {"", "Has the team reviewed standing human admin access with current business and support context?"},
		"FND-REL-007": {"disasterRecovery.declared", "Has the team recorded its disaster recovery strategy and supporting rationale?"},
		"FND-OPS-008": {"", "Is promotion gated by evaluations that reflect the project's release decision?"},
	}
	costlyRuleIDs = map[string]bool{
		"FND-COST-001": true, "FND-COST-002": true,
		"FND-NET-010": true, "FND-NET-011": true,
		"FND-OPS-001": true, "FND-OPS-002": true, "FND-OPS-006": true,
		"FND-REL-001": true, "FND-REL-002": true, "FND-REL-003": true,
		"FND-REL-008": true, "FND-REL-009": true,
		"FND-SEC-010": true, "FND-SEC-011": true,
	}
)

// Applicable reports whether r is part of the Phase 4 WAF control set.
func Applicable(r catalog.Rule) bool {
	if !applicableGroups[r.Group] {
		return false
	}
	if strings.TrimSpace(r.Pillar) == "" {
		return false
	}
	for _, phase := range r.Phases {
		switch phase {
		case "1", "2", "3", "4":
			return true
		}
	}
	return false
}

// Definitions returns the WAF control definitions derived from the catalogue.
func Definitions(rules []catalog.Rule) []assess.Definition {
	out := make([]assess.Definition, 0, len(rules))
	for _, r := range rules {
		if !Applicable(r) {
			continue
		}
		d := assess.Definition{
			RuleID:          r.ID,
			ControlID:       r.ID,
			ControlTitle:    r.Title,
			Pillar:          r.Pillar,
			Costly:          costlyRuleIDs[r.ID] || r.Pillar == "cost",
			NoEvidenceState: fallbackState(r),
		}
		if q, ok := questionRules[r.ID]; ok {
			d.NoEvidenceState = assess.StateQuestion
			d.QuestionKey = q.key
			d.QuestionPrompt = q.prompt
		}
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b assess.Definition) int { return strings.Compare(a.RuleID, b.RuleID) })
	return out
}

func fallbackState(r catalog.Rule) assess.State {
	for _, phase := range r.Phases {
		if phase == "4" {
			return assess.StateUnknown
		}
	}
	return assess.StateSkipped
}
