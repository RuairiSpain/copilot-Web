// Package ops holds the native FND-OPS-* rules for operations and governance.
package ops

import "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"

// Rules returns the rules of this group in ID order.
func Rules() []rules.Rule {
	return []rules.Rule{ops004{}, ops005{}, ops007{}, ops009{}, ops010{}}
}
