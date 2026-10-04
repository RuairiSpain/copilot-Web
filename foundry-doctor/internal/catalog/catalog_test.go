package catalog

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

const goodVerified = `id: FND-SEC-001
version: 1
group: SEC
title: Foundry local authentication disabled
description: Local (key) authentication must be disabled on Foundry accounts.
status: verified
phases: ["1"]
inputs: [bicep-arm]
basis: [security]
category: must-have
pillar: security
severity: {dev: warning, test: error, prod: error}
compatibility: {apiVersions: ["2026-09-01"]}
evidence: {description: "disableLocalAuth is true", confidence: certain}
recommendation: Disable local auth.
sources:
  - url: https://learn.microsoft.com/example
    lastVerified: "2026-10-04"
overlap: {decision: native, coverage: partial, rationale: "No Foundry-specific check exists."}
implementation: {owner: native, package: internal/rules/sec}
tests: {positive: [local auth enabled], negative: [local auth disabled]}
`

const seedSEC = "id: FND-SEC-001\nversion: 1\ngroup: SEC\ntitle: t\nstatus: proposed\nphases: [\"1\"]\n"

const seedProposed = "id: FND-CFG-001\nversion: 1\ngroup: CFG\ntitle: t\nstatus: proposed\nphases: [\"1\"]\n"

// with returns goodVerified with the first occurrence of old replaced by new, failing if old is absent.
func with(t *testing.T, old, new string) string {
	t.Helper()
	if !strings.Contains(goodVerified, old) {
		t.Fatalf("fixture does not contain %q", old)
	}
	return strings.Replace(goodVerified, old, new, 1)
}

func mapFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func load(t *testing.T, files map[string]string) ([]Rule, error) {
	t.Helper()
	return Load(context.Background(), mapFS(files), "rules/catalog")
}

