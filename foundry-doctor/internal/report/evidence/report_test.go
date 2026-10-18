package evidence

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var update = flag.Bool("update", false, "update golden files")

func fixture() assess.Assessment {
	return assess.Assessment{
		Framework:   "azure-waf",
		Profile:     "foundry-prod",
		ToolVersion: "0.4.0-test",
		GeneratedAt: "2026-10-18T00:00:00Z",
		ScopeNote:   "azure.yaml only",
		Decision:    "Proceed with conditions.",
		Limitations: []string{
			"Not a certification.",
		},
		Summary: assess.Summary{States: map[assess.State]int{assess.StatePass: 1, assess.StateFail: 1, assess.StateWarning: 2, assess.StateSkipped: 1, assess.StateQuestion: 1, assess.StateAttested: 1, assess.StateNotApplicable: 1}},
		Controls: []assess.Control{
			{Title: "Passed control", RuleID: "FND-OPS-001", RuleVersion: 1, Pillar: "operations", State: assess.StatePass, Basis: assess.BasisDeterministic, Evaluated: true, SourceURL: "https://learn.microsoft.com/ops", LastVerified: "2026-10-04"},
			{Title: "Failed control", RuleID: "FND-IDN-001", Pillar: "security", State: assess.StateFail, Mandatory: true, Findings: []sdk.Finding{{Evidence: "identity missing", Severity: sdk.SeverityError, Fingerprint: "fp1"}}},
			{Title: "Suppressed control", RuleID: "FND-SEC-011", Pillar: "security", State: assess.StateWarning, Findings: []sdk.Finding{{Evidence: "accepted", Suppressed: &sdk.Suppression{Reason: "tracked", Owner: "team", Expires: "2027-01-01"}}}},
			{Title: "Baselined control", RuleID: "FND-NET-010", Pillar: "security", State: assess.StateWarning, Findings: []sdk.Finding{{Evidence: "old debt", Baselined: true}}},
			{Title: "Skipped control", RuleID: "FND-OPS-002", Pillar: "operations", State: assess.StateSkipped, Skipped: []sdk.Skip{{Reason: "input-unavailable", MissingCapability: "control plane"}}},
			{Title: "Question control", RuleID: "FND-REL-007", Pillar: "reliability", State: assess.StateQuestion, ProjectOpinion: true},
			{Title: "Attested control", RuleID: "FND-REL-008", Pillar: "reliability", State: assess.StateAttested, Basis: assess.BasisAttested, QuestionAnswer: "true"},
			{Title: "Not applicable control", RuleID: "FND-SEC-003", Pillar: "security", State: assess.StateNotApplicable, Skipped: []sdk.Skip{{Reason: "not-applicable: no Cosmos DB"}}},
		},
		Unassessed: []assess.ControlRef{{ControlID: "FND-OPS-002", RuleID: "FND-OPS-002", Title: "Skipped control", Pillar: "operations", State: assess.StateSkipped}},
		Costly:     []assess.Action{{ControlID: "FND-REL-001", RuleID: "FND-REL-001", Title: "Search replicas", Pillar: "reliability", State: assess.StateWarning, Costly: true, CostBasis: "Replicas are billed.", CostSourceURL: "https://learn.microsoft.com/azure/search/search-sku-manage-costs", CostVerified: "2026-10-18"}},
	}
}

func TestMarkdownGolden(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "evidence.md.golden", b.Bytes())
}

func TestJSONGolden(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "evidence.json.golden", append(b.Bytes(), '\n'))
}

func TestSuppressedAndBaselinedRemainVisible(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, want := range []string{"suppressed: accepted", "baselined: old debt", "suppression reason: tracked; owner: team; expires: `2027-01-01`", "fingerprint `fp1`", "## ATTESTED", "## NOT_APPLICABLE", "## Costly recommendations (optional)", "Tool version: `0.4.0-test`"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %s: %v", name, err)
	}
	if bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		return
	}
	t.Fatalf("golden mismatch:\n%s", got)
}
