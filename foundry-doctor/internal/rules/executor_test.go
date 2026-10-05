package rules_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func cfgFor(profile string) rules.ExecConfig {
	return rules.ExecConfig{Profile: profile, Stamper: fakeStamper{}}
}

func run(t *testing.T, in *model.Input, es []rules.Entry, cfg rules.ExecConfig) rules.Outcome {
	t.Helper()
	out, err := rules.Execute(context.Background(), in, es, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestExecuteStampsFindings(t *testing.T) {
	m := meta("FND-SEC-001", "SEC")
	e := entry(m, failing("FND-SEC-001"))
	tests := []struct {
		profile string
		want    sdk.Severity
	}{
		{rules.ProfileDev, sdk.SeverityInfo}, {rules.ProfileTest, sdk.SeverityWarning}, {rules.ProfileProd, sdk.SeverityError},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			out := run(t, &model.Input{}, []rules.Entry{e}, cfgFor(tc.profile))
			if len(out.Findings) != 1 || len(out.Errors) != 0 {
				t.Fatalf("findings %d errors %v", len(out.Findings), out.Errors)
			}
			f := out.Findings[0]
			if f.Severity != tc.want || f.Profile != tc.profile || f.Category != sdk.CategoryMustHave || f.Pillar != "security" {
				t.Errorf("stamp: %+v", f)
			}
			if f.DocsURL != "https://example.com/a" || f.LastVerified != "2026-03-04" {
				t.Errorf("docs/verified: %q %q", f.DocsURL, f.LastVerified)
			}
			if f.Recommendation != "do the thing" || f.Fix != "fix it" {
				t.Errorf("fallbacks: %q %q", f.Recommendation, f.Fix)
			}
			if strings.Contains(f.Evidence, "SECRET") {
				t.Errorf("evidence not redacted: %q", f.Evidence)
			}
			if f.Fingerprint != "fp:FND-SEC-001|FND-SEC-001|" {
				t.Errorf("fingerprint %q", f.Fingerprint)
			}
			if out.Executed != 1 {
				t.Errorf("executed %d", out.Executed)
			}
		})
	}
}

func TestPlatformBasisIsErrorInEveryProfile(t *testing.T) {
	m := meta("FND-SEC-001", "SEC", func(m *catalog.Rule) {
		m.Basis = []string{"platform"}
		m.Severity = catalog.Severity{Dev: "info", Test: "info", Prod: "info"}
	})
	for _, p := range []string{rules.ProfileDev, rules.ProfileTest, rules.ProfileProd} {
		out := run(t, &model.Input{}, []rules.Entry{entry(m, failing("FND-SEC-001"))}, cfgFor(p))
		if got := out.Findings[0].Severity; got != sdk.SeverityError {
			t.Errorf("%s: severity %s, want error", p, got)
		}
	}
}

func TestSeverityForErrors(t *testing.T) {
	m := meta("FND-SEC-001", "SEC")
	if _, err := rules.SeverityFor(m, "nope"); err == nil {
		t.Error("unknown profile accepted")
	}
	m.Severity.Dev = "loud"
	if _, err := rules.SeverityFor(m, rules.ProfileDev); err == nil {
		t.Error("bad catalogue severity accepted")
	}
}

