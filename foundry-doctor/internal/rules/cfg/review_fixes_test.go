package cfg

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type reviewCase struct {
	name      string
	id        string
	yaml      string
	p         Providers
	arm       sdk.ARMModel
	want      int    // findings expected
	skip      string // "" none; otherwise the skip reason must contain this text
	contains  string // evidence of the first finding must contain this text
	notInSkip string // skip reason must not contain this text
}

func runReviewCases(t *testing.T, cases []reviewCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := find(RegisterWith(tc.p), tc.id)
			if r == nil {
				t.Fatalf("rule %s missing", tc.id)
			}
			res := run(t, r, &sdk.Input{AzureYAML: doc(t, tc.yaml), ARM: tc.arm})
			if tc.skip != "" {
				if res.Skipped == nil {
					t.Fatalf("want skip containing %q, got findings %+v", tc.skip, res.Findings)
				}
				if !strings.Contains(res.Skipped.Reason, tc.skip) {
					t.Fatalf("skip reason %q lacks %q", res.Skipped.Reason, tc.skip)
				}
				if tc.notInSkip != "" && strings.Contains(res.Skipped.Reason, tc.notInSkip) {
					t.Fatalf("skip reason %q must not contain %q", res.Skipped.Reason, tc.notInSkip)
				}
				if len(res.Findings) != 0 {
					t.Fatalf("skipped result carries findings %+v", res.Findings)
				}
				return
			}
			if res.Skipped != nil {
				t.Fatalf("unexpected skip %q", res.Skipped.Reason)
			}
			if len(res.Findings) != tc.want {
				t.Fatalf("findings=%d want %d: %+v", len(res.Findings), tc.want, res.Findings)
			}
			if tc.contains != "" && !strings.Contains(res.Findings[0].Evidence, tc.contains) {
				t.Fatalf("evidence %q lacks %q", res.Findings[0].Evidence, tc.contains)
			}
		})
	}
}

const hostedHead = "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    project: src\n    image: img\n"

func hosted(extra string) string { return hostedHead + extra }

func TestCFG004ContainerResources(t *testing.T) {
	res := func(cpu, mem string) string {
		out := "    container:\n      resources:\n"
		if cpu != "" {
			out += "        cpu: \"" + cpu + "\"\n"
		}
		if mem != "" {
			out += "        memory: " + mem + "\n"
		}
		return out
	}
	runReviewCases(t, []reviewCase{
		{name: "cpu 64 fails", id: "FND-CFG-004", yaml: hosted(res("64", "")), want: 1, contains: "container.resources.cpu"},
		{name: "cpu above 4 fails", id: "FND-CFG-004", yaml: hosted(res("4.5", "")), want: 1, contains: "cpu"},
		{name: "cpu below 0.25 fails", id: "FND-CFG-004", yaml: hosted(res("0.1", "")), want: 1, contains: "cpu"},
		{name: "cpu 500m passes", id: "FND-CFG-004", yaml: hosted(res("500m", ""))},
		{name: "cpu 100m fails", id: "FND-CFG-004", yaml: hosted(res("100m", "")), want: 1, contains: "cpu"},
		{name: "cpu 4 passes", id: "FND-CFG-004", yaml: hosted(res("4", ""))},
		{name: "cpu not a quantity fails", id: "FND-CFG-004", yaml: hosted(res("lots", "")), want: 1, contains: "cpu"},
		{name: "memory 512Mi passes", id: "FND-CFG-004", yaml: hosted(res("", "512Mi"))},
		{name: "memory 1Gi passes", id: "FND-CFG-004", yaml: hosted(res("", "1Gi"))},
		{name: "memory 1024Mi passes", id: "FND-CFG-004", yaml: hosted(res("", "1024Mi"))},
		{name: "memory 524288Ki passes", id: "FND-CFG-004", yaml: hosted(res("", "524288Ki"))},
		{name: "memory 8Gi passes", id: "FND-CFG-004", yaml: hosted(res("", "8Gi"))},
		{name: "memory 16Gi fails", id: "FND-CFG-004", yaml: hosted(res("", "16Gi")), want: 1, contains: "memory"},
		{name: "memory 100Mi fails", id: "FND-CFG-004", yaml: hosted(res("", "100Mi")), want: 1, contains: "memory"},
		{name: "memory 9000Mi fails", id: "FND-CFG-004", yaml: hosted(res("", "9000Mi")), want: 1, contains: "memory"},
		{name: "memory bad unit fails", id: "FND-CFG-004", yaml: hosted(res("", "2GB")), want: 1, contains: "memory"},
		{name: "reference is not evaluated", id: "FND-CFG-004", yaml: hosted("    container:\n      resources:\n        cpu: ${CPU}\n        memory: ${MEM}\n")},
		{name: "absent resources pass", id: "FND-CFG-004", yaml: hosted("")},
		{name: "service level cpu and memory are not schema fields and are ignored", id: "FND-CFG-004", yaml: hosted("    cpu: \"64\"\n    memory: 512Mi\n")},
	})
}

