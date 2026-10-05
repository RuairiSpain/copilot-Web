package sec_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/sec"
)

func TestRulesCompile(t *testing.T) {
	for _, r := range sec.Rules() {
		if r.ID() == "" || r.Version() < 1 {
			t.Fatalf("bad rule %T", r)
		}
	}
}
