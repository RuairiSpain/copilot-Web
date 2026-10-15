package cost

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Every finding of a rule that can emit several findings per resource carries
// a distinct Key, so no two share a fingerprint.
func TestCostFindingsHaveDistinctKeys(t *testing.T) {
	// A budget that violates several checks at once.
	bad := sdk.ARMResource{Type: typeBudget, Name: "b", Properties: map[string]any{"properties": map[string]any{
		"category": "Cost", "amount": float64(0),
	}}}
	out := run(t, "FND-COST-001", &sdk.Input{ARM: model{bad}})
	if len(out.Findings) < 3 {
		t.Fatalf("want several findings, got %+v", out)
	}
	seen := map[string]bool{}
	for _, f := range out.Findings {
		if f.Key == "" {
			t.Errorf("finding without Key: %q", f.Evidence)
		}
		if seen[f.Key] {
			t.Errorf("duplicate key %q", f.Key)
		}
		seen[f.Key] = true
	}
}

func TestCost004KeysAndUnevaluatedNote(t *testing.T) {
	search := sdk.ARMResource{Type: typeSearch, Name: "s", SKUName: "standard", Properties: map[string]any{}}
	apim := sdk.ARMResource{Type: typeAPIM, Name: "a", Properties: map[string]any{}}
	out := run(t, "FND-COST-004", &sdk.Input{ARM: model{search, apim}})
	if len(out.Findings) < 2 {
		t.Fatalf("want findings, got %+v", out)
	}
	seen := map[string]bool{}
	for _, f := range out.Findings {
		k := f.Resource.Name + "|" + f.Key
		if f.Key == "" || seen[k] {
			t.Errorf("missing or duplicate key %q for %q", k, f.Evidence)
		}
		seen[k] = true
	}
	// A conditional cost-relevant resource next to a finding stays visible.
	cond := sdk.ARMResource{Type: typeSearch, Name: "c", SKUName: "standard", Conditional: true, Condition: "[parameters('deploy')]"}
	out = run(t, "FND-COST-004", &sdk.Input{ARM: model{search, cond}})
	if len(out.Findings) == 0 || !strings.Contains(out.Findings[0].Evidence, "not evaluated") || !strings.Contains(out.Findings[0].Evidence, "conditional-deployment-unresolved") {
		t.Fatalf("unevaluated resources must be named in the first finding: %+v", out)
	}
}
