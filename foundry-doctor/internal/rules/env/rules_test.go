package env_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/env"
)

func TestRulesCompile(t *testing.T) {
	for _, r := range env.Rules() {
		if r.ID() == "" || r.Version() < 1 {
			t.Fatalf("bad rule %T", r)
		}
	}
}
