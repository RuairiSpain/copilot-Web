package preflight

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

// Phase 2 gate round 2: G4 (DEP-010) and G5/G7(ii) (DEP-008 subscription scope).

func TestDEP010HistoryCountsInheritedSubscriptionLock(t *testing.T) {
	storage := res("Microsoft.Storage/storageAccounts", "stfoo123")
	lockAt := func(scope string) azure.Lock {
		return azure.Lock{ID: scope + "/providers/Microsoft.Authorization/locks/l", Name: "l", Scope: scope, Level: "CanNotDelete"}
	}
	cases := []struct {
		name  string
		locks []azure.Lock
		count int
		want  outcome
		text  string
	}{
		{"CanNotDelete on the subscription, history near the limit, fails", []azure.Lock{lockAt(subID)}, 790, oFail, "covering the resource group"},
		{"the subscription lock names its own scope", []azure.Lock{lockAt(subID)}, 800, oFail, "scope " + subID + ","},
		{"lock exactly at the group is preferred when both exist", []azure.Lock{lockAt(subID), lockAt(rgID)}, 790, oFail, "scope " + rgID + ","},
		{"subscription lock, history well under the limit, is not a failure", []azure.Lock{lockAt(subID)}, 100, oUncertain, "Incremental mode"},
		{"a lock on a resource inside the group does not count", []azure.Lock{lockAt(rgID + "/providers/Microsoft.Storage/storageAccounts/other")}, 799, oUncertain, "Incremental mode"},
		{"a lock on another group does not count", []azure.Lock{lockAt(subID + "/resourceGroups/other")}, 799, oPass, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{locks: c.locks, deploys: azure.DeploymentCount{Count: c.count}}
			out := run(t, deps(f), "FND-DEP-010", storage)
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if c.text != "" && !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
		})
	}
}

func TestDEP010NestedDeploymentsAreAnUncertainNote(t *testing.T) {
	nested := res("Microsoft.Resources/deployments", "inner")
	out := run(t, deps(&fake{}), "FND-DEP-010", res("Microsoft.Storage/storageAccounts", "stfoo123"), nested)
	if classify(out) != oUncertain || !strings.Contains(out.Skipped.Reason, "1 nested Microsoft.Resources/deployments") {
		t.Errorf("nested deployment not reported: %+v", out)
	}
	out = run(t, deps(&fake{}), "FND-DEP-010", res("Microsoft.Storage/storageAccounts", "stfoo123"))
	if classify(out) != oPass {
		t.Errorf("no nested deployment must stay a pass: %+v", out)
	}
}

func subDeps(changes ...azure.WhatIfChange) Deps {
	d := deps(&fake{whatif: azure.WhatIfResult{Changes: changes}})
	d.Template = []byte(`{"$schema":"` + subSchema + `","resources":[]}`)
	return d
}

func TestDEP008SubscriptionScopeWithResourceGroup(t *testing.T) {
	newRG := subID + "/resourceGroups/new-rg"
	roleAssign := subID + "/providers/Microsoft.Authorization/roleAssignments/ra1"
	inRG := rgID + "/providers/Microsoft.Storage/storageAccounts/a"
	otherSub := "/subscriptions/other/resourceGroups/x/providers/Microsoft.Storage/storageAccounts/z"
	cases := []struct {
		name     string
		changes  []azure.WhatIfChange
		approved []string
		noRG     bool
		want     outcome
		text     string
	}{
		{"create of the resource group itself is a note", []azure.WhatIfChange{{ResourceID: newRG, ChangeType: azure.ChangeCreate}}, nil, false, oUncertain, "outside the target resource group but inside the subscription"},
		{"create of a subscription-level role assignment is a note", []azure.WhatIfChange{{ResourceID: roleAssign, ChangeType: azure.ChangeCreate}}, nil, false, oUncertain, roleAssign},
		{"a note never names a blocked finding", []azure.WhatIfChange{{ResourceID: newRG, ChangeType: azure.ChangeCreate}}, nil, false, oUncertain, "confirm it is intended"},
		{"delete under the subscription outside the group stays a finding", []azure.WhatIfChange{{ResourceID: newRG, ChangeType: azure.ChangeDelete}}, nil, false, oFail, "outside the target resource group"},
		{"create outside the subscription stays a finding", []azure.WhatIfChange{{ResourceID: otherSub, ChangeType: azure.ChangeCreate}}, nil, false, oFail, otherSub},
		{"create inside a non-target resource group stays a finding", []azure.WhatIfChange{{ResourceID: subID + "/resourceGroups/other-rg/providers/Microsoft.Storage/storageAccounts/z", ChangeType: azure.ChangeCreate}}, nil, false, oFail, "other-rg"},
		{"modify inside another resource group stays a finding", []azure.WhatIfChange{{ResourceID: subID + "/resourceGroups/other-rg/providers/Microsoft.Storage/storageAccounts/z", ChangeType: azure.ChangeModify}}, nil, false, oFail, "other-rg"},
		{"deploy inside another resource group stays a finding", []azure.WhatIfChange{{ResourceID: subID + "/resourceGroups/other-rg/providers/Microsoft.Storage/storageAccounts/z", ChangeType: azure.ChangeDeploy}}, nil, false, oFail, "other-rg"},
		{"modify of a subscription-level resource stays a finding", []azure.WhatIfChange{{ResourceID: roleAssign, ChangeType: azure.ChangeModify}}, nil, false, oFail, roleAssign},
		{"approved scope silences a finding in another resource group", []azure.WhatIfChange{{ResourceID: subID + "/resourceGroups/other-rg/providers/Microsoft.Storage/storageAccounts/z", ChangeType: azure.ChangeModify}}, []string{subID + "/resourceGroups/other-rg"}, false, oUncertain, "Incremental mode"},
		{"approved scope silences the note", []azure.WhatIfChange{{ResourceID: roleAssign, ChangeType: azure.ChangeCreate}}, []string{subID + "/providers/Microsoft.Authorization"}, false, oUncertain, "Incremental mode"},
		{"in the group is unaffected", []azure.WhatIfChange{{ResourceID: inRG, ChangeType: azure.ChangeCreate}}, nil, false, oUncertain, "Incremental mode"},
		{"resource group unset keeps the per-change skip", []azure.WhatIfChange{{ResourceID: newRG, ChangeType: azure.ChangeCreate}}, nil, true, oSkipped, "cross-resource-group scope policy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := subDeps(c.changes...)
			d.ApprovedScopes = c.approved
			if c.noRG {
				d.Target.ResourceGroup = ""
			}
			out := run(t, d, "FND-DEP-008", res("Microsoft.Storage/storageAccounts", "stfoo"))
			if got := classify(out); got != c.want {
				t.Fatalf("got %s want %s: %+v", got, c.want, out)
			}
			if !strings.Contains(textOf(out), c.text) {
				t.Errorf("output lacks %q: %q", c.text, textOf(out))
			}
			if c.name == "approved scope silences the note" && strings.Contains(textOf(out), "outside the target resource group") {
				t.Errorf("approved change still noted: %q", textOf(out))
			}
		})
	}
	// A resource-group-scope template keeps the finding for the same create.
	d := deps(&fake{whatif: azure.WhatIfResult{Changes: []azure.WhatIfChange{{ResourceID: newRG, ChangeType: azure.ChangeCreate}}}})
	if out := run(t, d, "FND-DEP-008", res("Microsoft.Storage/storageAccounts", "stfoo")); classify(out) != oFail {
		t.Errorf("resource-group-scope template must keep the finding: %+v", out)
	}
}
