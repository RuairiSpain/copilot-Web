package cfg

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// n3Eval runs one rule over y and returns the whole result.
func n3Eval(t *testing.T, id, y string, p Providers) sdk.Result {
	t.Helper()
	r := find(RegisterWith(p), id)
	if r == nil {
		t.Fatalf("rule %s missing", id)
	}
	return run(t, r, &sdk.Input{AzureYAML: doc(t, y)})
}

func wantSkipPrefixed(t *testing.T, res sdk.Result, wantSubstr string) {
	t.Helper()
	for _, s := range res.Skips {
		if !strings.HasPrefix(s.Reason, uncertainPrefix) {
			t.Errorf("skip %q lacks the %q prefix", s.Reason, uncertainPrefix)
		}
		if strings.Contains(s.Reason, wantSubstr) {
			return
		}
	}
	t.Errorf("no skip contains %q: %+v", wantSubstr, res.Skips)
}

// N3-1: a note never disappears because the rule also has a finding.
func TestFindingsCarryNotesAsSkips(t *testing.T) {
	const conn = "services:\n  c:\n    host: azure.ai.connection\n    authType: ApiKey\n    credentials:\n      key: aaa\n      secret: bbb\n      password: ccc\n"
	hostedRes := hostedHead + "    container:\n      resources:\n        cpu: \"64\"\n        memory: 500M\n"
	tests := []struct {
		name, id, yaml string
		findings       int
		note           string
	}{
		{"005 three literals and a name-only note", "FND-CFG-005",
			conn + "    env:\n      API_KEY: hello\n", 3, "looks secret-bearing by name only"},
		{"001 multi-document file with a bad name", "FND-CFG-001",
			"name: Bad_Name\nservices:\n  a:\n    host: azure.ai.agent\n    project: p\n    kind: prompt\n    model: m\n    instructions: i\n---\nname: other\n", 1, "multiple YAML documents"},
		{"003 toolbox finding and an undeclared-deployments note", "FND-CFG-003",
			"services:\n  p:\n    host: azure.ai.project\n  m:\n    host: azure.ai.memory-store\n    uses: [p]\n    chatModel: x\n  a:\n    host: azure.ai.agent\n    project: p\n    toolboxes: [nope]\n", 1, "deployments of project"},
		{"004 container finding and a quantity note", "FND-CFG-004", hostedRes, 1, "container.resources.memory"},
		{"011 legacy host and an unparseable extension constraint", "FND-CFG-011",
			"requiredVersions:\n  extensions:\n    azure.ai.agents: not-a-version\nservices:\n  a:\n    host: microsoft.foundry\n", 1, "azure.ai.agents"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := n3Eval(t, tc.id, tc.yaml, Providers{})
			if res.Skipped != nil {
				t.Fatalf("rule-level skip %q next to findings", res.Skipped.Reason)
			}
			if len(res.Findings) != tc.findings {
				t.Fatalf("findings = %d, want %d: %+v", len(res.Findings), tc.findings, res.Findings)
			}
			wantSkipPrefixed(t, res, tc.note)
		})
	}
}

// Without findings the notes stay a single rule-level uncertain skip.
func TestNotesWithoutFindingsStayRuleLevel(t *testing.T) {
	res := n3Eval(t, "FND-CFG-005", "services:\n  a:\n    host: azure.ai.agent\n    env:\n      API_KEY: hello\n", Providers{})
	if len(res.Findings) != 0 || res.Skipped == nil || len(res.Skips) != 0 {
		t.Fatalf("want a rule-level skip only, got %+v", res)
	}
	if !strings.HasPrefix(res.Skipped.Reason, uncertainPrefix) {
		t.Errorf("skip %q is not uncertain", res.Skipped.Reason)
	}
}

