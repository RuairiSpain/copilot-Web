package rules

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Round-2 R9 and R10 (ADR-010 Amendment 2): the classification is explicit.
func TestSkipClassRound2Decisions(t *testing.T) {
	cases := []struct {
		reason string
		want   SkipClass
	}{
		// By design: nothing to evaluate, or the profile has no key.
		{"profile-key-missing:network.cloud: private endpoints are in scope but the cloud cannot be determined", SkipByDesign},
		{"no-split-skill-in-project; no-explicit-query-definitions", SkipByDesign},
		{"project-has-no-agent-services", SkipByDesign},
		{"no-resilience-objective-declared", SkipByDesign},
		{"not-applicable: project declares no model deployments", SkipByDesign},
		// Hiding: a required input is missing, so the check could not run.
		{"budget-managed-outside-repository", SkipHidesCheck},
		{sdk.SkipInputUnavailable + ": subscription Defender pricing state is not fully available in the template", SkipHidesCheck},
		{sdk.SkipInputUnavailable + ": policy assignments live outside the scanned template", SkipHidesCheck},
		{"uncertain: no lock resources are declared in the template and locks can be applied later or at parent scope", SkipHidesCheck},
		{"knowledge-source-kind-without-backing-index-input", SkipHidesCheck},
		{"index-metadata-not-readable", SkipHidesCheck},
		{sdk.SkipInputUnavailable, SkipHidesCheck},
	}
	for _, tc := range cases {
		if got := ClassifySkip(sdk.Skip{Reason: tc.reason}); got != tc.want {
			t.Errorf("%q: class %v, want %v", tc.reason, got, tc.want)
		}
	}
}
