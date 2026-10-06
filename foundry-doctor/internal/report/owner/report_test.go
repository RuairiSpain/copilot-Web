package owner

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
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
		}},
		TopActions: []assess.Action{
			{Title: "Managed identities used", State: assess.StateFail, Pillar: "security", Recommendation: "Use managed identity.", SourceURL: "https://learn.microsoft.com/example", LastVerified: "2026-10-05"},
			{Title: "DR strategy question", State: assess.StateQuestion, Pillar: "reliability", Recommendation: "Record the DR decision."},
		},
		Unassessed: []assess.ControlRef{{Title: "Telemetry sink exists", Pillar: "operations", State: assess.StateUnknown}},
	}
}

func TestMarkdownGolden(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "owner.md.golden", b.Bytes())
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
