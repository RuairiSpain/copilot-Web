package waf

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func TestDefinitionsCoverEveryApplicableRule(t *testing.T) {
	rules, err := catalog.Load(context.Background(), ruledata.FS, ruledata.Root)
	if err != nil {
		t.Fatal(err)
	}
	defs := Definitions(rules)
	seen := map[string]assess.Definition{}
	for _, d := range defs {
		seen[d.RuleID] = d
	}
	for _, r := range rules {
		if !Applicable(r) {
			continue
		}
		d, ok := seen[r.ID]
		if !ok {
			t.Fatalf("missing definition for %s", r.ID)
		}
		if d.Pillar == "" || d.ControlID == "" || d.ControlTitle == "" {
			t.Fatalf("incomplete definition for %s: %+v", r.ID, d)
		}
		if len(r.Sources) > 0 && (r.Sources[0].URL == "" || r.Sources[0].LastVerified == "") {
			t.Fatalf("catalogue source missing url/date for %s", r.ID)
		}
	}
}
