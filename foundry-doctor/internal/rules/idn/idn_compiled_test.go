package idn

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Golden ARM JSON compiled from internal/rules/rulestest/main.bicep with
// Bicep CLI 0.48.1: subscriptionResourceId role definitions, parameter
// principals, expression-named Foundry account and project.

func TestCompiledIDN001OnlyFoundryAccounts(t *testing.T) {
	out := run(t, "FND-IDN-001", &sdk.Input{ARM: rulestest.Model(t)})
	// The AIServices account and its project have identities; the Speech
	// account is not a Foundry account and is not evaluated.
	if out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("want pass, got %+v", out)
	}
}

func TestCompiledIDN003(t *testing.T) {
	out := run(t, "FND-IDN-003", &sdk.Input{ARM: rulestest.Model(t)})
	if len(out.Findings) != 1 {
		t.Fatalf("want exactly the uamiClientId assignment, got %+v", out.Findings)
	}
	f := out.Findings[0]
	if f.Key != "principalId" || !strings.Contains(f.Evidence, "clientId") {
		t.Errorf("finding %+v", f)
	}
	// appIdentityPrincipalId and principalId parameters were not reported.
	for _, g := range out.Findings {
		if strings.Contains(g.Evidence, "appIdentityPrincipalId") {
			t.Errorf("appIdentityPrincipalId is an object ID parameter: %+v", g)
		}
	}
}

func TestCompiledIDN004ResolvesCompiledRoleAndScope(t *testing.T) {
	out := run(t, "FND-IDN-004", &sdk.Input{ARM: rulestest.Model(t)})
	if out.Skipped != nil || len(out.Findings) != 1 {
		t.Fatalf("want the project Owner assignment, got %+v", out)
	}
	if !strings.Contains(out.Findings[0].Evidence, "Owner") || !strings.Contains(out.Findings[0].Evidence, "broad scope") {
		t.Errorf("evidence %q", out.Findings[0].Evidence)
	}
}

func TestCompiledIDN005And006DoNotRegress(t *testing.T) {
	// Role GUIDs in compiled subscriptionResourceId form are read by IDN-005
	// too; the golden template has no human principals, so it is skipped by
	// design, not passed.
	out := run(t, "FND-IDN-005", &sdk.Input{ARM: rulestest.Model(t)})
	if out.Skipped == nil || len(out.Findings) != 0 {
		t.Fatalf("want skip, got %+v", out)
	}
}
