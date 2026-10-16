package env

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// N2-7: a service-level literal resourceGroup is the same in every
// environment, so its uncertain note is said once.
func TestENV001ServiceLiteralNoteIsNotRepeated(t *testing.T) {
	lit := []string{"services.web.resourceGroup"}
	var envs prov
	for _, n := range []string{"a", "b", "c", "d"} {
		envs = append(envs, Environment{Name: n, Subscription: "s", ResourceGroup: "rg-" + n, LiteralRGAt: lit})
	}
	res := eval(t, "FND-ENV-001", envs, &sdk.Input{})
	if res.Skipped == nil {
		t.Fatalf("want an uncertain skip, got %+v", res.Findings)
	}
	if n := strings.Count(res.Skipped.Reason, "services.web.resourceGroup"); n != 1 {
		t.Fatalf("note repeated %d times: %q", n, res.Skipped.Reason)
	}
	if strings.Contains(res.Skipped.Reason, "more") {
		t.Fatalf("duplicate notes still crowd the list: %q", res.Skipped.Reason)
	}
	// Two different services stay two notes.
	envs[0].LiteralRGAt = []string{"services.web.resourceGroup", "services.api.resourceGroup"}
	envs[1].LiteralRGAt = envs[0].LiteralRGAt
	res = eval(t, "FND-ENV-001", envs, &sdk.Input{})
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "services.api.resourceGroup") {
		t.Fatalf("distinct notes lost: %+v", res.Skipped)
	}
}

// Task 1: the unwired-provider skip names what is missing.
func TestUnwiredProviderSkipNamesTheInput(t *testing.T) {
	for _, r := range Register() {
		res, err := r.Evaluate(t.Context(), &sdk.Input{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable+": ") || res.Skipped.Reason == sdk.SkipInputUnavailable+": " {
			t.Errorf("%s: %+v", r.ID(), res.Skipped)
		}
	}
}