func TestCFG004HostedSource(t *testing.T) {
	layoutNoDockerfile := Providers{Layout: fakeLayout{"src/": true}}
	layoutDockerfile := Providers{Layout: fakeLayout{"src/": true, "src/Dockerfile": true}}
	noImage := "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    project: src\n"
	runReviewCases(t, []reviewCase{
		{name: "runtime and startupCommand are accepted", id: "FND-CFG-004", p: layoutNoDockerfile,
			yaml: noImage + "    runtime: python\n    startupCommand: python main.py\n"},
		{name: "startupCommand alone is accepted", id: "FND-CFG-004", p: layoutNoDockerfile, yaml: noImage + "    startupCommand: python main.py\n"},
		{name: "skill is accepted", id: "FND-CFG-004", p: layoutNoDockerfile, yaml: noImage + "    skill: s\n"},
		{name: "no source at all is uncertain, not an error", id: "FND-CFG-004", p: layoutNoDockerfile, yaml: noImage,
			skip: uncertainPrefix + "a: no Dockerfile"},
		{name: "dockerfile present passes", id: "FND-CFG-004", p: layoutDockerfile, yaml: noImage},
		{name: "image set passes without dockerfile", id: "FND-CFG-004", p: layoutNoDockerfile, yaml: noImage + "    image: i\n"},
		{name: "missing layout skips the source check", id: "FND-CFG-004", yaml: noImage},
		{name: "project directory missing is a finding", id: "FND-CFG-004", p: Providers{Layout: fakeLayout{}}, yaml: noImage, want: 1, contains: "does not exist"},
	})
}

func TestCFG004PromptModelDeployments(t *testing.T) {
	proj := func(deployments string) string {
		return "name: app\nservices:\n  p:\n    host: azure.ai.project\n" + deployments +
			"  a:\n    host: azure.ai.agent\n    kind: prompt\n    project: src\n    uses: [p]\n    model: m1\n    instructions: hi\n"
	}
	runReviewCases(t, []reviewCase{
		{name: "deployments absent is unknown and skipped", id: "FND-CFG-004", yaml: proj(""),
			skip: uncertainPrefix + "a: model \"m1\" cannot be matched"},
		{name: "deployments is not a list is unknown", id: "FND-CFG-004", yaml: proj("    deployments: ${D}\n"), skip: uncertainPrefix},
		{name: "deployment name unresolved is unknown", id: "FND-CFG-004", yaml: proj("    deployments:\n      - name: ${N}\n"), skip: uncertainPrefix},
		{name: "deployment from $ref is unknown", id: "FND-CFG-004", yaml: proj("    deployments:\n      - $ref: d.yaml\n"), skip: uncertainPrefix},
		{name: "matching deployment passes", id: "FND-CFG-004", yaml: proj("    deployments:\n      - name: m1\n")},
		{name: "other deployment is a finding", id: "FND-CFG-004", yaml: proj("    deployments:\n      - name: other\n"), want: 1, contains: "not a deployment"},
		{name: "explicit empty deployments is a finding", id: "FND-CFG-004", yaml: proj("    deployments: []\n"), want: 1, contains: "not a deployment"},
		{name: "existing project endpoint is unknown", id: "FND-CFG-004", yaml: proj("    endpoint: https://x.example\n    deployments:\n      - name: other\n"), skip: uncertainPrefix},
		{name: "findings win over uncertainty", id: "FND-CFG-004", yaml: proj("") + "  b:\n    host: azure.ai.agent\n    kind: prompt\n    project: src\n    uses: [p]\n    model: m1\n", want: 1, contains: "blank instructions"},
	})
}

