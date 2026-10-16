package env

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// N3-1: notes ride next to findings as uncertain skips.
func TestFindingsCarryNotesAsSkips(t *testing.T) {
	// Two environments share a group (a finding); a third has no .env file
	// (a note) and a fourth has no group (a note).
	p := prov{
		{Name: "a", Subscription: "s", ResourceGroup: "rg"},
		{Name: "b", Subscription: "s", ResourceGroup: "rg"},
		{Name: "c", Subscription: "s"},
		{Name: "d", Status: StatusMissing},
	}
	res := eval(t, "FND-ENV-001", p, &sdk.Input{})
	if res.Skipped != nil {
		t.Fatalf("rule-level skip %q next to a finding", res.Skipped.Reason)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(res.Findings))
	}
	var sawMissing, sawNoRG bool
	for _, s := range res.Skips {
		if !strings.HasPrefix(s.Reason, "uncertain: ") {
			t.Errorf("skip %q is not uncertain", s.Reason)
		}
		sawMissing = sawMissing || strings.Contains(s.Reason, "environment d: .env missing")
		sawNoRG = sawNoRG || strings.Contains(s.Reason, "environment c: no AZURE_SUBSCRIPTION_ID or AZURE_RESOURCE_GROUP")
		if s.RuleID != "FND-ENV-001" {
			t.Errorf("skip rule id = %q", s.RuleID)
		}
	}
	if !sawMissing || !sawNoRG {
		t.Errorf("notes lost: %+v", res.Skips)
	}
}

func TestNotesWithoutFindingsStayRuleLevel(t *testing.T) {
	p := prov{{Name: "a", Subscription: "s", ResourceGroup: "x"}, {Name: "b", Subscription: "s", ResourceGroup: "y"}, {Name: "c", Status: StatusMissing}}
	res := eval(t, "FND-ENV-001", p, &sdk.Input{})
	if len(res.Findings) != 0 || res.Skipped == nil || len(res.Skips) != 0 || !strings.HasPrefix(res.Skipped.Reason, "uncertain: ") {
		t.Fatalf("want a rule-level uncertain skip, got %+v", res)
	}
}

// N3-6: finding keys name the check, the variable and the sorted pair, and
// never a value or a value fingerprint, so a rotated value keeps its key.
func TestFindingKeysIgnoreValues(t *testing.T) {
	mk := func(val string) []Environment {
		return []Environment{
			{Name: "b", Subscription: "s1", ResourceGroup: "rgb", Values: vals("AZURE_ENV_NAME", "b", "SVC_ENDPOINT", val, "ONLY_B", "1", "OTHER_NAME", "x")},
			{Name: "a", Subscription: "s2", ResourceGroup: "rga", Values: vals("AZURE_ENV_NAME", "a", "SVC_ENDPOINT", val, "OTHER_NAME", "y")},
		}
	}
	keys := func(id string, envs []Environment) []string {
		res := eval(t, id, prov(envs), &sdk.Input{})
		var out []string
		for _, f := range res.Findings {
			if f.Key == "" {
				t.Errorf("%s: finding %q has no key", id, f.Evidence)
			}
			out = append(out, f.Key)
		}
		return out
	}
	for _, id := range []string{"FND-ENV-002", "FND-ENV-004"} {
		before, after := keys(id, mk("https://one.services.ai.azure.com")), keys(id, mk("https://two.services.ai.azure.com"))
		if len(before) == 0 {
			t.Fatalf("%s: no findings", id)
		}
		if strings.Join(before, "|") != strings.Join(after, "|") {
			t.Errorf("%s: keys changed with the value: %v vs %v", id, before, after)
		}
		for _, k := range before {
			if strings.Contains(k, "https") || strings.Contains(k, Fingerprint("https://one.services.ai.azure.com")) {
				t.Errorf("%s: key %q carries a value", id, k)
			}
		}
	}
	got := keys("FND-ENV-002", mk("https://one.services.ai.azure.com"))
	if len(got) != 1 || got[0] != "identical-value:SVC_ENDPOINT:a,b" {
		t.Errorf("ENV-002 keys = %v", got)
	}
	// The pair is sorted, so the order environments are listed in is irrelevant.
	rev := mk("https://one.services.ai.azure.com")
	rev[0], rev[1] = rev[1], rev[0]
	if r := keys("FND-ENV-002", rev); len(r) != 1 || r[0] != got[0] {
		t.Errorf("reversed order changes the key: %v", r)
	}
}

func TestEnv001And003Keys(t *testing.T) {
	r1 := eval(t, "FND-ENV-001", prov{{Name: "d", Subscription: "s", ResourceGroup: "rg"}, {Name: "p", Subscription: "s", ResourceGroup: "rg"}}, &sdk.Input{})
	if len(r1.Findings) != 1 || r1.Findings[0].Key != "shared-subscription-resource-group:d,p" {
		t.Errorf("ENV-001 = %+v", r1.Findings)
	}
	pol3 := pol{TierPolicyKey: map[string]any{"p": "prod", "d": "dev"}}
	r3 := eval(t, "FND-ENV-003", prov{{Name: "d", ResourceGroup: "rg"}, {Name: "p", ResourceGroup: "rg"}}, &sdk.Input{Policy: pol3})
	if len(r3.Findings) != 1 || r3.Findings[0].Key != "prod-shares-target:resource group:AZURE_RESOURCE_GROUP:d,p" {
		t.Errorf("ENV-003 = %+v", r3.Findings)
	}
}
