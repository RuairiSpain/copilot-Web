package cfg

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type reasonEnv struct {
	fakeEnv
	reason string
}

func (r reasonEnv) SelectionReason() string { return r.reason }

func evalFindings(t *testing.T, id, y string, p Providers, arm sdk.ARMModel) []sdk.Finding {
	t.Helper()
	res := run(t, find(RegisterWith(p), id), &sdk.Input{AzureYAML: doc(t, y), ARM: arm})
	if res.Skipped != nil {
		t.Fatalf("%s skipped: %s", id, res.Skipped.Reason)
	}
	return res.Findings
}

func fp(f sdk.Finding, id string) string {
	f.RuleID, f.RuleVersion = id, 1
	return findings.Fingerprint(f)
}

func byEvidence(t *testing.T, fs []sdk.Finding, sub string) sdk.Finding {
	t.Helper()
	for _, f := range fs {
		if strings.Contains(f.Evidence, sub) {
			return f
		}
	}
	t.Fatalf("no finding with %q in %+v", sub, fs)
	return sdk.Finding{}
}

// R1: a finding's fingerprint must not depend on sibling findings.
func TestFingerprintIndependentOfSiblings(t *testing.T) {
	agent := func(extra string) string {
		return "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n" + extra
	}
	t.Run("cfg004 blank instructions with and without blank model", func(t *testing.T) {
		alone := evalFindings(t, "FND-CFG-004", agent("    model: m\n"), Providers{}, nil)
		both := evalFindings(t, "FND-CFG-004", agent(""), Providers{}, nil)
		if len(alone) != 1 || len(both) != 2 {
			t.Fatalf("alone=%d both=%d", len(alone), len(both))
		}
		a, b := byEvidence(t, alone, "instructions"), byEvidence(t, both, "instructions")
		if fp(a, "FND-CFG-004") != fp(b, "FND-CFG-004") {
			t.Fatal("fingerprint changed when a sibling finding appeared")
		}
		if fp(byEvidence(t, both, "model"), "FND-CFG-004") == fp(b, "FND-CFG-004") {
			t.Fatal("two findings share a fingerprint")
		}
	})
	t.Run("cfg004 cpu fingerprint ignores the value and a memory sibling", func(t *testing.T) {
		y := func(cpu, mem string) string {
			return hosted("    container:\n      resources:\n        cpu: \"" + cpu + "\"\n        memory: " + mem + "\n")
		}
		one := evalFindings(t, "FND-CFG-004", y("8", "1Gi"), Providers{}, nil)
		two := evalFindings(t, "FND-CFG-004", y("9", "16Gi"), Providers{}, nil)
		if len(one) != 1 || len(two) != 2 {
			t.Fatalf("one=%d two=%d", len(one), len(two))
		}
		if fp(one[0], "FND-CFG-004") != fp(byEvidence(t, two, "cpu"), "FND-CFG-004") {
			t.Fatal("cpu fingerprint depends on sibling or value")
		}
	})
	t.Run("cfg001 two schema errors differ", func(t *testing.T) {
		fs := evalFindings(t, "FND-CFG-001", "name: my_app\nservices: {}\n", Providers{}, nil)
		if len(fs) != 2 || fp(fs[0], "FND-CFG-001") == fp(fs[1], "FND-CFG-001") {
			t.Fatalf("want two distinct fingerprints: %+v", fs)
		}
		alone := evalFindings(t, "FND-CFG-001", "name: my_app\n", Providers{}, nil)
		if fp(alone[0], "FND-CFG-001") != fp(byEvidence(t, fs, "name must be"), "FND-CFG-001") {
			t.Fatal("name finding fingerprint depends on the services finding")
		}
	})
	t.Run("cfg001 two services missing host differ", func(t *testing.T) {
		fs := evalFindings(t, "FND-CFG-001", "name: app\nservices:\n  a:\n    kind: x\n  b:\n    kind: x\n", Providers{}, nil)
		if len(fs) != 2 || fp(fs[0], "FND-CFG-001") == fp(fs[1], "FND-CFG-001") {
			t.Fatalf("want two distinct fingerprints: %+v", fs)
		}
	})
	t.Run("cfg006 variable name is the key", func(t *testing.T) {
		y := func(vars ...string) string {
			s := "services:\n  a:\n    host: azure.ai.agent\n    env:\n"
			for i, v := range vars {
				s += "      V" + string(rune('A'+i)) + ": ${" + v + "}\n"
			}
			return s
		}
		p := Providers{Env: fakeEnv{true, nil}}
		arm := armOutStub{[]string{"OTHER"}}
		one := evalFindings(t, "FND-CFG-006", y("NOPE_B"), p, arm)
		two := evalFindings(t, "FND-CFG-006", y("NOPE_A", "NOPE_B"), p, arm)
		if len(one) != 1 || len(two) != 2 {
			t.Fatalf("one=%d two=%d", len(one), len(two))
		}
		if fp(one[0], "FND-CFG-006") != fp(byEvidence(t, two, "NOPE_B"), "FND-CFG-006") {
			t.Fatal("fingerprint depends on sibling")
		}
		if two[0].Key == two[1].Key {
			t.Fatal("variables share a key")
		}
	})
	t.Run("cfg005 field path is the key", func(t *testing.T) {
		y := func(creds string) string {
			return "services:\n  c:\n    host: azure.ai.connection\n    authType: ApiKey\n    credentials:\n" + creds
		}
		one := evalFindings(t, "FND-CFG-005", y("      secret: lit2\n"), Providers{}, nil)
		two := evalFindings(t, "FND-CFG-005", y("      key: lit1\n      secret: lit2\n"), Providers{}, nil)
		if len(one) != 1 || len(two) != 2 {
			t.Fatalf("one=%d two=%d", len(one), len(two))
		}
		if fp(one[0], "FND-CFG-005") != fp(byEvidence(t, two, "credentials.secret"), "FND-CFG-005") {
			t.Fatal("fingerprint depends on sibling")
		}
	})
}

