package env

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

type prov []Environment

func (p prov) Environments() []Environment { return p }

type pol map[string]any

func (p pol) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

func eval(t *testing.T, id string, p Provider, in *sdk.Input) sdk.Result {
	t.Helper()
	for _, r := range RegisterWith(p) {
		if r.ID() == id {
			res, err := r.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			return res
		}
	}
	t.Fatalf("no rule %s", id)
	return sdk.Result{}
}

func TestRules(t *testing.T) {
	tiersPol := pol{TierPolicyKey: map[string]any{"p": "prod", "d": "dev"}}
	tests := []struct {
		name     string
		id       string
		p        Provider
		policy   sdk.Policy
		wantSkip bool
		want     int
		contains string
	}{
		{"001 shared rg", "FND-ENV-001", prov{{Name: "d", Subscription: "S", ResourceGroup: "RG"}, {Name: "p", Subscription: "s", ResourceGroup: "rg"}}, nil, false, 1, "share"},
		{"001 distinct", "FND-ENV-001", prov{{Name: "d", Subscription: "s", ResourceGroup: "a"}, {Name: "p", Subscription: "s", ResourceGroup: "b"}}, nil, false, 0, ""},
		{"001 literal rg", "FND-ENV-001", prov{{Name: "d", ResourceGroup: "a", LiteralRGInYAML: true}, {Name: "p"}}, nil, false, 1, "literal"},
		{"001 single env skips", "FND-ENV-001", prov{{Name: "d"}}, nil, true, 0, ""},
		{"001 no provider skips", "FND-ENV-001", nil, nil, true, 0, ""},
		{"002 shared endpoint", "FND-ENV-002", prov{{Name: "d", Values: map[string]string{"AZURE_ENV_NAME": "d", "X_ENDPOINT": "https://a.services.ai.azure.com"}}, {Name: "p", Values: map[string]string{"AZURE_ENV_NAME": "p", "X_ENDPOINT": "https://a.services.ai.azure.com"}}}, nil, false, 1, "fingerprint"},
		{"002 different", "FND-ENV-002", prov{{Name: "d", Values: map[string]string{"AZURE_ENV_NAME": "d", "X_ID": "1"}}, {Name: "p", Values: map[string]string{"AZURE_ENV_NAME": "p", "X_ID": "2"}}}, nil, false, 0, ""},
		{"002 not env bound", "FND-ENV-002", prov{{Name: "d", Values: map[string]string{"AZURE_ENV_NAME": "d", "FOO": "1"}}, {Name: "p", Values: map[string]string{"AZURE_ENV_NAME": "p", "FOO": "1"}}}, nil, false, 0, ""},
		{"003 no policy skips", "FND-ENV-003", prov{{Name: "d"}, {Name: "p"}}, nil, true, 0, ""},
		{"003 shared rg", "FND-ENV-003", prov{{Name: "d", ResourceGroup: "RG"}, {Name: "p", ResourceGroup: "rg"}}, tiersPol, false, 1, "prod environment p"},
		{"003 distinct", "FND-ENV-003", prov{{Name: "d", ResourceGroup: "a"}, {Name: "p", ResourceGroup: "b"}}, tiersPol, false, 0, ""},
		{"004 only in", "FND-ENV-004", prov{{Name: "d", Values: map[string]string{"A": "1"}}, {Name: "p", Values: map[string]string{}}}, nil, false, 1, "only in d"},
		{"004 secret by name", "FND-ENV-004", prov{{Name: "d", Values: map[string]string{"API_KEY": "s1"}}, {Name: "p", Values: map[string]string{"API_KEY": "s2"}}}, nil, false, 1, "differs between"},
		{"004 same", "FND-ENV-004", prov{{Name: "d", Values: map[string]string{"A": "1"}}, {Name: "p", Values: map[string]string{"A": "1"}}}, nil, false, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := eval(t, tc.id, tc.p, &sdk.Input{Policy: tc.policy})
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

func TestNoRawValuesInEvidence(t *testing.T) {
	p := prov{{Name: "d", Values: map[string]string{"API_KEY": "topsecret1", "N": "plainval1"}}, {Name: "p", Values: map[string]string{"API_KEY": "topsecret2", "N": "plainval2"}}}
	res := eval(t, "FND-ENV-004", p, &sdk.Input{})
	for _, f := range res.Findings {
		for _, v := range []string{"topsecret", "plainval"} {
			if strings.Contains(f.Evidence, v) {
				t.Fatalf("leaked %s", v)
			}
		}
	}
}

func TestRegisteredIDsInCatalogue(t *testing.T) {
	rules, err := catalog.Load(context.Background(), ruledata.FS, ruledata.Root)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range rules {
		for _, ph := range r.Phases {
			if ph == "1" {
				ids[r.ID] = true
			}
		}
	}
	for _, r := range Register() {
		if !ids[r.ID()] {
			t.Errorf("%s not phase 1 in catalogue", r.ID())
		}
	}
}