func TestCFG003MemoryWithoutDeployments(t *testing.T) {
	y := "name: app\nservices:\n  p:\n    host: azure.ai.project\n%s  m:\n    host: azure.ai.memory-store\n    project: src\n    uses: [p]\n    chatModel: c1\n"
	runReviewCases(t, []reviewCase{
		{name: "deployments absent skips model check", id: "FND-CFG-003", yaml: strings.Replace(y, "%s", "", 1), skip: uncertainPrefix},
		{name: "matching deployment passes", id: "FND-CFG-003", yaml: strings.Replace(y, "%s", "    deployments:\n      - name: c1\n", 1)},
		{name: "mismatching deployment fails", id: "FND-CFG-003", yaml: strings.Replace(y, "%s", "    deployments:\n      - name: z\n", 1), want: 1, contains: "chatModel"},
	})
}

func TestCFG001Shape(t *testing.T) {
	runReviewCases(t, []reviewCase{
		{name: "no services is valid", id: "FND-CFG-001", yaml: "name: app\n"},
		{name: "missing name", id: "FND-CFG-001", yaml: "services:\n  a:\n    host: containerapp\n", want: 1, contains: "name is required"},
		{name: "uppercase name", id: "FND-CFG-001", yaml: "name: App\n", want: 1, contains: "name must be"},
		{name: "single character name", id: "FND-CFG-001", yaml: "name: a\n", want: 1, contains: "name must be"},
		{name: "trailing hyphen name", id: "FND-CFG-001", yaml: "name: app-\n", want: 1, contains: "name must be"},
		{name: "underscore name", id: "FND-CFG-001", yaml: "name: my_app\n", want: 1, contains: "name must be"},
		{name: "valid hyphen name", id: "FND-CFG-001", yaml: "name: my-app-2\n"},
		{name: "name is a mapping", id: "FND-CFG-001", yaml: "name:\n  x: y\n", want: 1, contains: "name must be a string"},
		{name: "empty services mapping", id: "FND-CFG-001", yaml: "name: app\nservices: {}\n", want: 1, contains: "at least one service"},
		{name: "service without host", id: "FND-CFG-001", yaml: "name: app\nservices:\n  a:\n    kind: prompt\n", want: 1, contains: "host"},
	})
}

func TestCFG001RequiredVersionConstraints(t *testing.T) {
	ver := func(c string) string {
		return "name: app\nrequiredVersions:\n  extensions:\n    azure.ai.agents: \"" + c + "\"\n"
	}
	runReviewCases(t, []reviewCase{
		{name: "latest", id: "FND-CFG-001", yaml: ver("latest")},
		{name: "floor equals tested", id: "FND-CFG-001", yaml: ver(">=1.0.0-beta.18")},
		{name: "floor above tested", id: "FND-CFG-001", yaml: ver(">=1.0.0")},
		{name: "exact tested", id: "FND-CFG-001", yaml: ver("=1.0.0-beta.18")},
		{name: "bare tested", id: "FND-CFG-001", yaml: ver("1.0.0-beta.18")},
		{name: "spaced range containing tested", id: "FND-CFG-001", yaml: ver(">= 1.0.0-beta.18 < 2.0.0")},
		{name: "tilde above tested", id: "FND-CFG-001", yaml: ver("~1.0.0")},
		{name: "floor below tested range is uncertain, not an error", id: "FND-CFG-001", yaml: ver(">=1.0.0-beta.10"),
			skip: uncertainPrefix + "requiredVersions.extensions.azure.ai.agents allows versions below"},
		{name: "ceiling below tested range", id: "FND-CFG-001", yaml: ver("<1.0.0-beta.12"),
			skip: uncertainPrefix + "requiredVersions.extensions.azure.ai.agents excludes the tested range"},
		{name: "ceiling exclusive at tested", id: "FND-CFG-001", yaml: ver(">=1.0.0-beta.1 <1.0.0-beta.18"),
			skip: "excludes the tested range"},
		{name: "unparseable constraint", id: "FND-CFG-001", yaml: ver("^1.0.0 || 2"), skip: "cannot be parsed"},
		{name: "unknown extension ignored", id: "FND-CFG-001", yaml: "name: app\nrequiredVersions:\n  extensions:\n    other.ext: \">=0.0.1\"\n"},
		{name: "findings win over version notes", id: "FND-CFG-001", yaml: "requiredVersions:\n  extensions:\n    azure.ai.agents: \">=1.0.0-beta.10\"\n", want: 1, contains: "name is required"},
	})
}