func TestNormalizeProfile(t *testing.T) {
	for in, want := range map[string]string{"dev": "foundry-dev", "foundry-test": "foundry-test", "prod": "foundry-prod"} {
		if got, err := rules.NormalizeProfile(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if _, err := rules.NormalizeProfile("staging"); err == nil {
		t.Error("staging accepted")
	}
}

func TestExecuteConfigErrors(t *testing.T) {
	e := []rules.Entry{entry(meta("FND-SEC-001", "SEC"), failing("FND-SEC-001"))}
	ctx := context.Background()
	if _, err := rules.Execute(ctx, &model.Input{}, e, rules.ExecConfig{Profile: rules.ProfileDev}); err == nil {
		t.Error("nil stamper accepted")
	}
	if _, err := rules.Execute(ctx, nil, e, cfgFor(rules.ProfileDev)); err == nil {
		t.Error("nil input accepted")
	}
	if _, err := rules.Execute(ctx, &model.Input{}, e, cfgFor("dev")); err == nil {
		t.Error("short profile name accepted; the engine takes the long form")
	}
}

// TestSkipNeverPasses: skipped results land in Skipped, never in Passed, never as findings.
func TestSkipNeverPasses(t *testing.T) {
	r := fakeRule{id: "FND-SEC-001", version: 1}
	r.eval = func(context.Context, *model.Input) ([]rules.Result, error) {
		return []rules.Result{rules.SkipFor(r, rules.ReasonProfileKeyMissing(model.KeyTagsRequired), "SECRET detail", "policy.tags.required", sdk.ResourceRef{Name: "x"})}, nil
	}
	out := run(t, &model.Input{}, []rules.Entry{entry(meta("FND-SEC-001", "SEC"), r)}, cfgFor(rules.ProfileProd))
	if out.Passed != 0 || len(out.Findings) != 0 || len(out.Skipped) != 1 {
		t.Fatalf("passed %d findings %d skipped %d", out.Passed, len(out.Findings), len(out.Skipped))
	}
	if s := out.Skipped[0]; s.Reason != "profile-key-missing:policy.tags.required" || s.Detail != "[redacted] detail" || s.RuleVersion != 1 {
		t.Errorf("skip: %+v", s)
	}
	if got := rules.SkipSummary(out.Skipped); got["profile-key-missing:policy.tags.required"] != 1 || len(got) != 1 {
		t.Errorf("summary %v", got)
	}
	if rules.SkipSummary(nil) == nil {
		t.Error("SkipSummary(nil) must be non-nil")
	}
}

func TestPassCounted(t *testing.T) {
	out := run(t, &model.Input{}, []rules.Entry{entry(meta("FND-SEC-001", "SEC"), fakeRule{id: "FND-SEC-001", version: 1})}, cfgFor(rules.ProfileDev))
	if out.Passed != 1 || len(out.Findings) != 0 || len(out.Skipped) != 0 {
		t.Fatalf("%+v", out)
	}
}

func TestUncertainConfidence(t *testing.T) {
	r := fakeRule{id: "FND-SEC-001", version: 1}
	r.eval = func(context.Context, *model.Input) ([]rules.Result, error) {
		return []rules.Result{rules.Uncertain(rules.NewFinding(r, sdk.ResourceRef{Name: "a"}, "e", "r"))}, nil
	}
	out := run(t, &model.Input{}, []rules.Entry{entry(meta("FND-SEC-001", "SEC"), r)}, cfgFor(rules.ProfileDev))
	if out.Findings[0].Confidence != sdk.ConfidenceUncertain {
		t.Errorf("confidence %s", out.Findings[0].Confidence)
	}
}

func TestExecuteRuleFailures(t *testing.T) {
	mk := func(id string, eval func(r fakeRule) ([]rules.Result, error)) rules.Entry {
		r := fakeRule{id: id, version: 1}
		r.eval = func(context.Context, *model.Input) ([]rules.Result, error) { return eval(r) }
		return entry(meta(id, "SEC"), r)
	}
	tests := []struct {
		name  string
		entry rules.Entry
		panic bool
		want  string
	}{
		{"panic", mk("FND-SEC-002", func(fakeRule) ([]rules.Result, error) { panic("boom SECRET") }), true, "boom [redacted]"},
		{"nil map panic", mk("FND-SEC-002", func(fakeRule) ([]rules.Result, error) { var m map[string]int; m["a"] = 1; return nil, nil }), true, "panic"},
		{"error", mk("FND-SEC-002", func(fakeRule) ([]rules.Result, error) { return nil, errors.New("io failed") }), false, "io failed"},
		{"rule set severity", mk("FND-SEC-002", func(r fakeRule) ([]rules.Result, error) {
			return []rules.Result{rules.Fail(sdk.Finding{RuleID: r.id, RuleVersion: 1, Severity: sdk.SeverityError})}, nil
		}), false, "engine owns them"},
		{"wrong rule id in finding", mk("FND-SEC-002", func(r fakeRule) ([]rules.Result, error) {
			return []rules.Result{rules.Fail(sdk.Finding{RuleID: "FND-SEC-999", RuleVersion: 1})}, nil
		}), false, "finding names FND-SEC-999"},
		{"wrong version in skip", mk("FND-SEC-002", func(r fakeRule) ([]rules.Result, error) {
			return []rules.Result{rules.Skipped(sdk.SkippedCheck{RuleID: r.id, RuleVersion: 7, Reason: "x"})}, nil
		}), false, "skip names"},
		{"skip without reason", mk("FND-SEC-002", func(r fakeRule) ([]rules.Result, error) {
			return []rules.Result{{Status: sdk.StatusSkipped, Skip: &sdk.SkippedCheck{RuleID: r.id}}}, nil
		}), false, "reason"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A good rule on each side proves one failing rule does not stop the run.
			es := []rules.Entry{entry(meta("FND-SEC-001", "SEC"), failing("FND-SEC-001")), tc.entry, entry(meta("FND-SEC-003", "SEC"), failing("FND-SEC-003"))}
			out := run(t, &model.Input{}, es, cfgFor(rules.ProfileDev))
			if len(out.Findings) != 2 {
				t.Errorf("findings %d, want 2 from the healthy rules", len(out.Findings))
			}
			if len(out.Errors) != 1 {
				t.Fatalf("errors %v", out.Errors)
			}
			e := out.Errors[0]
			if e.RuleID != "FND-SEC-002" || e.Panic != tc.panic || !strings.Contains(e.Error(), tc.want) || e.Unwrap() == nil {
				t.Errorf("error %+v (%s)", e, e.Error())
			}
			if strings.Contains(e.Error(), "SECRET") {
				t.Error("error not redacted")
			}
		})
	}
}

func TestExecuteCancellation(t *testing.T) {
	es := []rules.Entry{
		entry(meta("FND-SEC-001", "SEC"), failing("FND-SEC-001")),
		entry(meta("FND-SEC-002", "SEC"), failing("FND-SEC-002")),
		entry(meta("FND-SEC-003", "SEC"), failing("FND-SEC-003")),
	}
	t.Run("already cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		out, err := rules.Execute(ctx, &model.Input{}, es, cfgFor(rules.ProfileDev))
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(out.Interrupted, context.Canceled) || out.Executed != 0 || len(out.NotRun) != 3 || len(out.Findings) != 0 {
			t.Fatalf("%+v", out)
		}
	})
	t.Run("cancelled by a rule", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mid := fakeRule{id: "FND-SEC-002", version: 1}
		mid.eval = func(context.Context, *model.Input) ([]rules.Result, error) {
			cancel()
			return []rules.Result{rules.Fail(rules.NewFinding(mid, sdk.ResourceRef{}, "partial", ""))}, nil
		}
		es2 := []rules.Entry{es[0], entry(meta("FND-SEC-002", "SEC"), mid), es[2]}
		out, err := rules.Execute(ctx, &model.Input{}, es2, cfgFor(rules.ProfileDev))
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(out.Interrupted, context.Canceled) {
			t.Fatalf("interrupted %v", out.Interrupted)
		}
		if len(out.Findings) != 1 || out.Findings[0].RuleID != "FND-SEC-001" {
			t.Errorf("partial results of the cancelled rule must be discarded: %+v", out.Findings)
		}
		if want := []string{"FND-SEC-002", "FND-SEC-003"}; !reflect.DeepEqual(out.NotRun, want) {
			t.Errorf("NotRun %v want %v", out.NotRun, want)
		}
		if out.Executed != 1 {
			t.Errorf("executed %d", out.Executed)
		}
	})
}