func one(content string) map[string]string {
	return map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": content}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		content string
		opt     Options
		wantErr string // substring; empty means valid
	}{
		{"verified rule is valid", goodVerified, Options{}, ""},
		{"proposed ok without gate", seedSEC, Options{}, ""},
		{"proposed fails phase0 gate", seedSEC, Options{Phase0Gate: true}, "still proposed"},
		{"verified without source", with(t, "sources:\n  - url: https://learn.microsoft.com/example\n    lastVerified: \"2026-10-04\"\n", ""), Options{}, "needs at least one source"},
		{"malformed date", with(t, "2026-10-04", "20x6-1x-0y"), Options{}, "valid YYYY-MM-DD date"},
		{"impossible date", with(t, "2026-10-04", "2026-13-45"), Options{}, "valid YYYY-MM-DD date"},
		{"non-https source", with(t, "https://learn", "http://learn"), Options{}, "must be https"},
		{"platform rule must be error everywhere", with(t, "basis: [security]", "basis: [platform]"), Options{}, "platform-basis rules must be error"},
		{"bad decision", with(t, "decision: native", "decision: maybe"), Options{}, "overlap.decision"},
		{"empty rationale", with(t, `rationale: "No Foundry-specific check exists."`, `rationale: ""`), Options{}, "overlap.rationale is required"},
		{"bad coverage", with(t, "coverage: partial", "coverage: lots"), Options{}, "overlap.coverage"},
		{"opinion needs opinion basis", with(t, "status: verified", "status: product-opinion"), Options{}, `basis "opinion"`},
		{"native decision needs native owner", with(t, "owner: native", "owner: psrule"), Options{}, "requires implementation.owner native"},
		{"wrap decision needs external owner", with(t, "decision: native", "decision: wrap"), Options{}, "requires an external"},
		{"group mismatch", with(t, "group: SEC", "group: NET"), Options{}, "does not match id group"},
		{"version zero", with(t, "version: 1", "version: 0"), Options{}, "version must be >= 1"},
		{"empty title", with(t, "title: Foundry local authentication disabled", `title: " "`), Options{}, "title is required"},
		{"unknown status", with(t, "status: verified", "status: maybe"), Options{}, "unknown status"},
		{"empty phases", with(t, `phases: ["1"]`, "phases: []"), Options{}, "phases is required"},
		{"unknown phase", with(t, `phases: ["1"]`, `phases: ["12"]`), Options{}, "unknown phase"},
		{"duplicate phase", with(t, `phases: ["1"]`, `phases: ["1", "1"]`), Options{}, "phases contains duplicate"},
		{"unknown input plane", with(t, "inputs: [bicep-arm]", "inputs: [telepathy]"), Options{}, "unknown input plane"},
		{"empty inputs", with(t, "inputs: [bicep-arm]", "inputs: []"), Options{}, "inputs is required"},
		{"unknown basis", with(t, "basis: [security]", "basis: [vibes]"), Options{}, "unknown basis"},
		{"unknown category", with(t, "category: must-have", "category: critical"), Options{}, "category must be"},
		{"unknown pillar", with(t, "pillar: security", "pillar: speed"), Options{}, "unknown pillar"},
		{"bad severity", with(t, "dev: warning", "dev: loud"), Options{}, "severity.dev"},
		{"bad confidence", with(t, "confidence: certain", "confidence: sure"), Options{}, "evidence.confidence"},
		{"empty evidence", with(t, `description: "disableLocalAuth is true"`, `description: ""`), Options{}, "evidence.description is required"},
		{"empty recommendation", with(t, "recommendation: Disable local auth.", `recommendation: ""`), Options{}, "recommendation is required"},
		{"empty description", with(t, "description: Local (key) authentication must be disabled on Foundry accounts.", `description: ""`), Options{}, "description is required"},
		{"no negative test", with(t, "negative: [local auth disabled]", "negative: []"), Options{}, "tests.positive and tests.negative"},
		{"no package", with(t, "package: internal/rules/sec", `package: ""`), Options{}, "implementation.package is required"},
		{"no compatibility", with(t, `compatibility: {apiVersions: ["2026-09-01"]}`, "compatibility: {}"), Options{}, "compatibility must name"},
		{"bad owner", with(t, "owner: native", "owner: nobody"), Options{}, "implementation.owner must be one of"},
		{"dropped needs drop decision", with(t, "status: verified", "status: dropped"), Options{}, "dropped rule must have overlap.decision drop"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := load(t, one(tc.content))
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

func TestValidateOwnerRules(t *testing.T) {
	base := func(decision, owner string) string {
		return with(t, "decision: native", "decision: "+decision) // owner replaced next
	}
	tests := []struct {
		decision, owner string
		wantErr         string
	}{
		{"native", "native", ""},
		{"adapt", "native", ""},
		{"adapt", "psrule", "decision adapt requires implementation.owner native"},
		{"native", "psrule", "decision native requires implementation.owner native"},
		{"wrap", "bicep", ""},
		{"reuse", "psrule", ""},
		{"wrap", "native", "decision wrap requires an external"},
		{"reuse", "native", "decision reuse requires an external"},
	}
	for _, tc := range tests {
		t.Run(tc.decision+"/"+tc.owner, func(t *testing.T) {
			content := strings.Replace(base(tc.decision, tc.owner), "owner: native", "owner: "+tc.owner, 1)
			rules, err := load(t, one(content))
			if err != nil {
				t.Fatal(err)
			}
			err = Validate(rules, Options{})
			if (tc.wantErr == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateStatusSpecific(t *testing.T) {
	// implemented needs a source just like verified
	noSource := strings.Replace(with(t, "status: verified", "status: implemented"),
		"sources:\n  - url: https://learn.microsoft.com/example\n    lastVerified: \"2026-10-04\"\n", "", 1)
	rules, _ := load(t, one(noSource))
	if err := Validate(rules, Options{}); err == nil || !strings.Contains(err.Error(), "implemented rule needs at least one source") {
		t.Fatalf("error = %v", err)
	}
	// a dropped rule with decision drop is valid and skips the remaining checks
	dropped := "id: FND-SEC-001\nversion: 1\ngroup: SEC\ntitle: t\nstatus: dropped\nphases: [\"1\"]\n" +
		"overlap: {decision: drop, coverage: full, rationale: covered by an Azure Policy built-in}\n"
	rules, _ = load(t, one(dropped))
	if err := Validate(rules, Options{}); err != nil {
		t.Fatalf("dropped rule: %v", err)
	}
	// product-opinion needs no source
	opinion := strings.Replace(with(t, "status: verified", "status: product-opinion"), "basis: [security]", "basis: [opinion]", 1)
	opinion = strings.Replace(opinion, "sources:\n  - url: https://learn.microsoft.com/example\n    lastVerified: \"2026-10-04\"\n", "", 1)
	rules, _ = load(t, one(opinion))
	if err := Validate(rules, Options{}); err != nil {
		t.Fatalf("product-opinion rule: %v", err)
	}
	// a platform rule with error everywhere is valid
	platform := strings.Replace(with(t, "basis: [security]", "basis: [platform]"), "dev: warning", "dev: error", 1)
	rules, _ = load(t, one(platform))
	if err := Validate(rules, Options{}); err != nil {
		t.Fatalf("platform rule: %v", err)
	}
}

func TestValidateBadIDAndDuplicate(t *testing.T) {
	if err := Validate([]Rule{{ID: "nope"}}, Options{}); err == nil || !strings.Contains(err.Error(), "id must match") {
		t.Fatalf("error = %v", err)
	}
	r := Rule{ID: "FND-CFG-001", Version: 1, Group: "CFG", Title: "t", Status: StatusProposed, Phases: []string{"1"}}
	if err := Validate([]Rule{r, r}, Options{}); err == nil || !strings.Contains(err.Error(), "duplicate rule id") {
		t.Fatalf("error = %v, want duplicate rule id", err)
	}
	if err := Validate(nil, Options{}); err == nil {
		t.Fatal("empty catalogue must be an error")
	}
}

func TestValidateOutputIsDeterministic(t *testing.T) {
	bad := with(t, "severity: {dev: warning, test: error, prod: error}", "severity: {dev: x, test: y, prod: z}")
	rules, err := load(t, one(bad))
	if err != nil {
		t.Fatal(err)
	}
	first := Validate(rules, Options{}).Error()
	for i := 0; i < 50; i++ {
		if got := Validate(rules, Options{}).Error(); got != first {
			t.Fatalf("run %d differs:\n%s\n---\n%s", i, first, got)
		}
	}
	if !(strings.Index(first, "severity.dev") < strings.Index(first, "severity.test") && strings.Index(first, "severity.test") < strings.Index(first, "severity.prod")) {
		t.Fatalf("severity errors out of order:\n%s", first)
	}
}

func TestLoadInvalidContent(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"unknown field", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\nbogus: 1\n"}, "field bogus not found"},
		{"duplicate key", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\nid: FND-CFG-001\n"}, "already defined"},
		{"file name mismatch", map[string]string{"rules/catalog/cfg/FND-CFG-002.yaml": "id: FND-CFG-001\n"}, "file name must equal rule id"},
		{"wrong directory", map[string]string{"rules/catalog/sec/FND-CFG-001.yaml": "id: FND-CFG-001\n"}, "must live in directory"},
		{"second document", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\n---\nid: FND-CFG-002\n"}, "more than one YAML document"},
		{"empty file", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": ""}, "file is empty"},
		{"too deep", map[string]string{"rules/catalog/x/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\n"}, "exactly <group>/<ID>.yaml"},
		{"too shallow", map[string]string{"rules/catalog/FND-CFG-001.yaml": "id: FND-CFG-001\n"}, "exactly <group>/<ID>.yaml"},
		{"stray file", map[string]string{"rules/catalog/cfg/notes.txt": "hello"}, "unexpected file"},
		{"yml extension", map[string]string{"rules/catalog/cfg/FND-CFG-001.yml": "id: FND-CFG-001\n"}, "unexpected file"},
		{"oversized file", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: " + strings.Repeat("a", maxRuleFileBytes+1)}, "exceeds"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.files)
			var inv *InvalidError
			if err == nil || !strings.Contains(err.Error(), tc.want) || !errors.As(err, &inv) {
				t.Fatalf("error = %v (InvalidError=%v), want InvalidError containing %q", err, errors.As(err, &inv), tc.want)
			}
		})
	}
}

func TestLoadRejectsSymlink(t *testing.T) {
	m := mapFS(map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\n"})
	m["rules/catalog/cfg/FND-CFG-001.yaml"].Mode = fs.ModeSymlink
	_, err := Load(context.Background(), m, "rules/catalog")
	var inv *InvalidError
	if !errors.As(err, &inv) || !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadIOErrorIsNotInvalidError(t *testing.T) {
	_, err := Load(context.Background(), fstest.MapFS{}, "rules/catalog")
	var inv *InvalidError
	if err == nil || errors.As(err, &inv) {
		t.Fatalf("missing root must be an I/O error, got %v", err)
	}
}

func TestLoadHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Load(ctx, mapFS(one(goodVerified)), "rules/catalog")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestLoadSortsByID(t *testing.T) {
	rules, err := load(t, map[string]string{
		"rules/catalog/sec/FND-SEC-001.yaml": goodVerified,
		"rules/catalog/cfg/FND-CFG-001.yaml": seedProposed,
	})
	if err != nil || len(rules) != 2 || rules[0].ID != "FND-CFG-001" {
		t.Fatalf("rules = %v, err = %v", rules, err)
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
