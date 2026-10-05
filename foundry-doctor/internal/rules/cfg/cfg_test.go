package cfg

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fakeLayout map[string]bool

func (f fakeLayout) DirExists(p string) bool  { return f[p+"/"] }
func (f fakeLayout) FileExists(p string) bool { return f[p] }

type fakeEnv struct {
	sel  bool
	vals map[string]string
}

func (f fakeEnv) Selected() (string, bool)  { return "dev", f.sel }
func (f fakeEnv) Values() map[string]string { return f.vals }

func doc(t *testing.T, y string) sdk.AzureYAMLView {
	t.Helper()
	d, err := azureyaml.Parse([]byte(y), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func run(t *testing.T, r sdk.Rule, in *sdk.Input) sdk.Result {
	t.Helper()
	res, err := r.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func find(rs []sdk.Rule, id string) sdk.Rule {
	for _, r := range rs {
		if r.ID() == id {
			return r
		}
	}
	return nil
}

func TestRules(t *testing.T) {
	prompt := func(extra string) string {
		return "name: x\nservices:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n" + extra
	}
	tests := []struct {
		name     string
		id       string
		yaml     string
		p        Providers
		arm      bool
		wantSkip bool
		want     int
		contains string
	}{
		{"cfg004 blank model", "FND-CFG-004", prompt("    instructions: hi\n"), Providers{}, false, false, 1, "blank model"},
		{"cfg004 complete prompt", "FND-CFG-004", prompt("    model: m\n    instructions: hi\n"), Providers{}, false, false, 0, ""},
		{"cfg004 hosted cpu above 4", "FND-CFG-004", "services:\n  a:\n    host: azure.ai.agent\n    project: src\n    image: i\n    cpu: \"8\"\n", Providers{}, false, false, 1, "cpu"},
		{"cfg004 code config missing entrypoint", "FND-CFG-004", "services:\n  a:\n    host: azure.ai.agent\n    project: src\n    codeConfiguration:\n      runtime: python\n", Providers{}, false, false, 1, "entryPoint"},
		{"cfg004 hosted missing dir", "FND-CFG-004", "services:\n  a:\n    host: azure.ai.agent\n    project: src\n", Providers{Layout: fakeLayout{}}, false, false, 1, "does not exist"},
		{"cfg005 literal api key", "FND-CFG-005", "services:\n  c:\n    host: azure.ai.connection\n    authType: ApiKey\n    credentials:\n      key: literal-value\n", Providers{}, false, false, 1, "credentials.key"},
		{"cfg005 env reference", "FND-CFG-005", "services:\n  c:\n    host: azure.ai.connection\n    authType: ApiKey\n    credentials:\n      key: ${MY_KEY}\n", Providers{}, false, false, 0, ""},
		{"cfg005 managed identity", "FND-CFG-005", "services:\n  c:\n    host: azure.ai.connection\n    authType: AAD\n", Providers{}, false, false, 0, ""},
		{"cfg005 url userinfo", "FND-CFG-005", "services:\n  c:\n    host: azure.ai.connection\n    target: https://u:p@example.com/x\n", Providers{}, false, false, 1, "embeds"},
		{"cfg007 http", "FND-CFG-007", "services:\n  c:\n    host: azure.ai.connection\n    target: http://example.com\n", Providers{}, false, false, 1, "non-https"},
		{"cfg007 localhost https", "FND-CFG-007", "services:\n  c:\n    host: azure.ai.connection\n    target: https://localhost/x\n", Providers{}, false, false, 1, "local"},
		{"cfg007 ok", "FND-CFG-007", "services:\n  c:\n    host: azure.ai.connection\n    target: https://example.com\n", Providers{}, false, false, 0, ""},
		{"cfg007 ref skipped", "FND-CFG-007", "services:\n  c:\n    host: azure.ai.connection\n    target: ${URL}\n", Providers{}, false, false, 0, ""},
		{"cfg011 legacy config", "FND-CFG-011", "services:\n  a:\n    host: azure.ai.agent\n    config: {}\n", Providers{}, false, false, 1, "legacy"},
		{"cfg011 clean", "FND-CFG-011", "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n", Providers{}, false, false, 0, ""},
		{"cfg006 no env skip", "FND-CFG-006", "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${FOO}\n", Providers{}, true, true, 0, ""},
		{"cfg006 no arm still evaluates", "FND-CFG-006", "services: {}\n", Providers{Env: fakeEnv{true, nil}}, false, false, 0, ""},
		{"cfg006 no arm undefined", "FND-CFG-006", "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${FOO}\n", Providers{Env: fakeEnv{true, nil}}, false, false, 1, "FOO"},
		{"cfg006 no arm agent producer", "FND-CFG-006", "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${AGENT_A_ENDPOINT}\n      Y: ${AZURE_LOCATION}\n", Providers{Env: fakeEnv{true, nil}}, false, false, 0, ""},
		{"cfg006 undefined", "FND-CFG-006", "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${FOO}\n", Providers{Env: fakeEnv{true, map[string]string{"BAR": "1"}}}, true, false, 1, "FOO"},
		{"cfg006 defined", "FND-CFG-006", "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${FOO}\n", Providers{Env: fakeEnv{true, map[string]string{"FOO": "1"}}}, true, false, 0, ""},
		{"cfg012 display name", "FND-CFG-012", "services: {}\n", Providers{Env: fakeEnv{true, map[string]string{"AZURE_LOCATION": "East US"}}}, false, false, 1, "lowercase"},
		{"cfg012 good", "FND-CFG-012", "services: {}\n", Providers{Env: fakeEnv{true, map[string]string{"AZURE_LOCATION": "eastus"}}}, false, false, 0, ""},
		{"cfg012 unavailable", "FND-CFG-012", "services: {}\n", Providers{}, false, true, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := find(RegisterWith(tc.p), tc.id)
			if r == nil {
				t.Fatalf("rule %s missing", tc.id)
			}
			in := &sdk.Input{AzureYAML: doc(t, tc.yaml)}
			if tc.arm {
				in.ARM = armStub{}
			}
			res := run(t, r, in)
			if (res.Skipped != nil) != tc.wantSkip {
				t.Fatalf("skipped=%v want %v", res.Skipped, tc.wantSkip)
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

func TestSkipWhenNoAzureYAML(t *testing.T) {
	for _, r := range Register() {
		res := run(t, r, &sdk.Input{})
		if res.Skipped == nil || len(res.Findings) != 0 {
			t.Errorf("%s: want skip, got %+v", r.ID(), res)
		}
	}
}

func TestEvidenceNeverContainsSecret(t *testing.T) {
	y := "services:\n  c:\n    host: azure.ai.connection\n    authType: ApiKey\n    credentials:\n      key: literal-value\n"
	res := run(t, find(Register(), "FND-CFG-005"), &sdk.Input{AzureYAML: doc(t, y)})
	for _, f := range res.Findings {
		if strings.Contains(f.Evidence, "literal-value") {
			t.Fatal("evidence leaks value")
		}
	}
}