func TestVersionConstraintParsing(t *testing.T) {
	tests := []struct {
		in      string
		ok, any bool
		loSet   bool
		hiSet   bool
	}{
		{"", true, true, false, false},
		{"latest", true, true, false, false},
		{">=1.0.0", true, false, true, false},
		{"~2.0.0", true, false, true, true},
		{"=3.1.2", true, false, true, true},
		{"3.1.2", true, false, true, true},
		{">= 1.0.0 < 2.0.0", true, false, true, true},
		{"<1.0.0-beta.12", true, false, false, true},
		{">=1.0.0-beta.10", true, false, true, false},
		{"^1.0.0", false, false, false, false},
		{">=1.0", false, false, false, false},
		{">=1.0.0 ||", false, false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			r, anyV, ok := parseConstraint(tc.in)
			if ok != tc.ok || (ok && anyV != tc.any) {
				t.Fatalf("ok=%v any=%v", ok, anyV)
			}
			if ok && (r.lo.set != tc.loSet || r.hi.set != tc.hiSet) {
				t.Fatalf("lo=%v hi=%v", r.lo.set, r.hi.set)
			}
		})
	}
	a, _ := parseSemver("1.0.0-beta.9")
	b, _ := parseSemver("1.0.0-beta.10")
	c, _ := parseSemver("1.0.0")
	if a.cmp(b) >= 0 || b.cmp(c) >= 0 || c.cmp(c) != 0 {
		t.Fatal("version ordering wrong")
	}
}

func TestCFG011VersionNotesAreNotErrors(t *testing.T) {
	y := "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    kind: prompt\nrequiredVersions:\n  extensions:\n    azure.ai.agents: \">=1.0.0-beta.2\"\n"
	runReviewCases(t, []reviewCase{
		{name: "floor below documented minimum is uncertain", id: "FND-CFG-011", yaml: y, skip: uncertainPrefix},
		{name: "no constraint is clean", id: "FND-CFG-011", yaml: "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n"},
	})
}

func TestCFG006(t *testing.T) {
	env := func(vals map[string]string) Providers { return Providers{Env: fakeEnv{true, vals}} }
	y := func(ref string) string {
		return "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: " + ref + "\n"
	}
	withOutputs := armOutStub{[]string{"SOMETHING_ELSE"}}
	runReviewCases(t, []reviewCase{
		{name: "tenant id is azd defined", id: "FND-CFG-006", yaml: y("${AZURE_TENANT_ID}"), p: env(nil), arm: withOutputs},
		{name: "principal id is azd defined", id: "FND-CFG-006", yaml: y("${AZURE_PRINCIPAL_ID}"), p: env(nil), arm: withOutputs},
		{name: "principal type is azd defined", id: "FND-CFG-006", yaml: y("${AZURE_PRINCIPAL_TYPE}"), p: env(nil), arm: withOutputs},
		{name: "registry endpoint is infra dependent and uncertain", id: "FND-CFG-006", yaml: y("${AZURE_CONTAINER_REGISTRY_ENDPOINT}"), p: env(nil), arm: withOutputs,
			skip: uncertainPrefix + "${AZURE_CONTAINER_REGISTRY_ENDPOINT}"},
		{name: "registry endpoint in env passes", id: "FND-CFG-006", yaml: y("${AZURE_CONTAINER_REGISTRY_ENDPOINT}"), p: env(map[string]string{"AZURE_CONTAINER_REGISTRY_ENDPOINT": "x"}), arm: withOutputs},
		{name: "undefined with outputs is an error", id: "FND-CFG-006", yaml: y("${NOPE}"), p: env(nil), arm: withOutputs, want: 1, contains: "NOPE"},
		{name: "undefined without arm is uncertain", id: "FND-CFG-006", yaml: y("${NOPE}"), p: env(nil), skip: uncertainPrefix + "${NOPE}"},
		{name: "no environment skips", id: "FND-CFG-006", yaml: y("${NOPE}"), arm: withOutputs, skip: sdk.SkipInputUnavailable},
		{name: "inline default is not a reference", id: "FND-CFG-006", yaml: y("${NOPE:-x}"), p: env(nil), arm: withOutputs},
		{name: "server side expression is not a reference", id: "FND-CFG-006", yaml: y("${{ inputs.x }}"), p: env(nil), arm: withOutputs},
		{name: "agent service value is a producer", id: "FND-CFG-006", yaml: y("${AGENT_A_ENDPOINT}"), p: env(nil), arm: withOutputs},
		{name: "reserved prefix keys in env are not findings", id: "FND-CFG-006", yaml: y("v"),
			p: env(map[string]string{"AGENT_A_NAME": "n", "AGENT_A_VERSION": "1", "FOUNDRY_X": "y"}), arm: withOutputs},
		{name: "finding wins over uncertainty", id: "FND-CFG-006", yaml: "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${NOPE}\n      Y: ${AZURE_CONTAINER_REGISTRY_ENDPOINT}\n",
			p: env(nil), arm: withOutputs, want: 1, contains: "NOPE"},
	})
}

