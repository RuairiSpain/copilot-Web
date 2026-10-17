package rules

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestEnrichOverridesOnlyLower(t *testing.T) {
	tests := []struct {
		name     string
		basis    []string
		sev      sdk.Severity
		conf     sdk.Confidence
		wantSev  sdk.Severity
		wantConf sdk.Confidence
	}{
		{"no override", nil, "", "", sdk.SeverityError, sdk.ConfidenceMedium},
		{"lower severity honoured", nil, sdk.SeverityInfo, "", sdk.SeverityInfo, sdk.ConfidenceMedium},
		{"warning below error honoured", nil, sdk.SeverityWarning, "", sdk.SeverityWarning, sdk.ConfidenceMedium},
		{"equal severity unchanged", nil, sdk.SeverityError, "", sdk.SeverityError, sdk.ConfidenceMedium},
		{"invalid severity ignored", nil, "bogus", "", sdk.SeverityError, sdk.ConfidenceMedium},
		{"lower confidence honoured", nil, "", sdk.ConfidenceLow, sdk.SeverityError, sdk.ConfidenceLow},
		{"higher confidence ignored", nil, "", sdk.ConfidenceHigh, sdk.SeverityError, sdk.ConfidenceMedium},
		{"invalid confidence ignored", nil, "", "bogus", sdk.SeverityError, sdk.ConfidenceMedium},
		{"platform basis ignores severity override", []string{PlatformBasis}, sdk.SeverityInfo, "", sdk.SeverityError, sdk.ConfidenceMedium},
		{"platform basis still lowers confidence", []string{PlatformBasis}, sdk.SeverityInfo, sdk.ConfidenceLow, sdk.SeverityError, sdk.ConfidenceLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := finding()
			f.Severity, f.Confidence = tt.sev, tt.conf
			out := Enrich(f, cr("FND-X-001", tt.basis, ""), "foundry-prod", sdk.SeverityError)
			if out.Severity != tt.wantSev || out.Confidence != tt.wantConf {
				t.Fatalf("got %s/%s want %s/%s", out.Severity, out.Confidence, tt.wantSev, tt.wantConf)
			}
		})
	}
}

func TestOverrideDoesNotChangeFingerprint(t *testing.T) {
	a := Enrich(finding(), cr("FND-X-001", nil, ""), "foundry-prod", sdk.SeverityError)
	f := finding()
	f.Severity, f.Confidence = sdk.SeverityInfo, sdk.ConfidenceLow
	b := Enrich(f, cr("FND-X-001", nil, ""), "foundry-prod", sdk.SeverityError)
	if a.Fingerprint != b.Fingerprint {
		t.Fatalf("override changed fingerprint: %s vs %s", a.Fingerprint, b.Fingerprint)
	}
}

func TestEngineReportsFindingsAndSkips(t *testing.T) {
	res := sdk.Result{
		Findings: []sdk.Finding{finding()},
		Skips:    []sdk.Skip{{Reason: "unresolved-expression: other: why"}},
	}
	e := &Engine{Catalog: []catalog.Rule{cr("FND-X-001", nil, "")}, Registry: reg(t, fake{id: "FND-X-001", res: res}), Required: []string{"FND-X-001"}}
	rep := run(t, e, "foundry-prod", "")
	if len(rep.Findings) != 1 || len(rep.Evaluated) != 1 {
		t.Fatalf("findings=%d evaluated=%v", len(rep.Findings), rep.Evaluated)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].RuleID != "FND-X-001" || !rep.Skipped[0].Required {
		t.Fatalf("skips = %+v", rep.Skipped)
	}
	if rep.Findings[0].Evidence != "e" {
		t.Fatalf("evidence altered: %q", rep.Findings[0].Evidence)
	}
}

func TestEngineRejectsSkippedWithSkips(t *testing.T) {
	res := sdk.Result{Skipped: &sdk.Skip{Reason: "x"}, Skips: []sdk.Skip{{Reason: "y"}}}
	e := &Engine{Catalog: []catalog.Rule{cr("FND-X-001", nil, "")}, Registry: reg(t, fake{id: "FND-X-001", res: res})}
	rep := run(t, e, "foundry-prod", "")
	if len(rep.Skipped) != 1 || rep.Skipped[0].Reason != SkipRuleError {
		t.Fatalf("skips = %+v", rep.Skipped)
	}
}

type provModel struct {
	res      []sdk.ARMResource
	fromDflt map[string]bool
}

func (m provModel) Resources() []sdk.ARMResource { return m.res }
func (m provModel) FromDefault(r sdk.ARMResource) []string {
	if m.fromDflt[r.Name] {
		return []string{"deploySearch"}
	}
	return nil
}

type pathModel struct {
	provModel
	paths map[string][]string
}

func (m pathModel) FromDefaultPaths(r sdk.ARMResource) []string { return m.paths[r.Name] }

// P4: a default caps confidence only when it fed the checked property or the
// condition; a name or region default, or an unrelated property, does not.
func TestCapDefaultDerivedNarrowedByPath(t *testing.T) {
	res := []sdk.ARMResource{{Type: "Microsoft.Search/searchServices", Name: "a"}}
	f := sdk.Finding{Resource: sdk.ResourceRef{Type: "Microsoft.Search/searchServices", Name: "a"},
		Key: "publicNetworkAccess", Evidence: "properties.publicNetworkAccess is Enabled"}
	for _, tc := range []struct {
		name  string
		paths []string
		want  sdk.Confidence
	}{
		{"condition default", []string{"condition"}, sdk.ConfidenceMedium},
		{"property named by the finding", []string{"properties.publicNetworkAccess"}, sdk.ConfidenceMedium},
		{"name default only", []string{"name"}, ""},
		{"location default only", []string{"location"}, ""},
		{"name and location only", []string{"location", "name"}, ""},
		{"unrelated property", []string{"properties.replicaCount"}, ""},
		{"no path recorded", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := pathModel{provModel: provModel{res: res, fromDflt: map[string]bool{"a": true}}, paths: map[string][]string{"a": tc.paths}}
			if got := capDefaultDerived(f, m).Confidence; got != tc.want {
				t.Errorf("confidence %q want %q", got, tc.want)
			}
		})
	}
}

func TestCapDefaultDerivedLowersConfidenceOnly(t *testing.T) {
	m := provModel{res: []sdk.ARMResource{{Type: "Microsoft.Search/searchServices", Name: "a"}, {Type: "Microsoft.Search/searchServices", Name: "b"}},
		fromDflt: map[string]bool{"a": true}}
	derived := sdk.Finding{Resource: sdk.ResourceRef{Type: "microsoft.search/searchservices", Name: "a"}, Evidence: "e"}
	plain := sdk.Finding{Resource: sdk.ResourceRef{Type: "Microsoft.Search/searchServices", Name: "b"}, Evidence: "e"}
	got := capDefaultDerived(derived, m)
	if got.Confidence != sdk.ConfidenceMedium || got.Evidence != "e" {
		t.Errorf("default-derived finding: confidence %q evidence %q", got.Confidence, got.Evidence)
	}
	if capDefaultDerived(plain, m).Confidence != "" {
		t.Error("a finding on a resource not shaped by a default must be unchanged")
	}
	low := derived
	low.Confidence = sdk.ConfidenceLow
	if capDefaultDerived(low, m).Confidence != sdk.ConfidenceLow {
		t.Error("an already lower confidence must stay lower")
	}
	if capDefaultDerived(derived, nil).Confidence != "" {
		t.Error("a nil model must be tolerated")
	}
}