// N3-2: CFG-009 never calls a trigger it does not understand not-applicable.
func TestCFG009UnderstoodAndUnknownTriggers(t *testing.T) {
	routine := func(trig string) string {
		return "services:\n  r:\n    host: azure.ai.routine\n    triggers:\n      default:\n" + trig
	}
	tests := []struct {
		name     string
		yaml     string
		skip     string // substring of the rule-level skip reason
		findings int
	}{
		{"beta.8 spelling, too frequent", routine("        type: recurring\n        cron_expression: \"* * * * *\"\n        time_zone: UTC\n"), "", 1},
		{"beta.8 spelling, fine", routine("        type: recurring\n        cron_expression: \"0 * * * *\"\n        time_zone: UTC\n"), "", 0},
		{"camelCase recurring is uncertain", routine("        type: recurring\n        cronExpression: \"* * * * *\"\n        timeZone: UTC\n"), uncertainPrefix + "r: recurring trigger default uses cronExpression", 0},
		{"schedule type is uncertain", routine("        type: schedule\n        cronExpression: \"* * * * *\"\n        timeZone: UTC\n"), uncertainPrefix + "r: trigger default has type schedule", 0},
		{"schedule type with snake case keys is uncertain", routine("        type: schedule\n        cron_expression: \"* * * * *\"\n"), uncertainPrefix + "r: trigger default has type schedule", 0},
		{"recurring without any cron key is uncertain", routine("        type: recurring\n"), uncertainPrefix + "r: recurring trigger default has no cron_expression", 0},
		{"timer is not evaluated, not not-applicable", routine("        type: timer\n        at: \"2030-01-01T00:00:00Z\"\n"), uncertainPrefix + "r: trigger default type timer", 0},
		{"missing type is uncertain", routine("        cronExpression: \"* * * * *\"\n"), uncertainPrefix + "r: trigger default type is missing", 0},
		{"no routine services is not applicable", "services:\n  a:\n    host: azure.ai.agent\n", sdk.SkipNotApplicable, 0},
		{"routine without triggers is not applicable", "services:\n  r:\n    host: azure.ai.routine\n", sdk.SkipNotApplicable, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := n3Eval(t, "FND-CFG-009", tc.yaml, Providers{})
			if len(res.Findings) != tc.findings {
				t.Fatalf("findings = %d, want %d", len(res.Findings), tc.findings)
			}
			switch {
			case tc.skip == "" && res.Skipped != nil:
				t.Fatalf("unexpected skip %q", res.Skipped.Reason)
			case tc.skip != "" && (res.Skipped == nil || !strings.Contains(res.Skipped.Reason, tc.skip)):
				t.Fatalf("skip = %+v, want substring %q", res.Skipped, tc.skip)
			}
			if tc.skip != "" && strings.Contains(tc.skip, uncertainPrefix) && strings.Contains(res.Skipped.Reason, sdk.SkipNotApplicable) {
				t.Errorf("uncertain trigger reported as not-applicable: %q", res.Skipped.Reason)
			}
		})
	}
}

func TestCFG009FindingKeepsOtherTriggerNotes(t *testing.T) {
	y := "services:\n  r:\n    host: azure.ai.routine\n    triggers:\n      fast:\n        type: recurring\n        cron_expression: \"* * * * *\"\n        time_zone: UTC\n      new:\n        type: schedule\n        cronExpression: \"0 * * * *\"\n"
	res := n3Eval(t, "FND-CFG-009", y, Providers{})
	if res.Skipped != nil || len(res.Findings) != 1 {
		t.Fatalf("want one finding and no rule-level skip, got %+v", res)
	}
	wantSkipPrefixed(t, res, "trigger new has type schedule")
}

// N3-3: a named environment that does not exist is not "the default".
func TestEnvironmentNotFoundReason(t *testing.T) {
	cases := []struct {
		name, reason, want string
	}{
		{"explicit name", "environment-not-found: nosuch", "input-unavailable: environment-not-found: nosuch"},
		{"default", "default-environment-not-found", "input-unavailable: default-environment-not-found"},
		{"hostile name is sanitised", "environment-not-found: a b\nc;d", "input-unavailable: environment-not-found: a?b?c?d"},
		{"long name is capped", "environment-not-found: " + strings.Repeat("x", 100), "input-unavailable: environment-not-found: " + strings.Repeat("x", 40) + "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, id := range []string{"FND-CFG-006", "FND-CFG-012"} {
				res := n3Eval(t, id, "services: {}\n", Providers{Env: reasonEnv{fakeEnv{false, nil}, tc.reason}})
				if res.Skipped == nil || res.Skipped.Reason != tc.want {
					t.Errorf("%s: skip = %+v, want %q", id, res.Skipped, tc.want)
				}
			}
		})
	}
}

// N3-4: azd replaces a space and a hyphen only; anything else is uncertain.
func TestAgentProducerNames(t *testing.T) {
	svc := func(name string) service { return service{Name: name, Host: hostAgent} }
	got := agentProducers(svc("my agent-1"))
	want := map[string]bool{
		"AGENT_MY_AGENT_1_NAME": true, "AGENT_MY_AGENT_1_VERSION": true, "AGENT_MY_AGENT_1_ENDPOINT": true,
		"AGENT_MY_AGENT_1_RESPONSES_ENDPOINT": true, "AGENT_MY_AGENT_1_INVOCATIONS_ENDPOINT": true,
		"AGENT_MY_AGENT_1_A2A_ENDPOINT": true, "AGENT_MY_AGENT_1_INVOCATIONS_WS_ENDPOINT": true,
	}
	if len(got) != len(want) {
		t.Fatalf("producers = %v", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected producer %s", n)
		}
	}
	for _, bad := range []string{"my.agent", "agent@1", "ägent", ""} {
		if p := agentProducers(svc(bad)); len(p) != 0 {
			t.Errorf("name %q yields producers %v", bad, p)
		}
	}
	if p := agentProducers(service{Name: "x", Host: hostProject}); len(p) != 0 {
		t.Errorf("non-agent host yields %v", p)
	}
}

