package rel

import (
	"sort"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func lockLevelRes(scope, level string) sdk.ARMResource {
	l := lock(scope)
	l.Properties = map[string]any{"level": level}
	return l
}

func findingKeys(r sdk.Result) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Resource.Type+"="+f.Key)
	}
	sort.Strings(out)
	return out
}

func TestREL004MissingAndReadOnlyLocks(t *testing.T) {
	acct := sdk.ARMResource{Type: typeFoundry, Name: "acct", Kind: "AIServices"}
	cosmos := res(typeCosmos, "cosmos", "", nil)
	storage := res(typeStorage, "stor", "Standard_GRS", nil)
	search := res(typeSearch, "srch", "standard", nil)
	kv := sdk.ARMResource{Type: "Microsoft.KeyVault/vaults", Name: "kv"}
	rgScope := "[resourceGroup().id]"
	cases := []struct {
		name   string
		arm    model
		policy pol
		keys   []string
		skip   string // substring expected in the per-resource skips
	}{
		{
			name: "every stateful dependency without a lock is a finding",
			arm:  model{acct, cosmos, storage, search},
			keys: []string{typeCosmos + "=missing-delete-lock", typeFoundry + "=missing-delete-lock", typeSearch + "=missing-delete-lock", typeStorage + "=missing-delete-lock"},
		},
		{
			name: "resource group CanNotDelete lock covers all",
			arm:  model{acct, cosmos, storage, search, lockLevelRes(rgScope, "CanNotDelete")},
		},
		{
			name: "only the locked resource passes, the rest are findings",
			arm:  model{acct, cosmos, lockLevelRes("[resourceId('Microsoft.DocumentDB/databaseAccounts', 'cosmos')]", "CanNotDelete")},
			keys: []string{typeFoundry + "=missing-delete-lock"},
		},
		{
			name: "ReadOnly resource group lock is reported for each uncovered target",
			arm:  model{acct, storage, lockLevelRes(rgScope, "ReadOnly")},
			keys: []string{typeFoundry + "=readonly-lock-instead-of-delete-lock", typeStorage + "=readonly-lock-instead-of-delete-lock"},
		},
		{
			name: "ReadOnly plus CanNotDelete on the same group passes",
			arm:  model{storage, lockLevelRes(rgScope, "ReadOnly"), lockLevelRes(rgScope, "CanNotDelete")},
		},
		{
			name: "ReadOnly resource lock with CanNotDelete group lock passes",
			arm:  model{storage, lockLevelRes("[resourceId('Microsoft.Storage/storageAccounts', 'stor')]", "ReadOnly"), lockLevelRes("", "CanNotDelete")},
		},
		{
			name: "NotSpecified level does not count",
			arm:  model{storage, lockLevelRes("", "NotSpecified")},
			keys: []string{typeStorage + "=missing-delete-lock"},
		},
		{
			name: "Key Vault is not a verified target",
			arm:  model{kv},
		},
		{
			name:   "locks managed outside the template are uncertain, not a finding",
			arm:    model{acct, storage},
			policy: pol{"reliability.locksManagedOutsideTemplate": true},
			skip:   "managed outside the template",
		},
		{
			name:   "policy false behaves like absent",
			arm:    model{storage},
			policy: pol{"reliability.locksManagedOutsideTemplate": false},
			keys:   []string{typeStorage + "=missing-delete-lock"},
		},
		{
			name:   "locks managed outside the template still reports a ReadOnly-only resource",
			arm:    model{storage, lockLevelRes("", "ReadOnly")},
			policy: pol{"reliability.locksManagedOutsideTemplate": true},
			keys:   []string{typeStorage + "=readonly-lock-instead-of-delete-lock"},
		},
		{
			name:   "locks managed outside the template with a covering lock passes",
			arm:    model{storage, lockLevelRes("", "CanNotDelete")},
			policy: pol{"reliability.locksManagedOutsideTemplate": true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &sdk.Input{ARM: tc.arm}
			if tc.policy != nil {
				in.Policy = tc.policy
			}
			got := eval(t, "FND-REL-004", in)
			if tc.skip != "" {
				if got.Skipped == nil || len(got.Findings) != 0 || !strings.Contains(got.Skipped.Reason, tc.skip) {
					t.Fatalf("want skip %q, got %+v", tc.skip, got)
				}
				return
			}
			want := append([]string(nil), tc.keys...)
			sort.Strings(want)
			if g := findingKeys(got); strings.Join(g, ",") != strings.Join(want, ",") {
				t.Fatalf("keys %v want %v", g, want)
			}
			if len(want) == 0 && got.Skipped != nil {
				t.Fatalf("want pass, got skip %+v", got.Skipped)
			}
			for _, f := range got.Findings {
				if f.Evidence == "" || f.Key == "" {
					t.Fatalf("finding needs evidence and key: %+v", f)
				}
			}
		})
	}
}
