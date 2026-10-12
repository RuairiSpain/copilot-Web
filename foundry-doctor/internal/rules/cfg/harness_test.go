package cfg_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cfg"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// fixturePath resolves a path below test/fixtures.
func fixturePath(rel string) string {
	return filepath.Join("..", "..", "..", "test", "fixtures", filepath.FromSlash(rel))
}

func readFixture(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(fixturePath(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// inputOpt mutates an Input under construction.
type inputOpt func(*model.Input)

// yamlInput parses azure.yaml text with the real reader and builds an Input.
func yamlInput(t *testing.T, text string, opts ...inputOpt) *model.Input {
	t.Helper()
	res, err := azureyaml.Parse([]byte(text), azureyaml.Options{File: "azure.yaml"})
	if err != nil {
		t.Fatalf("parse azure.yaml: %v", err)
	}
	in := &model.Input{
		Project:   model.Project{Name: res.YAML.Name, AzureYAMLPath: "azure.yaml", IaC: model.IaCNone},
		Profile:   "foundry-dev",
		AzureYAML: res.YAML, YAMLReport: res,
	}
	for _, o := range opts {
		o(in)
	}
	return in
}

func fixtureInput(t *testing.T, rel string, opts ...inputOpt) *model.Input {
	t.Helper()
	return yamlInput(t, readFixture(t, rel), opts...)
}

func withEnv(kv map[string]string) inputOpt {
	return func(in *model.Input) {
		e := model.NewEnvironment("dev", kv)
		in.Environment = &e
	}
}

func withIaC(m model.IaCMode) inputOpt { return func(in *model.Input) { in.Project.IaC = m } }

func withVersions(v model.Versions) inputOpt { return func(in *model.Input) { in.Versions = v } }

func withTools(ts ...sdk.ToolStatus) inputOpt { return func(in *model.Input) { in.Tools = ts } }

// withARM normalises a committed ARM JSON fixture with the real normaliser.
func withARM(t *testing.T, rel string) inputOpt {
	t.Helper()
	n, err := bicep.Normalise([]byte(readFixture(t, rel)), bicep.Options{File: "infra/main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	return func(in *model.Input) { in.ARM = n.Template; in.Project.IaC = model.IaCBicep }
}

func ruleByID(t *testing.T, id string) rules.Rule {
	t.Helper()
	for _, r := range cfg.Rules() {
		if r.ID() == id {
			return r
		}
	}
	t.Fatalf("rule %s not registered", id)
	return nil
}

// eval runs a rule and checks the contract every result must meet.
func eval(t *testing.T, id string, in *model.Input) []rules.Result {
	t.Helper()
	r := ruleByID(t, id)
	out, err := r.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s: no result", id)
	}
	for _, res := range out {
		if err := res.Validate(); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		switch {
		case res.Finding != nil:
			f := res.Finding
			if f.Key == "" || f.RuleID != id || f.RuleVersion != r.Version() || f.Evidence == "" || f.Recommendation == "" {
				t.Fatalf("%s: incomplete finding %+v", id, f)
			}
			if res.Status == sdk.StatusUncertain && f.Confidence != sdk.ConfidenceUncertain {
				t.Fatalf("%s: uncertain result with confidence %q", id, f.Confidence)
			}
		case res.Skip != nil:
			s := res.Skip
			if s.RuleID != id || s.RuleVersion != r.Version() || s.MissingCapability == "" && s.Reason != rules.ReasonNotApplicable {
				t.Fatalf("%s: incomplete skip %+v", id, s)
			}
		}
	}
	return out
}

func withStatus(rs []rules.Result, st sdk.Status) []rules.Result {
	var out []rules.Result
	for _, r := range rs {
		if r.Status == st {
			out = append(out, r)
		}
	}
	return out
}

// wantFinding asserts a finding of the status whose key starts with prefix.
func wantFinding(t *testing.T, rs []rules.Result, st sdk.Status, prefix string) *sdk.Finding {
	t.Helper()
	for _, r := range withStatus(rs, st) {
		if strings.HasPrefix(r.Finding.Key, prefix) {
			return r.Finding
		}
	}
	t.Fatalf("no %s finding with key prefix %q in %s", st, prefix, describe(rs))
	return nil
}

func wantNoFindings(t *testing.T, rs []rules.Result) {
	t.Helper()
	for _, r := range rs {
		if r.Finding != nil {
			t.Fatalf("unexpected finding in %s", describe(rs))
		}
	}
}

func wantPass(t *testing.T, rs []rules.Result) {
	t.Helper()
	if len(rs) != 1 || rs[0].Status != sdk.StatusPass {
		t.Fatalf("want one pass, got %s", describe(rs))
	}
}

func wantSkip(t *testing.T, rs []rules.Result, reasonPrefix string) *sdk.SkippedCheck {
	t.Helper()
	for _, r := range withStatus(rs, sdk.StatusSkipped) {
		if strings.HasPrefix(r.Skip.Reason, reasonPrefix) {
			return r.Skip
		}
	}
	t.Fatalf("no skip with reason %q in %s", reasonPrefix, describe(rs))
	return nil
}

func describe(rs []rules.Result) string {
	var parts []string
	for _, r := range rs {
		switch {
		case r.Finding != nil:
			parts = append(parts, string(r.Status)+"("+r.Finding.Key+")")
		case r.Skip != nil:
			parts = append(parts, "skipped("+r.Skip.Reason+")")
		default:
			parts = append(parts, string(r.Status))
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}