func TestCFG005Credentials(t *testing.T) {
	cred := func(v string) string {
		return "name: app\nservices:\n  c:\n    host: azure.ai.connection\n    authType: ApiKey\n    credentials:\n      key: " + v + "\n"
	}
	fakeKey := "sk-" + strings.Repeat("a", 24) // fake, built at run time
	runReviewCases(t, []reviewCase{
		{name: "whole reference passes", id: "FND-CFG-005", yaml: cred("${KEY}")},
		{name: "reference with empty default passes", id: "FND-CFG-005", yaml: cred("${KEY:-}")},
		{name: "reference with reference default passes", id: "FND-CFG-005", yaml: cred("${KEY:-${OTHER}}")},
		{name: "literal default fails", id: "FND-CFG-005", yaml: cred("${KEY:-" + fakeKey + "}"), want: 1, contains: "literal default"},
		{name: "literal fails", id: "FND-CFG-005", yaml: cred(fakeKey), want: 1, contains: "credentials.key"},
		{name: "mixed literal and reference is uncertain", id: "FND-CFG-005", yaml: cred("prefix-${KEY}"), skip: uncertainPrefix + "c: credentials.key mixes"},
		{name: "two references pass", id: "FND-CFG-005", yaml: cred("${A}${B}")},
		{name: "empty value passes", id: "FND-CFG-005", yaml: cred("\"\"")},
	})
}

func TestCFG005EnvNames(t *testing.T) {
	env := func(k, v string) string {
		return "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    env:\n      " + k + ": " + v + "\n"
	}
	fakeKey := "sk-" + strings.Repeat("b", 24)
	runReviewCases(t, []reviewCase{
		{name: "token limit is not secret", id: "FND-CFG-005", yaml: env("TOKEN_LIMIT", "4096")},
		{name: "max tokens count is not secret", id: "FND-CFG-005", yaml: env("TOKEN_COUNT", "10")},
		{name: "key rotation is not secret", id: "FND-CFG-005", yaml: env("MODEL_KEY_ROTATION", "30d")},
		{name: "key file is not secret", id: "FND-CFG-005", yaml: env("KEYWORD_FILE", "words.txt")},
		{name: "key size is not secret", id: "FND-CFG-005", yaml: env("KEY_SIZE", "256")},
		{name: "name only hit is uncertain", id: "FND-CFG-005", yaml: env("SERVICE_API_KEY", "plain"), skip: uncertainPrefix + "a: env.SERVICE_API_KEY looks secret-bearing by name only"},
		{name: "secret name with reference passes", id: "FND-CFG-005", yaml: env("SERVICE_API_KEY", "${SERVICE_API_KEY}")},
		{name: "secret shape fails under any name", id: "FND-CFG-005", yaml: env("PLAIN_NAME", fakeKey), want: 1, contains: "shape of a credential"},
		{name: "secret shape in default fails", id: "FND-CFG-005", yaml: env("PLAIN_NAME", "${X:-"+fakeKey+"}"), want: 1, contains: "shape of a credential"},
		{name: "benign value with benign name passes", id: "FND-CFG-005", yaml: env("LOG_LEVEL", "info")},
	})
}

