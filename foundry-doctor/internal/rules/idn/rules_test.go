package idn_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/idn"
)

func TestRulesCompile(t *testing.T) {
	for _, r := range idn.Rules() {
		if r.ID() == "" || r.Version() < 1 {
			t.Fatalf("bad rule %T", r)
		}
	}
}
