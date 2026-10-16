package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestSummaryHiddenAndSkippedByReason(t *testing.T) {
	fs, run := fixture()
	run.Skipped = append(run.Skipped,
		sdk.Skip{RuleID: "FND-030", Reason: "input-unavailable: no template"},
		sdk.Skip{RuleID: "FND-031", Reason: "input-unavailable: no parameters"},
		sdk.Skip{RuleID: "FND-032", Reason: "not-applicable"})
	var b bytes.Buffer
	if err := JSON(&b, fs, run); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Summary struct {
			Hidden          int            `json:"hidden"`
			Skipped         int            `json:"skipped"`
			SkippedByReason map[string]int `json:"skippedByReason"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary.Hidden != 2 || got.Summary.Skipped != 5 {
		t.Fatalf("summary = %+v", got.Summary)
	}
	if m := got.Summary.SkippedByReason; m["input-unavailable"] != 2 || m["not-applicable"] != 1 || m["az not signed in"] != 1 {
		t.Fatalf("skippedByReason = %v", m)
	}
	// Keys are emitted sorted, so the output is byte-stable.
	if i, j := strings.Index(b.String(), `"az not signed in"`), strings.Index(b.String(), `"input-unavailable"`); i < 0 || j < 0 || i > j {
		t.Fatalf("skippedByReason keys are not sorted:\n%s", b.String())
	}
}

func TestSummaryHiddenZeroIsExplicit(t *testing.T) {
	var j, c, m bytes.Buffer
	_ = JSON(&j, nil, Run{})
	_ = Console(&c, nil, Run{})
	_ = Markdown(&m, nil, Run{})
	if !strings.Contains(j.String(), `"hidden": 0`) || !strings.Contains(j.String(), `"skippedByReason": {}`) {
		t.Fatalf("json must always carry hidden and skippedByReason:\n%s", j.String())
	}
	if !strings.Contains(c.String(), "0 hidden by --min-severity") {
		t.Fatalf("console:\n%s", c.String())
	}
	if !strings.Contains(m.String(), "Hidden by --min-severity") {
		t.Fatalf("markdown:\n%s", m.String())
	}
}
