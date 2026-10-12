// Package cfg holds the native FND-CFG-* rules for azure.yaml structure and configuration.
//
// Every rule is a pure function of model.Input: no file system, no network, no process. A value that is
// an ${VAR} reference or an unresolved ARM expression is "cannot tell" (skipped or uncertain), never a
// pass or a fail. Rules never set Severity, Profile or Fingerprint; the engine stamps them.
package cfg

import "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"

// Rules returns the rules of this group in ID order.
func Rules() []rules.Rule {
	return []rules.Rule{
		rule001{}, rule002{}, rule003{}, rule004{}, rule005{}, rule006{}, rule007{}, rule011{}, rule012{},
	}
}
