package report

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestBuildReadiness(t *testing.T) {
	ids := []string{"FND-DEP-001", "FND-DEP-002", "FND-DEP-003", "FND-DEP-004"}
	fs := []sdk.Finding{{RuleID: "FND-DEP-002", Severity: sdk.SeverityError}}
	skips := []sdk.Skip{
		{RuleID: "FND-DEP-003", Reason: UncertainPrefix + "cannot be proven"},
		{RuleID: "FND-DEP-004", Reason: "input-unavailable: capability X; missing permission Y"},
	}
	r := BuildReadiness(ids, fs, skips)
	if r == nil {
		t.Fatal("nil readiness")
	}
	var b strings.Builder
	r.console(&b)
	got := b.String()
	for _, want := range []string{"FND-DEP-001", "FND-DEP-002", "FND-DEP-003", "FND-DEP-004", "guarantee"} {
		if !strings.Contains(got, want) {
			t.Errorf("console readiness missing %q:\n%s", want, got)
		}
	}
	var m strings.Builder
	r.markdown(&m)
	if !strings.Contains(m.String(), "FND-DEP-002") {
		t.Errorf("markdown readiness missing blocked rule:\n%s", m.String())
	}
	if readinessProps(r) == nil {
		t.Error("props nil")
	}
}

func TestBuildReadinessClassification(t *testing.T) {
	ids := []string{"A", "B", "C", "D", "E", "F"}
	fs := []sdk.Finding{
		{RuleID: "A", Severity: sdk.SeverityWarning},
		{RuleID: "B", Severity: sdk.SeverityInfo},
		{RuleID: "C", Severity: sdk.SeverityError, Baselined: true},
	}
	skips := []sdk.Skip{
		{RuleID: "D", Reason: UncertainPrefix + "x"},
		{RuleID: "E", Reason: "input-unavailable: capability X"},
	}
	r := BuildReadiness(ids, fs, skips)
	want := map[string][]string{
		"blocked": {"A", "B", "C"}, "uncertain": {"D"}, "skipped": {"E"}, "ready": {"F"},
	}
	got := map[string][]string{"blocked": r.Blocked, "uncertain": r.Uncertain, "skipped": r.Skipped, "ready": r.Ready}
	for k, w := range want {
		if strings.Join(got[k], ",") != strings.Join(w, ",") {
			t.Errorf("%s = %v, want %v", k, got[k], w)
		}
	}
}

func TestBuildReadinessNoRules(t *testing.T) {
	r := BuildReadiness(nil, nil, nil)
	var b strings.Builder
	if r != nil {
		r.console(&b)
	}
	if strings.Contains(b.String(), "FND-DEP") {
		t.Errorf("unexpected rule output: %s", b.String())
	}
}
