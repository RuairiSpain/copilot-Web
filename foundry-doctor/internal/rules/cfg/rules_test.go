package cfg_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cfg"
)

func TestRulesCompile(t *testing.T) {
	for _, r := range cfg.Rules() {
		if r.ID() == "" || r.Version() < 1 {
			t.Fatalf("bad rule %T", r)
		}
	}
}
