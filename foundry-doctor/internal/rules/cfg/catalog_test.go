package cfg

import (
	"context"
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
	r := find(RegisterWith(Providers{Env: fakeEnv{true, nil}}), "FND-CFG-006")
	cases := []struct {
		name string
		arm  sdk.ARMModel
		want int
	}{
		{"output produces var", armOutStub{[]string{"ACR_ENDPOINT"}}, 0},
		{"unrelated output", armOutStub{[]string{"OTHER"}}, 1},
		{"model without outputs", armStub{}, 1},
		{"nil arm", nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := run(t, r, &sdk.Input{AzureYAML: doc(t, y), ARM: tc.arm})
			if res.Skipped != nil || len(res.Findings) != tc.want {
				t.Fatalf("skipped=%v findings=%+v want %d", res.Skipped, res.Findings, tc.want)
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
