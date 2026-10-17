package net

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Review M7: an AVM-style private-endpoint module receives privateLinkServiceId
// as a parameter. After armmodel substitutes the caller's value the endpoint
// correlates with the account, so NET-001 passes instead of being uncertain.
func TestPEInModuleResolvesThroughSubstitutedTarget(t *testing.T) {
	m := rulestest.Scenario(t, "pemod")
	out := runRule(t, net001{}, m.Resources(), nil)
	if out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("NET-001 must resolve the module endpoint and pass: %+v", out)
	}
	// Without the endpoint module the private account has no endpoint.
	var without []sdk.ARMResource
	for _, r := range m.Resources() {
		if !strings.EqualFold(r.Type, "Microsoft.Network/privateEndpoints") {
			without = append(without, r)
		}
	}
	out = runRule(t, net001{}, without, nil)
	if len(out.Findings) != 1 {
		t.Fatalf("want one finding without the endpoint, got %+v", out)
	}
	// NET-003 sees the endpoint in scope (no zone group): needs the cloud key,
	// and with it reports the missing zone group.
	out = runRule(t, net003{}, m.Resources(), nil)
	if out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, "profile-key-missing:network.cloud") {
		t.Fatalf("endpoint is in scope, want profile-key-missing: %+v", out)
	}
	out = runRule(t, net003{}, m.Resources(), fakePolicy{"network.cloud": "public"})
	if len(out.Findings) == 0 {
		t.Fatalf("module endpoint without a zone group must be reported: %+v", out)
	}
	// NET-005: endpoint and vnet regions are the same parameter chain.
	out = runRule(t, net005{}, m.Resources(), nil)
	if out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("NET-005 got %+v", out)
	}
}