func TestCFG005URLQuery(t *testing.T) {
	u := func(q string) string {
		return "name: app\nservices:\n  c:\n    host: azure.ai.connection\n    target: https://example.com/x?" + q + "\n"
	}
	runReviewCases(t, []reviewCase{
		{name: "sv alone is not a credential", id: "FND-CFG-005", yaml: u("sv=2022-11-02")},
		{name: "sv with sig fails", id: "FND-CFG-005", yaml: u("sv=2022-11-02&sig=abc"), want: 1, contains: "embeds credentials"},
		{name: "sig alone fails", id: "FND-CFG-005", yaml: u("sig=abc"), want: 1, contains: "embeds credentials"},
		{name: "token fails", id: "FND-CFG-005", yaml: u("token=abc"), want: 1, contains: "embeds credentials"},
		{name: "plain query passes", id: "FND-CFG-005", yaml: u("page=2")},
	})
}

func TestCFG011ReservedPrefixStaysInYAMLEnv(t *testing.T) {
	runReviewCases(t, []reviewCase{
		{name: "azure.yaml env key with reserved prefix", id: "FND-CFG-011", yaml: "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n    env:\n      FOUNDRY_X: v\n", want: 1, contains: "reserved prefix"},
	})
}

func TestMergeKeysAndAliasesReachRules(t *testing.T) {
	base := "x-base: &base\n  host: azure.ai.agent\n  kind: prompt\n  project: src\n"
	runReviewCases(t, []reviewCase{
		{name: "cfg004 sees merged host and kind", id: "FND-CFG-004",
			yaml: base + "services:\n  a:\n    <<: *base\n    instructions: hi\n", want: 1, contains: "blank model"},
		{name: "cfg004 merged complete prompt passes", id: "FND-CFG-004",
			yaml: base + "services:\n  a:\n    <<: *base\n    instructions: hi\n    model: m\n"},
		{name: "cfg004 explicit key overrides merged", id: "FND-CFG-004",
			yaml: "x-base: &base\n  host: azure.ai.agent\n  kind: prompt\n  model: old\n  instructions: hi\nservices:\n  a:\n    <<: *base\n    model: \"\"\n", want: 1, contains: "blank model"},
		{name: "cfg004 aliased container resources", id: "FND-CFG-004",
			yaml: "r: &r\n  cpu: \"64\"\nservices:\n  a:\n    host: azure.ai.agent\n    project: src\n    image: i\n    container:\n      resources: *r\n", want: 1, contains: "cpu"},
		{name: "cfg005 aliased literal credential", id: "FND-CFG-005",
			yaml: "k: &k\n  key: literal-value\nservices:\n  c:\n    host: azure.ai.connection\n    authType: ApiKey\n    credentials: *k\n", want: 1, contains: "credentials.key"},
		{name: "cfg007 merged http target", id: "FND-CFG-007",
			yaml: "t: &t\n  host: azure.ai.connection\n  target: http://example.com\nservices:\n  c:\n    <<: *t\n", want: 1, contains: "non-https"},
		{name: "cfg006 variable behind alias", id: "FND-CFG-006",
			yaml: "e: &e\n  X: ${NOPE}\nservices:\n  a:\n    host: azure.ai.agent\n    env: *e\n",
			p:    Providers{Env: fakeEnv{true, nil}}, arm: armOutStub{[]string{"O"}}, want: 1, contains: "NOPE"},
		{name: "cfg003 merged toolbox reference", id: "FND-CFG-003",
			yaml: "a: &a\n  host: azure.ai.agent\n  project: src\n  toolboxes: [missing]\nservices:\n  x:\n    <<: *a\n", want: 1, contains: "azure.ai.toolbox"},
	})
}