func TestVersionGate(t *testing.T) {
	withAzd := func(m *catalog.Rule) { m.Compatibility.Azd = ">=1.34.2 <=1.36.0-beta.1" }
	withExt := func(m *catalog.Rule) {
		m.Compatibility.Extensions = map[string]string{"azure.ai.projects": ">=1.0.0-beta.13 <=1.0.0-beta.13"}
	}
	tests := []struct {
		name     string
		mut      func(*catalog.Rule)
		versions model.Versions
		strict   bool
		skipped  string // substring of Detail; empty means the rule runs
	}{
		{"inside range", withAzd, model.Versions{Azd: "1.35.0"}, false, ""},
		{"outside range", withAzd, model.Versions{Azd: "1.37.0"}, false, "azd 1.37.0 is outside the verified range"},
		{"below range", withAzd, model.Versions{Azd: "1.0.0"}, false, "outside"},
		{"undetected runs by default", withAzd, model.Versions{}, false, ""},
		{"undetected skipped when strict", withAzd, model.Versions{}, true, "azd version not detected"},
		{"unparseable version", withAzd, model.Versions{Azd: "next"}, false, "cannot be compared"},
		{"no range declared", func(*catalog.Rule) {}, model.Versions{Azd: "9.9.9"}, false, ""},
		{"extension outside", withExt, model.Versions{Extensions: map[string]string{"azure.ai.projects": "1.0.0"}}, false, "extension azure.ai.projects 1.0.0 is outside"},
		{"extension inside", withExt, model.Versions{Extensions: map[string]string{"azure.ai.projects": "1.0.0-beta.13"}}, false, ""},
		{"extension undetected", withExt, model.Versions{}, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cfgFor(rules.ProfileDev)
			cfg.StrictVersions = tc.strict
			out := run(t, &model.Input{Versions: tc.versions}, []rules.Entry{entry(meta("FND-CFG-001", "CFG", tc.mut), failing("FND-CFG-001"))}, cfg)
			if tc.skipped == "" {
				if len(out.Findings) != 1 || len(out.Skipped) != 0 {
					t.Fatalf("rule should run: %+v", out)
				}
				return
			}
			if len(out.Findings) != 0 || out.Executed != 0 || len(out.Skipped) != 1 {
				t.Fatalf("rule should be skipped: %+v", out)
			}
			s := out.Skipped[0]
			if s.Reason != rules.ReasonUnsupportedVersion || !strings.Contains(s.Detail, tc.skipped) || s.RuleID != "FND-CFG-001" || s.RuleVersion != 1 {
				t.Errorf("skip %+v", s)
			}
		})
	}
}

