package developer

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
		Profile:  "foundry-test",
		Decision: "Proceed with conditions.",
		Controls: []assess.Control{
			{
				Title:          "Managed identities used",
				RuleID:         "FND-IDN-001",
				Pillar:         "security",
				State:          assess.StateFail,
				RuleVersion:    2,
				Mandatory:      true,
				Basis:          assess.BasisDeterministic,
				StateReason:    "An active error-severity finding remains for the selected profile.",
				SourceURL:      "https://learn.microsoft.com/example",
				LastVerified:   "2026-10-05",
				Recommendation: "Use managed identity.",
				Fix:            "identity:\n  type: 'SystemAssigned'",
				Findings:       []sdk.Finding{{Severity: sdk.SeverityError, Evidence: "identity is missing", Location: sdk.Location{File: "infra/main.bicep", Line: 10, Column: 2}}},
			},
			{
				Title:          "DR strategy question",
				RuleID:         "FND-REL-007",
				Pillar:         "reliability",
				State:          assess.StateQuestion,
				Basis:          assess.BasisNone,
				ProjectOpinion: true,
				Costly:         true,
				CostBasis:      "Multi-region adds resources.",
				CostSourceURL:  "https://learn.microsoft.com/cost",
				QuestionPrompt: "Has the team recorded a disaster recovery strategy?",
				WARAReferences: []assess.WARARef{{ID: "dff62efe-c3a3-4621-98b3-c877c65cb195", Label: "AI Search multi-region", PGVerified: "false"}},
			},
			{
				Title:          "Telemetry sink exists",
				RuleID:         "FND-OPS-002",
				Pillar:         "operations",
				State:          assess.StateUnknown,
				ProjectOpinion: true,
				Skipped:        []sdk.Skip{{RuleID: "FND-OPS-002", Reason: "input-unavailable"}},
			},
		},
	}
}

func TestMarkdownGolden(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "developer.md.golden", b.Bytes())
}

func TestOutputsAreMateriallyDifferent(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if strings.Contains(s, "Top five actions") {
		t.Fatal("developer report should differ from owner summary")
	}
	for _, want := range []string{"## Operational Excellence", "  ```\n  identity:\n    type: 'SystemAssigned'\n  ```\n", "pgVerified: false", "Project opinion, not Microsoft guidance", "## Limitations"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
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
