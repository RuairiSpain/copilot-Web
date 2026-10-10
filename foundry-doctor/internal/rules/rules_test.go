package rules

import (
	"context"
	"errors"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fake struct {
	id  string
	res sdk.Result
	err error
	pan bool
}

func (f fake) ID() string { return f.id }
func (f fake) Evaluate(context.Context, *sdk.Input) (sdk.Result, error) {
	if f.pan {
		panic("boom")
	}
	return f.res, f.err
}

func cr(id string, basis []string, azd string) catalog.Rule {
	r := catalog.Rule{ID: id, Version: 3, Group: "CFG", Category: "must-have", Pillar: "security", Basis: basis,
		Severity:       catalog.Severity{Dev: "info", Test: "warning", Prod: "error"},
		Evidence:       catalog.Evidence{Confidence: "likely"},
		Recommendation: "do it", Sources: []catalog.Source{{URL: "https://example.test/doc", LastVerified: "2025-01-01"}}}
	if azd != "" {
		if err := yaml.Unmarshal([]byte(`"`+azd+`"`), &r.Compatibility.Azd); err != nil {
			panic(err)
		}
	}
	return r
}

func finding() sdk.Finding {
	return sdk.Finding{Resource: sdk.ResourceRef{Type: "t", Name: "n"}, Location: sdk.Location{File: "azure.yaml", Line: 2}, Evidence: "e"}
}

func run(t *testing.T, e *Engine, profile, azd string) *Report {
	t.Helper()
	rep, err := e.Run(context.Background(), &sdk.Input{Profile: profile, AzdVersion: azd})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func reg(t *testing.T, rs ...sdk.Rule) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, x := range rs {
		if err := r.Register(x); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("nil accepted")
	}
	if err := r.Register(fake{id: ""}); err == nil {
		t.Error("empty id accepted")
	}
	if err := r.Register(fake{id: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fake{id: "A"}); err == nil {
		t.Error("duplicate accepted")
	}
	_ = r.Register(fake{id: "0"})
	if ids := r.IDs(); len(ids) != 2 || ids[0] != "0" {
		t.Errorf("ids = %v", ids)
	}
}

func TestEnginePositiveEnrich(t *testing.T) {
	e := &Engine{Catalog: []catalog.Rule{cr("FND-CFG-001", []string{"opinion"}, "")},
		Registry: reg(t, fake{id: "FND-CFG-001", res: sdk.Result{Findings: []sdk.Finding{finding()}}})}
	for profile, want := range map[string]sdk.Severity{"foundry-dev": "info", "foundry-test": "warning", "foundry-prod": "error"} {
		rep := run(t, e, profile, "")
		if len(rep.Findings) != 1 {
			t.Fatalf("%s: %+v", profile, rep)
		}
		f := rep.Findings[0]
		if f.Severity != want || f.RuleVersion != 3 || f.Confidence != sdk.ConfidenceMedium || f.Fingerprint == "" ||
			f.DocsURL == "" || f.Profile != profile || f.Recommendation != "do it" || f.Category != sdk.CategoryMustHave {
			t.Errorf("%s: %+v", profile, f)
		}
	}
}

func TestEnginePlatformAlwaysError(t *testing.T) {
	c := cr("FND-CFG-002", []string{"platform"}, "")
	c.Severity = catalog.Severity{Dev: "info", Test: "warning", Prod: "warning"}
	e := &Engine{Catalog: []catalog.Rule{c}, Registry: reg(t, fake{id: c.ID, res: sdk.Result{Findings: []sdk.Finding{finding()}}})}
	for _, p := range []string{"foundry-dev", "foundry-test", "foundry-prod"} {
		if rep := run(t, e, p, ""); rep.Findings[0].Severity != sdk.SeverityError {
			t.Errorf("%s not error", p)
		}
	}
}

func TestEngineSkips(t *testing.T) {
	bad := cr("FND-X-003", nil, "")
	bad.Severity.Prod = "loud"
	missing := cr("FND-X-004", nil, "")
	missing.Severity.Test = ""
	ranged := cr("FND-X-005", nil, ">=1.0.0 <2.0.0")
	ok := fake{id: "FND-X-005", res: sdk.Result{}}
	tests := []struct {
		name    string
		cat     catalog.Rule
		impl    []sdk.Rule
		profile string
		azd     string
		reason  string
		req     bool
	}{
		{"not implemented", cr("FND-X-001", nil, ""), nil, "foundry-prod", "", sdk.SkipNotImplemented, false},
		{"required skip", cr("FND-X-001", nil, ""), nil, "foundry-prod", "", sdk.SkipNotImplemented, true},
		{"unsupported profile", cr("FND-X-002", nil, ""), []sdk.Rule{fake{id: "FND-X-002"}}, "custom", "", SkipUnsupportedProfile, false},
		{"invalid severity", bad, []sdk.Rule{fake{id: bad.ID}}, "foundry-prod", "", SkipInvalidMetadata, false},
		{"missing severity key", missing, []sdk.Rule{fake{id: missing.ID}}, "foundry-test", "", sdk.SkipProfileKeyPrefix + "severity.test", false},
		{"azd unsupported", ranged, []sdk.Rule{ok}, "foundry-prod", "3.0.0", sdk.SkipUnsupportedVersion, false},
		{"azd unknown", ranged, []sdk.Rule{ok}, "foundry-prod", "", sdk.SkipInputUnavailable, false},
		{"rule skip passthrough", cr("FND-X-006", nil, ""), []sdk.Rule{fake{id: "FND-X-006", res: sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipMissingPolicyKey("models.allowed")}}}}, "foundry-prod", "", "profile-key-missing:models.allowed", false},
		{"rule error", cr("FND-X-007", nil, ""), []sdk.Rule{fake{id: "FND-X-007", err: errors.New("x")}}, "foundry-prod", "", SkipRuleError, false},
		{"rule panic", cr("FND-X-008", nil, ""), []sdk.Rule{fake{id: "FND-X-008", pan: true}}, "foundry-prod", "", SkipRuleError, false},
		{"skip with findings", cr("FND-X-009", nil, ""), []sdk.Rule{fake{id: "FND-X-009", res: sdk.Result{Findings: []sdk.Finding{finding()}, Skipped: &sdk.Skip{Reason: "r"}}}}, "foundry-prod", "", SkipRuleError, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{Catalog: []catalog.Rule{tc.cat}, Registry: reg(t, tc.impl...)}
			if tc.req {
				e.Required = []string{tc.cat.ID}
			}
			rep := run(t, e, tc.profile, tc.azd)
			if len(rep.Findings) != 0 || len(rep.Evaluated) != 0 || len(rep.Skipped) != 1 {
				t.Fatalf("rep = %+v", rep)
			}
			if s := rep.Skipped[0]; s.Reason != tc.reason || s.RuleID != tc.cat.ID || s.Required != tc.req {
				t.Fatalf("skip = %+v", s)
			}
		})
	}
}

