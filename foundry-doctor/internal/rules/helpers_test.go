package rules_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// fakeRule is a configurable rule.
type fakeRule struct {
	id      string
	version int
	eval    func(ctx context.Context, in *model.Input) ([]rules.Result, error)
}

func (f fakeRule) ID() string   { return f.id }
func (f fakeRule) Version() int { return f.version }
func (f fakeRule) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if f.eval == nil {
		return []rules.Result{rules.Pass()}, nil
	}
	return f.eval(ctx, in)
}

// failing returns a rule that fails once on a resource named after the rule.
func failing(id string) fakeRule {
	f := fakeRule{id: id, version: 1}
	f.eval = func(context.Context, *model.Input) ([]rules.Result, error) {
		fd := rules.NewFinding(f, sdk.ResourceRef{Kind: "arm-resource", Type: "T", Name: id}, "key sk-SECRET here", "")
		return []rules.Result{rules.Fail(fd)}, nil
	}
	return f
}

// fakeStamper hashes nothing: the fingerprint is rule|name|key, and Redact masks "SECRET".
type fakeStamper struct{}

func (fakeStamper) Fingerprint(f sdk.Finding) string {
	return "fp:" + f.RuleID + "|" + f.Resource.Name + "|" + f.Key
}
func (fakeStamper) Redact(s string) string { return strings.ReplaceAll(s, "SECRET", "[redacted]") }

// meta builds a catalogue rule.
func meta(id, group string, mut ...func(*catalog.Rule)) catalog.Rule {
	m := catalog.Rule{
		ID: id, Version: 1, Group: group, Status: catalog.StatusVerified, Phases: []string{"1"},
		Inputs: []string{"azure.yaml"}, Basis: []string{"security"}, Category: "must-have", Pillar: "security",
		Severity:       catalog.Severity{Dev: "info", Test: "warning", Prod: "error"},
		Recommendation: "do the thing", Fix: "fix it",
		Sources:        []catalog.Source{{URL: "https://example.com/a", LastVerified: "2026-01-02"}, {URL: "https://example.com/b", LastVerified: "2026-03-04"}},
		Implementation: catalog.Implementation{Owner: "native", Package: "x"},
	}
	for _, f := range mut {
		f(&m)
	}
	return m
}

func entry(m catalog.Rule, r rules.Rule) rules.Entry { return rules.Entry{Meta: m, Impl: r} }

func mustRegistry(t *testing.T, cat []catalog.Rule, impls ...rules.Rule) *rules.Registry {
	t.Helper()
	reg, err := rules.NewRegistry(cat, impls, rules.Phase1)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func testCatalog() []catalog.Rule {
	return []catalog.Rule{
		meta("FND-SEC-001", "SEC"),
		meta("FND-SEC-002", "SEC", func(m *catalog.Rule) { m.Category = "nice-to-have" }),
		meta("FND-NET-001", "NET"),
		meta("FND-OPS-001", "OPS", func(m *catalog.Rule) { m.Phases = []string{"2"} }),
		meta("FND-SEC-014", "SEC", func(m *catalog.Rule) { m.Implementation.Owner = "bicep" }),
	}
}
