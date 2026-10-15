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
				Mandatory:      true,
				SourceURL:      "https://learn.microsoft.com/example",
				LastVerified:   "2026-10-05",
				Recommendation: "Use managed identity.",
				Fix:            "identity: { type: 'SystemAssigned' }",
				Findings:       []sdk.Finding{{Evidence: "identity is missing", Location: sdk.Location{File: "infra/main.bicep", Line: 10, Column: 2}}},
			},
			{
				Title:          "DR strategy question",
				RuleID:         "FND-REL-007",
				Pillar:         "reliability",
				State:          assess.StateQuestion,
				QuestionPrompt: "Has the team recorded a disaster recovery strategy?",
				WARAReferences: []string{"dff62efe"},
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
	if strings.Contains(b.String(), "Top five actions") {
		t.Fatal("developer report should differ from owner summary")
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
