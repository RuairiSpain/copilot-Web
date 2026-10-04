package plan_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
)

// profileCodes are the recommendation codes (XF3xx) a configuration produces.
func profileCodes(a plan.Analysis) []string {
	set := map[string]bool{}
	for _, d := range a.Diagnostics {
		if strings.HasPrefix(d.Code, "XF3") {
			set[d.Code] = true
			if d.Severity != diag.Warning || d.Pillar == "" {
				panic(fmt.Sprintf("recommendation %s must be a warning with a pillar", d.Code))
			}
		}
	}
	var out []string
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func env(name string) string { return fmt.Sprintf("defaults: {environment: %s}", name) }

func TestDevHasNoRecommendations(t *testing.T) {
	a := Run(t, env("dev"))
	MustOK(t, a)
	if got := profileCodes(a); len(got) != 0 {
		t.Fatalf("dev recommendations: %v", got)
	}
	if !a.OK() {
		t.Fatal("recommendations never block")
	}
}

func TestEnvironmentMustBeDevTestOrProd(t *testing.T) {
	if !Codes(Run(t, env("staging")), "")["XF102"] {
		t.Fatal("staging is not a valid environment")
	}
	for _, e := range []string{"dev", "test", "prod"} {
		MustOK(t, Run(t, env(e)))
	}
}

func TestBareProdAndTestRecommendations(t *testing.T) {
	prod := Run(t, env("prod"))
	want := []string{"XF304", "XF310", "XF316", "XF317", "XF320", "XF330"}
	if got := profileCodes(prod); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("prod = %v, want %v\n%s", got, want, Lines(prod.Diagnostics))
	}
	test := Run(t, env("test"))
	wantTest := []string{"XF310", "XF320", "XF330"}
	if got := profileCodes(test); fmt.Sprint(got) != fmt.Sprint(wantTest) {
		t.Fatalf("test = %v, want %v", got, wantTest)
	}
}

type waf struct {
	name, code, pillar string
	trigger            []string // overrides that provoke the recommendation
	fix                []string // overrides that satisfy it
	testToo            bool
}

var wafCases = []waf{
	{"search tier", "XF301", "Reliability", y(`search: {sku: basic}`), y(`search: {sku: standard, replicas: 3}`), false},
	{"search replicas", "XF301", "Reliability", y(`search: {sku: standard, replicas: 2}`), y(`search: {sku: standard, replicas: 3}`), false},
	{"storage redundancy", "XF302", "Reliability", y(`storage: {sku: Standard_LRS}`), y(`storage: {sku: Standard_ZRS}`), false},
	{"cosmos zone", "XF303", "Reliability", y(`cosmos: {continuousBackup: true}`), y(`cosmos: {zoneRedundant: true, continuousBackup: true}`), false},
	{"cosmos backup", "XF303", "Reliability", y(`cosmos: {zoneRedundant: true}`), y(`cosmos: {zoneRedundant: true, continuousBackup: true}`), false},
	{"locks", "XF304", "Reliability", y(`governance: {resourceLocks: false}`), y(`governance: {resourceLocks: true}`), false},
	{"private network", "XF310", "Security", y(Public), y(Private), true},
	{"local auth", "XF311", "Security", y(`security: {network: {mode: private}, roles: {admins: [a]}, localAuthentication: true}`), y(Private), true},
	{"storage keys", "XF311", "Security", y(`security: {roles: {admins: [a]}, localAuthentication: true}`, `storage: {localAuthentication: true}`), y(Private), true},
	{"purge protection", "XF312", "Security", y(`security: {roles: {admins: [a]}, purgeProtection: false}`), y(Private), false},
	{"key vault purge", "XF312", "Security", y(`keyVault: {purgeProtection: false}`), y(`keyVault: {purgeProtection: true}`), false},
	{"egress", "XF313", "Security", y(`security: {network: {mode: private, egress: azure-default}, roles: {admins: [a]}}`), y(`security: {network: {mode: private, egress: restricted}, roles: {admins: [a]}}`), false},
	{"policy", "XF316", "Security", y(`governance: {}`), y(`governance: {policyAssignments: [no-local-auth]}`), false},
	{"defender", "XF317", "Security", y(`governance: {defenderPlans: [ai]}`), y(`governance: {defenderPlans: [servers, appService, cosmosDb, ai]}`), false},
	{"identity", "XF319", "Security", y(`managedIdentity: {enabled: false}`), y(`managedIdentity: {enabled: true}`), false},
	{"diagnostics", "XF320", "Operational Excellence", y(`observability: {logAnalytics: false}`), y(`observability: {}`), true},
	{"alerts", "XF321", "Operational Excellence", y(`observability: {alerts: false}`), y(`observability: {alerts: true}`), false},
	{"agent subnet", "XF324", "Operational Excellence", y(`security: {network: {agentSubnetPrefixLength: 26}, roles: {admins: [a]}}`), y(`security: {network: {agentSubnetPrefixLength: 24}, roles: {admins: [a]}}`), false},
	{"budget", "XF330", "Cost Optimization", y(`governance: {}`), y(`governance: {budgets: {monthlyAmount: 1000}}`), true},
}

