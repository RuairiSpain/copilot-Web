package foundrylink_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
)

// R3: a connection module that receives s1.outputs.id links to the Search
// service declared in module s1 (AVM-style layout, compiled with Bicep 0.48.1).
func TestModuleOutputLinksConnectionToSearch(t *testing.T) {
	m := rulestest.Scenario(t, "avm")
	set := foundrylink.Collect(m)
	if !set.Any() {
		t.Fatal("the connection module must be collected")
	}
	ss := searchServices(m.Resources())
	if len(ss) != 1 {
		t.Fatalf("want 1 search service, got %d", len(ss))
	}
	if got := set.Classify(ss[0]); got != foundrylink.Linked {
		t.Errorf("search referenced through s1.outputs.id: %v, want Linked", got)
	}
}
