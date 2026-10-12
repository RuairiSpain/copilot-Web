//go:build allrules

package app_test

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
)

// TestAllRulesWired verifies the real registry: every phase 1 native rule has an implementation, and a run of the
// default pack on a fixture project reports no "not-implemented" skips. Run with -tags allrules once the rule
// packages are complete.
func TestAllRulesWired(t *testing.T) {
	cat, err := engine.DefaultCatalogue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.Registry.CheckComplete(rules.Phase1); err != nil {
		t.Fatal(err)
	}
	o := base(fixtureProject(t, "synthetic-foundry"), cat, "")
	o.Formats = []string{"json"}
	r := run(t, o)
	if r.code > 1 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	rep := decode(t, r.stdout)
	if n := rep.Summary.SkippedByReason["not-implemented"]; n != 0 {
		t.Fatalf("%d rules are catalogued but not implemented", n)
	}
}
