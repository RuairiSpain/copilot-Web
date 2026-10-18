package owner

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
)

var update = flag.Bool("update", false, "update golden files")

func fixture() assess.Assessment {
	return assess.Assessment{
		Profile:   "foundry-prod",
		Decision:  "Proceed with conditions: mandatory controls need remediation or manual review.",
		ScopeNote: "local project evidence only",
		Limitations: []string{
			"Not a certification.",
			"Control-plane evidence was not available.",
		},
		Summary: assess.Summary{States: map[assess.State]int{
			assess.StateFail: 1, assess.StateWarning: 1, assess.StateUnknown: 1, assess.StateQuestion: 1, assess.StateSkipped: 1, assess.StatePass: 2,
			assess.StateAttested: 1, assess.StateNotApplicable: 1,
		}},
		Controls: []assess.Control{
			{Title: "DR plan recorded", RuleID: "FND-REL-007", Pillar: "reliability", State: assess.StateAttested, Basis: assess.BasisAttested, QuestionAnswer: "true; reference: https://contoso.example/dr"},
		},
		TopActions: []assess.Action{
			{Title: "Managed identities used", State: assess.StateFail, Pillar: "security", Mandatory: true, Recommendation: "Use managed identity.", SourceURL: "https://learn.microsoft.com/example", LastVerified: "2026-10-05"},
			{Title: "Telemetry connected", State: assess.StateWarning, Pillar: "operations", ProjectOpinion: true, Recommendation: "Connect Application Insights.", SourceURL: "https://learn.microsoft.com/example2", LastVerified: "2026-10-05"},
		},
		Costly: []assess.Action{
			{Title: "Search replica posture", State: assess.StateWarning, Pillar: "reliability", Recommendation: "Add replicas.", SourceURL: "https://learn.microsoft.com/search", LastVerified: "2026-10-04", CostBasis: "Replicas are billed per search unit.", CostSourceURL: "https://learn.microsoft.com/azure/search/search-sku-manage-costs", CostVerified: "2026-10-18"},
		},
		Unassessed: []assess.ControlRef{{Title: "Telemetry sink exists", Pillar: "operations", State: assess.StateSkipped, StateReason: "Skipped: input-unavailable."}},
	}
}

func TestMarkdownGolden(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "owner.md.golden", b.Bytes())
}

func TestOpinionAndCostlyAreLabelled(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, want := range []string{"## Costly recommendations (optional)", "Cost basis: Replicas are billed", "Project opinion, not Microsoft guidance", "## Team-attested answers", "Operational Excellence", "Skipped\\: input\\-unavailable"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
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