func TestInputAvailabilityGate(t *testing.T) {
	armOnly := func(m *catalog.Rule) { m.Inputs = []string{"bicep-arm", "control-plane"} }
	armAndYAML := func(m *catalog.Rule) { m.Inputs = []string{"azure.yaml", "bicep-arm"} }
	live := func(m *catalog.Rule) { m.Inputs = []string{"control-plane", "data-plane"} }
	tests := []struct {
		name    string
		mut     func(*catalog.Rule)
		in      model.Input
		skipped string // expected reason; empty means the rule runs
		missing string
	}{
		{"synthetic skips arm-only", armOnly, model.Input{Project: model.Project{IaC: model.IaCSynthetic}}, rules.ReasonSyntheticInfrastructure, "compiled ARM template"},
		{"none skips arm-only", armOnly, model.Input{Project: model.Project{IaC: model.IaCNone}}, rules.ReasonSyntheticInfrastructure, "compiled ARM template"},
		{"bicep with ARM runs", armOnly, model.Input{Project: model.Project{IaC: model.IaCBicep}, ARM: &model.ARMTemplate{}}, "", ""},
		{"bicep without ARM is unresolved", armOnly, model.Input{Project: model.Project{IaC: model.IaCBicep}}, rules.ReasonUnresolvedValue("compiled-arm-unavailable"), "compiled ARM template"},
		{"arm-json without ARM is unresolved", armOnly, model.Input{Project: model.Project{IaC: model.IaCARMJSON}}, rules.ReasonUnresolvedValue("compiled-arm-unavailable"), "compiled ARM template"},
		{"azure.yaml rule runs in synthetic", armAndYAML, model.Input{Project: model.Project{IaC: model.IaCSynthetic}}, "", ""},
		{"live-only inputs", live, model.Input{}, rules.ReasonControlPlaneUnavailable, "live Azure access"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			out := run(t, &in, []rules.Entry{entry(meta("FND-SEC-001", "SEC", tc.mut), failing("FND-SEC-001"))}, cfgFor(rules.ProfileDev))
			if tc.skipped == "" {
				if len(out.Findings) != 1 || len(out.Skipped) != 0 {
					t.Fatalf("should run: %+v", out)
				}
				return
			}
			if len(out.Findings) != 0 || out.Passed != 0 || len(out.Skipped) != 1 {
				t.Fatalf("should skip and never pass: %+v", out)
			}
			if s := out.Skipped[0]; s.Reason != tc.skipped || s.MissingCapability != tc.missing {
				t.Errorf("skip %+v", s)
			}
		})
	}
}

