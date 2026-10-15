package rel

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// B2: one module deployed twice is two distinct findings with distinct
// fingerprints (no Finding.Key fallback needed for different resources).
func TestREL001TwoSearchModulesAreTwoFindings(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	got := eval(t, "FND-REL-001", &sdk.Input{ARM: m})
	if len(got.Findings) != 2 {
		t.Fatalf("want 2 findings, got %d: %+v", len(got.Findings), got)
	}
	a, b := got.Findings[0], got.Findings[1]
	if a.Resource.Name == b.Resource.Name {
		t.Errorf("both findings name %q", a.Resource.Name)
	}
	a.RuleID, b.RuleID = "FND-REL-001", "FND-REL-001"
	if findings.Fingerprint(a) == findings.Fingerprint(b) {
		t.Error("fingerprints must differ")
	}
}

// B1: a Search service in a conditional module is not reported.
func TestREL001ConditionalModuleIsNotReported(t *testing.T) {
	got := eval(t, "FND-REL-001", &sdk.Input{ARM: rulestest.Scenario(t, "condmod")})
	if len(got.Findings) != 0 {
		t.Fatalf("conditional/absent module must not produce findings: %+v", got.Findings)
	}
	if got.Skipped == nil || !strings.Contains(got.Skipped.Reason, "conditional or looped") {
		t.Errorf("want an uncertain skip, got %+v", got.Skipped)
	}
}

func TestREL001LoopedAndNestedModulesAreNotReported(t *testing.T) {
	for _, s := range []string{"loopmod", "nested"} {
		got := eval(t, "FND-REL-001", &sdk.Input{ARM: rulestest.Scenario(t, s)})
		if len(got.Findings) != 0 || got.Skipped == nil {
			t.Errorf("%s: want skipped, got %+v", s, got)
		}
	}
}

// M13: a parameter-valued lock scope is unknown, not "none".
func TestLockCoversGroupParameterScopeIsUnknown(t *testing.T) {
	cases := []struct {
		scope string
		want  lockCover
	}{
		{"", coverYes},
		{"[resourceGroup().id]", coverYes},
		{"[parameters('lockScope')]", coverUnknown},
		{"[variables('scope')]", coverUnknown},
		{"[format('/subscriptions/{0}/resourceGroups/{1}', subscription().subscriptionId, parameters('rg'))]", coverUnknown},
		{"[resourceId('Microsoft.Search/searchServices', 'x')]", coverNone},
		{"/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.Search/searchServices/x", coverNone},
	}
	for _, tc := range cases {
		if got := lockCoversGroup(tc.scope); got != tc.want {
			t.Errorf("lockCoversGroup(%q) = %v, want %v", tc.scope, got, tc.want)
		}
	}
}

func TestREL004ParameterScopedLockSkipsAsUncorrelated(t *testing.T) {
	search := res(typeSearch, searchName, "standard", nil)
	got := eval(t, "FND-REL-004", &sdk.Input{ARM: model{search, lock("[parameters('lockScope')]")}})
	if got.Skipped == nil || !strings.Contains(got.Skipped.Reason, "could not be correlated") {
		t.Fatalf("want could-not-be-correlated skip, got %+v", got)
	}
}

// Every finding a rel rule emits carries a Key, so the runner never has to
// derive one (and never prints the missing-Key diagnostic).
func TestRelFindingsCarryKeys(t *testing.T) {
	cases := map[string]model{
		"FND-REL-001": {res(typeSearch, "s", "standard", map[string]any{"replicaCount": float64(1)})},
		"FND-REL-005": {res(typeSearch, "s", "standard", map[string]any{"replicaCount": float64(13), "partitionCount": float64(5)})},
	}
	for id, arm := range cases {
		got := eval(t, id, &sdk.Input{ARM: arm})
		if len(got.Findings) == 0 {
			t.Fatalf("%s: expected findings", id)
		}
		keys := map[string]bool{}
		for _, f := range got.Findings {
			if f.Key == "" {
				t.Errorf("%s: finding without Key: %+v", id, f)
			}
			if keys[f.Key] {
				t.Errorf("%s: duplicate Key %q on one resource", id, f.Key)
			}
			keys[f.Key] = true
		}
	}
}
