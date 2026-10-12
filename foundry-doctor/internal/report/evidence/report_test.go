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
		Framework: "azure-waf",
		Profile:   "foundry-prod",
		Decision:  "Proceed with conditions.",
		Limitations: []string{
			"Not a certification.",
		},
		Controls: []assess.Control{
			{Title: "Passed control", RuleID: "FND-OPS-001", Pillar: "operations", State: assess.StatePass},
			{Title: "Failed control", RuleID: "FND-IDN-001", Pillar: "security", State: assess.StateFail, Findings: []sdk.Finding{{Evidence: "identity missing"}}},
			{Title: "Suppressed control", RuleID: "FND-SEC-011", Pillar: "security", State: assess.StateWarning, Findings: []sdk.Finding{{Evidence: "accepted", Suppressed: &sdk.Suppression{Reason: "tracked", Owner: "team"}}}},
			{Title: "Baselined control", RuleID: "FND-NET-010", Pillar: "security", State: assess.StateWarning, Findings: []sdk.Finding{{Evidence: "old debt", Baselined: true}}},
			{Title: "Skipped control", RuleID: "FND-OPS-002", Pillar: "operations", State: assess.StateSkipped, Skipped: []sdk.Skip{{Reason: "input-unavailable"}}},
			{Title: "Question control", RuleID: "FND-REL-007", Pillar: "reliability", State: assess.StateQuestion},
		},
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
	for _, want := range []string{"suppressed: accepted", "baselined: old debt"} {
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