// Every finding of every rule carries a Key, and keys are unique per rule and resource.
func TestEveryFindingHasUniqueKey(t *testing.T) {
	corpus := []string{
		"name: my_app\nservices:\n  a:\n    kind: x\n  b:\n    host: azure.ai.agent\n    kind: prompt\n  p:\n    host: azure.ai.project\n    project: x\n    runtime: y\n",
		"services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n    uses: [p]\n    model: m\n    toolboxes: [t1, t2]\n    connections: [c1]\n    env:\n      FOUNDRY_X: a\n      AGENT_Y: b\n      X: ${NOPE1}\n      Y: ${NOPE2}\n    tools:\n      - type: mcp\n        allowed_tools: []\n      - type: mcp\n        allowed_tools: []\n  p:\n    host: azure.ai.project\n    deployments:\n      - name: d1\n        model: {name: gpt, version: latest}\n      - name: d2\n        model: {name: gpt, version: latest}\n  c:\n    host: azure.ai.connection\n    authType: ApiKey\n    target: http://localhost\n    credentials: {key: a, secret: b}\n",
		hosted("    container:\n      resources:\n        cpu: \"9\"\n        memory: 99Gi\n    codeConfiguration: {}\n"),
	}
	p := Providers{Env: fakeEnv{true, nil}, Layout: fakeLayout{}}
	arm := armOutStub{[]string{"OTHER"}}
	total := 0
	for i, y := range corpus {
		for _, r := range RegisterWith(p) {
			res := run(t, r, &sdk.Input{AzureYAML: doc(t, y), ARM: arm})
			seen := map[string]bool{}
			for _, f := range res.Findings {
				total++
				if f.Key == "" {
					t.Errorf("corpus %d %s: finding without Key: %+v", i, r.ID(), f)
				}
				id := f.Resource.Name + "\x00" + f.Key
				if seen[id] {
					t.Errorf("corpus %d %s: duplicate key %q on %q", i, r.ID(), f.Key, f.Resource.Name)
				}
				seen[id] = true
			}
		}
	}
	if total < 10 {
		t.Fatalf("corpus produced only %d findings", total)
	}
}

