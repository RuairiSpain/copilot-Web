package catalog

import (
	"strings"
	"testing"
	"testing/fstest"
)

const goodVerified = `id: FND-SEC-001
version: 1
group: SEC
title: Foundry local authentication disabled
status: verified
phases: ["1"]
inputs: [bicep-arm]
basis: [security]
category: must-have
pillar: security
severity: {dev: warning, test: error, prod: error}
evidence: {description: "disableLocalAuth is true", confidence: certain}
recommendation: Disable local auth.
sources:
  - url: https://learn.microsoft.com/example
    lastVerified: "2026-10-04"
overlap: {decision: native, coverage: partial, rationale: "No Foundry-specific check exists."}
implementation: {owner: native, package: internal/rules/sec}
`

func load(t *testing.T, files map[string]string) ([]Rule, error) {
	t.Helper()
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return Load(m, "rules/catalog")
}

func TestValidate(t *testing.T) {
	seed := "id: FND-CFG-001\nversion: 1\ngroup: CFG\ntitle: t\nstatus: proposed\nphases: [\"1\"]\n"
	tests := []struct {
		name    string
		files   map[string]string
		opt     Options
		wantErr string // substring; empty means valid
	}{
		{"verified rule is valid", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": goodVerified}, Options{}, ""},
		{"proposed ok without gate", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": seed}, Options{}, ""},
		{"proposed fails phase0 gate", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": seed}, Options{Phase0Gate: true}, "still proposed"},
		{"verified without source", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": strings.Replace(goodVerified, "sources:\n  - url: https://learn.microsoft.com/example\n    lastVerified: \"2026-10-04\"\n", "", 1)}, Options{}, "needs at least one source"},
		{"bad date", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": strings.Replace(goodVerified, "2026-10-04", "4 Oct 2026", 1)}, Options{}, "lastVerified must be YYYY-MM-DD"},
		{"non-https source", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": strings.Replace(goodVerified, "https://learn", "http://learn", 1)}, Options{}, "must be https"},
		{"platform rule must be error everywhere", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": strings.Replace(strings.Replace(goodVerified, "basis: [security]", "basis: [platform]", 1), "dev: warning", "dev: warning", 1)}, Options{}, "platform-basis rules must be error"},
		{"missing decision", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": strings.Replace(goodVerified, "decision: native", "decision: maybe", 1)}, Options{}, "overlap.decision"},
		{"opinion needs basis", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": strings.Replace(goodVerified, "status: verified", "status: product-opinion", 1)}, Options{}, "basis \"opinion\""},
		{"native decision needs native owner", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": strings.Replace(goodVerified, "owner: native", "owner: psrule", 1)}, Options{}, "requires implementation.owner native"},
		{"group mismatch", map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": strings.Replace(goodVerified, "group: SEC", "group: NET", 1)}, Options{}, "does not match id group"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := load(t, tc.files)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			err = Validate(rules, tc.opt)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateDuplicateID(t *testing.T) {
	r := Rule{ID: "FND-CFG-001", Version: 1, Group: "CFG", Title: "t", Status: StatusProposed, Phases: []string{"1"}}
	err := Validate([]Rule{r, r}, Options{})
	if err == nil || !strings.Contains(err.Error(), "duplicate rule id") {
		t.Fatalf("error = %v, want duplicate rule id", err)
	}
}

func TestValidateEmpty(t *testing.T) {
	if err := Validate(nil, Options{}); err == nil {
		t.Fatal("empty catalogue must be an error")
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"unknown field", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\nbogus: 1\n"}, "field bogus not found"},
		{"file name mismatch", map[string]string{"rules/catalog/cfg/FND-CFG-002.yaml": "id: FND-CFG-001\n"}, "file name must equal rule id"},
		{"wrong directory", map[string]string{"rules/catalog/sec/FND-CFG-001.yaml": "id: FND-CFG-001\n"}, "must live in directory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.files)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestParseID(t *testing.T) {
	for id, ok := range map[string]bool{"FND-CFG-001": true, "FND-COST-004": true, "FND-IQ-012": true,
		"FND-cfg-001": false, "FND-CFG-1": false, "XYZ-CFG-001": false, "FND-CFG-00A": false, "FND--001": false} {
		if _, got := parseID(id); got != ok {
			t.Errorf("parseID(%q) ok=%v, want %v", id, got, ok)
		}
	}
}

func TestMarkdownDeterministic(t *testing.T) {
	a, _ := load(t, map[string]string{
		"rules/catalog/sec/FND-SEC-001.yaml": goodVerified,
		"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\nversion: 1\ngroup: CFG\ntitle: \"a|b\"\nstatus: proposed\nphases: [\"1\"]\n",
	})
	rev := []Rule{a[1], a[0]}
	if Markdown(a) != Markdown(rev) {
		t.Fatal("Markdown output depends on input order")
	}
	md := Markdown(a)
	for _, want := range []string{"DO NOT EDIT", "a\\|b", "| FND-SEC-001 |", "warning/error/error"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}
