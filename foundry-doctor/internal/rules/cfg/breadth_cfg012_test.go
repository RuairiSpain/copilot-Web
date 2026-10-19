package cfg

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type armRegionStub struct{ regions []string }

func (a armRegionStub) Resources() []sdk.ARMResource {
	var out []sdk.ARMResource
	for i, r := range a.regions {
		out = append(out, sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts", Name: "acct" + string(rune('a'+i)), Region: r})
	}
	return out
}

// FND-CFG-012 version 5: region names are recognised against the bundled
// lists; a name-form value with punctuation is a finding with a suggestion;
// an unlisted alphanumeric name and a display name stay uncertain.
func TestCFG012RegionNames(t *testing.T) {
	loc := func(v string) Providers { return Providers{Env: fakeEnv{true, map[string]string{"AZURE_LOCATION": v}}} }
	y := "services: {}\n"
	runReviewCases(t, []reviewCase{
		{name: "known-sdk-region-passes", id: "FND-CFG-012", yaml: y, p: loc("uksouth")},
		{name: "known-foundry-only-region-passes", id: "FND-CFG-012", yaml: y, p: loc("polandcentral")},
		{name: "typo-with-hyphen-is-a-finding", id: "FND-CFG-012", yaml: y, p: loc("eastus-typo"), want: 1, contains: "not an Azure location name"},
		{name: "underscore-is-a-finding", id: "FND-CFG-012", yaml: y, p: loc("east_us"), want: 1},
		{name: "punctuation-finding-has-low-confidence", id: "FND-CFG-012", yaml: y, p: loc("westeurope!"), want: 1},
		{name: "empty-is-still-a-finding", id: "FND-CFG-012", yaml: y, p: loc(""), want: 1, contains: "empty"},
		{name: "unlisted-alphanumeric-name-is-uncertain-with-suggestion", id: "FND-CFG-012", yaml: y, p: loc("eastsu"), skip: uncertainPrefix + "AZURE_LOCATION \"eastsu\" is not in the bundled list", notInSkip: "is not set"},
		{name: "unlisted-name-suggests-nearest", id: "FND-CFG-012", yaml: y, p: loc("westeurop"), skip: "Did you mean \"westeurope\"?"},
		{name: "far-name-has-no-suggestion", id: "FND-CFG-012", yaml: y, p: loc("zzzzzzzzzzzzzzz"), skip: uncertainPrefix, notInSkip: "Did you mean"},
		{name: "display-name-of-known-region-is-uncertain", id: "FND-CFG-012", yaml: y, p: loc("East US"), skip: "it names the region eastus"},
		{name: "display-name-with-parenthesis-is-never-a-finding", id: "FND-CFG-012", yaml: y, p: loc("East US 2 (Stage)"), skip: uncertainPrefix},
		{name: "mixed-case-known-region-is-uncertain", id: "FND-CFG-012", yaml: y, p: loc("WestEurope"), skip: "it names the region westeurope"},
		{name: "reference-is-not-judged", id: "FND-CFG-012", yaml: y, p: loc("${L}-x"), skip: uncertainPrefix + "AZURE_LOCATION is set"},
		{name: "no-environment-is-input-unavailable", id: "FND-CFG-012", yaml: y, skip: sdk.SkipInputUnavailable},
	})
}

func TestCFG012FindingCarriesSuggestionAndLowConfidence(t *testing.T) {
	p := Providers{Env: fakeEnv{true, map[string]string{"AZURE_LOCATION": "eastus-typo"}}}
	res := run(t, find(RegisterWith(p), "FND-CFG-012"), inputFor(t, "services: {}\n"))
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %+v", res.Findings)
	}
	f := res.Findings[0]
	if f.Key != "location-not-region-name" || f.Confidence != sdk.ConfidenceLow {
		t.Fatalf("key/confidence = %q/%q", f.Key, f.Confidence)
	}
	if !strings.Contains(f.Recommendation, `Did you mean "eastus"?`) {
		t.Fatalf("recommendation lacks suggestion: %q", f.Recommendation)
	}
}

func TestCFG012ARMLocations(t *testing.T) {
	p := Providers{Env: fakeEnv{true, map[string]string{"AZURE_LOCATION": "eastus"}}}
	cases := []struct {
		name    string
		regions []string
		want    int
	}{
		{"punctuated-literal-is-a-finding", []string{"eastus-typo"}, 1},
		{"valid-literal-passes", []string{"eastus", "uksouth"}, 0},
		{"expression-is-not-judged", []string{"[parameters('location')]"}, 0},
		{"global-is-not-judged", []string{"global"}, 0},
		{"display-name-is-not-judged", []string{"East US 2 (Stage)"}, 0},
		{"empty-is-not-judged", []string{""}, 0},
		{"two-bad-resources-two-findings", []string{"a-b", "c-d"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := run(t, find(RegisterWith(p), "FND-CFG-012"), &sdk.Input{AzureYAML: doc(t, "services: {}\n"), ARM: armRegionStub{tc.regions}})
			if len(res.Findings) != tc.want {
				t.Fatalf("findings = %+v want %d", res.Findings, tc.want)
			}
		})
	}
	t.Run("arm-finding-survives-missing-environment", func(t *testing.T) {
		res := run(t, find(Register(), "FND-CFG-012"), &sdk.Input{AzureYAML: doc(t, "services: {}\n"), ARM: armRegionStub{[]string{"x-y"}}})
		if len(res.Findings) != 1 || res.Skipped != nil {
			t.Fatalf("got %+v", res)
		}
	})
	t.Run("no-environment-and-clean-arm-is-input-unavailable", func(t *testing.T) {
		res := run(t, find(Register(), "FND-CFG-012"), &sdk.Input{AzureYAML: doc(t, "services: {}\n"), ARM: armRegionStub{[]string{"eastus"}}})
		if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable) {
			t.Fatalf("got %+v", res)
		}
	})
}

func TestRegionHelpers(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		d    int
	}{{"", "", 0}, {"abc", "abc", 0}, {"kitten", "sitting", 3}, {"eastus", "eastu", 1}, {"", "abc", 3}} {
		if got := editDistance(tc.a, tc.b); got != tc.d {
			t.Errorf("editDistance(%q,%q)=%d want %d", tc.a, tc.b, got, tc.d)
		}
	}
	if r, d := nearestRegion("eastus"); r != "eastus" || d != 0 {
		t.Errorf("nearest eastus = %q %d", r, d)
	}
	// Deterministic tie: eastus and westus are both 1 away from "xestus"? No:
	// the first alphabetical candidate at the minimum distance wins.
	a, _ := nearestRegion("eastu")
	b, _ := nearestRegion("eastu")
	if a != b || a != "eastus" {
		t.Errorf("nearest eastu = %q / %q", a, b)
	}
	for _, r := range knownRegions() {
		if !regionNameShape.MatchString(r) {
			t.Errorf("known region %q is not letters and digits", r)
		}
	}
}