// R2: exclusions match whole tokens.
func TestCFG005NameTokens(t *testing.T) {
	env := func(k string) string {
		return "services:\n  a:\n    host: azure.ai.agent\n    env:\n      " + k + ": plainvalue\n"
	}
	runReviewCases(t, []reviewCase{
		{name: "STORAGE_ACCOUNT_KEY literal is reported", id: "FND-CFG-005", yaml: env("STORAGE_ACCOUNT_KEY"), skip: uncertainPrefix + "a: env.STORAGE_ACCOUNT_KEY"},
		{name: "PROFILE_PASSWORD literal is reported", id: "FND-CFG-005", yaml: env("PROFILE_PASSWORD"), skip: uncertainPrefix + "a: env.PROFILE_PASSWORD"},
		{name: "TOKEN_LIMIT is a quantity", id: "FND-CFG-005", yaml: env("TOKEN_LIMIT")},
		{name: "MAX_TOKENS is a quantity", id: "FND-CFG-005", yaml: env("MAX_TOKENS")},
		{name: "KEY_COUNT is a quantity", id: "FND-CFG-005", yaml: env("KEY_COUNT")},
		{name: "SECRET_SIZE is a quantity", id: "FND-CFG-005", yaml: env("SECRET_SIZE")},
		{name: "API_KEY_FILE is a path", id: "FND-CFG-005", yaml: env("API_KEY_FILE")},
		{name: "KEY_ROTATION_DAYS is a setting", id: "FND-CFG-005", yaml: env("KEY_ROTATION_DAYS")},
		{name: "DESIGN_NAME is not secret", id: "FND-CFG-005", yaml: env("DESIGN_NAME")},
		{name: "API_KEY literal is reported", id: "FND-CFG-005", yaml: env("API_KEY"), skip: uncertainPrefix + "a: env.API_KEY"},
		{name: "api-key reference passes", id: "FND-CFG-005", yaml: "services:\n  a:\n    host: azure.ai.agent\n    env:\n      API_KEY: ${K}\n"},
	})
}

// R4: AGENT_<SVC>_* producers, agent-host services only.
func TestCFG006AgentProducers(t *testing.T) {
	p := Providers{Env: fakeEnv{true, nil}}
	arm := armOutStub{[]string{"OTHER"}}
	svc := func(host, ref string) string {
		return "services:\n  my-svc:\n    host: " + host + "\n    env:\n      X: " + ref + "\n"
	}
	runReviewCases(t, []reviewCase{
		{name: "responses endpoint", id: "FND-CFG-006", yaml: svc("azure.ai.agent", "${AGENT_MY_SVC_RESPONSES_ENDPOINT}"), p: p, arm: arm},
		{name: "invocations endpoint", id: "FND-CFG-006", yaml: svc("azure.ai.agent", "${AGENT_MY_SVC_INVOCATIONS_ENDPOINT}"), p: p, arm: arm},
		{name: "invocations ws endpoint", id: "FND-CFG-006", yaml: svc("azure.ai.agent", "${AGENT_MY_SVC_INVOCATIONS_WS_ENDPOINT}"), p: p, arm: arm},
		{name: "name and version", id: "FND-CFG-006", yaml: svc("azure.ai.agent", "${AGENT_MY_SVC_NAME}-${AGENT_MY_SVC_VERSION}"), p: p, arm: arm},
		{name: "other protocol is uncertain not an error", id: "FND-CFG-006", yaml: svc("azure.ai.agent", "${AGENT_MY_SVC_MCP_ENDPOINT}"), p: p, arm: arm, skip: uncertainPrefix + "${AGENT_MY_SVC_MCP_ENDPOINT}"},
		{name: "other service name is uncertain", id: "FND-CFG-006", yaml: svc("azure.ai.agent", "${AGENT_OTHER_ENDPOINT}"), p: p, arm: arm, skip: uncertainPrefix},
		{name: "non-agent service gets no producers", id: "FND-CFG-006", yaml: svc("azure.ai.project", "${AGENT_MY_SVC_RESPONSES_ENDPOINT}"), p: p, arm: arm, skip: uncertainPrefix + "${AGENT_MY_SVC_RESPONSES_ENDPOINT}"},
		{name: "non-agent service endpoint", id: "FND-CFG-006", yaml: svc("azure.ai.connection", "${AGENT_MY_SVC_ENDPOINT}"), p: p, arm: arm, skip: uncertainPrefix},
	})
}