func hasCode(a plan.Analysis, code string) (bool, string) {
	for _, d := range a.Diagnostics {
		if d.Code == code {
			return true, d.Pillar
		}
	}
	return false, ""
}

func TestEachRecommendationFiresInProdAndClearsWhenFixed(t *testing.T) {
	for _, c := range wafCases {
		t.Run(c.name, func(t *testing.T) {
			a := Run(t, append(y(env("prod")), c.trigger...)...)
			if found, pillar := hasCode(a, c.code); !found || pillar != c.pillar {
				t.Fatalf("expected %s (%s):\n%s", c.code, c.pillar, Lines(a.Diagnostics))
			}
			fixed := Run(t, append(y(env("prod")), c.fix...)...)
			if found, _ := hasCode(fixed, c.code); found {
				t.Fatalf("%s should be satisfied by %v:\n%s", c.code, c.fix, Lines(fixed.Diagnostics))
			}
			if !a.OK() {
				t.Fatalf("recommendations must not block:\n%s", Lines(a.Diagnostics))
			}
		})
	}
}

func TestTestEnvironmentGetsOnlyTheSubset(t *testing.T) {
	for _, c := range wafCases {
		t.Run(c.name, func(t *testing.T) {
			a := Run(t, append(y(env("test")), c.trigger...)...)
			found, _ := hasCode(a, c.code)
			if found != c.testToo {
				t.Fatalf("%s in test = %v, want %v:\n%s", c.code, found, c.testToo, Lines(a.Diagnostics))
			}
			dev := Run(t, append(y(env("dev")), c.trigger...)...)
			if found, _ := hasCode(dev, c.code); found {
				t.Fatalf("dev must not report %s", c.code)
			}
		})
	}
}

func TestRecommendationsSkipExistingResources(t *testing.T) {
	a := Run(t, env("prod"),
		fmt.Sprintf(`search: {existingResourceId: "%s/Microsoft.Search/searchServices/s1"}`, arm),
		fmt.Sprintf(`storage: {existingResourceId: "%s/Microsoft.Storage/storageAccounts/st1"}`, arm),
		fmt.Sprintf(`cosmos: {existingResourceId: "%s/Microsoft.DocumentDB/databaseAccounts/c1"}`, arm))
	for _, code := range []string{"XF301", "XF302", "XF303"} {
		if found, _ := hasCode(a, code); found {
			t.Fatalf("%s must not judge a resource the extension does not create", code)
		}
	}
}

func TestEnvironmentOverrideDoesNotEditTheDocument(t *testing.T) {
	file := "../../examples/standalone-minimal.yaml"
	dev, err := plan.AnalyseFileWith(file, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	prod, err := plan.AnalyseFileWith(file, plan.Options{Environment: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(profileCodes(dev)) != 0 || len(profileCodes(prod)) == 0 || prod.Plan.Config.Environment != "prod" || dev.Plan.Config.Environment != "dev" {
		t.Fatalf("dev %v prod %v", profileCodes(dev), profileCodes(prod))
	}
	if _, err := plan.AnalyseFileWith(file, plan.Options{Environment: "staging"}); err == nil {
		t.Fatal("an unknown environment is an error")
	}
}

func TestExamplesMeetTheirOwnEnvironmentProfile(t *testing.T) {
	for _, name := range []string{"enterprise", "standalone-private"} {
		a, err := plan.AnalyseFile("../../examples/" + name + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		MustOK(t, a)
		if len(a.Plan.Warnings) != 0 {
			t.Fatalf("%s should be warning-free for its environment:\n%s", name, Lines(a.Plan.Warnings))
		}
	}
}

func TestPlanJSONCarriesThePillar(t *testing.T) {
	a := Run(t, env("prod"))
	b, _ := MustOK(t, a).JSON("")
	if !strings.Contains(string(b), `"pillar":"Security"`) {
		t.Fatal("warnings in the plan JSON must include the pillar")
	}
}