func TestEngineAzdInRangeEvaluates(t *testing.T) {
	c := cr("FND-X-005", nil, ">=1.0.0 <2.0.0")
	e := &Engine{Catalog: []catalog.Rule{c}, Registry: reg(t, fake{id: c.ID})}
	rep := run(t, e, "foundry-prod", "1.5.0")
	if len(rep.Evaluated) != 1 || len(rep.Skipped) != 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestSelectorAndOrdering(t *testing.T) {
	a, b := cr("FND-B-001", nil, ""), cr("FND-A-001", nil, "")
	a.Group, b.Group = "B", "A"
	b.Category = "nice-to-have"
	dropped := cr("FND-A-002", nil, "")
	dropped.Status = catalog.StatusDropped
	im := reg(t, fake{id: a.ID, res: sdk.Result{Findings: []sdk.Finding{finding()}}}, fake{id: b.ID, res: sdk.Result{Findings: []sdk.Finding{finding()}}})
	tests := []struct {
		name string
		sel  Selector
		want []string
	}{
		{"all sorted, dropped hidden", Selector{}, []string{"FND-A-001", "FND-B-001"}},
		{"group", Selector{Groups: []string{"a"}}, []string{"FND-A-001"}},
		{"category", Selector{Categories: []string{"must-have"}}, []string{"FND-B-001"}},
		{"id", Selector{IDs: []string{"FND-B-001"}}, []string{"FND-B-001"}},
		{"exclude wins", Selector{IDs: []string{"FND-B-001"}, Exclude: []string{"FND-B-001"}}, nil},
		{"exclude category", Selector{Exclude: []string{"nice-to-have"}}, []string{"FND-B-001"}},
		{"no match", Selector{Pillars: []string{"cost"}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{Catalog: []catalog.Rule{a, dropped, b}, Registry: im, Selector: tc.sel}
			rep := run(t, e, "foundry-prod", "")
			if len(rep.Evaluated) != len(tc.want) {
				t.Fatalf("evaluated = %v", rep.Evaluated)
			}
			for i, w := range tc.want {
				if rep.Evaluated[i] != w {
					t.Fatalf("evaluated = %v, want %v", rep.Evaluated, tc.want)
				}
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	e := &Engine{Registry: NewRegistry()}
	if _, err := e.Run(context.Background(), nil); err == nil {
		t.Error("nil input accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.Catalog = []catalog.Rule{cr("FND-X-001", nil, "")}
	if _, err := e.Run(ctx, &sdk.Input{Profile: "foundry-prod"}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestEnrichRedactsEvidence(t *testing.T) {
	f := finding()
	f.Evidence = "pass" + "word=abcdefghijklmnop"
	out := Enrich(f, cr("FND-X-001", nil, ""), "foundry-prod", sdk.SeverityError)
	if out.Evidence == f.Evidence {
		t.Fatalf("evidence not redacted: %q", out.Evidence)
	}
}
