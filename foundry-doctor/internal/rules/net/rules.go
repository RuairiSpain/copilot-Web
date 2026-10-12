// Package net holds the native FND-NET-* rules for networking.
//
// The rules read the compiled ARM template only (Input.ARM) and link private endpoints, subnets and
// DNS zones through internal/graph. Anything that needs live ARM (connection approval state, zones in
// other subscriptions) is skipped with control-plane-unavailable. A value that is an ARM expression is
// never treated as pass or fail: the rule skips or reports uncertain.
package net

import "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"

// Rules returns the rules of this group in ID order.
func Rules() []rules.Rule {
	return []rules.Rule{
		newNet001(), newNet002(), newNet003(), newNet004(), newNet005(), newNet007(), newNet008(),
	}
}
