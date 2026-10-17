package rules

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func withInputs(r catalog.Rule, inputs ...string) catalog.Rule { r.Inputs = inputs; return r }

type stubARM struct{}

func (stubARM) Resources() []sdk.ARMResource { return nil }

func TestEngineSkipsARMRulesForSyntheticProjects(t *testing.T) {
	armOnly := withInputs(cr("FND-SEC-001", nil, ""), "bicep-arm", "control-plane")
	mixed := withInputs(cr("FND-SEC-002", nil, ""), "azure.yaml", "bicep-arm")
	yamlOnly := withInputs(cr("FND-CFG-001", nil, ""), "azure.yaml")
	impls := []sdk.Rule{
		fake{id: "FND-SEC-001", res: sdk.Result{Findings: []sdk.Finding{finding()}}},
		fake{id: "FND-SEC-002"}, fake{id: "FND-CFG-001"},
	}
	t.Run("synthetic and no ARM", func(t *testing.T) {
		e := &Engine{Catalog: []catalog.Rule{armOnly, mixed, yamlOnly}, Registry: reg(t, impls...), Synthetic: true}
		rep := run(t, e, "foundry-prod", "")
		if len(rep.Findings) != 0 || len(rep.Skipped) != 1 {
			t.Fatalf("findings=%d skipped=%+v", len(rep.Findings), rep.Skipped)
		}
		s := rep.Skipped[0]
		if s.RuleID != "FND-SEC-001" || s.Reason != sdk.SkipSyntheticInfrastructure || s.MissingCapability != MissingARMCapability {
			t.Fatalf("skip = %+v", s)
		}
		if rep.SkippedByReason[sdk.SkipSyntheticInfrastructure] != 1 {
			t.Fatalf("SkippedByReason = %v", rep.SkippedByReason)
		}
		if len(rep.Evaluated) != 2 {
			t.Fatalf("evaluated = %v", rep.Evaluated)
		}
	})
	t.Run("not synthetic runs the rule", func(t *testing.T) {
		e := &Engine{Catalog: []catalog.Rule{armOnly}, Registry: reg(t, impls[0])}
		rep := run(t, e, "foundry-prod", "")
		if len(rep.Findings) != 1 || len(rep.Skipped) != 0 {
			t.Fatalf("findings=%d skipped=%+v", len(rep.Findings), rep.Skipped)
		}
	})
	t.Run("synthetic with ARM present runs the rule", func(t *testing.T) {
		e := &Engine{Catalog: []catalog.Rule{armOnly}, Registry: reg(t, impls[0]), Synthetic: true}
		rep, err := e.Run(context.Background(), &sdk.Input{Profile: "foundry-prod", ARM: stubARM{}})
		if err != nil || len(rep.Findings) != 1 {
			t.Fatalf("err=%v findings=%d", err, len(rep.Findings))
		}
	})
}

func TestEngineAzdVersionUnknown(t *testing.T) {
	r := cr("FND-CFG-001", nil, ">=1.34.2 <=1.36.0-beta.1")
	e := &Engine{Catalog: []catalog.Rule{r}, Registry: reg(t, fake{id: "FND-CFG-001"})}
	rep := run(t, e, "foundry-prod", "")
	if len(rep.Skipped) != 1 || rep.Skipped[0].Reason != sdk.SkipAzdVersionUnknown || rep.Skipped[0].MissingCapability != "azd version" {
		t.Fatalf("skipped = %+v", rep.Skipped)
	}
	rep = run(t, e, "foundry-prod", "1.35.0")
	if len(rep.Skipped) != 0 || len(rep.Evaluated) != 1 {
		t.Fatalf("known version: skipped=%+v evaluated=%v", rep.Skipped, rep.Evaluated)
	}
	rep = run(t, e, "foundry-prod", "2.0.0")
	if len(rep.Skipped) != 1 || rep.Skipped[0].Reason != sdk.SkipUnsupportedVersion {
		t.Fatalf("out of range: %+v", rep.Skipped)
	}
}

