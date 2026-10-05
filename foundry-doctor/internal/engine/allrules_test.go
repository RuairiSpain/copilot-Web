//go:build allrules

package engine_test

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
)

// TestAllPhase1Registered requires every native phase 1 catalogue rule to have exactly one Go
// implementation of the equal version, and no implementation of any other rule. It fails until the
// Wave 2 group packages are complete, so it runs only with: go test -tags allrules ./internal/engine
func TestAllPhase1Registered(t *testing.T) {
	c, err := engine.DefaultCatalogue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Registry.CheckComplete(rules.Phase1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Registry.Resolve(c.Packs, rules.Selection{}, rules.Selector{}); err != nil {
		t.Fatal(err)
	}
}
