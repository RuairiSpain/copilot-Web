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
		{RuleID: "FND-COST-001", NoEvidenceState: StateUnknown, Costly: true},
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
	if got.Decision == "" || !strings.Contains(got.Decision, "mandatory") {
		t.Fatalf("decision=%q", got.Decision)
	}
	if len(got.TopActions) == 0 || got.TopActions[0].RuleID != "FND-SEC-001" {
		t.Fatalf("top actions=%+v", got.TopActions)
	}
}

func TestQuestionAnswerPromotesToPass(t *testing.T) {
	got, err := Build(Input{
		Profile: "foundry-prod",
		Catalogue: []catalog.Rule{
			rule("FND-REL-007", "reliability", "nice-to-have"),
		},
		Definitions: []Definition{
			{RuleID: "FND-REL-007", NoEvidenceState: StateQuestion, QuestionKey: "disasterRecovery.declared"},
		},
		Policy: map[string]any{"disasterRecovery.declared": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkState(t, got, "FND-REL-007", StatePass)
	if got.Controls[0].QuestionAnswer != "true" {
		t.Fatalf("answer=%q", got.Controls[0].QuestionAnswer)
	}
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
				t.Fatalf("%s state=%s want %s", ruleID, c.State, want)
			}
			return
		}
	}
	t.Fatalf("rule %s not found", ruleID)
}