func TestCFG006DottedAgentNameIsUncertain(t *testing.T) {
	env := Providers{Env: fakeEnv{true, map[string]string{"AZURE_ENV_NAME": "dev"}}}
	ref := "    env:\n      EP: ${AGENT_MY_AGENT_A2A_ENDPOINT}\n"
	good := n3Eval(t, "FND-CFG-006", "services:\n  \"my agent\":\n    host: azure.ai.agent\n"+ref, env)
	if good.Skipped != nil || len(good.Findings) != 0 || len(good.Skips) != 0 {
		t.Fatalf("space name with A2A endpoint should resolve, got %+v", good)
	}
	dotted := n3Eval(t, "FND-CFG-006", "services:\n  my.agent:\n    host: azure.ai.agent\n"+ref, env)
	if dotted.Skipped == nil || !strings.HasPrefix(dotted.Skipped.Reason, uncertainPrefix) || len(dotted.Findings) != 0 {
		t.Fatalf("dotted name must be uncertain, got %+v", dotted)
	}
}

// N3-11
func TestCFG012ReferenceIsUncertainAndAccurate(t *testing.T) {
	res := n3Eval(t, "FND-CFG-012", "services: {}\n", Providers{Env: fakeEnv{true, map[string]string{"AZURE_LOCATION": "${REGION}"}}})
	if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, uncertainPrefix) {
		t.Fatalf("want an uncertain skip, got %+v", res)
	}
	if strings.Contains(res.Skipped.Reason, "not set") || strings.Contains(res.Skipped.Reason, sdk.SkipInputUnavailable) {
		t.Errorf("message is inaccurate: %q", res.Skipped.Reason)
	}
	unset := n3Eval(t, "FND-CFG-012", "services: {}\n", Providers{Env: fakeEnv{true, map[string]string{}}})
	if unset.Skipped == nil || !strings.Contains(unset.Skipped.Reason, sdk.SkipInputUnavailable) || !strings.Contains(unset.Skipped.Reason, "not set") {
		t.Errorf("unset location: %+v", unset.Skipped)
	}
}

// N3-9 and N3-10: forms the differential test found unreliable are unverified.
func TestParseAzdRangeUnverifiedForms(t *testing.T) {
	for _, in := range []string{
		"1.2.3-box", "1.x-beta", ">=1.x", "xyz", "1.x.x", "x.x.x", "1.2.3 *", "! 1.2.3", "!", "- 1.2.3", "1.2.3 -", ">=1.0.0\t<2.0.0",
	} {
		if _, st := parseAzdRange(in); st != parseUnverified {
			t.Errorf("parseAzdRange(%q) = %v, want unverified", in, st)
		}
	}
	for _, in := range []string{"1.x", "1.2.x", "< 1.2.3", "!=1.2.3", "!1.2.3"} {
		if _, st := parseAzdRange(in); st != parseValid {
			t.Errorf("parseAzdRange(%q) = %v, want valid", in, st)
		}
	}
}

func TestParseMMConstraintUnverifiedForms(t *testing.T) {
	for _, in := range []string{"1.2.x-beta", "<x", "<=*", "^x", "~0.0.0", "1.2.3 - *"} {
		if _, _, st := parseMMConstraint(in); st != parseUnverified {
			t.Errorf("parseMMConstraint(%q) = %v, want unverified", in, st)
		}
	}
	if _, _, st := parseMMConstraint("1.2.3-01"); st != parseInvalid {
		t.Errorf("a numeric pre-release with a leading zero must be invalid, got %v", st)
	}
}

// N3-10: an invalid range is reported for a dev build too, because azd parses
// the range before it skips the check for a dev build.
func TestCFG001InvalidRangeReportedForDevBuild(t *testing.T) {
	y := "name: app\nrequiredVersions:\n  azd: \">=1.0.0, <2.0.0\"\n"
	r := find(Register(), "FND-CFG-001")
	res := run(t, r, &sdk.Input{AzureYAML: doc(t, y), AzdVersion: "0.0.0-dev.0"})
	if len(res.Findings) != 1 || res.Findings[0].Key != "required-versions-azd-invalid" {
		t.Fatalf("want the invalid-range finding for a dev build, got %+v", res)
	}
	ok := run(t, r, &sdk.Input{AzureYAML: doc(t, "name: app\nrequiredVersions:\n  azd: \">=9.0.0\"\n"), AzdVersion: "0.0.0-dev.0"})
	if len(ok.Findings) != 0 {
		t.Fatalf("a valid range that excludes a dev build must pass, got %+v", ok.Findings)
	}
}
