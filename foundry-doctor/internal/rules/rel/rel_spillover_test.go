package rel

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	keySpillAbsent  = "provisioned-deployment-spilloverdeploymentname-request-level-spillover"
	keySpillMissing = "spilloverdeploymentname-resolve-deployment-same-account"
	keySpillPTU     = "spilloverdeploymentname-must-refer-standard-deployment-another"
	keySpillModel   = "spilloverdeploymentname-target-same-model-version"
	keySpillLoop    = "spilloverdeploymentname-loop"
)

func gpt() map[string]any {
	return map[string]any{"format": "OpenAI", "name": "gpt-4o", "version": "1"}
}

func dep(name, sku string, props map[string]any) sdk.ARMResource {
	if props == nil {
		props = map[string]any{}
	}
	if _, ok := props["model"]; !ok {
		props["model"] = gpt()
	}
	return sdk.ARMResource{Type: typeDeployment, Name: name, SKUName: sku, Properties: props}
}

func TestREL008AbsentSpilloverProperty(t *testing.T) {
	absent := map[string]any{}
	nilVal := map[string]any{"spilloverDeploymentName": nil}
	empty := map[string]any{"spilloverDeploymentName": ""}
	blank := map[string]any{"spilloverDeploymentName": "  "}
	for name, props := range map[string]map[string]any{"absent": absent, "nil": nilVal, "empty": empty, "blank": blank} {
		t.Run(name, func(t *testing.T) {
			got := eval(t, "FND-REL-008", &sdk.Input{ARM: model{dep("acct/ptu", "GlobalProvisionedManaged", props)}})
			if len(got.Findings) != 1 {
				t.Fatalf("want 1 finding, got %+v", got)
			}
			f := got.Findings[0]
			if f.Key != keySpillAbsent {
				t.Fatalf("key %q want %q", f.Key, keySpillAbsent)
			}
			if !strings.Contains(f.Evidence, "has no spilloverDeploymentName") || strings.Contains(f.Evidence, "<nil>") {
				t.Fatalf("evidence %q", f.Evidence)
			}
		})
	}
}

func TestREL008SpilloverTargets(t *testing.T) {
	ptu := func(name, spill string) sdk.ARMResource {
		return dep(name, "GlobalProvisionedManaged", map[string]any{"spilloverDeploymentName": spill})
	}
	cases := []struct {
		name string
		arm  model
		keys []string // finding keys, sorted
		skip bool
	}{
		{"standard sibling same model passes", model{ptu("acct/ptu", "std"), dep("acct/std", "Standard", nil)}, nil, false},
		{"global standard sibling passes", model{ptu("acct/ptu", "std"), dep("acct/std", "GlobalStandard", nil)}, nil, false},
		{"data zone standard sibling passes", model{ptu("acct/ptu", "std"), dep("acct/std", "DataZoneStandard", nil)}, nil, false},
		{"target does not exist", model{ptu("acct/ptu", "std")}, []string{keySpillMissing}, false},
		{"target exists only in another account", model{ptu("acct/ptu", "std"), dep("other/std", "Standard", nil)}, []string{keySpillMissing}, false},
		{"target is another PTU deployment", model{ptu("acct/ptu", "ptu2"), dep("acct/ptu2", "DataZoneProvisionedManaged", nil)}, []string{keySpillPTU, keySpillAbsent}, false},
		{"target is a different model", model{ptu("acct/ptu", "std"), dep("acct/std", "Standard", map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt-4.1", "version": "1"}})}, []string{keySpillModel}, false},
		{"target is the same model, different version", model{ptu("acct/ptu", "std"), dep("acct/std", "Standard", map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt-4o", "version": "2"}})}, []string{keySpillModel}, false},
		{"self reference is a loop", model{ptu("acct/ptu", "ptu")}, []string{keySpillLoop, keySpillPTU}, false},
		{"two PTU deployments spilling to each other loop", model{ptu("acct/a", "b"), ptu("acct/b", "a")}, []string{keySpillLoop, keySpillLoop, keySpillPTU, keySpillPTU}, false},
		{"PTU chain is flagged at the first hop", model{ptu("acct/a", "b"), ptu("acct/b", "std"), dep("acct/std", "Standard", nil)}, []string{keySpillPTU}, false},
		{"non-provisioned deployment is out of scope", model{dep("acct/std", "Standard", nil)}, nil, false},
		{"target without a literal sku is uncertain", model{ptu("acct/ptu", "std"), dep("acct/std", "", nil)}, nil, true},
		{"target name resolved only through expressions is uncertain", model{ptu("acct/ptu", "std"), dep("[format('{0}/std', parameters('a'))]", "Standard", nil)}, nil, true},
		{"literal false target is absent", model{ptu("acct/ptu", "std"), func() sdk.ARMResource {
			r := dep("acct/std", "Standard", nil)
			r.Conditional, r.Condition = true, "[false()]"
			return r
		}()}, []string{keySpillMissing}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, "FND-REL-008", &sdk.Input{ARM: tc.arm})
			if tc.skip {
				if got.Skipped == nil || len(got.Findings) != 0 {
					t.Fatalf("want uncertain skip, got %+v", got)
				}
				return
			}
			var keys []string
			for _, f := range got.Findings {
				keys = append(keys, f.Key)
			}
			if !sameStrings(keys, tc.keys) {
				t.Fatalf("keys %v want %v (%+v)", keys, tc.keys, got)
			}
			if len(tc.keys) == 0 && got.Skipped != nil {
				t.Fatalf("want pass, got skip %+v", got.Skipped)
			}
		})
	}
}

func TestREL007And008MissingARMSkip(t *testing.T) {
	for _, id := range []string{"FND-REL-007", "FND-REL-008"} {
		got := eval(t, id, &sdk.Input{})
		if got.Skipped == nil || !strings.HasPrefix(got.Skipped.Reason, sdk.SkipInputUnavailable) {
			t.Fatalf("%s: missing ARM must skip with input-unavailable: %+v", id, got)
		}
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[string]int{}
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
	}
	for _, n := range count {
		if n != 0 {
			return false
		}
	}
	return true
}
