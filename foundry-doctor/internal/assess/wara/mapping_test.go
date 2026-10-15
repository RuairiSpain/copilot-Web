package wara

import (
	"context"
	"slices"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func TestEveryWaraRuleHasMapping(t *testing.T) {
	rules, err := catalog.Load(context.Background(), ruledata.FS, ruledata.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		if !slices.Contains(r.Basis, "wara") {
			continue
		}
		if got := Lookup(r.ID); len(got) == 0 {
			t.Fatalf("missing wara mapping for %s", r.ID)
		}
	}
}
