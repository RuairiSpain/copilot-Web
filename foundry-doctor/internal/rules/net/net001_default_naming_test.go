package net

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// ADR-018 amendment 3: rules.capDefaultDerived caps confidence only when the
// finding's Key or Evidence names the defaulted property (or the default fed
// the condition). NET-001 reads publicNetworkAccess and networkAcls.defaultAction,
// which armmodel records as the default paths properties.publicNetworkAccess
// and properties.networkAcls. This test fails if the finding stops naming
// them, which would make the cap silently stop applying.
func TestNET001FindingNamesTheDefaultedProperties(t *testing.T) {
	out := runRule(t, net001{}, []sdk.ARMResource{acct("a", m{})}, nil)
	if len(out.Findings) != 1 {
		t.Fatalf("want one finding on an account with the properties unset: %+v", out)
	}
	f := out.Findings[0]
	text := strings.ToLower(f.Key + "\n" + f.Evidence)
	for _, name := range []string{"publicnetworkaccess", "networkacls"} {
		if !strings.Contains(text, name) {
			t.Errorf("finding must name %q in Key or Evidence; key %q evidence %q", name, f.Key, f.Evidence)
		}
	}
	// The disabled branch reads publicNetworkAccess too.
	out = runRule(t, net001{}, []sdk.ARMResource{acct("b", m{"publicNetworkAccess": "Disabled"})}, nil)
	if len(out.Findings) != 1 {
		t.Fatalf("want one finding for Disabled without an endpoint: %+v", out)
	}
	if got := strings.ToLower(out.Findings[0].Key + "\n" + out.Findings[0].Evidence); !strings.Contains(got, "publicnetworkaccess") {
		t.Fatalf("disabled branch must name publicNetworkAccess: %q", got)
	}
}
