package idn

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// R4: Owner and User Access Administrator at `scope: account` compile to
// [resourceId('Microsoft.CognitiveServices/accounts', 'aiservices')]. The model
// must not fold that to the bare name, or the scope cannot be classified.
func TestCompiledIDN004ReportsResourceScopedOwnerAndUAA(t *testing.T) {
	m := rulestest.Golden(t, "idnscope")
	for _, r := range m.Resources() {
		if strings.HasSuffix(r.Type, "/roleAssignments") && !strings.HasPrefix(r.Scope, "[resourceId('Microsoft.CognitiveServices/accounts'") {
			t.Fatalf("scope was folded: %q", r.Scope)
		}
	}
	out := run(t, "FND-IDN-004", &sdk.Input{ARM: m})
	if out.Skipped != nil {
		t.Fatalf("skipped: %+v", out.Skipped)
	}
	var roles []string
	for _, f := range out.Findings {
		for _, role := range []string{"Owner", "User Access Administrator", "Contributor"} {
			if strings.HasPrefix(f.Evidence, role+" granted") {
				roles = append(roles, role)
			}
		}
		if !strings.Contains(f.Evidence, "at resource scope") {
			t.Errorf("evidence should name the resource scope: %q", f.Evidence)
		}
	}
	if len(roles) != 2 || roles[0] == roles[1] {
		t.Fatalf("want Owner and User Access Administrator (Contributor at one resource is by design not reported), got %v: %+v", roles, out.Findings)
	}
}