// R6 and R13: named skip reasons.
func TestSkipReasonsAreNamed(t *testing.T) {
	y := "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${V}\n"
	cases := []struct {
		name, id string
		p        Providers
		want     string
	}{
		{"006 nil env view", "FND-CFG-006", Providers{}, "input-unavailable: no-environment-selected"},
		{"006 not selected", "FND-CFG-006", Providers{Env: fakeEnv{false, nil}}, "input-unavailable: no-environment-selected"},
		{"006 env file missing", "FND-CFG-006", Providers{Env: reasonEnv{fakeEnv{false, nil}, "env-file-missing"}}, "input-unavailable: env-file-missing"},
		{"006 default not found", "FND-CFG-006", Providers{Env: reasonEnv{fakeEnv{false, nil}, "default-environment-not-found"}}, "input-unavailable: default-environment-not-found"},
		{"006 unknown reason falls back", "FND-CFG-006", Providers{Env: reasonEnv{fakeEnv{false, nil}, "whatever"}}, "input-unavailable: no-environment-selected"},
		{"012 nil env view", "FND-CFG-012", Providers{}, "input-unavailable: no-environment-selected"},
		{"012 env file missing", "FND-CFG-012", Providers{Env: reasonEnv{fakeEnv{false, nil}, "env-file-missing"}}, "input-unavailable: env-file-missing"},
		{"012 not set", "FND-CFG-012", Providers{Env: fakeEnv{true, nil}}, "input-unavailable: AZURE_LOCATION"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := run(t, find(RegisterWith(tc.p), tc.id), &sdk.Input{AzureYAML: doc(t, y)})
			if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, tc.want) {
				t.Fatalf("got %+v want prefix %q", res.Skipped, tc.want)
			}
		})
	}
	na := []struct{ name, id, y string }{
		{"008 no mcp tools", "FND-CFG-008", "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n"},
		{"009 no routines", "FND-CFG-009", "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n"},
		{"010 no deployments", "FND-CFG-010", "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n"},
	}
	for _, tc := range na {
		t.Run(tc.name, func(t *testing.T) {
			in := &sdk.Input{AzureYAML: doc(t, tc.y)}
			if tc.id == "FND-CFG-010" {
				in.ARM = emptyARM{} // the ARM model is available and declares nothing
			}
			res := run(t, find(Register(), tc.id), in)
			if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipNotApplicable+":") {
				t.Fatalf("got %+v want not-applicable", res.Skipped)
			}
		})
	}
	// R6: azure.yaml declares no deployments but the Bicep, which may declare
	// them, is unavailable: that is a hidden check, not "not applicable".
	t.Run("010 without the ARM model is input-unavailable", func(t *testing.T) {
		y := "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n"
		res := run(t, find(Register(), "FND-CFG-010"), &sdk.Input{AzureYAML: doc(t, y)})
		if res.Skipped == nil || res.Skipped.Reason != sdk.SkipInputUnavailable {
			t.Fatalf("got %+v", res.Skipped)
		}
	})
	t.Run("010 without any input stays input-unavailable", func(t *testing.T) {
		res := run(t, find(Register(), "FND-CFG-010"), &sdk.Input{})
		if res.Skipped == nil || res.Skipped.Reason != sdk.SkipInputUnavailable {
			t.Fatalf("got %+v", res.Skipped)
		}
	})
}

// R7: a display-name location is never an error.
func TestCFG012Locations(t *testing.T) {
	loc := func(v string) Providers { return Providers{Env: fakeEnv{true, map[string]string{"AZURE_LOCATION": v}}} }
	runReviewCases(t, []reviewCase{
		{name: "display name is uncertain", id: "FND-CFG-012", yaml: "services: {}\n", p: loc("East US"), skip: uncertainPrefix + "AZURE_LOCATION"},
		{name: "mixed case is uncertain", id: "FND-CFG-012", yaml: "services: {}\n", p: loc("EastUS"), skip: uncertainPrefix},
		{name: "lowercase passes", id: "FND-CFG-012", yaml: "services: {}\n", p: loc("eastus2")},
		{name: "empty is a finding", id: "FND-CFG-012", yaml: "services: {}\n", p: loc(" "), want: 1, contains: "empty"},
		{name: "reference is skipped", id: "FND-CFG-012", yaml: "services: {}\n", p: loc("${L}"), skip: sdk.SkipInputUnavailable},
	})
}

