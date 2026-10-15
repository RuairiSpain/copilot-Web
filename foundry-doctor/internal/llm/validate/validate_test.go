package validate

import (
	"strings"
	"testing"
)

func TestNarrativeAcceptsKnownRules(t *testing.T) {
	out, err := Narrative(`{"summary":"Advisory summary.","sections":[{"heading":"Recommended remediation","summary":"Address the configured remediation.","actions":["Rotate config"],"ruleIds":["FND-IDN-001"]}]}`, Options{
		AllowedRuleIDs: map[string]struct{}{"FND-IDN-001": {}},
		AllowedActions: map[string]struct{}{"Rotate config": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Summary == "" || len(out.Sections) != 1 {
		t.Fatalf("out=%+v", out)
	}
}

func TestNarrativeRejectsHallucinatedRuleAndClaims(t *testing.T) {
	for _, raw := range []string{
		`{"summary":"All controls pass.","sections":[{"heading":"Recommended remediation","summary":"Everything passes.","actions":["Rotate config"],"ruleIds":["FND-IDN-001"]}]}`,
		`{"summary":"Advisory.","sections":[{"heading":"Severity downgraded","summary":"Do the fix.","actions":["Rotate config"],"ruleIds":["FND-IDN-001"]}]}`,
		`{"summary":"Advisory.","sections":[{"heading":"Priority","summary":"Do the fix.","actions":["Rotate config"],"ruleIds":["FND-IDN-001"]}]}`,
		`{"summary":"Advisory.","sections":[{"heading":"Recommended remediation","summary":"Do the fix.","actions":["Invent remediation"],"ruleIds":["FND-IDN-001"]}]}`,
		`{"summary":"Advisory.","sections":[{"heading":"Recommended remediation","summary":"Do the fix.","actions":["Rotate config"],"ruleIds":["FND-NOPE-999"]}]}`,
	} {
		if _, err := Narrative(raw, Options{
			AllowedRuleIDs: map[string]struct{}{"FND-IDN-001": {}},
			AllowedActions: map[string]struct{}{"Rotate config": {}},
		}); err == nil {
			t.Fatalf("expected rejection for %s", raw)
		}
	}
}

func TestNarrativeRejectsOmittedKnownRule(t *testing.T) {
	_, err := Narrative(`{"summary":"Advisory.","sections":[{"heading":"Recommended remediation","summary":"Do the fix.","actions":["Rotate config"],"ruleIds":["FND-IDN-001"]}]}`, Options{
		AllowedRuleIDs: map[string]struct{}{"FND-IDN-001": {}, "FND-NET-001": {}},
		AllowedActions: map[string]struct{}{"Rotate config": {}},
	})
	if err == nil || !strings.Contains(err.Error(), "omitted rule") {
		t.Fatalf("err=%v", err)
	}
}

func TestMarshalCanonical(t *testing.T) {
	b, err := MarshalCanonical(map[string]any{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\"a\": \"b\"") {
		t.Fatalf("got=%s", b)
	}
}