func TestEngineFingerprintCollisions(t *testing.T) {
	mk := func(ev string) sdk.Finding { f := finding(); f.Evidence = ev; return f }
	cat := []catalog.Rule{cr("FND-CFG-005", nil, "")}
	t.Run("different evidence, no key", func(t *testing.T) {
		e := &Engine{Catalog: cat, Registry: reg(t, fake{id: "FND-CFG-005", res: sdk.Result{Findings: []sdk.Finding{mk("a"), mk("b")}}})}
		rep := run(t, e, "foundry-prod", "")
		if len(rep.Findings) != 2 || rep.Findings[0].Fingerprint == rep.Findings[1].Fingerprint {
			t.Fatalf("findings = %+v", rep.Findings)
		}
		if len(rep.Diagnostics) != 1 {
			t.Fatalf("diagnostics = %v", rep.Diagnostics)
		}
	})
	t.Run("rule key separates", func(t *testing.T) {
		a, b := mk("a"), mk("b")
		a.Key, b.Key = "env.A", "env.B"
		e := &Engine{Catalog: cat, Registry: reg(t, fake{id: "FND-CFG-005", res: sdk.Result{Findings: []sdk.Finding{a, b}}})}
		rep := run(t, e, "foundry-prod", "")
		if len(rep.Findings) != 2 || rep.Findings[0].Fingerprint == rep.Findings[1].Fingerprint || len(rep.Diagnostics) != 0 {
			t.Fatalf("findings=%+v diagnostics=%v", rep.Findings, rep.Diagnostics)
		}
	})
	t.Run("identical evidence is merged", func(t *testing.T) {
		e := &Engine{Catalog: cat, Registry: reg(t, fake{id: "FND-CFG-005", res: sdk.Result{Findings: []sdk.Finding{mk("a"), mk("a")}}})}
		rep := run(t, e, "foundry-prod", "")
		if len(rep.Findings) != 1 || len(rep.Diagnostics) != 0 {
			t.Fatalf("findings=%d diagnostics=%v", len(rep.Findings), rep.Diagnostics)
		}
	})
}

func TestClassifySkip(t *testing.T) {
	tests := []struct {
		reason string
		want   SkipClass
	}{
		{sdk.SkipNotImplemented, SkipByDesign},
		{sdk.SkipSyntheticInfrastructure, SkipByDesign},
		{sdk.SkipSyntheticInfrastructure + ": no on-disk infra", SkipByDesign},
		{sdk.SkipNotApplicable + ": no search service", SkipByDesign},
		{sdk.SkipMissingPolicyKey("tags.required"), SkipByDesign},
		{"no-explicit-observability-or-server-tracing-requirement", SkipByDesign},
		{"no-apis", SkipByDesign},
		{"project-has-no-agent-services", SkipByDesign},
		{"no-emit-token-metric-policy", SkipByDesign},
		{"no-resilience-objective-declared", SkipByDesign},
		{"no-access-requirement-declared", SkipByDesign},
		{"backend-does-not-target-foundry-or-azure-openai", SkipByDesign},
		{"schedule-null", SkipByDesign},
		{"no-split-skill-in-project; no-explicit-query-definitions", SkipByDesign},
		{sdk.SkipNotApplicable + ": fewer than two azd environments", SkipByDesign},
		// Missing input still hides a check.
		{"repository-root-not-available", SkipHidesCheck},
		{"index-metadata-not-readable", SkipHidesCheck},
		{"input-unavailable: account has no model deployments", SkipByDesign},
		{"input-unavailable: Project has no capability host or no bound resources", SkipByDesign},
		{"input-unavailable: Foundry management metadata is not available", SkipHidesCheck},
		{sdk.SkipInputUnavailable, SkipHidesCheck},
		{sdk.SkipUnsupportedVersion, SkipHidesCheck},
		{sdk.SkipAzdVersionUnknown, SkipHidesCheck},
		{SkipRuleError, SkipHidesCheck},
		{SkipInvalidMetadata, SkipHidesCheck},
		{SkipUnsupportedProfile, SkipHidesCheck},
		{"some-new-reason", SkipHidesCheck},
		{"", SkipHidesCheck},
	}
	for _, tt := range tests {
		if got := ClassifySkip(sdk.Skip{Reason: tt.reason}); got != tt.want {
			t.Errorf("ClassifySkip(%q) = %v, want %v", tt.reason, got, tt.want)
		}
	}
	if AnyHiddenCheck([]sdk.Skip{{Reason: sdk.SkipNotImplemented}, {Reason: sdk.SkipMissingPolicyKey("x")}}) {
		t.Error("by-design skips must not count as hidden checks")
	}
	if !AnyHiddenCheck([]sdk.Skip{{Reason: sdk.SkipNotImplemented}, {Reason: sdk.SkipInputUnavailable}}) {
		t.Error("input-unavailable must count as a hidden check")
	}
	if AnyHiddenCheck(nil) {
		t.Error("no skips, no hidden check")
	}
}
