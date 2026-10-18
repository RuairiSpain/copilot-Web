package assess

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestBuildStatesAndTopActions(t *testing.T) {
	rules := []catalog.Rule{
		rule("FND-SEC-001", "security", "must-have"),
		rule("FND-REL-007", "reliability", "nice-to-have"),
		rule("FND-OPS-002", "operations", "nice-to-have"),
		rule("FND-COST-001", "cost", "nice-to-have"),
		rule("FND-OPS-001", "operations", "must-have"),
	}
	defs := []Definition{
		{RuleID: "FND-SEC-001"},
		{RuleID: "FND-REL-007", NoEvidenceState: StateQuestion, QuestionKey: "disasterRecovery.declared", QuestionPrompt: "Has the team recorded a disaster recovery decision?"},
		{RuleID: "FND-OPS-002", NoEvidenceState: StateUnknown},
		{RuleID: "FND-COST-001", NoEvidenceState: StateUnknown},
		{RuleID: "FND-OPS-001"},
	}
	got, err := Build(Input{
		Profile:     "foundry-prod",
		Catalogue:   rules,
		Definitions: defs,
		Evaluated:   []string{"FND-SEC-001", "FND-OPS-001"},
		Findings: []sdk.Finding{
			{RuleID: "FND-SEC-001", Severity: sdk.SeverityError},
			{RuleID: "FND-OPS-001", Severity: sdk.SeverityWarning, Baselined: true},
		},
		Policy: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkState(t, got, "FND-SEC-001", StateFail)
	checkState(t, got, "FND-REL-007", StateQuestion)
	checkState(t, got, "FND-OPS-002", StateUnknown)
	checkState(t, got, "FND-COST-001", StateUnknown)
	checkState(t, got, "FND-OPS-001", StateWarning)
	if !strings.HasPrefix(got.Decision, "No-go") {
		t.Fatalf("decision=%q", got.Decision)
	}
	if len(got.TopActions) == 0 || got.TopActions[0].RuleID != "FND-SEC-001" {
		t.Fatalf("top actions=%+v", got.TopActions)
	}
}

// Review round 1 (lead B1, black belt B4): an evaluated rule with a skipped
// per-resource check must never PASS.
func TestPartialSkipIsNeverPass(t *testing.T) {
	got := build(t, Definition{RuleID: "FND-REL-002"}, Input{
		Evaluated: []string{"FND-REL-002"},
		Skipped:   []sdk.Skip{{RuleID: "FND-REL-002", Reason: "unresolved-expression: storage account st"}},
	}, "must-have")
	c := got.Controls[0]
	if c.State != StateUnknown || !strings.Contains(c.StateReason, "Partial evaluation") {
		t.Fatalf("state=%s reason=%q", c.State, c.StateReason)
	}
	if len(got.Unassessed) != 1 {
		t.Fatalf("partial skip not listed as unassessed: %+v", got.Unassessed)
	}
	if !strings.HasPrefix(got.Decision, "Proceed with conditions") {
		t.Fatalf("decision=%q", got.Decision)
	}
}

func TestEvaluatedWithNotApplicableSkipPasses(t *testing.T) {
	got := build(t, Definition{RuleID: "FND-REL-002"}, Input{
		Evaluated: []string{"FND-REL-002"},
		Skipped:   []sdk.Skip{{RuleID: "FND-REL-002", Reason: sdk.SkipNotApplicable + ": no storage account"}},
	}, "nice-to-have")
	checkState(t, got, "FND-REL-002", StatePass)
}

// Review round 1 (black belt B4): not-applicable is a distinct outcome that does
// not force conditions on the decision.
func TestNotApplicableDoesNotBlockDecision(t *testing.T) {
	got := build(t, Definition{RuleID: "FND-SEC-003"}, Input{
		Skipped: []sdk.Skip{{RuleID: "FND-SEC-003", Reason: sdk.SkipNotApplicable + ": project has no Cosmos DB account"}},
	}, "must-have")
	checkState(t, got, "FND-SEC-003", StateNotApplicable)
	if !strings.HasPrefix(got.Decision, "Proceed within assessed scope") {
		t.Fatalf("decision=%q", got.Decision)
	}
	if len(got.Unassessed) != 0 || len(got.TopActions) != 0 {
		t.Fatalf("not-applicable leaked: unassessed=%+v top=%+v", got.Unassessed, got.TopActions)
	}
}

func TestHiddenSkipIsSkippedNotApplicable(t *testing.T) {
	for _, reason := range []string{sdk.SkipNotImplemented, sdk.SkipSyntheticInfrastructure, sdk.SkipMissingPolicyKey("network.publicAccess"), sdk.SkipInputUnavailable + ": control plane"} {
		got := build(t, Definition{RuleID: "FND-SEC-001"}, Input{
			Skipped: []sdk.Skip{{RuleID: "FND-SEC-001", Reason: reason}},
		}, "must-have")
		if got.Controls[0].State != StateSkipped {
			t.Fatalf("reason %q: state=%s", reason, got.Controls[0].State)
		}
		if !strings.Contains(got.Controls[0].StateReason, reason) {
			t.Fatalf("reason not shown: %q", got.Controls[0].StateReason)
		}
	}
}

// Review round 2 (FL NB1, BB NB1): a configuration delegation to Azure Policy
// is an unverified team attestation, never a deterministic NOT_APPLICABLE, and
// a mandatory delegated control keeps the decision conditional.
func TestPolicyManagedIsAttestedNotApplicable(t *testing.T) {
	for _, tc := range []struct{ rule, reason string }{
		{"FND-OPS-001", "policy-managed"},
		{"FND-NET-003", "policy-managed"},
		{"FND-NET-003", "central-dns: DNS declared as centrally managed in policy"},
	} {
		got := build(t, Definition{RuleID: tc.rule}, Input{
			Skipped: []sdk.Skip{{RuleID: tc.rule, Reason: tc.reason}},
		}, "must-have")
		c := got.Controls[0]
		if c.State != StateAttested || c.Basis != BasisAttested {
			t.Fatalf("%s %q: state=%s basis=%s", tc.rule, tc.reason, c.State, c.Basis)
		}
		if !strings.Contains(c.StateReason, "Delegated to Azure Policy by configuration; assignment and compliance not verified") {
			t.Fatalf("reason=%q", c.StateReason)
		}
		if !strings.HasPrefix(got.Decision, "Proceed with conditions") {
			t.Fatalf("%s: mandatory delegated control must keep conditions, decision=%q", tc.rule, got.Decision)
		}
	}
}

// Review round 2: "nothing declared" skips are questions, listed as unassessed.
func TestUndeclaredRequirementIsQuestion(t *testing.T) {
	for _, tc := range []struct{ rule, reason string }{
		{"FND-OPS-002", "no-explicit-observability-or-server-tracing-requirement"},
		{"FND-REL-009", "no-resilience-objective-declared"},
	} {
		got := build(t, Definition{RuleID: tc.rule, Costly: true, CostBasis: "billed", CostSourceURL: "https://learn.microsoft.com/x", CostVerified: "2026-10-18"}, Input{
			Skipped: []sdk.Skip{{RuleID: tc.rule, Reason: tc.reason}},
		}, "nice-to-have")
		c := got.Controls[0]
		if c.State != StateQuestion || c.Basis != BasisNone {
			t.Fatalf("%s: state=%s basis=%s", tc.rule, c.State, c.Basis)
		}
		if len(got.Unassessed) != 1 || len(got.Costly) != 1 {
			t.Fatalf("%s: unassessed=%+v costly=%+v", tc.rule, got.Unassessed, got.Costly)
		}
	}
}

func TestClassifySkip(t *testing.T) {
	for reason, want := range map[string]skipKind{
		sdk.SkipNotApplicable + ": no Cosmos DB": skipNotApplicable,
		"dev-profile":                            skipNotApplicable,
		sdk.SkipInputUnavailable + ": project has no capability host": skipNotApplicable,
		"policy-managed": skipDelegated,
		"central-dns: DNS declared as centrally managed in policy":     skipDelegated,
		"no-resilience-objective-declared":                             skipUndeclared,
		"no-access-requirement-declared":                               skipUndeclared,
		"no-explicit-observability-or-server-tracing-requirement":      skipUndeclared,
		sdk.SkipNotImplemented:                                         skipHidden,
		sdk.SkipSyntheticInfrastructure:                                skipHidden,
		sdk.SkipMissingPolicyKey("network.cloud"):                      skipHidden,
		sdk.SkipInputUnavailable + ": no dependency is proven private": skipHidden,
		"unresolved-expression: storage st":                            skipHidden,
	} {
		if got := classifySkip(sdk.Skip{Reason: reason}); got != want {
			t.Errorf("%q: got %d want %d", reason, got, want)
		}
	}
}

// Review round 1 (black belt B1): info posture findings are not warnings and
// never reach top actions or the decision.
func TestInfoFindingIsNotWarning(t *testing.T) {
	got := build(t, Definition{RuleID: "FND-SEC-010", Costly: true, CostBasis: "Key Vault is billable", CostSourceURL: "https://learn.microsoft.com/x", CostVerified: "2026-10-18"}, Input{
		Evaluated: []string{"FND-SEC-010"},
		Findings:  []sdk.Finding{{RuleID: "FND-SEC-010", Severity: sdk.SeverityInfo, Evidence: "CMK posture: cmk"}},
	}, "nice-to-have")
	checkState(t, got, "FND-SEC-010", StateInfo)
	if len(got.TopActions) != 0 || len(got.Costly) != 0 {
		t.Fatalf("info leaked: top=%+v costly=%+v", got.TopActions, got.Costly)
	}
	if !strings.HasPrefix(got.Decision, "Proceed within assessed scope") {
		t.Fatalf("decision=%q", got.Decision)
	}
}

func TestInfoWithPartialSkipIsUnknown(t *testing.T) {
	got := build(t, Definition{RuleID: "FND-SEC-010"}, Input{
		Evaluated: []string{"FND-SEC-010"},
		Findings:  []sdk.Finding{{RuleID: "FND-SEC-010", Severity: sdk.SeverityInfo}},
		Skipped:   []sdk.Skip{{RuleID: "FND-SEC-010", Reason: "unresolved-expression: search"}},
	}, "nice-to-have")
	checkState(t, got, "FND-SEC-010", StateUnknown)
}

// Review round 1 (lead B2, black belt B5): answers are evaluated, not just
// present, and are recorded as attested.
func TestQuestionAnswers(t *testing.T) {
	def := Definition{RuleID: "FND-REL-007", NoEvidenceState: StateQuestion, QuestionKey: "disasterRecovery.declared", QuestionReferenceKey: "disasterRecovery.reference"}
	cases := []struct {
		name   string
		policy map[string]any
		want   State
		basis  string
	}{
		{"unanswered", map[string]any{}, StateQuestion, BasisNone},
		{"declared with reference", map[string]any{"disasterRecovery.declared": true, "disasterRecovery.reference": "https://contoso.example/dr"}, StateAttested, BasisAttested},
		{"declared without reference", map[string]any{"disasterRecovery.declared": true}, StateQuestion, BasisNone},
		{"declared with blank reference", map[string]any{"disasterRecovery.declared": true, "disasterRecovery.reference": "  "}, StateQuestion, BasisNone},
		{"declared false", map[string]any{"disasterRecovery.declared": false, "disasterRecovery.reference": "x"}, StateWarning, BasisAttested},
		{"non-boolean", map[string]any{"disasterRecovery.declared": "yes"}, StateQuestion, BasisNone},
	}
	for _, tc := range cases {
		got := build(t, def, Input{Policy: tc.policy, Skipped: []sdk.Skip{{RuleID: "FND-REL-007", Reason: sdk.SkipNotImplemented}}}, "nice-to-have")
		c := got.Controls[0]
		if c.State != tc.want || c.Basis != tc.basis {
			t.Errorf("%s: state=%s basis=%s reason=%q", tc.name, c.State, c.Basis, c.StateReason)
		}
		if c.State == StatePass {
			t.Errorf("%s: a recorded answer must never be PASS", tc.name)
		}
	}
}

func TestCostlyOptionalSeparatedFromTopActions(t *testing.T) {
	rules := []catalog.Rule{rule("FND-REL-001", "reliability", "nice-to-have"), rule("FND-OPS-003", "operations", "nice-to-have")}
	got, err := Build(Input{
		Catalogue: rules,
		Definitions: []Definition{
			{RuleID: "FND-REL-001", Costly: true, CostBasis: "replicas are billed", CostSourceURL: "https://learn.microsoft.com/azure/search/search-sku-manage-costs", CostVerified: "2026-10-18"},
			{RuleID: "FND-OPS-003"},
		},
		Evaluated: []string{"FND-REL-001", "FND-OPS-003"},
		Findings: []sdk.Finding{
			{RuleID: "FND-REL-001", Severity: sdk.SeverityWarning},
			{RuleID: "FND-OPS-003", Severity: sdk.SeverityWarning},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Costly) != 1 || got.Costly[0].RuleID != "FND-REL-001" || got.Costly[0].CostBasis == "" {
		t.Fatalf("costly=%+v", got.Costly)
	}
	for _, a := range got.TopActions {
		if a.RuleID == "FND-REL-001" {
			t.Fatal("costly optional recommendation must not be a top action")
		}
	}
	if len(got.TopActions) != 1 || got.TopActions[0].RuleID != "FND-OPS-003" {
		t.Fatalf("top=%+v", got.TopActions)
	}
}

func TestActionsCarryOpinionLabel(t *testing.T) {
	r := rule("FND-OPS-002", "operations", "nice-to-have")
	r.Status = catalog.StatusProductOpinion
	got, err := Build(Input{Catalogue: []catalog.Rule{r}, Definitions: []Definition{{RuleID: "FND-OPS-002"}},
		Evaluated: []string{"FND-OPS-002"}, Findings: []sdk.Finding{{RuleID: "FND-OPS-002", Severity: sdk.SeverityWarning}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TopActions) != 1 || !got.TopActions[0].ProjectOpinion {
		t.Fatalf("top=%+v", got.TopActions)
	}
}

func TestPillarName(t *testing.T) {
	for k, want := range map[string]string{"operations": "Operational Excellence", "performance": "Performance Efficiency", "cost": "Cost Optimization"} {
		if got := PillarName(k); got != want {
			t.Errorf("%s=%s", k, got)
		}
	}
}

func build(t *testing.T, def Definition, in Input, category string) Assessment {
	t.Helper()
	pillar := "security"
	if strings.Contains(def.RuleID, "REL") {
		pillar = "reliability"
	}
	in.Catalogue = []catalog.Rule{rule(def.RuleID, pillar, category)}
	in.Definitions = []Definition{def}
	got, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func rule(id, pillar, category string) catalog.Rule {
	return catalog.Rule{
		ID:             id,
		Title:          id + " title",
		Pillar:         pillar,
		Category:       category,
		Status:         catalog.StatusVerified,
		Recommendation: "Fix it.",
		Sources:        []catalog.Source{{URL: "https://learn.microsoft.com/example", LastVerified: "2026-10-05"}},
	}
}

func checkState(t *testing.T, got Assessment, ruleID string, want State) {
	t.Helper()
	for _, c := range got.Controls {
		if c.RuleID == ruleID {
			if c.State != want {
				t.Fatalf("%s state=%s want %s (reason %q)", ruleID, c.State, want, c.StateReason)
			}
			return
		}
	}
	t.Fatalf("rule %s not found", ruleID)
}

// Phase 4 loop 1 (lead B2, black belt N8): a control with no detected defect
// must not carry the rule's fix text in the actions list.
func TestUnassessedActionHasNoFixText(t *testing.T) {
	got, err := Build(Input{
		Profile:     "foundry-prod",
		Catalogue:   []catalog.Rule{rule("FND-SEC-001", "security", "must-have")},
		Definitions: []Definition{{RuleID: "FND-SEC-001", NoEvidenceState: StateUnknown}},
		Policy:      map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkState(t, got, "FND-SEC-001", StateUnknown)
	if len(got.TopActions) != 1 {
		t.Fatalf("top actions=%+v", got.TopActions)
	}
	rec := got.TopActions[0].Recommendation
	if strings.Contains(rec, "Fix it.") || !strings.HasPrefix(rec, "Obtain evidence") {
		t.Fatalf("recommendation=%q", rec)
	}
}

// Phase 4 loop 1 (lead N3): a clean decision must say how much was not assessed.
func TestDecisionStatesUnassessedCount(t *testing.T) {
	got, err := Build(Input{
		Profile:   "foundry-prod",
		Catalogue: []catalog.Rule{rule("FND-OPS-002", "operations", "nice-to-have"), rule("FND-OPS-003", "operations", "nice-to-have")},
		Definitions: []Definition{
			{RuleID: "FND-OPS-002", NoEvidenceState: StateSkipped},
			{RuleID: "FND-OPS-003", NoEvidenceState: StateSkipped},
		},
		Policy: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Decision, "Proceed within assessed scope") || !strings.Contains(got.Decision, "2 controls were not assessed") {
		t.Fatalf("decision=%q", got.Decision)
	}
}
