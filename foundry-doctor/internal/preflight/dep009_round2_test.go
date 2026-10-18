package preflight

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Phase 2 gate round 2, G2: DEP-009 content and cap.

func TestDEP009SendsTagsSkuKindIdentityZones(t *testing.T) {
	r := res("Microsoft.CognitiveServices/accounts", "acct")
	r.Properties = map[string]any{"disableLocalAuth": true}
	r.Tags = map[string]any{"env": "prod"}
	r.SKU = map[string]any{"name": "S0", "tier": "Standard"}
	r.Kind = "AIServices"
	r.Identity = map[string]any{"type": "SystemAssigned"}
	r.Zones = []string{"1", "2"}
	f := &fake{}
	out := run(t, deps(f), "FND-DEP-009", r)
	if len(f.restrReqs) != 1 {
		t.Fatalf("requests = %d", len(f.restrReqs))
	}
	c := f.restrReqs[0].Content
	for _, k := range []string{"name", "properties", "tags", "sku", "kind", "identity", "zones"} {
		if _, ok := c[k]; !ok {
			t.Errorf("content lacks %q: %#v", k, c)
		}
	}
	if c["kind"] != "AIServices" || !reflect.DeepEqual(c["zones"], []any{"1", "2"}) {
		t.Errorf("kind/zones wrong: %#v", c)
	}
	if classify(out) != oUncertain || strings.Contains(out.Skipped.Reason, "partial content") {
		t.Errorf("complete content is still uncertain (never a pass) but not partial: %+v", out)
	}
	// Absent members are not invented.
	f = &fake{}
	run(t, deps(f), "FND-DEP-009", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	for _, k := range []string{"tags", "sku", "kind", "identity", "zones"} {
		if _, ok := f.restrReqs[0].Content[k]; ok {
			t.Errorf("absent member %q was sent", k)
		}
	}
}

func TestDEP009DroppedFieldsDowngradeDenyToUncertain(t *testing.T) {
	deny := []azure.PolicyRestriction{{Kind: azure.RestrictionObservedDenial, Effect: "deny", AssignmentID: "a1", Message: "blocked"}}
	for name, mod := range map[string]func(*sdk.ARMResource){
		"expression tag": func(r *sdk.ARMResource) { r.Tags = map[string]any{"env": "[parameters('env')]"} },
		"credential-shaped tag": func(r *sdk.ARMResource) {
			r.Tags = map[string]any{"note": "DefaultEndpointsProtocol=https;AccountName=a;" + "Account" + "Key=zzzz=="}
		},
		"expression sku":  func(r *sdk.ARMResource) { r.SKU = map[string]any{"name": "[parameters('sku')]"} },
		"expression prop": func(r *sdk.ARMResource) { r.Properties = map[string]any{"publicNetworkAccess": "[parameters('p')]"} },
	} {
		t.Run(name, func(t *testing.T) {
			r := res("Microsoft.Storage/storageAccounts", "stfoo123")
			mod(&r)
			out := run(t, deps(&fake{restr: deny}), "FND-DEP-009", r)
			if classify(out) != oUncertain {
				t.Fatalf("a denial on partial content must not fail: %+v", out)
			}
			if !strings.Contains(out.Skipped.Reason, "not asserted as a failure") || !strings.Contains(out.Skipped.Reason, "a1") {
				t.Errorf("note missing: %q", out.Skipped.Reason)
			}
		})
	}
	// Control: the same denial on complete content still fails.
	if out := run(t, deps(&fake{restr: deny}), "FND-DEP-009", res("Microsoft.Storage/storageAccounts", "stfoo123")); classify(out) != oFail {
		t.Errorf("full content must keep failing: %+v", out)
	}
}

func TestDEP009CapsPolicyPosts(t *testing.T) {
	var rs []sdk.ARMResource
	for i := 0; i < maxPolicyChecks+7; i++ {
		rs = append(rs, res("Microsoft.Storage/storageAccounts", fmt.Sprintf("st%03d", i)))
	}
	f := &fake{}
	out := run(t, deps(f), "FND-DEP-009", rs...)
	if len(f.restrReqs) != maxPolicyChecks {
		t.Errorf("posts = %d, want %d", len(f.restrReqs), maxPolicyChecks)
	}
	if classify(out) != oUncertain || !strings.Contains(out.Skipped.Reason, "not checked for 7 resource(s)") {
		t.Errorf("remainder not reported as not checked: %+v", out)
	}
	f = &fake{}
	out = run(t, deps(f), "FND-DEP-009", rs[:maxPolicyChecks]...)
	if strings.Contains(textOf(out), "not checked for") || len(f.restrReqs) != maxPolicyChecks {
		t.Errorf("at the cap: %d posts, %q", len(f.restrReqs), textOf(out))
	}
}