// TestExecuteDeterministicUnderShuffle: any input order and repeated runs give the same outcome.
func TestExecuteDeterministicUnderShuffle(t *testing.T) {
	var es []rules.Entry
	for i := 1; i <= 12; i++ {
		id := fmt.Sprintf("FND-SEC-%03d", i)
		var e rules.Entry
		switch i % 3 {
		case 0:
			r := fakeRule{id: id, version: 1}
			r.eval = func(context.Context, *model.Input) ([]rules.Result, error) {
				return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable, "", "", sdk.ResourceRef{Name: "z"}), rules.SkipFor(r, rules.ReasonNotApplicable, "", "", sdk.ResourceRef{Name: "a"})}, nil
			}
			e = entry(meta(id, "SEC"), r)
		default:
			e = entry(meta(id, "SEC"), failing(id))
		}
		es = append(es, e)
	}
	want := run(t, &model.Input{}, es, cfgFor(rules.ProfileTest))
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		shuffled := append([]rules.Entry(nil), es...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		got := run(t, &model.Input{}, shuffled, cfgFor(rules.ProfileTest))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d differs from the sorted run", i)
		}
	}
	if want.Skipped[0].Resource.Name != "a" {
		t.Error("skipped list must be sorted by resource within a rule")
	}
}

func TestExecuteSkipsUnimplementedEntries(t *testing.T) {
	out := run(t, &model.Input{}, []rules.Entry{{Meta: meta("FND-OPS-001", "OPS")}}, cfgFor(rules.ProfileDev))
	if out.Executed != 0 || len(out.Skipped) != 0 || len(out.Findings) != 0 {
		t.Fatalf("%+v", out)
	}
}

// Race check: independent Execute calls over shared entries and a shared registry.
func TestExecuteConcurrentCalls(t *testing.T) {
	es := []rules.Entry{entry(meta("FND-SEC-001", "SEC"), failing("FND-SEC-001"))}
	done := make(chan rules.Outcome, 8)
	for i := 0; i < 8; i++ {
		go func() {
			out, _ := rules.Execute(context.Background(), &model.Input{}, es, cfgFor(rules.ProfileProd))
			done <- out
		}()
	}
	first := <-done
	for i := 1; i < 8; i++ {
		if got := <-done; !reflect.DeepEqual(got, first) {
			t.Fatal("concurrent outcomes differ")
		}
	}
}
