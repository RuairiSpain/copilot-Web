// Package idn holds the native FND-IDN-* rules for identity and role assignments.
package idn

import "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"

// Rules returns the rules of this group in ID order. FND-IDN-005 is not a Phase 1 rule.
func Rules() []rules.Rule {
	return []rules.Rule{newIDN001(), newIDN002(), newIDN003(), newIDN004(), newIDN006()}
}
