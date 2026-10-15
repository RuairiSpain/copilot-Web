package cfg

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

type armStub struct{}

func (armStub) Resources() []sdk.ARMResource { return nil }

type armOutStub struct{ names []string }

func (armOutStub) Resources() []sdk.ARMResource { return nil }
func (s armOutStub) Outputs() []sdk.ARMOutput {
	var o []sdk.ARMOutput
	for _, n := range s.names {
		o = append(o, sdk.ARMOutput{Name: n, Type: "string"})
	}
	return o
}

func TestCFG006ARMOutputsProducer(t *testing.T) {
	y := "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: ${ACR_ENDPOINT}\n"
	cases := []struct {
		name         string
		arm          sdk.ARMModel
		env          map[string]string
		wantFindings int
		wantSkip     bool
	}{
		{"output only, not provisioned yet is uncertain", armOutStub{[]string{"ACR_ENDPOINT"}}, nil, 0, true},
		{"output present and value in env passes", armOutStub{[]string{"ACR_ENDPOINT"}}, map[string]string{"ACR_ENDPOINT": "x"}, 0, false},
		{"unrelated output, name undefined", armOutStub{[]string{"OTHER"}}, nil, 1, false},
		{"model without outputs is uncertain", armStub{}, nil, 0, true},
		{"nil arm is uncertain", nil, nil, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := find(RegisterWith(Providers{Env: fakeEnv{true, tc.env}}), "FND-CFG-006")
			res := run(t, r, &sdk.Input{AzureYAML: doc(t, y), ARM: tc.arm})
			if (res.Skipped != nil) != tc.wantSkip || len(res.Findings) != tc.wantFindings {
				t.Fatalf("skipped=%v findings=%+v want findings=%d skip=%v", res.Skipped, res.Findings, tc.wantFindings, tc.wantSkip)
			}
			if tc.wantSkip && !strings.HasPrefix(res.Skipped.Reason, "uncertain: ") {
				t.Fatalf("skip reason %q is not marked uncertain", res.Skipped.Reason)
			}
		})
	}
}

func TestRegisteredIDsInCatalogue(t *testing.T) {
	rules, err := catalog.Load(context.Background(), ruledata.FS, ruledata.Root)
	if err != nil {
		t.Fatal(err)
	}
	cfgRules := map[string]bool{}
	for _, r := range rules {
		if r.Group == "CFG" {
			cfgRules[r.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, r := range Register() {
		if !cfgRules[r.ID()] {
			t.Errorf("%s not a CFG catalogue rule", r.ID())
		}
		if seen[r.ID()] {
			t.Errorf("duplicate %s", r.ID())
		}
		seen[r.ID()] = true
	}
}
