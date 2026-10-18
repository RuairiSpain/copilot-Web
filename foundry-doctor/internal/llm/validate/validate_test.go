package validate

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
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

func TestNarrativeRejectsUnknownRuleIDsAnywhere(t *testing.T) {
	opt := Options{
		AllowedRuleIDs: map[string]struct{}{"FND-IDN-001": {}},
		AllowedActions: map[string]struct{}{"Rotate config": {}},
	}
	for name, raw := range map[string]string{
		"mixed ruleIds":   `{"summary":"Advisory.","sections":[{"heading":"Recommended remediation","summary":"Do the fix.","actions":["Rotate config"],"ruleIds":["FND-IDN-001","FND-NOPE-999"]}]}`,
		"top summary":     `{"summary":"FND-NET-009 is also failing in this project.","sections":[{"heading":"Recommended remediation","summary":"Do the fix.","actions":["Rotate config"],"ruleIds":["FND-IDN-001"]}]}`,
		"section summary": `{"summary":"Advisory.","sections":[{"heading":"Recommended remediation","summary":"See fnd-net-999 as well.","actions":["Rotate config"],"ruleIds":["FND-IDN-001"]}]}`,
		"heading":         `{"summary":"Advisory.","sections":[{"heading":"FND-NET-001 and identity","summary":"Do the fix.","actions":["Rotate config"],"ruleIds":["FND-IDN-001"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Narrative(raw, opt)
			if !errors.Is(err, llm.ErrRejected) || !strings.Contains(err.Error(), "not in the deterministic input") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestNarrativeAllowsKnownRuleIDInText(t *testing.T) {
	_, err := Narrative(`{"summary":"FND-IDN-001 needs attention.","sections":[{"heading":"Recommended remediation","summary":"Address fnd-idn-001.","actions":[],"ruleIds":["FND-IDN-001"]}]}`, Options{
		AllowedRuleIDs: map[string]struct{}{"FND-IDN-001": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeKeepsRuneBoundary(t *testing.T) {
	out := sanitize(strings.Repeat("é", 400))
	if !utf8.ValidString(out) || strings.ContainsRune(out, utf8.RuneError) {
		t.Fatalf("invalid utf-8 after truncation: %q", out)
	}
}
