package cost_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cost"
)

func TestRulesCompile(t *testing.T) {
	for _, r := range cost.Rules() {
		if r.ID() == "" || r.Version() < 1 {
			t.Fatalf("bad rule %T", r)
		}
	}
}
