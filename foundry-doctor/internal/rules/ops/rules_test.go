package ops_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/ops"
)

func TestRulesCompile(t *testing.T) {
	for _, r := range ops.Rules() {
		if r.ID() == "" || r.Version() < 1 {
			t.Fatalf("bad rule %T", r)
		}
	}
}