// R8: only Ki/Mi/Gi/Ti memory and plain-decimal cpu are range-checked.
func TestCFG004QuantityForms(t *testing.T) {
	res := func(cpu, mem string) string {
		out := "    container:\n      resources:\n"
		if cpu != "" {
			out += "        cpu: \"" + cpu + "\"\n"
		}
		if mem != "" {
			out += "        memory: " + mem + "\n"
		}
		return hosted(out)
	}
	runReviewCases(t, []reviewCase{
		{name: "512M is uncertain", id: "FND-CFG-004", yaml: res("", "512M"), skip: uncertainPrefix + "a: container.resources.memory \"512M\""},
		{name: "2G is uncertain", id: "FND-CFG-004", yaml: res("", "2G"), skip: uncertainPrefix},
		{name: "20G is uncertain not out of range", id: "FND-CFG-004", yaml: res("", "20G"), skip: uncertainPrefix},
		{name: "bare 1 is uncertain", id: "FND-CFG-004", yaml: res("", "1"), skip: uncertainPrefix},
		{name: "bare 4294967296 is uncertain", id: "FND-CFG-004", yaml: res("", "4294967296"), skip: uncertainPrefix},
		{name: "1Ti is checked and out of range", id: "FND-CFG-004", yaml: res("", "1Ti"), want: 1, contains: "memory"},
		{name: "1048576Ki is checked and in range", id: "FND-CFG-004", yaml: res("", "1048576Ki")},
		{name: "1Pi is uncertain", id: "FND-CFG-004", yaml: res("", "1Pi"), skip: uncertainPrefix},
		{name: "decimal cpu out of range still fails", id: "FND-CFG-004", yaml: res("9", ""), want: 1, contains: "cpu"},
		{name: "finding wins over uncertain memory", id: "FND-CFG-004", yaml: res("9", "512M"), want: 1, contains: "cpu"},
		{name: "memory pattern violation is a finding", id: "FND-CFG-004", yaml: res("", "2GB"), want: 1, contains: "pattern"},
	})
}

// R16: an empty .env value is not a producer.
func TestCFG006EmptyValue(t *testing.T) {
	y := "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${MYVAR}\n"
	arm := armOutStub{[]string{"OTHER"}}
	runReviewCases(t, []reviewCase{
		{name: "empty value is uncertain", id: "FND-CFG-006", yaml: y, p: Providers{Env: fakeEnv{true, map[string]string{"MYVAR": ""}}}, arm: arm, skip: uncertainPrefix + "${MYVAR} is present in the environment with an empty value"},
		{name: "blank value is uncertain", id: "FND-CFG-006", yaml: y, p: Providers{Env: fakeEnv{true, map[string]string{"MYVAR": "  "}}}, arm: arm, skip: uncertainPrefix},
		{name: "non-empty value is a producer", id: "FND-CFG-006", yaml: y, p: Providers{Env: fakeEnv{true, map[string]string{"MYVAR": "v"}}}, arm: arm},
	})
}

// R9: version notes.
func TestVersionNotes(t *testing.T) {
	notes := func(c string) []versionNote {
		d := doc(t, "name: app\nrequiredVersions:\n  extensions:\n    azure.ai.projects: \""+c+"\"\n")
		return requiredVersionNotes(d.(document).Resolved(), cfg001MinVersions)
	}
	tests := []struct {
		in   string
		want int
		info bool
	}{
		{">=1.0.0-beta.13", 0, false},
		{">=1.0.0-beta.13, <2.0.0", 0, false},
		{">=1.0.0-beta.1", 1, true},
		{">=1.0.0-beta.14", 0, false},
		{">=1.0.0-beta.15", 1, false},
		{">1.0.0-beta.13", 0, false},
		{">1.0.0-beta.14", 1, false},
		{"=1.0.0", 1, false},
		{"^1.0.0", 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			ns := notes(tc.in)
			if len(ns) != tc.want || (tc.want == 1 && ns[0].Info != tc.info) {
				t.Fatalf("notes=%+v", ns)
			}
		})
	}
}

func TestIsSecretKeyTokenOrder(t *testing.T) {
	for name, want := range map[string]bool{
		"API_KEY": true, "FILE_SECRET": true, "STORAGE_ACCOUNT_KEY": true, "PROFILE_PASSWORD": true,
		"API_KEY_FILE": false, "KEY_ROTATION_DAYS": false, "TOKEN_LIMIT": false, "MAX_TOKENS": false, "SECRET_COUNT": false,
	} {
		if got := isSecretKey(name); got != want {
			t.Errorf("isSecretKey(%q) = %v, want %v", name, got, want)
		}
	}
}

type emptyARM struct{}

func (emptyARM) Resources() []sdk.ARMResource { return nil }
