package preflight

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// G7(i): through the real adapter, a subscription-scope what-if is posted to
// the subscription path (no resource group) and carries a top-level location.
func TestRealAdapterSubscriptionScopeWhatIfBody(t *testing.T) {
	rt := &routedRT{t: t, routes: map[string]string{}}
	d := realDeps(t, rt)
	d.Template = contractTemplate(subSchema)
	d.Parameters = map[string]any{"location": "eastus"}
	out := runInput(t, d, "FND-DEP-008", &sdk.Input{ARM: fakeARM{[]sdk.ARMResource{res("Microsoft.Storage/storageAccounts", "stfoo123")}}})
	if classify(out) != oUncertain {
		t.Fatalf("got %+v", out)
	}
	path := "/subscriptions/" + cSub + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/whatIf"
	if !rt.called("POST", path) {
		t.Fatalf("what-if not posted to the subscription path; calls: %v", rt.calls)
	}
	for _, c := range rt.calls {
		if strings.Contains(strings.ToLower(c), "/resourcegroups/") && strings.Contains(c, "whatIf") {
			t.Errorf("a resource group is in the what-if path: %s", c)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(rt.bodies["POST "+path]), &body); err != nil {
		t.Fatal(err)
	}
	if body["location"] != "eastus" {
		t.Errorf("top-level location = %v, body %v", body["location"], body)
	}
	props, _ := body["properties"].(map[string]any)
	if props == nil || props["mode"] != "Incremental" {
		t.Fatalf("properties = %v", body["properties"])
	}
	if _, nested := props["location"]; nested {
		t.Error("location must be top-level, not inside properties")
	}
}

// G7: object/array parameters are not supported and the skip says so.
func TestWhatIfSkipNamesUnsupportedObjectArrayParameters(t *testing.T) {
	storage := res("Microsoft.Storage/storageAccounts", "stfoo")
	d := deps(&fake{})
	d.Template = tmplJSON(rgSchema, `"tags":{"type":"object"},"zones":{"type":"array","defaultValue":[]},"name":{"type":"string"}`)
	d.Parameters = map[string]any{"tags": map[string]any{"a": "b"}, "zones": []any{"1"}, "name": "x"}
	out := run(t, d, "FND-DEP-008", storage)
	if classify(out) != oSkipped {
		t.Fatalf("got %+v", out)
	}
	for _, w := range []string{"template parameters unresolved: tags, zones", "object/array parameters are not supported: tags, zones"} {
		if !strings.Contains(out.Skipped.Reason, w) {
			t.Errorf("reason lacks %q: %q", w, out.Skipped.Reason)
		}
	}
	// An unresolved scalar alone does not claim the object/array limitation.
	d.Template = tmplJSON(rgSchema, `"name":{"type":"string"}`)
	d.Parameters = nil
	out = run(t, d, "FND-DEP-008", storage)
	if classify(out) != oSkipped || strings.Contains(out.Skipped.Reason, "object/array") {
		t.Errorf("unexpected: %+v", out)
	}
}
